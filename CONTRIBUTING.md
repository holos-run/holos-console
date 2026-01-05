# Contributing

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

### 2. Install mkcert for TLS Certificates

```bash
sudo apt-get install -y mkcert
```

### 3. Generate TLS Certificates

```bash
make certs
```

### 4. Install Frontend Dependencies

```bash
cd ui && npm install
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

Then open `https://localhost:5173/ui/` in your browser.

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
cd ui && npm run test:e2e
```

The `reuseExistingServer` option detects when servers are already running and skips starting new ones. This is useful for iterating on tests quickly or debugging specific failures.

## Authentication

The console uses an embedded OIDC identity provider (Dex) for development and testing.

### Default Credentials

For local development, use these credentials to log in:

- **Username:** `admin`
- **Password:** `verysecret`

### Customizing Credentials

Override the default credentials via environment variables:

```bash
export HOLOS_DEX_INITIAL_ADMIN_USERNAME=myuser
export HOLOS_DEX_INITIAL_ADMIN_PASSWORD=mypassword
make run
```

### Using an External Identity Provider

For production, point to an external OIDC provider:

```bash
./holos-console \
  --issuer=https://dex.example.com \
  --client-id=holos-console \
  --cert-file=server.crt \
  --key-file=server.key
```

The embedded Dex provider still runs but is ignored when `--issuer` points to an external URL.

See [docs/authentication.md](docs/authentication.md) for detailed documentation.

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
