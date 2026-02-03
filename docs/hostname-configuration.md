# Hostname Configuration

This document explains how the hostname and port flow through the holos-console stack.

## Key Concepts

Two flags control how the server presents itself externally:

| Flag | Default | Purpose |
|------|---------|---------|
| `--origin` | derived from `--listen` | Public-facing base URL for redirect URIs |
| `--issuer` | derived from `--listen` | OIDC issuer URL for Dex and token validation |
| `--listen` | `:8443` | Internal bind address |

The `--origin` flag is the source of truth for the server's public-facing base URL. Redirect URIs are derived from it:

| Derived URI | Value |
|-------------|-------|
| Redirect URI | `{origin}/pkce/verify` |
| Post-logout redirect URI | `{origin}/ui` |

The `--issuer` flag determines the OIDC issuer URL used for JWT validation. When `--enable-insecure-dex` is set, it also configures the embedded Dex provider. It does not affect redirect URIs.

When `--enable-insecure-dex` is set and neither `--origin` nor `--issuer` is provided, both are derived from `--listen`:

```
--listen :8443  →  origin  = https://localhost:8443
                →  issuer  = https://localhost:8443/dex
```

## Configuration Flow

### 1. CLI Entry Point

**File:** [cli/cli.go](../cli/cli.go)

The `deriveOrigin()` and `deriveIssuer()` functions resolve defaults from `--listen` when the flags are not explicitly set. With `--plain-http`, the scheme changes to `http`. The host `0.0.0.0` is normalized to `localhost`.

### 2. Console Server

**File:** [console/console.go](../console/console.go)

The server uses `origin` and `issuer` independently:

```go
// Redirect URIs derived from origin (not issuer)
redirectURI := deriveRedirectURI(s.cfg.Origin)       // {origin}/pkce/verify
postLogout := derivePostLogoutRedirectURI(s.cfg.Origin) // {origin}/ui

// Issuer passed directly to embedded Dex
oidcHandler, err := oidc.NewHandler(ctx, oidc.Config{
    Issuer:       s.cfg.Issuer,
    RedirectURIs: redirectURIs,
})
```

The OIDC configuration injected into the frontend combines both:

```go
oidcConfig = &OIDCConfig{
    Authority:             s.cfg.Issuer,                        // from --issuer
    RedirectURI:           deriveRedirectURI(s.cfg.Origin),     // from --origin
    PostLogoutRedirectURI: derivePostLogoutRedirectURI(s.cfg.Origin),
}
```

### 3. Embedded OIDC Provider (Dex)

**File:** [console/oidc/oidc.go](../console/oidc/oidc.go)

Dex receives the issuer URL and uses it for the OIDC discovery document, the `iss` claim in tokens, and the JWKS endpoint. It is mounted at `/dex/`.

### 4. React SPA

**File:** [ui/src/auth/config.ts](../ui/src/auth/config.ts)

The server injects `window.__OIDC_CONFIG__` into the HTML with the authority, redirect URI, and post-logout redirect URI. The SPA reads this at startup. A fallback derives these from `window.location.origin` if injection is missing.

During development, the Vite dev server proxies `/dex` requests to the Go backend so the SPA's origin-based URLs resolve correctly.

## Examples

### Local Development (defaults)

```bash
make run
# Equivalent to:
# --listen=:8443 --origin=https://localhost:8443 --issuer=https://localhost:8443/dex
```

| Component | URL |
|-----------|-----|
| OIDC Issuer | `https://localhost:8443/dex` |
| Redirect URI | `https://localhost:8443/pkce/verify` |
| Post-logout | `https://localhost:8443/ui` |

### Custom Hostname

```bash
./holos-console \
  --listen=:9443 \
  --cert=myhost.local.pem \
  --key=myhost.local-key.pem \
  --origin=https://myhost.local:9443 \
  --issuer=https://myhost.local:9443/dex
```

### Behind a Reverse Proxy

When a reverse proxy terminates TLS, the listen address and external URLs differ:

```bash
./holos-console \
  --plain-http \
  --listen=:8080 \
  --origin=https://console.example.com \
  --issuer=https://console.example.com/dex
```

The proxy forwards to port 8080. The `--origin` and `--issuer` reflect the external URLs that clients use.

## Common Mistakes

### Missing /dex Suffix on Issuer

```bash
# Wrong: issuer must include /dex
./holos-console --issuer=https://localhost:8443

# Correct
./holos-console --issuer=https://localhost:8443/dex
```

### Mismatched Origin and Issuer Hosts

When setting these explicitly, the origin and issuer should share the same scheme and host (the issuer adds the `/dex` path):

```bash
# Wrong: different hosts
--origin=https://console.example.com --issuer=https://other.example.com/dex

# Correct: same host
--origin=https://console.example.com --issuer=https://console.example.com/dex
```

### Forgetting Certificates for New Hostnames

When changing hostnames, regenerate TLS certificates to match:

```bash
mkcert myhost.local
```
