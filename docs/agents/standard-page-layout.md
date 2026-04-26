# Standard Page Layout

`StandardPageLayout` is the shared page shell for top-level resource list
routes that are backed by `ResourceGrid` v1. Import it from
`@/components/page-layout`.

Use it when a route needs the standard resource-list structure:

- Optional breadcrumbs above the grid.
- A title rendered by `ResourceGrid`, either as a plain string or as scoped
  title parts joined with ` / `.
- Optional header actions next to the grid's New button.
- Optional contextual content outside the grid card.
- URL search state wired through `ResourceGrid`'s `search` and
  `onSearchChange` props.

Keep detail pages, form pages, settings pages, and custom non-grid workflows
outside this layout unless they adopt `ResourceGrid` as their primary surface.

Canonical examples:

- `frontend/src/routes/_authenticated/projects/$projectName/secrets/index.tsx`
- `frontend/src/routes/_authenticated/projects/$projectName/deployments/index.tsx`
- `frontend/src/routes/_authenticated/projects/$projectName/templates/index.tsx`
- `frontend/src/routes/_authenticated/organizations/$orgName/projects/index.tsx`

## Props

| Prop | Type | Required | Purpose |
|---|---|---:|---|
| `title` | `string` | No | Plain grid title. Use this when the page title is not scope-derived. |
| `titleParts` | `string[]` | No | Scope-aware title parts joined with ` / `, such as `[projectName, 'Secrets']`. Mutually exclusive with `title`. |
| `breadcrumbs` | `BreadcrumbItem[]` | No | Breadcrumb links rendered above the grid. Omit `href` for the current page crumb. |
| `headerActions` | `ReactNode` | No | Header action slot passed to `ResourceGrid`, typically icon buttons or a custom create button. |
| `children` | `ReactNode` | No | Contextual content rendered below the grid, such as a help pane or banner. |
| `grid` | `ResourceGridConfig<S>` | Yes | Typed `ResourceGrid` prop bag. It excludes `title`, `headerActions`, `search`, and `onSearchChange` from the base grid props, then adds generic `search` and `onSearchChange` fields for route search state. |

`StandardPageLayout` is generic over `S extends ResourceGridSearch`. Use the
generic form when a route extends grid search state with its own params, such
as the Templates `help` query parameter.

## Minimal Usage

```tsx
import { useCallback } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { StandardPageLayout } from '@/components/page-layout'
import type { Row, ResourceGridSearch } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'

export const Route = createFileRoute('/_authenticated/projects/$projectName/secrets/')({
  validateSearch: parseGridSearch,
  component: SecretsListPage,
})

function SecretsListPage() {
  const { projectName } = Route.useParams()
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })

  const rows: Row[] = []
  const kinds = [{ id: 'Secret', label: 'Secret', newHref: `/projects/${projectName}/secrets/new` }]

  const handleSearchChange = useCallback(
    (updater: (prev: ResourceGridSearch) => ResourceGridSearch) => {
      navigate({ search: (prev) => updater(prev as ResourceGridSearch) })
    },
    [navigate],
  )

  return (
    <StandardPageLayout
      titleParts={[projectName, 'Secrets']}
      grid={{
        kinds,
        rows,
        isLoading: false,
        search,
        onSearchChange: handleSearchChange,
      }}
    />
  )
}
```

For pages with route-specific search params, preserve those params in the route
handler and instantiate the layout with the extended search type:

```tsx
<StandardPageLayout<TemplatesSearch>
  titleParts={[projectName, 'Templates']}
  headerActions={helpButton}
  grid={{ kinds, rows, search, onSearchChange: handleSearchChange }}
>
  <TemplatesHelpPane open={helpOpen} onOpenChange={handleHelpOpenChange} />
</StandardPageLayout>
```
