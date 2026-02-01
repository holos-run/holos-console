# Role-Based Access Control (RBAC)

holos-console uses a three-tier access control model combining **organization-level grants**, **project-level grants**, and **per-secret sharing grants**.

## Organizations

An **organization** is a Kubernetes Namespace with the prefix `{namespace-prefix}org-` and the label `console.holos.run/resource-type=organization`. Permission grants are stored as annotations on the Namespace resource.

Organization grants cascade to all projects within the organization. Users see only organizations where they have at least viewer-level access.

### Creating Organizations

Organization creation is controlled by CLI flags rather than grant-based authorization:

- `--org-creator-users`: Comma-separated email addresses allowed to create organizations
- `--org-creator-groups`: Comma-separated OIDC group names allowed to create organizations

The creator is automatically added as owner on the new organization.

## Projects

A **project** is a Kubernetes Namespace with the label `console.holos.run/resource-type=project`. Each project belongs to an organization, identified by the `console.holos.run/organization` label. The project name is stored in the `console.holos.run/project` label. Permission grants are stored as annotations on the Namespace resource.

Project grants cascade to all secrets within the project. Users see only projects where they have at least viewer-level access (directly or via the parent organization).

## Namespace Prefix Scheme

User-facing names are translated to Kubernetes namespace names using a configurable prefix (default: `holos-`):

| Resource | Pattern | CLI Flag | Example |
|---|---|---|---|
| Organization | `{prefix}org-{name}` | `--namespace-prefix` | `my-org` → `holos-org-my-org` |
| Project | `{prefix}{org}-{project}` | `--namespace-prefix` | org=`acme`, project=`web` → `holos-acme-web` |

Namespaces are distinguished by labels:
- `console.holos.run/resource-type`: `organization` or `project`
- `console.holos.run/organization`: the organization name (on project namespaces)
- `console.holos.run/project`: the project name (on project namespaces)

Project namespaces are looked up by label rather than by name derivation.

## Access Evaluation

Access to a secret is evaluated in this order (highest role wins):

1. Per-secret grants (`share-users`/`share-groups` annotations on the Secret)
2. Project grants (`share-users`/`share-groups` annotations on the project Namespace)
3. Organization grants (`share-users`/`share-groups` annotations on the organization Namespace)

If no grant matches at any tier, access is denied.

Access to a project is evaluated similarly:

1. Project grants (annotations on the project Namespace)
2. Organization grants (annotations on the organization Namespace)

## Grant Annotations

Grants are stored as JSON annotations on Namespace and Secret resources:

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

| Role | Secrets Permissions | Project Permissions | Organization Permissions |
|---|---|---|---|
| Viewer | List, Read | List, Read | List, Read |
| Editor | List, Read, Write | List, Read, Write | List, Read, Write |
| Owner | List, Read, Write, Delete, Admin | List, Read, Write, Delete, Admin, Create | List, Read, Write, Delete, Admin |

`PERMISSION_PROJECTS_CREATE` requires owner on **at least one existing project** or owner on the target organization.

Organization creation is controlled by CLI flags (`--org-creator-users`, `--org-creator-groups`), not by grant-based authorization.

## Example: Organization with Project and Secrets

```yaml
# Organization namespace
apiVersion: v1
kind: Namespace
metadata:
  name: holos-org-my-org
  labels:
    app.kubernetes.io/managed-by: console.holos.run
    console.holos.run/resource-type: organization
  annotations:
    console.holos.run/display-name: "My Organization"
    console.holos.run/share-users: '[{"principal":"alice@example.com","role":"owner"}]'
    console.holos.run/share-groups: '[{"principal":"dev-team","role":"editor"}]'
---
# Project namespace within the organization
apiVersion: v1
kind: Namespace
metadata:
  name: holos-my-org-my-project
  labels:
    app.kubernetes.io/managed-by: console.holos.run
    console.holos.run/resource-type: project
    console.holos.run/organization: my-org
    console.holos.run/project: my-project
  annotations:
    console.holos.run/display-name: "My Project"
    console.holos.run/description: "Production secrets"
    console.holos.run/share-users: '[{"principal":"bob@example.com","role":"viewer","exp":1735689600}]'
---
# Secret within the project
apiVersion: v1
kind: Secret
metadata:
  name: my-app-credentials
  namespace: holos-my-org-my-project
  labels:
    app.kubernetes.io/managed-by: console.holos.run
  annotations:
    console.holos.run/share-users: '[{"principal":"carol@example.com","role":"viewer"}]'
```

In this example:
- Alice has **owner** access to all projects and secrets in `my-org` via the organization grant
- Members of `dev-team` have **editor** access to all projects and secrets via the organization group grant
- Bob has **viewer** access to `my-project` and its secrets via the project grant (expires at the given timestamp)
- Carol has **viewer** access to `my-app-credentials` only, via the per-secret grant

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
| Create new projects | - | - | Yes (on any project or org) |

### Organization Permissions

| Permission | Viewer | Editor | Owner |
|---|---|---|---|
| List organizations | Yes | Yes | Yes |
| Read organization metadata | Yes | Yes | Yes |
| Update organization metadata | - | Yes | Yes |
| Delete organization | - | - | Yes |
| Update organization sharing | - | - | Yes |
| Create new organizations | - | - | Via CLI flags only |
