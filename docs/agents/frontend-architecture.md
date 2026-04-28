# Frontend Architecture

> **Agent quick-read** (~5 min). Navigation hub for frontend stack decisions.
> Source of record: HOL-944. For detailed UX behavior, defer to the linked
> `docs/ui/` contracts.

## Stack Boundary

The application in `frontend/` is a Vite + React single-page app using
TanStack Router, TanStack Query, TanStack Table, Tailwind v4, shadcn/Radix
primitives, lucide icons, and ConnectRPC.

Do not introduce Next.js, Remix, server components, or another parallel
frontend framework. Keep new work inside the existing Vite app, generated
TanStack route tree, query modules, and shared UI primitives.

## Routing Model

Routes are file-based TanStack Router routes under `frontend/src/routes/`.
`@tanstack/router-plugin/vite` generates `frontend/src/routeTree.gen.ts`; do
not edit the generated file by hand.

The active authenticated app lives under the pathless `_authenticated` layout
segment. User-visible URLs omit `_authenticated`.

Use the resource URL convention from
[docs/ui/resource-routing.md](../ui/resource-routing.md):

| Purpose | URL pattern | Examples |
|---|---|---|
| Create a resource | `/singular/new` | `/organization/new`, `/folder/new`, `/project/new` |
| Operate on an existing resource | `/plurals/$name/...` | `/organizations/$orgName/settings`, `/projects/$projectName/secrets` |

Use full plural nouns for scoped route prefixes: `organizations`, `projects`,
and `folders`. Do not reintroduce `/orgs/...`.

## Routing Hook Conventions

Use `useLocation()` or `useRouterState({ select })` for any route reads that
happen during render — they are reactive subscriptions and will re-render the
component when the location changes.

`router.state` is a non-reactive snapshot. Reserve it exclusively for one-shot
reads inside event handlers (e.g. capturing the current path at click time for
a `returnTo` param). Never read `router.state.location.*` from a component's
render body; doing so produces stale UI that does not update on client-side
navigation (the bug fixed in HOL-968).

## `returnTo` Pattern

Creation flows pass the caller's current location in a `returnTo` search param
and resolve it after a successful create:

- Build it with `buildReturnTo({ pathname, search })` from
  `frontend/src/lib/return-to.ts`.
- Consume it with `resolveReturnTo(search.returnTo, fallbackPath)`.
- Keep it same-origin and in-app; `resolveReturnTo` owns the validation rules.

The authoritative behavior and examples live in
[docs/ui/resource-routing.md](../ui/resource-routing.md#the-returnto-search-param-convention).

## Selected-Entity Sync

`useOrg()` and `useProject()` are the canonical selected-entity stores. URL
params are authoritative when present, and layouts sync URL params into stores
in one direction only: URL -> store.

Reference layouts:

| Route layout | Store sync |
|---|---|
| `frontend/src/routes/_authenticated/organizations/$orgName.tsx` | `$orgName` -> `useOrg().setSelectedOrg` |
| `frontend/src/routes/_authenticated/projects/$projectName.tsx` | `$projectName` -> `useProject().setSelectedProject` |

Pages read active context in this order:

1. Route params (`$orgName`, `$projectName`).
2. Search params (`orgName`, `projectName`).
3. Store fallback (`useOrg()`, `useProject()`).

Creation pages may read store fallback for ergonomics, but must not write to
the selected-entity stores. The authoritative contract is
[docs/ui/selected-entity-state.md](../ui/selected-entity-state.md).

## ConnectRPC Transport

The root route wires ConnectRPC and TanStack Query providers:

- `frontend/src/routes/__root.tsx` wraps the app in `TransportProvider` and
  `QueryClientProvider`.
- `frontend/src/lib/transport.ts` creates the ConnectRPC web transport and
  auth interceptor.
- `frontend/src/lib/query-client.ts` owns shared TanStack Query defaults.

Query hooks live in `frontend/src/queries/`. The common read-hook shape is:

```ts
const { isAuthenticated } = useAuth()
const transport = useTransport()
const client = useMemo(() => createClient(Service, transport), [transport])

return useQuery({
  queryKey,
  queryFn,
  enabled: isAuthenticated && !!requiredParam,
})
```

Tests at route boundaries should mock query modules with `vi.mock('@/queries/*')`
rather than mocking ConnectRPC clients directly. See
[docs/agents/testing-patterns.md](testing-patterns.md) for the worked testing
patterns.

## ScopePicker (HOL-1018)

All "new resource" forms for org-or-project-scoped resources use `ScopePicker`
from `frontend/src/components/scope-picker/ScopePicker.tsx`. It renders a
controlled dropdown (Organization / Project) that lets the user choose which
namespace a resource is created in.

```tsx
import { ScopePicker } from '@/components/scope-picker/ScopePicker'
import type { Scope } from '@/components/scope-picker/ScopePicker'

const [scope, setScope] = useState<Scope>('organization')
<ScopePicker value={scope} onChange={setScope} disabled={!canWrite} />
```

**Convention**: every new-resource form for `TemplatePolicy`,
`TemplatePolicyBinding`, `TemplateDependency`, `TemplateRequirement`, and
`TemplateGrant` must include a `ScopePicker` so the user can choose between org
and project scope. The picker reads `useProject().selectedProject` to disable
the Project option when no project is selected; it never writes to the
selected-entity stores.

The creation pages that use `ScopePicker`:

| Route | Resource | Scope options |
|-------|----------|---------------|
| `/organizations/$orgName/template-policies/new` | TemplatePolicy | org / project |
| `/organizations/$orgName/template-bindings/new` | TemplatePolicyBinding | org / project |
| `/organizations/$orgName/template-dependencies/new` | TemplateDependency | project (org disabled per admission rules) |
| `/organizations/$orgName/template-requirements/new` | TemplateRequirement | org |
| `/organizations/$orgName/template-grants/new` | TemplateGrant | org |
| `/projects/$projectName/templates/new` | Template | project (fixed, disabled) |

## Nested Templates Sidebar Nav (HOL-1014)

The Templates sidebar entry is a collapsible group implemented in
`frontend/src/components/app-sidebar.tsx`. When a project is selected it
expands to show three sub-groups:

| Sidebar group | Pages |
|---|---|
| **Policy** | Template Policies (`templates/policies/`), Policy Bindings (`templates/policy-bindings/`) |
| **Dependencies** | Template Dependencies (`templates/dependencies/`), Requirements (`templates/requirements/`) |
| **Grants** | Template Grants (`templates/grants/`) |

Each sub-link is a `SidebarMenuSubButton` backed by a `Link` from TanStack
Router. The group auto-expands (`open={isTemplatesActive}`) when any descendant
route is active. The relevant route files are:

- `frontend/src/routes/_authenticated/projects/$projectName/templates/policies/index.tsx` (HOL-1009)
- `frontend/src/routes/_authenticated/projects/$projectName/templates/policy-bindings/index.tsx` (HOL-1009)
- `frontend/src/routes/_authenticated/projects/$projectName/templates/dependencies/index.tsx` (HOL-1013)
- `frontend/src/routes/_authenticated/projects/$projectName/templates/requirements/index.tsx` (HOL-1013)
- `frontend/src/routes/_authenticated/projects/$projectName/templates/grants/index.tsx` (HOL-1013)

## Permissions UI Gating

UI affordances (buttons, action menus, edit forms) are gated by Kubernetes
SelfSubjectAccessReview results, not by client-side role inference. The
contract per [ADR 036](../adrs/036-rbac-and-oidc-impersonation.md):

- Frontend hooks consume `useResourcePermissions(...)` (HOL-1065) to fetch
  per-resource verb permissions in a single batched SSAR fan-out via the
  ConnectRPC `PermissionsService`.
- The legacy `claims.role`-based gating that used to live in components has
  been retired. Do not infer "viewer / editor / owner" from the JWT to
  decide whether to render an action — query the `PermissionsService`
  instead. A user's access is whatever the apiserver says it is.
- The `userRole` field still returned in `Organization` / `Folder` /
  `Project` proto responses is a UI hint for grant lists (e.g. "your role is
  editor") and must not be used as an access gate. The hint is derived from
  the resource's sharing annotation; the apiserver enforces the actual
  decision.

## Tables and Data Grids

New flat resource list pages should use `ResourceGrid` when the page is a named
resource table with search/filter state, create actions, optional delete
actions, and detail navigation.

Every `ResourceGrid` row with a detail page must set `detailHref` so both the
resource ID cell and the whole row navigate to the detail page. Row action
buttons must stop event propagation before opening menus or dialogs.

The source of record for shared table behavior is
[docs/agents/data-grid-architecture.md](data-grid-architecture.md).
[docs/agents/data-grid-conventions.md](data-grid-conventions.md) remains as a
quick pointer for the clickable-row and action-propagation rules.

## Build and Test Commands

Run commands from the repo root unless noted:

| Task | Command |
|---|---|
| Install frontend dependencies | `cd frontend && npm install` |
| Start Vite dev server | `make dev` |
| Build generated frontend assets | `make generate` |
| Run UI unit tests | `make test-ui` |
| Run frontend lint directly | `cd frontend && npm run lint` |
| Run repo lint target | `make lint` |
| Run Playwright E2E | `make test-e2e` |

`make test-ui` runs `cd frontend && npm test -- --run`. E2E tests require
local certificates and, for Kubernetes CRUD specs, a real k3d cluster; prefer
unit tests unless the test strategy explicitly calls for E2E coverage.

## Related Docs

- [docs/agents/frontend-audit-2026-04.md](frontend-audit-2026-04.md) - current
  inventory and target conventions from Phase 1.
- [docs/agents/tanstack-query-conventions.md](tanstack-query-conventions.md) -
  query-key factories, stale time defaults, mutation invalidation matrix, and
  prefetch policy (HOL-946).
- [docs/ui/resource-routing.md](../ui/resource-routing.md) - authoritative URL
  and `returnTo` behavior.
- [docs/ui/selected-entity-state.md](../ui/selected-entity-state.md) -
  authoritative selected-entity store behavior.
