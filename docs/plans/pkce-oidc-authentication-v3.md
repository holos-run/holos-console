# Plan: PKCE OIDC Authentication System (v3)

> **Status:** APPROVED
>
> This plan has been reviewed, revised, and approved for implementation.

## Overview

This plan replaces v1/v2 approaches with an embedded Dex IDP strategy. Instead of:
- Build-tag separation between dev and prod (v1)
- Custom zitadel/oidc implementation (v1)
- Multiple operational modes

We now embed Dex directly into the holos-console executable for all builds. This provides:
- Battle-tested CNCF IDP with minimal maintenance burden
- Single binary deployment for both development and production
- User choice: use embedded Dex or configure an external issuer

## Design Decisions

| Topic | Decision | Rationale |
|-------|----------|-----------|
| IDP Library | Embedded Dex | CNCF project, battle-tested, minimal custom code to maintain |
| Build separation | None | Single binary for all environments; users choose IDP at runtime |
| Default password | `HOLOS_DEX_INITIAL_ADMIN_PASSWORD` env var, default `verysecret` | Simple override mechanism; const in single location |
| Auth configuration | `--issuer` flag configures JWT validation only | Decouples auth validation from embedded IDP |
| JWT validation | `coreos/go-oidc/v3` + custom interceptor | Well-maintained, minimal code to maintain |

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      holos-console binary                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────────┐      ┌──────────────────────────────────┐ │
│  │   Embedded Dex   │      │         Console Server           │ │
│  │                  │      │                                  │ │
│  │  /dex/*          │      │  /ui/*          (React SPA)      │ │
│  │  /.well-known/*  │      │  /api/*         (ConnectRPC)     │ │
│  │                  │      │  /metrics       (Prometheus)     │ │
│  │  Mock Password   │      │                                  │ │
│  │  Connector with  │      │  JWT Validation via              │ │
│  │  configurable    │      │  --issuer (any OIDC provider)    │ │
│  │  credentials     │      │                                  │ │
│  └──────────────────┘      └──────────────────────────────────┘ │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Key Behaviors

1. **Embedded Dex provided as `http.Handler`** - Users mount it at a configurable path prefix (default `/dex`)
2. **All Dex URLs under mount point** - Discovery at `/dex/.well-known/openid-configuration`, etc.
3. **`--issuer` flag** controls which OIDC provider validates tokens:
   - Default: `https://localhost:8443/dex` (embedded Dex)
   - Override: Any external OIDC issuer URL
4. **Users can ignore embedded Dex** by pointing `--issuer` to their own IDP

## Embedded Dex Configuration

### Default User Credentials

A single const defines the default password, overridable via environment:

```go
// console/oidc/config.go

const (
    // DefaultPassword is the password for the embedded OIDC identity provider.
    // Override via HOLOS_DEX_INITIAL_ADMIN_PASSWORD environment variable.
    DefaultPassword = "verysecret"

    // DefaultUsername is the username for the embedded OIDC identity provider.
    // Override via HOLOS_DEX_INITIAL_ADMIN_USERNAME environment variable.
    DefaultUsername = "admin"
)

func GetPassword() string {
    if p := os.Getenv("HOLOS_DEX_INITIAL_ADMIN_PASSWORD"); p != "" {
        return p
    }
    return DefaultPassword
}

func GetUsername() string {
    if u := os.Getenv("HOLOS_DEX_INITIAL_ADMIN_USERNAME"); u != "" {
        return u
    }
    return DefaultUsername
}
```

### Dex Library Integration

Dex provides a clean library API for embedding. The key types are:

```go
import (
    "github.com/dexidp/dex/server"
    "github.com/dexidp/dex/storage"
    "github.com/dexidp/dex/storage/memory"
    "github.com/dexidp/dex/connector/mock"
)
```

#### Minimal Embedding Pattern

```go
// 1. Create in-memory storage
store, _ := (&memory.Config{}).Open(logger)

// 2. Add static client for holos-console SPA
store = storage.WithStaticClients(store, []storage.Client{
    {
        ID:           "holos-console",
        RedirectURIs: []string{"https://localhost:8443/ui/callback"},
        Name:         "Holos Console",
        Public:       true, // SPA = public client, no secret
    },
})

// 3. Add mock password connector
connectorConfig, _ := json.Marshal(mock.PasswordConfig{
    Username: GetUsername(),
    Password: GetPassword(),
})
store = storage.WithStaticConnectors(store, []storage.Connector{
    {
        ID:     "mock",
        Type:   "mockPassword",
        Name:   "Development Login",
        Config: connectorConfig,
    },
})

// 4. Create Dex server - issuer URL includes the mount path
dexServer, _ := server.NewServer(ctx, server.Config{
    Issuer:             "https://localhost:8443/dex",  // Includes mount path
    Storage:            store,
    SkipApprovalScreen: true,
    Logger:             logger,
    SupportedResponseTypes: []string{"code"},
    AllowedOrigins:     []string{"https://localhost:8443"},
})

// 5. Mount at configurable path prefix (default /dex)
// The handler serves all Dex endpoints under this path:
//   /dex/.well-known/openid-configuration
//   /dex/auth
//   /dex/token
//   /dex/keys
//   /dex/userinfo
//   etc.
mux.Handle("/dex/", http.StripPrefix("/dex", dexServer))
```

#### Handler Integration API

The `console/oidc` package exposes a simple factory that returns an `http.Handler`:

```go
// NewHandler creates an http.Handler for the embedded OIDC identity provider.
// The issuer must include the full URL with the mount path (e.g., "https://localhost:8443/dex").
// The handler should be mounted at the path suffix of the issuer URL.
func NewHandler(ctx context.Context, cfg Config) (http.Handler, error)

type Config struct {
    // Issuer is the full OIDC issuer URL including mount path
    // Example: "https://localhost:8443/dex"
    Issuer string

    // ClientID is the OAuth2 client ID for the SPA
    ClientID string

    // RedirectURIs are the allowed OAuth2 redirect URIs
    RedirectURIs []string

    // Logger for operations
    Logger *slog.Logger
}
```

Usage:

```go
// Create OIDC identity provider handler
oidcHandler, err := oidc.NewHandler(ctx, oidc.Config{
    Issuer:       "https://localhost:8443/dex",
    ClientID:     "holos-console",
    RedirectURIs: []string{"https://localhost:8443/ui/callback"},
    Logger:       logger,
})
if err != nil {
    return err
}

// Mount at /dex/ - all OIDC provider URLs are under this path
mux.Handle("/dex/", http.StripPrefix("/dex", oidcHandler))
```

### Why Dex's Mock Password Connector?

The `mock.PasswordConfig` connector in `github.com/dexidp/dex/connector/mock`:
- Accepts a single username/password pair
- Returns a hardcoded identity (customizable)
- Perfect for development/testing
- Zero maintenance - it's Dex's own test connector

## CLI Flags

```
--issuer string     OIDC issuer URL for token validation (default "https://localhost:8443/dex")
--client-id string  Expected audience for tokens (default "holos-console")
--dex-listen string Address for embedded Dex (default ":8443", shares with main server)
```

### Usage Examples

```bash
# Development: Use embedded Dex with default password
./holos-console --cert-file=... --key-file=...

# Development: Use embedded Dex with custom password
HOLOS_DEX_INITIAL_ADMIN_PASSWORD=mysecret ./holos-console --cert-file=... --key-file=...

# Production: Use external IDP (embedded Dex still runs but is ignored)
./holos-console --issuer=https://dex.example.com --client-id=holos-console
```

## File Structure (After Implementation)

```
console/
├── oidc/
│   ├── oidc.go         # Embedded OIDC identity provider (Dex) initialization
│   ├── config.go       # Default credentials, env var handling
│   └── oidc_test.go    # OIDC provider integration tests
├── auth.go             # OIDC verifier setup (NEW)
├── console.go          # Wire OIDC provider routes (MODIFIED)
└── rpc/
    ├── auth.go         # JWT interceptor (NEW)
    ├── claims.go       # Claims context (NEW)
    ├── version.go      # (existing)
    ├── metrics.go      # (existing)
    └── logging.go      # (existing)
```

## Current Implementation State

The codebase currently has:
- `console/console.go` - HTTP server with UI and RPC handlers
- `console/rpc/version.go`, `metrics.go`, `logging.go` - Existing RPC infrastructure
- `cli/cli.go` - Cobra CLI with `--listen`, `--cert`, `--key` flags

The codebase does **not** have:
- Any authentication or OIDC code
- `--issuer` or `--client-id` flags
- JWT validation interceptors
- Embedded identity provider

## Changes Required

### Add (New Files)
- `console/oidc/config.go` - Default credentials, env var handling
- `console/oidc/oidc.go` - Embedded OIDC identity provider (Dex)
- `console/auth.go` - OIDC verifier setup
- `console/rpc/auth.go` - JWT interceptor
- `console/rpc/claims.go` - Claims context helpers

### Modify (Existing Files)
- `console/console.go` - Wire OIDC provider routes, apply auth interceptors
- `cli/cli.go` - Add `--issuer` and `--client-id` flags

## Dependencies

### Add
```
github.com/dexidp/dex v2.41.1    # Or latest stable - embedded OIDC provider
github.com/coreos/go-oidc/v3     # JWT validation
```

## Phase 1: Embedded OIDC Identity Provider

### 1.1 Add dependencies

```bash
go get github.com/dexidp/dex@latest
go get github.com/coreos/go-oidc/v3
```

### 1.2 Create console/oidc package

Create `console/oidc/config.go`:
- `DefaultPassword`, `DefaultUsername` constants
- `GetPassword()`, `GetUsername()` functions with env var override

Create `console/oidc/oidc.go`:
- `NewHandler(ctx, cfg Config) (http.Handler, error)` factory
- Configure in-memory storage
- Add static client for holos-console
- Add mock password connector
- Return `http.Handler`

### 1.3 Wire OIDC provider routes in console

Update `console/console.go`:
- Initialize OIDC provider (Dex)
- Mount at `/dex/` path (configurable)
- Provider handles its own discovery at `/dex/.well-known/openid-configuration`

### 1.4 Add CLI flags

Update `cli/cli.go`:
- Add `--issuer` flag (default: embedded OIDC provider URL)
- Add `--client-id` flag (default: `holos-console`)
- Pass these to `console.Config`

## Phase 2: JWT Validation

### 2.1 Create OIDC verifier

Create `console/auth.go`:
- `NewIDTokenVerifier(ctx, issuer, clientID)` factory
- Fetch OIDC discovery from issuer URL
- Return `*oidc.IDTokenVerifier`

### 2.2 Create auth interceptor

Create `console/rpc/auth.go`:
- `AuthInterceptor(verifier)` - Requires valid bearer token
- `OptionalAuthInterceptor(verifier)` - Validates if present, allows unauthenticated

Create `console/rpc/claims.go`:
- `Claims` struct with `Sub`, `Email`, `Name`, `Groups`
- `ContextWithClaims(ctx, claims)` - Store claims in context
- `ClaimsFromContext(ctx)` - Retrieve claims from context

### 2.3 Apply interceptor to routes

Update `console/console.go`:
- Create OIDC verifier when issuer is configured
- Apply auth interceptor to protected RPC handlers
- VersionService remains public (no auth required)

### 2.4 Integration tests

Test that:
- Embedded Dex serves discovery document
- Token obtained from Dex validates correctly
- Protected RPC endpoints accept Dex-issued tokens
- Unauthenticated requests are rejected

## Phase 3: React SPA Integration

### 3.1 Update OIDC config

Frontend config points to embedded Dex:
- Issuer: `/dex` (relative, resolved to same host)
- Client ID: `holos-console`
- Redirect URI: `/ui/callback`

### 3.2 Vite proxy configuration

Ensure Vite dev server proxies `/dex/*` to Go backend.

## Phase 4: Testing

### 4.1 OIDC package tests

Test `console/oidc/`:
- Handler initialization
- Discovery endpoint returns valid config
- Token endpoint exchanges codes
- Mock connector accepts configured credentials

### 4.2 E2E tests

Playwright tests:
- Login with default credentials (`admin` / `verysecret`)
- Login with env-overridden credentials
- Verify protected routes work after login

## Phase 5: Documentation

### 5.1 Update CONTRIBUTING.md

Document:
- Default credentials for development
- How to override password via env var
- How to use external IDP in production

### 5.2 Create docs/authentication.md

Document:
- Architecture overview
- Embedded Dex configuration
- External IDP configuration
- Security considerations

### 5.3 Create docs/hostname-configuration.md

Document how the hostname and port flow through the entire stack. Written for contributors wondering how to set the hostname in one place and have it propagate everywhere.

**Key concept:** The `--issuer` flag is the canonical source of truth for the external URL.

Document the flow:

1. **CLI Entry Point** (`cli/cli.go`)
   - `--issuer` flag (e.g., `https://console.example.com/dex`)
   - `--listen` flag for HTTP server bind address (e.g., `:8443`)
   - The issuer URL determines the external hostname; listen address is internal

2. **Console Server** (`console/console.go`)
   - Receives issuer URL from CLI
   - Parses issuer to extract base URL (scheme + host)
   - Passes issuer to OIDC provider
   - Uses base URL for CORS configuration

3. **Embedded OIDC Provider** (`console/oidc/oidc.go`)
   - Receives full issuer URL including mount path
   - Configures Dex with this issuer
   - All OIDC discovery documents use this issuer
   - Redirect URIs must match this hostname

4. **JWT Validation** (`console/auth.go`)
   - Fetches OIDC discovery from issuer URL
   - Validates tokens have matching issuer claim

5. **React SPA - Production** (`ui/src/`)
   - Config injected via `<script>` tag in index.html
   - Server injects `window.__OIDC_CONFIG__` with issuer derived from request
   - SPA reads this at runtime, no build-time hostname needed

6. **React SPA - Development** (`ui/vite.config.ts`)
   - Vite dev server runs on different port (e.g., 5173)
   - Proxy configuration forwards `/dex/*` to Go backend
   - OIDC config uses relative paths or Vite env vars

**Example: Changing the hostname**

To run holos-console on `https://myhost.local:9443`:

```bash
# Generate certs for the new hostname
mkcert myhost.local

# Start server with new issuer
./holos-console \
  --listen=:9443 \
  --cert-file=myhost.local.pem \
  --key-file=myhost.local-key.pem \
  --issuer=https://myhost.local:9443/dex
```

The issuer URL flows to:
- Dex discovery at `https://myhost.local:9443/dex/.well-known/openid-configuration`
- Token `iss` claim will be `https://myhost.local:9443/dex`
- SPA will use `https://myhost.local:9443/dex` for auth

**Key files to understand:**
- `cli/cli.go` - Flag definitions
- `console/console.go` - URL parsing and handler setup
- `console/oidc/oidc.go` - Dex configuration
- `ui/src/auth/config.ts` - Frontend config reading
- `ui/vite.config.ts` - Dev server proxy

---

## TODO (Implementation Checklist)

### Phase 1: Embedded OIDC Identity Provider
- [x] 1.1a: Add `github.com/dexidp/dex` dependency
- [x] 1.1b: Add `github.com/coreos/go-oidc/v3` dependency
- [x] 1.2a: Create `console/oidc/config.go` with default credentials
- [x] 1.2b: Create `console/oidc/oidc.go` with `NewHandler()` factory
- [x] 1.3: Wire OIDC provider routes in `console/console.go`
- [x] 1.4a: Add `--issuer` flag to `cli/cli.go`
- [x] 1.4b: Add `--client-id` flag to `cli/cli.go`

### Phase 2: JWT Validation
- [x] 2.1: Create `console/auth.go` with `NewIDTokenVerifier()`
- [x] 2.2a: Create `console/rpc/claims.go` with Claims struct and context helpers
- [ ] 2.2b: Create `console/rpc/auth.go` with `AuthInterceptor()`
- [ ] 2.3: Apply auth interceptor to protected routes in `console/console.go`
- [ ] 2.4a: Write integration test for OIDC discovery endpoint
- [ ] 2.4b: Write integration test for token validation
- [ ] 2.4c: Write integration test for protected RPC endpoints

### Phase 3: React SPA Updates
- [ ] 3.1: Add `oidc-client-ts` dependency
- [ ] 3.2: Create `ui/src/auth/` with AuthProvider, useAuth, config
- [ ] 3.3: Update Vite proxy to forward `/dex/*` to backend
- [ ] 3.4: Create callback route component

### Phase 4: Testing
- [ ] 4.1a: Write unit tests for `console/oidc/config.go`
- [ ] 4.1b: Write integration tests for `console/oidc/oidc.go`
- [ ] 4.2: Write Playwright E2E tests for login flow

### Phase 5: Documentation
- [ ] 5.1: Update CONTRIBUTING.md with authentication workflow
- [ ] 5.2: Create docs/authentication.md
- [ ] 5.3: Create docs/hostname-configuration.md (how hostname flows through stack)

---

## Appendix: Dex Library API Reference

### Key Types

```go
// server.Config - Main configuration
type Config struct {
    Issuer                 string
    Storage                storage.Storage
    SupportedResponseTypes []string        // Default: ["code"]
    AllowedGrantTypes      []string        // Default: all supported
    SkipApprovalScreen     bool            // Skip consent screen
    Logger                 *slog.Logger
    AllowedOrigins         []string        // CORS origins
    Web                    WebConfig       // UI assets (optional)
}

// storage.Client - OAuth2 client
type Client struct {
    ID           string
    Secret       string   // Empty for public clients
    RedirectURIs []string
    Name         string
    Public       bool     // true for SPAs
}

// storage.Connector - Identity connector
type Connector struct {
    ID     string
    Type   string // "mockPassword", "oidc", "ldap", etc.
    Name   string
    Config []byte // JSON-serialized connector config
}

// mock.PasswordConfig - Simple username/password connector
type PasswordConfig struct {
    Username string `json:"username"`
    Password string `json:"password"`
}
```

### Storage Decorators

```go
// Wrap storage with static (read-only) configuration
store = storage.WithStaticClients(store, clients)
store = storage.WithStaticConnectors(store, connectors)
store = storage.WithStaticPasswords(store, passwords, logger)
```

### Connector Types Available

| Type | Package | Use Case |
|------|---------|----------|
| `mockPassword` | `connector/mock` | Dev/test with fixed credentials |
| `mockCallback` | `connector/mock` | Auto-approve (no login UI) |
| `oidc` | `connector/oidc` | Upstream OIDC provider |
| `ldap` | `connector/ldap` | LDAP/AD directory |
| `github` | `connector/github` | GitHub OAuth |
| `google` | `connector/google` | Google OAuth |
| `saml` | `connector/saml` | SAML 2.0 |
