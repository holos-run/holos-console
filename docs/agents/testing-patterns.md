# Testing Patterns

Specific test frameworks and conventions for each layer.

See `docs/testing.md` for the complete decision rule, the ConnectRPC mock pattern with a worked example, file-naming conventions for route-directory test files, and a table of all existing test files.

## Go Tests

Standard `*_test.go` files with table-driven tests. Uses `k8s.io/client-go/kubernetes/fake` for K8s operations. CLI integration tests use `testscript` in `console/testscript_test.go`.

## UI Unit Tests

Vitest + React Testing Library + jsdom. Mock query hooks (`@/queries/*`) with `vi.mock()` and `vi.fn()`. Route-directory test files must be prefixed with `-` (e.g. `-about.test.tsx`) so TanStack Router's generator ignores them. Run with `make test-ui`.

## E2E Tests

Playwright in `frontend/e2e/`. `make test-e2e` orchestrates the full stack (builds Go binary, starts Go backend on :8443 and Vite on :5173). For tight iteration, start servers once and run targeted tests — see `docs/e2e-testing.md` for the full workflow including K8s-backed tests.

## Multi-Persona E2E Helpers

`frontend/e2e/helpers.ts` exports `getPersonaToken()`, `switchPersona()`, `loginAsPersona()`, and `apiGrantOrgAccess()` for tests that verify RBAC behavior across different roles. These helpers use the dev token endpoint (`POST /api/dev/token`) to obtain tokens and inject them into sessionStorage. See `docs/e2e-testing.md` for usage patterns.

## Related

- [Test Strategy](test-strategy.md) — When to use unit tests vs E2E
- [Contributing — Dev Tools and Persona Switching](../../CONTRIBUTING.md#dev-tools-and-persona-switching) — Test personas and dev token endpoint
- [Contributing — Testing](../../CONTRIBUTING.md#testing) — How to run individual tests
