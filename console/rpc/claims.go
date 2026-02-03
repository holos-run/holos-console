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

	// Roles is the list of roles the user belongs to (from the configured OIDC claim).
	Roles []string `json:"groups"`
}

// ExtractRoles extracts roles from a generic claims map using the specified claim name.
// This allows operators to configure which OIDC claim is used for role membership.
func ExtractRoles(claims map[string]interface{}, rolesClaim string) []string {
	val, ok := claims[rolesClaim]
	if !ok {
		return nil
	}
	arr, ok := val.([]interface{})
	if !ok {
		return nil
	}
	roles := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			roles = append(roles, s)
		}
	}
	return roles
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
