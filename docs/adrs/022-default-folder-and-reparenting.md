# ADR 022: Default Folder and Resource Reparenting

## Status

Accepted

## Context

ADR 020 introduces folders as optional intermediate grouping levels between an
Organization and a Project. However, two operational concerns are unaddressed:

1. **Projects without a folder are difficult to reparent later.** When an
   organization starts without folders and later adopts them, every existing
   project must be manually moved. There is no "inbox" folder that collects
   projects by default.

2. **Moving resources between parents has no permission model.** ADR 020 defines
   depth enforcement at creation time but does not specify how a folder or
   project can be moved to a different parent after creation. Reparenting changes
   RBAC inheritance chains, which has security implications that require a
   dedicated permission.

This ADR addresses both concerns by introducing a default folder created at
organization time and a fine-grained `PERMISSION_REPARENT` that governs
resource moves.

## Decisions

### 1. Organizations, folders, and projects share a global namespace with slug-based identifiers.

All three resource types are stored as Kubernetes Namespaces. Each type is
distinguished by an independently configurable prefix on the namespace name:

| Resource     | Prefix       | Identifier source     | Example (no collision)   | Example (collision)            |
|--------------|--------------|-----------------------|--------------------------|--------------------------------|
| Organization | `holos-org-` | user-chosen slug      | `holos-org-acme`         | n/a (org slugs must be unique) |
| Folder       | `holos-fld-` | slug from display name | `holos-fld-default`      | `holos-fld-default-482917`     |
| Project      | `holos-prj-` | slug from display name | `holos-prj-frontend`     | `holos-prj-frontend-731204`    |

The `holos-` portion is the namespace prefix (`--namespace-prefix`), and `org-`,
`fld-`, `prj-` are the type prefixes (`--organization-prefix`, `--folder-prefix`,
`--project-prefix`).

**Slug-based naming with collision suffix.** Folder and project identifiers are
derived from the display name by slugifying it (lowercase, hyphens for spaces,
strip non-alphanumeric). If the resulting namespace name is already taken
(globally across all organizations), the server appends a hyphen and a
**six-digit random number** (e.g., `-482917`) to produce a unique identifier.
The first caller to claim a slug gets the clean name; subsequent collisions
receive the suffixed variant. This follows the Google Cloud project naming
model where identifiers are globally unique and human-readable.

The human-readable display name is stored separately in the
`console.holos.run/display-name` annotation and is not required to be unique.

Organization identifiers remain user-chosen slugs that must be globally unique
(no auto-suffix — creation fails if the slug is taken).

RPC listing endpoints (ListOrganizations, ListFolders, ListProjects) filter
using **Kubernetes label selectors** (`console.holos.run/resource-type`,
`console.holos.run/organization`, etc.), never namespace name prefix matching.

Tests should use unique slugs (e.g., include a random suffix or test-specific
prefix) for folder and project identifiers to avoid collisions in shared test
clusters.

### 2. Identifier availability check RPC.

Each service that creates resources with slug-based identifiers exposes a
**`CheckIdentifier`** RPC (e.g., `FolderService.CheckFolderIdentifier`,
`ProjectService.CheckProjectIdentifier`). The RPC:

1. Takes a proposed `identifier` (the slug, without the type prefix).
2. Checks whether the resulting namespace name is available.
3. Returns `available = true` if the identifier is free, or `available = false`
   with a `suggested_identifier` that appends a six-digit random suffix.

The server generates the suggestion so that all callers (web UI, CLI, API
scripts) get consistent behavior. The UI calls this RPC as the user types or
after blur to show availability feedback and auto-fill the suggested
alternative — similar to the Google Cloud Console project creation flow.

The `suggested_identifier` is not reserved — another caller could claim it
between the check and the actual create. The `Create` RPC handles this race
by retrying with a new suffix if the namespace already exists (up to a
bounded number of retries).

### 3. Default folder created at organization creation.

`CreateOrganization` creates a default folder as an immediate child of the
organization. The folder's display name is `"Default"` (or the value of
`CreateOrganizationRequest.default_folder_display_name` if set). The
identifier is derived from the display name by slugifying it (per Decision 1):
for the default display name `"Default"`, the slug is `default`, producing
namespace `holos-fld-default`. If that namespace is already taken (another
org already claimed `holos-fld-default`), the server appends a six-digit
random suffix (e.g., `holos-fld-default-482917`).

The folder's identifier (the slug portion, e.g., `default` or
`default-482917`) is stored as a `console.holos.run/default-folder`
annotation on the organization namespace.

If `CreateOrganizationRequest.default_folder_display_name` is unset, the
server uses `"Default"` as the display name.

### 4. Default folder is configurable.

`UpdateOrganization` can change the default folder reference via
`UpdateOrganizationRequest.default_folder`. The annotation on the organization
namespace is updated to point to the new folder's identifier (the slug, e.g.,
`default` or `engineering-482917`). The referenced folder must exist and be an
immediate child of the organization. The server validates this constraint and
returns `codes.InvalidArgument` if the folder does not exist or is not an
immediate child.

Changing the default folder does not move existing projects. It only affects
where new projects are created when no explicit parent is specified.

### 5. Projects default to the default folder.

When `CreateProjectRequest.parent_type` is unset and `parent_name` is unset,
the handler resolves the organization's default folder and uses it as the
parent. If no default folder is set (legacy organizations created before this
ADR), the handler falls back to the organization root. This preserves backwards
compatibility for existing organizations.

The resolution order is:
1. If `parent_type` and `parent_name` are set, use them (explicit parent).
2. If unset, read the `console.holos.run/default-folder` annotation from the
   organization namespace.
3. If the annotation exists and the referenced folder exists, use it as the
   parent.
4. If the annotation is missing or the referenced folder does not exist, fall
   back to the organization as the direct parent (backwards-compatible
   behavior).

### 6. PERMISSION_REPARENT — a new fine-grained permission.

A new `PERMISSION_REPARENT = 44` is added to the `Permission` enum in
`rbac.proto`. This permission is granted only to OWNERs. It is required on
**both** the source parent and the destination parent to move a resource.

This is deliberately more restrictive than WRITE because reparenting changes
RBAC inheritance chains. An EDITOR who can modify folder metadata should not
be able to move a subtree into a scope where they gain elevated permissions.

The cascade table grants `PERMISSION_REPARENT` to OWNERs at every scope level
(organization, folder). It is never granted to VIEWERs or EDITORs.

### 7. Reparent via Update RPCs.

`UpdateFolderRequest` and `UpdateProjectRequest` gain optional parent fields
(`parent_type` and `parent_name`). When these fields are set, the handler
validates the move:

1. **Permission check**: The caller must hold `PERMISSION_REPARENT` on both the
   current parent and the destination parent.
2. **Existence check**: The destination parent must exist and be in the same
   organization as the resource being moved.
3. **Type check**: A project can be moved to an organization or a folder. A
   folder can be moved to an organization or another folder (but not to a
   project).
4. **Depth check**: Moving a folder subtree must not exceed the 3-level depth
   limit (ADR 020 Decision 5). The handler computes the maximum depth of the
   subtree being moved and validates against the new parent's depth.
5. **Cycle check**: Moving a folder must not create a cycle in the hierarchy.
   The handler walks the destination parent's ancestor chain to verify the folder
   being moved is not an ancestor of the destination.

If all checks pass, the handler updates the
`console.holos.run/parent` label on the resource's Kubernetes Namespace to
point to the new parent namespace. For folders, all descendant namespaces
retain their existing parent labels — only the moved folder's label changes.

When the optional parent fields are unset, `UpdateFolder` and `UpdateProject`
behave exactly as before (update metadata only, no reparenting).

### 8. Depth enforcement on reparent.

Moving a folder subtree must not exceed the 3-level depth limit (ADR 020
Decision 5). The handler computes the maximum depth of the subtree being moved
by walking all descendants. It then adds this depth to the new parent's depth
and validates that the total does not exceed 3.

Example: A folder at depth 1 with a child folder at depth 2 (max subtree depth
= 1) can be moved under a parent at depth 1 (resulting max depth = 1 + 1 = 2,
within limits). The same folder cannot be moved under a parent at depth 3
(resulting max depth = 3 + 1 = 4, exceeds limit).

## Consequences

### Positive

- **Smoother adoption path.** Organizations that start simple get a default
  folder from day one. When they later adopt a folder hierarchy, existing
  projects are already in a folder and can be reorganized without special-case
  migration logic.

- **Explicit security boundary for moves.** `PERMISSION_REPARENT` ensures that
  only OWNERs can change RBAC inheritance chains, preventing privilege
  escalation via reparenting.

- **Backwards compatible.** Legacy organizations without a default folder
  continue to work — projects created without an explicit parent fall back to
  the organization root.

- **Human-readable namespace names.** Slug-based identifiers (e.g.,
  `holos-fld-engineering`) are meaningful when inspecting Kubernetes resources
  directly, unlike opaque numeric IDs. The collision suffix is only added when
  needed, keeping the common case clean.

- **Consistent identifier generation.** Server-side `CheckIdentifier` RPCs
  normalize slug generation and collision handling across all callers (web UI,
  CLI, API scripts), preventing client-side divergence.

- **Standard Update RPC pattern.** Reparenting reuses the existing Update RPCs
  with optional fields rather than introducing separate Move RPCs, keeping the
  API surface minimal.

### Negative

- **Additional CreateOrganization complexity.** Organization creation now
  involves creating both the organization namespace and the default folder
  namespace. If folder creation fails, the organization creation must be rolled
  back or the organization left in a partially-created state.

- **Owner-only reparenting.** EDITORs cannot move resources even within their
  own scope. This is by design but may require escalation workflows in large
  organizations.

### Risks

- **Annotation integrity.** The `console.holos.run/default-folder` annotation
  on the organization namespace can reference a folder that has been deleted.
  The resolution logic (Decision 5, step 4) handles this gracefully by falling
  back to the organization root, but the stale annotation should be cleaned up
  when a folder is deleted.

- **Concurrent reparent operations.** Two concurrent reparent operations could
  in theory create a cycle or exceed depth limits if they race. Mitigated by
  Kubernetes optimistic concurrency (resource version checks on label updates).

- **Slug collision race.** `CheckIdentifier` returns availability at a point in
  time; another caller could claim the slug before `Create` executes. Mitigated
  by retry logic in the `Create` RPC — if the namespace already exists, the
  server regenerates with a new random suffix (bounded retries).

## References

- [ADR 007: Organization Grants Do Not Cascade](007-org-grants-no-cascade.md)
- [ADR 016: Configuration Management Resource Schema](016-config-management-resource-schema.md) — Decision 4 defers folders
- [ADR 020: v1alpha2 Folder Hierarchy](020-v1alpha2-folder-hierarchy.md) — folder storage, depth limits, walk algorithm
- [ADR 021: Unified Template Service](021-unified-template-service.md) — template permissions model
