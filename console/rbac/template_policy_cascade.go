package rbac

import (
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// Template policy permission aliases (HOL-554 / HOL-556). These mirror the
// TemplateCascadePerms aliases so handlers can reference permissions via the
// rbac package instead of reaching into the generated enum directly.
const (
	PermissionTemplatePoliciesList   = consolev1.Permission_PERMISSION_TEMPLATE_POLICIES_LIST
	PermissionTemplatePoliciesRead   = consolev1.Permission_PERMISSION_TEMPLATE_POLICIES_READ
	PermissionTemplatePoliciesWrite  = consolev1.Permission_PERMISSION_TEMPLATE_POLICIES_WRITE
	PermissionTemplatePoliciesDelete = consolev1.Permission_PERMISSION_TEMPLATE_POLICIES_DELETE
	PermissionTemplatePoliciesAdmin  = consolev1.Permission_PERMISSION_TEMPLATE_POLICIES_ADMIN
)

// TemplatePolicyCascadePerms maps roles to the TemplatePolicy permissions each
// role grants via cascade on the policy's owning scope.
//
// WRITE, DELETE, and ADMIN MUST only be reachable via organization or folder
// grants (HOL-554 storage-isolation design note). Project-owner roles MAY be
// granted at most LIST and READ through this table because project-namespace
// storage is forbidden and projects never own a policy ConfigMap directly —
// read flows through ancestor traversal to the folder/org policy ConfigMaps
// (HOL-557 will materialize that traversal in the resolver).
//
// The cascade table itself does not know the scope it is evaluated at; the
// handler is responsible for choosing the correct grants (org or folder) and
// rejecting any attempt to evaluate against a project namespace. Keeping the
// permissions in a single table — rather than a per-scope variant — matches
// the TemplateCascadePerms pattern (ADR 017 Decision 5, ADR 021 Decision 2)
// and avoids a divergent code path that could accidentally admit a project
// owner to policy writes.
var TemplatePolicyCascadePerms = CascadeTable{
	RoleViewer: {
		PermissionTemplatePoliciesList: true,
		PermissionTemplatePoliciesRead: true,
	},
	RoleEditor: {
		PermissionTemplatePoliciesList:  true,
		PermissionTemplatePoliciesRead:  true,
		PermissionTemplatePoliciesWrite: true,
	},
	RoleOwner: {
		PermissionTemplatePoliciesList:   true,
		PermissionTemplatePoliciesRead:   true,
		PermissionTemplatePoliciesWrite:  true,
		PermissionTemplatePoliciesDelete: true,
		PermissionTemplatePoliciesAdmin:  true,
	},
}
