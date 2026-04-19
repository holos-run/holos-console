# E2E Testing

Playwright E2E tests live in `frontend/e2e/`. Tests are run via `make test-e2e`.

## Tight Iteration Loop

Playwright reuses running servers when `reuseExistingServer` is true (the default outside CI). Start servers once, then iterate on specific tests without restarting.

### Step 1 — Start servers

For tests that do **not** need Kubernetes (auth, sidebar navigation):

```bash
make build
make run &   # Go backend on :8443
make dev &   # Vite dev server on :5173
```

For tests that **do** need Kubernetes (orgs, projects, secrets):

```bash
make build
KUBECONFIG=$(k3d kubeconfig get workload) make run &
make dev &
```

> **Note:** Only one server can bind a port at a time. If `make run` fails with "address already in use", kill the existing process first:
> ```bash
> pkill -f holos-console
> ```

### Step 2 — Run a specific test

With servers running, Playwright reuses them and runs only the targeted tests:

```bash
cd frontend

# Run a single test by name pattern (chromium only, no retries):
npx playwright test --grep "should create secret with sharing" --project=chromium --reporter=list

# Run a whole spec file:
npx playwright test e2e/auth.spec.ts --project=chromium --reporter=list

# Run all tests as CI would:
make test-e2e
```

### Step 3 — Iterate

Edit the test or component, then re-run the same command. The servers stay running between runs.

When done:

```bash
pkill -f holos-console
pkill -f vite
```

## Port Overrides

`playwright.config.ts` reads `HOLOS_BACKEND_PORT` (default `8443`) and `HOLOS_VITE_PORT` (default `5173`). This allows running a second backend on a non-default port without stopping the main server:

```bash
# Start a K8s-backed backend on 8444 (main server stays on 8443):
KUBECONFIG=$(k3d kubeconfig get workload) ./bin/holos-console \
  --enable-insecure-dex --cert certs/tls.crt --key certs/tls.key --listen :8444 &

# Run tests against it (Playwright starts Vite on 5174, proxying to 8444):
cd frontend && HOLOS_BACKEND_PORT=8444 HOLOS_VITE_PORT=5174 \
  npx playwright test --grep "should create secret" --project=chromium

# Caveat: OIDC redirect URIs are hardcoded for :5173 in the Go server,
# so login flows break on :5174. Use the standard ports when possible.
```

## CI

The CI e2e job installs k3s so the full service stack (orgs, projects, secrets) is available. The `KUBECONFIG` is set in `$GITHUB_ENV` and inherited by the Go binary when Playwright starts it.

Tests that require Kubernetes time out in CI without k3s because `OrganizationService` and `ProjectService` are not registered when no kubeconfig is available.

## Multi-Persona Tests

E2E tests can authenticate as different test personas to verify RBAC behavior. The helpers in `frontend/e2e/helpers.ts` use the dev token endpoint (`POST /api/dev/token`) to obtain signed OIDC tokens and inject them into `sessionStorage`.

### Available Helpers

| Helper | Purpose |
|--------|---------|
| `loginAsPersona(page, email)` | Auto-login as admin, then switch to the requested persona |
| `apiGrantOrgAccess(page, org, email, role)` | Grant a persona a role on an org |

### Email Constants

```ts
import {
  ADMIN_EMAIL,              // admin@localhost
  PLATFORM_ENGINEER_EMAIL,  // platform@localhost
  PRODUCT_ENGINEER_EMAIL,   // product@localhost
  SRE_EMAIL,                // sre@localhost
} from './helpers'
```

### Example: Test RBAC across personas

```ts
test('editor cannot delete org', async ({ page }) => {
  // Login as admin (owner) and create an org
  await loginAsPersona(page, ADMIN_EMAIL)
  await apiCreateOrg(page, 'test-org')
  await apiGrantOrgAccess(page, 'test-org', PRODUCT_ENGINEER_EMAIL, 2) // EDITOR

  // Switch to product engineer (editor role) by re-logging in as that persona
  await loginAsPersona(page, PRODUCT_ENGINEER_EMAIL)

  // Verify the editor cannot delete the org
  // ... assertion logic ...

  // Cleanup as admin
  await loginAsPersona(page, ADMIN_EMAIL)
  await apiDeleteOrg(page, 'test-org')
})
```

### Notes

- The dev token endpoint is available whenever `--enable-insecure-dex` is set (always in E2E).
- `loginAsPersona()` first completes the OIDC auto-login flow (authenticating as admin), then switches to the requested persona if it is not admin. Internally it exchanges the persona email for a signed ID token via `POST /api/dev/token` and injects the token into `sessionStorage`.
- Call `loginAsPersona()` again mid-test to switch identities; the helper clears the prior session and reloads the page.
- The `multi-persona.spec.ts` test file demonstrates RBAC grant verification patterns across personas.

## Which Tests Need Kubernetes

| Test file | Tests | Needs K8s? |
|-----------|-------|-----------|
| `e2e/auth.spec.ts` | All | No |
| `e2e/multi-persona.spec.ts` | All (Multi-Persona RBAC) | Yes |
| `e2e/secrets.spec.ts` | sidebar navigation (first 2) | No |
| `e2e/secrets.spec.ts` | create/update/list secrets, add key | Yes |
