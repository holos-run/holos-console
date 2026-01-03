# Contributing

## Prerequisites

- Go 1.24.2 or later
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
