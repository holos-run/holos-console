# ADR 002: BFF Architecture with oauth2-proxy for Production Authentication

## Status

Accepted

## Context

Holos Console is a web UI for managing Holos platform resources. The frontend is a React
single-page application (SPA) that requires authentication via OIDC.

The IETF's [OAuth 2.0 for Browser-Based Applications](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-browser-based-apps)
(draft-26) explicitly recommends the Backend-For-Frontend (BFF) pattern for SPAs:

> "While implementing OAuth logic directly in the browser was once considered acceptable,
> this is no longer recommended. Storing any authentication state in the browser (such
> as access tokens) has proven to be inherently risky."

Browser-based token storage (localStorage, sessionStorage) is vulnerable to XSS attacks.
The BFF pattern keeps tokens server-side, exposing only HttpOnly cookies to the browser.

Comparable internal developer tools (ArgoCD, Backstage, Kubernetes Dashboard, Harbor)
all use cookie-based session management rather than browser-side token storage.

See [docs/plans/token-ttl-and-storage.md](../plans/token-ttl-and-storage.md) for detailed
research on industry standards and comparable tools.

## Decision

Holos Console will operate in a BFF model using [oauth2-proxy](https://oauth2-proxy.github.io/oauth2-proxy/)
as a sidecar to handle authentication in production deployments.

### Architecture

```
┌─────────────┐     ┌───────────────┐     ┌─────────────────┐
│   Browser   │────▶│ oauth2-proxy  │────▶│  holos-console  │
│   (SPA)     │◀────│   (sidecar)   │◀────│    (backend)    │
└─────────────┘     └───────────────┘     └─────────────────┘
       │                    │
       │   HttpOnly         │
       │   Session Cookie   │
       │                    ▼
       │            ┌───────────────┐
       │            │  OIDC IdP     │
       │            │ (Dex/Okta/    │
       │            │  Keycloak)    │
       └───────────▶└───────────────┘
```

### How it works

1. **oauth2-proxy** acts as a reverse proxy in front of holos-console
2. Unauthenticated requests are redirected to the OIDC provider for login
3. After successful authentication, oauth2-proxy stores tokens server-side
4. The browser receives an HttpOnly session cookie (`_oauth2_proxy`)
5. Subsequent requests include the cookie; oauth2-proxy validates the session
6. oauth2-proxy forwards requests to holos-console with `X-Forwarded-User` headers
7. The SPA never sees or manages tokens directly

### Frontend Mode Detection

The frontend must detect whether it is running in BFF mode (behind oauth2-proxy) or
development mode (using oidc-client-ts directly). This changes how authentication works:

| Mode | Token Management | Login/Logout | User Info |
|------|------------------|--------------|-----------|
| **BFF (Production)** | oauth2-proxy handles all tokens | Redirect to `/oauth2/start` and `/oauth2/sign_out` | Fetch from `/api/userinfo` endpoint |
| **Development** | oidc-client-ts in browser | Use UserManager methods | From oidc-client-ts User object |

#### HttpOnly Cookie Detection Limitation

**Important:** The `_oauth2_proxy` session cookie is HttpOnly by default (and should remain
so for security). This means **JavaScript cannot read the cookie** via `document.cookie`.

The original plan proposed detecting BFF mode by checking for the `_oauth2_proxy` cookie:

```typescript
// This will NOT work if the cookie is HttpOnly (which it should be)
function isBFFMode(): boolean {
  return document.cookie.includes('_oauth2_proxy')
}
```

**Alternative detection strategies:**

1. **Backend endpoint approach (recommended):** The frontend calls `/api/userinfo` on load.
   If the endpoint returns user data, we're in BFF mode with an active session. If it returns
   401, we're either not authenticated or not in BFF mode.

2. **Environment variable injection:** The backend injects a flag into the HTML (similar to
   `__OIDC_CONFIG__`) indicating whether oauth2-proxy is expected.

3. **Configuration-based:** A build-time or runtime configuration flag explicitly sets the mode.

4. **Disable HttpOnly (not recommended):** oauth2-proxy supports `--cookie-httponly=false`,
   but this weakens security and is not recommended.

See [docs/research/httponly-cookies.md](../research/httponly-cookies.md) for detailed
explanation of HttpOnly cookies and implications for frontend development.

### Session Storage Options

oauth2-proxy supports two session storage backends:

| Backend | Description | Use Case |
|---------|-------------|----------|
| **Cookie** (default) | Session data encrypted in client cookies | Single-instance deployments |
| **Redis/Valkey** | Session tickets stored server-side | Multi-instance/HA deployments |

For production Kubernetes deployments with multiple replicas, Redis or Valkey is required
for session consistency across instances.

### Open Question: Embedding oauth2-proxy

It is an open question whether oauth2-proxy should be embedded directly into the
holos-console executable, similar to how Dex is currently embedded.

**Arguments for embedding:**
- Single binary deployment (simpler operations)
- Consistent with embedded Dex pattern
- No separate sidecar container needed

**Arguments against embedding:**
- oauth2-proxy is a mature, well-maintained project
- Sidecar pattern is well-understood in Kubernetes
- Embedding requires maintaining Go integration code
- Session storage (Redis/Valkey) is still needed for HA

**If embedded, requirements:**
- Valkey or Redis for session storage (required for horizontal scaling)
- Configuration flags for oauth2-proxy settings
- Integration with existing Dex configuration or external IdP

This decision is deferred to a future ADR pending further investigation.

## Consequences

### Positive

- Tokens never exposed to browser JavaScript (XSS protection)
- Session can be immediately revoked server-side
- Aligns with IETF BFF recommendation
- Consistent with how comparable tools handle auth
- oauth2-proxy handles token refresh automatically

### Negative

- Additional component to deploy and configure in production
- Session storage (Redis/Valkey) required for HA deployments
- CSRF protection required (oauth2-proxy handles this)
- More complex local development setup (mitigated by ADR 003)

### Neutral

- The embedded Dex OIDC provider remains useful for:
  - Local development without external IdP
  - Testing authentication flows
  - Demos and evaluations
- Production deployments may use embedded Dex or external IdP with oauth2-proxy

## References

- [oauth2-proxy Documentation](https://oauth2-proxy.github.io/oauth2-proxy/)
- [oauth2-proxy Session Storage](https://oauth2-proxy.github.io/oauth2-proxy/configuration/session_storage/)
- [IETF OAuth 2.0 for Browser-Based Applications](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-browser-based-apps)
- [Duende: Securing SPAs using the BFF Pattern](https://blog.duendesoftware.com/posts/20210326_bff/)
