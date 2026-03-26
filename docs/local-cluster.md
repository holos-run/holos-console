# Local Cluster

<!-- Vendored from https://github.com/holos-run/holos/blob/main/doc/md/topics/local-cluster.mdx -->

Set up a local k3d cluster for development and testing. After completing this
guide you'll have a Kubernetes API server with proper DNS and TLS certificates.

## Prerequisites

1. [k3d](https://k3d.io/#installation) - local Kubernetes via Docker
2. [OrbStack](https://docs.orbstack.dev/install) or [Docker](https://docs.docker.com/get-docker/) - container runtime
3. [kubectl](https://kubernetes.io/docs/tasks/tools/) - Kubernetes CLI
4. [mkcert](https://github.com/FiloSottile/mkcert) - trusted local TLS certificates
5. [jq](https://jqlang.github.io/jq/download/) - JSON processing (used by cluster scripts)

## One-Time DNS Setup

Configure your machine to resolve `*.holos.localhost` to your loopback
interface so requests reach the workload cluster:

```bash
scripts/local-dns
```

This installs dnsmasq via Homebrew and configures `/etc/resolver/holos.localhost`.
Requires `sudo` for system DNS configuration.

## Create the Cluster

Create a local k3d cluster with a container registry:

```bash
scripts/local-k3d
```

This creates:
- A local registry at `k3d-registry.holos.localhost:5100`
- A k3d cluster named `workload` with port 443 forwarded and Traefik disabled

## Setup Trusted TLS

Install the mkcert root CA into the cluster so cert-manager can issue trusted
certificates:

```bash
sudo -v
scripts/local-ca
```

Run this each time you recreate the cluster.

## Reset the Cluster

To reset to a clean state:

```bash
scripts/local-k3d    # Deletes and recreates the cluster
scripts/local-ca     # Re-installs the root CA
```

## Clean Up

Remove the cluster entirely:

```bash
k3d cluster delete workload
```
