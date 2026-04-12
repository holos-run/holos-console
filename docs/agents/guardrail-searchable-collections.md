# Guardrail: Searchable Collections

**All index/listing pages and all combo boxes displaying dynamic collections must include a search or filter input.** Users should never scroll through an unfiltered list to find an item.

## Standard Pattern: TanStack Table Global Filter

Index pages use TanStack Table with client-side global filtering:

```tsx
const [globalFilter, setGlobalFilter] = useState('')

const table = useReactTable({
  data: items,
  columns,
  state: { globalFilter },
  onGlobalFilterChange: setGlobalFilter,
  globalFilterFn: 'includesString',
  getCoreRowModel: getCoreRowModel(),
  getFilteredRowModel: getFilteredRowModel(),
})
```

The search input renders above the table:

```tsx
<Input
  placeholder="Search items…"
  value={globalFilter}
  onChange={(e) => setGlobalFilter(e.target.value)}
  className="max-w-sm"
/>
```

`globalFilterFn: 'includesString'` is the standard filter function. It performs case-insensitive substring matching across all columns.

## Standard Pattern: Combobox

Combo boxes for dynamic collections use the `Combobox` component (`frontend/src/components/ui/combobox.tsx`), which has a built-in `CommandInput` search field. No additional wiring is needed -- the search behavior is provided by the `cmdk` command palette underneath.

## Triggers

Apply this rule when:
- Creating a new index/listing page under `frontend/src/routes/`
- Adding a combo box or dropdown that displays a dynamic collection
- Reviewing a page that lists resources fetched from an API

## Incorrect Patterns

| Pattern | Why it is wrong |
|---------|-----------------|
| Index page with no search input | Users cannot find items without scrolling the entire list |
| `Select` for a dynamic collection | `Select` has no search; use `Combobox` instead (see [Selection Components](selection-components.md)) |
| Custom filter logic instead of `globalFilterFn` | Inconsistent behavior; use the TanStack Table built-in |

## Existing Examples

| Page | File | Pattern used |
|------|------|-------------|
| Projects index | `frontend/src/routes/_authenticated/orgs/$orgName/projects/index.tsx` | `globalFilterFn: 'includesString'` |
| Folders index | `frontend/src/routes/_authenticated/orgs/$orgName/folders/index.tsx` | `globalFilterFn: 'includesString'` |

## Related

- [Collection Index Pages](guardrail-collection-index.md) -- every collection needs an index page
- [Selection Components](selection-components.md) -- Combobox vs Select decision rule
- [UI Architecture](ui-architecture.md) -- TanStack Table and component library
