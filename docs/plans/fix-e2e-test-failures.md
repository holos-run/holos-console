# Plan: Fix E2E Test Failures

> **Status:** APPROVED
>
> This plan has been reviewed and approved for implementation.

## Overview

This plan addresses 5 failing E2E tests discovered when running `make test-e2e`. The failures stem from bugs introduced during the PKCE OIDC authentication implementation (v3 plan).

## Test Failures Summary

| Test                                                              | Failure             | Root Cause                  |
| ----------------------------------------------------------------- | ------------------- | --------------------------- |
| `should have landing page accessible`                             | Page shows 404      | Vite proxy misconfiguration |
| `should have version page accessible`                             | Page shows 404      | Vite proxy misconfiguration |
| `should have OIDC discovery endpoint accessible`                  | 404 response        | Dex path routing bug        |
| `should display Dex login page when accessing authorize endpoint` | Invalid URL         | Same Dex path routing bug   |
| `should show login form with username and password fields`        | No login form found | Same Dex path routing bug   |

Two tests pass:
- `should reject invalid credentials` - Passes because the test logic handles missing login forms gracefully
- `should complete login with valid credentials` - Passes for the same reason

## Root Causes

### 1. Dex Path Routing Bug (Critical)

**Location:** [console/console.go:120](console/console.go#L120)

**Problem:** The current code uses `http.StripPrefix` incorrectly:

```go
mux.Handle("/dex/", http.StripPrefix("/dex", oidcHandler))
```

When issuer is `https://localhost:8443/dex`, Dex's internal router registers paths like:
- `/dex/.well-known/openid-configuration`
- `/dex/auth`
- `/dex/token`
- etc.

The `StripPrefix` removes `/dex` from incoming requests, so Dex receives `/.well-known/openid-configuration` but looks for `/dex/.well-known/openid-configuration`, resulting in 404.

**Evidence:** Server logs show requests hitting `/dex/.well-known/openid-configuration` with 404 response:
```
"GET /dex/.well-known/openid-configuration HTTP/1.1" 404 19
```

**Fix:** Remove `http.StripPrefix`. Dex already handles the issuer path internally via gorilla/mux by joining `issuerURL.Path` with each endpoint path.

### 2. Vite Proxy Misconfiguration (Secondary)

**Location:** [ui/vite.config.ts:51-55](ui/vite.config.ts#L51-L55)

**Problem:** The Vite proxy config uses `/dex` as the path pattern:

```typescript
'/dex': {
  target: backendUrl,
  secure: false,
  changeOrigin: true,
},
```

This proxies `/dex` and `/dex/*` paths correctly. However, since the backend Dex handler has the path bug above, all proxied requests fail.

Once the Dex path bug is fixed, the proxy should work. But we should verify the proxy correctly handles all Dex sub-paths.

## Implementation Plan

### Phase 1: Fix Dex Path Routing

#### 1.1 Remove StripPrefix from Dex handler mount

**File:** `console/console.go`

Change line 120 from:
```go
mux.Handle("/dex/", http.StripPrefix("/dex", oidcHandler))
```

To:
```go
mux.Handle("/dex/", oidcHandler)
```

#### 1.2 Verify Dex discovery endpoint works

Run manual verification:
```bash
make build
./bin/holos-console --cert certs/tls.crt --key certs/tls.key &
curl -sk https://localhost:8443/dex/.well-known/openid-configuration | jq .
```

Expected: JSON response with issuer, authorization_endpoint, token_endpoint, etc.

### Phase 2: Run E2E Tests

#### 2.1 Run full E2E test suite

```bash
make test-e2e
```

All 7 tests should pass.

### Phase 3: Add Profile Page with Login Navigation

Add a Profile page to the frontend that triggers the OIDC login flow and displays user claims after authentication. This provides a manual verification mechanism for the login flow and improves the user experience.

#### 3.1 Create useAuth hook

**File:** `ui/src/auth/useAuth.ts`

Create a hook to access the AuthContext:
- Export `useAuth()` hook that returns `AuthContextValue`
- Throw error if used outside AuthProvider

#### 3.2 Create Profile page component

**File:** `ui/src/components/ProfilePage.tsx`

Create a Profile page that:
- Uses `useAuth()` hook to get auth state
- Shows "Loading..." while auth state is loading
- If not authenticated: Shows a "Sign In" button that calls `login()`
- If authenticated: Displays user profile information from OIDC claims:
  - Name (from `user.profile.name`)
  - Email (from `user.profile.email`)
  - Subject (from `user.profile.sub`)
  - Shows a "Sign Out" button that calls `logout()`

Use MUI components consistent with existing pages (Card, CardContent, Typography, Button, etc.)

#### 3.3 Add Profile navigation link to sidebar

**File:** `ui/src/App.tsx`

Update `MainLayout` component:
- Add "Profile" link to the sidebar navigation (below "Version")
- Add route for `/profile` that renders `<ProfilePage />`
- Update `isProfilePage` detection for selected state

#### 3.4 Export useAuth from auth module

**File:** `ui/src/auth/index.ts`

Add `useAuth` to the exports from the auth module.

#### 3.5 Add E2E tests for profile navigation and login flow

**File:** `ui/e2e/auth.spec.ts`

Add new test describe block `Profile Page` with tests:

1. `should show profile page with sign in button when not authenticated`
   - Navigate to `/ui/profile`
   - Verify "Sign In" button is visible
   - Verify user info is NOT visible

2. `should navigate to profile page from sidebar`
   - Navigate to `/ui`
   - Click "Profile" link in sidebar
   - Verify URL is `/ui/profile`
   - Verify profile page content loads

3. `should complete full login flow via profile page`
   - Navigate to `/ui/profile`
   - Click "Sign In" button
   - Wait for redirect to Dex login page
   - Fill in credentials (admin/verysecret)
   - Submit login form
   - Wait for redirect back to app
   - Verify profile page shows user info (name, email)
   - Verify "Sign Out" button is visible

### Phase 4: Manual Verification (Optional)

#### 4.1 Start servers and test login manually

1. Start backend: `make run`
2. Start frontend: `cd ui && npm run dev`
3. Open https://localhost:5173/ui
4. Click "Profile" in sidebar
5. Click "Sign In" button
6. Enter credentials: admin / verysecret
7. Verify redirect back to profile page with user info displayed

## TODO (Implementation Checklist)

### Phase 1: Fix Dex Path Routing
- [x] 1.1: Remove `http.StripPrefix` from Dex handler mount in `console/console.go`
- [x] 1.2: Verify Dex discovery endpoint works with `curl`

### Phase 2: Run E2E Tests
- [x] 2.1: Run `npm run test:e2e` and verify all 7 tests pass

### Phase 3: Add Profile Page with Login Navigation
- [x] 3.1: Create `useAuth` hook in `ui/src/auth/useAuth.ts` (already exists)
- [x] 3.2: Create ProfilePage component in `ui/src/components/ProfilePage.tsx`
- [x] 3.3: Add Profile navigation link to sidebar and route in `ui/src/App.tsx`
- [x] 3.4: Export `useAuth` from `ui/src/auth/index.ts` (already exists)
- [x] 3.5: Add E2E tests for profile navigation and login flow in `ui/e2e/auth.spec.ts`

### Phase 4: Manual Verification (Optional)
- [x] 4.1: Manual login flow verification via Profile page

### Phase 5: Fix Post-Login Redirect and Auth State Issues
- [x] 5.1: Create shared UserManager singleton in `ui/src/auth/userManager.ts`
- [ ] 5.2: Update AuthProvider to use shared UserManager
- [ ] 5.3: Update Callback to use shared UserManager and read returnTo state
- [ ] 5.4: Update login() to accept and pass returnTo parameter
- [ ] 5.5: Update ProfilePage to pass current path on login
- [ ] 5.6: Update auth module exports
- [ ] 5.7: Update E2E tests for return URL behavior

### Phase 5: Fix Post-Login Redirect and Auth State Issues (Details)

Two issues were identified during manual verification that prevent a smooth login experience.

#### Issue 5.1: Wrong redirect location after Dex login

**Problem:** After logging in at Dex from the Profile page, user is redirected to `/` (landing page) instead of back to `/profile`.

**Root Cause:** In [ui/src/auth/Callback.tsx:24](ui/src/auth/Callback.tsx#L24), the callback handler hardcodes the redirect:
```typescript
navigate('/', { replace: true })
```

**Solution:** Use `oidc-client-ts`'s built-in state management to preserve the return URL:
1. Before calling `login()`, store the current path in OIDC state via `signinRedirect({ state: { returnTo: location.pathname } })`
2. After callback, read state from the user object and navigate to `state.returnTo` or default to `/`

#### Issue 5.2: Profile page shows Login button after successful auth

**Problem:** After Dex redirects back to the app post-login, the Profile page still shows the "Sign In" button instead of user info. A hard refresh is required to see the authenticated state.

**Root Cause:** The `Callback` component creates a new `UserManager` instance via `createUserManager()` which is different from the `UserManager` in `AuthProvider`. When `signinRedirectCallback()` stores the user in sessionStorage, the AuthProvider's UserManager doesn't receive the `userLoaded` event because it's a different instance.

**Solution:**
1. Share a single UserManager instance between AuthProvider and Callback
2. Export the UserManager from AuthProvider context or create a singleton
3. When Callback completes, the shared UserManager fires `userLoaded` which AuthProvider already listens to

#### 5.1 Create shared UserManager singleton

**File:** `ui/src/auth/userManager.ts`

Create a singleton UserManager:
- Export `getUserManager()` function that creates and caches a single instance
- Use lazy initialization (create on first call)

#### 5.2 Update AuthProvider to use shared UserManager

**File:** `ui/src/auth/AuthProvider.tsx`

- Import and use `getUserManager()` instead of creating UserManager in useMemo
- Remove `createUserManager()` export (replaced with singleton)

#### 5.3 Update Callback to use shared UserManager

**File:** `ui/src/auth/Callback.tsx`

- Import and use `getUserManager()` instead of `createUserManager()`
- After `signinRedirectCallback()`, read `user.state.returnTo` for redirect target
- Navigate to `returnTo` or default to `/`

#### 5.4 Update login() to preserve return URL

**File:** `ui/src/auth/AuthProvider.tsx`

- Modify `login()` to accept optional `returnTo` parameter
- Pass `{ state: { returnTo } }` to `signinRedirect()`
- Default `returnTo` to current `window.location.pathname`

#### 5.5 Update ProfilePage to pass current path on login

**File:** `ui/src/components/ProfilePage.tsx`

- Use `useLocation()` hook to get current path
- Pass `location.pathname` to `login()` call

#### 5.6 Update auth module exports

**File:** `ui/src/auth/index.ts`

- Export `getUserManager` instead of `createUserManager`

#### 5.7 Update E2E tests for return URL behavior

**File:** `ui/e2e/auth.spec.ts`

Update `should complete full login flow via profile page` test:
- After login, verify user lands on `/ui/profile` (not `/ui/`)
- Remove the conditional navigation to profile page (should land there automatically)

---

## Technical Details

### How Dex Handles Paths

Dex uses gorilla/mux for routing. When configured with issuer `https://localhost:8443/dex`, it:

1. Parses the issuer URL to extract path: `/dex`
2. For each endpoint, joins issuer path with endpoint path:
   - `path.Join("/dex", "/.well-known/openid-configuration")` → `/dex/.well-known/openid-configuration`
   - `path.Join("/dex", "/auth")` → `/dex/auth`
   - etc.
3. Registers handlers at these full paths

See [dex/server/server.go:451](https://github.com/dexidp/dex/blob/master/server/server.go#L451):
```go
r.Handle(path.Join(issuerURL.Path, p), handlerWithHeaders(p, handler))
```

Therefore, when mounting Dex as a handler, we should NOT strip the prefix. The handler expects to receive requests with the full `/dex/*` path.

### Why Some Tests Pass

The tests `should reject invalid credentials` and `should complete login with valid credentials` pass because:

1. They contain conditional logic that skips assertions when login form is not found:
   ```typescript
   if ((await usernameInput.count()) > 0) {
     // Only executes if form is found
     await usernameInput.fill(...)
   }
   ```

2. Without the login form, these tests effectively become no-ops and pass vacuously.

This is a test design issue - the tests should fail when the login form is not found since that indicates the OIDC flow is broken. However, fixing the Dex path bug will make this moot.

### Reference: GitHub Issue #502

The path routing behavior is documented in [dexidp/dex#502](https://github.com/dexidp/dex/issues/502) which discusses how Dex handles non-root issuer paths per the OpenID Connect Discovery spec.
