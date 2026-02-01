// Package rbac provides role-based access control for the console.
package rbac

import (
	"fmt"
	"strings"

	"connectrpc.com/connect"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// Role type alias for the proto-generated Role enum.
type Role = consolev1.Role

// Permission type alias for the proto-generated Permission enum.
type Permission = consolev1.Permission

// Role constants aliasing proto enum values.
const (
	RoleUnspecified = consolev1.Role_ROLE_UNSPECIFIED
	RoleViewer      = consolev1.Role_ROLE_VIEWER
	RoleEditor      = consolev1.Role_ROLE_EDITOR
	RoleOwner       = consolev1.Role_ROLE_OWNER
)

// Permission constants aliasing proto enum values.
const (
	PermissionUnspecified    = consolev1.Permission_PERMISSION_UNSPECIFIED
	PermissionSecretsRead    = consolev1.Permission_PERMISSION_SECRETS_READ
	PermissionSecretsList    = consolev1.Permission_PERMISSION_SECRETS_LIST
	PermissionSecretsWrite   = consolev1.Permission_PERMISSION_SECRETS_WRITE
	PermissionSecretsDelete  = consolev1.Permission_PERMISSION_SECRETS_DELETE
	PermissionSecretsAdmin   = consolev1.Permission_PERMISSION_SECRETS_ADMIN
	PermissionProjectsRead   = consolev1.Permission_PERMISSION_PROJECTS_READ
	PermissionProjectsList   = consolev1.Permission_PERMISSION_PROJECTS_LIST
	PermissionProjectsWrite  = consolev1.Permission_PERMISSION_PROJECTS_WRITE
	PermissionProjectsDelete = consolev1.Permission_PERMISSION_PROJECTS_DELETE
	PermissionProjectsAdmin  = consolev1.Permission_PERMISSION_PROJECTS_ADMIN
	PermissionProjectsCreate       = consolev1.Permission_PERMISSION_PROJECTS_CREATE
	PermissionOrganizationsRead    = consolev1.Permission_PERMISSION_ORGANIZATIONS_READ
	PermissionOrganizationsList    = consolev1.Permission_PERMISSION_ORGANIZATIONS_LIST
	PermissionOrganizationsWrite   = consolev1.Permission_PERMISSION_ORGANIZATIONS_WRITE
	PermissionOrganizationsDelete  = consolev1.Permission_PERMISSION_ORGANIZATIONS_DELETE
	PermissionOrganizationsAdmin   = consolev1.Permission_PERMISSION_ORGANIZATIONS_ADMIN
	PermissionOrganizationsCreate  = consolev1.Permission_PERMISSION_ORGANIZATIONS_CREATE
)

// rolePermissions defines which permissions each role has.
// Higher-level roles inherit all permissions from lower-level roles.
var rolePermissions = map[Role]map[Permission]bool{
	RoleViewer: {
		PermissionSecretsRead:        true,
		PermissionSecretsList:        true,
		PermissionProjectsRead:       true,
		PermissionProjectsList:       true,
		PermissionOrganizationsRead:  true,
		PermissionOrganizationsList:  true,
	},
	RoleEditor: {
		PermissionSecretsRead:         true,
		PermissionSecretsList:         true,
		PermissionSecretsWrite:        true,
		PermissionProjectsRead:        true,
		PermissionProjectsList:        true,
		PermissionProjectsWrite:       true,
		PermissionOrganizationsRead:   true,
		PermissionOrganizationsList:   true,
		PermissionOrganizationsWrite:  true,
	},
	RoleOwner: {
		PermissionSecretsRead:          true,
		PermissionSecretsList:          true,
		PermissionSecretsWrite:         true,
		PermissionSecretsDelete:        true,
		PermissionSecretsAdmin:         true,
		PermissionProjectsRead:         true,
		PermissionProjectsList:         true,
		PermissionProjectsWrite:        true,
		PermissionProjectsDelete:       true,
		PermissionProjectsAdmin:        true,
		PermissionProjectsCreate:       true,
		PermissionOrganizationsRead:    true,
		PermissionOrganizationsList:    true,
		PermissionOrganizationsWrite:   true,
		PermissionOrganizationsDelete:  true,
		PermissionOrganizationsAdmin:   true,
		PermissionOrganizationsCreate:  true,
	},
}

// HasPermission returns true if the given role has the specified permission.
func HasPermission(role Role, permission Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	return perms[permission]
}

// RoleFromString converts a role name string to a Role constant using case-insensitive matching.
// Returns RoleUnspecified for unknown or empty strings.
func RoleFromString(s string) Role {
	switch strings.ToLower(s) {
	case "viewer":
		return RoleViewer
	case "editor":
		return RoleEditor
	case "owner":
		return RoleOwner
	default:
		return RoleUnspecified
	}
}

// CheckAccessGrants verifies access using per-user and per-group sharing grants.
// Returns nil if access is granted, or a PermissionDenied error.
func CheckAccessGrants(
	userEmail string,
	userGroups []string,
	shareUsers map[string]string,
	shareGroups map[string]string,
	permission Permission,
) error {
	bestLevel := -1

	// Check per-user sharing grants
	if shareUsers != nil {
		emailLower := strings.ToLower(userEmail)
		for email, roleName := range shareUsers {
			if strings.ToLower(email) == emailLower {
				role := RoleFromString(roleName)
				if level := roleLevel[role]; level > bestLevel {
					bestLevel = level
				}
			}
		}
	}

	// Check per-group sharing grants
	if shareGroups != nil {
		for _, ug := range userGroups {
			ugLower := strings.ToLower(ug)
			for group, roleName := range shareGroups {
				if strings.ToLower(group) == ugLower {
					role := RoleFromString(roleName)
					if level := roleLevel[role]; level > bestLevel {
						bestLevel = level
					}
				}
			}
		}
	}

	// Evaluate best role from grant sources only
	if bestLevel > 0 {
		for role, level := range roleLevel {
			if level == bestLevel {
				if HasPermission(role, permission) {
					return nil
				}
			}
		}
	}

	return connect.NewError(
		connect.CodePermissionDenied,
		fmt.Errorf("RBAC: authorization denied"),
	)
}

// BestRoleFromGrants returns the highest role a user has from grants.
// Returns RoleUnspecified if no grants match.
func BestRoleFromGrants(
	userEmail string,
	userGroups []string,
	shareUsers map[string]string,
	shareGroups map[string]string,
) Role {
	bestLevel := 0

	if shareUsers != nil {
		emailLower := strings.ToLower(userEmail)
		for email, roleName := range shareUsers {
			if strings.ToLower(email) == emailLower {
				role := RoleFromString(roleName)
				if level := roleLevel[role]; level > bestLevel {
					bestLevel = level
				}
			}
		}
	}

	if shareGroups != nil {
		for _, ug := range userGroups {
			ugLower := strings.ToLower(ug)
			for group, roleName := range shareGroups {
				if strings.ToLower(group) == ugLower {
					role := RoleFromString(roleName)
					if level := roleLevel[role]; level > bestLevel {
						bestLevel = level
					}
				}
			}
		}
	}

	for role, level := range roleLevel {
		if level == bestLevel {
			return role
		}
	}
	return RoleUnspecified
}

// RoleLevel returns the hierarchy level of a role for comparison.
// Higher values indicate more privileged roles.
func RoleLevel(role Role) int {
	return roleLevel[role]
}

// CascadeSecretToProject maps a secret-level permission to the project-level
// permission required when evaluating project grants as a fallback for secret
// access. Returns PermissionUnspecified if project grants should never authorize
// the given secret operation (e.g., reading secret data requires a direct grant).
func CascadeSecretToProject(p Permission) Permission {
	switch p {
	case PermissionSecretsList:
		return PermissionProjectsRead
	case PermissionSecretsWrite:
		return PermissionProjectsWrite
	case PermissionSecretsDelete:
		return PermissionProjectsAdmin
	case PermissionSecretsAdmin:
		return PermissionProjectsAdmin
	default:
		return PermissionUnspecified
	}
}

// CascadeSecretToOrg maps a secret-level permission to the org-level permission
// required when evaluating org grants as a fallback for secret access.
// Org grants never cascade to secrets, so this always returns PermissionUnspecified.
func CascadeSecretToOrg(_ Permission) Permission {
	return PermissionUnspecified
}

// CascadeProjectToOrg maps a project-level permission to the org-level permission
// required when evaluating org grants as a fallback for project access.
// Org grants never cascade to projects, so this always returns PermissionUnspecified.
func CascadeProjectToOrg(_ Permission) Permission {
	return PermissionUnspecified
}

// roleLevel defines the hierarchy level of each role for comparison.
var roleLevel = map[Role]int{
	RoleUnspecified: 0,
	RoleViewer:      1,
	RoleEditor:      2,
	RoleOwner:       3,
}
