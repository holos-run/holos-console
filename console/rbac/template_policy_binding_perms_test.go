package rbac

import "testing"

// TestTemplatePolicyBindingPermsMatchesPolicyTable locks in the invariant
// that TemplatePolicyBinding grants exactly the same permissions as
// TemplatePolicy grants. HOL-595 adopts the policy permission shape verbatim;
// a drift here would mean the binding handler and policy handler interpret
// the same grant differently, which is the opposite of what ADR 029 intends.
func TestTemplatePolicyBindingPermsMatchesPolicyTable(t *testing.T) {
	if len(TemplatePolicyBindingPerms) != len(TemplatePolicyPerms) {
		t.Fatalf("binding permissions have %d roles, policy permissions have %d",
			len(TemplatePolicyBindingPerms), len(TemplatePolicyPerms))
	}
	for role, policyPerms := range TemplatePolicyPerms {
		bindingPerms, ok := TemplatePolicyBindingPerms[role]
		if !ok {
			t.Errorf("role %v missing from binding permissions", role)
			continue
		}
		if len(bindingPerms) != len(policyPerms) {
			t.Errorf("role %v: binding permissions have %d perms, policy permissions have %d",
				role, len(bindingPerms), len(policyPerms))
			continue
		}
		for perm, want := range policyPerms {
			if got := bindingPerms[perm]; got != want {
				t.Errorf("role %v perm %v: binding permissions have %v, policy permissions have %v",
					role, perm, got, want)
			}
		}
	}
}

// TestTemplatePolicyBindingOnlyPolicyPerms keeps the table focused on the
// PERMISSION_TEMPLATE_POLICIES_* family. If someone later tries to attach,
// e.g., PERMISSION_TEMPLATES_WRITE here, the check below fails: binding
// permissions must not leak authority into other resource families.
func TestTemplatePolicyBindingOnlyPolicyPerms(t *testing.T) {
	allowed := map[Permission]bool{
		PermissionTemplatePoliciesList:   true,
		PermissionTemplatePoliciesRead:   true,
		PermissionTemplatePoliciesWrite:  true,
		PermissionTemplatePoliciesDelete: true,
		PermissionTemplatePoliciesAdmin:  true,
	}
	for role, perms := range TemplatePolicyBindingPerms {
		for perm := range perms {
			if !allowed[perm] {
				t.Errorf("role %v: unexpected permission %v in binding permissions", role, perm)
			}
		}
	}
}

// TestTemplatePolicyBindingRoleCoverage asserts that the three roles
// the rest of the RBAC subsystem understands (Viewer, Editor, Owner) each
// appear in the table. A missing role would silently deny all bindings
// access for users who only hold that role at the scope.
func TestTemplatePolicyBindingRoleCoverage(t *testing.T) {
	for _, role := range []Role{RoleViewer, RoleEditor, RoleOwner} {
		if _, ok := TemplatePolicyBindingPerms[role]; !ok {
			t.Errorf("binding permissions missing role %v", role)
		}
	}
}
