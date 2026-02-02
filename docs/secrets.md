# Secrets Management

holos-console provides a web UI for managing Kubernetes Secrets. Secrets are stored as standard Kubernetes `Opaque` secrets using the native `map<string, bytes>` data model, where each key is a filename and each value is the file content as raw bytes.

## Data Model

Each secret contains one or more key-value entries:

| Field | Description |
|---|---|
| **Key** | The data key (filename). Must be unique within a secret. Used as the filename when mounted as a volume. |
| **Value** | The data value (file content). Stored as raw bytes. The UI assumes UTF-8 encoding for display and editing. |

This maps directly to the Kubernetes Secret `data` field:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-app-credentials
  labels:
    app.kubernetes.io/managed-by: console.holos.run
  annotations:
    console.holos.run/description: "Production database and API credentials"
    console.holos.run/url: "https://myapp.example.com"
type: Opaque
data:
  database-url: cG9zdGdyZXM6Ly9sb2NhbGhvc3QvbXlkYg==    # base64-encoded
  api-key: c2VjcmV0LWtleS12YWx1ZQ==                       # base64-encoded
```

The UI handles base64 encoding/decoding transparently -- you work with plaintext values.

## UI Workflow

### Secrets List

The `/projects/:projectName/secrets` page displays all secrets in the project's namespace (resolved via label lookup) that have the label `app.kubernetes.io/managed-by=console.holos.run`. Each secret shows:

- The secret name (links to the detail page)
- A description of the secret's purpose (when set), or a sharing summary (e.g., "2 users, 1 role") as secondary text
- A sharing summary chip alongside the description (when both are present)
- An accessibility indicator -- secrets you cannot access show a "No access" chip with a lock icon

### Creating a Secret

1. Click **Create Secret** on the secrets list page.
2. Enter a **Name** (lowercase alphanumeric and hyphens only).
3. Optionally enter a **Description** (human-readable purpose of the secret) and **URL** (link to the service that uses it).
4. Add one or more key-value entries using the file-based editor. Each entry has a **Key** field (the filename) and a **Value** field (multiline content area with monospace font).
5. Click **Create**. You are automatically added as the Owner of the new secret.

Duplicate keys are detected and flagged in the editor before submission.

### Viewing and Editing a Secret

Navigate to `/secrets/<name>` to view a secret's data. The detail page shows editable **Description** and **URL** fields at the top, followed by two view modes toggled via a button group:

- **Editor** (default) — Individual key-value entries in the file-based editor. Each entry shows the key (filename) and value (file content) as separate fields. Modify fields directly; the **Save** button enables when changes are detected (dirty checking). Saving replaces the entire secret data map.
- **Raw** — The full Kubernetes Secret manifest as pretty-printed JSON. The raw view converts `data` (base64) to `stringData` (plaintext) for readability. An "Include all fields" toggle controls whether server-managed metadata fields (uid, resourceVersion, creationTimestamp, etc.) are shown. A "Copy to Clipboard" button copies the rendered JSON. The raw view is read-only; Save is disabled while it is active.

### Deleting a Secret

Click **Delete** on the secret detail page or the delete icon on the secrets list. A confirmation dialog appears. Deletion is permanent and cannot be undone. Requires the Owner role.

### Sharing

Owners can manage access grants on the secret detail page via the Sharing panel. Grants can be scoped to individual users (by email) or roles (by OIDC role name), with optional time bounds (not-before and expiration timestamps). See [rbac.md](rbac.md) for the full access control model.

## Consuming Secrets in Pods

Kubernetes secrets created through holos-console are standard `Opaque` secrets. They can be consumed by pods using any standard Kubernetes mechanism.

### Volume Mounts (Recommended)

Mount the secret as a volume. Each key in the secret becomes a file in the mount path:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
spec:
  containers:
    - name: app
      image: my-app:latest
      volumeMounts:
        - name: app-credentials
          mountPath: /etc/secrets
          readOnly: true
  volumes:
    - name: app-credentials
      secret:
        secretName: my-app-credentials
```

With the example secret above, the pod would see:
- `/etc/secrets/database-url` containing `postgres://localhost/mydb`
- `/etc/secrets/api-key` containing `secret-key-value`

To mount a single key to a specific path, use the `items` field:

```yaml
volumes:
  - name: app-credentials
    secret:
      secretName: my-app-credentials
      items:
        - key: database-url
          path: db-connection-string
```

### Environment Variables

Reference individual keys as environment variables using `secretKeyRef`:

```yaml
containers:
  - name: app
    image: my-app:latest
    env:
      - name: DATABASE_URL
        valueFrom:
          secretKeyRef:
            name: my-app-credentials
            key: database-url
      - name: API_KEY
        valueFrom:
          secretKeyRef:
            name: my-app-credentials
            key: api-key
```

To inject all keys as environment variables at once, use `envFrom`:

```yaml
containers:
  - name: app
    image: my-app:latest
    envFrom:
      - secretRef:
          name: my-app-credentials
```

With `envFrom`, each key in the secret becomes an environment variable name. Choose key names accordingly (e.g., `DATABASE_URL` instead of `database-url`) if you plan to use `envFrom`.

## Programmatic Access

The `SecretsService` ConnectRPC API provides programmatic access to secrets. All RPCs require authentication via an `Authorization: Bearer <id_token>` header.

| RPC | Required Role | Description |
|---|---|---|
| `ListSecrets` | Viewer | List all console-managed secrets with metadata |
| `GetSecret` | Viewer | Retrieve a secret's data by name |
| `CreateSecret` | Editor | Create a new secret with data and sharing grants |
| `UpdateSecret` | Editor | Replace a secret's data map |
| `GetSecretRaw` | Viewer | Retrieve the full K8s Secret object as verbatim JSON |
| `DeleteSecret` | Owner | Delete a secret by name |
| `UpdateSharing` | Owner | Update sharing grants without touching data |

Secret data is transmitted as `map<string, bytes>` -- values are raw bytes, not base64-encoded, in the protobuf wire format. `CreateSecret` and `UpdateSecret` also accept a `string_data` field (`map<string, string>`) for plaintext values that are merged into `data` (with `string_data` taking precedence), matching Kubernetes `stringData` semantics. Both RPCs also accept optional `description` and `url` fields stored as Kubernetes annotations (`console.holos.run/description` and `console.holos.run/url`).
