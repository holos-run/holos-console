package rbac

// TemplatePolicyBindingPerms maps roles to the TemplatePolicyBinding
// permissions each role grants through scope-inherited access on the binding's
// owning scope.
//
// TemplatePolicyBinding is the explicit, non-glob successor to
// TemplatePolicy's target-selector rules (ADR 029, HOL-590). A binding is
// meaningless without its policy — anyone who can read/write the policy can
// read/write the set of targets that policy applies to — so bindings reuse
// the PERMISSION_TEMPLATE_POLICIES_* permission family and share an
// identical permission shape with TemplatePolicyPerms.
//
// WRITE, DELETE, and ADMIN MUST only be reachable via organization or folder
// grants (HOL-554 storage-isolation design note). Project-owner roles MAY be
// granted at most LIST and READ through this table because project-namespace
// storage is forbidden and projects never own a binding ConfigMap directly —
// read flows through ancestor traversal to the folder/org binding
// ConfigMaps.
//
// The permission table itself does not know the scope it is evaluated at; the
// handler is responsible for choosing the correct grants (org or folder) and
// rejecting any attempt to evaluate against a project namespace. Keeping the
// permissions in a single table instead of a per-scope variant avoids a
// divergent code path that could accidentally admit a project owner to binding
// writes.
var TemplatePolicyBindingPerms = map[Role]map[Permission]bool{
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
