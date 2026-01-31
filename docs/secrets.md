# Secrets Management

holos-console provides a web UI for managing Kubernetes Secret objects. Secrets managed through the console are standard Kubernetes Secrets and can be consumed by pods using native Kubernetes mechanisms.

## Data Model

Each secret is a standard Kubernetes `Opaque` Secret with a `map<string, bytes>` data field. Each entry in the map represents a named file: the map key is the filename and the value is the file content stored as raw bytes.

The console manages only secrets with the label `app.kubernetes.io/managed-by=console.holos.run`. Secrets without this label are ignored.

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
    console.holos.run/share-users: '[{"principal":"alice@example.com","role":"owner"}]'
stringData:
  database.env: |
    DB_HOST=postgres.internal
    DB_PASSWORD=secret123
  tls.crt: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
type: Opaque
```

## UI Workflow

### Secrets List

The `/secrets` page lists all console-managed secrets in the configured namespace. Each entry shows the secret name, sharing summary, and an accessibility indicator. Secrets the current user cannot read are shown with a lock icon and cannot be opened.

### Creating a Secret

Click **Create Secret** on the secrets list page. The dialog asks for:

- **Name** -- lowercase alphanumeric and hyphens (must be a valid Kubernetes resource name).
- **Data** -- one or more key-value entries where each key is a filename and each value is the file content.

The creating user is automatically granted the **Owner** role on the new secret.

### Viewing and Editing a Secret

The `/secrets/:name` detail page displays the secret data. Users with Editor or Owner access can modify values and click **Save** to persist changes. The save operation replaces the entire data map on the Kubernetes Secret.

### Deleting a Secret

Owners can delete a secret from either the list page or the detail page. Deletion removes the underlying Kubernetes Secret object and is irreversible.

### Sharing

The detail page includes a sharing panel. Owners can grant access to other users (by email) or OIDC groups with a chosen role (Viewer, Editor, or Owner). Grants can optionally include time bounds:

- **Not Before (nbf)** -- the grant is inactive until this time.
- **Expires (exp)** -- the grant becomes inactive at this time.

See [rbac.md](rbac.md) for the full access control model.

## Consuming Secrets in Kubernetes Pods

Secrets managed through the console are standard Kubernetes Secrets. Pods consume them using native Kubernetes mechanisms -- no special sidecar or integration is required.

### Volume Mounts (Recommended)

Mount the secret as a volume to project each data key as a file inside the container:

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
        - name: credentials
          mountPath: /etc/secrets
          readOnly: true
  volumes:
    - name: credentials
      secret:
        secretName: my-app-credentials
```

With the example secret above, the container filesystem would contain:

```
/etc/secrets/database.env
/etc/secrets/tls.crt
```

Applications read these files at runtime. When the secret is updated (via the console UI or the API), the kubelet eventually updates the mounted files automatically.

To mount a single key rather than the entire secret, use the `items` field:

```yaml
volumes:
  - name: credentials
    secret:
      secretName: my-app-credentials
      items:
        - key: tls.crt
          path: tls.crt
```

### Environment Variables

Map individual keys to environment variables:

```yaml
containers:
  - name: app
    image: my-app:latest
    env:
      - name: DB_HOST
        valueFrom:
          secretKeyRef:
            name: my-app-credentials
            key: database.env
```

Note: this injects the entire file content as the environment variable value. If the file contains multiple lines (like the `database.env` example), consider using `envFrom` with a secret whose keys map directly to individual values, or use a volume mount and parse the file in your application.

### envFrom

To inject all keys as environment variables at once:

```yaml
containers:
  - name: app
    image: my-app:latest
    envFrom:
      - secretRef:
          name: my-app-credentials
```

Each key in the secret data map becomes an environment variable. This works best when keys are valid environment variable names and values are single-line strings.

## Programmatic Access

The console exposes a ConnectRPC `SecretsService` at `/holos.console.v1.SecretsService/`. Authenticated clients can call these RPCs directly using any gRPC or ConnectRPC client:

| RPC | Description | Required Role |
|-----|-------------|---------------|
| `ListSecrets` | List all console-managed secrets with metadata | Any authenticated user |
| `GetSecret` | Read secret data by name | Viewer or above |
| `CreateSecret` | Create a new secret with initial sharing grants | Editor or above |
| `UpdateSecret` | Replace the data map of an existing secret | Editor or above |
| `DeleteSecret` | Delete a secret | Owner |
| `UpdateSharing` | Update sharing grants without touching data | Owner |

All RPCs require a valid Bearer token in the `Authorization` header.
