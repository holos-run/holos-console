package rpc

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"connectrpc.com/connect"
	"github.com/coreos/go-oidc/v3/oidc"
)

// LazyAuthInterceptor returns a ConnectRPC interceptor that lazily initializes
// the OIDC verifier on first use. This is needed because the OIDC provider (Dex)
// may not be running when the interceptor is created. The provided HTTP client
// is used for OIDC discovery and must trust the issuer's TLS certificate.
//
// If OIDC discovery fails, the error is not cached. Subsequent requests retry
// discovery until it succeeds, at which point the verifier is cached permanently.
func LazyAuthInterceptor(issuer, clientID, rolesClaim string, client *http.Client) connect.UnaryInterceptorFunc {
	var (
		mu       sync.Mutex
		verifier *oidc.IDTokenVerifier
	)

	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// Double-checked locking: fast path avoids the mutex when already initialized.
			mu.Lock()
			v := verifier
			mu.Unlock()

			if v == nil {
				mu.Lock()
				v = verifier
				if v == nil {
					oidcCtx := oidc.ClientContext(ctx, client)
					provider, err := oidc.NewProvider(oidcCtx, issuer)
					if err != nil {
						mu.Unlock()
						return nil, connect.NewError(connect.CodeUnavailable, err)
					}
					v = provider.Verifier(&oidc.Config{
						ClientID: clientID,
					})
					verifier = v
				}
				mu.Unlock()
			}

			claims, err := extractAndVerifyToken(ctx, req, v, rolesClaim)
			if err != nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, err)
			}

			ctx = ContextWithClaims(ctx, claims)
			return next(ctx, req)
		}
	}
}

// AuthInterceptor returns a ConnectRPC interceptor that requires a valid bearer token.
// Requests without a valid token are rejected with an Unauthenticated error.
func AuthInterceptor(verifier *oidc.IDTokenVerifier, rolesClaim string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			claims, err := extractAndVerifyToken(ctx, req, verifier, rolesClaim)
			if err != nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, err)
			}

			ctx = ContextWithClaims(ctx, claims)
			return next(ctx, req)
		}
	}
}

// OptionalAuthInterceptor returns a ConnectRPC interceptor that validates bearer tokens
// if present, but allows unauthenticated requests through.
func OptionalAuthInterceptor(verifier *oidc.IDTokenVerifier, rolesClaim string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			claims, err := extractAndVerifyToken(ctx, req, verifier, rolesClaim)
			if err == nil && claims != nil {
				ctx = ContextWithClaims(ctx, claims)
			}
			// Allow unauthenticated requests through
			return next(ctx, req)
		}
	}
}

// extractAndVerifyToken extracts the bearer token from the Authorization header
// and verifies it using the provided verifier.
func extractAndVerifyToken(ctx context.Context, req connect.AnyRequest, verifier *oidc.IDTokenVerifier, rolesClaim string) (*Claims, error) {
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

	idToken, err := verifier.Verify(ctx, token)
	if err != nil {
		return nil, err
	}

	var claims Claims
	if err := idToken.Claims(&claims); err != nil {
		return nil, err
	}

	// Extract roles from the configured claim name (supports non-standard claim names).
	// The json:"groups" tag on Claims.Roles handles the default "groups" claim, but
	// when a custom rolesClaim is configured, we need to extract from a raw map.
	if rolesClaim != "" && rolesClaim != "groups" {
		var rawClaims map[string]interface{}
		if err := idToken.Claims(&rawClaims); err == nil {
			claims.Roles = ExtractRoles(rawClaims, rolesClaim)
		}
	}

	// Ensure Sub is set from the token's Subject
	if claims.Sub == "" {
		claims.Sub = idToken.Subject
	}

	return &claims, nil
}
