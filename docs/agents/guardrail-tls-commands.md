# Guardrail: TLS Example Commands

**Never include TLS verification bypass flags in any example command**, whether in documentation, UI-rendered snippets, tests, or code comments. Specifically:

- Do not emit `curl -k`, `curl -sk`, or `curl --insecure`.
- Do not emit `grpcurl -insecure`.

**Reason**: Holos Console always runs with valid TLS certs. Local development uses `make certs` to generate a locally-trusted mkcert certificate; production uses a public-CA cert. There is no supported mode in which the server serves an untrusted certificate.

**Correct form** (matches `scripts/rpc-version`):

```bash
curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" https://localhost:8443/...
grpcurl -cacert "$(mkcert -CAROOT)/rootCA.pem" localhost:8443 ...
```

Production examples whose server cert chains to a public CA may omit `--cacert` entirely and rely on the system CA store.

**Not covered by this rule**: `--enable-insecure-dex` is the server CLI flag that enables the embedded Dex OIDC provider. It is unrelated to TLS verification and must not be changed or removed.

**Triggers**: Apply this rule when writing or editing any `.md`, `.go`, `.ts`, `.tsx`, or shell script that contains a `curl` or `grpcurl` example.

## Related

- [API Access](api-access.md) — Correct curl/grpcurl invocations
- [Authentication](authentication.md) — Dev token endpoint examples
