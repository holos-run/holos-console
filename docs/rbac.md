# Role-Based Access Control (RBAC)

holos-console uses a four-tier access control model combining **organization-level grants**, **folder-level grants**, **project-level grants**, and **per-secret sharing grants**.

## Organizations

An **organization** is a Kubernetes Namespace with the name `{namespace-prefix}{organization-prefix}{name}` (defaults: empty namespace prefix, `org-` organization prefix) and the label `console.holos.run/resource-type=organization`. Permission grants are stored as annotations on the Namespace resource.

Organization grants authorize only organization-level operations (viewing the org, managing IAM bindings). They do **not** cascade to projects or secrets (see [ADR 007](adrs/007-org-grants-no-cascade.md)). Users see only organizations where they have at least viewer-level access.

### Creating Organizations

Organization creation is controlled by CLI flags rather than grant-based authorization:

- `--disable-org-creation`: Disables the implicit grant that allows all authenticated principals to create organizations. Explicit creator lists are still honored when this flag is set.
- `--org-creator-users`: Comma-separated email addresses allowed to create organizations
- `--org-creator-roles`: Comma-separated OIDC role names allowed to create organizations

The creator is automatically added as owner on the new organization.

## Folders

A **folder** is a Kubernetes Namespace with the name `{namespace-prefix}{folder-prefix}{name}` (defaults: empty namespace prefix, `fld-` folder prefix) and the label `console.holos.run/resource-type=folder`. Folders are optional intermediate grouping levels between an Organization and a Project (ADR 020). The `console.holos.run/organization` label scopes the folder to an organization. The `console.holos.run/parent` label stores the parent namespace name (organization or another folder). Maximum folder depth is 3 levels between an organization and a project (ADR 020 Decision 5).

A **default folder** is auto-created when an organization is created. Its identifier is derived from the display name (default: "Default") using slug-based naming (ADR 022 Decision 3). The organization stores the default folder's identifier in the `console.holos.run/default-folder` annotation. New projects without an explicit parent are placed in the default folder.

## Projects

A **project** is a Kubernetes Namespace with the name `{namespace-prefix}{project-prefix}{name}` (defaults: empty namespace prefix, `prj-` project prefix) and the label `console.holos.run/resource-type=project`. Projects are global resources — the `console.holos.run/organization` label is optional and represents an IAM association, not a containment relationship. The project name is stored in the `console.holos.run/project` label. The `console.holos.run/parent` label stores the parent namespace name (organization or folder). Permission grants are stored as annotations on the Namespace resource.

Project grants cascade to all secrets within the project. Users see only projects where they have at least viewer-level access (directly or via an associated organization).

## Namespace Prefix Scheme

User-facing names are translated to Kubernetes namespace names using a three-part naming scheme: `{namespace-prefix}{type-prefix}{name}`. The optional `--namespace-prefix` flag enables multiple console instances (e.g., ci, qa, prod) to coexist in the same Kubernetes cluster by prepending a global prefix to all namespace names.

| Resource | Pattern | CLI Flags | Default | Example (`--namespace-prefix=prod-`) |
|---|---|---|---|---|
| Organization | `{namespace-prefix}{org-prefix}{name}` | `--namespace-prefix`, `--organization-prefix` | `""`, `org-` | `acme` → `prod-org-acme` |
| Folder | `{namespace-prefix}{fld-prefix}{name}` | `--namespace-prefix`, `--folder-prefix` | `""`, `fld-` | `default` → `prod-fld-default` |
| Project | `{namespace-prefix}{prj-prefix}{name}` | `--namespace-prefix`, `--project-prefix` | `""`, `prj-` | `api` → `prod-prj-api` |

When `--namespace-prefix` is empty (the default), the naming scheme is unchanged from the two-part `{type-prefix}{name}` form (e.g., `org-acme`, `prj-api`).

Namespaces are distinguished by labels:
- `console.holos.run/resource-type`: `organization`, `folder`, or `project`
- `console.holos.run/organization`: the organization name (on folder and project namespaces)
- `console.holos.run/parent`: the parent namespace name (on folder and project namespaces)
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

Organization grants have no cascade tables — they never cascade to projects or secrets ([ADR 007](adrs/007-org-grants-no-cascade.md)).

## Metadata Annotations

Metadata annotations are stored on organization, folder, and project Namespace resources:

| Annotation | Resource | Description |
|---|---|---|
| `console.holos.run/display-name` | Organization, Folder, Project | Human-readable display name |
| `console.holos.run/creator-email` | Organization, Folder, Project | Email address of the user who created the resource |
| `console.holos.run/default-folder` | Organization | Identifier of the default folder for new projects |

The `creator-email` annotation is written at creation time from the authenticated user's OIDC email claim. It is read-only after creation and surfaced in the settings UI.

## Grant Annotations

Grants are stored as JSON annotations on Namespace and Secret resources:

| Annotation | Format | Description |
|---|---|---|
| `console.holos.run/share-users` | `[{"principal":"email","role":"role","nbf":ts,"exp":ts}]` | Per-user grants |
| `console.holos.run/share-roles` | `[{"principal":"role","role":"role","nbf":ts,"exp":ts}]` | Per-role grants |
| `console.holos.run/default-share-users` | `[{"principal":"email","role":"role","nbf":ts,"exp":ts}]` | Default per-user grants applied to new folders and projects within the organization (propagated via ancestor walk). Settable on organization or folder namespaces. |
| `console.holos.run/default-share-roles` | `[{"principal":"role","role":"role","nbf":ts,"exp":ts}]` | Default per-role grants applied to new folders and projects within the organization (propagated via ancestor walk). Settable on organization or folder namespaces. |

Each grant is a JSON object with:

| Field | Type | Required | Description |
|---|---|---|---|
| `principal` | string | yes | Email address (users) or OIDC role name (roles) |
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

`PERMISSION_REPARENT` is required to move a folder or project to a different parent. The caller must hold this permission on **both** the source parent and the destination parent. It is granted only to OWNERs via the `ReparentCascadePerms` cascade table (ADR 022 Decision 6).

Organization creation is controlled by CLI flags (`--disable-org-creation`, `--org-creator-users`, `--org-creator-roles`), not by grant-based authorization.

## Organization Default Sharing

Organizations can define **default sharing grants** that are automatically applied to new folders and projects created within the organization. These defaults are stored as annotations on the organization namespace (`console.holos.run/default-share-users` and `console.holos.run/default-share-roles`) and are merged into descendant namespaces at creation time via the ancestor-default-share cascade. When `CreateOrganization` is called with `populate_defaults: true`, the backend seeds the three standard role grants (Owner, Editor, Viewer) into `console.holos.run/default-share-roles` *before* the default folder or default project is created, so the seeded descendants inherit them. Changing the defaults does not retroactively update existing folders or projects.

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
    console.holos.run/creator-email: "alice@example.com"
    console.holos.run/share-users: '[{"principal":"alice@example.com","role":"owner"}]'
    console.holos.run/share-roles: '[{"principal":"dev-team","role":"editor"}]'
    console.holos.run/default-folder: "default"
---
# Default folder namespace (auto-created with the organization)
apiVersion: v1
kind: Namespace
metadata:
  name: fld-default
  labels:
    app.kubernetes.io/managed-by: console.holos.run
    console.holos.run/resource-type: folder
    console.holos.run/organization: my-org
    console.holos.run/parent: org-my-org
  annotations:
    console.holos.run/display-name: "Default"
    console.holos.run/creator-email: "alice@example.com"
    console.holos.run/share-users: '[{"principal":"alice@example.com","role":"owner"}]'
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
    console.holos.run/creator-email: "bob@example.com"
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

### Folder Permissions

| Permission | Viewer | Editor | Owner |
|---|---|---|---|
| List folders | Yes | Yes | Yes |
| Read folder metadata | Yes | Yes | Yes |
| Update folder metadata | - | Yes | Yes |
| Delete folder | - | - | Yes |
| Update folder sharing | - | - | Yes |
| Reparent folder | - | - | Yes (on both source and destination parents) |

### Project Permissions

| Permission | Viewer | Editor | Owner |
|---|---|---|---|
| List projects | Yes | Yes | Yes |
| Read project metadata | Yes | Yes | Yes |
| Update project metadata | - | Yes | Yes |
| Delete project | - | - | Yes |
| Update project sharing | - | - | Yes |
| Create new projects | - | - | Yes (on any project or org) |
| Reparent project | - | - | Yes (on both source and destination parents) |

### Organization Permissions

| Permission | Viewer | Editor | Owner |
|---|---|---|---|
| List organizations | Yes | Yes | Yes |
| Read organization metadata | Yes | Yes | Yes |
| Update organization metadata | - | Yes | Yes |
| Delete organization | - | - | Yes |
| Update organization sharing | - | - | Yes |
| Create new organizations | - | - | Via CLI flags only |
