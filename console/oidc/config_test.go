package oidc_test

import (
	"os"
	"testing"

	"github.com/holos-run/holos-console/console/oidc"
)

func TestGetUsername(t *testing.T) {
	// Test default username
	os.Unsetenv("HOLOS_DEX_INITIAL_ADMIN_USERNAME")
	if got := oidc.GetUsername(); got != oidc.DefaultUsername {
		t.Errorf("GetUsername() = %q, want %q", got, oidc.DefaultUsername)
	}

	// Test environment variable override
	os.Setenv("HOLOS_DEX_INITIAL_ADMIN_USERNAME", "custom-user")
	defer os.Unsetenv("HOLOS_DEX_INITIAL_ADMIN_USERNAME")
	if got := oidc.GetUsername(); got != "custom-user" {
		t.Errorf("GetUsername() = %q, want %q", got, "custom-user")
	}
}

func TestDefaultValues(t *testing.T) {
	// Verify default constants are set
	if oidc.DefaultUsername == "" {
		t.Error("DefaultUsername is empty")
	}
}
