# Plan: Fix Dex Issuer Port Mismatch

> **Status:** UNREVIEWED / UNAPPROVED
>
> This plan has not been reviewed. Do not implement until approved.

## Overview

When the server is started on a non-default port (e.g., `--listen :4443`), the OIDC sign-in flow fails because:

1. The `--issuer` flag defaults to `https://localhost:8443/dex` regardless of the listen address
2. The frontend derives its OIDC authority from `window.location.origin`, which doesn't account for explicit `--issuer` values or external identity providers

This plan fixes both the backend default and ensures the frontend uses the backend's configured issuer value.

## Problem Analysis

### Current Behavior

1. **CLI default** ([cli/cli.go:56](cli/cli.go#L56)): `--issuer` defaults to `https://localhost:8443/dex`
2. **Frontend OIDC config** ([ui/src/auth/config.ts:29-31](ui/src/auth/config.ts#L29-L31)): Uses `window.location.origin` to derive authority:
   ```typescript
   const origin = window.location.origin
   return {
     authority: `${origin}/dex`,
     ...
   }
   ```
3. **Dex issuer** ([console/oidc/oidc.go:84-86](console/oidc/oidc.go#L84-L86)): Uses the `--issuer` value verbatim

### Why window.location.origin Is Insufficient

The frontend's use of `window.location.origin` fails in these scenarios:

1. **Non-default port**: Running `--listen :4443` but issuer defaults to port 8443
2. **Reverse proxy**: External URL is `https://console.example.com` but server listens on `:8443`
3. **External IDP**: Using `--issuer https://auth.example.com/dex` to point to an external identity provider

The frontend must use the actual issuer value configured on the backend, not derive it from the browser's URL.

### Failure Scenario

```bash
./bin/holos-console --cert certs/tls.crt --key certs/tls.key --listen :4443
```

- Server listens on port 4443
- Dex issuer defaults to `https://localhost:8443/dex` (wrong!)
- User opens `https://localhost:4443/ui/profile`
- Frontend calculates authority as `https://localhost:4443/dex`
- Mismatch between frontend authority and Dex issuer causes OIDC failures

## Goal

1. Backend: Derive `--issuer` from `--listen` address when not explicitly specified
2. Frontend: Use the backend's configured issuer value via server-injected config
3. Dev mode: Vite injects config based on its proxy target, preserving HMR

## Design Decisions

| Topic | Decision | Rationale |
| ----- | -------- | --------- |
| Config injection | `<script>` tag before `</head>` | Sets `window.__OIDC_CONFIG__` before app loads; no async fetch needed |
| Default issuer | Derive from `--listen` | Sensible default for local development |
| Explicit issuer | User-provided value wins | Required for reverse proxy and external IDP deployments |
| Redirect URIs | Derive from issuer | Replace `/dex` with `/ui/callback` for consistency |
| Vite dev mode | Plugin injects config from `backendUrl` | Preserves HMR; Vite controls HTML, not backend |
| Port parsing | Use `net.SplitHostPort` | Handle various listen formats (`:8443`, `0.0.0.0:8443`, `localhost:8443`) |

## Changes Required

### Modify (Existing Files)

- [cli/cli.go](cli/cli.go) - Remove default from `--issuer`, add derivation logic
- [console/console.go](console/console.go) - Inject OIDC config into index.html when serving
- [ui/vite.config.ts](ui/vite.config.ts) - Add plugin to inject OIDC config in dev mode
- [ui/src/auth/config.ts](ui/src/auth/config.ts) - Simplify to always use injected config (remove origin fallback)

### Add (New Files)

None required.

## Implementation

### Phase 1: Backend - Derive Issuer from Listen Address

#### 1.1 Remove hardcoded default from --issuer flag

Change the `--issuer` flag definition to have an empty default:

```go
cmd.Flags().StringVar(&issuer, "issuer", "",
    "OIDC issuer URL (defaults to https://localhost:<port>/dex based on --listen)")
```

#### 1.2 Add issuer derivation logic

Add a function to derive the issuer URL from the listen address:

```go
// deriveIssuer returns the issuer URL based on the listen address.
// If issuer is already set, returns it unchanged.
// Otherwise, derives from listen address using https and /dex path.
func deriveIssuer(listenAddr, issuer string) string {
    if issuer != "" {
        return issuer
    }

    // Parse listen address to extract host and port
    host, port, err := net.SplitHostPort(listenAddr)
    if err != nil {
        // Fallback if parsing fails
        return "https://localhost:8443/dex"
    }

    // Use localhost if host is empty or 0.0.0.0
    if host == "" || host == "0.0.0.0" {
        host = "localhost"
    }

    return fmt.Sprintf("https://%s:%s/dex", host, port)
}
```

#### 1.3 Apply derivation in Run function

Update the `Run` function to derive the issuer before creating config.

### Phase 2: Backend - Inject OIDC Config into index.html

#### 2.1 Add OIDCConfig struct and JSON generation

Add to `console/console.go`:

```go
// OIDCConfig is the OIDC configuration injected into the frontend.
type OIDCConfig struct {
    Authority             string `json:"authority"`
    ClientID              string `json:"client_id"`
    RedirectURI           string `json:"redirect_uri"`
    PostLogoutRedirectURI string `json:"post_logout_redirect_uri"`
}

// deriveRedirectURI derives the redirect URI from the issuer URL.
// Replaces /dex suffix with /ui/callback.
func deriveRedirectURI(issuer string) string {
    base := strings.TrimSuffix(issuer, "/dex")
    return base + "/ui/callback"
}

// derivePostLogoutRedirectURI derives the post-logout redirect URI from the issuer URL.
func derivePostLogoutRedirectURI(issuer string) string {
    base := strings.TrimSuffix(issuer, "/dex")
    return base + "/ui"
}
```

#### 2.2 Modify uiHandler to inject config

Update the `uiHandler` struct to hold the OIDC config and inject it when serving index.html:

```go
type uiHandler struct {
    fs         fs.FS
    oidcConfig *OIDCConfig
}

func newUIHandler(uiContent fs.FS, oidcConfig *OIDCConfig) http.Handler {
    return &uiHandler{fs: uiContent, oidcConfig: oidcConfig}
}

func (h *uiHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
    // Read index.html
    data, err := fs.ReadFile(h.fs, "index.html")
    if err != nil {
        http.NotFound(w, r)
        return
    }

    // Inject OIDC config if available
    if h.oidcConfig != nil {
        configJSON, err := json.Marshal(h.oidcConfig)
        if err == nil {
            script := fmt.Sprintf(`<script>window.__OIDC_CONFIG__=%s;</script>`, configJSON)
            // Insert before </head>
            data = bytes.Replace(data, []byte("</head>"), []byte(script+"</head>"), 1)
        }
    }

    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.Write(data)
}
```

#### 2.3 Wire OIDC config in Serve method

Update the `Serve` method to create and pass the OIDC config:

```go
// Create OIDC config for frontend injection
oidcConfig := &OIDCConfig{
    Authority:             s.cfg.Issuer,
    ClientID:              s.cfg.ClientID,
    RedirectURI:           deriveRedirectURI(s.cfg.Issuer),
    PostLogoutRedirectURI: derivePostLogoutRedirectURI(s.cfg.Issuer),
}

uiHandler := newUIHandler(uiContent, oidcConfig)
```

### Phase 3: Frontend - Simplify Config to Use Injected Values

#### 3.1 Update config.ts to require injected config

Update [ui/src/auth/config.ts](ui/src/auth/config.ts) to require the injected config and remove the `window.location.origin` fallback:

```typescript
function getConfig(): OIDCConfig {
  // Config must be injected by server (production) or Vite plugin (development)
  if (window.__OIDC_CONFIG__) {
    return window.__OIDC_CONFIG__
  }

  // Fallback for edge cases (should not happen in normal operation)
  console.warn('OIDC config not injected, using origin-based fallback')
  const origin = window.location.origin
  return {
    authority: `${origin}/dex`,
    client_id: 'holos-console',
    redirect_uri: `${origin}/ui/callback`,
    post_logout_redirect_uri: `${origin}/ui`,
  }
}
```

### Phase 4: Vite Dev Server - Inject Config via Plugin

#### 4.1 Create Vite plugin to inject OIDC config

Update [ui/vite.config.ts](ui/vite.config.ts) to add a plugin that injects the OIDC config in development mode:

```typescript
const backendUrl = 'https://localhost:8443'

// Derive OIDC config from backend URL
const oidcConfig = {
  authority: `${backendUrl}/dex`,
  client_id: 'holos-console',
  redirect_uri: 'https://localhost:5173/ui/callback',  // Vite dev server
  post_logout_redirect_uri: 'https://localhost:5173/ui',
}

const injectOIDCConfig = (): Plugin => ({
  name: 'inject-oidc-config',
  transformIndexHtml(html) {
    const script = `<script>window.__OIDC_CONFIG__=${JSON.stringify(oidcConfig)};</script>`
    return html.replace('</head>', `${script}</head>`)
  },
})

export default defineConfig({
  plugins: [injectOIDCConfig(), uiCanonicalRedirect(), react()],
  // ... rest of config
})
```

This approach:
- Derives the OIDC authority from `backendUrl` (same variable used for proxy config)
- Uses Vite's dev server URL for redirect URIs (port 5173)
- Preserves HMR because Vite controls the HTML transformation
- Keeps the backend proxy working for `/dex` requests

#### 4.2 Update backend redirect URIs to include Vite dev server

The backend already includes the Vite dev server redirect URI in the allowed list ([console/console.go:110-114](console/console.go#L110-L114)). No changes needed.

### Phase 5: Testing

#### 5.1 Add unit tests for deriveIssuer

Add test cases in `cli/cli_test.go`:

```go
func TestDeriveIssuer(t *testing.T) {
    tests := []struct {
        name       string
        listenAddr string
        issuer     string
        want       string
    }{
        {
            name:       "explicit issuer takes precedence",
            listenAddr: ":8443",
            issuer:     "https://console.example.com/dex",
            want:       "https://console.example.com/dex",
        },
        {
            name:       "derive from port-only listen",
            listenAddr: ":4443",
            issuer:     "",
            want:       "https://localhost:4443/dex",
        },
        {
            name:       "derive from full listen address",
            listenAddr: "localhost:9000",
            issuer:     "",
            want:       "https://localhost:9000/dex",
        },
        {
            name:       "0.0.0.0 becomes localhost",
            listenAddr: "0.0.0.0:8443",
            issuer:     "",
            want:       "https://localhost:8443/dex",
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := deriveIssuer(tt.listenAddr, tt.issuer)
            if got != tt.want {
                t.Errorf("deriveIssuer() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

#### 5.2 Manual E2E verification - non-default port

```bash
# Build and run on non-default port
make build
./bin/holos-console --cert certs/tls.crt --key certs/tls.key --listen :4443

# Verify log shows correct issuer
# Should see: "issuer": "https://localhost:4443/dex"

# Open https://localhost:4443/ui/profile
# View page source - should see injected __OIDC_CONFIG__ with port 4443
# Sign in should work correctly
```

#### 5.3 Manual E2E verification - Vite dev mode

```bash
# Terminal 1: Start backend on default port
make run

# Terminal 2: Start Vite dev server
cd ui && npm run dev

# Open https://localhost:5173/ui/profile
# View page source - should see injected __OIDC_CONFIG__
# Sign in should work (Vite proxies /dex to backend)
```

---

## TODO (Implementation Checklist)

### Phase 1: Backend - Derive Issuer from Listen Address
- [x] 1.1: Remove hardcoded default from --issuer flag
- [x] 1.2: Add deriveIssuer function to cli/cli.go
- [x] 1.3: Apply derivation in Run function

### Phase 2: Backend - Inject OIDC Config into index.html
- [x] 2.1: Add OIDCConfig struct and helper functions
- [x] 2.2: Modify uiHandler to inject config into index.html
- [x] 2.3: Wire OIDC config creation in Serve method

### Phase 3: Frontend - Simplify Config
- [x] 3.1: Update config.ts to expect injected config (keep fallback with warning)

### Phase 4: Vite Dev Server - Inject Config
- [x] 4.1: Add injectOIDCConfig plugin to vite.config.ts

### Phase 5: Testing
- [x] 5.1: Add unit tests for deriveIssuer
- [ ] 5.2: Manual E2E verification - non-default port
- [ ] 5.3: Manual E2E verification - Vite dev mode

---

## Testing

After implementation, verify with:

```bash
# Build
make build

# Test 1: Non-default port (production mode)
./bin/holos-console --cert certs/tls.crt --key certs/tls.key --listen :4443
# Open https://localhost:4443/ui/profile
# Sign in should work

# Test 2: Explicit issuer
./bin/holos-console --cert certs/tls.crt --key certs/tls.key --listen :4443 --issuer https://localhost:4443/dex
# Should behave same as Test 1

# Test 3: Vite dev mode
make run  # Backend on :8443
cd ui && npm run dev  # Vite on :5173
# Open https://localhost:5173/ui/profile
# Sign in should work (proxied through Vite)
```

Run unit tests:
```bash
make test
```

## Security Considerations

- OIDC config is not sensitive (public client, no secrets)
- Injected config is derived from server-side values, not user input
- Token validation continues to use the configured issuer URL
- No XSS risk - config is JSON-encoded and injected as literal object

## Alternatives Considered

### Alternative 1: Fetch config from API endpoint

Add a `/api/config` endpoint that returns OIDC config, frontend fetches on startup.

**Rejected:** Adds latency to app startup. Script injection is synchronous and simpler.

### Alternative 2: Environment variables for Vite

Use `VITE_OIDC_AUTHORITY` env var to configure Vite.

**Rejected:** Requires coordinating env vars between Go and Vite. Deriving from `backendUrl` is more maintainable since it's already used for proxy config.

### Alternative 3: Keep window.location.origin fallback as primary

Only inject config for explicit `--issuer` values.

**Rejected:** Doesn't solve the non-default port case. The injected config approach is more robust and handles all scenarios consistently.
