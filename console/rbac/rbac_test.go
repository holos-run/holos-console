package rbac

import (
	"testing"
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

// --- Option B: Role-per-scope cascade table tests ---

func TestProjectCascadeSecretPerms(t *testing.T) {
	t.Run("project viewer can list secrets", func(t *testing.T) {
		if !HasCascadePermission(RoleViewer, PermissionSecretsList, ProjectCascadeSecretPerms) {
			t.Error("project viewer should have secrets:list via cascade")
		}
	})

	t.Run("project viewer cannot read secret data", func(t *testing.T) {
		if HasCascadePermission(RoleViewer, PermissionSecretsRead, ProjectCascadeSecretPerms) {
			t.Error("project viewer should not have secrets:read via cascade")
		}
	})

	t.Run("project viewer cannot write secrets", func(t *testing.T) {
		if HasCascadePermission(RoleViewer, PermissionSecretsWrite, ProjectCascadeSecretPerms) {
			t.Error("project viewer should not have secrets:write via cascade")
		}
	})

	t.Run("project viewer cannot delete secrets", func(t *testing.T) {
		if HasCascadePermission(RoleViewer, PermissionSecretsDelete, ProjectCascadeSecretPerms) {
			t.Error("project viewer should not have secrets:delete via cascade")
		}
	})

	t.Run("project viewer cannot admin secrets", func(t *testing.T) {
		if HasCascadePermission(RoleViewer, PermissionSecretsAdmin, ProjectCascadeSecretPerms) {
			t.Error("project viewer should not have secrets:admin via cascade")
		}
	})

	t.Run("project editor can list secrets", func(t *testing.T) {
		if !HasCascadePermission(RoleEditor, PermissionSecretsList, ProjectCascadeSecretPerms) {
			t.Error("project editor should have secrets:list via cascade")
		}
	})

	t.Run("project editor can write secrets", func(t *testing.T) {
		if !HasCascadePermission(RoleEditor, PermissionSecretsWrite, ProjectCascadeSecretPerms) {
			t.Error("project editor should have secrets:write via cascade")
		}
	})

	t.Run("project editor cannot read secret data", func(t *testing.T) {
		if HasCascadePermission(RoleEditor, PermissionSecretsRead, ProjectCascadeSecretPerms) {
			t.Error("project editor should not have secrets:read via cascade")
		}
	})

	t.Run("project editor cannot delete secrets", func(t *testing.T) {
		if HasCascadePermission(RoleEditor, PermissionSecretsDelete, ProjectCascadeSecretPerms) {
			t.Error("project editor should not have secrets:delete via cascade")
		}
	})

	t.Run("project editor cannot admin secrets", func(t *testing.T) {
		if HasCascadePermission(RoleEditor, PermissionSecretsAdmin, ProjectCascadeSecretPerms) {
			t.Error("project editor should not have secrets:admin via cascade")
		}
	})

	t.Run("project owner can list secrets", func(t *testing.T) {
		if !HasCascadePermission(RoleOwner, PermissionSecretsList, ProjectCascadeSecretPerms) {
			t.Error("project owner should have secrets:list via cascade")
		}
	})

	t.Run("project owner can write secrets", func(t *testing.T) {
		if !HasCascadePermission(RoleOwner, PermissionSecretsWrite, ProjectCascadeSecretPerms) {
			t.Error("project owner should have secrets:write via cascade")
		}
	})

	t.Run("project owner can delete secrets", func(t *testing.T) {
		if !HasCascadePermission(RoleOwner, PermissionSecretsDelete, ProjectCascadeSecretPerms) {
			t.Error("project owner should have secrets:delete via cascade")
		}
	})

	t.Run("project owner can admin secrets", func(t *testing.T) {
		if !HasCascadePermission(RoleOwner, PermissionSecretsAdmin, ProjectCascadeSecretPerms) {
			t.Error("project owner should have secrets:admin via cascade")
		}
	})

	t.Run("project owner cannot read secret data via cascade", func(t *testing.T) {
		if HasCascadePermission(RoleOwner, PermissionSecretsRead, ProjectCascadeSecretPerms) {
			t.Error("project owner should not have secrets:read via cascade")
		}
	})

	t.Run("unspecified role has no cascade permissions", func(t *testing.T) {
		if HasCascadePermission(RoleUnspecified, PermissionSecretsList, ProjectCascadeSecretPerms) {
			t.Error("unspecified role should not have any cascade permissions")
		}
	})
}

func TestCheckCascadeAccess(t *testing.T) {
	t.Run("project viewer can list secrets via cascade", func(t *testing.T) {
		err := CheckCascadeAccess(
			"alice@example.com",
			nil,
			map[string]string{"alice@example.com": "viewer"},
			nil,
			PermissionSecretsList,
			ProjectCascadeSecretPerms,
		)
		if err != nil {
			t.Errorf("expected access granted, got: %v", err)
		}
	})

	t.Run("project viewer cannot read secret data via cascade", func(t *testing.T) {
		err := CheckCascadeAccess(
			"alice@example.com",
			nil,
			map[string]string{"alice@example.com": "viewer"},
			nil,
			PermissionSecretsRead,
			ProjectCascadeSecretPerms,
		)
		if err == nil {
			t.Fatal("expected PermissionDenied, got nil")
		}
	})

	t.Run("project editor can write secrets via cascade", func(t *testing.T) {
		err := CheckCascadeAccess(
			"bob@example.com",
			nil,
			map[string]string{"bob@example.com": "editor"},
			nil,
			PermissionSecretsWrite,
			ProjectCascadeSecretPerms,
		)
		if err != nil {
			t.Errorf("expected access granted, got: %v", err)
		}
	})

	t.Run("project owner can delete secrets via cascade", func(t *testing.T) {
		err := CheckCascadeAccess(
			"carol@example.com",
			nil,
			map[string]string{"carol@example.com": "owner"},
			nil,
			PermissionSecretsDelete,
			ProjectCascadeSecretPerms,
		)
		if err != nil {
			t.Errorf("expected access granted, got: %v", err)
		}
	})

	t.Run("group grant works with cascade", func(t *testing.T) {
		err := CheckCascadeAccess(
			"dave@example.com",
			[]string{"engineering"},
			nil,
			map[string]string{"engineering": "editor"},
			PermissionSecretsWrite,
			ProjectCascadeSecretPerms,
		)
		if err != nil {
			t.Errorf("expected access granted via role cascade, got: %v", err)
		}
	})

	t.Run("no grants denies cascade access", func(t *testing.T) {
		err := CheckCascadeAccess(
			"nobody@example.com",
			nil,
			nil,
			nil,
			PermissionSecretsList,
			ProjectCascadeSecretPerms,
		)
		if err == nil {
			t.Fatal("expected PermissionDenied, got nil")
		}
	})

}

func TestHasPermission_DeploymentPermissions(t *testing.T) {
	t.Run("viewer can list and read deployments and view logs", func(t *testing.T) {
		if !HasPermission(RoleViewer, PermissionDeploymentsList) {
			t.Error("viewer should have deployments:list")
		}
		if !HasPermission(RoleViewer, PermissionDeploymentsRead) {
			t.Error("viewer should have deployments:read")
		}
		if !HasPermission(RoleViewer, PermissionDeploymentsLogs) {
			t.Error("viewer should have deployments:logs")
		}
	})

	t.Run("viewer cannot write or delete deployments", func(t *testing.T) {
		if HasPermission(RoleViewer, PermissionDeploymentsWrite) {
			t.Error("viewer should not have deployments:write")
		}
		if HasPermission(RoleViewer, PermissionDeploymentsDelete) {
			t.Error("viewer should not have deployments:delete")
		}
		if HasPermission(RoleViewer, PermissionDeploymentsAdmin) {
			t.Error("viewer should not have deployments:admin")
		}
	})

	t.Run("editor can write deployments", func(t *testing.T) {
		if !HasPermission(RoleEditor, PermissionDeploymentsWrite) {
			t.Error("editor should have deployments:write")
		}
	})

	t.Run("editor cannot delete or admin deployments", func(t *testing.T) {
		if HasPermission(RoleEditor, PermissionDeploymentsDelete) {
			t.Error("editor should not have deployments:delete")
		}
		if HasPermission(RoleEditor, PermissionDeploymentsAdmin) {
			t.Error("editor should not have deployments:admin")
		}
	})

	t.Run("owner has all deployment permissions", func(t *testing.T) {
		for _, perm := range []Permission{
			PermissionDeploymentsList,
			PermissionDeploymentsRead,
			PermissionDeploymentsWrite,
			PermissionDeploymentsDelete,
			PermissionDeploymentsAdmin,
			PermissionDeploymentsLogs,
		} {
			if !HasPermission(RoleOwner, perm) {
				t.Errorf("owner should have permission %v", perm)
			}
		}
	})
}

func TestHasPermission_DeploymentTemplatePermissions(t *testing.T) {
	t.Run("viewer can list and read templates", func(t *testing.T) {
		if !HasPermission(RoleViewer, PermissionDeploymentTemplatesList) {
			t.Error("viewer should have templates:list")
		}
		if !HasPermission(RoleViewer, PermissionDeploymentTemplatesRead) {
			t.Error("viewer should have templates:read")
		}
	})

	t.Run("viewer cannot write or delete templates", func(t *testing.T) {
		if HasPermission(RoleViewer, PermissionDeploymentTemplatesWrite) {
			t.Error("viewer should not have templates:write")
		}
		if HasPermission(RoleViewer, PermissionDeploymentTemplatesDelete) {
			t.Error("viewer should not have templates:delete")
		}
	})

	t.Run("editor can write templates", func(t *testing.T) {
		if !HasPermission(RoleEditor, PermissionDeploymentTemplatesWrite) {
			t.Error("editor should have templates:write")
		}
	})

	t.Run("editor cannot delete or admin templates", func(t *testing.T) {
		if HasPermission(RoleEditor, PermissionDeploymentTemplatesDelete) {
			t.Error("editor should not have templates:delete")
		}
		if HasPermission(RoleEditor, PermissionDeploymentTemplatesAdmin) {
			t.Error("editor should not have templates:admin")
		}
	})

	t.Run("owner has all template permissions", func(t *testing.T) {
		for _, perm := range []Permission{
			PermissionDeploymentTemplatesList,
			PermissionDeploymentTemplatesRead,
			PermissionDeploymentTemplatesWrite,
			PermissionDeploymentTemplatesDelete,
			PermissionDeploymentTemplatesAdmin,
		} {
			if !HasPermission(RoleOwner, perm) {
				t.Errorf("owner should have permission %v", perm)
			}
		}
	})
}

func TestHasPermission_ProjectSettingsPermissions(t *testing.T) {
	t.Run("viewer can read project settings", func(t *testing.T) {
		if !HasPermission(RoleViewer, PermissionProjectSettingsRead) {
			t.Error("viewer should have settings:read")
		}
	})

	t.Run("viewer cannot write project settings", func(t *testing.T) {
		if HasPermission(RoleViewer, PermissionProjectSettingsWrite) {
			t.Error("viewer should not have settings:write")
		}
	})

	t.Run("editor cannot write project settings", func(t *testing.T) {
		if HasPermission(RoleEditor, PermissionProjectSettingsWrite) {
			t.Error("editor should not have settings:write (owner-only)")
		}
	})

	t.Run("owner can read and write project settings", func(t *testing.T) {
		if !HasPermission(RoleOwner, PermissionProjectSettingsRead) {
			t.Error("owner should have settings:read")
		}
		if !HasPermission(RoleOwner, PermissionProjectSettingsWrite) {
			t.Error("owner should have settings:write")
		}
	})
}

func TestProjectCascadeDeploymentPerms(t *testing.T) {
	t.Run("project viewer can list and read deployments and view logs", func(t *testing.T) {
		if !HasCascadePermission(RoleViewer, PermissionDeploymentsList, ProjectCascadeDeploymentPerms) {
			t.Error("project viewer should have deployments:list via cascade")
		}
		if !HasCascadePermission(RoleViewer, PermissionDeploymentsRead, ProjectCascadeDeploymentPerms) {
			t.Error("project viewer should have deployments:read via cascade")
		}
		if !HasCascadePermission(RoleViewer, PermissionDeploymentsLogs, ProjectCascadeDeploymentPerms) {
			t.Error("project viewer should have deployments:logs via cascade")
		}
	})

	t.Run("project viewer cannot write or delete deployments", func(t *testing.T) {
		if HasCascadePermission(RoleViewer, PermissionDeploymentsWrite, ProjectCascadeDeploymentPerms) {
			t.Error("project viewer should not have deployments:write via cascade")
		}
		if HasCascadePermission(RoleViewer, PermissionDeploymentsDelete, ProjectCascadeDeploymentPerms) {
			t.Error("project viewer should not have deployments:delete via cascade")
		}
	})

	t.Run("project editor can write deployments", func(t *testing.T) {
		if !HasCascadePermission(RoleEditor, PermissionDeploymentsWrite, ProjectCascadeDeploymentPerms) {
			t.Error("project editor should have deployments:write via cascade")
		}
	})

	t.Run("project owner can delete and admin deployments", func(t *testing.T) {
		if !HasCascadePermission(RoleOwner, PermissionDeploymentsDelete, ProjectCascadeDeploymentPerms) {
			t.Error("project owner should have deployments:delete via cascade")
		}
		if !HasCascadePermission(RoleOwner, PermissionDeploymentsAdmin, ProjectCascadeDeploymentPerms) {
			t.Error("project owner should have deployments:admin via cascade")
		}
	})
}

func TestProjectCascadeTemplatePerms(t *testing.T) {
	t.Run("project viewer can list and read templates", func(t *testing.T) {
		if !HasCascadePermission(RoleViewer, PermissionDeploymentTemplatesList, ProjectCascadeTemplatePerms) {
			t.Error("project viewer should have templates:list via cascade")
		}
		if !HasCascadePermission(RoleViewer, PermissionDeploymentTemplatesRead, ProjectCascadeTemplatePerms) {
			t.Error("project viewer should have templates:read via cascade")
		}
	})

	t.Run("project viewer cannot write templates", func(t *testing.T) {
		if HasCascadePermission(RoleViewer, PermissionDeploymentTemplatesWrite, ProjectCascadeTemplatePerms) {
			t.Error("project viewer should not have templates:write via cascade")
		}
	})

	t.Run("project editor can write templates", func(t *testing.T) {
		if !HasCascadePermission(RoleEditor, PermissionDeploymentTemplatesWrite, ProjectCascadeTemplatePerms) {
			t.Error("project editor should have templates:write via cascade")
		}
	})

	t.Run("project owner can delete and admin templates", func(t *testing.T) {
		if !HasCascadePermission(RoleOwner, PermissionDeploymentTemplatesDelete, ProjectCascadeTemplatePerms) {
			t.Error("project owner should have templates:delete via cascade")
		}
		if !HasCascadePermission(RoleOwner, PermissionDeploymentTemplatesAdmin, ProjectCascadeTemplatePerms) {
			t.Error("project owner should have templates:admin via cascade")
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
			t.Errorf("expected access granted via role, got: %v", err)
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

	t.Run("denies access without explicit grants", func(t *testing.T) {
		// User is in "owner" OIDC group but has no explicit grants
		err := CheckAccessGrants(
			"nobody@example.com",
			[]string{"owner"},
			nil,
			nil,
			PermissionProjectsRead,
		)
		if err == nil {
			t.Fatal("expected PermissionDenied without explicit grants, got nil")
		}
	})
}

func TestOrgCascadeSystemTemplatePerms(t *testing.T) {
	t.Run("org OWNER has PERMISSION_SYSTEM_DEPLOYMENTS_EDIT", func(t *testing.T) {
		if !HasCascadePermission(RoleOwner, PermissionSystemDeploymentsEdit, OrgCascadeSystemTemplatePerms) {
			t.Error("org OWNER should have PERMISSION_SYSTEM_DEPLOYMENTS_EDIT via cascade")
		}
	})

	t.Run("org EDITOR does not have PERMISSION_SYSTEM_DEPLOYMENTS_EDIT", func(t *testing.T) {
		if HasCascadePermission(RoleEditor, PermissionSystemDeploymentsEdit, OrgCascadeSystemTemplatePerms) {
			t.Error("org EDITOR should not have PERMISSION_SYSTEM_DEPLOYMENTS_EDIT")
		}
	})

	t.Run("org VIEWER does not have PERMISSION_SYSTEM_DEPLOYMENTS_EDIT", func(t *testing.T) {
		if HasCascadePermission(RoleViewer, PermissionSystemDeploymentsEdit, OrgCascadeSystemTemplatePerms) {
			t.Error("org VIEWER should not have PERMISSION_SYSTEM_DEPLOYMENTS_EDIT")
		}
	})

	t.Run("CheckCascadeAccess grants OWNER", func(t *testing.T) {
		err := CheckCascadeAccess(
			"owner@example.com",
			nil,
			map[string]string{"owner@example.com": "owner"},
			nil,
			PermissionSystemDeploymentsEdit,
			OrgCascadeSystemTemplatePerms,
		)
		if err != nil {
			t.Errorf("expected access granted for org OWNER, got %v", err)
		}
	})

	t.Run("CheckCascadeAccess denies VIEWER", func(t *testing.T) {
		err := CheckCascadeAccess(
			"viewer@example.com",
			nil,
			map[string]string{"viewer@example.com": "viewer"},
			nil,
			PermissionSystemDeploymentsEdit,
			OrgCascadeSystemTemplatePerms,
		)
		if err == nil {
			t.Error("expected access denied for org VIEWER, got nil")
		}
	})

	t.Run("CheckCascadeAccess denies EDITOR", func(t *testing.T) {
		err := CheckCascadeAccess(
			"editor@example.com",
			nil,
			map[string]string{"editor@example.com": "editor"},
			nil,
			PermissionSystemDeploymentsEdit,
			OrgCascadeSystemTemplatePerms,
		)
		if err == nil {
			t.Error("expected access denied for org EDITOR, got nil")
		}
	})
}
