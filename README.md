# Holos Console

A Go HTTPS server that serves a web console UI for managing Kubernetes secrets with OIDC authentication and role-based access control. The built UI is embedded into the Go binary for single-binary deployment.

## Quick Start

```bash
make certs   # Generate TLS certificates (one-time)
make run     # Build and start the server
```

Open <https://localhost:8443/ui> in your browser. The embedded Dex OIDC provider auto-logs in and redirects to the console.

## Reference Documentation

| Document | Description |
|----------|-------------|
| [CONTRIBUTING.md](CONTRIBUTING.md) | Development setup, build commands, testing, and commit message format |
| [AGENTS.md](AGENTS.md) | Agent and CI guidance for working with this codebase |
| [docs/authentication.md](docs/authentication.md) | OIDC authentication modes (embedded Dex PKCE and BFF oauth2-proxy) |
| [docs/rbac.md](docs/rbac.md) | Project-level grants, per-secret sharing grants, and permission model |
| [docs/secrets.md](docs/secrets.md) | Secret data model, UI workflows, and consuming secrets in pods |
| [docs/dev-server.md](docs/dev-server.md) | Two-server development setup (Go backend + Vite dev server) |
| [docs/hostname-configuration.md](docs/hostname-configuration.md) | Hostname and port configuration, reverse proxy setup |
| [docs/observability.md](docs/observability.md) | Structured logging, audit events, and Datadog integration |
| [docs/rpc-service-definitions.md](docs/rpc-service-definitions.md) | Protobuf and ConnectRPC code generation, adding new RPCs |
| [docs/adrs/](docs/adrs/) | Architecture Decision Records |
| [docs/research/](docs/research/) | Technical research and analysis documents |
