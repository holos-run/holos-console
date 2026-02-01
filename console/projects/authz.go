package projects

import (
	"time"

	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/secrets"
	corev1 "k8s.io/api/core/v1"
)

// timeNow is a function returning the current time, overridable in tests.
var timeNow = time.Now

// CheckProjectReadAccess verifies the user has read permission on the project.
func CheckProjectReadAccess(email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return rbac.CheckAccessGrants(email, groups, shareUsers, shareGroups, rbac.PermissionProjectsRead)
}

// CheckProjectWriteAccess verifies the user has write permission on the project.
func CheckProjectWriteAccess(email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return rbac.CheckAccessGrants(email, groups, shareUsers, shareGroups, rbac.PermissionProjectsWrite)
}

// CheckProjectDeleteAccess verifies the user has delete permission on the project.
func CheckProjectDeleteAccess(email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return rbac.CheckAccessGrants(email, groups, shareUsers, shareGroups, rbac.PermissionProjectsDelete)
}

// CheckProjectAdminAccess verifies the user has admin permission on the project.
func CheckProjectAdminAccess(email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return rbac.CheckAccessGrants(email, groups, shareUsers, shareGroups, rbac.PermissionProjectsAdmin)
}

// CheckProjectListAccess verifies the user has list permission on the project.
func CheckProjectListAccess(email string, groups []string, shareUsers, shareGroups map[string]string) error {
	return rbac.CheckAccessGrants(email, groups, shareUsers, shareGroups, rbac.PermissionProjectsList)
}

// CheckProjectCreateAccess verifies the user is an owner on at least one existing project.
func CheckProjectCreateAccess(email string, groups []string, allProjects []*corev1.Namespace) error {
	for _, ns := range allProjects {
		shareUsers, _ := GetShareUsers(ns)
		shareGroups, _ := GetShareGroups(ns)
		activeUsers := secrets.ActiveGrantsMap(shareUsers, timeNow())
		activeGroups := secrets.ActiveGrantsMap(shareGroups, timeNow())
		if err := rbac.CheckAccessGrants(email, groups, activeUsers, activeGroups, rbac.PermissionProjectsCreate); err == nil {
			return nil
		}
	}
	return rbac.CheckAccessGrants(email, groups, nil, nil, rbac.PermissionProjectsCreate)
}
