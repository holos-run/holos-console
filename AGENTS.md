# AGENTS.md

Agent context map for the `holos-console` code repo. For architecture,
UI conventions, and the full catalog of cross-cutting guardrails, see the
companion
[AGENTS.md in holos-console-docs](https://github.com/holos-run/holos-console-docs/blob/main/AGENTS.md).

This code is not yet released. Do not preserve backwards compatibility when
making changes. Review `CONTRIBUTING.md` for commit message requirements before
opening a PR.

## Testing

- [Test Strategy](docs/agents/test-strategy.md) — Prefer unit tests; reserve E2E for OIDC login and real K8s round-trips.
- [Testing Patterns](docs/agents/testing-patterns.md) — Go table-driven tests, Vitest + RTL for UI, Playwright for E2E, multi-persona helpers.
- [Testing Guide](docs/testing.md) — Full decision-rule table and ConnectRPC mock worked example.
- [E2E Testing](docs/e2e-testing.md) — Tight iteration loop, port overrides, multi-persona helpers, and which tests need Kubernetes.
- [E2E Refactor Audit](docs/agents/e2e-refactor-audit.md) — Per-spec verdict (Keep / Refactor-to-unit / Split / Delete) with target test files and mocks for each row; consumed by HOL-653 through HOL-658.

## Guardrails

- [Demo Docs Routing](https://github.com/holos-run/holos-console-docs/tree/main/demo) — Demo setup materials and CUE example snippets belong in `holos-run/holos-console-docs/demo/`, **not** in this repo; demo-related issues must include concrete examples and operator guidance.
- [Smoke Test Contract](https://github.com/holos-run/holos-console-docs/tree/main/demo/smoke-tests) — Smoke-test instructions must use `kubectl` commands for the resources required to observe the feature in the demo environment.
- [Demo README](https://github.com/holos-run/holos-console-docs/blob/main/demo/README.md) — Forward pointer to the demo setup order, prerequisites, and per-template walkthroughs.
