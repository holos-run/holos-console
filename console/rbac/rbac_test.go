package rbac

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
)

func TestRoleConstants(t *testing.T) {
	t.Run("defines viewer, editor, owner roles", func(t *testing.T) {
		// Verify role constants are defined and distinct
		if RoleViewer == RoleUnspecified {
			t.Error("RoleViewer should not equal RoleUnspecified")
		}
		if RoleEditor == RoleUnspecified {
			t.Error("RoleEditor should not equal RoleUnspecified")
		}
		if RoleOwner == RoleUnspecified {
			t.Error("RoleOwner should not equal RoleUnspecified")
		}
		if RoleViewer == RoleEditor {
			t.Error("RoleViewer should not equal RoleEditor")
		}
		if RoleViewer == RoleOwner {
			t.Error("RoleViewer should not equal RoleOwner")
		}
		if RoleEditor == RoleOwner {
			t.Error("RoleEditor should not equal RoleOwner")
		}
	})
}

func TestPermissionConstants(t *testing.T) {
	t.Run("defines secrets permissions", func(t *testing.T) {
		// Verify permission constants are defined and distinct
		permissions := []Permission{
			PermissionSecretsRead,
			PermissionSecretsList,
			PermissionSecretsWrite,
			PermissionSecretsDelete,
			PermissionSecretsAdmin,
		}
		for i, p1 := range permissions {
			if p1 == PermissionUnspecified {
				t.Errorf("Permission %d should not equal PermissionUnspecified", i)
			}
			for j, p2 := range permissions {
				if i != j && p1 == p2 {
					t.Errorf("Permission %d should not equal Permission %d", i, j)
				}
			}
		}
	})
}

func TestHasPermission(t *testing.T) {
	t.Run("viewer can read and list secrets", func(t *testing.T) {
		if !HasPermission(RoleViewer, PermissionSecretsRead) {
			t.Error("viewer should have secrets:read permission")
		}
		if !HasPermission(RoleViewer, PermissionSecretsList) {
			t.Error("viewer should have secrets:list permission")
		}
	})

	t.Run("viewer cannot write, delete, or admin secrets", func(t *testing.T) {
		if HasPermission(RoleViewer, PermissionSecretsWrite) {
			t.Error("viewer should not have secrets:write permission")
		}
		if HasPermission(RoleViewer, PermissionSecretsDelete) {
			t.Error("viewer should not have secrets:delete permission")
		}
		if HasPermission(RoleViewer, PermissionSecretsAdmin) {
			t.Error("viewer should not have secrets:admin permission")
		}
	})

	t.Run("editor can read, list, and write secrets", func(t *testing.T) {
		if !HasPermission(RoleEditor, PermissionSecretsRead) {
			t.Error("editor should have secrets:read permission")
		}
		if !HasPermission(RoleEditor, PermissionSecretsList) {
			t.Error("editor should have secrets:list permission")
		}
		if !HasPermission(RoleEditor, PermissionSecretsWrite) {
			t.Error("editor should have secrets:write permission")
		}
	})

	t.Run("editor cannot delete or admin secrets", func(t *testing.T) {
		if HasPermission(RoleEditor, PermissionSecretsDelete) {
			t.Error("editor should not have secrets:delete permission")
		}
		if HasPermission(RoleEditor, PermissionSecretsAdmin) {
			t.Error("editor should not have secrets:admin permission")
		}
	})

	t.Run("owner has all permissions", func(t *testing.T) {
		if !HasPermission(RoleOwner, PermissionSecretsRead) {
			t.Error("owner should have secrets:read permission")
		}
		if !HasPermission(RoleOwner, PermissionSecretsList) {
			t.Error("owner should have secrets:list permission")
		}
		if !HasPermission(RoleOwner, PermissionSecretsWrite) {
			t.Error("owner should have secrets:write permission")
		}
		if !HasPermission(RoleOwner, PermissionSecretsDelete) {
			t.Error("owner should have secrets:delete permission")
		}
		if !HasPermission(RoleOwner, PermissionSecretsAdmin) {
			t.Error("owner should have secrets:admin permission")
		}
	})

	t.Run("unspecified role has no permissions", func(t *testing.T) {
		if HasPermission(RoleUnspecified, PermissionSecretsRead) {
			t.Error("unspecified role should not have secrets:read permission")
		}
		if HasPermission(RoleUnspecified, PermissionSecretsList) {
			t.Error("unspecified role should not have secrets:list permission")
		}
	})
}

func TestMapGroupToRole(t *testing.T) {
	t.Run("maps lowercase group names to roles", func(t *testing.T) {
		if got := MapGroupToRole("viewer"); got != RoleViewer {
			t.Errorf("expected RoleViewer for 'viewer', got %v", got)
		}
		if got := MapGroupToRole("editor"); got != RoleEditor {
			t.Errorf("expected RoleEditor for 'editor', got %v", got)
		}
		if got := MapGroupToRole("owner"); got != RoleOwner {
			t.Errorf("expected RoleOwner for 'owner', got %v", got)
		}
	})

	t.Run("case-insensitive mapping", func(t *testing.T) {
		if got := MapGroupToRole("VIEWER"); got != RoleViewer {
			t.Errorf("expected RoleViewer for 'VIEWER', got %v", got)
		}
		if got := MapGroupToRole("Editor"); got != RoleEditor {
			t.Errorf("expected RoleEditor for 'Editor', got %v", got)
		}
		if got := MapGroupToRole("OWNER"); got != RoleOwner {
			t.Errorf("expected RoleOwner for 'OWNER', got %v", got)
		}
	})

	t.Run("unknown groups return RoleUnspecified", func(t *testing.T) {
		if got := MapGroupToRole("unknown"); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for 'unknown', got %v", got)
		}
		if got := MapGroupToRole("admin"); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for 'admin', got %v", got)
		}
		if got := MapGroupToRole(""); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for empty string, got %v", got)
		}
	})
}

func TestMapGroupsToRoles(t *testing.T) {
	t.Run("maps multiple groups to roles", func(t *testing.T) {
		groups := []string{"viewer", "editor"}
		roles := MapGroupsToRoles(groups)

		if len(roles) != 2 {
			t.Fatalf("expected 2 roles, got %d", len(roles))
		}
		// Check both roles are present (order may vary)
		hasViewer := false
		hasEditor := false
		for _, r := range roles {
			if r == RoleViewer {
				hasViewer = true
			}
			if r == RoleEditor {
				hasEditor = true
			}
		}
		if !hasViewer {
			t.Error("expected RoleViewer in result")
		}
		if !hasEditor {
			t.Error("expected RoleEditor in result")
		}
	})

	t.Run("skips unknown groups", func(t *testing.T) {
		groups := []string{"viewer", "unknown", "admin"}
		roles := MapGroupsToRoles(groups)

		if len(roles) != 1 {
			t.Fatalf("expected 1 role (viewer), got %d", len(roles))
		}
		if roles[0] != RoleViewer {
			t.Errorf("expected RoleViewer, got %v", roles[0])
		}
	})

	t.Run("empty groups returns empty roles", func(t *testing.T) {
		roles := MapGroupsToRoles([]string{})
		if len(roles) != 0 {
			t.Errorf("expected empty roles, got %v", roles)
		}
	})

	t.Run("nil groups returns empty roles", func(t *testing.T) {
		roles := MapGroupsToRoles(nil)
		if roles == nil {
			t.Error("expected non-nil empty slice, got nil")
		}
		if len(roles) != 0 {
			t.Errorf("expected empty roles, got %v", roles)
		}
	})
}

func TestNewGroupMapping(t *testing.T) {
	t.Run("default mapping uses viewer, editor, owner groups", func(t *testing.T) {
		gm := NewGroupMapping(nil, nil, nil)
		if got := gm.MapGroupToRole("viewer"); got != RoleViewer {
			t.Errorf("expected RoleViewer for 'viewer', got %v", got)
		}
		if got := gm.MapGroupToRole("editor"); got != RoleEditor {
			t.Errorf("expected RoleEditor for 'editor', got %v", got)
		}
		if got := gm.MapGroupToRole("owner"); got != RoleOwner {
			t.Errorf("expected RoleOwner for 'owner', got %v", got)
		}
		if got := gm.MapGroupToRole("unknown"); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for 'unknown', got %v", got)
		}
	})

	t.Run("custom viewer groups", func(t *testing.T) {
		gm := NewGroupMapping([]string{"readers", "readonly"}, nil, nil)
		// Custom groups should map to the role
		if got := gm.MapGroupToRole("readers"); got != RoleViewer {
			t.Errorf("expected RoleViewer for 'readers', got %v", got)
		}
		if got := gm.MapGroupToRole("readonly"); got != RoleViewer {
			t.Errorf("expected RoleViewer for 'readonly', got %v", got)
		}
		// Default "viewer" should no longer map when custom groups are set
		if got := gm.MapGroupToRole("viewer"); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for 'viewer' when custom viewer groups set, got %v", got)
		}
		// Other defaults should still work
		if got := gm.MapGroupToRole("editor"); got != RoleEditor {
			t.Errorf("expected RoleEditor for 'editor', got %v", got)
		}
		if got := gm.MapGroupToRole("owner"); got != RoleOwner {
			t.Errorf("expected RoleOwner for 'owner', got %v", got)
		}
	})

	t.Run("custom editor groups", func(t *testing.T) {
		gm := NewGroupMapping(nil, []string{"writers", "developers"}, nil)
		if got := gm.MapGroupToRole("writers"); got != RoleEditor {
			t.Errorf("expected RoleEditor for 'writers', got %v", got)
		}
		if got := gm.MapGroupToRole("developers"); got != RoleEditor {
			t.Errorf("expected RoleEditor for 'developers', got %v", got)
		}
		if got := gm.MapGroupToRole("editor"); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for 'editor' when custom editor groups set, got %v", got)
		}
	})

	t.Run("custom owner groups", func(t *testing.T) {
		gm := NewGroupMapping(nil, nil, []string{"admins", "superusers"})
		if got := gm.MapGroupToRole("admins"); got != RoleOwner {
			t.Errorf("expected RoleOwner for 'admins', got %v", got)
		}
		if got := gm.MapGroupToRole("superusers"); got != RoleOwner {
			t.Errorf("expected RoleOwner for 'superusers', got %v", got)
		}
		if got := gm.MapGroupToRole("owner"); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for 'owner' when custom owner groups set, got %v", got)
		}
	})

	t.Run("all custom groups", func(t *testing.T) {
		gm := NewGroupMapping(
			[]string{"readers"},
			[]string{"writers"},
			[]string{"admins"},
		)
		if got := gm.MapGroupToRole("readers"); got != RoleViewer {
			t.Errorf("expected RoleViewer for 'readers', got %v", got)
		}
		if got := gm.MapGroupToRole("writers"); got != RoleEditor {
			t.Errorf("expected RoleEditor for 'writers', got %v", got)
		}
		if got := gm.MapGroupToRole("admins"); got != RoleOwner {
			t.Errorf("expected RoleOwner for 'admins', got %v", got)
		}
		// None of the defaults should work
		if got := gm.MapGroupToRole("viewer"); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for 'viewer', got %v", got)
		}
		if got := gm.MapGroupToRole("editor"); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for 'editor', got %v", got)
		}
		if got := gm.MapGroupToRole("owner"); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for 'owner', got %v", got)
		}
	})

	t.Run("case-insensitive matching with custom groups", func(t *testing.T) {
		gm := NewGroupMapping([]string{"Readers"}, nil, nil)
		if got := gm.MapGroupToRole("readers"); got != RoleViewer {
			t.Errorf("expected RoleViewer for 'readers', got %v", got)
		}
		if got := gm.MapGroupToRole("READERS"); got != RoleViewer {
			t.Errorf("expected RoleViewer for 'READERS', got %v", got)
		}
	})
}

func TestGroupMappingMapGroupsToRoles(t *testing.T) {
	t.Run("maps custom groups to roles", func(t *testing.T) {
		gm := NewGroupMapping([]string{"readers"}, []string{"writers"}, nil)
		roles := gm.MapGroupsToRoles([]string{"readers", "writers", "unknown"})
		if len(roles) != 2 {
			t.Fatalf("expected 2 roles, got %d", len(roles))
		}
	})
}

func TestGroupMappingCheckAccess(t *testing.T) {
	t.Run("grants access with custom group mapping", func(t *testing.T) {
		gm := NewGroupMapping([]string{"readers"}, nil, nil)
		// User is in "readers" group, which maps to viewer role
		// Secret allows "readers" (which maps to viewer)
		err := gm.CheckAccess([]string{"readers"}, []string{"readers"}, PermissionSecretsRead)
		if err != nil {
			t.Errorf("expected access granted, got error: %v", err)
		}
	})

	t.Run("denies access when custom group not in allowed roles", func(t *testing.T) {
		gm := NewGroupMapping([]string{"readers"}, []string{"writers"}, nil)
		// User is in "readers" group (viewer role), needs write permission
		err := gm.CheckAccess([]string{"readers"}, []string{"readers"}, PermissionSecretsWrite)
		if err == nil {
			t.Fatal("expected PermissionDenied error, got nil")
		}
	})
}

func TestParseGroups(t *testing.T) {
	t.Run("parses comma-separated groups", func(t *testing.T) {
		groups := ParseGroups("readers,writers,admins")
		if len(groups) != 3 {
			t.Fatalf("expected 3 groups, got %d: %v", len(groups), groups)
		}
		if groups[0] != "readers" || groups[1] != "writers" || groups[2] != "admins" {
			t.Errorf("unexpected groups: %v", groups)
		}
	})

	t.Run("trims whitespace", func(t *testing.T) {
		groups := ParseGroups(" readers , writers , admins ")
		if len(groups) != 3 {
			t.Fatalf("expected 3 groups, got %d: %v", len(groups), groups)
		}
		if groups[0] != "readers" || groups[1] != "writers" || groups[2] != "admins" {
			t.Errorf("unexpected groups: %v", groups)
		}
	})

	t.Run("returns nil for empty string", func(t *testing.T) {
		groups := ParseGroups("")
		if groups != nil {
			t.Errorf("expected nil for empty string, got %v", groups)
		}
	})

	t.Run("single group", func(t *testing.T) {
		groups := ParseGroups("admins")
		if len(groups) != 1 || groups[0] != "admins" {
			t.Errorf("expected [admins], got %v", groups)
		}
	})
}

func TestCheckAccess(t *testing.T) {
	t.Run("grants access when user has matching role with permission", func(t *testing.T) {
		// User is in "viewer" group, secret allows "viewer" role
		userGroups := []string{"viewer"}
		allowedRoles := []string{"viewer"}

		err := CheckAccess(userGroups, allowedRoles, PermissionSecretsRead)
		if err != nil {
			t.Errorf("expected access granted, got error: %v", err)
		}
	})

	t.Run("grants access when user has higher-permission role", func(t *testing.T) {
		// User is in "owner" group, secret allows "viewer" role - owner should still have access
		userGroups := []string{"owner"}
		allowedRoles := []string{"viewer"}

		err := CheckAccess(userGroups, allowedRoles, PermissionSecretsRead)
		if err != nil {
			t.Errorf("expected access granted for owner, got error: %v", err)
		}
	})

	t.Run("denies access when user role lacks permission", func(t *testing.T) {
		// User is in "viewer" group, but requesting write permission
		userGroups := []string{"viewer"}
		allowedRoles := []string{"viewer"}

		err := CheckAccess(userGroups, allowedRoles, PermissionSecretsWrite)
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

	t.Run("denies access when user is not in allowed roles", func(t *testing.T) {
		// User has no recognized role groups
		userGroups := []string{"developers"}
		allowedRoles := []string{"viewer", "editor"}

		err := CheckAccess(userGroups, allowedRoles, PermissionSecretsRead)
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

	t.Run("denies access when no allowed roles specified", func(t *testing.T) {
		userGroups := []string{"owner"}
		allowedRoles := []string{}

		err := CheckAccess(userGroups, allowedRoles, PermissionSecretsRead)
		if err == nil {
			t.Fatal("expected PermissionDenied error, got nil")
		}
	})

	t.Run("error message includes allowed roles", func(t *testing.T) {
		userGroups := []string{"developers"}
		allowedRoles := []string{"viewer", "editor"}

		err := CheckAccess(userGroups, allowedRoles, PermissionSecretsRead)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "viewer") || !strings.Contains(msg, "editor") {
			t.Errorf("expected message to contain allowed roles, got %q", msg)
		}
	})

	t.Run("case-insensitive role matching", func(t *testing.T) {
		// User has uppercase group, allowed roles are lowercase
		userGroups := []string{"VIEWER"}
		allowedRoles := []string{"viewer"}

		err := CheckAccess(userGroups, allowedRoles, PermissionSecretsRead)
		if err != nil {
			t.Errorf("expected access granted with case-insensitive match, got error: %v", err)
		}
	})
}
