# CLAUDE.md

This file is a map of context for Claude Code agents working in this repository. Each link points to a single-idea document in `docs/agents/`. Follow the cross-links within each document to navigate related concepts.

This code is not yet released. Do not preserve backwards compatibility when making changes.

Before making changes, review `CONTRIBUTING.md` for commit message requirements.

## Architecture

- [Project Overview](docs/agents/project-overview.md) — Go HTTPS server with embedded UI serving a web console via ConnectRPC.
- [Package Structure](docs/agents/package-structure.md) — Go package layout: api, cmd, cli, console (services), proto, gen, frontend.
- [UI Architecture](docs/agents/ui-architecture.md) — React 19 + Vite 7 + TanStack Router/Query + shadcn/ui + ConnectRPC Query.
- [Embedded Services](docs/agents/embedded-services.md) — Dex and NATS embedded in the binary for zero-dependency dev mode.
- [Template Service](docs/agents/template-service.md) — Unified CUE-based templates at org, folder, and project scopes with explicit linking, semver releases, and version constraints.
- [Deployment Service](docs/agents/deployment-service.md) — Kubernetes Deployment CRUD with CUE rendering, reconcile, and rollback semantics.

## Authentication & Authorization

- [Authentication](docs/agents/authentication.md) — OIDC PKCE flow, embedded Dex, test personas, and dev token endpoint.
- [RBAC](docs/agents/rbac.md) — Four-tier grant model (org/folder/project/secret) on K8s annotations with namespace prefixes.

## Development Workflow

- [Build Commands](docs/agents/build-commands.md) — All make targets for building, testing, running, and versioning.
- [Pre-Commit Workflow](docs/agents/pre-commit.md) — Always run `make generate` before committing.
- [Code Generation](docs/agents/code-generation.md) — buf generates Go structs, ConnectRPC bindings, and TypeScript types from proto files.
- [Adding New RPCs](docs/agents/adding-rpcs.md) — Define proto, generate, implement handler, auto-wire.
- [API Access](docs/agents/api-access.md) — Call RPCs from the command line with curl or grpcurl.
- [Version Management](docs/agents/version-management.md) — Embedded version files, ldflags, container build workflow.
- [Tool Dependencies](docs/agents/tool-dependencies.md) — Pinned tools (buf) via Go tools pattern; CUE as a runtime dependency.

## UI Conventions

- [UI Styling Conventions](docs/agents/ui-styling.md) — Dark-only theme with semantic CSS tokens, shadcn/ui components, and Tailwind spacing.
- [Selection Components](docs/agents/selection-components.md) — Use Combobox for dynamic collections, Select only for small static enumerations.

## Testing

- [Test Strategy](docs/agents/test-strategy.md) — Prefer unit tests; reserve E2E for OIDC login and real K8s round-trips.
- [Testing Patterns](docs/agents/testing-patterns.md) — Go table-driven tests, Vitest + RTL for UI, Playwright for E2E, multi-persona helpers.

## Guardrails

- [Template Fields](docs/agents/guardrail-template-fields.md) — New proto fields must propagate through types, render pipeline, frontend preview, and defaults extraction.
- [Template Linking](docs/agents/guardrail-template-linking.md) — Linked templates annotation must use v1alpha2 format and call ListOrgTemplateSourcesForRender.
- [Template Docs](docs/agents/guardrail-template-docs.md) — Verify cue-template-guide.md completeness after any template or render change.
- [TLS Commands](docs/agents/guardrail-tls-commands.md) — Never use `-k`, `--insecure`, or `-plaintext` in any example command.
- [Terminology](docs/agents/guardrail-terminology.md) — Use "platform template" not "system template" for org/folder-level templates.
- [Resource Naming](docs/agents/guardrail-resource-naming.md) — Slug-based identifiers with six-digit collision suffix, never random-only.
- [URL Scheme](docs/agents/guardrail-url-scheme.md) — Top-level resources get dedicated URL prefixes; never nest one top-level resource under another.
- [Collection Index Pages](docs/agents/guardrail-collection-index.md) — Every resource collection must have an index/listing page at the root URL; settings live at a `/settings` subroute.
- [Searchable Collections](docs/agents/guardrail-searchable-collections.md) — All index pages and dynamic-collection combo boxes must include a search/filter input using TanStack Table `globalFilterFn: 'includesString'`.
- [Template-First Field Ordering](docs/agents/guardrail-template-first-field.md) — Template must be the first form field in Create Deployment; selecting it auto-populates all other fields.

## Planning & Execution

- [Implementing Plans](docs/agents/implementing-plans.md) — GitHub issue to feature branch to PR with merge commits.
- [Agent Slot Identification](docs/agents/agent-slot.md) — Derive slot from working directory path; include in PR footer, not title.
- [Red-Green Implementation](docs/agents/red-green.md) — Write failing tests first, then the minimum implementation.
- [Dispatching Plans](docs/agents/dispatching-plans.md) — `scripts/dispatch <issue-number>` creates a worktree and starts an agent.
- [Cleanup Phase](docs/agents/cleanup-phase.md) — Every plan ends with a scan for dead code, stale docs, and unused imports.
- [Tracking Progress](docs/agents/tracking-progress.md) — Check off TODO items in GitHub issues as phases complete.

## Browser Automation

- [Browser Automation](docs/agents/browser-automation.md) — agent-browser setup, authentication scripts, and screenshot capture.
- [Per-Agent Dev Servers](docs/agents/per-agent-dev-servers.md) — Deterministic port assignment (9000+N) with SIGPIPE lifecycle.
- [Visual Verification](docs/agents/visual-verification.md) — Screenshot capture workflow for PRs, only when explicitly requested.

## Skills & Contributing

- [Skills](docs/agents/skills.md) — Directory-based skill layout and current skill inventory.
- [Contributing](docs/agents/contributing.md) — Issue tracker for maintainers; Discord for community reports.
