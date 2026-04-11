# RBAC

Four-tier access control model evaluated in order (highest role wins):

1. **Organization-level**: Per-org grants stored as JSON annotations on K8s Namespace objects (prefix configurable via `--organization-prefix`, default `org-`)
2. **Folder-level**: Per-folder grants stored as JSON annotations on K8s Namespace objects (prefix configurable via `--folder-prefix`, default `fld-`)
3. **Project-level**: Per-project grants stored as JSON annotations on K8s Namespace objects (prefix configurable via `--project-prefix`, default `prj-`)
4. **Secret-level**: Per-secret grants stored as JSON annotations on K8s Secret objects

## Annotations

Grant annotations: `console.holos.run/share-users`, `console.holos.run/share-roles`

Metadata annotations on org/project namespaces: `console.holos.run/display-name`, `console.holos.run/creator-email` (email of the user who created the resource, written at creation time from the OIDC email claim)

## Namespace Prefix Scheme

Three-part naming: `{namespace-prefix}{type-prefix}{name}`

- Organizations: `{namespace-prefix}{organization-prefix}{name}` (resource-type label: `organization`)
- Folders: `{namespace-prefix}{folder-prefix}{name}` (resource-type label: `folder`, organization label for scoping, parent label for hierarchy)
- Projects: `{namespace-prefix}{project-prefix}{name}` (resource-type label: `project`, optional organization label for IAM inheritance, project label stores project name)

The `--namespace-prefix` flag (default `"holos-"`) prefixes all console-managed namespace names, enabling multi-instance isolation in the same cluster (e.g., `prod-org-acme`, `ci-prj-api`).

## Organization Creation

Organization creation is controlled by `--disable-org-creation`, `--org-creator-users`, and `--org-creator-roles` CLI flags. By default all authenticated principals can create organizations (implicit grant). Setting `--disable-org-creation` disables this implicit grant; explicit `--org-creator-users` and `--org-creator-roles` lists are still honored.

Creating an organization also auto-creates a default folder (slug-based identifier, e.g., `holos-fld-default`) as an immediate child. The default folder's identifier is stored in the `console.holos.run/default-folder` annotation on the organization namespace. See [Resource Naming Guardrail](guardrail-resource-naming.md) for the slug-based naming model.

## Roles Claim

The `--roles-claim` flag (default `"groups"`) configures which OIDC token claim is used to extract role memberships. This allows integration with identity providers that use non-standard claim names (e.g., `realm_roles`).

## Role Levels

Roles: VIEWER (1), EDITOR (2), OWNER (3) defined in `proto/holos/console/v1/rbac.proto`

`PERMISSION_TEMPLATES_WRITE` is required to create, update, or delete templates at any scope. It is granted to EDITORs and OWNERs via the unified `TemplateCascadePerms` cascade table in `console/rbac/rbac.go`.

`PERMISSION_REPARENT` is required to move a folder or project to a different parent. It is granted only to OWNERs via the `ReparentCascadePerms` cascade table. The caller must hold this permission on both the source parent and the destination parent. This is deliberately more restrictive than WRITE because reparenting changes RBAC inheritance chains (ADR 022 Decision 6).

## Related

- [Authentication](authentication.md) — OIDC flow that produces the identity claims RBAC evaluates
- [Resource Naming Guardrail](guardrail-resource-naming.md) — Slug-based namespace identifiers
- [Template Service](template-service.md) — Template permissions use the cascade table pattern
