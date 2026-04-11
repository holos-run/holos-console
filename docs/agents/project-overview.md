# Project Overview

Holos Console is a Go HTTPS server that serves a web console UI and exposes ConnectRPC services, with the built UI embedded into the Go binary via `//go:embed` for single-binary deployment.

This code is not yet released. Do not preserve backwards compatibility when making changes.

Before making changes, review `CONTRIBUTING.md` for commit message requirements.

## Related

- [Package Structure](package-structure.md) — Go package layout and directory organization
- [UI Architecture](ui-architecture.md) — Frontend tech stack and key files
- [Embedded Services](embedded-services.md) — Services bundled into the binary for zero-dependency dev mode
- [Build Commands](build-commands.md) — All make targets for building, testing, and running
