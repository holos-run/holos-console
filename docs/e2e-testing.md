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

## Which Tests Need Kubernetes

| Test file | Tests | Needs K8s? |
|-----------|-------|-----------|
| `e2e/auth.spec.ts` | All | No |
| `e2e/secrets.spec.ts` | sidebar navigation (first 2) | No |
| `e2e/secrets.spec.ts` | create/update/list secrets, add key | Yes |
