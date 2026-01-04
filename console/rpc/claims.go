package rpc

import "context"

// Claims represents the claims extracted from an OIDC ID token.
type Claims struct {
	// Sub is the subject identifier (unique user ID).
	Sub string `json:"sub"`

	// Email is the user's email address.
	Email string `json:"email"`

	// EmailVerified indicates whether the email has been verified.
	EmailVerified bool `json:"email_verified"`

	// Name is the user's full name.
	Name string `json:"name"`

	// Groups is the list of groups the user belongs to.
	Groups []string `json:"groups"`
}

// claimsKey is the context key for storing claims.
type claimsKey struct{}

// ContextWithClaims returns a new context with the claims stored.
func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

// ClaimsFromContext retrieves the claims from the context.
// Returns nil if no claims are present.
func ClaimsFromContext(ctx context.Context) *Claims {
	claims, _ := ctx.Value(claimsKey{}).(*Claims)
	return claims
}
