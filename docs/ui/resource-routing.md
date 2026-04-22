# Resource URL Convention — Singular Create, Plural Scoped

> **Agent quick-read** (~3 min). Covers the URL naming rule that prevents
> namespace collisions between creation pages and resource-scoped pages.
> Source of record: HOL-867 (plan) / HOL-873 (this doc).

## The Rule

| Purpose | URL pattern | Examples |
|---------|-------------|---------|
| Create a new resource (no identifier yet) | `/singular/new` | `/organization/new`, `/folder/new`, `/project/new` |
| Operate on an existing, identified resource | `/plurals/$name/…` | `/organizations/$name/settings`, `/folders/$name/edit`, `/projects/$name/settings` |

**Singular** (`/folder/new`) for creation — the resource does not exist yet so
there is no identifier.

**Plural + identifier** (`/folders/$name/settings`) for scoped operations —
there is a known resource name in the path.

## Why This Matters

A collision occurs when the same path segment is both a literal keyword and a
dynamic identifier. For example, if creation were placed at `/folders/new`, the
router would have to decide whether `new` is:

1. The keyword that triggers the creation form, or
2. The name of an existing folder literally called `new`.

Both interpretations are structurally valid — `new` is a legal Kubernetes
resource name — so neither the router nor future developers could distinguish
them from the URL alone.

Keeping creation pages under a **singular prefix** (`/folder/new`) eliminates
the ambiguity completely:

- `/folder/new` — always the creation form (no identifier exists).
- `/folders/new/settings` — the settings page for the folder named `new`.

The same principle applies to every top-level resource kind: `organization`,
`folder`, `project`.

## The `returnTo` Search-Param Convention

Creation routes need to redirect the user back to the page they came from after
a successful create. The convention uses a single `returnTo` search param.

### Building the param (caller side)

```ts
import { buildReturnTo } from '@/lib/return-to'

// Inside a component that has router access:
const router = useRouter()
const { pathname, searchStr } = router.state.location
const returnTo = buildReturnTo({ pathname, search: searchStr })

<Link to="/folder/new" search={{ orgName, returnTo }}>New Folder</Link>
```

`buildReturnTo` concatenates `pathname + search` into a single string. No
additional encoding is applied at this layer; TanStack Router handles
URL-encoding when it serialises the `search` object.

### Consuming the param (creation route side)

```ts
import { resolveReturnTo } from '@/lib/return-to'

// In the onSuccess / navigate call after the resource is created:
const target = resolveReturnTo(search.returnTo, '/resource-manager')
navigate({ to: target })
```

`resolveReturnTo` validates the `returnTo` value against a strict
same-origin allowlist before using it. Any value that fails validation
falls back to the supplied default path.

### Security contract (summary)

Only same-origin, in-app paths are accepted. A valid `returnTo` value:

- Starts with `/` but **not** `//` (blocks protocol-relative URLs).
- Contains no colon (`:`) before the first path separator (blocks `javascript:`).
- Contains no backslash (blocks Windows path-traversal tricks).
- Round-trips through `decodeURIComponent` without throwing.

See `frontend/src/lib/return-to.ts` for the full implementation and JSDoc.

## Worked Example — Resource Manager's New Dropdown

The Resource Manager page (`/resource-manager`) shows a single **New ▾**
dropdown that navigates to all three creation routes. Each `Link` encodes the
current URL (including the `?expanded=…` tree state) as `returnTo` so the user
lands back on the same Resource Manager view after creating a resource.

```tsx
// frontend/src/routes/_authenticated/resource-manager/index.tsx

function NewDropdown({ orgName }: { orgName: string }) {
  const router = useRouter()
  const { pathname, searchStr } = router.state.location
  const returnTo = buildReturnTo({ pathname, search: searchStr })

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="sm">
          <Plus className="mr-1 h-4 w-4" />
          New
          <ChevronDown className="ml-1 h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {/* Singular prefix → /organization/new */}
        <DropdownMenuItem asChild>
          <Link to="/organization/new" search={{ returnTo }}>
            Organization
          </Link>
        </DropdownMenuItem>

        {/* Singular prefix → /folder/new */}
        <DropdownMenuItem asChild>
          <Link to="/folder/new" search={orgName ? { orgName, returnTo } : { returnTo }}>
            Folder
          </Link>
        </DropdownMenuItem>

        {/* Singular prefix → /project/new */}
        <DropdownMenuItem asChild>
          <Link to="/project/new" search={orgName ? { orgName, returnTo } : { returnTo }}>
            Project
          </Link>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
```

After the resource is created, each creation page calls:

```ts
const target = resolveReturnTo(search.returnTo, '/resource-manager')
navigate({ to: target })
```

…which returns the user to `/resource-manager?expanded=…` with the tree state
intact.

## File Map

| File | Role |
|------|------|
| `frontend/src/routes/_authenticated/organization/new.tsx` | Create organisation form |
| `frontend/src/routes/_authenticated/folder/new.tsx` | Create folder form |
| `frontend/src/routes/_authenticated/project/new.tsx` | Create project form |
| `frontend/src/routes/_authenticated/organizations/index.tsx` | List organisations (plural) |
| `frontend/src/routes/_authenticated/orgs/$orgName/settings/index.tsx` | Org settings (plural + identifier) |
| `frontend/src/routes/_authenticated/folders/$folderName/settings/index.tsx` | Folder settings (plural + identifier) |
| `frontend/src/routes/_authenticated/projects/$projectName/settings/index.tsx` | Project settings (plural + identifier) |
| `frontend/src/lib/return-to.ts` | `buildReturnTo` / `resolveReturnTo` / `isValidReturnTo` |
