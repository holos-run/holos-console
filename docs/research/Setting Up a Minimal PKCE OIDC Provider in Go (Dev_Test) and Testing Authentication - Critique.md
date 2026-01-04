# Critique: Setting Up a Minimal PKCE OIDC Provider in Go (Dev/Test)

This document provides a critical analysis of the research document on embedding a minimal OIDC provider for development and testing purposes.

## Executive Summary

The research presents a pragmatic approach to local development authentication but makes several recommendations that warrant closer examination. While the core premise—embedding an OIDC provider for dev/test—is sound, some library choices, testing strategies, and configuration approaches deserve scrutiny.

---

## 1. Library Selection: go-oidc vs zitadel/oidc vs Dex

### Research Recommendation
The document suggests `luikyv/go-oidc` or `zitadel/oidc` for embedded providers, with Dex as an alternative.

### Critique

**luikyv/go-oidc Concerns:**
- The library has relatively low GitHub stars and limited community adoption
- Being "fully compliant" is a strong claim—OIDC certification requires passing the official test suite
- The in-memory storage is convenient but the library's maturity is unclear

**zitadel/oidc Strengths:**
- Actively maintained by a funded company (Zitadel)
- Used in production by Zitadel's own product
- OpenID Foundation certified
- Well-documented with working examples

**Dex Trade-offs:**
- Running as a separate process adds complexity, but also provides better isolation
- Battle-tested in Kubernetes environments (part of CNCF)
- Configuration-driven approach is actually an advantage for reproducibility

**Recommendation:** Prefer `zitadel/oidc` for embedded use due to certification and active maintenance. Consider Dex if process isolation is acceptable—its maturity and CNCF backing outweigh the "separate process" inconvenience.

---

## 2. Security Boundary Between Dev and Production

### Research Recommendation
Use build flags or runtime config to disable the internal IdP in production.

### Critique

**The Concern:**
The research correctly identifies that dev IdP code should not exist in production, but underestimates the risk of misconfiguration. A simple runtime flag (`--dev-idp`) could accidentally be enabled in production by:
- Environment variable pollution
- Copy-paste deployment configurations
- CI/CD misconfigurations

**Stronger Alternatives:**

1. **Build Tags (Recommended):**
   ```go
   //go:build dev
   ```
   Code literally doesn't exist in production binaries. No runtime risk.

2. **Separate Binaries:**
   Build `holos-console` for production and `holos-console-dev` that includes the IdP. This makes the distinction explicit.

3. **Panic on Production Detection:**
   If the dev IdP initializes and detects it's running in a production-like environment (e.g., non-localhost issuer, no `DEV` env var), panic immediately rather than serving requests.

**Recommendation:** Use build tags exclusively. Runtime flags for security-sensitive features are an anti-pattern.

---

## 3. JWT Validation Approach

### Research Recommendation
Use `coreos/go-oidc` or a Connect interceptor like `deepworx/go-utils/pkg/connectrpc/jwtauth`.

### Critique

**deepworx/go-utils Concerns:**
- Extremely low visibility library (appears to be a personal/small project)
- Using third-party interceptors for security-critical code is risky
- The library may not be maintained or audited

**Better Approach:**

1. **Use coreos/go-oidc directly:**
   - Well-maintained (now under `github.com/coreos/go-oidc/v3`)
   - Handles JWKS caching and rotation
   - Only does token verification—leave interceptor logic to you

2. **Write a simple interceptor:**
   ```go
   func AuthInterceptor(verifier *oidc.IDTokenVerifier) connect.UnaryInterceptorFunc {
       return func(next connect.UnaryFunc) connect.UnaryFunc {
           return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
               token := extractBearerToken(req.Header())
               idToken, err := verifier.Verify(ctx, token)
               if err != nil {
                   return nil, connect.NewError(connect.CodeUnauthenticated, err)
               }
               ctx = context.WithValue(ctx, claimsKey, idToken.Claims)
               return next(ctx, req)
           }
       }
   }
   ```

**Recommendation:** Avoid `deepworx/go-utils`. Write a thin interceptor using `coreos/go-oidc/v3` directly—it's ~20 lines of code and you control the security-critical path.

---

## 4. Testing Strategy

### Research Recommendation
- Unit tests with forged tokens
- Integration tests with the real dev IdP
- E2E tests with Playwright and saved auth state

### Critique

**Forged Tokens in Unit Tests:**
The suggestion to "forge a token using the same signing key" is pragmatic but has a subtle issue: your test is now coupled to implementation details (the specific key used). If the dev IdP regenerates keys on startup (common for ephemeral providers), tests break.

**Better Approach:**
1. Export a test key deterministically (e.g., from a seed or fixed file)
2. Or use the actual dev IdP in all tests that need tokens—it's fast enough

**E2E Global Setup Concerns:**
The Playwright global setup approach assumes the IdP login page is stable. If the dev IdP changes its HTML form, tests break. Consider:

1. **Programmatic login endpoint (research mentions this but dismisses it):**
   Actually, a `/dev/login` endpoint that issues a token directly is not a "hack"—it's a pragmatic testing interface. Many production systems have test/staging login shortcuts.

2. **API-level auth for E2E:**
   If testing the frontend's auth handling is not the goal, inject the token via localStorage before tests run, bypassing the login UI entirely.

**HashiCorp cap/oidc Overlooked:**
The research mentions `cap/oidc`'s `StartTestProvider` but doesn't emphasize it enough. This is purpose-built for testing and may be preferable to running your app's dev IdP in tests—separation of concerns.

---

## 5. Configuration Injection

### Research Recommendation
Two options: Go template injection in `index.html` or a `config.json` fetched at runtime.

### Critique

**Template Injection Concerns:**
- Mixes Go templating into the frontend build artifact
- The embedded `ui/` files become templates, not static assets
- This couples the Go server tightly to the frontend's runtime config needs

**config.json Concerns:**
- Adds a network round-trip before the app can initialize OIDC
- Race conditions if the app tries to use OIDC before config loads
- Must block rendering until config is fetched

**Alternative: Script Tag Injection (Recommended):**
Instead of templating the entire HTML, inject a single `<script>` tag dynamically:

```go
func serveIndex(w http.ResponseWriter, r *http.Request) {
    config := fmt.Sprintf(`<script>window.__CONFIG__=%s</script>`,
        json.Marshal(map[string]string{
            "oidcIssuer":   cfg.OIDCIssuer,
            "oidcClientID": cfg.OIDCClientID,
        }))
    // Insert before </head> in the static index.html
    html := strings.Replace(staticIndex, "</head>", config+"</head>", 1)
    w.Write([]byte(html))
}
```

This approach:
- Keeps index.html as a normal static file during development
- No network round-trip for config
- Config is available synchronously before React boots

---

## 6. Missing Considerations

### Token Refresh
The research focuses on initial authentication but doesn't address token refresh. SPAs using PKCE typically need:
- Silent refresh via iframe (deprecated in some browsers)
- Refresh tokens (requires secure storage consideration)
- Short-lived access tokens with session cookies as backup

### CORS in Development
If the Vite dev server runs on a different port than the Go backend, CORS configuration is needed for the token endpoint. The research doesn't mention this.

### State Management
Where does the React app store tokens? `localStorage` is vulnerable to XSS. `sessionStorage` doesn't survive page refreshes. Memory is safest but complex. The research should address this.

### Logout
No mention of OIDC logout flows (front-channel, back-channel, session management). A complete dev IdP should support `end_session_endpoint`.

---

## 7. Summary of Recommendations

| Topic | Research Suggestion | Recommended Alternative |
|-------|---------------------|------------------------|
| Library | go-oidc or zitadel | **zitadel/oidc** (certified, maintained) |
| Dev/Prod boundary | Runtime config | **Build tags** (compile-time exclusion) |
| JWT validation | deepworx interceptor | **coreos/go-oidc v3** + custom interceptor |
| Test tokens | Forge with key | **Deterministic key** or use real IdP |
| E2E login | Browser automation | **Programmatic login endpoint** for speed |
| Config injection | Template or fetch | **Script tag injection** (sync, simple) |

---

## Conclusion

The research provides a solid foundation but leans toward convenience over rigor in some areas. The most significant gaps are:

1. **Security boundary weakness:** Runtime flags are insufficient for excluding dev-only code
2. **Third-party library risk:** The suggested JWT interceptor library is too obscure for security-critical code
3. **Incomplete scope:** Token refresh, CORS, and logout are not addressed

For a production-bound project, I recommend stricter build-time separation, using only well-established libraries for authentication, and expanding the scope to cover the full token lifecycle.
