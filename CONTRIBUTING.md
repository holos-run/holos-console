# Contributing

## Prerequisites

- Go 1.25.0 or later
- Node.js 18+ and npm for frontend development
- [mkcert](https://github.com/FiloSottile/mkcert) for local TLS certificates
- [grpcurl](https://github.com/fullstorydev/grpcurl) for testing RPC endpoints

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

E2E tests use Playwright and require both servers to be running:

```bash
# Terminal 1: Start Go backend
make run

# Terminal 2: Start Vite dev server
cd ui && npm run dev

# Terminal 3: Run E2E tests
cd ui && npm run test:e2e
```

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
