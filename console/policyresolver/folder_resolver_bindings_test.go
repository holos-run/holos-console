package policyresolver

import (
	"context"
	"testing"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// bindingListerFromMap adapts an in-memory map to BindingListerInNamespace.
// Mirrors policyListerFromClient — tests feed per-namespace
// TemplatePolicyBinding CRs and the resolver reads them through the same
// ancestor-walking lister production uses.
//
// HOL-662 switched the return type from corev1.ConfigMap to the CRD shape;
// tests no longer vendor a ConfigMap decoder.
type bindingListerFromMap struct {
	items map[string][]templatesv1alpha1.TemplatePolicyBinding
}

func (b *bindingListerFromMap) ListBindingsInNamespace(_ context.Context, ns string) ([]templatesv1alpha1.TemplatePolicyBinding, error) {
	return b.items[ns], nil
}

// bindingCRD returns a TemplatePolicyBinding CR populated with the given
// policy_ref and target_refs. Mirrors policyCRD. Tests build fixtures from
// this helper rather than constructing raw CRDs inline so the shape stays
// consistent across the suite and so a schema drift surfaces in exactly one
// place.
func bindingCRD(
	namespace, name string,
	policyRef templatesv1alpha1.LinkedTemplatePolicyRef,
	targets []templatesv1alpha1.TemplatePolicyBindingTargetRef,
) templatesv1alpha1.TemplatePolicyBinding {
	return templatesv1alpha1.TemplatePolicyBinding{
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef:  policyRef,
			TargetRefs: targets,
		},
		// metav1.ObjectMeta embedded directly — the CRD exposes the
		// Name/Namespace fields via the anonymous ObjectMeta struct.
		ObjectMeta: objectMetaCRD(name, namespace, v1alpha2.ResourceTypeTemplatePolicyBinding),
	}
}

// orgPolicyRefCRD builds an organization-scoped CRD policy_ref.
func orgPolicyRefCRD(policyName string) templatesv1alpha1.LinkedTemplatePolicyRef {
	return templatesv1alpha1.LinkedTemplatePolicyRef{
		Scope:     v1alpha2.TemplateScopeOrganization,
		ScopeName: "acme",
		Name:      policyName,
	}
}

// folderPolicyRefCRD builds a folder-scoped CRD policy_ref.
func folderPolicyRefCRD(folder, policyName string) templatesv1alpha1.LinkedTemplatePolicyRef {
	return templatesv1alpha1.LinkedTemplatePolicyRef{
		Scope:     v1alpha2.TemplateScopeFolder,
		ScopeName: folder,
		Name:      policyName,
	}
}

// deploymentTargetCRD / projectTemplateTargetCRD return the binding-side
// target refs the table cases use.
func deploymentTargetCRD(project, name string) templatesv1alpha1.TemplatePolicyBindingTargetRef {
	return templatesv1alpha1.TemplatePolicyBindingTargetRef{
		Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
		Name:        name,
		ProjectName: project,
	}
}

func projectTemplateTargetCRD(project, name string) templatesv1alpha1.TemplatePolicyBindingTargetRef {
	return templatesv1alpha1.TemplatePolicyBindingTargetRef{
		Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
		Name:        name,
		ProjectName: project,
	}
}

// newFolderResolverWithBindingsForTest constructs a resolver wired with
// both rule-side and binding-side deps, so the tests in this file and
// folder_resolver_test.go exercise the post-HOL-600 binding evaluation
// path. HOL-662 dropped the unmarshaler seams — construction is now
// 4-arg.
func newFolderResolverWithBindingsForTest(
	policies PolicyListerInNamespace,
	bindings *bindingListerFromMap,
	walker WalkerInterface,
	r *resolver.Resolver,
) PolicyResolver {
	return NewFolderResolverWithBindings(policies, walker, r, bindings)
}

// TestFolderResolver_BindingsNonexistentPolicyIsNoopAndDoesNotError
// asserts a binding whose policy_ref does not resolve to any policy in
// the ancestor chain is treated as a no-op: the resolver does not
// error, no refs are injected, and the explicit refs passed by the
// caller are returned unchanged. This is the degrade-gracefully
// contract inherited from HOL-596.
func TestFolderResolver_BindingsNonexistentPolicyIsNoopAndDoesNotError(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	// No policies exist — the binding points at "missing" which has
	// nothing to resolve to.
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "orphan",
				orgPolicyRefCRD("missing"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
			),
		},
	}

	explicit := []*consolev1.LinkedTemplateRef{
		orgTemplateRef("explicit"),
	}
	pl := &policyListerFromClient{items: nil}
	bl := &bindingListerFromMap{items: bindings}
	fr := newFolderResolverWithBindingsForTest(pl, bl, walker, r)

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", explicit)
	if err != nil {
		t.Fatalf("Resolve returned error on nonexistent policy; expected no-op: %v", err)
	}
	names := refNames(got)
	if len(names) != 1 || names[0] != "explicit" {
		t.Errorf("expected explicit refs passed through, got %v", names)
	}
}

// TestFolderResolver_BindingsEmptyTargetListContributesNothing asserts a
// binding with no target_refs never covers any render target — an empty
// target list declares intent to attach zero render targets. Post-
// HOL-600 the bound policy's rules also contribute nothing because
// selection runs exclusively through the binding target_refs.
func TestFolderResolver_BindingsEmptyTargetListContributesNothing(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	policies := map[string][]templatesv1alpha1.TemplatePolicy{
		ns["org"]: {
			policyCRD(ns["org"], "audit", []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
			}),
		},
	}
	// Binding with empty target_refs. The binding exists but covers nothing.
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "empty-bind",
				orgPolicyRefCRD("audit"),
				nil, // zero targets
			),
		},
	}

	pl := &policyListerFromClient{items: policies}
	bl := &bindingListerFromMap{items: bindings}
	fr := newFolderResolverWithBindingsForTest(pl, bl, walker, r)

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no refs when binding covers zero targets, got %v", refNames(got))
	}
}

// TestFolderResolver_BindingProjectNameMismatchContributesNothing: a
// binding whose target_refs reference a different project than the
// render target does not select the current target. The bound
// policy's rules do not contribute because bindings are the sole
// selector post-HOL-600.
func TestFolderResolver_BindingProjectNameMismatchContributesNothing(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	policies := map[string][]templatesv1alpha1.TemplatePolicy{
		ns["org"]: {
			policyCRD(ns["org"], "audit", []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
			}),
		},
	}
	// Binding targets roses/api, render target is lilies/api — no
	// match on project_name.
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "audit-to-roses",
				orgPolicyRefCRD("audit"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("roses", "api")},
			),
		},
	}

	pl := &policyListerFromClient{items: policies}
	bl := &bindingListerFromMap{items: bindings}
	fr := newFolderResolverWithBindingsForTest(pl, bl, walker, r)

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no refs when binding names a different project, got %v", refNames(got))
	}
}
