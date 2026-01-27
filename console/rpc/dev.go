package rpc

import "os"

// InjectDevGroups adds groups to claims in development mode.
// This allows testing group-based RBAC without a full OIDC provider
// that returns groups claims.
//
// Only active when HOLOS_MODE=dev.
func InjectDevGroups(claims *Claims) {
	if os.Getenv("HOLOS_MODE") != "dev" {
		return
	}
	if claims == nil {
		return
	}
	// Admin user gets owner group for dev testing
	// Check for both "admin" (from embedded Dex connector) and "admin@example.com"
	if claims.Email == "admin" || claims.Email == "admin@example.com" {
		claims.Groups = append(claims.Groups, "owner")
	}
}
