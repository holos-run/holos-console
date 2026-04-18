# Holos Console

A Go HTTPS server with a React frontend that serves a web console UI and
exposes ConnectRPC services. The built UI is embedded into the Go binary via
`go:embed`.

## Development

See `AGENTS.md` for project conventions and the `Makefile` for common tasks:

- `make test` runs Go and UI unit tests (`test-go` and `test-ui`)
- `make generate` regenerates code when proto or schema files change
- `make test-e2e` runs Playwright end-to-end tests; the Playwright config
  starts the Go backend and Vite dev server automatically
