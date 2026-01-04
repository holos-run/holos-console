# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Before making changes, review `CONTRIBUTING.md` for commit message requirements.

## Implementing Plans

When implementing a plan from `docs/plans/`:

1. **Mark steps complete in the plan file** - Before committing, update the plan's checklist to mark completed steps (change `- [ ]` to `- [x]`). Include the plan file in the commit.

2. **Include step identifiers in commit messages** - Put the step identifier (e.g., 1.1, 1.2, 2.1) at the END of the first line in parentheses.

Example workflow:
```bash
# 1. Implement step 1.1
# 2. Edit docs/plans/my-plan.md to mark step 1.1 as [x]
# 3. Commit both the implementation and the updated plan
git add src/file.ts docs/plans/my-plan.md
git commit -m "Add webServer configuration to playwright.config.ts (1.1)

Configure Playwright to automatically start Go backend and Vite dev
server before running E2E tests."
```

## Build Commands

```bash
make build          # Build executable to bin/holos-console
make debug          # Build with debug symbols to bin/holos-console-debug
make test           # Run tests with race detector
make generate       # Run go generate (regenerates protobuf code)
make tools          # Install pinned tool dependencies (buf)
make certs          # Generate TLS certificates with mkcert (one-time setup)
make run            # Build and run server with generated certificates
make fmt            # Format code
make vet            # Run go vet
make lint           # Run golangci-lint
```

## Architecture

This is a Go HTTPS server that serves a web console UI and exposes ConnectRPC services.

### Package Structure

- `cmd/` - Main entrypoint, calls into cli package
- `cli/` - Cobra CLI setup, exposes `Command()` and `Run()` functions
- `console/` - Core server package
  - `console.go` - HTTP server setup, TLS, route registration, embedded UI serving
  - `version.go` - Version info with embedded version files and ldflags
  - `rpc/` - ConnectRPC handler implementations
  - `ui/` - Embedded static files served at `/ui/` (build output, not source)
- `proto/` - Protobuf source files
- `gen/` - Generated protobuf Go code (do not edit)

### Code Generation

Protobuf code is generated using buf. The `generate.go` file contains the `//go:generate buf generate` directive. After modifying `.proto` files in `proto/`, run:

```bash
make generate   # or: go generate ./...
```

This produces:
- `gen/**/*.pb.go` - Go structs for messages
- `gen/**/consolev1connect/*.connect.go` - ConnectRPC client/server bindings

### Adding New RPCs

1. Define RPC and messages in `proto/holos/console/v1/*.proto`
2. Run `make generate`
3. Implement handler method in `console/rpc/` (embed `Unimplemented*Handler` for forward compatibility)
4. Handler is auto-wired when service is registered in `console/console.go`

See `docs/rpc-service-definitions.md` for detailed examples.

### Version Management

Version is determined by:
1. `console/version/{major,minor,patch}` files (embedded at compile time)
2. `GitDescribe` ldflags override (set by Makefile during build)

Build metadata (commit, tree state, date) injected via ldflags in Makefile.

### Tool Dependencies

Tool versions are pinned in `tools.go` using the Go tools pattern. Install with `make tools`. Currently pins: buf.
