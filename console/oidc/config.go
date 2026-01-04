// Package oidc provides an embedded OIDC identity provider using Dex.
package oidc

import "os"

const (
	// DefaultPassword is the password for the embedded OIDC identity provider.
	// Override via HOLOS_DEX_INITIAL_ADMIN_PASSWORD environment variable.
	DefaultPassword = "verysecret"

	// DefaultUsername is the username for the embedded OIDC identity provider.
	// Override via HOLOS_DEX_INITIAL_ADMIN_USERNAME environment variable.
	DefaultUsername = "admin"
)

// GetPassword returns the password for the embedded OIDC identity provider.
// It checks the HOLOS_DEX_INITIAL_ADMIN_PASSWORD environment variable first,
// falling back to DefaultPassword if not set.
func GetPassword() string {
	if p := os.Getenv("HOLOS_DEX_INITIAL_ADMIN_PASSWORD"); p != "" {
		return p
	}
	return DefaultPassword
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
