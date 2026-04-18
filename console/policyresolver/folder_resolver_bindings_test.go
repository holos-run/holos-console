package policyresolver

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// storedPolicyRefTest mirrors the templatepolicybindings.storedPolicyRef wire
// shape. Keeping a vendored copy in test code avoids a cross-package import
// (and, more importantly, makes the test's assertions fail loudly if the
// production wire shape drifts from what the resolver consumes).
type storedPolicyRefTest struct {
	Scope     string `json:"scope"`
	ScopeName string `json:"scopeName"`
	Name      string `json:"name"`
}

type storedTargetRefTest struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	ProjectName string `json:"projectName"`
}

// testUnmarshalPolicyRef mirrors templatepolicybindings.UnmarshalPolicyRef.
// A drift between the two would silently change test assertions, so the
// minimal decoder is vendored here.
func testUnmarshalPolicyRef(raw string) (*consolev1.LinkedTemplatePolicyRef, error) {
	if raw == "" {
		return nil, nil
	}
	var sr storedPolicyRefTest
	if err := json.Unmarshal([]byte(raw), &sr); err != nil {
		return nil, err
	}
	return &consolev1.LinkedTemplatePolicyRef{
		ScopeRef: &consolev1.TemplateScopeRef{
			Scope:     scopeFromTemplateLabelTest(sr.Scope),
			ScopeName: sr.ScopeName,
		},
		Name: sr.Name,
	}, nil
}

// testUnmarshalTargetRefs mirrors templatepolicybindings.UnmarshalTargetRefs.
func testUnmarshalTargetRefs(raw string) ([]*consolev1.TemplatePolicyBindingTargetRef, error) {
	if raw == "" {
		return nil, nil
	}
	var stored []storedTargetRefTest
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, err
	}
	refs := make([]*consolev1.TemplatePolicyBindingTargetRef, 0, len(stored))
	for _, s := range stored {
		refs = append(refs, &consolev1.TemplatePolicyBindingTargetRef{
			Kind:        targetKindFromStringTest(s.Kind),
			Name:        s.Name,
			ProjectName: s.ProjectName,
		})
	}
	return refs, nil
}

func scopeFromTemplateLabelTest(label string) consolev1.TemplateScope {
	switch label {
	case v1alpha2.TemplateScopeOrganization:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION
	case v1alpha2.TemplateScopeFolder:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER
	case v1alpha2.TemplateScopeProject:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT
	default:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED
	}
}

func targetKindFromStringTest(s string) consolev1.TemplatePolicyBindingTargetKind {
	switch s {
	case "project-template":
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE
	case "deployment":
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT
	default:
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_UNSPECIFIED
	}
}

// bindingListerFromMap adapts an in-memory map to BindingListerInNamespace.
// Mirrors policyListerFromClient — tests feed per-namespace binding
// ConfigMaps and the resolver reads them through the same ancestor-walking
// lister production uses.
type bindingListerFromMap struct {
	items map[string][]corev1.ConfigMap
}

func (b *bindingListerFromMap) ListBindingsInNamespace(_ context.Context, ns string) ([]corev1.ConfigMap, error) {
	return b.items[ns], nil
}

// bindingCM returns a fake TemplatePolicyBinding ConfigMap with the given
// policy_ref and target_refs encoded into the annotations. Mirrors policyCM
// but populates the binding's JSON annotation wire shape.
func bindingCM(namespace, name string, policyRef storedPolicyRefTest, targets []storedTargetRefTest, t *testing.T) corev1.ConfigMap {
	policyJSON, err := json.Marshal(policyRef)
	if err != nil {
		t.Fatalf("marshal policy ref: %v", err)
	}
	targetsJSON, err := json.Marshal(targets)
	if err != nil {
		t.Fatalf("marshal target refs: %v", err)
	}
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicyBinding,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationTemplatePolicyBindingPolicyRef:  string(policyJSON),
				v1alpha2.AnnotationTemplatePolicyBindingTargetRefs: string(targetsJSON),
			},
		},
	}
}

// newFolderResolverWithBindingsForTest constructs a resolver wired with
// both rule-side and binding-side deps, so the tests in this file and
// folder_resolver_test.go exercise the post-HOL-600 binding evaluation
// path.
func newFolderResolverWithBindingsForTest(
	policies PolicyListerInNamespace,
	bindings *bindingListerFromMap,
	walker WalkerInterface,
	r *resolver.Resolver,
) PolicyResolver {
	return NewFolderResolverWithBindings(
		policies,
		walker,
		r,
		RuleUnmarshalerFunc(testUnmarshalRules),
		bindings,
		BindingUnmarshalerAdapter{
			PolicyRefFunc:  testUnmarshalPolicyRef,
			TargetRefsFunc: testUnmarshalTargetRefs,
		},
	)
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
	bindings := map[string][]corev1.ConfigMap{
		ns["org"]: {
			bindingCM(ns["org"], "orphan",
				storedPolicyRefTest{Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "missing"},
				[]storedTargetRefTest{{Kind: "deployment", Name: "api", ProjectName: "lilies"}},
				t,
			),
		},
	}

	explicit := []*consolev1.LinkedTemplateRef{
		{
			Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
			ScopeName: "acme",
			Name:      "explicit",
		},
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

	policies := map[string][]corev1.ConfigMap{
		ns["org"]: {
			policyCM(ns["org"], "audit", []storedRuleTest{
				requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
			}, t),
		},
	}
	// Binding with empty target_refs. The JSON wire shape is "[]" which
	// decodes to an empty slice. The binding exists but covers nothing.
	bindings := map[string][]corev1.ConfigMap{
		ns["org"]: {
			bindingCM(ns["org"], "empty-bind",
				orgPolicyRefStored("audit"),
				nil, // zero targets
				t,
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

	policies := map[string][]corev1.ConfigMap{
		ns["org"]: {
			policyCM(ns["org"], "audit", []storedRuleTest{
				requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
			}, t),
		},
	}
	// Binding targets roses/api, render target is lilies/api — no
	// match on project_name.
	bindings := map[string][]corev1.ConfigMap{
		ns["org"]: {
			bindingCM(ns["org"], "audit-to-roses",
				orgPolicyRefStored("audit"),
				[]storedTargetRefTest{deploymentTargetStored("roses", "api")},
				t,
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
