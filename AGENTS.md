# AGENTS.md

Agent context map for the `holos-console` code repo. For architecture, testing,
UI conventions, and the full catalog of cross-cutting guardrails, see the
companion
[AGENTS.md in holos-console-docs](https://github.com/holos-run/holos-console-docs/blob/main/AGENTS.md).

This code is not yet released. Do not preserve backwards compatibility when
making changes. Review `CONTRIBUTING.md` for commit message requirements before
opening a PR.

## Guardrails

- [Demo Docs Routing](https://github.com/holos-run/holos-console-docs/tree/main/demo) — Demo setup materials and CUE example snippets belong in `holos-run/holos-console-docs/demo/`, **not** in this repo; demo-related issues must include concrete examples and operator guidance.
- [Smoke Test Contract](https://github.com/holos-run/holos-console-docs/tree/main/demo/smoke-tests) — Smoke-test instructions must use `kubectl` commands for the resources required to observe the feature in the demo environment.
- [Demo README](https://github.com/holos-run/holos-console-docs/blob/main/demo/README.md) — Forward pointer to the demo setup order, prerequisites, and per-template walkthroughs.
