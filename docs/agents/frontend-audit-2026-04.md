# Frontend Architecture Audit - April 2026

Source issue: HOL-943. This is the short baseline for the HOL-942 frontend
architecture cleanup phases. It records the current routing, query, grid,
selected-entity, and test conventions that later phases should codify or apply.

## Current state

The frontend is a Vite, React 19, TanStack Router, TanStack Query, TanStack
Table, ConnectRPC, shadcn/Radix, and Vitest application under `frontend/`.
`make test-ui` runs `cd frontend && npm test -- --run`.

### Route inventory

The active resource routes use full plural path segments for scoped resources.
The generated TanStack file routes include the `_authenticated` layout segment,
but user-visible links omit it.

| Resource | Active route files |
|---|---|
| Secrets | `frontend/src/routes/_authenticated/projects/$projectName/secrets/index.tsx`, `new.tsx`, `$name.tsx` |
| Deployments | `frontend/src/routes/_authenticated/projects/$projectName/deployments/index.tsx`, `new.tsx`, `$deploymentName.tsx` |
| Templates | `frontend/src/routes/_authenticated/projects/$projectName/templates/index.tsx`, `new.tsx`, `$templateName.tsx`; org routes under `organizations/$orgName/templates/`; folder routes under `folders/$folderName/templates/` |
| TemplatePolicy | `frontend/src/routes/_authenticated/organizations/$orgName/template-policies/`; `frontend/src/routes/_authenticated/folders/$folderName/template-policies/` |
| TemplatePolicyBinding | `frontend/src/routes/_authenticated/organizations/$orgName/template-bindings/`; `frontend/src/routes/_authenticated/folders/$folderName/template-policy-bindings/` |

Project-scoped list pages for Secrets, Deployments, and Templates use
`ResourceGrid`. Organization- and folder-scoped TemplatePolicy and
TemplatePolicyBinding list pages still use local TanStack Table wiring.

### Query module inventory

Query hooks live in `frontend/src/queries/`:

- `deployments.ts`
- `folders.ts`
- `organizations.ts`
- `project-settings.ts`
- `projects.ts`
- `resources.ts`
- `secrets.ts`
- `templatePolicies.ts`
- `templatePolicyBindings.ts`
- `templates.ts`
- `version.ts`

The de-facto query-key factory pattern is to keep small module-local functions
near the hooks that use them, returning readonly tuple keys. Examples:
`listSecretsKey(project)`, `deploymentListKey(project)`,
`deploymentGetKey(project, name)`, `templateListKey(namespace)`,
`templatePolicyListKey(namespace)`, and `bindingListKey(namespace)`.
Mutations invalidate the same factory-produced keys when available.

The ConnectRPC query idiom is consistent across read hooks:

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

Fan-out hooks, such as `useAllTemplatesForOrg`,
`useAllTemplatePoliciesForOrg`, and `useAllTemplatePolicyBindingsForOrg`, keep
the same auth/parameter guard but compose multiple queries with
`aggregateFanOut` so partial data can render with a warning.

### Grid inventory

`frontend/src/components/resource-grid/` contains:

- `ResourceGrid.tsx` - shared TanStack Table wrapper, 470 lines after the HOL-947 split.
- `types.ts` - `Row`, `Kind`, `LineageDirection`, and `ResourceGridSearch`.
- `url-state.ts` - `parseGridSearch`, `serialiseGridSearch`,
  `parseKindIds`, and `serialiseKindIds`.
- `-resource-grid.test.tsx` - unit coverage for columns, filters, loading,
  errors, empty state, delete dialog, and link behavior.

`ResourceGrid` owns global search, multi-kind filtering, the New button or
dropdown, parent-column auto-hide, error and loading states, and delete
confirmation. Rows become clickable when callers set `Row.detailHref`; the ID
and display-name cells use TanStack Router `Link`, and row actions call
`e.stopPropagation()` before opening dialogs.

### shadcn primitive inventory

`frontend/src/components/ui/` contains these primitives and shared wrappers:

- `alert-dialog.tsx`
- `alert.tsx`
- `badge.tsx`
- `button.tsx`
- `card.tsx`
- `checkbox.tsx`
- `collapsible.tsx`
- `combobox.tsx`
- `command.tsx`
- `confirm-delete-dialog.tsx`
- `dialog.tsx`
- `dropdown-menu.tsx`
- `input.tsx`
- `label.tsx`
- `popover.tsx`
- `select.tsx`
- `separator.tsx`
- `sheet.tsx`
- `sidebar.tsx`
- `skeleton.tsx`
- `sonner.tsx`
- `switch.tsx`
- `table.tsx`
- `tabs.tsx`
- `textarea.tsx`
- `tooltip.tsx`

### Selected-entity state

`useOrg()` and `useProject()` are the canonical selected-entity stores.
URL params are authoritative when present. Layouts sync URL params into the
stores in one direction only:

- `frontend/src/routes/_authenticated/organizations/$orgName.tsx` syncs
  `$orgName` to `useOrg().setSelectedOrg`.
- `frontend/src/routes/_authenticated/projects/$projectName.tsx` syncs
  `$projectName` to `useProject().setSelectedProject` and resolves the
  owning organization through `useGetProject`.

Pages should read in this order: route params, then search params, then store
fallback. Creation pages may read store fallback but must not write selected
entity state.

`frontend/src/routes/-selected-entity-state.test.tsx` enforces the layout
contract by reading the layout source files and asserting the canonical imports
and setter calls exist. This is intentionally a static guardrail, not a render
test.

### Tests

The UI test stack is Vitest, React Testing Library, and jsdom. Route test files
inside route directories use the leading `-` filename convention so TanStack
Router ignores them during route generation. Query hooks are mocked with
`vi.mock('@/queries/*')` at route-test boundaries; `ResourceGrid` itself has a
direct component test and callers only need page-level rendering coverage.

## Gaps

- Query keys are still a convention, not a shared contract. Some modules expose
  well-named factories, while others use ad-hoc inline keys such as direct
  `['templates', 'policy-state', namespace]` invalidation from callers.
- `ResourceGrid.tsx` was split in HOL-947: loading, empty, error, toolbar,
  columns, row navigation, and delete confirmation now live in separate modules
  (`Toolbar.tsx`, `KindFilter.tsx`, `useDeleteConfirm.ts`).
- There is no documented virtualization decision rule. Current tables render
  straightforward in-memory rows, which is fine for current data sizes but
  leaves future high-cardinality resource lists without a trigger for when to
  introduce TanStack Virtual or server-side paging.
- Template-family routing is not uniform across scopes: organization bindings
  use `template-bindings`, folder bindings use `template-policy-bindings`, and
  the shared row-link helper must know both spellings.
- Unified project Templates uses direct service clients for multi-namespace
  delete and then invalidates raw query-key arrays. That is pragmatic, but it
  bypasses the local mutation hooks and increases the chance of key drift.
- The older org/folder TemplatePolicy and TemplatePolicyBinding list pages use
  local table code instead of `ResourceGrid`, so clickable-row, URL search, and
  empty/error conventions are duplicated or absent there.

## Target conventions

- Keep scoped URLs on full plural nouns: `/organizations/...`,
  `/projects/...`, and `/folders/...`. Keep creation pages on singular
  prefixes: `/organization/new`, `/project/new`, and `/folder/new`.
- Every route tree with `$orgName` or `$projectName` has a sibling layout that
  syncs URL -> store. Pages never sync store -> URL. Creation pages read store
  fallback only.
- Query modules own their key factories and use them for reads, invalidation,
  and any cross-module invalidation helpers. Prefer exported key factories when
  another module must invalidate the cache directly.
- Read hooks use `useAuth`, `useTransport`, `createClient` in `useMemo`, and an
  `enabled` guard that includes authentication and all required route/search
  params. Mutations may omit the auth guard but should invalidate only the
  affected resource keys.
- New flat resource list pages should use `ResourceGrid` when the shape fits:
  named resources, table rows, optional kind filter, optional create action,
  optional delete action, and `detailHref` detail navigation.
- Every `ResourceGrid` row that has a detail page sets `detailHref`. Action
  buttons in rows must stop propagation before doing their own work.
- If a list may exceed a few hundred visible rows or combines fan-out across
  many namespaces, the implementation plan must explicitly choose one of:
  keep client-side rows, add virtualization, or add server-side pagination.
- Keep page tests thin around `ResourceGrid` callers. Put shared grid behavior
  in `-resource-grid.test.tsx`; route tests should mock query hooks and assert
  route-specific wiring, permissions, and links.

## Out-of-scope follow-ups

- Split `ResourceGrid.tsx` into smaller internal pieces after conventions land. **Done in HOL-947**: `Toolbar.tsx`, `KindFilter.tsx`, `useDeleteConfirm.ts` extracted.
- Decide and document the virtualization or pagination threshold for resource
  lists.
- Normalize TemplatePolicyBinding path spelling across organization and folder
  scopes, or explicitly document why both spellings are permanent.
- Export query-key factories for cross-module invalidation where direct service
  calls are still needed.
- Move org/folder TemplatePolicy and TemplatePolicyBinding list pages onto
  `ResourceGrid` if their UX should match project-scoped resource lists.
- Replace client fan-out with server-side search/list RPCs when backend support
  is available.
