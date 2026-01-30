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
	PermissionUnspecified  = consolev1.Permission_PERMISSION_UNSPECIFIED
	PermissionSecretsRead  = consolev1.Permission_PERMISSION_SECRETS_READ
	PermissionSecretsList  = consolev1.Permission_PERMISSION_SECRETS_LIST
	PermissionSecretsWrite = consolev1.Permission_PERMISSION_SECRETS_WRITE
	PermissionSecretsDelete = consolev1.Permission_PERMISSION_SECRETS_DELETE
	PermissionSecretsAdmin = consolev1.Permission_PERMISSION_SECRETS_ADMIN
)

// rolePermissions defines which permissions each role has.
// Higher-level roles inherit all permissions from lower-level roles.
var rolePermissions = map[Role]map[Permission]bool{
	RoleViewer: {
		PermissionSecretsRead: true,
		PermissionSecretsList: true,
	},
	RoleEditor: {
		PermissionSecretsRead:  true,
		PermissionSecretsList:  true,
		PermissionSecretsWrite: true,
	},
	RoleOwner: {
		PermissionSecretsRead:   true,
		PermissionSecretsList:   true,
		PermissionSecretsWrite:  true,
		PermissionSecretsDelete: true,
		PermissionSecretsAdmin:  true,
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

// GroupMapping holds the mapping from OIDC group names to roles.
// When custom groups are provided for a role, only those groups map to that role.
// When no custom groups are provided (nil), the default group name is used.
type GroupMapping struct {
	groupToRole map[string]Role
}

// NewGroupMapping creates a GroupMapping. For each role, if the provided slice is
// non-nil, those group names are used; otherwise the default group name
// ("viewer", "editor", or "owner") is used.
func NewGroupMapping(viewerGroups, editorGroups, ownerGroups []string) *GroupMapping {
	if viewerGroups == nil {
		viewerGroups = []string{"viewer"}
	}
	if editorGroups == nil {
		editorGroups = []string{"editor"}
	}
	if ownerGroups == nil {
		ownerGroups = []string{"owner"}
	}

	m := make(map[string]Role)
	for _, g := range viewerGroups {
		m[strings.ToLower(g)] = RoleViewer
	}
	for _, g := range editorGroups {
		m[strings.ToLower(g)] = RoleEditor
	}
	for _, g := range ownerGroups {
		m[strings.ToLower(g)] = RoleOwner
	}

	return &GroupMapping{groupToRole: m}
}

// ParseGroups splits a comma-separated string into a slice of trimmed group names.
// Returns nil for an empty string.
func ParseGroups(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	groups := make([]string, 0, len(parts))
	for _, p := range parts {
		g := strings.TrimSpace(p)
		if g != "" {
			groups = append(groups, g)
		}
	}
	if len(groups) == 0 {
		return nil
	}
	return groups
}

// MapGroupToRole maps a group name to a Role using the configured mapping.
func (gm *GroupMapping) MapGroupToRole(group string) Role {
	role, ok := gm.groupToRole[strings.ToLower(group)]
	if !ok {
		return RoleUnspecified
	}
	return role
}

// MapGroupsToRoles maps a slice of group names to roles, filtering out unknown groups.
func (gm *GroupMapping) MapGroupsToRoles(groups []string) []Role {
	roles := make([]Role, 0, len(groups))
	for _, g := range groups {
		role := gm.MapGroupToRole(g)
		if role != RoleUnspecified {
			roles = append(roles, role)
		}
	}
	return roles
}

// CheckAccess verifies that the user has at least one role that grants the required permission.
// This is the method form that uses the configured group mapping.
func (gm *GroupMapping) CheckAccess(userGroups, allowedRoles []string, permission Permission) error {
	// Map user groups to roles
	userRoles := gm.MapGroupsToRoles(userGroups)

	// Find the minimum required role level from allowed roles
	minLevel := -1
	for _, r := range allowedRoles {
		role := gm.MapGroupToRole(r)
		if role != RoleUnspecified {
			level := roleLevel[role]
			if minLevel < 0 || level < minLevel {
				minLevel = level
			}
		}
	}

	// If no valid allowed roles, deny access
	if minLevel < 0 {
		return connect.NewError(
			connect.CodePermissionDenied,
			fmt.Errorf("RBAC: authorization denied (allowed roles: [%s])",
				strings.Join(allowedRoles, " ")),
		)
	}

	// Check if any user role is at or above the minimum level AND has the required permission
	for _, userRole := range userRoles {
		if roleLevel[userRole] >= minLevel && HasPermission(userRole, permission) {
			return nil
		}
	}

	return connect.NewError(
		connect.CodePermissionDenied,
		fmt.Errorf("RBAC: authorization denied (allowed roles: [%s])",
			strings.Join(allowedRoles, " ")),
	)
}

// defaultMapping is the package-level default GroupMapping using built-in group names.
var defaultMapping = NewGroupMapping(nil, nil, nil)

// MapGroupToRole maps a group name to a Role using case-insensitive matching.
// Returns RoleUnspecified for unknown groups.
// Uses the default group mapping (viewer, editor, owner).
func MapGroupToRole(group string) Role {
	return defaultMapping.MapGroupToRole(group)
}

// MapGroupsToRoles maps a slice of group names to roles, filtering out unknown groups.
// Uses the default group mapping.
func MapGroupsToRoles(groups []string) []Role {
	return defaultMapping.MapGroupsToRoles(groups)
}

// roleLevel defines the hierarchy level of each role for comparison.
// Higher values indicate more privileged roles.
var roleLevel = map[Role]int{
	RoleUnspecified: 0,
	RoleViewer:      1,
	RoleEditor:      2,
	RoleOwner:       3,
}

// CheckAccess verifies that the user has at least one role that grants the required permission.
// userGroups are the groups the user belongs to.
// allowedRoles are the roles that are allowed to access the resource.
// permission is the specific permission required.
// Returns nil if access is granted, or a PermissionDenied error otherwise.
//
// Access is granted if the user has a role with the required permission AND
// their role is at or above the minimum required role level.
// For example, if a secret allows "viewer" role, then viewers, editors, and owners
// can all access it (assuming they have the required permission).
func CheckAccess(userGroups, allowedRoles []string, permission Permission) error {
	// Map user groups to roles
	userRoles := MapGroupsToRoles(userGroups)

	// Find the minimum required role level from allowed roles
	minLevel := -1
	for _, r := range allowedRoles {
		role := MapGroupToRole(r)
		if role != RoleUnspecified {
			level := roleLevel[role]
			if minLevel < 0 || level < minLevel {
				minLevel = level
			}
		}
	}

	// If no valid allowed roles, deny access
	if minLevel < 0 {
		return connect.NewError(
			connect.CodePermissionDenied,
			fmt.Errorf("RBAC: authorization denied (allowed roles: [%s])",
				strings.Join(allowedRoles, " ")),
		)
	}

	// Check if any user role is at or above the minimum level AND has the required permission
	for _, userRole := range userRoles {
		if roleLevel[userRole] >= minLevel && HasPermission(userRole, permission) {
			return nil
		}
	}

	return connect.NewError(
		connect.CodePermissionDenied,
		fmt.Errorf("RBAC: authorization denied (allowed roles: [%s])",
			strings.Join(allowedRoles, " ")),
	)
}
