# Dev Token Endpoint

The dev token endpoint provides programmatic access to OIDC ID tokens for any registered test user. This enables API testing, E2E tests, and CLI workflows without requiring a browser-based OIDC flow.

## Availability

The endpoint is only available when the embedded Dex OIDC provider is enabled via `--enable-insecure-dex`. It is not mounted otherwise and returns 404.

## Endpoint

```
POST /api/dev/token
Content-Type: application/json
```

### Request

```json
{
  "email": "platform@localhost"
}
```

The `email` field must match one of the registered test users:

| Email | Role | Groups |
|-------|------|--------|
| `admin@localhost` | Admin (Owner) | `["owner"]` |
| `platform@localhost` | Platform Engineer (Owner) | `["owner"]` |
| `product@localhost` | Product Engineer (Editor) | `["editor"]` |
| `sre@localhost` | SRE (Viewer) | `["viewer"]` |

### Response

```json
{
  "id_token": "eyJhbGciOiJSUzI1NiIs...",
  "email": "platform@localhost",
  "groups": ["owner"],
  "expires_in": 3600
}
```

| Field | Type | Description |
|-------|------|-------------|
| `id_token` | string | A signed OIDC ID token (JWT) that passes `LazyAuthInterceptor` verification |
| `email` | string | The email address of the authenticated user |
| `groups` | string[] | The OIDC groups included in the token |
| `expires_in` | integer | Token lifetime in seconds (3600 = 1 hour) |

### Error Responses

| Status | Condition |
|--------|-----------|
| 400 | Email is empty, not recognized, or request body is invalid JSON |
| 404 | Dex is not enabled (`--enable-insecure-dex` not set) |
| 405 | Request method is not POST |
| 500 | Internal error (e.g., Dex signing keys not yet initialized) |

## Usage Examples

### Obtain a token with curl

```bash
curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" \
  -X POST https://localhost:8443/api/dev/token \
  -H "Content-Type: application/json" \
  -d '{"email":"platform@localhost"}'
```

### Use the token to call an RPC

```bash
# Get a token
TOKEN=$(curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" \
  -X POST https://localhost:8443/api/dev/token \
  -H "Content-Type: application/json" \
  -d '{"email":"platform@localhost"}' | jq -r .id_token)

# Call an RPC with the token
curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" \
  -H "Connect-Protocol-Version: 1" \
  -H "Authorization: Bearer $TOKEN" \
  https://localhost:8443/holos.console.v1.VersionService/GetVersion
```

### Switch personas in a test script

```bash
for email in admin@localhost platform@localhost product@localhost sre@localhost; do
  TOKEN=$(curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" \
    -X POST https://localhost:8443/api/dev/token \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$email\"}" | jq -r .id_token)
  echo "=== $email ==="
  curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" \
    -H "Connect-Protocol-Version: 1" \
    -H "Authorization: Bearer $TOKEN" \
    https://localhost:8443/holos.console.v1.OrganizationService/ListOrganizations \
    -d '{}'
done
```

## Implementation Details

The endpoint mints tokens directly using Dex's signing keys from the in-memory storage. This approach:

- Produces tokens identical in structure to those Dex issues through the standard OIDC flow
- Avoids registering additional Dex connectors (which would break auto-login by causing Dex to show a connector selection page)
- Uses the same signing key and algorithm that Dex uses for all other tokens

The token claims include `iss`, `sub`, `aud`, `exp`, `iat`, `email`, `email_verified`, `groups`, and `name`, matching the scopes `openid`, `email`, `groups`, and `profile`.

## Security

This endpoint is for **local development only**. It requires no authentication and returns tokens for any registered test user. It is gated behind `--enable-insecure-dex` to prevent accidental exposure in production.

## See Also

- [ADR 023](adrs/023-multi-persona-test-identities.md) -- Design decisions for the multi-persona test identity system
- [Authentication](authentication.md) -- Overview of the OIDC authentication system and test personas
- [E2E Testing](e2e-testing.md) -- Multi-persona E2E test patterns using this endpoint
- [CONTRIBUTING.md](../CONTRIBUTING.md) -- Dev tools setup and persona switching for local development
