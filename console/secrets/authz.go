package secrets

import (
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/holos-run/holos-console/console/rbac"
)

// CheckReadAccess verifies that the user has permission to read secrets.
// Uses role-based access control with the PERMISSION_SECRETS_READ permission.
func CheckReadAccess(gm *rbac.GroupMapping, userGroups, allowedRoles []string) error {
	return gm.CheckAccess(userGroups, allowedRoles, rbac.PermissionSecretsRead)
}

// CheckWriteAccess verifies that the user has permission to write secrets.
// Uses role-based access control with the PERMISSION_SECRETS_WRITE permission.
func CheckWriteAccess(gm *rbac.GroupMapping, userGroups, allowedRoles []string) error {
	return gm.CheckAccess(userGroups, allowedRoles, rbac.PermissionSecretsWrite)
}

// CheckDeleteAccess verifies that the user has permission to delete secrets.
// Uses role-based access control with the PERMISSION_SECRETS_DELETE permission.
func CheckDeleteAccess(gm *rbac.GroupMapping, userGroups, allowedRoles []string) error {
	return gm.CheckAccess(userGroups, allowedRoles, rbac.PermissionSecretsDelete)
}

// CheckListAccess verifies that the user has permission to list secrets.
// Uses role-based access control with the PERMISSION_SECRETS_LIST permission.
func CheckListAccess(gm *rbac.GroupMapping, userGroups, allowedRoles []string) error {
	return gm.CheckAccess(userGroups, allowedRoles, rbac.PermissionSecretsList)
}

// CheckReadAccessSharing verifies read permission using sharing-aware access control.
func CheckReadAccessSharing(gm *rbac.GroupMapping, email string, groups []string, shareUsers, shareGroups map[string]string, allowedRoles []string) error {
	return gm.CheckAccessSharing(email, groups, shareUsers, shareGroups, allowedRoles, rbac.PermissionSecretsRead)
}

// CheckWriteAccessSharing verifies write permission using sharing-aware access control.
func CheckWriteAccessSharing(gm *rbac.GroupMapping, email string, groups []string, shareUsers, shareGroups map[string]string, allowedRoles []string) error {
	return gm.CheckAccessSharing(email, groups, shareUsers, shareGroups, allowedRoles, rbac.PermissionSecretsWrite)
}

// CheckDeleteAccessSharing verifies delete permission using sharing-aware access control.
func CheckDeleteAccessSharing(gm *rbac.GroupMapping, email string, groups []string, shareUsers, shareGroups map[string]string, allowedRoles []string) error {
	return gm.CheckAccessSharing(email, groups, shareUsers, shareGroups, allowedRoles, rbac.PermissionSecretsDelete)
}

// CheckListAccessSharing verifies list permission using sharing-aware access control.
func CheckListAccessSharing(gm *rbac.GroupMapping, email string, groups []string, shareUsers, shareGroups map[string]string, allowedRoles []string) error {
	return gm.CheckAccessSharing(email, groups, shareUsers, shareGroups, allowedRoles, rbac.PermissionSecretsList)
}

// CheckAccess verifies that the user has at least one group in common with the allowed groups.
// Deprecated: Use CheckReadAccess or CheckListAccess instead.
// Returns nil if access is granted, or a PermissionDenied error otherwise.
func CheckAccess(userGroups, allowedGroups []string) error {
	for _, ug := range userGroups {
		for _, ag := range allowedGroups {
			if ug == ag {
				return nil
			}
		}
	}
	return connect.NewError(
		connect.CodePermissionDenied,
		fmt.Errorf("RBAC: authorization denied (not a member of: [%s])",
			strings.Join(allowedGroups, " ")),
	)
}
