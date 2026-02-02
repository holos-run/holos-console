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
func CheckProjectReadAccess(email string, roles []string, shareUsers, shareRoles map[string]string) error {
	return rbac.CheckAccessGrants(email, roles, shareUsers, shareRoles, rbac.PermissionProjectsRead)
}

// CheckProjectWriteAccess verifies the user has write permission on the project.
func CheckProjectWriteAccess(email string, roles []string, shareUsers, shareRoles map[string]string) error {
	return rbac.CheckAccessGrants(email, roles, shareUsers, shareRoles, rbac.PermissionProjectsWrite)
}

// CheckProjectDeleteAccess verifies the user has delete permission on the project.
func CheckProjectDeleteAccess(email string, roles []string, shareUsers, shareRoles map[string]string) error {
	return rbac.CheckAccessGrants(email, roles, shareUsers, shareRoles, rbac.PermissionProjectsDelete)
}

// CheckProjectAdminAccess verifies the user has admin permission on the project.
func CheckProjectAdminAccess(email string, roles []string, shareUsers, shareRoles map[string]string) error {
	return rbac.CheckAccessGrants(email, roles, shareUsers, shareRoles, rbac.PermissionProjectsAdmin)
}

// CheckProjectListAccess verifies the user has list permission on the project.
func CheckProjectListAccess(email string, roles []string, shareUsers, shareRoles map[string]string) error {
	return rbac.CheckAccessGrants(email, roles, shareUsers, shareRoles, rbac.PermissionProjectsList)
}

// CheckProjectCreateAccess verifies the user is an owner on at least one existing project.
func CheckProjectCreateAccess(email string, roles []string, allProjects []*corev1.Namespace) error {
	for _, ns := range allProjects {
		shareUsers, _ := GetShareUsers(ns)
		shareRoles, _ := GetShareRoles(ns)
		activeUsers := secrets.ActiveGrantsMap(shareUsers, timeNow())
		activeRoles := secrets.ActiveGrantsMap(shareRoles, timeNow())
		if err := rbac.CheckAccessGrants(email, roles, activeUsers, activeRoles, rbac.PermissionProjectsCreate); err == nil {
			return nil
		}
	}
	return rbac.CheckAccessGrants(email, roles, nil, nil, rbac.PermissionProjectsCreate)
}
