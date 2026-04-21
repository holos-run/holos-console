# AGENTS.md

Agent context map for the `holos-console` code repo. For architecture,
UI conventions, and the full catalog of cross-cutting guardrails, see the
companion
[AGENTS.md in holos-console-docs](https://github.com/holos-run/holos-console-docs/blob/main/AGENTS.md).

This code is not yet released. Do not preserve backwards compatibility when
making changes. Review `CONTRIBUTING.md` for commit message requirements before
opening a PR.

## Testing

All testing guidance lives in this repo. Read the entries below in order the first time; after that, jump straight to the doc you need.

1. [Test Strategy](docs/agents/test-strategy.md) — Decision rule: prefer unit tests; reserve E2E for the OIDC login flow (requires a real Dex server) and full-stack CRUD round-trips (require a real Kubernetes cluster).
2. [Testing Patterns](docs/agents/testing-patterns.md) — Frameworks and conventions per layer: Go table-driven tests with `k8s.io/client-go/kubernetes/fake`, Vitest + React Testing Library + jsdom for UI with `vi.mock('@/queries/*')` for ConnectRPC hooks, Playwright for E2E, plus the `loginAsPersona()` multi-persona helper.
3. [Testing Guide](docs/testing.md) — Full decision-rule table (what belongs in a unit vs. E2E test), the ConnectRPC mock worked example, route-directory test-file naming rules (`-<name>.test.tsx`), and the catalog of existing unit-test files.
4. [E2E Testing](docs/e2e-testing.md) — Tight iteration loop against running servers, port overrides, multi-persona helpers, and the per-spec table of which E2E tests require Kubernetes.
5. [E2E Refactor Audit](docs/agents/e2e-refactor-audit.md) — Historical per-spec verdict (Keep / Refactor-to-unit / Split / Delete) consumed by HOL-653 through HOL-658, and the post-refactor CI timing results (Results section: `E2E Tests` job dropped from ~11m 23s to ~7m 43s).

**Make targets**: `make test-go` (Go tests), `make test-ui` (Vitest unit tests), `make test-e2e` (Playwright, needs `make certs` and a k3d cluster), `make test` (all three). Run `make generate` before committing if proto or generated code is affected.

## Guardrails

- [Demo Docs Routing](https://github.com/holos-run/holos-console-docs/tree/main/demo) — Demo setup materials and CUE example snippets belong in `holos-run/holos-console-docs/demo/`, **not** in this repo; demo-related issues must include concrete examples and operator guidance.
- [Smoke Test Contract](https://github.com/holos-run/holos-console-docs/tree/main/demo/smoke-tests) — Smoke-test instructions must use `kubectl` commands for the resources required to observe the feature in the demo environment.
- [Demo README](https://github.com/holos-run/holos-console-docs/blob/main/demo/README.md) — Forward pointer to the demo setup order, prerequisites, and per-template walkthroughs.

## Example Template Registry

The UI picker on every "New Template" page is backed by a Go registry of built-in CUE drop-in examples. Adding a new example requires **only a single CUE file** — no Go or TypeScript changes.

### Adding a new example (drop-in workflow)

1. Create `console/templates/examples/<name>-<version>.cue` with four top-level fields:

   ```cue
   displayName: "Human Readable Name (version)"
   name:        "url-safe-slug-v1"
   description: "One sentence describing what the template produces."

   cueTemplate: """
     // CUE template body visible in the editor.
     // Reference #PlatformInput and #ProjectInput freely — they are
     // prepended by the renderer at evaluation time.
     platform: #PlatformInput
     projectResources: {}
     """
   ```

2. Run `make test-go` to confirm the new example compiles against the `v1alpha2` generated schema. The test in `examples_test.go` verifies every registry file.

3. That is all. The ConnectRPC `ListTemplateExamples` handler reads the registry at startup; the picker fetches from that RPC. No Go or TypeScript changes are required unless you also want to seed the example into a namespace via the populate-defaults flow.

### Docs-sync contract

The `holos-console-docs/demo/` directory hosts **demo walkthrough snippets** tied to the CI demo environment (full cluster configs, hard-coded gateway namespaces, etc.). The `console/templates/examples/` registry hosts **generic drop-in starters** intended for new contributors. The two sets are intentionally different — they serve different audiences.

The sync contract is: **both must compile** against the `v1alpha2` generated schema.

`console/templates/examples/docs_sync_test.go` enforces this contract for the pinned copies of docs snippets stored under `console/templates/examples/testdata/docs-snippets/`. When the docs repo updates a snippet, copy the new version into the corresponding `testdata/` subdirectory and run `make test-go` to confirm it still compiles:

```bash
# Update after a holos-console-docs change:
cp /path/to/holos-console-docs/demo/httpbin-v1/httpbin-v1.cue \
    console/templates/examples/testdata/docs-snippets/httpbin-v1/httpbin-v1.cue
cp /path/to/holos-console-docs/demo/allowed-resources/allowed-resources.cue \
    console/templates/examples/testdata/docs-snippets/allowed-resources/allowed-resources.cue
make test-go
```

## Binary layout

This repo ships two independent binaries from disjoint source trees per [ADR 031](https://github.com/holos-run/holos-console-docs/blob/main/docs/adrs/031-secret-injector-binary-split.md). The `holos-console` web application owns `cmd/holos-console/`, `api/templates/`, `internal/controller/`, `config/holos-console/{crd,rbac,admission}/`, and `Dockerfile.console`; the `holos-secret-injector` controller owns `cmd/secret-injector/`, `api/secrets/`, `internal/secretinjector/`, `config/secret-injector/{crd,rbac}/`, and `Dockerfile.secret-injector`. Shared infrastructure (`console/`, `frontend/`, `proto/`, `pkg/`) is fair game for either binary. The one hard invariant: **no cross-imports** between `internal/controller` and `internal/secretinjector`. `make check-imports` enforces this locally and in CI; if you find yourself reaching across the boundary, lift the shared code into `pkg/` instead. Secret material never travels through templates CRs — CRs carry metadata and `v1.Secret` refs only (ADR 031's no-sensitive-on-CRs rule).
