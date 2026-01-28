# RBAC Examples

This document explains the role-based access control (RBAC) system for secrets in Holos Console and provides example configurations.

## Overview

Holos Console uses a three-tier role-based access control system to protect secrets. Access decisions are based on:

1. **User's groups** - Extracted from the OIDC ID token `groups` claim
2. **Secret's allowed roles** - Defined in the `holos.run/allowed-roles` annotation
3. **Required permission** - What operation the user is trying to perform

## Role Hierarchy

| Role | Level | Permissions | Description |
|------|-------|-------------|-------------|
| `viewer` | 1 | READ, LIST | Can view secret values and list secrets |
| `editor` | 2 | READ, LIST, WRITE | Can view, list, and modify secrets |
| `owner` | 3 | READ, LIST, WRITE, DELETE, ADMIN | Full administrative access |

**Important:** Roles are hierarchical. A user with the `owner` role can access any secret that allows `viewer` or `editor` because owner (level 3) >= viewer (level 1).

## How Groups Map to Roles

The console maps OIDC group names to roles using case-insensitive matching:

| Group Claim | Maps To |
|-------------|---------|
| `viewer`, `Viewer`, `VIEWER` | ROLE_VIEWER |
| `editor`, `Editor`, `EDITOR` | ROLE_EDITOR |
| `owner`, `Owner`, `OWNER` | ROLE_OWNER |
| Any other value | (ignored - no role) |

Configure your OIDC provider (Dex, Keycloak, Azure AD, etc.) to include one of these group names in the `groups` claim for users who should access secrets.

### Development Default

In development mode with the embedded Dex provider, all authenticated users automatically receive the `owner` group, granting full access.

## Secret Annotations

Secrets are protected using Kubernetes annotations:

```yaml
metadata:
  annotations:
    holos.run/allowed-roles: '["viewer", "editor"]'
```

**Requirements:**
- The annotation key must be `holos.run/allowed-roles`
- The value must be a valid JSON array of strings
- Role names are case-insensitive

**Legacy Support:**
The deprecated `holos.run/allowed-groups` annotation is still supported for backward compatibility but should not be used for new secrets.

## Access Decision Logic

Access is **granted** when:
1. The user has at least one group that maps to a valid role
2. The user's highest role level >= the minimum role level from allowed-roles
3. The user's role has the required permission (e.g., READ, WRITE)

Access is **denied** when:
- The user has no groups that map to valid roles
- The user's role level is below the minimum required
- The allowed-roles annotation is empty or contains no valid roles

## Example Manifests

The `manifests/` directory contains example secrets demonstrating different access patterns.

### Example 1: Secret the Owner Can Read

**File:** `manifests/secret-example.yaml`

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: example
  namespace: holos-console
  labels:
    app.kubernetes.io/managed-by: console.holos.run
  annotations:
    holos.run/allowed-roles: '["owner"]'
stringData:
  API_KEY: abc123
  API_URL: https://example.com
type: Opaque
```

**Access:** Users with the `owner` group can read this secret. Users with `viewer` or `editor` groups cannot access it because their role level (1 or 2) is below the required level (3).

### Example 2: Secret the Owner Cannot Read

**File:** `manifests/secret-restricted.yaml`

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: restricted-secret
  namespace: holos-console
  labels:
    app.kubernetes.io/managed-by: console.holos.run
  annotations:
    holos.run/allowed-roles: '["security-auditors"]'
stringData:
  INTERNAL_KEY: restricted-value
  AUDIT_TOKEN: audit-only-access
type: Opaque
```

**Access:** Nobody can read this secret because `security-auditors` does not map to any known role. The secret appears in the list but shows "No access" with the allowed roles in the tooltip.

**Use Case:** Placeholder for secrets managed by a separate system or reserved for future role expansion.

### Example 3: Secret with Read-Only Access

**File:** `manifests/secret-readonly.yaml`

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: readonly-config
  namespace: holos-console
  labels:
    app.kubernetes.io/managed-by: console.holos.run
  annotations:
    holos.run/allowed-roles: '["viewer"]'
stringData:
  CONFIG_URL: https://config.example.com
  CONFIG_VERSION: v1.2.3
type: Opaque
```

**Access:** All users with `viewer`, `editor`, or `owner` groups can read this secret. However, only users with `editor` or `owner` roles have WRITE permission - the `viewer` role in allowed-roles only sets the minimum access level for reading.

**Note:** Currently, write operations are not exposed in the UI, but when they are, this annotation pattern will enforce read-only access for viewers.

### Example 4: Secret with Read-Write Access

**File:** `manifests/secret-readwrite.yaml`

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: editable-credentials
  namespace: holos-console
  labels:
    app.kubernetes.io/managed-by: console.holos.run
  annotations:
    holos.run/allowed-roles: '["editor"]'
stringData:
  DATABASE_URL: postgres://localhost:5432/mydb
  DATABASE_PASSWORD: changeme
type: Opaque
```

**Access:** Users with `editor` or `owner` groups can read and write this secret. Users with only the `viewer` group cannot access it.

**Use Case:** Credentials that application developers need to update but should not be accessible to all users.

### Example 5: Broadly Accessible Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: public-config
  namespace: holos-console
  labels:
    app.kubernetes.io/managed-by: console.holos.run
  annotations:
    holos.run/allowed-roles: '["viewer", "editor", "owner"]'
stringData:
  PUBLIC_API_URL: https://api.example.com
type: Opaque
```

**Access:** All authenticated users with any valid role can read this secret. This is equivalent to `["viewer"]` since higher roles inherit access.

## Troubleshooting

### "Permission denied" error

1. Check the user's groups in their ID token (Profile page shows groups)
2. Verify the groups claim contains a valid role name (`viewer`, `editor`, or `owner`)
3. Check the secret's `holos.run/allowed-roles` annotation
4. Ensure the annotation value is valid JSON

### Secret not appearing in list

Ensure the secret has the required label:
```yaml
labels:
  app.kubernetes.io/managed-by: console.holos.run
```

### Invalid annotation error

The `holos.run/allowed-roles` annotation must be valid JSON:

**Correct:**
```yaml
holos.run/allowed-roles: '["viewer", "editor"]'
```

**Incorrect:**
```yaml
holos.run/allowed-roles: viewer, editor     # Not JSON
holos.run/allowed-roles: "[viewer, editor]" # Not valid JSON (missing quotes)
holos.run/allowed-roles: '{"role": "viewer"}' # Object, not array
```

## OIDC Provider Configuration

### Dex (Development)

The embedded Dex provider automatically assigns the `owner` group. For custom groups, configure Dex with a static connector:

```yaml
connectors:
  - type: mockCallback
    id: mock
    name: Mock
    config:
      groups:
        - viewer    # or editor, owner
```

### Production OIDC Providers

Configure your provider to include the appropriate group in the `groups` claim:

- **Keycloak:** Add group mapper to client scope
- **Azure AD:** Configure group claims in app registration
- **Okta:** Add groups claim to authorization server

Refer to your provider's documentation for specific configuration steps.
