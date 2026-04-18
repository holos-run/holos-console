# Holos Console

A Go HTTPS server with a React frontend that serves a web console UI and
exposes ConnectRPC services. The built UI is embedded into the Go binary via
`go:embed`.

## Development

See `AGENTS.md` for project conventions and the `Makefile` for common tasks:

- `make test` runs the full test suite
- `make generate` regenerates code when proto or schema files change
- `make test-e2e` runs end-to-end tests against a local k3d cluster
