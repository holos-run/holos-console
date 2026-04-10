package folders

import (
	"github.com/holos-run/holos-console/console/rbac"
)

// CheckFolderListAccess verifies the user has list permission on the folder.
func CheckFolderListAccess(email string, roles []string, shareUsers, shareRoles map[string]string) error {
	return rbac.CheckAccessGrants(email, roles, shareUsers, shareRoles, rbac.PermissionFoldersList)
}

// CheckFolderReadAccess verifies the user has read permission on the folder.
func CheckFolderReadAccess(email string, roles []string, shareUsers, shareRoles map[string]string) error {
	return rbac.CheckAccessGrants(email, roles, shareUsers, shareRoles, rbac.PermissionFoldersRead)
}

// CheckFolderWriteAccess verifies the user has write permission on the folder.
func CheckFolderWriteAccess(email string, roles []string, shareUsers, shareRoles map[string]string) error {
	return rbac.CheckAccessGrants(email, roles, shareUsers, shareRoles, rbac.PermissionFoldersWrite)
}

// CheckFolderDeleteAccess verifies the user has delete permission on the folder.
func CheckFolderDeleteAccess(email string, roles []string, shareUsers, shareRoles map[string]string) error {
	return rbac.CheckAccessGrants(email, roles, shareUsers, shareRoles, rbac.PermissionFoldersDelete)
}

// CheckFolderAdminAccess verifies the user has admin permission on the folder.
func CheckFolderAdminAccess(email string, roles []string, shareUsers, shareRoles map[string]string) error {
	return rbac.CheckAccessGrants(email, roles, shareUsers, shareRoles, rbac.PermissionFoldersAdmin)
}

// CheckFolderCreateAccess verifies the user has create permission on the folder.
func CheckFolderCreateAccess(email string, roles []string, shareUsers, shareRoles map[string]string) error {
	return rbac.CheckAccessGrants(email, roles, shareUsers, shareRoles, rbac.PermissionFoldersCreate)
}
