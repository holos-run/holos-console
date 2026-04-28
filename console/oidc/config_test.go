package oidc_test

import (
	"os"
	"testing"

	"github.com/holos-run/holos-console/console/oidc"
)

func TestGetUsername(t *testing.T) {
	// Test default username
	if err := os.Unsetenv("HOLOS_DEX_INITIAL_ADMIN_USERNAME"); err != nil {
		t.Fatalf("unset env: %v", err)
	}
	if got := oidc.GetUsername(); got != oidc.DefaultUsername {
		t.Errorf("GetUsername() = %q, want %q", got, oidc.DefaultUsername)
	}

	// Test environment variable override
	if err := os.Setenv("HOLOS_DEX_INITIAL_ADMIN_USERNAME", "custom-user"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("HOLOS_DEX_INITIAL_ADMIN_USERNAME")
	})
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

func TestTestUsers_Count(t *testing.T) {
	// Four personas: admin, platform, product, sre
	if got := len(oidc.TestUsers); got != 4 {
		t.Errorf("TestUsers count = %d, want 4", got)
	}
}

func TestTestUsers_RequiredFields(t *testing.T) {
	for _, u := range oidc.TestUsers {
		t.Run(u.ID, func(t *testing.T) {
			if u.ID == "" {
				t.Error("ID is empty")
			}
			if u.Email == "" {
				t.Error("Email is empty")
			}
			if u.Password == "" {
				t.Error("Password is empty")
			}
			if len(u.Groups) == 0 {
				t.Error("Groups is empty")
			}
			if u.DisplayName == "" {
				t.Error("DisplayName is empty")
			}
			if u.UserID == "" {
				t.Error("UserID is empty")
			}
		})
	}
}

func TestTestUsers_UniqueIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, u := range oidc.TestUsers {
		if seen[u.ID] {
			t.Errorf("duplicate TestUser ID: %q", u.ID)
		}
		seen[u.ID] = true
	}
}

func TestTestUsers_UniqueEmails(t *testing.T) {
	seen := make(map[string]bool)
	for _, u := range oidc.TestUsers {
		if seen[u.Email] {
			t.Errorf("duplicate TestUser Email: %q", u.Email)
		}
		seen[u.Email] = true
	}
}

func TestTestUsers_UniqueUserIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, u := range oidc.TestUsers {
		if seen[u.UserID] {
			t.Errorf("duplicate TestUser UserID: %q", u.UserID)
		}
		seen[u.UserID] = true
	}
}

func TestTestUsers_Personas(t *testing.T) {
	// Build a map for easy lookup
	users := make(map[string]oidc.TestUser)
	for _, u := range oidc.TestUsers {
		users[u.ID] = u
	}

	tests := []struct {
		id     string
		email  string
		groups []string
	}{
		{id: "admin", email: oidc.EmailAdmin, groups: []string{"owner"}},
		{id: "platform", email: oidc.EmailPlatform, groups: []string{"owner"}},
		{id: "product", email: oidc.EmailProduct, groups: []string{"editor"}},
		{id: "sre", email: oidc.EmailSRE, groups: []string{"viewer"}},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			u, ok := users[tt.id]
			if !ok {
				t.Fatalf("TestUser %q not found", tt.id)
			}
			if u.Email != tt.email {
				t.Errorf("Email = %q, want %q", u.Email, tt.email)
			}
			if len(u.Groups) != len(tt.groups) {
				t.Fatalf("Groups length = %d, want %d", len(u.Groups), len(tt.groups))
			}
			for i, g := range u.Groups {
				if g != tt.groups[i] {
					t.Errorf("Groups[%d] = %q, want %q", i, g, tt.groups[i])
				}
			}
			if u.Password != oidc.DefaultPassword {
				t.Errorf("Password = %q, want %q", u.Password, oidc.DefaultPassword)
			}
		})
	}
}

func TestEmailConstants(t *testing.T) {
	if oidc.EmailAdmin != "admin@localhost" {
		t.Errorf("EmailAdmin = %q, want %q", oidc.EmailAdmin, "admin@localhost")
	}
	if oidc.EmailPlatform != "platform@localhost" {
		t.Errorf("EmailPlatform = %q, want %q", oidc.EmailPlatform, "platform@localhost")
	}
	if oidc.EmailProduct != "product@localhost" {
		t.Errorf("EmailProduct = %q, want %q", oidc.EmailProduct, "product@localhost")
	}
	if oidc.EmailSRE != "sre@localhost" {
		t.Errorf("EmailSRE = %q, want %q", oidc.EmailSRE, "sre@localhost")
	}
}
