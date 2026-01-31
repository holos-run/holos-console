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

## Access Control

Access to secrets uses a two-tier model: **platform roles** and **per-secret sharing grants**. Platform roles (configured via `--platform-viewers`, `--platform-editors`, `--platform-owners` flags) map OIDC groups to baseline roles across all secrets. Per-secret sharing grants (stored as Kubernetes annotations) provide fine-grained access to individual secrets.

The highest role from any source wins. See [docs/rbac.md](docs/rbac.md) for the full access control model.

### Secret Configuration

To make a secret accessible through the console, configure it with:

1. **Required label** (makes the secret visible in the UI):
   ```yaml
   labels:
     app.kubernetes.io/managed-by: console.holos.run
   ```

2. **Sharing annotations** (controls who can access the secret):
   ```yaml
   annotations:
     console.holos.run/share-users: '[{"principal":"alice@example.com","role":"owner"},{"principal":"bob@example.com","role":"viewer"}]'
     console.holos.run/share-groups: '[{"principal":"dev-team","role":"editor"}]'
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
    console.holos.run/share-users: '[{"principal":"alice@example.com","role":"owner"}]'
    console.holos.run/share-groups: '[{"principal":"platform-team","role":"editor"}]'
stringData:
  API_KEY: secret-value
type: Opaque
```

### UI Behavior

- **Secrets List**: The `/secrets` page shows all console-managed secrets with accessibility indicators and sharing summaries.
- **Creating Secrets**: Click "Create Secret" to open a dialog with a name field and a file-based key-value editor. Each entry has a key (filename) and a multiline value (file content). The creator is automatically granted the Owner role.
- **Secret Detail**: The `/secrets/:name` page displays secret data as individual key-value entries in a file-based editor. Editors can modify entries and save changes. Owners can delete the secret and manage sharing grants with optional time bounds. Unauthorized users receive a permission denied error.
- **Consuming Secrets**: Secrets are standard Kubernetes `Opaque` secrets. Mount them as volumes (each key becomes a file) or inject them as environment variables. See [docs/secrets.md](docs/secrets.md) for detailed examples.

## Kubernetes Integration

holos-console is designed to run behind a TLS-terminating Gateway or Ingress
controller.  Use the `--plain-http` flag to listen on plain HTTP and the
`--issuer` flag to set the OIDC issuer URL that matches the externally
reachable domain.

### Key Concepts

The `--issuer` flag value determines:

1. The embedded Dex OIDC provider's issuer URL.
2. The OIDC redirect URIs derived from the issuer base (everything before `/dex`):
   - **Redirect URI:** `{base}/pkce/verify`
   - **Post-logout redirect URI:** `{base}/ui`
3. The HTTPRoute must forward the issuer path (`/dex`), the UI path (`/ui`),
   and the PKCE callback path (`/pkce`) to the holos-console Service so that
   the OIDC flow and the frontend are reachable at the same origin.

If you use an **external Dex instance** instead of the embedded one, configure
a static client with:

- **Client ID:** `holos-console` (or the value of `--client-id`)
- **Redirect URIs:** the three URIs listed above

### Health Probes

holos-console exposes conventional Kubernetes health endpoints:

| Path | Purpose | Behavior |
|------|---------|----------|
| `/healthz` | Liveness probe | Returns `200 OK` when the process is alive |
| `/readyz` | Readiness probe | Returns `200 OK` when the server is ready to accept traffic |

### Example Manifests

See the `deploy/` directory for reference Kubernetes manifests including
Deployment, Service, ServiceAccount, RBAC, and namespace resources.

The HTTPRoute must forward the following paths to the holos-console Service:

| Path | Purpose |
|------|---------|
| `/dex` | Embedded OIDC provider (for expedience, not production use) |
| `/pkce` | OIDC PKCE callback (`/pkce/verify`) |
| `/ui` | Frontend SPA |
| `/holos.console.v1` | ConnectRPC and gRPC public API |
| `/metrics` | Prometheus metrics |
| `/` | Root redirect to `/ui` |

The ConnectRPC and gRPC endpoints under `/holos.console.v1` are a public API
intended for programmatic access.  Authenticated clients can call these
endpoints directly using any gRPC or ConnectRPC client.
