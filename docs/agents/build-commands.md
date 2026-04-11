# Build Commands

All make targets for building, testing, and running holos-console.

## Targets

```bash
make build          # Build executable to bin/holos-console
make debug          # Build with debug symbols to bin/holos-console-debug
make test           # Run all tests (Go + UI unit tests)
make test-go        # Run Go tests with race detector
make test-ui        # Run UI unit tests (one-shot)
make test-e2e       # Run E2E tests (builds binary, starts servers, runs Playwright)
make generate       # Run go generate (regenerates protobuf code + builds UI)
make tools          # Install pinned tool dependencies (buf)
make certs          # Generate TLS certificates with mkcert (one-time setup)
make run            # Build and run server with generated certificates
make dev            # Start Vite dev server with hot reload (use alongside make run)
make dispatch ISSUE=N  # Dispatch a plan issue to a Claude Code agent in a new worktree
make agent-tools    # Install agent-browser for browser automation
make cluster        # Create local k3d cluster (DNS + cluster + CA)
make bump-major     # Bump major version (resets minor and patch to 0)
make bump-minor     # Bump minor version (resets patch to 0)
make bump-patch     # Bump patch version
make tag            # Create annotated git tag from version files (never use git tag directly)
make fmt            # Format code
make vet            # Run go vet
make lint           # Run golangci-lint
make coverage       # Generate HTML coverage report
```

## Running Single Tests

```bash
# Go: single test by name
go test -v -run TestNewHandler_Success ./console/oidc

# UI unit: by file or test name
cd frontend && npm test -- SecretPage
cd frontend && npm test -- -t "displays error message"

# E2E: by test name
cd frontend && npx playwright test --grep "should complete full login flow"
```

## Related

- [Pre-Commit Workflow](pre-commit.md) — Always run `make generate` before committing
- [Testing Patterns](testing-patterns.md) — When to use Go tests vs UI tests vs E2E
- [Version Management](version-management.md) — Bump and tag procedures
- [Browser Automation](browser-automation.md) — `make agent-tools` setup
