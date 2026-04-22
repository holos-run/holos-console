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
| `src/components/cue-template-editor.test.tsx` | CUE editor: textarea, onChange, readOnly, save button, preview tab (platform input, project input, rendered YAML), render error, render status indicator |
| `src/components/env-var-editor.test.tsx` | Env var editor: add/remove rows, literal value, secretKeyRef, configMapKeyRef, name/key select population |
| `src/components/linkified-text.test.tsx` | LinkifiedText: plain text, single/multiple URLs, mid-sentence URL, empty/undefined, link styling |
| `src/routes/_authenticated/-about.test.tsx` | About page: Server Version card, license card |
| `src/routes/_authenticated/-profile.test.tsx` | Profile page: token claims, raw JSON view |
| `src/routes/_authenticated/projects/$projectName/secrets/-index.test.tsx` | Secrets list page: table, sorting, error/loading |
| `src/routes/_authenticated/projects/$projectName/secrets/-$name.test.tsx` | Secret detail page: display, edit, delete |
| `src/routes/_authenticated/projects/$projectName/settings/-settings.test.tsx` | Project settings page: display name, description, sharing, default secret sharing, delete |
| `src/routes/_authenticated/projects/$projectName/settings/-settings-deployments.test.tsx` | Project settings — Features section: deployments toggle, RBAC (owner/editor/viewer) |
| `src/routes/_authenticated/projects/$projectName/templates/-index.test.tsx` | Deployment templates list: template names, create/delete buttons, RBAC, empty/error state |
| `src/routes/_authenticated/projects/$projectName/templates/-$templateName.test.tsx` | Project-scoped template redirect shim: sr-only status, error alert, deferred navigation until project resolves, navigates to consolidated editor under `prj-*` namespace |
| `src/routes/_authenticated/projects/$projectName/templates/-new.test.tsx` | Create template page: form fields (display name, slug, description, CUE), slug auto-derivation, validation, cancel link |
| `src/routes/_authenticated/projects/$projectName/deployments/-index.test.tsx` | Deployments list: names, image/tag, status badges, create/delete, RBAC, empty/error state |
| `src/routes/_authenticated/projects/$projectName/deployments/-$deploymentName.test.tsx` | Deployment detail: image/tag, replicas, conditions, logs, re-deploy/delete, RBAC, tab layout (Status/Logs) with legacy `?tab=template` degrading to Status, App URL row sourced from `statusSummary.output.url` |
| `src/routes/_authenticated/projects/$projectName/deployments/-new.test.tsx` | Create deployment page: form fields (display name, slug, description, template, image, tag, command, args, env vars, port), Combobox template selector, defaults pre-fill (name/description from CUE defaults), RBAC |
| `src/routes/_authenticated/orgs/$orgName/settings/-settings.test.tsx` | Org settings page: display name, description, sharing, default sharing, delete |
| `src/routes/_authenticated/orgs/$orgName/templates/-index.test.tsx` | Org templates list: loading skeleton, error alert, empty state, cross-scope rows (org/folder/project namespaces), links to consolidated editor, search by display name/namespace/slug, Create Template button RBAC, empty-state OWNER vs non-OWNER prompts |
| `src/routes/_authenticated/orgs/$orgName/templates/-new.test.tsx` | Create org template page: page heading, Display Name/Name/Description/CUE fields, Enabled switch, Create and Cancel controls, slug auto-derivation, validation, form submission |
| `src/routes/_authenticated/orgs/$orgName/templates/-$namespace.$name.test.tsx` | Consolidated template editor: loading skeleton, error alert, namespace+name display, displayName heading, save via UpdateTemplate RPC, Delete button visibility, confirmation dialog open/cancel/confirm, post-delete navigation, TemplateExamplePicker integration (load/confirm/cancel) |
| `src/routes/_authenticated/orgs/$orgName/templates/-cross-scope.test.tsx` | Cross-scope equivalence (HOL-607): consolidated editor renders identical structure for org, folder, and project namespace prefixes |
| `src/routes/_authenticated/projects/-$projectName.test.tsx` | ProjectLayout: sets selected project from URL param |
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
| `src/queries/-templatePolicies.test.ts` | `aggregateFanOut` helper: idle/disabled queries, pending while fetching, org-only and org+folder concatenation, partial-failure tolerance, non-Error wrapping, empty input |
| `src/routes/_authenticated/organization/-new.test.tsx` | Create organization page: heading, full-page (not dialog), fields, slug auto-derivation, validation, submit, returnTo, error |
| `src/routes/_authenticated/folder/-new.test.tsx` | Create folder page: heading, full-page (not dialog), fields, slug auto-derivation, validation, submit, returnTo, error, orgName guard |
| `src/routes/_authenticated/project/-new.test.tsx` | Create project page: heading, full-page (not dialog), fields, slug auto-derivation, validation, submit, returnTo, error, orgName/folderName guards |
| `src/components/resource-grid/-resource-grid.test.tsx` | ResourceGrid v1: column headers, kind filter, lineage filter, global search, empty/loading/error states, delete-confirm dialog |
| `src/components/ui/-confirm-delete-dialog.test.tsx` | ConfirmDeleteDialog: open/close, confirm, error, isDeleting state |
| `src/components/-app-sidebar.tree.test.tsx` | Sidebar integration (real primitives): enabled entries render Link, disabled entries render tooltip-wrapped button |
| `src/components/templates/-templates-help-pane.test.tsx` | TemplatesHelpPane: renders help content sections |
| `src/components/resource-manager/-resource-tree.test.tsx` | ResourceTree: expand/collapse, leaf nodes, empty state |
| `src/routes/_authenticated/resource-manager/-index.test.tsx` | Resource Manager page: renders ResourceTree, loading/error states |
| `src/routes/_authenticated/projects/$projectName/templates/-index-help.test.tsx` | Templates index with help pane: ? icon toggle, pane visibility |
| `src/lib/-template-row-link.test.ts` | templateRowLink helper: namespace/name → detail href mapping |
| `src/components/template-policy-bindings/BindingForm.test.tsx` | BindingForm: ProjectTemplate / Deployment / ProjectNamespace kind selection, project-name field shown/hidden, wildcard validation, submit / save paths (HOL-814) |
| `src/index.test.ts` | App entry point smoke test |

### Go test files

| File | What it covers |
|---|---|
| `console/projects/handler_project_namespace_test.go` | Four HOL-812 ACs: no bindings → typed-create path; one binding → applier path + typed-create skipped; render error → `CodeInternal`; apply timeout → `CodeDeadlineExceeded`. Uses inline fakes for all five pipeline seams. |
| `console/projects/projectapply/applier_test.go` | Per-branch SSA semantics (cluster-scoped, namespace, namespace-scoped ordering; `DeadlineExceededError`; merge conflict detection) via `dynamicfake` client. |
| `console/projects/projectapply/applier_envtest_test.go` | Production-only invariants against a real `envtest` apiserver: FieldManager enforcement, real namespace-controller `.status.phase` transition, end-to-end apply ordering. |
| `console/templates/examples/examples_test.go` | Registry loads all four built-in examples; each compiles against the `v1alpha2` generated schema. |
