# Embedded Services

The console server embeds external dependencies so that `make run` starts a fully functional system with zero external infrastructure. This document describes the approach and the conventions for adding new embedded services.

## Design Philosophy

**Configure as close to production as possible, optimize for developer experience and automation.**

The embedded services use the same protocols and interfaces as their production counterparts. The only differences are:

1. **No passwords or manual credentials** — embedded services use auto-login connectors, pre-shared tokens, or anonymous access so that `make run` and automated scripts work without interactive prompts.
2. **In-memory or ephemeral storage** — state is discarded on restart. This keeps the dev environment clean and reproducible.
3. **Single-process lifecycle** — embedded services start and stop with the console server. No separate daemons, docker-compose files, or background processes to manage.

The goal is a Heroku-like inner loop: change code, restart, and the full system is running. Automated agents and browser scripts depend on this property — they cannot enter passwords or start sidecar processes.

## Current Embedded Services

### Dex (OIDC Provider)

- **Flag**: `--enable-insecure-dex` (default: false)
- **Package**: `console/oidc/`
- **Mount**: HTTP handler at `/dex/`
- **Storage**: In-memory (all state lost on restart)
- **Auth**: Auto-login connector in dev mode — no password prompt. The `holosAuto` connector returns an identity immediately using `HOLOS_DEX_INITIAL_ADMIN_USERNAME` (default `admin`).
- **Lifecycle**: Created during `Serve()` as an `http.Handler`, mounted on the main mux. No explicit shutdown needed — stateless.
- **Production alternative**: External OIDC provider configured via `--issuer`

### NATS with JetStream (Event Backbone) — Planned

- **Flag**: `--enable-embedded-nats` (default: false, implied by `--enable-insecure-dex` in dev mode)
- **Package**: `console/nats/` (planned)
- **Storage**: In-memory JetStream with file-backed option for persistence across restarts
- **Auth**: No authentication in embedded mode. Production uses NATS credentials file via `--nats-creds-file`.
- **Lifecycle**: Embedded `nats-server` started as a goroutine during `Serve()`, drained on graceful shutdown.
- **Production alternative**: External NATS cluster configured via `--nats-url`
- **Use cases**: Harbor webhook event streaming, auto-deploy triggers, future alerting channels, Choria Machine Room integration

## Adding a New Embedded Service

Follow this pattern when embedding a new external dependency:

### 1. CLI Flags

Add an `--enable-embedded-<service>` flag that defaults to `false`. The flag enables the in-process server. Add a corresponding `--<service>-url` flag for connecting to an external instance in production.

When the embedded flag is set, the server should derive the connection URL automatically (e.g., `nats://localhost:<port>` for embedded NATS, `https://localhost:<port>/dex` for embedded Dex).

### 2. Package Structure

Create `console/<service>/` with:
- `embedded.go` — starts the in-process server, returns a handle for shutdown
- `client.go` — client/connection logic (used by both embedded and external modes)
- `config.go` — configuration types, environment variable handling
- `*_test.go` — tests using the embedded server directly (no external dependencies)

### 3. Lifecycle

- **Start**: During `console.Serve()`, after TLS setup but before the HTTP listener starts.
- **Ready**: The embedded service must be accepting connections before dependent services initialize. Use a health check or readiness probe.
- **Shutdown**: Graceful drain/stop in the server's shutdown sequence, before the HTTP listener closes.

### 4. Dev Mode Conventions

- **No passwords or tokens** in dev mode. Use anonymous access or pre-shared static tokens.
- **Deterministic ports** so that scripts and agents can connect without discovery. Derive from `--listen` address when possible.
- **Ephemeral by default** — in-memory storage. Optionally support a `--<service>-data-dir` flag for persistent state during development.

### 5. Testing

Embedding the real service in tests (instead of mocking) is preferred when the dependency is lightweight. The embedded NATS server (`github.com/nats-io/nats-server/v2/server`) starts in milliseconds and provides full JetStream — use it in Go tests. This matches the Dex pattern where tests use the real OIDC handler rather than mocking token validation.
