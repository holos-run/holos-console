# Role-Based Access Control (RBAC)

holos-console uses a three-tier access control model combining **organization-level grants**, **project-level grants**, and **per-secret sharing grants**.

## Organizations

An **organization** is a Kubernetes Namespace with the name `{namespace-prefix}{organization-prefix}{name}` (defaults: empty namespace prefix, `org-` organization prefix) and the label `console.holos.run/resource-type=organization`. Permission grants are stored as annotations on the Namespace resource.

Organization grants cascade to all projects associated with the organization. Users see only organizations where they have at least viewer-level access.

### Creating Organizations

Organization creation is controlled by CLI flags rather than grant-based authorization:

- `--disable-org-creation`: When set, unconditionally blocks organization creation regardless of other flags
- `--org-creator-users`: Comma-separated email addresses allowed to create organizations
- `--org-creator-groups`: Comma-separated OIDC group names allowed to create organizations

The creator is automatically added as owner on the new organization.

## Projects

A **project** is a Kubernetes Namespace with the name `{namespace-prefix}{project-prefix}{name}` (defaults: empty namespace prefix, `prj-` project prefix) and the label `console.holos.run/resource-type=project`. Projects are global resources — the `console.holos.run/organization` label is optional and represents an IAM association, not a containment relationship. The project name is stored in the `console.holos.run/project` label. Permission grants are stored as annotations on the Namespace resource.

Project grants cascade to all secrets within the project. Users see only projects where they have at least viewer-level access (directly or via an associated organization).

## Namespace Prefix Scheme

User-facing names are translated to Kubernetes namespace names using a three-part naming scheme: `{namespace-prefix}{type-prefix}{name}`. The optional `--namespace-prefix` flag enables multiple console instances (e.g., ci, qa, prod) to coexist in the same Kubernetes cluster by prepending a global prefix to all namespace names.

| Resource | Pattern | CLI Flags | Default | Example (`--namespace-prefix=prod-`) |
|---|---|---|---|---|
| Organization | `{namespace-prefix}{org-prefix}{name}` | `--namespace-prefix`, `--organization-prefix` | `""`, `org-` | `acme` → `prod-org-acme` |
| Project | `{namespace-prefix}{prj-prefix}{name}` | `--namespace-prefix`, `--project-prefix` | `""`, `prj-` | `api` → `prod-prj-api` |

When `--namespace-prefix` is empty (the default), the naming scheme is unchanged from the two-part `{type-prefix}{name}` form (e.g., `org-acme`, `prj-api`).

Namespaces are distinguished by labels:
- `console.holos.run/resource-type`: `organization` or `project`
- `console.holos.run/organization`: the organization name (optional, on project namespaces)
- `console.holos.run/project`: the project name (on project namespaces)

## Access Evaluation

Grants on a resource authorize operations on **that resource level only**. Parent grants use scope-aware cascade — they do not implicitly grant full access to child resources.

### Secret access

Access to a secret is evaluated in this order:

1. **Per-secret grants** — Full secret permissions (read, write, delete, admin)
2. **Project grants (cascade)** — Limited: list metadata only (viewer), create/update (editor), delete/admin (owner). **Reading secret data always requires a direct per-secret grant.**
3. **Organization grants** — Never cascade to secrets

If no grant matches at any tier, access is denied.

### Project access

Access to a project is evaluated in this order:

1. **Project grants** — Full project permissions
2. **Organization grants** — Never cascade to projects. Org grants only authorize viewing the org resource itself.

### Role-per-scope cascade tables

Cascade behavior is defined by explicit permission tables per scope (`CascadeTable` in `console/rbac/rbac.go`). Each table maps a parent role to the set of child permissions it grants. This makes cascade policy readable at a glance without tracing through indirect permission mappings.

#### `ProjectCascadeSecretPerms` — project role → secret permissions

| Project Role | `SECRETS_LIST` | `SECRETS_READ` | `SECRETS_WRITE` | `SECRETS_DELETE` | `SECRETS_ADMIN` |
|---|---|---|---|---|---|
| Viewer | yes | **no** | no | no | no |
| Editor | yes | **no** | yes | no | no |
| Owner | yes | **no** | yes | yes | yes |

`SECRETS_READ` is never cascaded — reading secret data always requires a direct per-secret grant.

#### `OrgCascadeSecretPerms` — empty (org grants never cascade to secrets)

#### `OrgCascadeProjectPerms` — empty (org grants never cascade to projects)

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

### Direct grant permissions

When a role is granted directly on a resource, it authorizes these operations:

| Role | Secrets Permissions | Project Permissions | Organization Permissions |
|---|---|---|---|
| Viewer | List, Read | List, Read | List, Read |
| Editor | List, Read, Write | List, Read, Write | List, Read, Write |
| Owner | List, Read, Write, Delete, Admin | List, Read, Write, Delete, Admin, Create | List, Read, Write, Delete, Admin |

### Cascade permissions (parent → child)

Parent grants do **not** implicitly grant full access to child resources:

| Parent Grant | Child: List metadata | Child: Read data | Child: Write | Child: Delete/Admin |
|---|---|---|---|---|
| Project → Secret | Viewer | Never | Editor | Owner |
| Org → Project | Never | Never | Never | Never |
| Org → Secret | Never | Never | Never | Never |

`PERMISSION_PROJECTS_CREATE` requires owner on **at least one existing project** or owner on the target organization (checked via a separate authorization path, not cascade).

Organization creation is controlled by CLI flags (`--disable-org-creation`, `--org-creator-users`, `--org-creator-groups`), not by grant-based authorization.

## Example: Organization with Project and Secrets

```yaml
# Organization namespace
apiVersion: v1
kind: Namespace
metadata:
  name: org-my-org
  labels:
    app.kubernetes.io/managed-by: console.holos.run
    console.holos.run/resource-type: organization
  annotations:
    console.holos.run/display-name: "My Organization"
    console.holos.run/share-users: '[{"principal":"alice@example.com","role":"owner"}]'
    console.holos.run/share-groups: '[{"principal":"dev-team","role":"editor"}]'
---
# Project namespace (optionally associated with the organization)
apiVersion: v1
kind: Namespace
metadata:
  name: prj-my-project
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
  namespace: prj-my-project
  labels:
    app.kubernetes.io/managed-by: console.holos.run
  annotations:
    console.holos.run/share-users: '[{"principal":"carol@example.com","role":"viewer"}]'
```

In this example:
- Alice has **owner** on `my-org` — this grants access to the org resource itself only; it does not cascade to projects or secrets
- Members of `dev-team` have **editor** on `my-org` — same scope restriction as above
- Bob has **viewer** on `my-project` — can view the project and list secret metadata, but **cannot read secret data** (requires a direct per-secret grant)
- Carol has **viewer** on `my-app-credentials` — can read the secret data via the direct per-secret grant

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
