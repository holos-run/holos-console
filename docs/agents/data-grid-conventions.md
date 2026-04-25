# Data Grid Conventions

> **Agent quick-read** (~2 min). Covers the clickable-row and clickable-identifier
> convention for all `ResourceGrid` tables.
> Source of record: HOL-940.

## The Rule

Every `ResourceGrid` data row that represents a named resource **must** be
fully clickable:

1. **Resource ID cell** — the `id` column renders as a `<Link>` to the resource
   detail page when `Row.detailHref` is set.
2. **Entire row** — the `<TableRow>` has an `onClick` handler that navigates to
   `Row.detailHref` when it is set, and a `cursor-pointer` visual hint.

## How to apply

Set `detailHref` on every `Row` you pass to `ResourceGrid`. The component
handles both the cell link and the row click automatically — no extra wiring
is required in the calling page.

```ts
// Example — secrets/index.tsx
const rows: Row[] = secretRows.map(({ secret, scope }) => ({
  // ...other fields...
  detailHref: `/projects/${projectName}/secrets/${secret.name}`,
}))
```

## Propagation guard

The delete-icon button calls `e.stopPropagation()` before opening the confirm
dialog so that clicking the trash icon does **not** trigger the row navigation.
Any future action button added to the actions column must also call
`e.stopPropagation()`.

## Scope

This convention applies to all pages backed by `ResourceGrid`:

| Page | `detailHref` pattern |
|------|----------------------|
| Secrets | `/projects/$projectName/secrets/$secretName` |
| Deployments | `/projects/$projectName/deployments/$deploymentName` |
| Templates | `/projects/$projectName/templates/$templateName` |

When adding a new resource kind to `ResourceGrid`, include a `detailHref` on
each row unless the resource has no dedicated detail page.
