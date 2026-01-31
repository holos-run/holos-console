# ADR 005: Use Refresh Tokens Instead of Iframes for Silent Token Renewal

## Status

Accepted

## Context

The oidc-client-ts library supports two mechanisms for silent token renewal:

1. **Iframe-based**: Opens a hidden iframe to the authorization server's `/auth`
   endpoint with `prompt=none`. The server responds with new tokens via the
   iframe's redirect URL (`silent_redirect_uri`). This requires a
   `silent-callback.html` page to relay the response back to the parent window.

2. **Refresh token-based**: Uses the OAuth2 refresh token (obtained via the
   `offline_access` scope) to request new tokens directly from the token
   endpoint. No iframe or additional HTML page is needed.

### Problem

The "Refresh Now" button on the Auth Debug page stopped working. The
`signinSilent()` call was attempting iframe-based renewal because
`silent_redirect_uri` was configured in the OIDC settings. The iframe approach
fails because embedded Dex does not support `prompt=none` for iframe-based
silent authentication, resulting in a timeout error.

Meanwhile, the configuration already requests the `offline_access` scope, which
provides a refresh token. The refresh token approach is simpler, more reliable,
and doesn't require iframe support from the identity provider.

### Why iframes are problematic

- Require a dedicated `silent-callback.html` file served at the correct path
- Depend on the identity provider supporting `prompt=none`
- Subject to third-party cookie restrictions in modern browsers
- Add complexity (cross-origin messaging, iframe lifecycle management)
- Dex (our embedded OIDC provider) does not reliably support iframe-based
  silent auth

### Why refresh tokens are better for this use case

- Direct HTTP request to the token endpoint (no browser sandbox concerns)
- No dependency on `prompt=none` support
- No third-party cookie issues
- Already available via the `offline_access` scope we request
- Simpler: no `silent-callback.html` page needed

## Decision

**Use refresh tokens for silent token renewal.** Remove all iframe-related
configuration:

- Remove `silent_redirect_uri` from the OIDC config (Go struct, Vite config,
  TypeScript interface)
- Remove `deriveSilentRedirectURI()` Go function and its test
- Delete `ui/public/silent-callback.html`
- Remove the silent redirect URI from Dex's allowed redirect URIs

When `silent_redirect_uri` is not set, oidc-client-ts automatically falls back
to using the refresh token for `signinSilent()` and `automaticSilentRenew`.

This decision applies to local development mode only. In production, the BFF
(oauth2-proxy) handles all token management server-side (see ADR 002).

## Consequences

### Positive

- "Refresh Now" button works correctly
- Automatic silent renewal (`automaticSilentRenew: true`) works correctly
- Simpler configuration with fewer moving parts
- No `silent-callback.html` to maintain
- Not affected by browser third-party cookie restrictions

### Negative

- Cannot fall back to iframe-based renewal if the refresh token is revoked
  (user must re-authenticate via redirect, which is acceptable for development)

### Neutral

- Session storage decision (ADR 003) is unaffected; refresh tokens are stored
  in session storage alongside other OIDC state

## References

- [ADR 003: Use sessionStorage for Local Development](003-session-storage-for-development.md)
- [oidc-client-ts signinSilent](https://authts.github.io/oidc-client-ts/classes/UserManager.html#signinSilent)
- [OAuth 2.0 Refresh Tokens (RFC 6749 Section 1.5)](https://datatracker.ietf.org/doc/html/rfc6749#section-1.5)
