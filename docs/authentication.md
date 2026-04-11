# Authentication

This document describes the authentication system in holos-console.

## Overview

holos-console uses OIDC (OpenID Connect) with PKCE (Proof Key for Code Exchange) for authentication. The application embeds [Dex](https://dexidp.io/), a CNCF identity provider, which can be enabled for local development via the `--enable-insecure-dex` flag. For production, configure an external OIDC provider.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      holos-console binary                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────────┐      ┌──────────────────────────────────┐ │
│  │   Embedded Dex   │      │         Console Server           │ │
│  │  (opt-in only)   │      │                                  │ │
│  │  /dex/*          │      │  /*             (React SPA)      │ │
│  │                  │      │  /api/*         (ConnectRPC)     │ │
│  │  Auto-Login      │      │  /metrics       (Prometheus)     │ │
│  │  Connector       │      │                                  │ │
│  │  (no credentials │      │  JWT Validation via              │ │
│  │   required)      │      │  --issuer (any OIDC provider)    │ │
│  └──────────────────┘      └──────────────────────────────────┘ │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Embedded Dex Provider

The embedded Dex OIDC provider is **disabled by default** and must be explicitly enabled with the `--enable-insecure-dex` flag. When enabled, it runs at `/dex/*` and provides:

- **OIDC Discovery**: `/.well-known/openid-configuration` (under `/dex/`)
- **Authorization**: `/dex/auth`
- **Token Exchange**: `/dex/token`
- **User Info**: `/dex/userinfo`
- **JWKS**: `/dex/keys`

### Development Auto-Login

> **WARNING**: The embedded Dex server performs **no authentication**. Users are automatically logged in without entering credentials when they click "Login". Only enable this for local development.

The auto-login connector:
- Immediately authenticates users without showing a login form
- Assigns the configured username (default: `admin`)
- Assigns the user to the `owner` group (full permissions)
- Is intended for **local development only**

### Enabling Embedded Dex for Development

```bash
./holos-console --enable-insecure-dex --cert certs/tls.crt --key certs/tls.key
```

Or use the Makefile shortcut which includes the flag:

```bash
make run
```

### Customizing the Auto-Login Username

Override via environment variable before starting the server:

```bash
export HOLOS_DEX_INITIAL_ADMIN_USERNAME=myuser
./holos-console --enable-insecure-dex --cert certs/tls.crt --key certs/tls.key
```

## Test Personas

When running with `--enable-insecure-dex`, embedded Dex registers four test identities with distinct RBAC roles. These personas enable testing permission boundaries without an external identity provider.

| Persona | Email | Groups | RBAC Role | UserID |
|---------|-------|--------|-----------|--------|
| Admin (default) | `admin@localhost` | `["owner"]` | OWNER | `test-admin-001` |
| Platform Engineer | `platform@localhost` | `["owner"]` | OWNER | `test-platform-001` |
| Product Engineer | `product@localhost` | `["editor"]` | EDITOR | `test-product-001` |
| SRE | `sre@localhost` | `["viewer"]` | VIEWER | `test-sre-001` |

All non-admin users share the password `verysecret`. The admin user authenticates automatically via the auto-login connector (no credentials required).

The persona definitions live in `console/oidc/config.go` as the `TestUsers` variable.

### Dev Token Endpoint

The `POST /api/dev/token` endpoint provides programmatic token acquisition for any registered test user. This is used by E2E test helpers and the Dev Tools persona switcher. See [docs/dev-token-endpoint.md](dev-token-endpoint.md) for the full API reference.

### Dev Tools UI

When `--enable-dev-tools` is also set (included in `make run`), a Dev Tools page at `/dev-tools` provides an interactive persona switcher. Clicking a persona card injects a signed token into sessionStorage and reloads the page, instantly switching the authenticated identity without a Dex redirect.

See [ADR 023](adrs/023-multi-persona-test-identities.md) for the design rationale.

## Authentication Flow

1. **User clicks Login** - React SPA calls `login()` from `useAuth()` hook
2. **PKCE Challenge Generated** - oidc-client-ts generates code verifier and challenge
3. **Redirect to Dex** - Browser redirects to `/dex/auth` with PKCE parameters
4. **Auto-Login** - Embedded Dex immediately authenticates user (no form displayed)
5. **Authorization Code Returned** - Dex redirects to `/pkce/verify` with code
6. **Token Exchange** - Callback component exchanges code for tokens via `/dex/token`
7. **Session Established** - Tokens stored in session storage, user redirected to app

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--enable-insecure-dex` | `false` | Enable the built-in Dex OIDC provider with auto-login |
| `--enable-dev-tools` | `false` | Enable Dev Tools UI (persona switcher at `/dev-tools`) |
| `--issuer` | (none) | OIDC issuer URL for token validation |
| `--client-id` | `holos-console` | Expected audience for tokens |
| `--listen` | `:8443` | Address to listen on |
| `--cert` | (auto-generated) | TLS certificate file |
| `--key` | (auto-generated) | TLS key file |
| `--id-token-ttl` | `1h` | ID token lifetime |
| `--refresh-token-ttl` | `12h` | Refresh token absolute lifetime |

## Using an External OIDC Provider

For production, configure an external identity provider:

```bash
./holos-console \
  --issuer=https://dex.example.com \
  --client-id=holos-console \
  --cert server.crt \
  --key server.key
```

When `--issuer` points to an external URL, JWT validation uses the external issuer's OIDC discovery. The embedded Dex provider is not started unless `--enable-insecure-dex` is also set.

### External Provider Requirements

Your external OIDC provider must:

1. Support PKCE with S256 challenge method
2. Allow public clients (no client secret)
3. Have `holos-console` registered as a client with redirect URI matching your deployment

### Example: Configuring Dex as External Provider

```yaml
# dex-config.yaml
issuer: https://dex.example.com

staticClients:
  - id: holos-console
    name: Holos Console
    public: true
    redirectURIs:
      - https://console.example.com/pkce/verify
```

## React SPA Integration

The React frontend uses [oidc-client-ts](https://github.com/authts/oidc-client-ts) for OIDC.

### Using the Auth Hook

```tsx
import { useAuth } from './auth'

function LoginButton() {
  const { isAuthenticated, login, logout, user } = useAuth()

  if (isAuthenticated) {
    return (
      <button onClick={logout}>
        Logout {user?.profile.name}
      </button>
    )
  }

  return <button onClick={login}>Login</button>
}
```

### ConnectRPC Token Injection

All ConnectRPC requests made through the shared `transport` (from `frontend/src/lib/transport.ts`) have Bearer tokens attached automatically by the `createAuthInterceptor`. Query hooks in `frontend/src/queries/` use this transport via the `TransportProvider` in `__root.tsx`, so no manual token injection is needed.

The interceptor also handles token expiry: on a `401 Unauthenticated` response it calls `signinSilent()` once to renew the token and retries the request. Concurrent 401s are coalesced so only one renewal flow runs at a time.

## Security Considerations

### PKCE

PKCE (RFC 7636) prevents authorization code interception attacks. The SPA generates a random code verifier, hashes it to create a challenge, and the authorization server verifies the original verifier during token exchange.

### Token Storage

Tokens are stored in session storage (not local storage) by default:
- Survives page refreshes within the same session
- Cleared when the browser tab is closed
- Not shared between tabs

### Automatic Token Renewal

The auth provider automatically renews tokens before expiration using silent refresh.

## Troubleshooting

### "OIDC discovery failed"

Verify the OIDC provider is accessible:

```bash
curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" https://localhost:8443/dex/.well-known/openid-configuration
```

### "Callback error" after login

Check that the redirect URI matches the configuration:
- For development: `https://localhost:5173/pkce/verify` (Vite)
- For production: `https://your-host/pkce/verify`

### CORS errors

The embedded Dex provider allows CORS from the same origin. For external providers, configure CORS to allow your deployment origin.
