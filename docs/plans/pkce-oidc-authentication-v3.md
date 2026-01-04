# Plan: PKCE OIDC Authentication System (v3)

> **Status:** UNREVIEWED / UNAPPROVED
>
> This plan has not been reviewed or approved. Do not implement until approved.

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
| Default password | `HOLOS_DEX_PASSWORD` env var, default `verysecret` | Simple override mechanism; const in single location |
| Auth configuration | `--issuer` flag configures JWT validation only | Decouples auth validation from embedded IDP |
| JWT validation | `coreos/go-oidc/v3` + custom interceptor | Already implemented in current codebase |

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

1. **Embedded Dex always runs** at `/dex/*` endpoints
2. **`--issuer` flag** controls which OIDC provider validates tokens:
   - Default: `https://localhost:8443/dex` (embedded Dex)
   - Override: Any external OIDC issuer URL
3. **Users can ignore embedded Dex** by pointing `--issuer` to their own IDP

## Embedded Dex Configuration

### Default User Credentials

A single const defines the default password, overridable via environment:

```go
// console/dex/config.go

const (
    // DefaultPassword is the password for the embedded Dex mock connector.
    // Override via HOLOS_DEX_PASSWORD environment variable.
    DefaultPassword = "verysecret"

    // DefaultUsername is the username for the embedded Dex mock connector.
    // Override via HOLOS_DEX_USERNAME environment variable.
    DefaultUsername = "admin"
)

func GetPassword() string {
    if p := os.Getenv("HOLOS_DEX_PASSWORD"); p != "" {
        return p
    }
    return DefaultPassword
}

func GetUsername() string {
    if u := os.Getenv("HOLOS_DEX_USERNAME"); u != "" {
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

// 4. Create Dex server
dexServer, _ := server.NewServer(ctx, server.Config{
    Issuer:             "https://localhost:8443/dex",
    Storage:            store,
    SkipApprovalScreen: true,
    Logger:             logger,
    SupportedResponseTypes: []string{"code"},
    AllowedOrigins:     []string{"https://localhost:8443"},
})

// 5. Mount at /dex/
mux.Handle("/dex/", http.StripPrefix("/dex", dexServer))
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
HOLOS_DEX_PASSWORD=mysecret ./holos-console --cert-file=... --key-file=...

# Production: Use external IDP (embedded Dex still runs but is ignored)
./holos-console --issuer=https://dex.example.com --client-id=holos-console
```

## File Structure

```
console/
├── dex/
│   ├── dex.go          # Dex server initialization
│   ├── config.go       # Default credentials, env var handling
│   └── dex_test.go     # Dex integration tests
├── auth.go             # OIDC verifier (existing, unchanged)
├── console.go          # Wire Dex routes
└── rpc/
    ├── auth.go         # JWT interceptor (existing, unchanged)
    └── claims.go       # Claims context (existing, unchanged)
```

## Changes from Current Implementation

### Remove
- `internal/devoidc/` - Custom zitadel/oidc provider (replaced by Dex)
- `console/devmode.go` - Build-tag dev routes
- `console/devmode_prod.go` - Build-tag prod stubs
- Build tags for dev/prod separation

### Keep (Unchanged)
- `console/auth.go` - OIDC verifier setup
- `console/rpc/auth.go` - JWT interceptor
- `console/rpc/claims.go` - Claims context helpers

### Add
- `console/dex/` - Embedded Dex package

## Dependencies

### Add
```
github.com/dexidp/dex v2.41.1  # Or latest stable
```

### Keep
```
github.com/coreos/go-oidc/v3   # Already in use for JWT validation
```

### Remove
```
github.com/zitadel/oidc/v3     # Replaced by embedded Dex
```

## Phase 1: Embedded Dex Setup

### 1.1 Add Dex dependency

```bash
go get github.com/dexidp/dex@latest
```

### 1.2 Create console/dex package

Create `console/dex/config.go`:
- `DefaultPassword`, `DefaultUsername` constants
- `GetPassword()`, `GetUsername()` functions with env var override

Create `console/dex/dex.go`:
- `NewServer(ctx, issuer, clientRedirectURIs, logger)` factory
- Configure in-memory storage
- Add static client for holos-console
- Add mock password connector
- Return `http.Handler`

### 1.3 Wire Dex routes in console

Update `console/console.go`:
- Initialize Dex server
- Mount at `/dex/` path
- Dex handles its own discovery at `/dex/.well-known/openid-configuration`

### 1.4 Remove old devoidc implementation

Delete:
- `internal/devoidc/` directory
- `console/devmode.go`
- `console/devmode_prod.go`

Update:
- Remove build tags from Makefile targets
- `make build` and `make run` work identically

### 1.5 Update default issuer

Update CLI:
- Default `--issuer` to embedded Dex URL
- Document that embedded Dex always runs

## Phase 2: Verify Existing Auth Works

The current auth implementation should work unchanged:
- `console/auth.go` creates verifier from `--issuer`
- `console/rpc/auth.go` validates tokens
- Only the issuer URL changes (now points to embedded Dex by default)

### 2.1 Integration test

Test that:
- Embedded Dex serves discovery document
- Token obtained from Dex validates correctly
- Protected RPC endpoints accept Dex-issued tokens

## Phase 3: React SPA Integration

### 3.1 Update OIDC config

Frontend config points to embedded Dex:
- Issuer: `/dex` (relative, resolved to same host)
- Client ID: `holos-console`
- Redirect URI: `/ui/callback`

### 3.2 Vite proxy configuration

Ensure Vite dev server proxies `/dex/*` to Go backend.

## Phase 4: Testing

### 4.1 Dex package tests

Test `console/dex/`:
- Server initialization
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

---

## TODO (Implementation Checklist)

### Phase 1: Embedded Dex Setup
- [ ] 1.1: Add `github.com/dexidp/dex` dependency
- [ ] 1.2a: Create `console/dex/config.go` with default credentials
- [ ] 1.2b: Create `console/dex/dex.go` with Dex server factory
- [ ] 1.3: Wire Dex routes in `console/console.go`
- [ ] 1.4a: Delete `internal/devoidc/` directory
- [ ] 1.4b: Delete `console/devmode.go` and `console/devmode_prod.go`
- [ ] 1.4c: Remove build tags from Makefile (`build-dev`, `run-dev` → just `build`, `run`)
- [ ] 1.5: Update `--issuer` default to embedded Dex URL

### Phase 2: Verify Auth Integration
- [ ] 2.1a: Write integration test for Dex discovery endpoint
- [ ] 2.1b: Write integration test for token validation with Dex-issued tokens
- [ ] 2.1c: Verify protected RPC endpoints work with Dex tokens

### Phase 3: React SPA Updates
- [ ] 3.1: Update frontend OIDC config to use `/dex` issuer
- [ ] 3.2: Update Vite proxy to forward `/dex/*` to backend

### Phase 4: Testing
- [ ] 4.1a: Write unit tests for `console/dex/config.go`
- [ ] 4.1b: Write integration tests for `console/dex/dex.go`
- [ ] 4.2: Write Playwright E2E tests for login flow

### Phase 5: Documentation
- [ ] 5.1: Update CONTRIBUTING.md with auth dev workflow
- [ ] 5.2: Create docs/authentication.md

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
