# ADR 023: Top-Level URL Scheme for Globally Namespaced Resources

## Status

Accepted

## Context

ADR 022 establishes that organizations, folders, and projects share a global
namespace with slug-based identifiers. Each resource has a globally unique
identifier (the Kubernetes namespace name minus its type prefix). This means
any resource can be addressed without knowing its parent — `holos-fld-engineering`
is unique across all organizations, not just within one.

The existing URL structure partially reflects this: projects already live at
`/projects/$project/...` (top-level). However, folders are currently nested
under their parent organization at `/orgs/$org/folders/$folder`, which:

1. **Embeds redundant hierarchy in the URL.** Because folder identifiers are
   globally unique, the `$org` segment is unnecessary for disambiguation.
   Including it makes URLs longer and couples them to the org hierarchy —
   reparenting a folder to a different org would invalidate all existing URLs.

2. **Creates inconsistency.** Projects are top-level (`/projects/$project`) but
   folders are not (`/orgs/$org/folders/$folder`). Both resource types share the
   same global namespace (ADR 022), so they should follow the same URL pattern.

3. **Complicates deep linking.** External references (Slack messages, bookmarks,
   documentation) that include the org name break when the resource is moved.
   A stable, hierarchy-independent URL is more durable.

This ADR defines the URL scheme rules for all console resources to align URL
structure with the global namespace model.

## Decisions

### 1. Top-level resources get dedicated URL prefixes.

Resources with globally unique identifiers — organizations, folders, and
projects — each get a dedicated top-level path prefix:

- `/orgs/$org/...`
- `/folders/$folder/...`
- `/projects/$project/...`

Because their identifiers are globally unique (ADR 022), no parent context is
needed in the URL to resolve them. This makes URLs stable across reparenting
operations and shorter to read.

### 2. Sub-resources are scoped under their parent.

Resources that are owned by a single project — secrets, deployments, project
templates — are scoped under the project prefix:

- `/projects/$project/secrets/...`
- `/projects/$project/deployments/...`
- `/projects/$project/templates/...`

These resources do not have globally unique identifiers; their names are
unique only within the project. The project prefix provides the necessary
scope.

### 3. Org-scoped views remain under the org prefix.

Navigation views that are filtered by organization stay under `/orgs/$org/`:

- `/orgs/$org/folders` — list of folders in the org
- `/orgs/$org/projects` — list of projects in the org
- `/orgs/$org/settings` — org settings page
- `/orgs/$org/settings/org-templates/$tpl` — org-scoped template editor

These are not resource detail pages — they are org-contextualized navigation.
The org prefix provides the filter context the view needs.

### 4. Old URLs redirect to new.

When route structures change (e.g., folders moving from
`/orgs/$org/folders/$folder` to `/folders/$folder`), the old routes must
redirect to the new location using TanStack Router's `redirect` in
`beforeLoad`. This preserves bookmarks, shared links, and browser history
entries.

Redirect routes should be maintained for at least one release cycle to give
users time to update bookmarks. After that period, they may be removed.

## URL Pattern Table

| Resource | URL | Rationale |
|----------|-----|-----------|
| Org settings | `/orgs/$org/settings` | Org sub-page |
| Org templates | `/orgs/$org/settings/org-templates/$tpl` | Org sub-page |
| Org folder list | `/orgs/$org/folders` | Org-scoped navigation |
| Org project list | `/orgs/$org/projects` | Org-scoped navigation |
| Folder index | `/folders/$folder` | Global namespace — top-level; shows folder contents (child folders and projects) |
| Folder settings | `/folders/$folder/settings` | Folder sub-page; display name, parent, description, danger zone |
| Folder templates | `/folders/$folder/templates` | Folder sub-page |
| Project secrets | `/projects/$project/secrets` | Project sub-page |
| Project deployments | `/projects/$project/deployments` | Project sub-page |
| Project templates | `/projects/$project/templates` | Project sub-page |
| Project settings | `/projects/$project/settings` | Project sub-page |

### Incorrect patterns (do not use)

| Pattern | Why it is wrong |
|---------|-----------------|
| `/orgs/$org/folders/$folder` | Embeds redundant org context; folder ID is globally unique |
| `/orgs/$org/projects/$project` | Embeds redundant org context; project ID is globally unique |
| `/projects/$project/orgs/$org/settings` | Inverts the hierarchy; org is the parent, not the child |

## Consequences

### Positive

- **Stable deep links.** URLs do not change when a resource is reparented
  (moved to a different org or folder), because the URL does not encode
  parent hierarchy.

- **Consistent pattern.** All globally namespaced resources (orgs, folders,
  projects) follow the same `/<type>/<identifier>/...` pattern. Developers
  can predict URLs without memorizing special cases.

- **Shorter URLs.** Removing redundant parent segments makes URLs more
  compact and easier to share in chat messages and documentation.

- **Simpler routing.** TanStack Router file-based routes map directly:
  `routes/_authenticated/folders/$folder/index.tsx` handles
  `/folders/$folder`. No parameter threading through nested layouts is needed
  for resources that are independently addressable.

### Negative

- **Redirect maintenance.** Moving folders from `/orgs/$org/folders/$folder`
  to `/folders/$folder` requires redirect routes during the transition period.
  These add file count and must eventually be cleaned up.

- **Loss of navigational context in the URL.** A URL like
  `/folders/engineering` does not tell the viewer which organization owns the
  folder. The UI must display this context (breadcrumbs, sidebar) rather than
  relying on the URL.

### Risks

- **Bookmark breakage if redirects are removed too early.** Mitigated by
  keeping redirects for at least one release cycle and logging redirect hits
  to measure residual traffic before removal.

## References

- [ADR 022: Default Folder and Resource Reparenting](022-default-folder-and-reparenting.md) — global namespace, slug-based identifiers
- [ADR 020: v1alpha2 Folder Hierarchy](020-v1alpha2-folder-hierarchy.md) — folder resource definition
