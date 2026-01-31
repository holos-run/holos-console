package secrets

import (
	"github.com/holos-run/holos-console/console/rbac"
)

// CheckReadAccessSharing verifies read permission using sharing-aware access control.
func CheckReadAccessSharing(gm *rbac.GroupMapping, email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return gm.CheckAccessSharing(email, groups, shareUsers, shareGroups, rbac.PermissionSecretsRead)
}

// CheckWriteAccessSharing verifies write permission using sharing-aware access control.
func CheckWriteAccessSharing(gm *rbac.GroupMapping, email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return gm.CheckAccessSharing(email, groups, shareUsers, shareGroups, rbac.PermissionSecretsWrite)
}

// CheckDeleteAccessSharing verifies delete permission using sharing-aware access control.
func CheckDeleteAccessSharing(gm *rbac.GroupMapping, email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return gm.CheckAccessSharing(email, groups, shareUsers, shareGroups, rbac.PermissionSecretsDelete)
}

// CheckListAccessSharing verifies list permission using sharing-aware access control.
func CheckListAccessSharing(gm *rbac.GroupMapping, email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return gm.CheckAccessSharing(email, groups, shareUsers, shareGroups, rbac.PermissionSecretsList)
}

// CheckAdminAccessSharing verifies admin permission using sharing-aware access control.
// Used for operations like updating sharing grants, which require owner-level access.
func CheckAdminAccessSharing(gm *rbac.GroupMapping, email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return gm.CheckAccessSharing(email, groups, shareUsers, shareGroups, rbac.PermissionSecretsAdmin)
}
