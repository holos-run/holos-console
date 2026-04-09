# API Access

## Overview

The Holos Console backend is a ConnectRPC server that simultaneously speaks the
Connect protocol, gRPC, and gRPC-Web on the same TLS listener. gRPC reflection
is enabled unconditionally (ADR 009), so any HTTP client — including plain
`curl` — can call the API without extra tooling.

The Connect protocol is the recommended path: it uses ordinary JSON over HTTPS
with two extra headers, which means `curl` alone is sufficient. The gRPC path
(`grpcurl`) is supported for backward compatibility and for users who prefer the
gRPC mental model.

## Getting an ID Token

The profile page (`/profile`) has an **API Access** section that displays your
current ID token in a copy-pastable shell form.

The token is a JWT signed by Dex and verified by the server's `IDTokenVerifier`
(audience = client ID). It expires when the browser session expires. The refresh
token is deliberately not shown — it grants long-lived silent renewals and must
never leave the browser.

Copy the export snippet from the profile page and paste it into your terminal:

```bash
export HOLOS_ID_TOKEN="<paste id token here>"
```

Before pasting, disable command history in your shell so the token does not land
in your history file. See the per-shell instructions below.

### Disabling shell history before pasting

**zsh** — disable history recording for the current session:

```zsh
unset HISTFILE
export HOLOS_ID_TOKEN="<paste id token here>"
```

Alternatively, prefix the export with a leading space and enable
`HIST_IGNORE_SPACE` (lines starting with a space are not recorded):

```zsh
setopt HIST_IGNORE_SPACE
 export HOLOS_ID_TOKEN="<paste id token here>"
```

To restore history, start a new shell or run
`export HISTFILE=~/.zsh_history`.

**bash** — wrap the export with `set +o history` / `set -o history`:

```bash
set +o history
export HOLOS_ID_TOKEN="<paste id token here>"
set -o history
```

`set +o history` disables recording for the current shell session.
`set -o history` re-enables it after you paste.

## Calling an RPC with curl (Connect protocol — recommended)

All examples assume a valid TLS certificate. For local development, run
`make certs` to generate a locally-trusted mkcert certificate; the
`--cacert "$(mkcert -CAROOT)/rootCA.pem"` argument validates against
that local root CA. For production deployments whose server cert chains
to a public CA, `--cacert` can be omitted entirely.

The Connect protocol uses `POST /<package>.<service>/<method>` with a JSON body
and two headers:

```bash
curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" \
  https://localhost:8443/holos.console.v1.OrganizationService/ListOrganizations \
  -H "Content-Type: application/json" \
  -H "Connect-Protocol-Version: 1" \
  -H "Authorization: Bearer $HOLOS_ID_TOKEN" \
  -d '{}'
```

Header notes:

- `Content-Type: application/json` selects the Connect+JSON unary codec.
- `Connect-Protocol-Version: 1` is required by the Connect protocol spec.

Replace `localhost:8443` with whatever origin the console is served from.

## Calling an RPC with grpcurl (gRPC backward compatibility)

ConnectRPC handlers also speak native gRPC on the same port. Use `-cacert`
(not `-plaintext` or `-insecure`) because the listener is TLS-only and the
server always presents a valid certificate:

```bash
grpcurl -cacert "$(mkcert -CAROOT)/rootCA.pem" \
  -H "Authorization: Bearer $HOLOS_ID_TOKEN" \
  -d '{}' \
  localhost:8443 \
  holos.console.v1.OrganizationService/ListOrganizations
```

## gRPC Reflection

Reflection is unauthenticated by design (ADR 009). List all services:

```bash
grpcurl -cacert "$(mkcert -CAROOT)/rootCA.pem" localhost:8443 list
```

Describe a service or message:

```bash
grpcurl -cacert "$(mkcert -CAROOT)/rootCA.pem" localhost:8443 describe holos.console.v1.OrganizationService
grpcurl -cacert "$(mkcert -CAROOT)/rootCA.pem" localhost:8443 describe holos.console.v1.ListOrganizationsRequest
```

## Rendered Preview Example

The `GetDeploymentRenderPreview` RPC returns the rendered CUE template output
for a live deployment. With `curl`:

```bash
curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" \
  https://localhost:8443/holos.console.v1.DeploymentService/GetDeploymentRenderPreview \
  -H "Content-Type: application/json" \
  -H "Connect-Protocol-Version: 1" \
  -H "Authorization: Bearer $HOLOS_ID_TOKEN" \
  -d '{"project": "<project-name>", "name": "<deployment-name>"}'
```

With `grpcurl`:

```bash
grpcurl -cacert "$(mkcert -CAROOT)/rootCA.pem" \
  -H "Authorization: Bearer $HOLOS_ID_TOKEN" \
  -d '{"project": "<project-name>", "name": "<deployment-name>"}' \
  localhost:8443 \
  holos.console.v1.DeploymentService/GetDeploymentRenderPreview
```

The deployment detail page (`/projects/<p>/deployments/<d>`) shows pre-filled
versions of both commands with the correct project and deployment names
substituted in.

## Troubleshooting: `first record does not look like a TLS handshake`

This error appears in the server logs when the client used `-plaintext`:

```
http: TLS handshake error from ...: tls: first record does not look like a TLS handshake
```

The client-side symptom is:

```
Failed to dial target host "localhost:8443": context deadline exceeded
```

**Root cause**: `-plaintext` tells `grpcurl` to open an h2c (HTTP/2 cleartext)
connection — it sends the h2c connection preface (`PRI * HTTP/2.0...`) without a
TLS ClientHello. The server's TLS stack reads that preface as the first TLS
record and rejects it.

**Fix**: Drop `-plaintext`. Use `-cacert "$(mkcert -CAROOT)/rootCA.pem"` for
local development with mkcert, or omit `--cacert` entirely for production
servers whose cert chains to a public CA.
