// Package rbac is the residue of the legacy in-process RBAC layer.
//
// Per ADR 036, the Kubernetes API server is the single arbiter of access; the
// per-handler permission checks that used to live here have been migrated to
// rpc.ImpersonatedClientsetFromContext and per-resource RoleBindings
// reconciled by console/resourcerbac (HOL-1062 / HOL-1063 / HOL-1064).
//
// What remains is intentionally small and load-bearing for two narrow uses:
//
//  1. console/settings still gates ProjectSettings reads/writes via
//     in-process grant evaluation. Migrating it to impersonation is tracked
//     as a follow-up; until then it imports CheckAccessGrants and
//     CheckCascadeAccess plus the two Permission constants those calls take.
//
//  2. console/{organizations,folders,projects} use the Role enum and
//     BestRoleFromGrants / RoleLevel / RoleFromString to derive the
//     userRole field returned in list/get responses for UI hints. This
//     derivation does not gate access — the apiserver already did that —
//     but the proto field is part of the public API contract.
//
// New code MUST NOT import this package. Add new gating via Kubernetes RBAC
// + impersonation. The settings handler is expected to follow when its
// migration lands.
package rbac

import (
	"fmt"
	"strings"

	"connectrpc.com/connect"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// Role is the proto-generated Role enum.
type Role = consolev1.Role

// Permission is the proto-generated Permission enum.
type Permission = consolev1.Permission

const (
	RoleUnspecified = consolev1.Role_ROLE_UNSPECIFIED
	RoleViewer      = consolev1.Role_ROLE_VIEWER
	RoleEditor      = consolev1.Role_ROLE_EDITOR
	RoleOwner       = consolev1.Role_ROLE_OWNER
)

// Permission constants used by the surviving call sites.
//
// PermissionProjectSettingsRead is the permission CheckAccessGrants resolves
// for project-grant evaluation in console/settings.
//
// PermissionProjectDeploymentsEnable is the permission CheckCascadeAccess
// resolves for the org→project cascade in OrgCascadeProjectSettingsPerms.
const (
	PermissionProjectSettingsRead      = consolev1.Permission_PERMISSION_PROJECT_SETTINGS_READ
	PermissionProjectDeploymentsEnable = consolev1.Permission_PERMISSION_PROJECT_DEPLOYMENTS_ENABLE
)

// rolePermissions enumerates the per-role grants CheckAccessGrants consults.
// Trimmed to the only Permission still consumed by an in-process check
// (PermissionProjectSettingsRead in console/settings).
var rolePermissions = map[Role]map[Permission]bool{
	RoleViewer: {PermissionProjectSettingsRead: true},
	RoleEditor: {PermissionProjectSettingsRead: true},
	RoleOwner:  {PermissionProjectSettingsRead: true},
}

// HasPermission returns true if role has been granted permission in the
// rolePermissions table.
func HasPermission(role Role, permission Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	return perms[permission]
}

// RoleFromString converts a role-name string (case-insensitive) to a Role.
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

// CheckAccessGrants verifies access using per-user and per-role sharing
// grants. Returns nil if granted, or a PermissionDenied error otherwise.
func CheckAccessGrants(
	userEmail string,
	userRoles []string,
	shareUsers map[string]string,
	shareRoles map[string]string,
	permission Permission,
) error {
	bestLevel := -1

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

	if shareRoles != nil {
		for _, ur := range userRoles {
			urLower := strings.ToLower(ur)
			for roleClaim, roleName := range shareRoles {
				if strings.ToLower(roleClaim) == urLower {
					role := RoleFromString(roleName)
					if level := roleLevel[role]; level > bestLevel {
						bestLevel = level
					}
				}
			}
		}
	}

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

// BestRoleFromGrants returns the highest role the user holds via grants, or
// RoleUnspecified if none match.
func BestRoleFromGrants(
	userEmail string,
	userRoles []string,
	shareUsers map[string]string,
	shareRoles map[string]string,
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

	if shareRoles != nil {
		for _, ur := range userRoles {
			urLower := strings.ToLower(ur)
			for roleClaim, roleName := range shareRoles {
				if strings.ToLower(roleClaim) == urLower {
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

// RoleLevel returns the hierarchy level of role for comparison.
func RoleLevel(role Role) int {
	return roleLevel[role]
}

// CascadeTable maps roles to permissions when a parent-resource grant
// cascades to a child resource. Only one cascade table remains
// (OrgCascadeProjectSettingsPerms) — the others retired with the handlers
// that consumed them.
type CascadeTable map[Role]map[Permission]bool

// OrgCascadeProjectSettingsPerms grants org-level OWNERs the right to toggle
// project deployments in console/settings.
var OrgCascadeProjectSettingsPerms = CascadeTable{
	RoleOwner: {
		PermissionProjectDeploymentsEnable: true,
	},
}

// HasCascadePermission returns true if role has permission in table.
func HasCascadePermission(role Role, perm Permission, table CascadeTable) bool {
	perms, ok := table[role]
	if !ok {
		return false
	}
	return perms[perm]
}

// CheckCascadeAccess resolves the best role from grants and checks whether
// that role has permission in table. Returns nil on grant or a
// PermissionDenied error otherwise.
func CheckCascadeAccess(
	userEmail string,
	userRoles []string,
	shareUsers map[string]string,
	shareRoles map[string]string,
	permission Permission,
	table CascadeTable,
) error {
	role := BestRoleFromGrants(userEmail, userRoles, shareUsers, shareRoles)
	if HasCascadePermission(role, permission, table) {
		return nil
	}
	return connect.NewError(
		connect.CodePermissionDenied,
		fmt.Errorf("RBAC: authorization denied"),
	)
}

var roleLevel = map[Role]int{
	RoleUnspecified: 0,
	RoleViewer:      1,
	RoleEditor:      2,
	RoleOwner:       3,
}
