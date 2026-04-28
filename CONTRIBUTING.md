# Contributing

Working with Claude Code or another coding agent? Start with [AGENTS.md](AGENTS.md) for the repo-local guardrails and a pointer to the shared agent context in `holos-console-docs`.

## Prerequisites

- Go 1.25.0 or later
- Node.js 18+ and npm for frontend development
- [mkcert](https://github.com/FiloSottile/mkcert) for local TLS certificates
- [grpcurl](https://github.com/fullstorydev/grpcurl) for testing RPC endpoints

## Setting Up a Debian 13 (Trixie) VM

A fresh Debian 13 VM requires additional packages for development. Install them in this order:

### 1. Install Build Tools (Required for Go Race Detector)

Go tests run with `CGO_ENABLED=1` for race detection, which requires a C compiler:

```bash
sudo apt-get update
sudo apt-get install -y build-essential
```

### 2. Install mkcert and Trust the Local CA

`mkcert` provides the leaf TLS certificates used by the local Vite dev
server (`https://localhost:5173/`) and the Go backend
(`https://localhost:8443/`). Independently of leaf-cert generation, its
root CA must be registered with the system, Chromium, and Firefox trust
stores or Playwright-driven E2E tests fail at the TLS handshake before
any test logic runs. Chromium and Firefox use NSS, which `mkcert
-install` drives via `certutil` from the `libnss3-tools` package; if
`certutil` is missing, `mkcert -install` prints a warning and skips the
browser trust stores even though it still updates the system store.

```bash
sudo apt-get install -y mkcert libnss3-tools
mkcert -install
```

On the first run (fresh VM) you should see:

```
Created a new local CA at "<CAROOT>" 💥
The local CA is now installed in the system trust store! 👍
The local CA is now installed in the Firefox and/or Chrome/Chromium trust store! 👍
```

On subsequent runs the messages flip to `already installed`:

```
The local CA is already installed in the system trust store! 👍
The local CA is already installed in the Firefox and/or Chrome/Chromium trust store! 👍
```

If the Firefox/Chromium line is missing or replaced by a `certutil` warning,
re-check that `libnss3-tools` is installed. This matches the `Trust mkcert
CA` step in `.github/workflows/ci.yaml` (the recipe CI relies on for E2E
tests on Debian-based runners).

### 3. Generate TLS Certificates

```bash
make certs
```

### 4. Install Frontend Dependencies

```bash
cd frontend && npm install
cd ..
```

### 5. Install Go Tool Dependencies

```bash
make tools
```

### 6. Verify Setup

```bash
make test
```

All tests should pass after completing these steps.

## Getting Started

Clone the repository and install tool dependencies:

```bash
git clone https://github.com/holos-run/holos-console.git
cd holos-console
make tools
```

## Tool Dependencies

This project uses Go modules to pin tool versions. Tool dependencies are declared in [tools.go](tools.go) using the standard Go tools pattern. This ensures all contributors use the same tool versions.

To install all pinned tools:

```bash
make tools
```

This installs tools to `$GOPATH/bin`. Ensure `$GOPATH/bin` is in your `PATH`.

### Adding a New Tool

1. Add the import to `tools.go`:

```go
import (
    _ "github.com/bufbuild/buf/cmd/buf"
    _ "github.com/example/newtool"  // Add new tool
)
```

2. Run `go mod tidy` to update go.mod and go.sum
3. Run `make tools` to install

## Development Workflow

### Building

```bash
make build          # Build the executable
make debug          # Build with debug symbols
```

### Running Locally

Generate TLS certificates (one-time setup):

```bash
make certs
```

Start the server:

```bash
make run
```

### Frontend Development

For frontend development with hot reloading, run the Vite dev server alongside the Go backend. See [docs/dev-server.md](docs/dev-server.md) for detailed instructions.

Quick start:

```bash
# Terminal 1: Start Go backend
make run

# Terminal 2: Start Vite dev server
make dev
```

Then open `https://localhost:5173/` in your browser.

### Code Generation

Protocol buffer code is generated using buf. After modifying `.proto` files:

```bash
make generate
```

This runs `go generate ./...` which invokes buf via the directive in [generate.go](generate.go).

### Testing

```bash
make test           # Run tests
make rpc-version    # Test version RPC with grpcurl
```

### E2E Testing

E2E tests use Playwright. The test runner automatically starts and stops both servers:

```bash
make test-e2e
```

This command:
1. Builds the Go binary
2. Starts the Go backend on https://localhost:8443
3. Starts the Vite dev server on https://localhost:5173
4. Runs all Playwright tests
5. Cleans up both servers when tests finish

#### Debugging with Manual Servers

For debugging, you can start the servers manually and reuse them across test runs:

```bash
# Terminal 1: Start Go backend
make run

# Terminal 2: Start Vite dev server
make dev

# Terminal 3: Run E2E tests (reuses existing servers)
cd frontend && npm run test:e2e
```

The `reuseExistingServer` option detects when servers are already running and skips starting new ones. This is useful for iterating on tests quickly or debugging specific failures.

## Authentication

The embedded Dex OIDC provider is enabled by `make run` via `--enable-insecure-dex` and auto-logs in during local development. See [docs/authentication.md](docs/authentication.md) for detailed documentation including external OIDC provider configuration.

### Dev Tools and Persona Switching

`make run` passes `--enable-dev-tools`, which exposes a Dev Tools page at `/dev-tools` in the sidebar. The Dev Tools page provides an interactive persona switcher to test the application as different RBAC roles without restarting the server.

Available personas:

| Persona | Email | Role |
|---------|-------|------|
| Platform Engineer | `platform@localhost` | Owner |
| Product Engineer | `product@localhost` | Editor |
| SRE | `sre@localhost` | Viewer |

To switch personas in the UI: navigate to Dev Tools in the sidebar and click a persona card.

To obtain tokens for API testing via the command line:

```bash
curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" \
  -X POST https://localhost:8443/api/dev/token \
  -H "Content-Type: application/json" \
  -d '{"email":"product@localhost"}' | jq -r .id_token
```

See [docs/dev-token-endpoint.md](docs/dev-token-endpoint.md) for the full API reference.

## Annotation Keys for External Links

When working in `console/deployments/` or `console/links/`, refer to
[ADR 028: External Link Annotations on Deployment Resources](https://github.com/holos-run/cartographer/blob/main/docs/adrs/028-external-link-annotations.md)
and the template-author guide at
[CUE Template Guide → External Links](https://github.com/holos-run/cartographer/blob/main/docs/cue-template-guide.md#external-links).
The annotation key constants — `AnnotationExternalLinkPrefix`,
`AnnotationPrimaryURL`, `AnnotationAggregatedLinks`,
`AnnotationArgoCDLinkPrefix` — live in `api/v1alpha2/annotations.go`.

## ADR Open Questions

When an ADR defers a decision to a later ticket, open a placeholder Linear issue for it, link it from the ADR's "Open Questions" section, and close both the issue and the ADR open question when the work ships. This keeps deferred design questions tracked and prevents prose rot.

Example trail: HOL-664 (ADR-029 open question on wildcards) → HOL-767 (implementation umbrella) → Phases 1–5.

## Commit Messages

All commit messages must follow this format and include the root-cause analysis for why the issue happened, with citations to sources (for example, deep links to GitHub issues that describe the problem and its cause):

```
Without this patch ...  This patch fixes the problem by ...  Result: ... [AGENT INCLUDE VERIFICATION steps and output pasted into the commit]
```

### Formatting and Linting

```bash
make fmt            # Format code
make vet            # Run go vet
make lint           # Run linters (requires golangci-lint)
```

## Makefile Targets

Run `make help` to see all available targets:

```
make build          Build executable
make tools          Install tool dependencies
make generate       Generate code
make test           Run tests
make run            Run the server with generated certificates
make help           Display help menu
```
