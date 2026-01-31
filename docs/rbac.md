# Role-Based Access Control (RBAC)

holos-console uses a two-tier access control model combining **platform roles** and **per-secret sharing grants**.

## Platform Roles

Platform roles provide baseline access across all secrets. They are assigned by mapping OIDC groups to roles via CLI flags.

| Platform Role | Permissions | Use Case |
|---|---|---|
| Viewer | List, Read | Audit, read-only users |
| Editor | List, Read, Write | Developers who create and update secrets |
| Owner | List, Read, Write, Delete, Admin | Platform administrators |

### Configuration

```bash
holos-console \
  --platform-viewers=audit-team,readonly-users \
  --platform-editors=developers,sre-team \
  --platform-owners=platform-admins
```

| Flag | Default Group | Description |
|---|---|---|
| `--platform-viewers` | `viewer` | OIDC groups with platform viewer role |
| `--platform-editors` | `editor` | OIDC groups with platform editor role |
| `--platform-owners` | `owner` | OIDC groups with platform owner role |

When a flag is not set, the default group name is used (e.g., users in the OIDC group `viewer` automatically get the viewer platform role).

## Per-Secret Sharing Grants

Sharing grants provide fine-grained access to individual secrets. They are stored as Kubernetes annotations on the secret.

### Annotations

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

### Example

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-app-credentials
  namespace: holos-console
  labels:
    app.kubernetes.io/managed-by: console.holos.run
  annotations:
    console.holos.run/share-users: '[{"principal":"alice@example.com","role":"owner"},{"principal":"bob@example.com","role":"viewer","exp":1735689600}]'
    console.holos.run/share-groups: '[{"principal":"dev-team","role":"editor"}]'
```

## How Roles Combine

When a user has roles from multiple sources, the **highest role wins**.

Evaluation order:
1. Check per-user sharing grants (`share-users` annotation)
2. Check per-group sharing grants (`share-groups` annotation)
3. Check platform roles (OIDC group mapping)

The highest role found across all three sources determines access.

### Example

Alice is in the OIDC group `viewer` (platform viewer role). A secret has `share-users: [{"principal":"alice@example.com","role":"editor"}]`. Alice gets **editor** access to that secret because editor > viewer.

Bob is in the OIDC group `owner` (platform owner role). A secret has no sharing grants for Bob. Bob still gets **owner** access via his platform role.

## Permission Matrix

| Permission | Viewer | Editor | Owner |
|---|---|---|---|
| List secrets | Yes | Yes | Yes |
| Read secret data | Yes | Yes | Yes |
| Create secrets | - | Yes | Yes |
| Update secret data | - | Yes | Yes |
| Delete secrets | - | - | Yes |
| Update sharing grants | - | - | Yes |
