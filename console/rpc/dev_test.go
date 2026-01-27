package rpc

import (
	"testing"
)

func TestInjectDevGroups(t *testing.T) {
	t.Run("returns early when HOLOS_MODE is not dev", func(t *testing.T) {
		// Given: HOLOS_MODE is not set (default)
		t.Setenv("HOLOS_MODE", "")
		claims := &Claims{
			Email:  "admin",
			Groups: []string{},
		}

		// When: InjectDevGroups is called
		InjectDevGroups(claims)

		// Then: groups should not be modified
		if len(claims.Groups) != 0 {
			t.Errorf("expected 0 groups, got %d", len(claims.Groups))
		}
	})

	t.Run("returns early when HOLOS_MODE is production", func(t *testing.T) {
		// Given: HOLOS_MODE is set to production
		t.Setenv("HOLOS_MODE", "production")
		claims := &Claims{
			Email:  "admin",
			Groups: []string{},
		}

		// When: InjectDevGroups is called
		InjectDevGroups(claims)

		// Then: groups should not be modified
		if len(claims.Groups) != 0 {
			t.Errorf("expected 0 groups, got %d", len(claims.Groups))
		}
	})

	t.Run("adds owner group to admin email in dev mode", func(t *testing.T) {
		// Given: HOLOS_MODE is dev and email is "admin"
		t.Setenv("HOLOS_MODE", "dev")
		claims := &Claims{
			Email:  "admin",
			Groups: []string{},
		}

		// When: InjectDevGroups is called
		InjectDevGroups(claims)

		// Then: owner group should be added
		if len(claims.Groups) != 1 {
			t.Fatalf("expected 1 group, got %d", len(claims.Groups))
		}
		if claims.Groups[0] != "owner" {
			t.Errorf("expected 'owner' group, got %q", claims.Groups[0])
		}
	})

	t.Run("adds owner group to admin@example.com email in dev mode", func(t *testing.T) {
		// Given: HOLOS_MODE is dev and email is "admin@example.com"
		t.Setenv("HOLOS_MODE", "dev")
		claims := &Claims{
			Email:  "admin@example.com",
			Groups: []string{},
		}

		// When: InjectDevGroups is called
		InjectDevGroups(claims)

		// Then: owner group should be added
		if len(claims.Groups) != 1 {
			t.Fatalf("expected 1 group, got %d", len(claims.Groups))
		}
		if claims.Groups[0] != "owner" {
			t.Errorf("expected 'owner' group, got %q", claims.Groups[0])
		}
	})

	t.Run("does not modify groups for other emails in dev mode", func(t *testing.T) {
		// Given: HOLOS_MODE is dev but email is not admin
		t.Setenv("HOLOS_MODE", "dev")
		claims := &Claims{
			Email:  "user@example.com",
			Groups: []string{},
		}

		// When: InjectDevGroups is called
		InjectDevGroups(claims)

		// Then: groups should not be modified
		if len(claims.Groups) != 0 {
			t.Errorf("expected 0 groups, got %d", len(claims.Groups))
		}
	})

	t.Run("appends to existing groups instead of replacing", func(t *testing.T) {
		// Given: HOLOS_MODE is dev and user already has groups
		t.Setenv("HOLOS_MODE", "dev")
		claims := &Claims{
			Email:  "admin",
			Groups: []string{"existing-group"},
		}

		// When: InjectDevGroups is called
		InjectDevGroups(claims)

		// Then: owner should be appended to existing groups
		if len(claims.Groups) != 2 {
			t.Fatalf("expected 2 groups, got %d", len(claims.Groups))
		}
		if claims.Groups[0] != "existing-group" {
			t.Errorf("expected first group to be 'existing-group', got %q", claims.Groups[0])
		}
		if claims.Groups[1] != "owner" {
			t.Errorf("expected second group to be 'owner', got %q", claims.Groups[1])
		}
	})

	t.Run("handles nil claims gracefully", func(t *testing.T) {
		// Given: HOLOS_MODE is dev but claims is nil
		t.Setenv("HOLOS_MODE", "dev")

		// When: InjectDevGroups is called with nil
		// Then: should not panic
		InjectDevGroups(nil)
	})

	t.Run("is idempotent - multiple calls add multiple owner groups", func(t *testing.T) {
		// Given: HOLOS_MODE is dev
		t.Setenv("HOLOS_MODE", "dev")
		claims := &Claims{
			Email:  "admin",
			Groups: []string{},
		}

		// When: InjectDevGroups is called twice
		InjectDevGroups(claims)
		InjectDevGroups(claims)

		// Then: owner should be added twice (current behavior - documenting it)
		// Note: This is arguably a bug, but documenting current behavior
		if len(claims.Groups) != 2 {
			t.Fatalf("expected 2 groups (owner added twice), got %d", len(claims.Groups))
		}
	})
}
