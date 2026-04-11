// Package oidc provides an embedded OIDC identity provider using Dex.
package oidc

import "os"

const (
	// DefaultUsername is the username for the embedded OIDC identity provider.
	// Override via HOLOS_DEX_INITIAL_ADMIN_USERNAME environment variable.
	DefaultUsername = "admin"

	// DefaultPassword is the password for all embedded test users.
	DefaultPassword = "verysecret"

	// Persona email constants for test users.
	EmailAdmin    = "admin@localhost"
	EmailPlatform = "platform@localhost"
	EmailProduct  = "product@localhost"
	EmailSRE      = "sre@localhost"
)

// TestUser defines a static test user registered with the embedded Dex provider.
type TestUser struct {
	// ID is a short unique identifier used as part of the Dex connector ID.
	ID string
	// Email is the user's email address, used as both username and email claim.
	Email string
	// Password is the user's password.
	Password string
	// Groups are the OIDC groups included in the user's token.
	Groups []string
	// DisplayName is the human-readable name shown on the Dex connector selection page.
	DisplayName string
	// UserID is the unique user identifier included in the sub claim.
	UserID string
}

// TestUsers lists all static test users registered with the embedded Dex provider.
// These users authenticate via the password connector on the Dex login form.
var TestUsers = []TestUser{
	{
		ID:          "admin",
		Email:       EmailAdmin,
		Password:    DefaultPassword,
		Groups:      []string{"owner"},
		DisplayName: "Admin (Owner)",
		UserID:      "test-admin-001",
	},
	{
		ID:          "platform",
		Email:       EmailPlatform,
		Password:    DefaultPassword,
		Groups:      []string{"owner"},
		DisplayName: "Platform Engineer (Owner)",
		UserID:      "test-platform-001",
	},
	{
		ID:          "product",
		Email:       EmailProduct,
		Password:    DefaultPassword,
		Groups:      []string{"editor"},
		DisplayName: "Product Engineer (Editor)",
		UserID:      "test-product-001",
	},
	{
		ID:          "sre",
		Email:       EmailSRE,
		Password:    DefaultPassword,
		Groups:      []string{"viewer"},
		DisplayName: "SRE (Viewer)",
		UserID:      "test-sre-001",
	},
}

// GetUsername returns the username for the embedded OIDC identity provider.
// It checks the HOLOS_DEX_INITIAL_ADMIN_USERNAME environment variable first,
// falling back to DefaultUsername if not set.
func GetUsername() string {
	if u := os.Getenv("HOLOS_DEX_INITIAL_ADMIN_USERNAME"); u != "" {
		return u
	}
	return DefaultUsername
}
