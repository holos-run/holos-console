# Plan: PKCE OIDC Authentication System

## Goal

Implement a complete OIDC PKCE authentication system for holos-console with:
- An embedded development/test OIDC provider in the Go backend
- JWT validation for protected ConnectRPC endpoints
- React SPA authentication using oidc-client-ts
- Comprehensive testing at unit, integration, and E2E levels

## Design Decisions

Based on research and critique analysis, the following decisions apply:

| Topic | Decision | Rationale |
|-------|----------|-----------|
| Library | `zitadel/oidc` | OpenID Foundation certified, actively maintained, production-proven |
| Dev/Prod boundary | Build tags (`//go:build dev`) | Compile-time exclusion prevents accidental production exposure |
| JWT validation | `coreos/go-oidc/v3` + custom interceptor | Well-maintained, ~20 lines of code, full control over security path |
| Test tokens | Real IdP in tests | Use actual dev IdP for integration tests; avoid coupling to key implementation details |
| E2E login | Playwright global setup + programmatic endpoint | Fast, reliable; `/dev/login` endpoint for speed when testing non-auth flows |
| Config injection | Script tag injection | Synchronous, no network round-trip, keeps index.html as static file in dev |
| Token storage | Memory + httpOnly cookie session | Memory for access token (XSS-safe), session cookie for refresh |

## Phase 1: Embedded OIDC Provider (Dev Only)

### 1.1 Add zitadel/oidc dependency

Add the zitadel/oidc library to go.mod:

```bash
go get github.com/zitadel/oidc/v3
```

### 1.2 Create dev-only IdP package

Create `internal/devoidc/` package with build tag:

```go
//go:build dev
```

Files to create:
- `internal/devoidc/provider.go` - OIDC provider setup with zitadel/oidc
- `internal/devoidc/storage.go` - In-memory storage for clients, users, tokens
- `internal/devoidc/login.go` - Simple HTML login form handler
- `internal/devoidc/users.go` - Static test users (testuser/testpass)

Key configuration:
- Issuer URL: `https://localhost:8443` (same as server)
- Client ID: `holos-console` (public client, no secret)
- Redirect URI: `https://localhost:8443/ui/callback`
- Scopes: `openid`, `profile`, `email`
- PKCE: Required (S256)

### 1.3 Wire provider routes (dev build only)

Create `console/devmode.go` with build tag:

```go
//go:build dev
```

Mount OIDC endpoints:
- `/.well-known/openid-configuration` - Discovery
- `/authorize` - Authorization endpoint
- `/token` - Token endpoint
- `/jwks` - JSON Web Key Set
- `/userinfo` - User info endpoint
- `/end_session` - Logout endpoint
- `/dev/login` - Programmatic login for tests (returns token directly)

### 1.4 Create production stub

Create `console/devmode_prod.go`:

```go
//go:build !dev
```

This file provides no-op implementations so production builds compile without the dev IdP code.

### 1.5 Update Makefile

Add build targets:
- `make build` - Production build (no dev tag)
- `make build-dev` - Dev build with embedded IdP
- `make run-dev` - Run dev build with certificates

## Phase 2: JWT Validation Interceptor

### 2.1 Add coreos/go-oidc dependency

```bash
go get github.com/coreos/go-oidc/v3
```

### 2.2 Create auth interceptor

Create `console/rpc/auth.go`:

```go
func AuthInterceptor(verifier *oidc.IDTokenVerifier) connect.UnaryInterceptorFunc {
    return func(next connect.UnaryFunc) connect.UnaryFunc {
        return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
            token := extractBearerToken(req.Header())
            if token == "" {
                return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
            }
            idToken, err := verifier.Verify(ctx, token)
            if err != nil {
                return nil, connect.NewError(connect.CodeUnauthenticated, err)
            }
            var claims Claims
            if err := idToken.Claims(&claims); err != nil {
                return nil, connect.NewError(connect.CodeInternal, err)
            }
            ctx = ContextWithClaims(ctx, &claims)
            return next(ctx, req)
        }
    }
}
```

### 2.3 Create claims context helpers

Create `console/rpc/claims.go`:
- `Claims` struct with `Sub`, `Email`, `Name`, `Groups`
- `ContextWithClaims(ctx, claims)` - Store claims in context
- `ClaimsFromContext(ctx)` - Retrieve claims from context

### 2.4 Configure OIDC verifier

Create `console/auth.go`:
- Initialize `oidc.Provider` from issuer URL (config-driven)
- Create `oidc.IDTokenVerifier` with expected audience
- Support both dev IdP and external IdP based on config

Configuration via environment/flags:
- `OIDC_ISSUER` - Issuer URL (e.g., `https://localhost:8443` for dev)
- `OIDC_CLIENT_ID` - Expected audience (e.g., `holos-console`)

### 2.5 Apply interceptor to protected routes

Update `console/console.go`:
- Create separate muxes for public vs protected routes
- Apply auth interceptor to protected RPC handlers
- Keep VersionService public for now (or make it protected as example)

## Phase 3: React Authentication

### 3.1 Add oidc-client-ts dependency

```bash
cd ui && npm install oidc-client-ts
```

### 3.2 Create auth context

Create `ui/src/auth/AuthProvider.tsx`:
- Initialize `UserManager` with OIDC config
- Provide auth state via React Context
- Handle login, logout, token refresh
- Store access token in memory (not localStorage)

Create `ui/src/auth/useAuth.ts`:
- `useAuth()` hook returning `{ user, isAuthenticated, isLoading, login, logout }`

### 3.3 Implement config injection

Update Go server to inject OIDC config:

```go
func serveIndex(w http.ResponseWriter, r *http.Request) {
    config := fmt.Sprintf(`<script>window.__OIDC_CONFIG__=%s</script>`,
        mustMarshalJSON(map[string]string{
            "issuer":      cfg.OIDCIssuer,
            "clientId":    cfg.OIDCClientID,
            "redirectUri": cfg.OIDCRedirectURI,
        }))
    html := strings.Replace(staticIndex, "</head>", config+"</head>", 1)
    w.Header().Set("Content-Type", "text/html")
    w.Write([]byte(html))
}
```

Create `ui/src/auth/config.ts`:
- Read `window.__OIDC_CONFIG__` if available
- Fall back to Vite env vars for dev server
- Export typed config object

### 3.4 Create callback route

Create `ui/src/pages/Callback.tsx`:
- Handle OIDC redirect callback
- Process authorization code
- Redirect to original destination

Add route in `ui/src/App.tsx`:
```tsx
<Route path="/callback" element={<Callback />} />
```

### 3.5 Create protected route wrapper

Create `ui/src/auth/ProtectedRoute.tsx`:
- Check authentication state
- Redirect to login if not authenticated
- Render children if authenticated

### 3.6 Update transport with auth header

Update `ui/src/client.ts`:
- Accept auth token from context/hook
- Attach `Authorization: Bearer <token>` header to requests

## Phase 4: Token Lifecycle

### 4.1 Implement silent refresh

Update `AuthProvider.tsx`:
- Configure `UserManager` with `automaticSilentRenew: true`
- Handle token expiration events
- Implement refresh token flow

### 4.2 Add logout support

Implement logout flow:
- Clear local auth state
- Call `/end_session` endpoint on IdP
- Redirect to home/login page

### 4.3 Handle CORS in development

Update Vite proxy config to handle OIDC endpoints:
- Proxy `/authorize`, `/token`, `/jwks`, etc. to Go backend
- Ensure cookies pass through correctly

## Phase 5: Backend Testing

### 5.1 Unit tests for auth interceptor

Create `console/rpc/auth_test.go`:
- Test missing token returns `Unauthenticated`
- Test invalid token returns `Unauthenticated`
- Test expired token returns `Unauthenticated`
- Test valid token passes through with claims in context

### 5.2 Integration tests with real dev IdP

Create `console/auth_integration_test.go` (build tag: `dev`):
- Start server with embedded IdP
- Obtain token via `/dev/login` endpoint
- Make authenticated RPC call
- Verify claims in response

### 5.3 Test the dev IdP endpoints

Create `internal/devoidc/provider_test.go`:
- Test discovery endpoint returns valid config
- Test JWKS endpoint returns valid keys
- Test authorize endpoint initiates PKCE flow
- Test token endpoint exchanges code for tokens
- Test `/dev/login` returns valid token

## Phase 6: Frontend Testing

### 6.1 Unit tests for auth hooks

Create `ui/src/auth/__tests__/`:
- Test `useAuth` hook states (loading, authenticated, unauthenticated)
- Test `ProtectedRoute` redirects when unauthenticated
- Mock `UserManager` for isolation

### 6.2 Integration tests for protected components

Create tests that:
- Mock MSW handlers for authenticated API calls
- Verify auth header is attached to requests
- Test error handling for 401 responses

### 6.3 E2E tests with Playwright

Create `ui/e2e/auth.spec.ts`:
- Global setup: Login via browser automation, save storage state
- Test protected page access with saved auth state
- Test logout flow
- Test token refresh (if feasible)

Playwright global setup (`ui/e2e/global-setup.ts`):
```typescript
async function globalSetup() {
  const browser = await chromium.launch();
  const page = await browser.newPage();

  // Navigate to protected page (triggers redirect to login)
  await page.goto('https://localhost:8443/ui/');

  // Fill login form on dev IdP
  await page.fill('input[name="username"]', 'testuser');
  await page.fill('input[name="password"]', 'testpass');
  await page.click('button[type="submit"]');

  // Wait for redirect back to app
  await page.waitForURL('https://localhost:8443/ui/**');

  // Save auth state
  await page.context().storageState({ path: 'e2e/.auth/state.json' });
  await browser.close();
}
```

## Phase 7: Production Configuration

### 7.1 Document external IdP setup

Create `docs/production-auth.md`:
- Required environment variables
- Supported IdP configurations (Dex, Auth0, Okta, etc.)
- Client registration requirements

### 7.2 Add configuration validation

Startup validation:
- Verify OIDC issuer is reachable
- Verify JWKS can be fetched
- Fail fast with clear error messages

### 7.3 Health check considerations

Update `/healthz` or similar:
- Include IdP connectivity status
- Report token verification capability

## Implementation Order

1. **Phase 1** (Embedded IdP): Foundation for all other phases
2. **Phase 2** (JWT Interceptor): Backend auth enforcement
3. **Phase 3** (React Auth): Frontend auth flow
4. **Phase 4** (Token Lifecycle): Refresh and logout
5. **Phase 5** (Backend Tests): Verify backend auth
6. **Phase 6** (Frontend Tests): Verify frontend auth
7. **Phase 7** (Production Config): Production readiness

## Files to Create

### Go Backend
- `internal/devoidc/provider.go` (dev build)
- `internal/devoidc/storage.go` (dev build)
- `internal/devoidc/login.go` (dev build)
- `internal/devoidc/users.go` (dev build)
- `internal/devoidc/provider_test.go` (dev build)
- `console/devmode.go` (dev build)
- `console/devmode_prod.go` (prod build)
- `console/rpc/auth.go`
- `console/rpc/claims.go`
- `console/rpc/auth_test.go`
- `console/auth.go`
- `console/auth_integration_test.go` (dev build)

### React Frontend
- `ui/src/auth/AuthProvider.tsx`
- `ui/src/auth/useAuth.ts`
- `ui/src/auth/config.ts`
- `ui/src/auth/ProtectedRoute.tsx`
- `ui/src/auth/__tests__/useAuth.test.ts`
- `ui/src/auth/__tests__/ProtectedRoute.test.tsx`
- `ui/src/pages/Callback.tsx`
- `ui/e2e/auth.spec.ts`
- `ui/e2e/global-setup.ts`

### Documentation
- `docs/production-auth.md`

## Dependencies to Add

### Go
- `github.com/zitadel/oidc/v3`
- `github.com/coreos/go-oidc/v3`

### Node
- `oidc-client-ts`
- `@playwright/test` (if not already present)

## TODO (implementation)

- [ ] Phase 1.1: Add zitadel/oidc dependency
- [ ] Phase 1.2: Create internal/devoidc package with provider, storage, login, users
- [ ] Phase 1.3: Create console/devmode.go to wire OIDC routes
- [ ] Phase 1.4: Create console/devmode_prod.go stub
- [ ] Phase 1.5: Update Makefile with build-dev and run-dev targets
- [ ] Phase 2.1: Add coreos/go-oidc dependency
- [ ] Phase 2.2: Create console/rpc/auth.go interceptor
- [ ] Phase 2.3: Create console/rpc/claims.go context helpers
- [ ] Phase 2.4: Create console/auth.go verifier setup
- [ ] Phase 2.5: Apply interceptor to protected routes
- [ ] Phase 3.1: Add oidc-client-ts dependency
- [ ] Phase 3.2: Create ui/src/auth/ with AuthProvider and useAuth
- [ ] Phase 3.3: Implement config injection in Go server
- [ ] Phase 3.4: Create Callback route component
- [ ] Phase 3.5: Create ProtectedRoute wrapper
- [ ] Phase 3.6: Update transport with auth header
- [ ] Phase 4.1: Implement silent token refresh
- [ ] Phase 4.2: Implement logout flow
- [ ] Phase 4.3: Configure CORS for development
- [ ] Phase 5.1: Write auth interceptor unit tests
- [ ] Phase 5.2: Write auth integration tests
- [ ] Phase 5.3: Write dev IdP endpoint tests
- [ ] Phase 6.1: Write useAuth hook tests
- [ ] Phase 6.2: Write protected component integration tests
- [ ] Phase 6.3: Write Playwright E2E auth tests
- [ ] Phase 7.1: Document external IdP setup
- [ ] Phase 7.2: Add configuration validation
- [ ] Phase 7.3: Update health checks
