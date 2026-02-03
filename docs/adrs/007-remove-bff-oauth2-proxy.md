# ADR 007: Remove BFF oauth2-proxy Authentication Mode

## Status

Accepted

## Context

ADR 002 introduced a Backend-For-Frontend (BFF) authentication mode using oauth2-proxy as a sidecar reverse proxy. This mode trusted `X-Forwarded-User` and `X-Forwarded-Email` HTTP headers set by oauth2-proxy to identify users, and detected BFF mode by checking for the `_oauth2_proxy` cookie.

An application security review (#134, FINDING-02) identified that the `/api/userinfo` endpoint trusts forwarded headers without validation. If the server is accessible without an authenticating reverse proxy in front of it, any client can set these headers to spoof user identity. While the RPC layer uses JWT validation and is unaffected, the endpoint creates an unnecessary attack surface.

Additionally:

- The BFF mode was never deployed to production.
- The dual-mode authentication (BFF vs PKCE) added complexity to both the backend and frontend with no current benefit.
- The `_oauth2_proxy` cookie is HttpOnly by default, making the cookie-based detection strategy (`document.cookie.includes('_oauth2_proxy')`) unreliable as documented in ADR 002 itself.

## Decision

Reverse ADR 002. Remove all BFF/oauth2-proxy authentication support. The sole authentication mechanism is the standard `Authorization: Bearer <token>` header with OIDC PKCE flow, validated by the `LazyAuthInterceptor` in the backend.

Specifically removed:

- The `/api/userinfo` endpoint that trusted `X-Forwarded-*` headers
- The `isBFFMode()` detection function and `BFF_ENDPOINTS` configuration
- The `BFFUser` type and `bffUser`/`isBFF` fields from the auth context
- All BFF-conditional branching in `AuthProvider` (login, logout, session check)
- BFF mode UI in the profile/debug page

## Consequences

### Positive

- Eliminates the forwarded-header identity spoofing attack surface (FINDING-02)
- Simplifies authentication to a single well-understood path (OIDC PKCE + Bearer tokens)
- Reduces frontend and backend code complexity
- Removes dead code paths that were never production-tested

### Negative

- Deploying behind oauth2-proxy as a transparent authentication layer is no longer supported. Operators who want a reverse proxy must ensure the backend still receives the `Authorization: Bearer` header (e.g., by configuring oauth2-proxy to pass the access token as a bearer header rather than using forwarded headers).

### Neutral

- The IETF BFF recommendation remains valid for SPAs in general, but holos-console's architecture (backend serves the SPA and validates JWTs directly) means the BFF pattern adds no security benefit over PKCE with short-lived tokens and refresh token rotation.

## References

- [ADR 002: BFF Architecture with oauth2-proxy](002-bff-architecture-oauth2-proxy.md) (reversed)
- [Security Review FINDING-02](https://github.com/holos-run/holos-console/issues/134)
- [IETF OAuth 2.0 for Browser-Based Applications](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-browser-based-apps)
