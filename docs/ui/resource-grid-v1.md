# ResourceGrid v1 — Design Note

> **Agent quick-read** (~4 min). Covers everything you need to add a new
> resource kind to an existing grid, or to wire a new page onto `ResourceGrid`.

## Location

```
frontend/src/components/resource-grid/
  ResourceGrid.tsx   — the component
  types.ts           — Row, Kind, LineageDirection, ResourceGridSearch
  url-state.ts       — parseGridSearch / serialiseGridSearch / parseKindIds / serialiseKindIds
```

## What ResourceGrid v1 does

`ResourceGrid` is a TanStack Table wrapper that provides:

- A **global search** box (`?search=`) using `includesString` filtering.
- A **multi-kind filter** (`?kind=a,b`) rendered as checkboxes when more than
  one `Kind` is passed. Checking all or none = show everything.
- A **lineage filter** (`?lineage=ancestors|descendants|both` + `?recursive=1`)
  — the grid reads these from URL state and exposes them via `search` props so
  the parent route's data-fetching layer can act on them. The grid itself passes
  rows through unchanged (data-source-agnostic).
- A **"New" button** — a single Link when one creatable kind exists, a dropdown
  when multiple exist.
- **Row-level delete** via `ConfirmDeleteDialog`; callers supply an `onDelete`
  async callback.
- **Parent column auto-hide** — the `parentId` / `parentLabel` column is hidden
  automatically when all rows share the same parent.

## Columns (default order)

| Column | Source field | Notes |
|---|---|---|
| Parent | `row.parentId` / `row.parentLabel` | Hidden when all rows share one parent |
| Resource ID | `row.id` | Monospace, muted |
| Display Name | `row.displayName` \|\| `row.name` | Links to `row.detailHref` if present |
| Description | `row.description` | Truncated, max-width |
| _(extraColumns)_ | caller-supplied | Inserted after Description |
| Created At | `row.createdAt` | ISO-8601 string, rendered via `toLocaleDateString()` |
| Actions | — | Delete icon button; triggers ConfirmDeleteDialog |

## The `Row` interface

```ts
interface Row {
  kind: string        // must match a Kind.id
  name: string        // Kubernetes resource name
  namespace: string   // Kubernetes namespace
  id: string          // stable identifier (UID or compound key)
  parentId: string    // parent resource name/namespace
  parentLabel: string // human label for parent (e.g. project display name)
  displayName: string // user-facing label (falls back to name)
  description: string
  createdAt: string   // ISO-8601
  detailHref?: string // makes displayName a link
}
```

## The `Kind` interface

```ts
interface Kind {
  id: string          // stable, used in ?kind= URL param
  label: string       // shown in checkboxes and dropdown
  newHref?: string    // "New" button destination
  canCreate?: boolean // controls whether "New" button appears
}
```

## URL state contract

All search params flow through `ResourceGridSearch`:

```ts
interface ResourceGridSearch {
  kind?: string          // comma-separated Kind.id list; absent = show all
  search?: string        // global filter string
  lineage?: 'ancestors' | 'descendants' | 'both'
  recursive?: '0' | '1' // '0' / absent = non-recursive (default)
}
```

Consumer routes call `parseGridSearch` from `url-state.ts` inside
`validateSearch`:

```ts
import { parseGridSearch } from '@/components/resource-grid/url-state'

export const Route = createFileRoute('/projects/$projectName/secrets')({
  validateSearch: parseGridSearch,
  component: SecretsPage,
})
```

Inside the page component, wire the grid to the router:

```ts
const search = Route.useSearch()
const navigate = useNavigate({ from: Route.fullPath })

<ResourceGrid
  search={search}
  onSearchChange={(updater) =>
    navigate({ search: updater, replace: true })
  }
  ...
/>
```

## Extension points

### `extraColumns`

Append TanStack Table `ColumnDef<Row>` entries after the Description column:

```ts
import { createColumnHelper } from '@tanstack/react-table'
import type { Row } from '@/components/resource-grid/types'

const col = createColumnHelper<Row>()

const phaseColumn = col.display({
  id: 'phase',
  header: 'Phase',
  cell: ({ row }) => <PhaseBadge phase={row.original.extra?.phase} />,
})

<ResourceGrid extraColumns={[phaseColumn]} ... />
```

The `Row.extra` field does not exist on the base interface — attach it via
module augmentation or pass it out-of-band through closure if needed.
Deployments uses a local `DeploymentRow` type that extends `Row` for this
purpose; see `frontend/src/routes/_authenticated/projects/$projectName/deployments/index.tsx`.

### `onDelete`

Async callback receiving the full `Row`. Throw to surface a toast error.

```ts
const { mutateAsync } = useDeleteSecret()

const handleDelete = async (row: Row) => {
  await mutateAsync({ name: row.name, namespace: row.namespace })
  // queryClient.invalidateQueries(...) if needed
}

<ResourceGrid onDelete={handleDelete} ... />
```

### `headerContent`

A `React.ReactNode` rendered inside the Card header below the title, above the
toolbar. Used by the Deployments page for its description banner.

### `headerActions`

A `React.ReactNode` rendered in the Card header to the left of the "New"
button. Used by the Templates page for the help-pane toggle (? icon button).

## When to use ResourceGrid v1 vs. the Resource Manager tree

| Situation | Use |
|---|---|
| Flat list of resources under one project (Secrets, Deployments, Templates) | ResourceGrid v1 |
| Cross-scope hierarchical view of all resources (org → folder → project) | Resource Manager tree |
| A new resource kind that belongs to a single project | ResourceGrid v1 — add a `Kind` entry and map data to `Row` |
| A new resource kind that spans the org hierarchy | Resource Manager tree — add a node type to `TreeNode` |

## Adding a new kind to an existing grid

Example: adding a hypothetical `TemplateRelease` kind to the Templates grid.

1. Define a new `Kind` entry in the page component:

   ```ts
   const kinds: Kind[] = [
     existingKind,
     { id: 'template-release', label: 'Template Release', newHref: '/...', canCreate: isOwner },
   ]
   ```

2. Map API response items to `Row` objects with `kind: 'template-release'`.

3. Merge the two data arrays and pass as `rows` to `ResourceGrid`.

4. Add a unit test asserting the new kind appears in the kind-filter checkboxes
   and that its rows are visible when the filter is set to `template-release`.

No changes to `ResourceGrid` itself are required.

## Unit-test reference

`frontend/src/components/resource-grid/-resource-grid.test.tsx` — covers:

- Column headers rendered
- Kind-filter checkboxes (multi-kind scenario)
- Lineage filter controls
- Global search filtering
- Empty state when `rows=[]`
- Loading skeleton (`data-testid="resource-grid-loading"`)
- Error card when `error` is set and rows is empty
- Partial-error inline banner when `error` is set and rows exist
- Delete-confirm dialog: open on trash icon click, cancel, confirm, error state

See `docs/agents/testing-patterns.md` §"ResourceGrid v1 unit-test pattern" for
the recommended mock strategy at the page level.
