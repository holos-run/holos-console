# Security: ID Token Validation

This document describes how holos-console validates OIDC ID tokens and how each validation step conforms to the OpenID Connect specification.

## Overview

holos-console validates ID tokens using the [go-oidc](https://github.com/coreos/go-oidc) library (v3). Token validation occurs in the `LazyAuthInterceptor` ConnectRPC interceptor, which protects RPC endpoints that require authentication.

## Validation Flow

```
1. Extract Authorization header
2. Verify Bearer token format
3. Validate JWT signature against JWKS
4. Verify token expiration (exp)
5. Verify issuer (iss)
6. Verify audience (aud)
7. Extract and validate claims
8. Extract roles from configured claim
```

## Validation Checks

### 1. Bearer Token Extraction

**Location:** [console/rpc/auth.go:90-103](console/rpc/auth.go#L90-L103)

```go
auth := req.Header().Get("Authorization")
if auth == "" {
    return nil, connect.NewError(connect.CodeUnauthenticated, nil)
}

const bearerPrefix = "Bearer "
if !strings.HasPrefix(auth, bearerPrefix) {
    return nil, connect.NewError(connect.CodeUnauthenticated, nil)
}

token := strings.TrimPrefix(auth, bearerPrefix)
if token == "" {
    return nil, connect.NewError(connect.CodeUnauthenticated, nil)
}
```

**OIDC Conformance:** While not part of the ID Token validation spec itself, this implements RFC 6750 (Bearer Token Usage) for transmitting tokens in the Authorization header.

### 2. JWT Signature Verification

**Location:** [console/rpc/auth.go:105](console/rpc/auth.go#L105)

```go
idToken, err := verifier.Verify(ctx, token)
```

The `verifier.Verify()` method validates the JWT signature by:
1. Fetching the JSON Web Key Set (JWKS) from the provider's `jwks_uri` endpoint
2. Verifying the token's signature against the appropriate key based on the `kid` (Key ID) header
3. Ensuring the signing algorithm matches supported algorithms (defaults to RS256)

**OIDC Conformance:** [OpenID Connect Core 1.0, Section 3.1.3.7](https://openid.net/specs/openid-connect-core-1_0.html#IDTokenValidation), Step 6:
> "The Client MUST validate the signature of all other ID Tokens according to JWS using the algorithm specified in the JWT alg Header Parameter."

### 3. Token Expiration (exp claim)

**Location:** Handled by `verifier.Verify()` at [console/rpc/auth.go:105](console/rpc/auth.go#L105)

The go-oidc library automatically verifies that `exp` (expiration time) has not passed. Expired tokens are rejected with a `TokenExpiredError`.

**OIDC Conformance:** [OpenID Connect Core 1.0, Section 3.1.3.7](https://openid.net/specs/openid-connect-core-1_0.html#IDTokenValidation), Step 9:
> "The current time MUST be before the time represented by the exp Claim."

### 4. Issuer Validation (iss claim)

**Location:** Handled by `verifier.Verify()` at [console/rpc/auth.go:105](console/rpc/auth.go#L105)

**Configuration:** The expected issuer is configured via the `--issuer` CLI flag and passed to `LazyAuthInterceptor` at [console/console.go:203](console/console.go#L203).

The go-oidc library verifies that the token's `iss` claim exactly matches the configured issuer URL.

**OIDC Conformance:** [OpenID Connect Core 1.0, Section 3.1.3.7](https://openid.net/specs/openid-connect-core-1_0.html#IDTokenValidation), Step 1:
> "The Issuer Identifier for the OpenID Provider MUST exactly match the value of the iss (issuer) Claim."

### 5. Audience Validation (aud claim)

**Location:** Handled by `verifier.Verify()` at [console/rpc/auth.go:105](console/rpc/auth.go#L105)

**Configuration:** The expected client ID is configured via the `--client-id` CLI flag (default: `holos-console`) and passed to the verifier at [console/rpc/auth.go:36-38](console/rpc/auth.go#L36-L38):

```go
verifier = provider.Verifier(&oidc.Config{
    ClientID: clientID,
})
```

The go-oidc library verifies that the token's `aud` claim contains the configured client ID.

**OIDC Conformance:** [OpenID Connect Core 1.0, Section 3.1.3.7](https://openid.net/specs/openid-connect-core-1_0.html#IDTokenValidation), Step 2:
> "The Client MUST validate that the aud (audience) Claim contains its client_id value registered at the Issuer."

### 6. Subject Claim Extraction (sub claim)

**Location:** [console/rpc/auth.go:126-128](console/rpc/auth.go#L126-L128)

```go
if claims.Sub == "" {
    claims.Sub = idToken.Subject
}
```

The subject identifier is extracted from the validated token and stored in claims for use by RPC handlers.

**OIDC Conformance:** [OpenID Connect Core 1.0, Section 2](https://openid.net/specs/openid-connect-core-1_0.html#IDToken):
> "sub: REQUIRED. Subject Identifier. A locally unique and never reassigned identifier within the Issuer for the End-User."

### 7. Claims Extraction

**Location:** [console/rpc/auth.go:110-113](console/rpc/auth.go#L110-L113)

```go
var claims Claims
if err := idToken.Claims(&claims); err != nil {
    return nil, err
}
```

**Claims Structure:** [console/rpc/claims.go:6-21](console/rpc/claims.go#L6-L21)

```go
type Claims struct {
    Sub           string   `json:"sub"`            // Subject identifier
    Email         string   `json:"email"`          // User's email
    EmailVerified bool     `json:"email_verified"` // Email verification status
    Name          string   `json:"name"`           // User's full name
    Roles         []string `json:"groups"`         // Role memberships (from configured OIDC claim)
}
```

### 8. Configurable Roles Claim Extraction

**Location:** [console/rpc/auth.go:115-123](console/rpc/auth.go#L115-L123)

```go
if rolesClaim != "" && rolesClaim != "groups" {
    var rawClaims map[string]interface{}
    if err := idToken.Claims(&rawClaims); err == nil {
        claims.Roles = ExtractRoles(rawClaims, rolesClaim)
    }
}
```

**Configuration:** The `--roles-claim` CLI flag (default: `"groups"`) configures which OIDC token claim is used for role membership extraction. This allows integration with identity providers that use non-standard claim names (e.g., `realm_roles` for Keycloak).

**Behavior:**
- When `rolesClaim` is `"groups"` (the default), roles are deserialized directly from the token's `groups` claim via the `json:"groups"` struct tag on `Claims.Roles`.
- When `rolesClaim` is set to a custom value (e.g., `"realm_roles"`), `extractAndVerifyToken` re-parses the token into a raw `map[string]interface{}` and calls `ExtractRoles()` ([console/rpc/claims.go:25-41](console/rpc/claims.go#L25-L41)) to extract the string array from the specified claim name.

**ExtractRoles helper:** [console/rpc/claims.go:25-41](console/rpc/claims.go#L25-L41)

`ExtractRoles` handles type assertions safely: it returns `nil` if the claim is missing or is not a `[]interface{}`. Non-string elements within the array are silently skipped.

## OIDC Provider Discovery

**Location:** [console/rpc/auth.go:30-38](console/rpc/auth.go#L30-L38)

```go
provider, err := oidc.NewProvider(oidcCtx, issuer)
if err != nil {
    initErr = err
    return
}

verifier = provider.Verifier(&oidc.Config{
    ClientID: clientID,
})
```

The `oidc.NewProvider()` function fetches the OIDC discovery document from `{issuer}/.well-known/openid-configuration` to obtain:
- `jwks_uri`: URL for fetching signing keys
- `issuer`: Canonical issuer identifier
- Supported algorithms and other provider metadata

**OIDC Conformance:** [OpenID Connect Discovery 1.0, Section 4](https://openid.net/specs/openid-connect-discovery-1_0.html#ProviderConfig)

## Validation NOT Performed

The following validations are NOT performed by holos-console:

### Nonce Validation

The `nonce` claim is not validated server-side. Per the OIDC spec, nonce validation is primarily a client-side concern to prevent replay attacks during the authorization flow. The frontend (oidc-client-ts) handles nonce validation during the token exchange.

### Issued At (iat) Validation

The go-oidc library does not enforce `iat` validation by default. Per the OIDC spec, this is an OPTIONAL check.

### Access Token Hash (at_hash) Validation

The `at_hash` claim is not validated. This is only required when an access token is returned alongside the ID token in the authorization response (implicit flow). holos-console uses the authorization code flow with PKCE.

## Interceptor Types

holos-console provides three authentication interceptors:

| Interceptor | Location | Behavior |
|------------|----------|----------|
| `LazyAuthInterceptor` | [auth.go:17-54](console/rpc/auth.go#L17-L54) | Requires valid token; lazy provider initialization |
| `AuthInterceptor` | [auth.go:58-70](console/rpc/auth.go#L58-L70) | Requires valid token; immediate provider required |
| `OptionalAuthInterceptor` | [auth.go:74-85](console/rpc/auth.go#L74-L85) | Validates if present; allows unauthenticated |

Protected endpoints (e.g., SecretsService) use `LazyAuthInterceptor` configured at [console/console.go:203](console/console.go#L203).

## Security Considerations

### TLS for OIDC Discovery

TLS certificate verification is always enforced for OIDC discovery connections. When using certificates signed by a custom CA (e.g., mkcert for local development), provide the CA certificate via the `--ca-cert` flag so the server can verify the issuer's TLS certificate. For example: `--ca-cert $(mkcert -CAROOT)/rootCA.pem`. In production with publicly trusted certificates, no `--ca-cert` flag is needed.

### Token Storage

Tokens are stored in browser session storage (not local storage) to:
- Survive page refreshes within the same session
- Clear automatically when the browser tab closes
- Isolate sessions between browser tabs

See [docs/authentication.md](docs/authentication.md) for frontend security details.

## References

- [OpenID Connect Core 1.0](https://openid.net/specs/openid-connect-core-1_0.html)
- [OpenID Connect Discovery 1.0](https://openid.net/specs/openid-connect-discovery-1_0.html)
- [RFC 6750: Bearer Token Usage](https://tools.ietf.org/html/rfc6750)
- [RFC 7636: PKCE](https://tools.ietf.org/html/rfc7636)
- [go-oidc library](https://github.com/coreos/go-oidc)
