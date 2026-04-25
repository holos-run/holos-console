# Data Grid Architecture

Source issue: HOL-947. This document is the source of record for shared
`ResourceGrid` behavior, extension points, and decision rules.

## Grid Shell Contract

`frontend/src/components/resource-grid/ResourceGrid.tsx` exports the public
`<ResourceGrid>` component. Its props are intentionally stable:

- `title`, `kinds`, `rows`, and `onDelete` are required.
- `isLoading` renders a skeleton table placeholder.
- `error` renders a destructive card when no rows are available, or an inline
  destructive banner when stale rows can still be shown.
- `search` and `onSearchChange` bind grid-owned URL params to TanStack Router.
- `extraColumns` inserts caller-owned TanStack Table columns after Description
  and before Created At.
- `headerContent` renders banners or descriptions below the card title.
- `headerActions` renders compact icon actions next to the create control.

The shell owns TanStack Table setup, row rendering, empty/no-match states,
parent-column visibility, and the delete confirmation dialog. Search toolbar,
kind filter, and delete-confirm state are split into local modules so callers
still use one component.

## Column Definitions Per Kind

Every row passed to `ResourceGrid` must satisfy `Row` from
`frontend/src/components/resource-grid/types.ts`:

```ts
interface Row {
  kind: string
  name: string
  namespace: string
  id: string
  parentId: string
  parentLabel: string
  displayName: string
  description: string
  createdAt: string
  detailHref?: string
}
```

Default columns render in this order:

| Column | Source | Rule |
|---|---|---|
| Parent | `parentLabel || parentId` | Hidden when all rows share one parent |
| Resource ID | `id` | Monospace, muted, linked when `detailHref` is set |
| Display Name | `displayName || name` | Linked when `detailHref` is set |
| Description | `description` | Muted, truncated, title tooltip |
| Extra columns | caller-owned | Inserted before Created At |
| Created At | `createdAt` | RFC3339 string rendered via `toLocaleDateString()` |
| Actions | row object | Delete icon button and future row actions |

Kind-specific columns belong in the route that owns the kind mapping. Use
`createColumnHelper<Row>()` and close over any kind-specific row extension data
there; do not expand the base `Row` type unless every grid needs the field.

## Toolbar, Filter, Sort, Selection, And Row Actions

`Toolbar.tsx` owns global search and the optional multi-kind filter. Search is
client-side and uses TanStack Table's `includesString` global filter.
`KindFilter.tsx` renders checkboxes only when more than one `Kind` exists.
Checking all kinds or no kinds means "show all" and serializes as no `kind`
search param.

Sort state is URL-owned when introduced. Add sort keys to
`ResourceGridSearch`, parse them in `url-state.ts`, and pass them into
TanStack Table state from `ResourceGrid.tsx`. Keep client-only sort state out
of TanStack Query keys unless the backend request uses it.

Selection is not part of ResourceGrid v1. If bulk selection is added, selected
row IDs must be explicit URL or component state, and action cells must keep the
row-navigation propagation guard described below.

Row actions belong in the rightmost Actions column. Each action button must
call `e.stopPropagation()` before opening a menu, dialog, or editor so the row
click handler does not navigate.

## Pagination And Virtualization Decision Rule

ResourceGrid v1 intentionally uses client filtering over the rows returned by
the current list query. Keep client-side rendering when the view normally
returns small metadata lists and the query response already contains every row
needed for search, kind filter, and local sort.

Adopt server pagination when a view cannot reasonably fetch the full metadata
list for normal use, when authorization must be evaluated page-by-page, or when
the backend exposes a stable cursor/limit contract that all visible filters and
sorts can share.

Adopt virtualization only when the median row count for a view exceeds **500**
for more than **20%** of operators, measured against that view's TanStack Query
response sizes. The measurement source must be the concrete list hook response
used by the route, not a synthetic fixture. This phase does not introduce
virtualization.

If a view crosses the virtualization threshold before server pagination exists,
open a follow-up issue that records the measured response-size distribution,
the affected route, and the proposed rendering library or table integration.

## Dense Display Defaults

Resource grids are operational tables, not marketing surfaces. Use compact,
scan-friendly defaults:

- row height target: about 40px with single-line cells;
- resource identifiers: `font-mono text-muted-foreground text-sm`;
- display names: medium weight, single-line link when a detail route exists;
- descriptions: muted, truncated, with the full value in `title`;
- header: sticky when a grid is placed in a bounded scroll container;
- cells: stable widths or truncation for long names so hover and filter states
  do not resize the table.

Existing ResourceGrid v1 pages do not add a scroll container, so sticky header
styling is only applied when a future route introduces bounded table scrolling.

## Interaction Rules

Every row representing a named resource should set `detailHref` when a detail
page exists. With `detailHref` set:

- the Resource ID cell renders a TanStack Router `<Link>`;
- the Display Name cell renders a TanStack Router `<Link>`;
- the full row calls `navigate({ to: detailHref })`;
- the row uses `cursor-pointer`.

Links and action buttons must call `e.stopPropagation()` so cell clicks do not
also trigger row navigation. In-app navigation must use TanStack Router `Link`;
raw `<a href>` anchors in production route cells are forbidden because they
cause full-page reloads.

Rows without `detailHref` are allowed for transitional cases where the resource
cannot resolve a namespace or does not have a detail page. They render the ID
as plain text and are not row-clickable.

## URL-Backed State

Grid search params flow through `ResourceGridSearch` and
`frontend/src/components/resource-grid/url-state.ts`.

```ts
interface ResourceGridSearch {
  kind?: string
  search?: string
}
```

Routes call `parseGridSearch` from `validateSearch` and wire search updates
through TanStack Router:

```ts
const search = Route.useSearch()
const navigate = useNavigate({ from: Route.fullPath })

<ResourceGrid
  search={search}
  onSearchChange={(updater) => navigate({ search: updater, replace: true })}
  {...props}
/>
```

Client-only search, kind filter, and future client sort params stay in the URL
so refresh/back/forward behavior is stable. Server-side filters and server sort
must also be represented in the query key factory described in
`docs/agents/tanstack-query-conventions.md`.

## Query Coordination

ResourceGrid-backed list hooks should use `placeholderData: keepPreviousData`
when stale rows are safer than blanking the table across route/search changes.
Use the key factories in `frontend/src/queries/keys.ts`; do not add ad-hoc
query-key arrays in grid routes.

Mutation hooks invalidate cache only. Route components decide toast copy,
navigation, and `returnTo` behavior after awaiting the mutation.

## Adding A New Kind

1. Add a `Kind` entry in the owning route.
2. Map API results into `Row` objects with a matching `kind` value.
3. Set `detailHref` whenever the resource has a detail page.
4. Add kind-specific columns via `extraColumns`.
5. Add route-level tests that assert the grid renders, and component tests only
   when new grid behavior is introduced.

No ResourceGrid code change is required for a new project-scoped kind that fits
the base row contract.

## Test Coverage

`frontend/src/components/resource-grid/-resource-grid.test.tsx` covers the
shell behavior: loading, error states, default columns, row navigation,
filtering, empty/no-match states, create controls, and delete-confirm flow.

`frontend/src/components/resource-grid/-toolbar.test.tsx` covers the extracted
toolbar in isolation: search input wiring, multi-kind visibility, and checkbox
interaction. Route-level tests for pages that wrap ResourceGrid should mock
query hooks and assert the grid appears; they should not re-test shared grid
filter logic.
