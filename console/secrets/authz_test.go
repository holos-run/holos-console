package secrets

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
)

func TestCheckAccess(t *testing.T) {
	t.Run("allows access when user has matching group", func(t *testing.T) {
		// Given: User groups ["developers", "readers"], allowed ["admin", "readers"]
		userGroups := []string{"developers", "readers"}
		allowedGroups := []string{"admin", "readers"}

		// When: CheckAccess is called
		err := CheckAccess(userGroups, allowedGroups)

		// Then: Returns nil (access granted)
		if err != nil {
			t.Errorf("expected nil error (access granted), got %v", err)
		}
	})

	t.Run("denies access when no matching groups", func(t *testing.T) {
		// Given: User groups ["developers"], allowed ["admin", "ops"]
		userGroups := []string{"developers"}
		allowedGroups := []string{"admin", "ops"}

		// When: CheckAccess is called
		err := CheckAccess(userGroups, allowedGroups)

		// Then: Returns PermissionDenied error
		if err == nil {
			t.Fatal("expected PermissionDenied error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connectErr.Code())
		}
	})

	t.Run("denies access when user has no groups", func(t *testing.T) {
		// Given: User groups [], allowed ["admin"]
		userGroups := []string{}
		allowedGroups := []string{"admin"}

		// When: CheckAccess is called
		err := CheckAccess(userGroups, allowedGroups)

		// Then: Returns PermissionDenied error
		if err == nil {
			t.Fatal("expected PermissionDenied error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connectErr.Code())
		}
	})

	t.Run("denies access when secret has no allowed groups", func(t *testing.T) {
		// Given: User groups ["admin"], allowed []
		userGroups := []string{"admin"}
		allowedGroups := []string{}

		// When: CheckAccess is called
		err := CheckAccess(userGroups, allowedGroups)

		// Then: Returns PermissionDenied error
		if err == nil {
			t.Fatal("expected PermissionDenied error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connectErr.Code())
		}
	})

	t.Run("error message includes allowed groups", func(t *testing.T) {
		// Given: Denied access with allowed ["admin", "ops"]
		userGroups := []string{"developers"}
		allowedGroups := []string{"admin", "ops"}

		// When: Error is returned
		err := CheckAccess(userGroups, allowedGroups)

		// Then: Message contains "not a member of: [admin ops]"
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "not a member of") {
			t.Errorf("expected message to contain 'not a member of', got %q", msg)
		}
		if !strings.Contains(msg, "admin") || !strings.Contains(msg, "ops") {
			t.Errorf("expected message to contain allowed groups 'admin' and 'ops', got %q", msg)
		}
	})
}
