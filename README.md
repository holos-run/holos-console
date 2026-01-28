# Holos Console

## Quick Start

1. Generate TLS certificates (one-time setup):

   ```bash
   make certs
   ```

2. Start the server:

   ```bash
   make run
   ```

3. Open the console and sign in with the Dex default credentials:

   **Username:**
   ```
   admin
   ```

   **Password:**
   ```
   verysecret
   ```

## Configuration

The server reads Kubernetes secrets from the namespace specified by the `--namespace` flag. The default namespace is `holos-console`.

Secrets must have the label `app.kubernetes.io/managed-by=console.holos.run` to appear in the UI.

## Role-Based Access Control for Secrets

Access to secrets is controlled using a three-tier role-based system that maps OIDC groups to roles with specific permissions.

### Role Hierarchy

| Role | Permissions | Description |
|------|-------------|-------------|
| `viewer` | READ, LIST | Can view secret values and list secrets |
| `editor` | READ, LIST, WRITE | Can view, list, and modify secrets |
| `owner` | READ, LIST, WRITE, DELETE, ADMIN | Full administrative access |

Roles are hierarchical - higher roles can access resources allowed for lower roles.

### How It Works

1. **User Groups**: When a user logs in via OIDC, their group memberships are extracted from the `groups` claim in the ID token.

2. **Group-to-Role Mapping**: Groups are mapped to roles using case-insensitive matching (`viewer`, `editor`, `owner`).

3. **Secret Allowed Roles**: Each secret specifies which roles can access it via the `holos.run/allowed-roles` annotation containing a JSON array of role names.

4. **Access Decision**: A user can access a secret if their role level is at or above the minimum role level specified in the annotation and they have the required permission.

### Secret Configuration

To make a secret accessible through the console, configure it with:

1. **Required label** (makes the secret visible in the UI):
   ```yaml
   labels:
     app.kubernetes.io/managed-by: console.holos.run
   ```

2. **Required annotation** (controls who can access the secret):
   ```yaml
   annotations:
     holos.run/allowed-roles: '["viewer"]'
   ```

### Example Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-app-credentials
  namespace: holos-console
  labels:
    app.kubernetes.io/managed-by: console.holos.run
  annotations:
    holos.run/allowed-roles: '["editor"]'
stringData:
  API_KEY: secret-value
type: Opaque
```

In this example, users with `editor` or `owner` groups can access the secret. Users with only the `viewer` group cannot.

### UI Behavior

- **Secrets List**: The `/secrets` page shows all console-managed secrets. Secrets the user cannot access display a "No access" indicator with a tooltip showing which roles are allowed.
- **Secret Detail**: The `/secrets/:name` page displays the secret data in environment variable format (`KEY=value`) for authorized users. Unauthorized users receive a permission denied error.

### More Examples

See [docs/rbac-examples.md](docs/rbac-examples.md) for comprehensive examples including:
- Secrets that specific roles cannot access
- Read-only vs read-write access patterns
- OIDC provider configuration
