package rpc

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"connectrpc.com/connect"
	"github.com/coreos/go-oidc/v3/oidc"
)

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

	// Debug logging for groups claim investigation
	if os.Getenv("HOLOS_MODE") == "dev" {
		slog.Debug("token claims before dev groups injection",
			"sub", claims.Sub,
			"email", claims.Email,
			"groups", claims.Groups,
			"groups_count", len(claims.Groups),
		)
	}

	// Inject dev groups if in dev mode
	InjectDevGroups(&claims)

	// Debug logging after injection
	if os.Getenv("HOLOS_MODE") == "dev" {
		slog.Debug("token claims after dev groups injection",
			"sub", claims.Sub,
			"email", claims.Email,
			"groups", claims.Groups,
			"groups_count", len(claims.Groups),
		)
	}

	return &claims, nil
}
