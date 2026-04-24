# Selected-Entity State Contract — URL ↔ useOrg / useProject Store

> **Agent quick-read** (~4 min). Covers the canonical stores for the currently
> selected organisation and project, how URL params sync into those stores, and
> the read-precedence rules every page and creation form must follow.
> Source of record: HOL-927 (plan) / HOL-931 (this doc).
>
> Prerequisite: read [Resource URL Convention](resource-routing.md) first.
> This doc assumes familiarity with the singular-create / plural-scoped URL rule.

---

## Canonical Stores

| Store hook | File | localStorage key |
|---|---|---|
| `useOrg()` | `frontend/src/lib/org-context.tsx` | `holos-selected-org` |
| `useProject()` | `frontend/src/lib/project-context.tsx` | `holos-selected-project` |

`OrgProvider` and `ProjectProvider` wrap the entire authenticated subtree.
Every component under `/_authenticated` can call `useOrg()` or `useProject()`
without additional setup. The stores are the **single source of truth** for
which org/project the user has active; the `WorkspaceMenu` in
`frontend/src/components/workspace-menu.tsx` (rendered inside `app-sidebar.tsx`) is
the user-visible control that reads from those stores.

---

## Sync Direction: URL → Store (one-way only)

**URL params are authoritative when present. The store never updates the URL.**

Layouts sync URL → store via a `useEffect`. No page ever calls `setSelectedOrg`
or `setSelectedProject` and then navigates to encode that value in the URL.
Inverting this (store → URL) would cause feedback loops and phantom history
entries.

### Required Layout Pattern

Any route segment that owns a `$orgName` path param must include a layout
component that syncs the param into the store:

```tsx
// frontend/src/routes/_authenticated/orgs/$orgName.tsx
import { createFileRoute, Outlet } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useOrg } from '@/lib/org-context'

export const Route = createFileRoute('/_authenticated/orgs/$orgName')({
  component: RouteComponent,
})

function RouteComponent() {
  const { orgName } = Route.useParams()
  return <OrgLayout orgName={orgName} />
}

export function OrgLayout({ orgName }: { orgName: string }) {
  const { setSelectedOrg } = useOrg()

  useEffect(() => {
    setSelectedOrg(orgName)
  }, [orgName, setSelectedOrg])

  return <Outlet />
}
```

Key invariants of this pattern:

1. The `useEffect` fires whenever `orgName` changes — e.g. the user navigates
   from one org to another via a direct URL.
2. The layout calls only `setSelectedOrg`; it never calls `navigate()` or
   mutates the URL.
3. Child routes rendered by `<Outlet />` can call `useOrg()` and receive the
   freshly synced value.

Apply the same pattern for any future `$projectName` layout segment.

---

## Read Precedence for Pages

When a page needs to know the active org or project it must consult sources in
this order:

| Priority | Source | When to use |
|---|---|---|
| 1 | `useParams()` — `$orgName` / `$projectName` | The page owns that segment in its route path |
| 2 | `useSearch()` — `orgName` / `projectName` search param | The param was passed explicitly by a caller (e.g. a creation page receiving its parent context) |
| 3 | `useOrg()` / `useProject()` store | Fallback for ergonomics (deep links, page refreshes, stale bookmarks) |

Never skip level 1 or 2 in favour of the store when a more-specific value is
available — the store may lag a URL change by one render cycle.

---

## Creation-Page Contract

Creation pages (`/organization/new`, `/folder/new`, `/project/new`) follow an
additional set of rules because they do not own a resource identifier yet:

1. **Always receive the parent entity via `search` param** — callers pass
   `orgName` (and optionally `folderName`) as search params; the creation page
   reads them with `useSearch()`.

2. **Fall back to the store for ergonomics** — if the search param is absent
   (e.g. the user landed via a bare bookmark), the page may read from
   `useOrg()` / `useProject()` as a convenience fallback:

   ```tsx
   const search = Route.useSearch()
   const { selectedOrg } = useOrg()
   // URL search param wins; store is a read-only safety net for refreshes.
   const orgName = search.orgName ?? selectedOrg ?? undefined
   ```

3. **Never mutate the store** — creation pages are read-only consumers of the
   store. They must never call `setSelectedOrg` or `setSelectedProject`.
   Mutating the store from a creation page would silently change the sidebar's
   active org/project for every other open tab.

4. **Post-create navigation** — use `resolveReturnTo(search.returnTo, fallback)`
   to return the user to the originating page. See
   [Resource URL Convention § returnTo](resource-routing.md#the-returnto-search-param-convention).

---

## localStorage Persistence

Both stores initialise from localStorage on mount:

```ts
const [selectedOrg, setSelectedOrgState] = useState<string | null>(() => {
  return localStorage.getItem('holos-selected-org')
})
```

`setSelectedOrg` / `setSelectedProject` keep localStorage in sync. This means
the selected entity survives a full page refresh, so users don't lose context
when they reload a page.

When the selected org changes, `ProjectProvider` clears `selectedProject` (and
its localStorage entry) — unless the change was triggered by a project-URL
navigation, in which case a `suppressClearRef` guard prevents the race
condition that would otherwise clear the just-set project.

---

## User-Visible Truth: WorkspaceMenu

`WorkspaceMenu` (`frontend/src/components/workspace-menu.tsx`, rendered by
`app-sidebar.tsx`) is the user-visible
control for switching org and project. It navigates to `/organizations` (Switch
Organization) or `/organizations/$orgName/projects` (Switch Projects) — the store
is updated indirectly when those pages' layouts sync the URL param into the store.
`WorkspaceMenu` reads from the stores (`selectedOrg`, `selectedProject`) to
display the active context label; it does not call `setSelectedOrg` or
`setSelectedProject` directly. All components are read-only consumers of the store
except layouts, which write via the one-way URL → store sync.

---

## Invariants Summary

| Rule | Rationale |
|---|---|
| URL → store (never store → URL) | Prevents history-entry storms and tab-crosstalk |
| Layouts own the sync, pages are read-only | Single place to update when the URL shape changes |
| Creation pages read store but never write it | A silent write from a creation page would change the sidebar context globally |
| localStorage mirrors the store | Survives page refreshes without a round-trip |

---

## File Map

| File | Role |
|---|---|
| `frontend/src/lib/org-context.tsx` | `useOrg()` hook and `OrgProvider` |
| `frontend/src/lib/project-context.tsx` | `useProject()` hook and `ProjectProvider` |
| `frontend/src/components/workspace-menu.tsx` | `WorkspaceMenu` — user-visible org/project switcher; read-only store consumer |
| `frontend/src/components/app-sidebar.tsx` | Sidebar shell; renders `WorkspaceMenu` and flat nav |
| `frontend/src/routes/_authenticated/orgs/$orgName.tsx` | Reference layout — syncs `$orgName` URL param → `useOrg()` store |
| `frontend/src/routes/_authenticated/project/new.tsx` | Reference creation page — reads store, never writes it |
| `docs/ui/resource-routing.md` | Singular-create / plural-scoped URL rule; `returnTo` contract |

## CI Enforcement

`frontend/src/routes/-selected-entity-state.test.tsx` is the machine-enforced
guardrail for this contract.  It reads each layout file listed in its explicit
allowlist as a source string and asserts:

1. The file exists.
2. It imports `useOrg` / `useProject` from the canonical context module.
3. It calls `setSelectedOrg(` / `setSelectedProject(`.

`make test-ui` runs this check on every CI push.  Adding a new
`/organizations/$orgName/...` or `/projects/$projectName/...` URL tree without
a compliant layout file will fail CI with an actionable error naming the missing
file and the required imports.
