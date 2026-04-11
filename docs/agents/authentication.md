# Authentication

OIDC PKCE flow: Requires `--enable-insecure-dex` flag for embedded Dex at `/dex/`, or an external OIDC provider configured with `--issuer`. Tokens stored in session storage, sent as `Authorization: Bearer` headers. Default credentials: `admin` / `verysecret` (override with `HOLOS_DEX_INITIAL_ADMIN_USERNAME`/`PASSWORD` env vars).

Backend auth: `LazyAuthInterceptor` in `console/rpc/auth.go` verifies JWTs from the `Authorization: Bearer` header and stores `rpc.Claims` in context. Lazy initialization avoids startup race with embedded Dex.

## Test Personas

When running with `--enable-insecure-dex`, embedded Dex registers four test identities (defined in `console/oidc/config.go`):

| Persona | Email | Groups | RBAC Role | Password |
|---------|-------|--------|-----------|----------|
| Admin (default) | `admin@localhost` | `["owner"]` | OWNER | (auto-login) |
| Platform Engineer | `platform@localhost` | `["owner"]` | OWNER | `verysecret` |
| Product Engineer | `product@localhost` | `["editor"]` | EDITOR | `verysecret` |
| SRE | `sre@localhost` | `["viewer"]` | VIEWER | `verysecret` |

The admin user is authenticated automatically via the auto-login connector. The other three personas are available via the dev token endpoint and the Dev Tools UI.

**Dev token endpoint** (`POST /api/dev/token`): Obtain a signed OIDC ID token for any test persona. Requires `--enable-insecure-dex`. See `docs/dev-token-endpoint.md` for full API reference.

```bash
curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" \
  -X POST https://localhost:8443/api/dev/token \
  -H "Content-Type: application/json" \
  -d '{"email":"sre@localhost"}'
```

**Dev Tools UI** (`/dev-tools`): Enable with `--enable-dev-tools` (passed automatically by `make run`). Provides an interactive persona switcher that injects tokens into sessionStorage without a Dex redirect. See ADR 023 for design rationale.

## Related

- [RBAC](rbac.md) — Four-tier access control model that uses these identities
- [Embedded Services](embedded-services.md) — How Dex is embedded in the binary
- [Testing Patterns](testing-patterns.md) — Multi-persona E2E helpers using the dev token endpoint
- [TLS Command Guardrail](guardrail-tls-commands.md) — Correct `--cacert` usage in examples
