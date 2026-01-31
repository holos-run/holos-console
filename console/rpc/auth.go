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
func LazyAuthInterceptor(issuer, clientID string, client *http.Client) connect.UnaryInterceptorFunc {
	var (
		verifier *oidc.IDTokenVerifier
		initOnce sync.Once
		initErr  error
	)

	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// Initialize verifier on first use
			initOnce.Do(func() {
				oidcCtx := oidc.ClientContext(ctx, client)

				provider, err := oidc.NewProvider(oidcCtx, issuer)
				if err != nil {
					initErr = err
					return
				}

				verifier = provider.Verifier(&oidc.Config{
					ClientID: clientID,
				})
			})

			if initErr != nil {
				return nil, connect.NewError(connect.CodeUnavailable, initErr)
			}

			claims, err := extractAndVerifyToken(ctx, req, verifier)
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
func AuthInterceptor(verifier *oidc.IDTokenVerifier) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			claims, err := extractAndVerifyToken(ctx, req, verifier)
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
func OptionalAuthInterceptor(verifier *oidc.IDTokenVerifier) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			claims, err := extractAndVerifyToken(ctx, req, verifier)
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
func extractAndVerifyToken(ctx context.Context, req connect.AnyRequest, verifier *oidc.IDTokenVerifier) (*Claims, error) {
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

	// Ensure Sub is set from the token's Subject
	if claims.Sub == "" {
		claims.Sub = idToken.Subject
	}

	return &claims, nil
}
