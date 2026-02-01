package organizations

import (
	"github.com/holos-run/holos-console/console/rbac"
)

// CheckOrgReadAccess verifies the user has read permission on the organization.
func CheckOrgReadAccess(email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return rbac.CheckAccessGrants(email, groups, shareUsers, shareGroups, rbac.PermissionOrganizationsRead)
}

// CheckOrgWriteAccess verifies the user has write permission on the organization.
func CheckOrgWriteAccess(email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return rbac.CheckAccessGrants(email, groups, shareUsers, shareGroups, rbac.PermissionOrganizationsWrite)
}

// CheckOrgDeleteAccess verifies the user has delete permission on the organization.
func CheckOrgDeleteAccess(email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return rbac.CheckAccessGrants(email, groups, shareUsers, shareGroups, rbac.PermissionOrganizationsDelete)
}

// CheckOrgAdminAccess verifies the user has admin permission on the organization.
func CheckOrgAdminAccess(email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return rbac.CheckAccessGrants(email, groups, shareUsers, shareGroups, rbac.PermissionOrganizationsAdmin)
}

// CheckOrgListAccess verifies the user has list permission on the organization.
func CheckOrgListAccess(email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return rbac.CheckAccessGrants(email, groups, shareUsers, shareGroups, rbac.PermissionOrganizationsList)
}
