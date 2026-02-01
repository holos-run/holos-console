# Role-Based Access Control (RBAC)

holos-console uses a two-tier access control model combining **project-level grants** and **per-secret sharing grants**.

## Projects

A **project** is a Kubernetes Namespace labeled `app.kubernetes.io/managed-by=console.holos.run`. Permission grants are stored as annotations on the Namespace resource.

Users see only projects where they have at least viewer-level access.

## Access Evaluation

Access to a secret is evaluated in this order (highest role wins):

1. Per-secret grants (`share-users`/`share-groups` annotations on the Secret)
2. Project grants (`share-users`/`share-groups` annotations on the Namespace)

If no grant matches, access is denied.

## Grant Annotations

Grants are stored as JSON annotations on both Namespace and Secret resources:

| Annotation | Format | Description |
|---|---|---|
| `console.holos.run/share-users` | `[{"principal":"email","role":"role","nbf":ts,"exp":ts}]` | Per-user grants |
| `console.holos.run/share-groups` | `[{"principal":"group","role":"role","nbf":ts,"exp":ts}]` | Per-group grants |

Each grant is a JSON object with:

| Field | Type | Required | Description |
|---|---|---|---|
| `principal` | string | yes | Email address (users) or OIDC group name (groups) |
| `role` | string | yes | One of `viewer`, `editor`, `owner` |
| `nbf` | int64 | no | Unix timestamp before which the grant is inactive |
| `exp` | int64 | no | Unix timestamp at or after which the grant is inactive |

When `nbf` or `exp` is omitted, the grant has no time restriction for that bound.

## Roles

| Role | Secrets Permissions | Project Permissions |
|---|---|---|
| Viewer | List, Read | List, Read |
| Editor | List, Read, Write | List, Read, Write |
| Owner | List, Read, Write, Delete, Admin | List, Read, Write, Delete, Admin, Create |

`PERMISSION_PROJECTS_CREATE` requires owner on **at least one existing project** (not on the project being created).

## Example: Project with Secrets

```yaml
# Project namespace with grants
apiVersion: v1
kind: Namespace
metadata:
  name: my-project
  labels:
    app.kubernetes.io/managed-by: console.holos.run
  annotations:
    console.holos.run/display-name: "My Project"
    console.holos.run/description: "Production secrets"
    console.holos.run/share-users: '[{"principal":"alice@example.com","role":"owner"}]'
    console.holos.run/share-groups: '[{"principal":"dev-team","role":"editor"}]'
---
# Secret within the project
apiVersion: v1
kind: Secret
metadata:
  name: my-app-credentials
  namespace: my-project
  labels:
    app.kubernetes.io/managed-by: console.holos.run
  annotations:
    console.holos.run/share-users: '[{"principal":"bob@example.com","role":"viewer","exp":1735689600}]'
```

In this example:
- Alice has **owner** access to all secrets in `my-project` via the project grant
- Members of `dev-team` have **editor** access to all secrets via the project group grant
- Bob has **viewer** access to `my-app-credentials` only, via the per-secret grant (expires at the given timestamp)

## Bootstrap

The first project must be created via `kubectl` since no user has `PERMISSION_PROJECTS_CREATE` until they are an owner on at least one project:

```bash
# Label the namespace as managed by console
kubectl label namespace my-project app.kubernetes.io/managed-by=console.holos.run

# Grant the bootstrap user owner access
kubectl annotate namespace my-project \
  'console.holos.run/share-users=[{"principal":"admin@example.com","role":"owner"}]'
```

After bootstrap, the owner can create additional projects and manage sharing through the UI.

## Permission Matrix

### Secret Permissions

| Permission | Viewer | Editor | Owner |
|---|---|---|---|
| List secrets | Yes | Yes | Yes |
| Read secret data | Yes | Yes | Yes |
| Create secrets | - | Yes | Yes |
| Update secret data | - | Yes | Yes |
| Delete secrets | - | - | Yes |
| Update sharing grants | - | - | Yes |

### Project Permissions

| Permission | Viewer | Editor | Owner |
|---|---|---|---|
| List projects | Yes | Yes | Yes |
| Read project metadata | Yes | Yes | Yes |
| Update project metadata | - | Yes | Yes |
| Delete project | - | - | Yes |
| Update project sharing | - | - | Yes |
| Create new projects | - | - | Yes (on any project) |
