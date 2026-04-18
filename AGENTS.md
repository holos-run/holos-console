# AGENTS.md

Agent context map for the `holos-console` code repo. For architecture, testing,
UI conventions, and the full catalog of cross-cutting guardrails, see the
companion
[AGENTS.md in holos-console-docs](https://github.com/holos-run/holos-console-docs/blob/main/AGENTS.md).

This code is not yet released. Do not preserve backwards compatibility when
making changes. Review `CONTRIBUTING.md` for commit message requirements before
opening a PR.

## Guardrails

- **Demo docs routing** — Demo setup materials and CUE example snippets belong
  in
  [`holos-run/holos-console-docs/demo/`](https://github.com/holos-run/holos-console-docs/tree/main/demo),
  **not** in this repo. Demo-related issues must include concrete examples and
  operator guidance. Smoke-test instructions must include `kubectl` commands
  for the resources required to observe the feature in the demo environment.
  Forward pointers:
  [demo README](https://github.com/holos-run/holos-console-docs/blob/main/demo/README.md)
  and
  [smoke tests](https://github.com/holos-run/holos-console-docs/tree/main/demo/smoke-tests).
