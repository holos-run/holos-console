package organizations

import (
	"time"

	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/secrets"
	corev1 "k8s.io/api/core/v1"
)

// timeNow is a function returning the current time, overridable in tests.
var timeNow = time.Now

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

// CheckOrgCreateAccess verifies the user is an owner on at least one existing organization.
func CheckOrgCreateAccess(email string, groups []string, allOrgs []*corev1.Namespace) error {
	for _, ns := range allOrgs {
		shareUsers, _ := GetShareUsers(ns)
		shareGroups, _ := GetShareGroups(ns)
		activeUsers := secrets.ActiveGrantsMap(shareUsers, timeNow())
		activeGroups := secrets.ActiveGrantsMap(shareGroups, timeNow())
		if err := rbac.CheckAccessGrants(email, groups, activeUsers, activeGroups, rbac.PermissionOrganizationsCreate); err == nil {
			return nil
		}
	}
	return rbac.CheckAccessGrants(email, groups, nil, nil, rbac.PermissionOrganizationsCreate)
}
