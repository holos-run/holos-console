package rbac

import "testing"

// TestTemplatePolicyBindingCascadePermsMatchesPolicyTable locks in the
// invariant that TemplatePolicyBinding cascades grant exactly the same
// permissions as TemplatePolicy cascades. HOL-595 adopts the policy cascade
// shape verbatim — a drift here would mean the binding handler and the
// policy handler interpret the same grant differently, which is the opposite
// of what ADR 029 intends.
func TestTemplatePolicyBindingCascadePermsMatchesPolicyTable(t *testing.T) {
	if len(TemplatePolicyBindingCascadePerms) != len(TemplatePolicyCascadePerms) {
		t.Fatalf("binding cascade has %d roles, policy cascade has %d",
			len(TemplatePolicyBindingCascadePerms), len(TemplatePolicyCascadePerms))
	}
	for role, policyPerms := range TemplatePolicyCascadePerms {
		bindingPerms, ok := TemplatePolicyBindingCascadePerms[role]
		if !ok {
			t.Errorf("role %v missing from binding cascade", role)
			continue
		}
		if len(bindingPerms) != len(policyPerms) {
			t.Errorf("role %v: binding cascade has %d perms, policy cascade has %d",
				role, len(bindingPerms), len(policyPerms))
			continue
		}
		for perm, want := range policyPerms {
			if got := bindingPerms[perm]; got != want {
				t.Errorf("role %v perm %v: binding cascade has %v, policy cascade has %v",
					role, perm, got, want)
			}
		}
	}
}

// TestTemplatePolicyBindingCascadeOnlyPolicyPerms keeps the cascade focused
// on the PERMISSION_TEMPLATE_POLICIES_* family. If someone later tries to
// attach, e.g., PERMISSION_TEMPLATES_WRITE here, the check below fails —
// binding cascades must not leak authority into other resource families.
func TestTemplatePolicyBindingCascadeOnlyPolicyPerms(t *testing.T) {
	allowed := map[Permission]bool{
		PermissionTemplatePoliciesList:   true,
		PermissionTemplatePoliciesRead:   true,
		PermissionTemplatePoliciesWrite:  true,
		PermissionTemplatePoliciesDelete: true,
		PermissionTemplatePoliciesAdmin:  true,
	}
	for role, perms := range TemplatePolicyBindingCascadePerms {
		for perm := range perms {
			if !allowed[perm] {
				t.Errorf("role %v: unexpected permission %v in binding cascade", role, perm)
			}
		}
	}
}

// TestTemplatePolicyBindingCascadeRoleCoverage asserts that the three roles
// the rest of the RBAC subsystem understands (Viewer, Editor, Owner) each
// appear in the cascade. A missing role would silently deny all bindings
// access for users who only hold that role at the scope.
func TestTemplatePolicyBindingCascadeRoleCoverage(t *testing.T) {
	for _, role := range []Role{RoleViewer, RoleEditor, RoleOwner} {
		if _, ok := TemplatePolicyBindingCascadePerms[role]; !ok {
			t.Errorf("binding cascade missing role %v", role)
		}
	}
}
