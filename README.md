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

## Group-Based Access Control for Secrets

Access to secrets is controlled by comparing the user's OIDC groups with allowed groups specified on each secret.

### How It Works

1. **User Groups**: When a user logs in via OIDC, their group memberships are extracted from the `groups` claim in the ID token.

2. **Secret Allowed Groups**: Each secret specifies which groups can access it via the `holos.run/allowed-groups` annotation. This annotation must contain a JSON array of group names.

3. **Access Decision**: A user can read a secret if they belong to at least one group listed in the secret's `holos.run/allowed-groups` annotation. If the annotation is missing or empty, no users can access the secret.

### Secret Configuration

To make a secret accessible through the console, configure it with:

1. **Required label** (makes the secret visible in the UI):
   ```yaml
   labels:
     app.kubernetes.io/managed-by: console.holos.run
   ```

2. **Required annotation** (controls who can read the secret data):
   ```yaml
   annotations:
     holos.run/allowed-groups: '["admin", "developers"]'
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
    holos.run/allowed-groups: '["platform-team", "my-app-owners"]'
stringData:
  API_KEY: secret-value
type: Opaque
```

In this example, users in either the `platform-team` or `my-app-owners` groups can view the secret.

### UI Behavior

- **Secrets List**: The `/secrets` page shows all console-managed secrets. Secrets the user cannot access display a "No access" indicator with a tooltip showing which groups are allowed.
- **Secret Detail**: The `/secrets/:name` page displays the secret data in environment variable format (`KEY=value`) for authorized users. Unauthorized users receive a permission denied error.

## Kubernetes Integration

holos-console is designed to run behind a TLS-terminating Gateway or Ingress
controller.  Use the `--plain-http` flag to listen on plain HTTP and the
`--issuer` flag to set the OIDC issuer URL that matches the externally
reachable domain.

### Key Concepts

The `--issuer` flag value determines:

1. The embedded Dex OIDC provider's issuer URL.
2. The OIDC redirect URIs derived from the issuer base (everything before `/dex`):
   - **Redirect URI:** `{base}/ui/callback`
   - **Silent redirect URI:** `{base}/ui/silent-callback.html`
   - **Post-logout redirect URI:** `{base}/ui`
3. The HTTPRoute must forward the issuer path (`/dex`) and the UI path (`/ui`)
   to the holos-console Service so that both the OIDC flow and the frontend
   are reachable at the same origin.

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

### Example: Deploying with Dex behind a Gateway

The following example deploys holos-console behind a Gateway API
`HTTPRoute` at `https://holos.example.com`.

#### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: holos-console
  namespace: holos-console
spec:
  replicas: 1
  selector:
    matchLabels:
      app: holos-console
  template:
    metadata:
      labels:
        app: holos-console
    spec:
      serviceAccountName: holos-console
      containers:
        - name: holos-console
          image: ghcr.io/holos-run/holos-console:latest
          args:
            - --plain-http
            - --listen=:8080
            - --issuer=https://holos.example.com/dex
          ports:
            - name: http
              containerPort: 8080
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 2
            periodSeconds: 15
          readinessProbe:
            httpGet:
              path: /readyz
              port: http
            initialDelaySeconds: 2
            periodSeconds: 5
          resources:
            requests:
              cpu: 10m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
          securityContext:
            runAsNonRoot: true
            readOnlyRootFilesystem: true
```

#### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: holos-console
  namespace: holos-console
spec:
  selector:
    app: holos-console
  ports:
    - name: http
      port: 80
      targetPort: 8080
```

#### HTTPRoute

The HTTPRoute forwards traffic for `/dex`, `/ui`, and the root path to the
console backend.  Adjust `parentRefs` and `hostnames` for your Gateway.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: holos-console
  namespace: holos-console
spec:
  parentRefs:
    - name: default
      namespace: istio-ingress
  hostnames:
    - holos.example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /dex
        - path:
            type: PathPrefix
            value: /ui
        - path:
            type: PathPrefix
            value: /holos.console.v1
        - path:
            type: PathPrefix
            value: /metrics
        - path:
            type: Exact
            value: /
      backendRefs:
        - name: holos-console
          port: 80
```
