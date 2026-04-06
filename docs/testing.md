# Testing Guide

## Decision Rule: Unit Tests First

**Prefer unit tests. Use E2E only when a real server or Kubernetes cluster is necessary.**

| Behaviour to test | Use |
|---|---|
| Component renders correct DOM given props or state | Unit test |
| Interaction changes displayed state (sort, toggle, filter) | Unit test |
| Navigation logic triggered by user action (picker selects org, nav items change) | Unit test |
| ConnectRPC data shapes up in the UI (list, grid, badges) | Unit test with mocked query hooks |
| Full OIDC login/redirect flow | E2E (`auth.spec.ts`) |
| Secret CRUD round-trip against a real Kubernetes API server | E2E (`secrets.spec.ts`) |
| Picker selection triggers a real route navigation | E2E (`navigation.spec.ts`) |

**Why:** E2E tests are slow (30 s server startup, serial execution, 2 retries in CI), brittle (selector churn, timing), and require a full cluster for most pages. Unit tests with mocked RPC data give the same rendering confidence in milliseconds with no infrastructure.

## Running Tests

```bash
make test-ui    # Fast: Vitest unit tests, no cluster required (< 5 s)
make test-e2e   # Slow: Playwright E2E, needs Go backend + K8s cluster
```

### Running individual tests

```bash
# Unit: by file or test name
cd frontend && npm test -- SecretPage
cd frontend && npm test -- -t "renders table with Name"

# E2E: by test name
cd frontend && npx playwright test --grep "full login flow"
```

## Mocking ConnectRPC Query Hooks

The query hooks in `frontend/src/queries/` wrap ConnectRPC clients.  Unit tests
mock these modules directly with `vi.mock`, following the same pattern already
used for `useAuth`, `useOrg`, and `useVersion`.

### Pattern

```typescript
// At the top of the test file, before any imports
vi.mock('@/queries/secrets', () => ({
  useListSecrets: vi.fn(),
  useCreateSecret: vi.fn(),
  useDeleteSecret: vi.fn(),
}))

vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))

import type { Mock } from 'vitest'
import { useListSecrets, useCreateSecret, useDeleteSecret } from '@/queries/secrets'
import { useAuth } from '@/lib/auth'
import { SecretsListPage } from './index'

// In each test (or a shared helper):
;(useListSecrets as Mock).mockReturnValue({
  data: [
    { name: 'my-secret', description: 'Desc', accessible: true, userGrants: [], roleGrants: [] },
  ],
  isLoading: false,
  error: null,
})
;(useCreateSecret as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, reset: vi.fn() })
;(useDeleteSecret as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, reset: vi.fn(), error: null })
;(useAuth as Mock).mockReturnValue({
  isAuthenticated: true,
  isLoading: false,
  user: { profile: { email: 'test@example.com' } },
})
```

### Mocking Route Parameters

Page components that call `Route.useParams()` need the router mock to provide a
`useParams` implementation.  Override `createFileRoute` so that the exported
`Route` object has a callable `useParams`:

```typescript
vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({ useParams: () => ({ projectName: 'test-project' }) }),
    Link: ({ children }: { children: React.ReactNode }) => <a href="#">{children}</a>,
    useNavigate: () => vi.fn(),
  }
})
```

`vi.mock` calls are hoisted to the top of the file by Vitest, so the mock is
active before the module-under-test is imported.

### Mutation Return Values

Mock mutations return objects that match `useMutation`'s shape:

```typescript
;(useSomeMutation as Mock).mockReturnValue({
  mutateAsync: vi.fn().mockResolvedValue({ /* response shape */ }),
  isPending: false,
  reset: vi.fn(),
  error: null,
})
```

For testing pending/error states, override `isPending: true` or `error: new Error('...')`.

### Worked Example: SecretsListPage

See `frontend/src/routes/_authenticated/projects/$projectName/secrets/-index.test.tsx`
for a complete example covering:

- Table column headers rendered
- Secret name links rendered
- Sharing summary badge
- Sort toggle (ascending → descending → ascending)
- Empty state
- Loading skeleton when auth is loading
- Error state when fetch fails

## File Naming Convention

Test files inside `frontend/src/routes/` must be prefixed with `-` so TanStack
Router's file-based routing ignores them:

```
src/routes/_authenticated/-about.test.tsx        ✓
src/routes/_authenticated/about.test.tsx         ✗  (causes a route tree warning)
```

Test files in `src/components/` and `src/lib/` can use any name.

## Existing Test Files

| File | What it covers |
|---|---|
| `src/components/app-sidebar.test.tsx` | Sidebar rendering: footer links, version, project/org pickers, nav items |
| `src/components/view-mode-toggle.test.tsx` | Data/Resource and Claims/Raw toggle buttons |
| `src/components/secret-data-grid.test.tsx` | Key-value grid: add/remove rows, trailing newline, copy toast |
| `src/components/sharing-panel.test.tsx` | Grant display, edit mode, save, cancel, nbf/exp |
| `src/components/raw-view.test.tsx` | JSON pretty-print, field filtering, copy |
| `src/components/secret-data-editor.test.tsx` | Editor add/remove key |
| `src/components/secret-data-viewer.test.tsx` | Viewer reveal/hide/copy |
| `src/components/cue-template-editor.test.tsx` | CUE editor: textarea, onChange, readOnly, save button, preview tab (system/user input, rendered YAML), render error, render status indicator |
| `src/components/env-var-editor.test.tsx` | Env var editor: add/remove rows, literal value, secretKeyRef, configMapKeyRef, name/key select population |
| `src/components/linkified-text.test.tsx` | LinkifiedText: plain text, single/multiple URLs, mid-sentence URL, empty/undefined, link styling |
| `src/routes/_authenticated/-about.test.tsx` | About page: Server Version card, license card |
| `src/routes/_authenticated/-profile.test.tsx` | Profile page: token claims, raw JSON view |
| `src/routes/_authenticated/projects/$projectName/secrets/-index.test.tsx` | Secrets list page: table, sorting, error/loading |
| `src/routes/_authenticated/projects/$projectName/secrets/-$name.test.tsx` | Secret detail page: display, edit, delete |
| `src/routes/_authenticated/projects/$projectName/settings/-settings.test.tsx` | Project settings page: display name, description, sharing, default secret sharing, delete |
| `src/routes/_authenticated/projects/$projectName/settings/-settings-deployments.test.tsx` | Project settings — Features section: deployments toggle, RBAC (owner/editor/viewer) |
| `src/routes/_authenticated/projects/$projectName/templates/-index.test.tsx` | Deployment templates list: template names, create/delete buttons, RBAC, empty/error state |
| `src/routes/_authenticated/projects/$projectName/templates/-$templateName.test.tsx` | Deployment template detail: CUE editor, save/delete, RBAC, skeleton, error state |
| `src/routes/_authenticated/projects/$projectName/templates/-new.test.tsx` | Create template page: form fields (display name, slug, description, CUE), slug auto-derivation, validation, cancel link |
| `src/routes/_authenticated/projects/$projectName/deployments/-index.test.tsx` | Deployments list: names, image/tag, status badges, create/delete, RBAC, empty/error state |
| `src/routes/_authenticated/projects/$projectName/deployments/-$deploymentName.test.tsx` | Deployment detail: image/tag, replicas, conditions, logs, re-deploy/delete, RBAC, Template Preview section, grpcurl Command section |
| `src/routes/_authenticated/projects/$projectName/deployments/-new.test.tsx` | Create deployment page: form fields (display name, slug, description, template, image, tag, command, args, env vars, port), RBAC |
| `src/routes/_authenticated/orgs/$orgName/settings/-settings.test.tsx` | Org settings page: display name, description, sharing, default sharing, delete |
| `src/routes/_authenticated/orgs/$orgName/settings/-system-templates.test.tsx` | System templates list and detail: names, descriptions, mandatory badge, RBAC (owner/viewer), skeleton, error state |
| `src/routes/_authenticated/projects/-$projectName.test.tsx` | ProjectLayout: sets selected project from URL param |
| `src/routes/_authenticated/orgs/$orgName/projects/-index.test.tsx` | Org projects page: list, navigate to project |
| `src/routes/-_authenticated.test.tsx` | Auth layout: silent renewal, OIDC redirect |
| `src/lib/isOwner.test.ts` | RBAC owner check logic |
| `src/lib/org-context.test.tsx` | Org context: persistence, reset, filtering |
| `src/lib/project-context.test.tsx` | Project context: persistence, reset, filtering |
| `src/lib/-query-client.test.ts` | QueryClient retry logic: suppresses Unauthenticated errors, retries other errors up to 3 times |
| `src/lib/slug.test.ts` | Slug generation from display names |
| `src/lib/transport.test.ts` | Token storage and transport setup |
| `src/hooks/-use-debounced-value.test.ts` | useDebouncedValue: initial value, delay behavior, timer reset on rapid changes, default delay |
| `src/queries/-organizations.test.ts` | Organization query hooks: get, update, sharing, default sharing, delete |
| `src/queries/-projects.test.ts` | useListProjects and useCreateProject hooks |
| `src/components/create-org-dialog.test.tsx` | Create organization dialog: validation, submission |
| `src/components/create-project-dialog.test.tsx` | Create project dialog: validation, submission |
| `src/index.test.ts` | App entry point smoke test |
