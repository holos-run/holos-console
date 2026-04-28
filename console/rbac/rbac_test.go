package rbac

import (
	"testing"
)

func TestRoleConstantsAreDistinct(t *testing.T) {
	if RoleViewer == RoleUnspecified || RoleEditor == RoleUnspecified || RoleOwner == RoleUnspecified {
		t.Fatal("role constants must not collapse to RoleUnspecified")
	}
	if RoleViewer == RoleEditor || RoleViewer == RoleOwner || RoleEditor == RoleOwner {
		t.Fatal("role constants must be pairwise distinct")
	}
}

func TestRoleFromString(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want Role
	}{
		{"viewer", RoleViewer},
		{"VIEWER", RoleViewer},
		{"Editor", RoleEditor},
		{"OWNER", RoleOwner},
		{"", RoleUnspecified},
		{"admin", RoleUnspecified},
	} {
		if got := RoleFromString(tt.in); got != tt.want {
			t.Errorf("RoleFromString(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestRoleLevelOrdering(t *testing.T) {
	if RoleLevel(RoleUnspecified) >= RoleLevel(RoleViewer) ||
		RoleLevel(RoleViewer) >= RoleLevel(RoleEditor) ||
		RoleLevel(RoleEditor) >= RoleLevel(RoleOwner) {
		t.Fatal("RoleLevel must be strictly increasing: Unspecified < Viewer < Editor < Owner")
	}
}

func TestBestRoleFromGrants(t *testing.T) {
	t.Run("user grant resolves to its role", func(t *testing.T) {
		got := BestRoleFromGrants("alice@example.com", nil,
			map[string]string{"alice@example.com": "editor"}, nil)
		if got != RoleEditor {
			t.Fatalf("got %v, want RoleEditor", got)
		}
	})

	t.Run("group grant resolves to its role", func(t *testing.T) {
		got := BestRoleFromGrants("bob@example.com", []string{"engineering"},
			nil, map[string]string{"engineering": "viewer"})
		if got != RoleViewer {
			t.Fatalf("got %v, want RoleViewer", got)
		}
	})

	t.Run("highest grant wins", func(t *testing.T) {
		got := BestRoleFromGrants("carol@example.com", []string{"engineering"},
			map[string]string{"carol@example.com": "viewer"},
			map[string]string{"engineering": "owner"})
		if got != RoleOwner {
			t.Fatalf("got %v, want RoleOwner", got)
		}
	})

	t.Run("no grants returns RoleUnspecified", func(t *testing.T) {
		got := BestRoleFromGrants("nobody@example.com", nil, nil, nil)
		if got != RoleUnspecified {
			t.Fatalf("got %v, want RoleUnspecified", got)
		}
	})
}

func TestCheckAccessGrantsForProjectSettingsRead(t *testing.T) {
	t.Run("viewer grant allows project settings read", func(t *testing.T) {
		err := CheckAccessGrants("alice@example.com", nil,
			map[string]string{"alice@example.com": "viewer"}, nil,
			PermissionProjectSettingsRead)
		if err != nil {
			t.Fatalf("expected access granted, got %v", err)
		}
	})

	t.Run("group grant allows project settings read", func(t *testing.T) {
		err := CheckAccessGrants("bob@example.com", []string{"engineering"},
			nil, map[string]string{"engineering": "editor"},
			PermissionProjectSettingsRead)
		if err != nil {
			t.Fatalf("expected access granted via role, got %v", err)
		}
	})

	t.Run("no grants denies access", func(t *testing.T) {
		err := CheckAccessGrants("nobody@example.com", []string{"unknown"}, nil, nil,
			PermissionProjectSettingsRead)
		if err == nil {
			t.Fatal("expected PermissionDenied, got nil")
		}
	})
}

func TestCheckCascadeAccessForOrgProjectSettings(t *testing.T) {
	t.Run("org owner can enable deployments via cascade", func(t *testing.T) {
		err := CheckCascadeAccess("owner@example.com", nil,
			map[string]string{"owner@example.com": "owner"}, nil,
			PermissionProjectDeploymentsEnable, OrgCascadeProjectSettingsPerms)
		if err != nil {
			t.Fatalf("expected access granted for OWNER, got %v", err)
		}
	})

	t.Run("org editor cannot enable deployments via cascade", func(t *testing.T) {
		err := CheckCascadeAccess("editor@example.com", nil,
			map[string]string{"editor@example.com": "editor"}, nil,
			PermissionProjectDeploymentsEnable, OrgCascadeProjectSettingsPerms)
		if err == nil {
			t.Fatal("expected PermissionDenied for EDITOR, got nil")
		}
	})
}
