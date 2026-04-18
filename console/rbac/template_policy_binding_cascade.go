package rbac

// TemplatePolicyBindingCascadePerms maps roles to the TemplatePolicyBinding
// permissions each role grants via cascade on the binding's owning scope.
//
// TemplatePolicyBinding is the explicit, non-glob successor to
// TemplatePolicy's target-selector rules (ADR 029, HOL-590). A binding is
// meaningless without its policy — anyone who can read/write the policy can
// read/write the set of targets that policy applies to — so bindings reuse
// the PERMISSION_TEMPLATE_POLICIES_* permission family and share an
// identical cascade shape with TemplatePolicyCascadePerms.
//
// WRITE, DELETE, and ADMIN MUST only be reachable via organization or folder
// grants (HOL-554 storage-isolation design note). Project-owner roles MAY be
// granted at most LIST and READ through this table because project-namespace
// storage is forbidden and projects never own a binding ConfigMap directly —
// read flows through ancestor traversal to the folder/org binding
// ConfigMaps.
//
// The cascade table itself does not know the scope it is evaluated at; the
// handler is responsible for choosing the correct grants (org or folder) and
// rejecting any attempt to evaluate against a project namespace. Keeping the
// permissions in a single table — rather than a per-scope variant — matches
// the TemplateCascadePerms / TemplatePolicyCascadePerms pattern (ADR 017
// Decision 5, ADR 021 Decision 2, ADR 029) and avoids a divergent code path
// that could accidentally admit a project owner to binding writes.
var TemplatePolicyBindingCascadePerms = CascadeTable{
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
