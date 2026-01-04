# Plan: Token TTL Configuration and Production BFF Mode

> **Status:** REVIEWED / APPROVED
>
> Ready for implementation.

## Related ADRs

- [ADR 002: BFF Architecture with oauth2-proxy](../adrs/002-bff-architecture-oauth2-proxy.md) - Production authentication architecture
- [ADR 003: Use sessionStorage for Local Development](../adrs/003-session-storage-for-development.md) - Development token storage decision

## Overview

This plan addresses authentication configuration for Holos Console:

1. **Token TTL configuration**: ID tokens and access tokens default to 24 hours, which is too long for production. Need configurable short-lived tokens (15 minutes default) with 12-hour refresh token maximum lifetime.
2. **Auth debugging page**: Add a debug page showing token expiration countdown, last refresh status, and manual refresh trigger.
3. **Production BFF mode**: Configure the frontend to work with oauth2-proxy as a BFF sidecar in production, where the proxy handles authentication via cookies.

### What This Plan Does NOT Do

Per ADR 003, this plan explicitly does **not** change the frontend token storage from sessionStorage to localStorage. The multi-tab authentication issue is solved in production by the BFF architecture (oauth2-proxy handles sessions via cookies), and the minor inconvenience in local development is accepted.

## Problem Analysis

### 1. Token TTL Too Long

**Current Behavior:**
- [console/oidc/oidc.go:84-91](console/oidc/oidc.go#L84-L91): Creates Dex server with default settings
- Dex default `IDTokensValidFor`: 24 hours
- No refresh token lifetime configuration

**Production requirements:**
- ID/Access tokens: 15 minutes (configurable down to seconds for testing)
- Refresh token absolute lifetime: 12 hours (forces daily re-authentication)

### 2. No Auth Debugging Visibility

**Current Behavior:**
- No visibility into token expiration time
- No indication when silent refresh occurs
- Debugging token issues requires browser dev tools

### 3. Production Deployment Needs BFF Mode

**Current Behavior:**
- Frontend uses oidc-client-ts to manage OIDC flow directly
- Tokens stored in sessionStorage (vulnerable to XSS)
- No support for oauth2-proxy sidecar pattern

**Production requirements:**
- Support oauth2-proxy as authentication proxy
- Frontend should detect when running behind oauth2-proxy
- Disable oidc-client-ts token management when BFF handles auth

## State of the Art: SPA Authentication in 2025

This section provides context for security review by summarizing current industry standards, IETF recommendations, and how comparable internal developer tools handle authentication.

### IETF Standards and OAuth 2.1

The IETF's [OAuth 2.0 for Browser-Based Applications](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-browser-based-apps) (draft-26, December 2025) and [RFC 9700: Best Current Practice for OAuth 2.0 Security](https://datatracker.ietf.org/doc/rfc9700/) (January 2025) establish the current standards:

1. **PKCE is mandatory**: OAuth 2.1 requires PKCE for all clients, not just public ones. The Implicit Flow is deprecated.

2. **Three recommended architectures** (ranked by security):
   - **Backend For Frontend (BFF)**: Most secure. Backend acts as confidential OAuth client, manages all tokens server-side, exposes only HttpOnly session cookie to browser.
   - **Token-Mediating Backend**: Backend obtains tokens as confidential client but passes access tokens to frontend.
   - **Browser-based OAuth Client**: Least secure. Frontend handles OAuth directly (our development approach).

3. **Token storage warnings**: The IETF explicitly warns that "localStorage does not protect against unauthorized access from malicious JavaScript" and that "none of these client-side storage solutions prevent attackers from obtaining fresh tokens by running new OAuth flows themselves."

### The BFF Pattern Recommendation

The IETF's current recommendation is to delegate all authentication logic to a server-side Backend-For-Frontend:

> "While implementing OAuth logic directly in the browser was once considered acceptable, this is no longer recommended. Storing any authentication state in the browser (such as access tokens) has proven to be inherently risky."

The BFF pattern works by:
- Backend acts as a confidential OAuth client (can hold client secrets)
- Backend manages access/refresh tokens in server-side session storage
- Frontend only receives an HttpOnly, Secure, SameSite cookie
- Backend proxies API requests, attaching tokens server-side

**Trade-offs:**
- Requires additional backend infrastructure and session management
- Cookie-based sessions introduce CSRF attack surface (requires anti-CSRF tokens)
- More complex deployment (session storage like Redis needed for horizontal scaling)
- Significantly more secure against XSS token theft

### What Comparable Tools Do

| Tool | Architecture | Token Storage | Multi-Tab Behavior |
|------|--------------|---------------|-------------------|
| **ArgoCD** | JWT tokens | Cookie (24h expiry) | Shared via cookie |
| **Backstage** | OAuth + BFF hybrid | Refresh token in HttpOnly cookie, access token in memory | Shared session via /refresh endpoint |
| **Kubernetes Dashboard** | OAuth2-Proxy (BFF) | Cookie-based session | Shared via cookie |
| **Harbor** | OIDC + cookie session | Server-side session | Shared via cookie |
| **Grafana** | Cookie session or JWT | Configurable | Cookie: shared; JWT: configurable |
| **Retool/Appsmith/ToolJet** | Server-side session | HttpOnly cookies | Shared via cookie |

**Key observations:**

1. **Most internal developer tools use cookie-based sessions**, not localStorage/sessionStorage for tokens. This is because:
   - Cookies are automatically shared across tabs
   - HttpOnly cookies are inaccessible to JavaScript (XSS protection)
   - Server-side session allows immediate revocation

2. **ArgoCD** uses JWT tokens with 24-hour expiry stored in cookies, providing cross-tab access while keeping tokens out of JavaScript-accessible storage.

3. **Backstage** uses a hybrid approach: refresh tokens in HttpOnly cookies (secure), access tokens handed to frontend via postMessage (for API calls), with a /refresh endpoint to restore sessions in new tabs.

4. **Kubernetes Dashboard** commonly uses OAuth2-Proxy, which implements the BFF pattern with cookie-based sessions.

### localStorage vs sessionStorage vs Cookies

| Storage | XSS Vulnerable | Cross-Tab | Persists After Close | CSRF Risk |
|---------|---------------|-----------|---------------------|-----------|
| localStorage | Yes | Yes | Yes | No |
| sessionStorage | Yes | **No** | No | No |
| Cookie (HttpOnly) | **No** | Yes | Configurable | Yes |
| Cookie (non-HttpOnly) | Yes | Yes | Configurable | Yes |

**Security implications:**
- Both localStorage and sessionStorage are equally vulnerable to XSS
- The security difference is persistence (localStorage survives browser close)
- HttpOnly cookies are the only browser storage mechanism inaccessible to JavaScript
- Cookie-based approaches require CSRF protection

### Holos Console Architecture Decision

Per ADR 002, Holos Console uses the **BFF pattern with oauth2-proxy** in production:

- **Production**: oauth2-proxy sidecar handles all authentication
  - Tokens stored server-side (in cookies or Redis)
  - Frontend receives HttpOnly session cookie
  - oidc-client-ts is bypassed entirely
  - Multi-tab works automatically via shared cookie

- **Development**: Direct OIDC with embedded Dex
  - oidc-client-ts manages tokens in sessionStorage
  - Per-tab sessions (minor inconvenience, per ADR 003)
  - Simple setup with `make run`

This provides production-grade security while maintaining simple local development.

### References

- [IETF OAuth 2.0 for Browser-Based Applications (draft-26)](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-browser-based-apps)
- [RFC 9700: Best Current Practice for OAuth 2.0 Security](https://datatracker.ietf.org/doc/rfc9700/)
- [Duende: Securing SPAs using the BFF Pattern](https://blog.duendesoftware.com/posts/20210326_bff/)
- [Curity: SPA Best Practices](https://curity.io/resources/learn/spa-best-practices/)
- [Auth0: The Backend for Frontend Pattern](https://auth0.com/blog/the-backend-for-frontend-pattern-bff/)
- [ArgoCD Security Documentation](https://argo-cd.readthedocs.io/en/stable/operator-manual/security/)
- [Backstage Authentication](https://backstage.io/docs/auth/)

---

## Goals

1. Configure production-appropriate token lifetimes with dev/test flexibility
2. Provide auth debugging page for development and troubleshooting
3. Support oauth2-proxy BFF mode in production deployments

## Design Decisions

| Topic | Decision | Rationale |
| ----- | -------- | --------- |
| Token storage | Keep sessionStorage (no change) | Per ADR 003: Production uses BFF; development inconvenience is acceptable |
| ID token TTL default | 15 minutes | Industry standard for short-lived access tokens |
| ID token TTL flag | `--id-token-ttl` | Allow testing with very short TTLs (e.g., 30s) |
| Refresh token max | 12 hours | Forces re-authentication at least daily |
| Refresh token flag | `--refresh-token-ttl` | Configure refresh token absolute lifetime |
| Debug page location | `/auth-debug` route | Separate from profile, focused on token internals |
| Debug page nav | Sidebar link | Consistent with existing navigation pattern |
| BFF detection | Check for `_oauth2_proxy` cookie | Standard oauth2-proxy session cookie name |

## Changes Required

### Modify (Existing Files)

- [cli/cli.go](cli/cli.go) - Add `--id-token-ttl` and `--refresh-token-ttl` flags
- [console/console.go](console/console.go) - Pass TTL config to OIDC handler
- [console/oidc/oidc.go](console/oidc/oidc.go) - Configure Dex server with TTL settings and RefreshTokenPolicy
- [ui/src/auth/AuthProvider.tsx](ui/src/auth/AuthProvider.tsx) - Add BFF mode detection, expose refresh trigger and status
- [ui/src/auth/config.ts](ui/src/auth/config.ts) - Add BFF mode configuration
- [ui/src/App.tsx](ui/src/App.tsx) - Add route and nav link for auth debug page

### Add (New Files)

- [ui/src/components/AuthDebugPage.tsx](ui/src/components/AuthDebugPage.tsx) - Debug page component

## Implementation

### Phase 1: Backend - Configurable Token TTLs

#### 1.1 Add TTL flags to CLI

Add flags to [cli/cli.go](cli/cli.go):

```go
var (
    // ... existing vars ...
    idTokenTTL      string
    refreshTokenTTL string
)

// In Command():
cmd.Flags().StringVar(&idTokenTTL, "id-token-ttl", "15m",
    "ID token lifetime (e.g., 15m, 1h, 30s for testing)")
cmd.Flags().StringVar(&refreshTokenTTL, "refresh-token-ttl", "12h",
    "Refresh token absolute lifetime - forces re-authentication")
```

#### 1.2 Update console.Config struct

Add TTL fields to [console/console.go](console/console.go) Config struct:

```go
type Config struct {
    // ... existing fields ...
    IDTokenTTL      time.Duration
    RefreshTokenTTL time.Duration
}
```

#### 1.3 Parse duration and pass to config

In [cli/cli.go](cli/cli.go) Run function:

```go
idTTL, err := time.ParseDuration(idTokenTTL)
if err != nil {
    return fmt.Errorf("invalid --id-token-ttl: %w", err)
}
refreshTTL, err := time.ParseDuration(refreshTokenTTL)
if err != nil {
    return fmt.Errorf("invalid --refresh-token-ttl: %w", err)
}

cfg := console.Config{
    // ... existing fields ...
    IDTokenTTL:      idTTL,
    RefreshTokenTTL: refreshTTL,
}
```

### Phase 2: Backend - Configure Dex Token Lifetimes

#### 2.1 Update OIDC Config struct

Add TTL fields to [console/oidc/oidc.go](console/oidc/oidc.go):

```go
type Config struct {
    // ... existing fields ...

    // IDTokenTTL is the lifetime of ID tokens.
    IDTokenTTL time.Duration

    // RefreshTokenTTL is the absolute lifetime of refresh tokens.
    // After this duration, users must re-authenticate.
    RefreshTokenTTL time.Duration
}
```

#### 2.2 Configure Dex server with TTLs

Update the server.NewServer call in [console/oidc/oidc.go](console/oidc/oidc.go):

```go
import (
    // ... existing imports ...
    "log/slog"
)

// Create refresh token policy for absolute lifetime
refreshPolicy, err := server.NewRefreshTokenPolicy(
    slogToLogrusAdapter(logger), // Dex uses logrus
    true,                         // rotation enabled
    "",                           // validIfNotUsedFor (empty = no limit)
    cfg.RefreshTokenTTL.String(), // absoluteLifetime
    "3s",                         // reuseInterval (handle network retries)
)
if err != nil {
    return nil, fmt.Errorf("failed to create refresh token policy: %w", err)
}

dexServer, err := server.NewServer(ctx, server.Config{
    Issuer:                 cfg.Issuer,
    Storage:                store,
    SkipApprovalScreen:     true,
    Logger:                 logger,
    SupportedResponseTypes: []string{"code"},
    AllowedOrigins:         []string{"*"},
    IDTokensValidFor:       cfg.IDTokenTTL,
    RefreshTokenPolicy:     refreshPolicy,
})
```

#### 2.3 Add slog to logrus adapter

Dex's RefreshTokenPolicy uses logrus. Add a simple adapter:

```go
// slogToLogrusAdapter adapts slog.Logger to logrus.FieldLogger interface.
// This is needed because Dex's RefreshTokenPolicy uses logrus internally.
type slogLogrusAdapter struct {
    logger *slog.Logger
}

func (a *slogLogrusAdapter) WithField(key string, value interface{}) *logrus.Entry {
    // Return a logrus entry that logs to our slog
    entry := logrus.NewEntry(logrus.New())
    entry.Data[key] = value
    return entry
}

// ... implement other logrus.FieldLogger methods ...
```

Note: May need to investigate if Dex accepts slog.Logger directly in newer versions.

#### 2.4 Wire TTL config in console.Serve

Update [console/console.go](console/console.go) to pass TTLs:

```go
oidcHandler, err := oidc.NewHandler(ctx, oidc.Config{
    Issuer:          s.cfg.Issuer,
    ClientID:        s.cfg.ClientID,
    RedirectURIs:    redirectURIs,
    Logger:          slog.Default(),
    IDTokenTTL:      s.cfg.IDTokenTTL,
    RefreshTokenTTL: s.cfg.RefreshTokenTTL,
})
```

### Phase 3: Frontend - BFF Mode Support

This phase adds support for running behind oauth2-proxy in production. When in BFF mode, oidc-client-ts is bypassed and authentication is handled entirely by the proxy's session cookies.

#### 3.1 Understanding oauth2-proxy Integration

When oauth2-proxy is deployed as a sidecar:

1. **All requests go through oauth2-proxy first**
2. **Unauthenticated users** are redirected to `/oauth2/start` → OIDC provider → `/oauth2/callback`
3. **Authenticated users** have an `_oauth2_proxy` session cookie
4. **oauth2-proxy forwards headers** to upstream (holos-console):
   - `X-Forwarded-User`: User's email or subject
   - `X-Forwarded-Email`: User's email
   - `X-Forwarded-Access-Token`: Access token (if configured)

The frontend doesn't need to manage tokens at all - it just needs to:
- Detect it's running in BFF mode
- Read user info from a backend endpoint that exposes forwarded headers
- Redirect to `/oauth2/sign_out` for logout

#### 3.2 Does oidc-client-ts Support BFF Mode?

**Short answer: No, but that's okay.**

oidc-client-ts is designed for browser-based OAuth clients where the frontend manages the OIDC flow. It doesn't have a "BFF mode" because in true BFF architecture, the frontend doesn't interact with the OIDC provider at all.

**Our approach:** Conditionally bypass oidc-client-ts entirely when running in BFF mode:

```typescript
// Detect BFF mode by checking for oauth2-proxy cookie
function isBFFMode(): boolean {
  return document.cookie.includes('_oauth2_proxy')
}
```

#### 3.3 Add BFF mode detection to config

Update [ui/src/auth/config.ts](ui/src/auth/config.ts):

```typescript
// Check if running behind oauth2-proxy (BFF mode)
export function isBFFMode(): boolean {
  // oauth2-proxy sets this cookie when user is authenticated
  return document.cookie.includes('_oauth2_proxy')
}

// BFF mode endpoints (oauth2-proxy standard paths)
export const BFF_ENDPOINTS = {
  // Initiate login - redirects to OIDC provider
  login: '/oauth2/start',
  // Logout - clears session and optionally redirects to OIDC logout
  logout: '/oauth2/sign_out',
  // Get user info from forwarded headers (requires backend endpoint)
  userInfo: '/api/userinfo',
}
```

#### 3.4 Add userinfo endpoint to backend

Add a simple endpoint that returns the forwarded user headers. This allows the frontend to get user info without managing tokens.

Add to [console/console.go](console/console.go):

```go
// handleUserInfo returns user information from oauth2-proxy forwarded headers.
// This endpoint is used by the frontend in BFF mode to get the current user.
func handleUserInfo(w http.ResponseWriter, r *http.Request) {
    user := r.Header.Get("X-Forwarded-User")
    email := r.Header.Get("X-Forwarded-Email")

    if user == "" && email == "" {
        // Not authenticated or not running behind oauth2-proxy
        http.Error(w, "Not authenticated", http.StatusUnauthorized)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{
        "user":  user,
        "email": email,
    })
}

// Register in Serve():
mux.HandleFunc("/api/userinfo", handleUserInfo)
```

#### 3.5 Update AuthProvider for BFF mode

Update [ui/src/auth/AuthProvider.tsx](ui/src/auth/AuthProvider.tsx) to support both modes:

```typescript
import { isBFFMode, BFF_ENDPOINTS } from './config'

// BFF mode user type (simpler than oidc-client-ts User)
interface BFFUser {
  user: string
  email: string
}

export function AuthProvider({ children }: AuthProviderProps) {
  const [user, setUser] = useState<User | null>(null)
  const [bffUser, setBffUser] = useState<BFFUser | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)
  const [isBFF] = useState(() => isBFFMode())

  // Use shared UserManager singleton (only in non-BFF mode)
  const userManager = useMemo(() => isBFF ? null : getUserManager(), [isBFF])

  // Check for existing session on mount
  useEffect(() => {
    const checkAuth = async () => {
      try {
        if (isBFF) {
          // BFF mode: check /api/userinfo
          const response = await fetch(BFF_ENDPOINTS.userInfo, {
            credentials: 'include', // Include cookies
          })
          if (response.ok) {
            const data = await response.json()
            setBffUser(data)
          }
        } else {
          // Development mode: use oidc-client-ts
          const existingUser = await userManager!.getUser()
          if (existingUser && !existingUser.expired) {
            setUser(existingUser)
          }
        }
      } catch (err) {
        console.error('Error checking auth state:', err)
        setError(err instanceof Error ? err : new Error(String(err)))
      } finally {
        setIsLoading(false)
      }
    }

    checkAuth()
  }, [isBFF, userManager])

  // Login handler
  const login = useCallback(async (returnTo?: string) => {
    if (isBFF) {
      // BFF mode: redirect to oauth2-proxy login endpoint
      const returnUrl = returnTo ?? window.location.pathname
      window.location.href = `${BFF_ENDPOINTS.login}?rd=${encodeURIComponent(returnUrl)}`
    } else {
      // Development mode: use oidc-client-ts
      const targetPath = returnTo ?? window.location.pathname
      await userManager!.signinRedirect({ state: { returnTo: targetPath } })
    }
  }, [isBFF, userManager])

  // Logout handler
  const logout = useCallback(async () => {
    if (isBFF) {
      // BFF mode: redirect to oauth2-proxy logout endpoint
      window.location.href = BFF_ENDPOINTS.logout
    } else {
      // Development mode: use oidc-client-ts
      await userManager!.signoutRedirect()
    }
  }, [isBFF, userManager])

  // ... rest of the component, handling both user types

  const isAuthenticated = isBFF ? !!bffUser : (!!user && !user.expired)

  // ... context value and provider
}
```

#### 3.6 Handle BFF mode in AuthDebugPage

The auth debug page should show different information in BFF mode:

```typescript
export function AuthDebugPage() {
  const { user, bffUser, isBFF, isAuthenticated } = useAuth()

  if (isBFF) {
    // BFF mode: show cookie-based session info
    return (
      <Card variant="outlined">
        <CardContent>
          <Typography variant="h5" gutterBottom>
            BFF Mode Active
          </Typography>
          <Alert severity="info" sx={{ mb: 2 }}>
            Authentication is handled by oauth2-proxy. Token refresh happens
            automatically via the proxy's session management.
          </Alert>
          {bffUser && (
            <>
              <Typography variant="subtitle2" color="text.secondary">
                User
              </Typography>
              <Typography variant="body2" sx={{ mb: 2, fontFamily: 'monospace' }}>
                {bffUser.user}
              </Typography>
              <Typography variant="subtitle2" color="text.secondary">
                Email
              </Typography>
              <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
                {bffUser.email}
              </Typography>
            </>
          )}
        </CardContent>
      </Card>
    )
  }

  // Development mode: show token details (existing implementation)
  // ...
}
```

### Phase 4: Frontend - Auth Debug Page

#### 4.1 Extend AuthContext with refresh functionality

Update [ui/src/auth/AuthProvider.tsx](ui/src/auth/AuthProvider.tsx):

```typescript
export interface AuthContextValue {
  // ... existing fields ...

  // True if running in BFF mode (behind oauth2-proxy)
  isBFF: boolean

  // BFF mode user info (null in development mode)
  bffUser: BFFUser | null

  // Trigger manual token refresh (development mode only)
  refreshTokens: () => Promise<void>

  // Last silent renew result (development mode only)
  lastRefreshStatus: 'idle' | 'success' | 'error'
  lastRefreshTime: Date | null
  lastRefreshError: Error | null
}
```

Add state and handler:

```typescript
const [lastRefreshStatus, setLastRefreshStatus] = useState<'idle' | 'success' | 'error'>('idle')
const [lastRefreshTime, setLastRefreshTime] = useState<Date | null>(null)
const [lastRefreshError, setLastRefreshError] = useState<Error | null>(null)

const refreshTokens = useCallback(async () => {
  if (isBFF) {
    // BFF mode: no manual refresh needed, proxy handles it
    console.warn('Manual refresh not available in BFF mode')
    return
  }

  try {
    setLastRefreshStatus('idle')
    const refreshedUser = await userManager!.signinSilent()
    setUser(refreshedUser)
    setLastRefreshStatus('success')
    setLastRefreshTime(new Date())
    setLastRefreshError(null)
  } catch (err) {
    setLastRefreshStatus('error')
    setLastRefreshTime(new Date())
    setLastRefreshError(err instanceof Error ? err : new Error(String(err)))
    throw err
  }
}, [isBFF, userManager])

// Update silent renew handlers to track status
const handleUserLoaded = (loadedUser: User) => {
  setUser(loadedUser)
  setError(null)
  setLastRefreshStatus('success')
  setLastRefreshTime(new Date())
  setLastRefreshError(null)
}

const handleSilentRenewError = (err: Error) => {
  console.error('Silent renew error:', err)
  setError(err)
  setLastRefreshStatus('error')
  setLastRefreshTime(new Date())
  setLastRefreshError(err)
}
```

#### 4.2 Create AuthDebugPage component

Create [ui/src/components/AuthDebugPage.tsx](ui/src/components/AuthDebugPage.tsx):

```typescript
import { useState, useEffect } from 'react'
import {
  Card,
  CardContent,
  Typography,
  Button,
  Box,
  LinearProgress,
  Alert,
  Chip,
  Stack,
  Divider,
} from '@mui/material'
import { useAuth } from '../auth'

export function AuthDebugPage() {
  const {
    user,
    bffUser,
    isBFF,
    isAuthenticated,
    refreshTokens,
    lastRefreshStatus,
    lastRefreshTime,
    lastRefreshError,
    login,
  } = useAuth()

  const [timeRemaining, setTimeRemaining] = useState<number | null>(null)
  const [isRefreshing, setIsRefreshing] = useState(false)

  // Calculate time remaining on ID token (development mode only)
  useEffect(() => {
    if (isBFF || !user?.expires_at) {
      setTimeRemaining(null)
      return
    }

    const updateTimeRemaining = () => {
      const now = Math.floor(Date.now() / 1000)
      const remaining = user.expires_at - now
      setTimeRemaining(Math.max(0, remaining))
    }

    updateTimeRemaining()
    const interval = setInterval(updateTimeRemaining, 1000)
    return () => clearInterval(interval)
  }, [isBFF, user?.expires_at])

  const handleRefresh = async () => {
    setIsRefreshing(true)
    try {
      await refreshTokens()
    } catch (err) {
      console.error('Manual refresh failed:', err)
    } finally {
      setIsRefreshing(false)
    }
  }

  // BFF Mode UI
  if (isBFF) {
    return (
      <Stack spacing={3}>
        <Card variant="outlined">
          <CardContent>
            <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 2 }}>
              <Typography variant="h5">Auth Debug</Typography>
              <Chip label="BFF Mode" color="info" size="small" />
            </Stack>

            <Alert severity="info" sx={{ mb: 2 }}>
              Authentication is handled by oauth2-proxy. Tokens are managed
              server-side and are not accessible to the frontend.
            </Alert>

            {isAuthenticated && bffUser ? (
              <>
                <Typography variant="subtitle2" color="text.secondary">
                  User
                </Typography>
                <Typography variant="body2" sx={{ mb: 2, fontFamily: 'monospace' }}>
                  {bffUser.user}
                </Typography>
                <Typography variant="subtitle2" color="text.secondary">
                  Email
                </Typography>
                <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
                  {bffUser.email}
                </Typography>
              </>
            ) : (
              <Button variant="contained" onClick={() => login()}>
                Sign In
              </Button>
            )}
          </CardContent>
        </Card>
      </Stack>
    )
  }

  // Development Mode UI
  if (!isAuthenticated) {
    return (
      <Card variant="outlined">
        <CardContent>
          <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 2 }}>
            <Typography variant="h5">Auth Debug</Typography>
            <Chip label="Development Mode" color="warning" size="small" />
          </Stack>
          <Typography color="text.secondary" paragraph>
            Sign in to view token information.
          </Typography>
          <Button variant="contained" onClick={() => login()}>
            Sign In
          </Button>
        </CardContent>
      </Card>
    )
  }

  const formatTime = (seconds: number) => {
    const mins = Math.floor(seconds / 60)
    const secs = seconds % 60
    return `${mins}:${secs.toString().padStart(2, '0')}`
  }

  const totalLifetime = user?.expires_in ?? 900 // Default 15 min
  const progress = timeRemaining !== null
    ? ((totalLifetime - timeRemaining) / totalLifetime) * 100
    : 0

  return (
    <Stack spacing={3}>
      <Card variant="outlined">
        <CardContent>
          <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 2 }}>
            <Typography variant="h5">ID Token Status</Typography>
            <Chip label="Development Mode" color="warning" size="small" />
          </Stack>

          <Box sx={{ mb: 3 }}>
            <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 1 }}>
              <Typography variant="body2" color="text.secondary">
                Time Remaining
              </Typography>
              <Typography variant="body2" fontWeight="bold">
                {timeRemaining !== null ? formatTime(timeRemaining) : 'N/A'}
              </Typography>
            </Box>
            <LinearProgress
              variant="determinate"
              value={progress}
              color={timeRemaining !== null && timeRemaining < 60 ? 'warning' : 'primary'}
            />
          </Box>

          <Stack direction="row" spacing={1} sx={{ mb: 2 }}>
            <Chip
              label={user?.expired ? 'Expired' : 'Valid'}
              color={user?.expired ? 'error' : 'success'}
              size="small"
            />
            <Chip
              label={`Expires: ${new Date((user?.expires_at ?? 0) * 1000).toLocaleTimeString()}`}
              size="small"
              variant="outlined"
            />
          </Stack>

          <Button
            variant="outlined"
            onClick={handleRefresh}
            disabled={isRefreshing}
          >
            {isRefreshing ? 'Refreshing...' : 'Refresh Now'}
          </Button>
        </CardContent>
      </Card>

      <Card variant="outlined">
        <CardContent>
          <Typography variant="h5" gutterBottom>
            Last Refresh Status
          </Typography>

          <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 2 }}>
            <Chip
              label={lastRefreshStatus}
              color={
                lastRefreshStatus === 'success'
                  ? 'success'
                  : lastRefreshStatus === 'error'
                  ? 'error'
                  : 'default'
              }
              size="small"
            />
            {lastRefreshTime && (
              <Typography variant="body2" color="text.secondary">
                {lastRefreshTime.toLocaleTimeString()}
              </Typography>
            )}
          </Stack>

          {lastRefreshError && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {lastRefreshError.message}
            </Alert>
          )}
        </CardContent>
      </Card>

      <Card variant="outlined">
        <CardContent>
          <Typography variant="h5" gutterBottom>
            Token Details
          </Typography>
          <Divider sx={{ my: 2 }} />

          <Typography variant="subtitle2" color="text.secondary">
            Subject (sub)
          </Typography>
          <Typography variant="body2" sx={{ mb: 2, fontFamily: 'monospace' }}>
            {user?.profile?.sub ?? 'N/A'}
          </Typography>

          <Typography variant="subtitle2" color="text.secondary">
            Email
          </Typography>
          <Typography variant="body2" sx={{ mb: 2 }}>
            {user?.profile?.email ?? 'N/A'}
          </Typography>

          <Typography variant="subtitle2" color="text.secondary">
            Scopes
          </Typography>
          <Typography variant="body2" sx={{ mb: 2, fontFamily: 'monospace' }}>
            {user?.scope ?? 'N/A'}
          </Typography>

          <Typography variant="subtitle2" color="text.secondary">
            Token Type
          </Typography>
          <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
            {user?.token_type ?? 'N/A'}
          </Typography>
        </CardContent>
      </Card>
    </Stack>
  )
}
```

#### 4.3 Add route and navigation

Update [ui/src/App.tsx](ui/src/App.tsx):

```typescript
import { AuthDebugPage } from './components/AuthDebugPage'

function MainLayout() {
  const location = useLocation()
  const isVersionPage = location.pathname.startsWith('/version')
  const isProfilePage = location.pathname.startsWith('/profile')
  const isAuthDebugPage = location.pathname.startsWith('/auth-debug')

  return (
    // ... existing layout ...
    <List sx={{ px: 1 }}>
      {/* ... existing nav items ... */}
      <ListItemButton
        component={Link}
        to="/auth-debug"
        selected={isAuthDebugPage}
      >
        <ListItemText primary="Auth Debug" />
      </ListItemButton>
    </List>

    // ... in Routes ...
    <Route path="/auth-debug" element={<AuthDebugPage />} />
  )
}
```

### Phase 5: Testing

#### 5.1 Add unit tests for TTL parsing

Add tests in [cli/cli_test.go](cli/cli_test.go):

```go
func TestTTLParsing(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    time.Duration
        wantErr bool
    }{
        {"15 minutes", "15m", 15 * time.Minute, false},
        {"1 hour", "1h", time.Hour, false},
        {"30 seconds", "30s", 30 * time.Second, false},
        {"12 hours", "12h", 12 * time.Hour, false},
        {"invalid", "invalid", 0, true},
    }
    // ... test implementation ...
}
```

#### 5.2 Manual E2E verification - Token expiration (development mode)

```bash
# Build and run with short TTL for testing
make build
./bin/holos-console --cert certs/tls.crt --key certs/tls.key \
    --id-token-ttl 30s --refresh-token-ttl 2m

# Open https://localhost:8443/ui/auth-debug
# Verify:
# - Shows "Development Mode" chip
# - Countdown timer shows ~30s after login
# - Token refreshes automatically before expiration
# - After 2 minutes, refresh fails and user must re-authenticate
```

#### 5.3 Manual E2E verification - Debug page

```bash
make run

# Open https://localhost:8443/ui/auth-debug
# Verify:
# - Shows "Sign In" button when not authenticated
# - After login, shows countdown timer
# - "Refresh Now" button triggers token refresh
# - Last refresh status updates after refresh
```

#### 5.4 Manual E2E verification - BFF mode (requires oauth2-proxy setup)

```bash
# This requires deploying holos-console behind oauth2-proxy
# See deployment documentation for oauth2-proxy configuration

# Open https://your-deployment/ui/auth-debug
# Verify:
# - Shows "BFF Mode" chip
# - Shows user info from X-Forwarded-* headers
# - No token countdown (tokens managed by proxy)
# - Logout redirects to /oauth2/sign_out
```

---

## TODO (Implementation Checklist)

### Phase 1: Backend - Configurable Token TTLs
- [x] 1.1: Add `--id-token-ttl` and `--refresh-token-ttl` flags to cli/cli.go
- [x] 1.2: Add TTL fields to console.Config struct
- [x] 1.3: Parse duration strings and wire to config

### Phase 2: Backend - Configure Dex Token Lifetimes
- [x] 2.1: Add TTL fields to oidc.Config struct
- [x] 2.2: Configure Dex server.Config with IDTokensValidFor
- [x] 2.3: Create RefreshTokenPolicy with absolute lifetime
- [ ] 2.4: Wire TTL config from console.Serve to oidc.NewHandler

### Phase 3: Frontend - BFF Mode Support
- [ ] 3.1: Add isBFFMode() detection function
- [ ] 3.2: Add BFF_ENDPOINTS constants
- [ ] 3.3: Add /api/userinfo backend endpoint
- [ ] 3.4: Update AuthProvider to support both modes
- [ ] 3.5: Update login/logout for BFF mode

### Phase 4: Frontend - Auth Debug Page
- [ ] 4.1: Extend AuthContext with BFF mode and refresh status
- [ ] 4.2: Create AuthDebugPage component with dual-mode support
- [ ] 4.3: Add /auth-debug route and navigation link

### Phase 5: Testing
- [ ] 5.1: Add unit tests for TTL duration parsing
- [ ] 5.2: Manual E2E verification - Token expiration with short TTL
- [ ] 5.3: Manual E2E verification - Auth debug page (development mode)
- [ ] 5.4: Manual E2E verification - BFF mode (with oauth2-proxy)

---

## Security Considerations

### Token TTL Defaults

- 15-minute ID tokens: Industry standard, balances UX and security
- 12-hour refresh tokens: Ensures daily re-authentication, limits persistent access

### Development Mode (sessionStorage)

- Tokens stored in sessionStorage, cleared when browser closes
- Vulnerable to XSS (same as localStorage)
- Acceptable for local development on trusted machines
- Per ADR 003: Multi-tab inconvenience is accepted

### Production Mode (BFF with oauth2-proxy)

- Tokens never exposed to browser JavaScript
- HttpOnly session cookie managed by oauth2-proxy
- CSRF protection handled by oauth2-proxy
- Session can be immediately revoked server-side
- Aligns with IETF BFF recommendation

### Debug Page

- Only exposes information already available in browser dev tools (development mode)
- In BFF mode, only shows forwarded headers (user/email)
- Useful for troubleshooting without requiring dev tool access

## References

- [ADR 002: BFF Architecture with oauth2-proxy](../adrs/002-bff-architecture-oauth2-proxy.md)
- [ADR 003: Use sessionStorage for Local Development](../adrs/003-session-storage-for-development.md)
- [oauth2-proxy Documentation](https://oauth2-proxy.github.io/oauth2-proxy/)
- [oauth2-proxy Session Storage](https://oauth2-proxy.github.io/oauth2-proxy/configuration/session_storage/)
- [oidc-client-ts UserManagerSettings](https://authts.github.io/oidc-client-ts/interfaces/UserManagerSettings.html)
- [Dex Token Configuration](https://dexidp.io/docs/configuration/tokens/)
- [Dex RefreshTokenPolicy PR](https://github.com/dexidp/dex/pull/1846)
- [Dex server.Config](https://pkg.go.dev/github.com/dexidp/dex/server)
