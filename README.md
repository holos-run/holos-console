# Holos Console

A Go HTTPS server with a React frontend that serves a web console UI and
exposes ConnectRPC services. The built UI is embedded into the Go binary via
`go:embed`.

## Quick Start

A first-time cluster operator can bootstrap the full cluster surface — CRDs,
admission policies, RBAC, and the secret-injector ServiceAccount — and reach a
running server with a handful of commands.

### Prerequisites

- A Kubernetes cluster. Run `make cluster` to create a local k3d cluster
  (installs DNS, k3d, and a local mkcert CA). Alternatively, point `kubectl` at
  any existing cluster.
- `kubectl` context set to that cluster.
- `mkcert` for local TLS certificates, plus `libnss3-tools` (Debian/Ubuntu)
  or `nss-tools` (Fedora/RHEL) so `mkcert -install` can register the local
  CA with the Chromium and Firefox NSS trust stores. Without the NSS tools
  Playwright-driven E2E tests will fail at the TLS handshake.
- Go 1.25+ and Node 18+ / npm for building the server and frontend.

See [CONTRIBUTING.md](CONTRIBUTING.md) for full toolchain setup instructions,
including Debian 13 / fresh-VM steps.

### Apply the cluster surface

The kustomize overlays are split into two layers. Namespace-scoped resources
(the holos-secret-injector ServiceAccount, Role, and RoleBinding in
`holos-system`) must be applied **before** the cluster-scoped overlay because
the ClusterRoleBindings in that overlay reference the ServiceAccount by name.
See [config/secret-injector/namespace-scoped/README.md](config/secret-injector/namespace-scoped/README.md)
for the rationale.

The `holos-system` namespace must exist before the namespace-scoped overlay is
applied. Kustomize overlays intentionally do not declare the Namespace resource
itself (M1 deferral), so create it first, idempotently:

```bash
kubectl create namespace holos-system --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -k config/namespace-scoped/
kubectl apply -k config/cluster-scoped/
```

This is the same sequence that `make kind-up` (via `scripts/kind-up`) runs for
automated cluster bootstrap.

### Run the server

```bash
make certs
make run
```

The server listens on <https://localhost:8443>. With the cluster surface in
place the controller manager primes its informers cleanly — you will not see
`no matches for kind "RenderState"` or missing RBAC errors in the logs.

For deeper context on project conventions, testing, and architecture, see
[AGENTS.md](AGENTS.md).

## Development

See `AGENTS.md` for project conventions and the `Makefile` for common tasks:

- `make test` runs Go and UI unit tests (`test-go` and `test-ui`)
- `make generate` regenerates code when proto or schema files change
- `make test-e2e` runs Playwright end-to-end tests; the Playwright config
  starts the Go backend and Vite dev server automatically
