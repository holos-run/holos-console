# Plan: Token TTL Configuration and Cross-Tab Storage

> **Status:** UNREVIEWED / UNAPPROVED
>
> Do not implement until reviewed and approved.

## Overview

This plan addresses three related authentication issues:

1. **Cross-tab token sharing**: Tokens stored in sessionStorage are not accessible from other tabs, forcing users to re-authenticate when opening new tabs.
2. **Token TTL configuration**: ID tokens and access tokens default to 24 hours, which is too long for production. Need configurable short-lived tokens (15 minutes default) with 12-hour refresh token maximum lifetime.
3. **Auth debugging page**: Add a debug page showing token expiration countdown, last refresh status, and manual refresh trigger.

## Problem Analysis

### 1. Cross-Tab Token Isolation

**Current Behavior:**
- [ui/src/auth/config.ts:55](ui/src/auth/config.ts#L55): Uses `sessionStorage` for token storage
- sessionStorage is isolated per tab/window - data is not shared between tabs
- Opening a new tab requires re-authentication

**Why sessionStorage was chosen:**
- Clears when browser closes (security benefit)
- Survives page refreshes within the same tab
- No persistent tokens on disk

**The tradeoff:**
- localStorage enables cross-tab access but persists after browser close
- sessionStorage isolates tabs but requires re-authentication per tab

### 2. Token TTL Too Long

**Current Behavior:**
- [console/oidc/oidc.go:84-91](console/oidc/oidc.go#L84-L91): Creates Dex server with default settings
- Dex default `IDTokensValidFor`: 24 hours
- No refresh token lifetime configuration

**Production requirements:**
- ID/Access tokens: 15 minutes (configurable down to seconds for testing)
- Refresh token absolute lifetime: 12 hours (forces daily re-authentication)

### 3. No Auth Debugging Visibility

**Current Behavior:**
- No visibility into token expiration time
- No indication when silent refresh occurs
- Debugging token issues requires browser dev tools

## Goals

1. Enable cross-tab token sharing while maintaining security
2. Configure production-appropriate token lifetimes with dev/test flexibility
3. Provide auth debugging page for development and troubleshooting

## Design Decisions

| Topic | Decision | Rationale |
| ----- | -------- | --------- |
| Cross-tab storage | Switch from sessionStorage to localStorage | Required for cross-tab sharing; oidc-client-ts default is sessionStorage but localStorage is widely used |
| Cross-tab events | Use storage events for sync | The `storage` event fires when localStorage changes in other tabs; simpler than BroadcastChannel |
| ID token TTL default | 15 minutes | Industry standard for short-lived access tokens |
| ID token TTL flag | `--id-token-ttl` | Allow testing with very short TTLs (e.g., 30s) |
| Refresh token max | 12 hours | Forces re-authentication at least daily |
| Refresh token flag | `--refresh-token-ttl` | Configure refresh token absolute lifetime |
| Debug page location | `/auth-debug` route | Separate from profile, focused on token internals |
| Debug page nav | Sidebar link | Consistent with existing navigation pattern |

## Changes Required

### Modify (Existing Files)

- [cli/cli.go](cli/cli.go) - Add `--id-token-ttl` and `--refresh-token-ttl` flags
- [console/console.go](console/console.go) - Pass TTL config to OIDC handler
- [console/oidc/oidc.go](console/oidc/oidc.go) - Configure Dex server with TTL settings and RefreshTokenPolicy
- [ui/src/auth/config.ts](ui/src/auth/config.ts) - Switch from sessionStorage to localStorage
- [ui/src/auth/AuthProvider.tsx](ui/src/auth/AuthProvider.tsx) - Add storage event listener for cross-tab sync, expose refresh trigger and status
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

### Phase 3: Frontend - Switch to localStorage for Cross-Tab Sharing

#### 3.1 Change storage from sessionStorage to localStorage

Update [ui/src/auth/config.ts:55](ui/src/auth/config.ts#L55):

```typescript
// Use localStorage to enable cross-tab token sharing
// Note: Tokens persist until browser clears localStorage, but refresh tokens
// have absolute lifetime configured on the server
userStore: new WebStorageStateStore({ store: window.localStorage }),
```

#### 3.2 Add cross-tab storage event listener

Update [ui/src/auth/AuthProvider.tsx](ui/src/auth/AuthProvider.tsx) to sync state across tabs:

```typescript
// Listen for storage events from other tabs
useEffect(() => {
  const handleStorageChange = async (event: StorageEvent) => {
    // oidc-client-ts stores user data with a key containing the authority and client_id
    if (event.key?.includes('oidc.user:')) {
      // User state changed in another tab - reload user
      const currentUser = await userManager.getUser()
      if (currentUser && !currentUser.expired) {
        setUser(currentUser)
      } else {
        setUser(null)
      }
    }
  }

  window.addEventListener('storage', handleStorageChange)
  return () => window.removeEventListener('storage', handleStorageChange)
}, [userManager])
```

### Phase 4: Frontend - Auth Debug Page

#### 4.1 Extend AuthContext with refresh functionality

Update [ui/src/auth/AuthProvider.tsx](ui/src/auth/AuthProvider.tsx):

```typescript
export interface AuthContextValue {
  // ... existing fields ...

  // Trigger manual token refresh
  refreshTokens: () => Promise<void>

  // Last silent renew result
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
  try {
    setLastRefreshStatus('idle')
    const refreshedUser = await userManager.signinSilent()
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
}, [userManager])

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
    isAuthenticated,
    refreshTokens,
    lastRefreshStatus,
    lastRefreshTime,
    lastRefreshError,
    login,
  } = useAuth()

  const [timeRemaining, setTimeRemaining] = useState<number | null>(null)
  const [isRefreshing, setIsRefreshing] = useState(false)

  // Calculate time remaining on ID token
  useEffect(() => {
    if (!user?.expires_at) {
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
  }, [user?.expires_at])

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

  if (!isAuthenticated) {
    return (
      <Card variant="outlined">
        <CardContent>
          <Typography variant="h5" gutterBottom>
            Auth Debug
          </Typography>
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
          <Typography variant="h5" gutterBottom>
            ID Token Status
          </Typography>

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

#### 5.2 Manual E2E verification - Token expiration

```bash
# Build and run with short TTL for testing
make build
./bin/holos-console --cert certs/tls.crt --key certs/tls.key \
    --id-token-ttl 30s --refresh-token-ttl 2m

# Open https://localhost:8443/ui/auth-debug
# Verify:
# - Countdown timer shows ~30s after login
# - Token refreshes automatically before expiration
# - After 2 minutes, refresh fails and user must re-authenticate
```

#### 5.3 Manual E2E verification - Cross-tab sync

```bash
make run

# Open https://localhost:8443/ui/profile in Tab 1
# Sign in
# Open https://localhost:8443/ui/profile in Tab 2
# Verify: Tab 2 is already authenticated (no login required)

# Click logout in Tab 1
# Verify: Tab 2 also shows logged out state
```

#### 5.4 Manual E2E verification - Debug page

```bash
make run

# Open https://localhost:8443/ui/auth-debug
# Verify:
# - Shows "Sign In" button when not authenticated
# - After login, shows countdown timer
# - "Refresh Now" button triggers token refresh
# - Last refresh status updates after refresh
```

---

## TODO (Implementation Checklist)

### Phase 1: Backend - Configurable Token TTLs
- [ ] 1.1: Add `--id-token-ttl` and `--refresh-token-ttl` flags to cli/cli.go
- [ ] 1.2: Add TTL fields to console.Config struct
- [ ] 1.3: Parse duration strings and wire to config

### Phase 2: Backend - Configure Dex Token Lifetimes
- [ ] 2.1: Add TTL fields to oidc.Config struct
- [ ] 2.2: Configure Dex server.Config with IDTokensValidFor
- [ ] 2.3: Create RefreshTokenPolicy with absolute lifetime
- [ ] 2.4: Wire TTL config from console.Serve to oidc.NewHandler

### Phase 3: Frontend - Cross-Tab Token Storage
- [ ] 3.1: Switch userStore from sessionStorage to localStorage
- [ ] 3.2: Add storage event listener for cross-tab state sync

### Phase 4: Frontend - Auth Debug Page
- [ ] 4.1: Extend AuthContext with refresh status and manual refresh
- [ ] 4.2: Create AuthDebugPage component with countdown timer
- [ ] 4.3: Add /auth-debug route and navigation link

### Phase 5: Testing
- [ ] 5.1: Add unit tests for TTL duration parsing
- [ ] 5.2: Manual E2E verification - Token expiration with short TTL
- [ ] 5.3: Manual E2E verification - Cross-tab token sync
- [ ] 5.4: Manual E2E verification - Auth debug page functionality

---

## Security Considerations

### localStorage vs sessionStorage

Switching to localStorage means tokens persist until explicitly cleared or the browser clears storage. However:

1. **Short-lived tokens**: 15-minute ID tokens limit exposure window
2. **Refresh token lifetime**: 12-hour absolute lifetime forces daily re-authentication
3. **XSS risk**: Same as sessionStorage - any XSS can access both
4. **CSRF**: Not applicable - tokens are in storage, not cookies

The tradeoff is acceptable because:
- Token lifetimes provide time-bound access control
- Cross-tab UX benefit is significant for multi-tab workflows
- Security boundary (XSS) is unchanged from sessionStorage

### Token TTL Defaults

- 15-minute ID tokens: Industry standard, balances UX and security
- 12-hour refresh tokens: Ensures daily re-authentication, limits persistent access

### Debug Page

- Only exposes information already available in browser dev tools
- Useful for troubleshooting without requiring dev tool access
- No sensitive data beyond what's in the existing token

## Alternatives Considered

### Alternative 1: BroadcastChannel API for cross-tab sync

Use BroadcastChannel to explicitly communicate between tabs.

**Rejected:** Storage events are simpler and work with oidc-client-ts's existing localStorage support. BroadcastChannel adds complexity without clear benefit.

### Alternative 2: Keep sessionStorage with login-on-demand

Keep sessionStorage but detect when a tab needs auth and trigger silent signin.

**Rejected:** Silent signin requires the auth session to still be valid at the IdP. With 12-hour refresh token lifetime, this approach would still require re-authentication, negating the UX benefit.

### Alternative 3: Service Worker for token management

Use a Service Worker to manage tokens and share across tabs.

**Rejected:** Significantly more complex. Service Worker lifecycle management adds edge cases. localStorage provides the same cross-tab capability with simpler implementation.

## References

- [oidc-client-ts UserManagerSettings](https://authts.github.io/oidc-client-ts/interfaces/UserManagerSettings.html)
- [Dex Token Configuration](https://dexidp.io/docs/configuration/tokens/)
- [Dex RefreshTokenPolicy PR](https://github.com/dexidp/dex/pull/1846)
- [Dex server.Config](https://pkg.go.dev/github.com/dexidp/dex/server)
- [oidc-client-ts Cross-Tab Issue](https://github.com/IdentityModel/oidc-client-js/issues/830)
