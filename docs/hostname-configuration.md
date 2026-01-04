# Hostname Configuration

This document explains how the hostname and port flow through the entire holos-console stack. It's written for contributors wondering how to set the hostname in one place and have it propagate everywhere.

## Key Concept

The `--issuer` flag is the **canonical source of truth** for the external URL.

```bash
./holos-console --issuer=https://console.example.com/dex
```

This single flag determines:
- The OIDC issuer URL in discovery documents
- The `iss` claim in issued tokens
- The expected hostname for redirect URIs
- The URL the SPA uses for authentication

## Configuration Flow

### 1. CLI Entry Point

**File:** [cli/cli.go](../cli/cli.go)

The CLI defines two key flags:

| Flag | Default | Purpose |
|------|---------|---------|
| `--issuer` | `https://localhost:8443/dex` | External URL for OIDC |
| `--listen` | `:8443` | Internal bind address |

The issuer URL determines the external hostname. The listen address is the internal bind address and doesn't need to match the issuer's port (useful behind a load balancer or reverse proxy).

```go
cmd.Flags().StringVar(&issuer, "issuer", "https://localhost:8443/dex",
    "OIDC issuer URL for token validation")
cmd.Flags().StringVar(&listenAddr, "listen", ":8443",
    "Address to listen on")
```

### 2. Console Server

**File:** [console/console.go](../console/console.go)

The console server receives the issuer URL from the CLI and:

1. Passes it to the OIDC provider configuration
2. Derives the redirect URI by stripping `/dex` and appending `/ui/callback`
3. Uses it for CORS configuration (allowed origins)

```go
// Derive redirect URI from issuer
redirectURI := strings.TrimSuffix(s.cfg.Issuer, "/dex") + "/ui/callback"

oidcHandler, err := oidc.NewHandler(ctx, oidc.Config{
    Issuer:       s.cfg.Issuer,
    ClientID:     s.cfg.ClientID,
    RedirectURIs: []string{redirectURI},
})
```

### 3. Embedded OIDC Provider (Dex)

**File:** [console/oidc/oidc.go](../console/oidc/oidc.go)

The OIDC provider receives the full issuer URL including the mount path:

```go
dexServer, err := server.NewServer(ctx, server.Config{
    Issuer:  cfg.Issuer,  // e.g., "https://console.example.com/dex"
    Storage: store,
    // ...
})
```

This issuer appears in:
- OIDC discovery document (`/.well-known/openid-configuration`)
- Token `iss` claim
- JWKS endpoint URL

### 4. JWT Validation

**File:** [console/auth.go](../console/auth.go)

The JWT verifier fetches OIDC discovery from the issuer URL and validates that tokens have a matching issuer claim:

```go
verifier, err := NewIDTokenVerifier(ctx, issuer, clientID)
// Verifier checks that token.iss == issuer
```

### 5. React SPA - Production

**Files:** [ui/src/auth/config.ts](../ui/src/auth/config.ts)

In production, the SPA reads configuration from `window.__OIDC_CONFIG__`, which would be injected by the server based on the request. Currently, the SPA falls back to using `window.location.origin`:

```typescript
function getConfig(): OIDCConfig {
  // Check for server-injected config (production)
  if (window.__OIDC_CONFIG__) {
    return window.__OIDC_CONFIG__
  }

  // Development defaults
  const origin = window.location.origin
  return {
    authority: `${origin}/dex`,
    client_id: 'holos-console',
    redirect_uri: `${origin}/ui/callback`,
    post_logout_redirect_uri: `${origin}/ui`,
  }
}
```

### 6. React SPA - Development

**File:** [ui/vite.config.ts](../ui/vite.config.ts)

During development, the Vite dev server runs on a different port (5173) and proxies OIDC requests to the Go backend:

```typescript
proxy: {
  '/dex': {
    target: 'https://localhost:8443',
    secure: false,
    changeOrigin: true,
  },
}
```

The SPA uses `window.location.origin` which resolves to the Vite dev server, but the proxy transparently forwards to the Go backend.

## Example: Changing the Hostname

To run holos-console on `https://myhost.local:9443`:

### 1. Generate Certificates

```bash
mkcert myhost.local
```

This creates `myhost.local.pem` and `myhost.local-key.pem`.

### 2. Start the Server

```bash
./holos-console \
  --listen=:9443 \
  --cert-file=myhost.local.pem \
  --key-file=myhost.local-key.pem \
  --issuer=https://myhost.local:9443/dex
```

### 3. Result

The issuer URL flows through the entire stack:

| Component | URL |
|-----------|-----|
| OIDC Discovery | `https://myhost.local:9443/dex/.well-known/openid-configuration` |
| Token `iss` claim | `https://myhost.local:9443/dex` |
| SPA Authority | `https://myhost.local:9443/dex` |
| Redirect URI | `https://myhost.local:9443/ui/callback` |

## Behind a Reverse Proxy

When running behind a reverse proxy (e.g., nginx, Traefik), the listen address and issuer can differ:

```bash
./holos-console \
  --listen=:8080 \                                    # Internal port
  --issuer=https://console.example.com/dex            # External URL
```

The proxy terminates TLS and forwards to the internal port. The issuer URL reflects the external URL that clients use.

## Key Files Reference

| File | Purpose |
|------|---------|
| [cli/cli.go](../cli/cli.go) | Flag definitions (`--issuer`, `--listen`) |
| [console/console.go](../console/console.go) | URL parsing, handler setup |
| [console/oidc/oidc.go](../console/oidc/oidc.go) | Dex configuration with issuer |
| [ui/src/auth/config.ts](../ui/src/auth/config.ts) | Frontend OIDC config |
| [ui/vite.config.ts](../ui/vite.config.ts) | Dev server proxy configuration |

## Common Mistakes

### Mismatched Issuer and Listen Port

```bash
# Wrong: issuer port doesn't match listen port
./holos-console --listen=:8443 --issuer=https://localhost:9443/dex
```

Tokens will be issued with `iss=https://localhost:9443/dex` but the server is on port 8443. Fix by matching the ports (unless using a reverse proxy).

### Missing /dex Suffix

```bash
# Wrong: missing /dex suffix
./holos-console --issuer=https://localhost:8443
```

The embedded Dex provider is mounted at `/dex/`. The issuer must include this path.

### Forgetting to Regenerate Certificates

When changing hostnames, regenerate certificates:

```bash
# Old hostname
mkcert localhost

# New hostname - need new certs!
mkcert myhost.local
```
