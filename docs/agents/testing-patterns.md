# Testing Patterns

Specific test frameworks and conventions for each layer.

See `docs/testing.md` for the complete decision rule, the ConnectRPC mock pattern with a worked example, file-naming conventions for route-directory test files, and a table of all existing test files.

## Go Tests

Standard `*_test.go` files with table-driven tests. Uses `k8s.io/client-go/kubernetes/fake` for K8s operations. CLI integration tests use `testscript` in `console/testscript_test.go`.

### Pipeline seam / interface-injection pattern (HOL-812)

Multi-step pipelines (resolver → renderer → applier) expose small named
interfaces as seams so handler-level tests can inject fakes without pulling
the full dependency stack into test wiring. The production wiring lives in
`console/console.go` and threads real implementations through adapters.

Pattern in use: `console/projects/projectnspipeline` exposes
`BindingResolver`, `PolicyGetter`, `TemplateGetter`, `Renderer`, and
`Applier` interfaces. The handler-level test file
`console/projects/handler_project_namespace_test.go` defines inline fakes
for all five seams, wires them into a `projectnspipeline.Pipeline` via
`projectnspipeline.New(...)`, and wraps it in a local `pipelineAdapter`
that satisfies the handler's `ProjectNamespacePipeline` interface. Compile-
time interface assertions (`var _ Iface = (*fakeImpl)(nil)`) at the bottom
of the test file guard against silent drift when an interface is renamed or
a method signature changes.

Use this pattern whenever a handler delegates to a multi-step pipeline:
define one interface per seam, keep fakes inline in the `_test.go` file,
and add compile-time assertions for every seam.

### envtest for production-only SSA invariants (HOL-811)

Some invariants cannot be exercised against `k8s.io/client-go/kubernetes/fake`:
FieldManager enforcement, real namespace-controller `.status.phase` transitions,
and SSA merge semantics. `console/projects/projectapply/applier_envtest_test.go`
runs these cases against a real `envtest` apiserver. Scope envtest tests to
invariants the fake client cannot reach; keep the unit tests for per-branch
semantics that do not require a real apiserver.

## UI Unit Tests

Vitest + React Testing Library + jsdom. Mock query hooks (`@/queries/*`) with `vi.mock()` and `vi.fn()`. Route-directory test files must be prefixed with `-` (e.g. `-about.test.tsx`) so TanStack Router's generator ignores them. Run with `make test-ui`.

## E2E Tests

Playwright in `frontend/e2e/`. `make test-e2e` orchestrates the full stack (builds Go binary, starts Go backend on :8443 and Vite on :5173). For tight iteration, start servers once and run targeted tests — see `docs/e2e-testing.md` for the full workflow including K8s-backed tests.

## Multi-Persona E2E Helpers

`frontend/e2e/helpers.ts` exports `loginAsPersona()` and `apiGrantOrgAccess()` for tests that verify RBAC behavior across different roles. `loginAsPersona()` uses the dev token endpoint (`POST /api/dev/token`) to obtain a signed ID token and inject it into sessionStorage. See `docs/e2e-testing.md` for usage patterns.

## Related

- [Test Strategy](test-strategy.md) — When to use unit tests vs E2E
- [Contributing — Dev Tools and Persona Switching](../../CONTRIBUTING.md#dev-tools-and-persona-switching) — Test personas and dev token endpoint
- [Contributing — Testing](../../CONTRIBUTING.md#testing) — How to run individual tests
