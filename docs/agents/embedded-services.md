# Embedded Services

External dependencies (OIDC, NATS) are embedded in the console binary for dev mode and single-replica deployments. The approach: configure as close to production as possible, optimize for developer experience (no passwords, no sidecars), and support full automation. `make run` starts a complete system with zero external infrastructure.

See `docs/embedded-services.md` for the full pattern, lifecycle conventions, and instructions for adding new embedded services.

## Current Embedded Services

- **Dex** (`console/oidc/`) — OIDC provider, enabled via `--enable-insecure-dex`
- **NATS JetStream** (`console/nats/`, planned) — event backbone, enabled via `--enable-embedded-nats`

## Related

- [Authentication](authentication.md) — OIDC PKCE flow using embedded Dex
- [Project Overview](project-overview.md) — Single-binary architecture
- [Build Commands](build-commands.md) — `make run` starts the full system
