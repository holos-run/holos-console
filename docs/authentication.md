# Authentication

This document describes the authentication system in holos-console.

## Overview

holos-console uses OIDC (OpenID Connect) with PKCE (Proof Key for Code Exchange) for authentication. The application embeds [Dex](https://dexidp.io/), a CNCF identity provider, directly into the binary for development and simple deployments. For production, you can configure an external OIDC provider.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      holos-console binary                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────────┐      ┌──────────────────────────────────┐ │
│  │   Embedded Dex   │      │         Console Server           │ │
│  │                  │      │                                  │ │
│  │  /dex/*          │      │  /ui/*          (React SPA)      │ │
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

The embedded Dex OIDC provider runs at `/dex/*` and provides:

- **OIDC Discovery**: `/.well-known/openid-configuration` (under `/dex/`)
- **Authorization**: `/dex/auth`
- **Token Exchange**: `/dex/token`
- **User Info**: `/dex/userinfo`
- **JWKS**: `/dex/keys`

### Development Auto-Login

> **IMPORTANT**: The embedded Dex server performs **no authentication** for development convenience. Users are automatically logged in without entering credentials when they click "Login".

The auto-login connector:
- Immediately authenticates users without showing a login form
- Assigns the configured username (default: `admin`)
- Assigns the user to the `owner` group (full permissions)
- Is intended for **local development only**

For production deployments, configure an external OIDC provider with proper authentication.

### Customizing the Auto-Login Username

Override via environment variable before starting the server:

```bash
export HOLOS_DEX_INITIAL_ADMIN_USERNAME=myuser
./holos-console --cert-file=... --key-file=...
```

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
| `--issuer` | `https://localhost:8443/dex` | OIDC issuer URL for token validation |
| `--client-id` | `holos-console` | Expected audience for tokens |
| `--listen` | `:8443` | Address to listen on |
| `--cert-file` | (auto-generated) | TLS certificate file |
| `--key-file` | (auto-generated) | TLS key file |
| `--id-token-ttl` | `15m` | ID token lifetime |
| `--refresh-token-ttl` | `12h` | Refresh token absolute lifetime |

## Using an External OIDC Provider

For production, configure an external identity provider:

```bash
./holos-console \
  --issuer=https://dex.example.com \
  --client-id=holos-console \
  --cert-file=server.crt \
  --key-file=server.key
```

When `--issuer` points to an external URL, the embedded Dex provider still runs but JWT validation uses the external issuer's OIDC discovery.

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

### Getting Access Tokens for API Calls

```tsx
import { useAuth } from './auth'

function MyComponent() {
  const { getAccessToken } = useAuth()

  const fetchData = async () => {
    const token = getAccessToken()
    const response = await fetch('/api/endpoint', {
      headers: {
        Authorization: `Bearer ${token}`,
      },
    })
    // ...
  }
}
```

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
curl -k https://localhost:8443/dex/.well-known/openid-configuration
```

### "Callback error" after login

Check that the redirect URI matches the configuration:
- For development: `https://localhost:5173/pkce/verify` (Vite)
- For production: `https://your-host/pkce/verify`

### CORS errors

The embedded Dex provider allows CORS from the same origin. For external providers, configure CORS to allow your deployment origin.
