package rbac

import (
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
	gm := NewGroupMapping(nil, nil, nil)

	t.Run("maps lowercase group names to roles", func(t *testing.T) {
		if got := gm.MapGroupToRole("viewer"); got != RoleViewer {
			t.Errorf("expected RoleViewer for 'viewer', got %v", got)
		}
		if got := gm.MapGroupToRole("editor"); got != RoleEditor {
			t.Errorf("expected RoleEditor for 'editor', got %v", got)
		}
		if got := gm.MapGroupToRole("owner"); got != RoleOwner {
			t.Errorf("expected RoleOwner for 'owner', got %v", got)
		}
	})

	t.Run("case-insensitive mapping", func(t *testing.T) {
		if got := gm.MapGroupToRole("VIEWER"); got != RoleViewer {
			t.Errorf("expected RoleViewer for 'VIEWER', got %v", got)
		}
		if got := gm.MapGroupToRole("Editor"); got != RoleEditor {
			t.Errorf("expected RoleEditor for 'Editor', got %v", got)
		}
		if got := gm.MapGroupToRole("OWNER"); got != RoleOwner {
			t.Errorf("expected RoleOwner for 'OWNER', got %v", got)
		}
	})

	t.Run("unknown groups return RoleUnspecified", func(t *testing.T) {
		if got := gm.MapGroupToRole("unknown"); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for 'unknown', got %v", got)
		}
		if got := gm.MapGroupToRole("admin"); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for 'admin', got %v", got)
		}
		if got := gm.MapGroupToRole(""); got != RoleUnspecified {
			t.Errorf("expected RoleUnspecified for empty string, got %v", got)
		}
	})
}

func TestMapGroupsToRoles(t *testing.T) {
	gm := NewGroupMapping(nil, nil, nil)

	t.Run("maps multiple groups to roles", func(t *testing.T) {
		groups := []string{"viewer", "editor"}
		roles := gm.MapGroupsToRoles(groups)

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
		roles := gm.MapGroupsToRoles(groups)

		if len(roles) != 1 {
			t.Fatalf("expected 1 role (viewer), got %d", len(roles))
		}
		if roles[0] != RoleViewer {
			t.Errorf("expected RoleViewer, got %v", roles[0])
		}
	})

	t.Run("empty groups returns empty roles", func(t *testing.T) {
		roles := gm.MapGroupsToRoles([]string{})
		if len(roles) != 0 {
			t.Errorf("expected empty roles, got %v", roles)
		}
	})

	t.Run("nil groups returns empty roles", func(t *testing.T) {
		roles := gm.MapGroupsToRoles(nil)
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

func TestRoleFromString(t *testing.T) {
	tests := []struct {
		input string
		want  Role
	}{
		{"viewer", RoleViewer},
		{"editor", RoleEditor},
		{"owner", RoleOwner},
		{"VIEWER", RoleViewer},
		{"Editor", RoleEditor},
		{"OWNER", RoleOwner},
		{"", RoleUnspecified},
		{"unknown", RoleUnspecified},
		{"admin", RoleUnspecified},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := RoleFromString(tt.input)
			if got != tt.want {
				t.Errorf("RoleFromString(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHasPermission_ProjectPermissions(t *testing.T) {
	t.Run("viewer can read and list projects", func(t *testing.T) {
		if !HasPermission(RoleViewer, PermissionProjectsRead) {
			t.Error("viewer should have projects:read permission")
		}
		if !HasPermission(RoleViewer, PermissionProjectsList) {
			t.Error("viewer should have projects:list permission")
		}
	})

	t.Run("viewer cannot write projects", func(t *testing.T) {
		if HasPermission(RoleViewer, PermissionProjectsWrite) {
			t.Error("viewer should not have projects:write permission")
		}
	})

	t.Run("editor can write projects", func(t *testing.T) {
		if !HasPermission(RoleEditor, PermissionProjectsWrite) {
			t.Error("editor should have projects:write permission")
		}
	})

	t.Run("editor cannot create projects", func(t *testing.T) {
		if HasPermission(RoleEditor, PermissionProjectsCreate) {
			t.Error("editor should not have projects:create permission")
		}
	})

	t.Run("owner can create projects", func(t *testing.T) {
		if !HasPermission(RoleOwner, PermissionProjectsCreate) {
			t.Error("owner should have projects:create permission")
		}
	})

	t.Run("owner can delete projects", func(t *testing.T) {
		if !HasPermission(RoleOwner, PermissionProjectsDelete) {
			t.Error("owner should have projects:delete permission")
		}
	})

	t.Run("owner can admin projects", func(t *testing.T) {
		if !HasPermission(RoleOwner, PermissionProjectsAdmin) {
			t.Error("owner should have projects:admin permission")
		}
	})
}

func TestCheckAccessGrants(t *testing.T) {
	t.Run("user grant allows access", func(t *testing.T) {
		err := CheckAccessGrants(
			"alice@example.com",
			nil,
			map[string]string{"alice@example.com": "viewer"},
			nil,
			PermissionProjectsRead,
		)
		if err != nil {
			t.Errorf("expected access granted, got: %v", err)
		}
	})

	t.Run("group grant allows access", func(t *testing.T) {
		err := CheckAccessGrants(
			"bob@example.com",
			[]string{"engineering"},
			nil,
			map[string]string{"engineering": "editor"},
			PermissionProjectsWrite,
		)
		if err != nil {
			t.Errorf("expected access granted via group, got: %v", err)
		}
	})

	t.Run("no grants denies access", func(t *testing.T) {
		err := CheckAccessGrants(
			"nobody@example.com",
			[]string{"unknown"},
			nil,
			nil,
			PermissionProjectsRead,
		)
		if err == nil {
			t.Fatal("expected PermissionDenied, got nil")
		}
	})

	t.Run("does not use platform roles", func(t *testing.T) {
		// User is in "owner" OIDC group but has no grants
		err := CheckAccessGrants(
			"nobody@example.com",
			[]string{"owner"},
			nil,
			nil,
			PermissionProjectsRead,
		)
		if err == nil {
			t.Fatal("expected PermissionDenied (no platform role fallback), got nil")
		}
	})
}

func TestCheckAccessSharing(t *testing.T) {
	gm := NewGroupMapping(nil, nil, nil)

	t.Run("user email match grants access", func(t *testing.T) {
		err := gm.CheckAccessSharing(
			"alice@example.com",
			[]string{},
			map[string]string{"alice@example.com": "viewer"},
			nil,
			PermissionSecretsRead,
		)
		if err != nil {
			t.Errorf("expected access granted via email, got: %v", err)
		}
	})

	t.Run("user email match is case-insensitive", func(t *testing.T) {
		err := gm.CheckAccessSharing(
			"Alice@Example.COM",
			[]string{},
			map[string]string{"alice@example.com": "viewer"},
			nil,
			PermissionSecretsRead,
		)
		if err != nil {
			t.Errorf("expected access granted via case-insensitive email, got: %v", err)
		}
	})

	t.Run("group match in shareGroups grants access", func(t *testing.T) {
		err := gm.CheckAccessSharing(
			"bob@example.com",
			[]string{"platform-team"},
			nil,
			map[string]string{"platform-team": "editor"},
			PermissionSecretsWrite,
		)
		if err != nil {
			t.Errorf("expected access granted via group share, got: %v", err)
		}
	})

	t.Run("group match in shareGroups is case-insensitive", func(t *testing.T) {
		err := gm.CheckAccessSharing(
			"bob@example.com",
			[]string{"Platform-Team"},
			nil,
			map[string]string{"platform-team": "viewer"},
			PermissionSecretsRead,
		)
		if err != nil {
			t.Errorf("expected access granted via case-insensitive group share, got: %v", err)
		}
	})

	t.Run("highest role wins across all sources", func(t *testing.T) {
		// User has viewer via email, but owner via group share
		// Should allow delete (owner permission)
		err := gm.CheckAccessSharing(
			"alice@example.com",
			[]string{"ops"},
			map[string]string{"alice@example.com": "viewer"},
			map[string]string{"ops": "owner"},
			PermissionSecretsDelete,
		)
		if err != nil {
			t.Errorf("expected access granted via highest role, got: %v", err)
		}
	})

	t.Run("denies when no source grants sufficient permission", func(t *testing.T) {
		err := gm.CheckAccessSharing(
			"alice@example.com",
			[]string{},
			map[string]string{"alice@example.com": "viewer"},
			nil,
			PermissionSecretsWrite,
		)
		if err == nil {
			t.Fatal("expected PermissionDenied, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connectErr.Code())
		}
	})

	t.Run("denies when no grants at all", func(t *testing.T) {
		err := gm.CheckAccessSharing(
			"nobody@example.com",
			[]string{"unknown-group"},
			nil,
			nil,
			PermissionSecretsRead,
		)
		if err == nil {
			t.Fatal("expected PermissionDenied, got nil")
		}
	})

	t.Run("platform viewer role grants read access", func(t *testing.T) {
		// User has no per-secret grants, but belongs to the "viewer" OIDC group
		err := gm.CheckAccessSharing(
			"nobody@example.com",
			[]string{"viewer"},
			nil,
			nil,
			PermissionSecretsRead,
		)
		if err != nil {
			t.Errorf("expected access granted via platform viewer role, got: %v", err)
		}
	})

	t.Run("platform viewer role denies write access", func(t *testing.T) {
		err := gm.CheckAccessSharing(
			"nobody@example.com",
			[]string{"viewer"},
			nil,
			nil,
			PermissionSecretsWrite,
		)
		if err == nil {
			t.Fatal("expected PermissionDenied, got nil")
		}
	})

	t.Run("platform editor role grants write access", func(t *testing.T) {
		err := gm.CheckAccessSharing(
			"nobody@example.com",
			[]string{"editor"},
			nil,
			nil,
			PermissionSecretsWrite,
		)
		if err != nil {
			t.Errorf("expected access granted via platform editor role, got: %v", err)
		}
	})

	t.Run("platform owner role grants delete access", func(t *testing.T) {
		err := gm.CheckAccessSharing(
			"nobody@example.com",
			[]string{"owner"},
			nil,
			nil,
			PermissionSecretsDelete,
		)
		if err != nil {
			t.Errorf("expected access granted via platform owner role, got: %v", err)
		}
	})

	t.Run("platform role combined with per-secret grant uses highest", func(t *testing.T) {
		// Per-secret grant gives viewer, platform role gives editor
		// Should allow write (editor permission)
		err := gm.CheckAccessSharing(
			"alice@example.com",
			[]string{"editor"},
			map[string]string{"alice@example.com": "viewer"},
			nil,
			PermissionSecretsWrite,
		)
		if err != nil {
			t.Errorf("expected access granted via highest role (platform editor), got: %v", err)
		}
	})

	t.Run("custom platform groups grant access", func(t *testing.T) {
		customGM := NewGroupMapping([]string{"readers"}, []string{"writers"}, []string{"admins"})
		err := customGM.CheckAccessSharing(
			"nobody@example.com",
			[]string{"writers"},
			nil,
			nil,
			PermissionSecretsWrite,
		)
		if err != nil {
			t.Errorf("expected access granted via custom platform editor group, got: %v", err)
		}
	})
}
