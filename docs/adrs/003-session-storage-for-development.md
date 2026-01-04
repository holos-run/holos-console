# ADR 003: Use sessionStorage for Local Development Authentication

## Status

Accepted

## Context

Holos Console uses [oidc-client-ts](https://github.com/authts/oidc-client-ts) for OIDC
authentication in the React frontend. The library supports multiple storage backends:

- `sessionStorage` (current default) - Per-tab, cleared on browser close
- `localStorage` - Cross-tab, persists until explicitly cleared
- Custom stores (Web Workers, IndexedDB, etc.)

With the decision to use oauth2-proxy as a BFF in production (see ADR 002), the frontend's
token storage mechanism becomes irrelevant for production deployments - oauth2-proxy handles
all token management server-side.

However, local development still uses the embedded Dex OIDC provider with browser-side
token management via oidc-client-ts.

### The Multi-Tab Problem

When using sessionStorage (current behavior):
- Opening a new tab requires re-authentication
- Each tab has an isolated session
- Logging out in one tab doesn't affect other tabs

This is a known limitation that could be addressed by switching to localStorage.

### Security Trade-offs

| Storage | XSS Vulnerable | Cross-Tab | Persists After Close |
|---------|---------------|-----------|---------------------|
| sessionStorage | Yes | No | No |
| localStorage | Yes | Yes | Yes |

Both are equally vulnerable to XSS. The difference is:
- sessionStorage: Tokens lost when tab closes (more secure for dev machines)
- localStorage: Tokens persist until cleared (more convenient for multi-tab)

## Decision

**Keep sessionStorage for local development.** Do not invest effort in changing storage
mechanisms because:

1. **Production uses BFF**: In production, oauth2-proxy handles authentication. The frontend
   doesn't manage tokens at all - it just includes cookies with requests. The oidc-client-ts
   storage configuration is irrelevant in production.

2. **Development is single-user**: Local development is typically single-user on a trusted
   machine. The multi-tab UX friction is minor compared to the security benefit of tokens
   being cleared when the browser closes.

3. **Effort better spent elsewhere**: Implementing cross-tab synchronization with localStorage
   requires additional code (storage event listeners, state sync). This effort provides no
   production benefit and minimal development benefit.

4. **Consistency with production model**: Keeping sessionStorage in development subtly
   encourages the mental model that authentication is per-session, aligning with how
   the production BFF model works (cookie-based sessions).

### Accepted Trade-off

Developers must re-authenticate when opening new tabs during local development. This is
acceptable because:
- Re-authentication with embedded Dex is fast (single click)
- This only affects `make run` local development, not production
- The session survives page refreshes within the same tab

## Consequences

### Positive

- No additional code to maintain for storage synchronization
- Tokens cleared when browser closes (better for shared dev machines)
- Development effort focused on production-relevant features
- Clear separation: dev uses oidc-client-ts, prod uses oauth2-proxy

### Negative

- Developers must re-authenticate per tab during local development
- New tabs show "Sign In" button instead of being automatically authenticated

### Neutral

- The oidc-client-ts configuration remains simple
- No breaking changes to existing development workflow

## References

- [ADR 002: BFF Architecture with oauth2-proxy](002-bff-architecture-oauth2-proxy.md)
- [oidc-client-ts UserManagerSettings](https://authts.github.io/oidc-client-ts/interfaces/UserManagerSettings.html)
- [ui/src/auth/config.ts](../../ui/src/auth/config.ts) - Current sessionStorage configuration
