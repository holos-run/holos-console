package policyresolver

import (
	"context"
	"errors"
	"testing"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// errorBindingLister returns a hardcoded error for a given namespace and
// forwards all other namespaces to inner. Mirrors errorPolicyLister.
//
// HOL-662 switched the return type to the CRD; there is no JSON
// annotation wire format left to simulate.
type errorBindingLister struct {
	inner   BindingListerInNamespace
	failFor string
	err     error
}

func (e *errorBindingLister) ListBindingsInNamespace(ctx context.Context, ns string) ([]*templatesv1alpha1.TemplatePolicyBinding, error) {
	if ns == e.failFor {
		return nil, e.err
	}
	return e.inner.ListBindingsInNamespace(ctx, ns)
}

// TestAncestorBindingLister_SkipsProjectNamespaces is the HOL-554
// storage-isolation guardrail for bindings: even if a (forbidden) binding
// CR is seeded in a project namespace, the lister must not pick it up.
// Mirrors the guardrail already covered for TemplatePolicy rules.
//
// Post-HOL-618 the CEL ValidatingAdmissionPolicy rejects such a CR at
// admission time; this defensive in-code skip remains as belt-and-
// suspenders so a misconfigured cluster that lacks the VAP still honors
// the isolation boundary.
func TestAncestorBindingLister_SkipsProjectNamespaces(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	// A forbidden binding stashed in a project namespace and a legitimate
	// binding in the org namespace. Only the org-namespace binding should
	// be returned.
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["projectLilies"]: {
			bindingCRD(ns["projectLilies"], "pwned",
				orgPolicyRefCRD("audit"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
			),
		},
		ns["org"]: {
			bindingCRD(ns["org"], "legit",
				orgPolicyRefCRD("audit"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
			),
		},
	}

	lister := NewAncestorBindingLister(
		&bindingListerFromMap{items: bindings},
		walker,
		r,
	)

	got, err := lister.ListBindings(context.Background(), ns["projectLilies"])
	if err != nil {
		t.Fatalf("ListBindings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 binding (the org-namespace one), got %d: %+v", len(got), got)
	}
	if got[0].Name != "legit" {
		t.Errorf("expected the legit org-namespace binding; got %q", got[0].Name)
	}
	if got[0].Namespace != ns["org"] {
		t.Errorf("expected binding namespace %q; got %q", ns["org"], got[0].Namespace)
	}
}

// TestAncestorBindingLister_PerNamespaceErrorIsLogged: a lister error for
// one namespace should not break traversal. The peer namespace's bindings
// are still returned. Mirrors TestFolderResolver_PolicyListerErrorIsLogged.
func TestAncestorBindingLister_PerNamespaceErrorIsLogged(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	inner := &bindingListerFromMap{items: map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "org-bind",
				orgPolicyRefCRD("audit"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
			),
		},
	}}
	wrapped := &errorBindingLister{inner: inner, failFor: ns["folderEng"], err: errors.New("boom")}

	lister := NewAncestorBindingLister(wrapped, walker, r)

	got, err := lister.ListBindings(context.Background(), ns["projectLilies"])
	if err != nil {
		t.Fatalf("ListBindings: %v", err)
	}
	if len(got) != 1 || got[0].Name != "org-bind" {
		t.Errorf("expected org-bind to survive folder-namespace error; got %+v", got)
	}
}

// TestAncestorBindingLister_Misconfigured returns nil without error on any
// nil dependency — the fail-open contract mirrors AncestorPolicyLister.
func TestAncestorBindingLister_Misconfigured(t *testing.T) {
	l := NewAncestorBindingLister(nil, nil, nil)
	got, err := l.ListBindings(context.Background(), "holos-prj-x")
	if err != nil {
		t.Fatalf("expected nil error on misconfigured lister; got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result on misconfigured lister; got %v", got)
	}
}

// TestAncestorBindingLister_EmptyPolicyRefIsNoop verifies that a binding
// whose spec.policyRef is the zero-value struct still returns in the
// lister output — the folder resolver's coverage loop skips entries with
// a nil PolicyRef so the binding contributes no rules, but listing it
// preserves traversal (a peer binding in the same namespace must still
// flow through). This is the post-HOL-662 equivalent of the pre-CRD
// "ParseErrorSkipsBinding" case: the CRD itself validates policyRef via
// kubebuilder tags, so admission rejects a malformed value at create
// time rather than leaving a half-decoded entry on disk.
func TestAncestorBindingLister_EmptyPolicyRefIsNoop(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	empty := bindingCRD(ns["org"], "empty",
		templatesv1alpha1.LinkedTemplatePolicyRef{}, // zero value — unreachable in prod but defensive
		[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
	)
	good := bindingCRD(ns["org"], "good",
		orgPolicyRefCRD("audit"),
		[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
	)

	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {empty, good},
	}

	lister := NewAncestorBindingLister(
		&bindingListerFromMap{items: bindings},
		walker,
		r,
	)

	got, err := lister.ListBindings(context.Background(), ns["projectLilies"])
	if err != nil {
		t.Fatalf("ListBindings: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected both bindings returned (resolver decides coverage); got %d", len(got))
	}
	// The empty one must have a nil PolicyRef — that is how the
	// resolver's coverage loop skips it downstream.
	var emptyResolved, goodResolved *ResolvedBinding
	for _, rb := range got {
		switch rb.Name {
		case "empty":
			emptyResolved = rb
		case "good":
			goodResolved = rb
		}
	}
	if emptyResolved == nil || goodResolved == nil {
		t.Fatalf("expected both bindings present; got %+v", got)
	}
	if emptyResolved.PolicyRef != nil {
		t.Errorf("empty binding should have nil PolicyRef; got %+v", emptyResolved.PolicyRef)
	}
	if goodResolved.PolicyRef == nil || goodResolved.PolicyRef.GetName() != "audit" {
		t.Errorf("good binding should decode to the audit policy; got %+v", goodResolved.PolicyRef)
	}
}

// TestAncestorBindingLister_DecodesPolicyRefAndTargets asserts end-to-end
// decoding: the returned ResolvedBinding carries a decoded policy_ref and
// a decoded target_refs slice. This pins the wire-shape contract between
// the resolver and the templatepolicybindings package.
func TestAncestorBindingLister_DecodesPolicyRefAndTargets(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["folderEng"]: {
			bindingCRD(ns["folderEng"], "eng-bind",
				folderPolicyRefCRD("eng", "eng-audit"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					deploymentTargetCRD("lilies", "api"),
					projectTemplateTargetCRD("lilies", "baseline"),
				},
			),
		},
	}

	lister := NewAncestorBindingLister(
		&bindingListerFromMap{items: bindings},
		walker,
		r,
	)

	got, err := lister.ListBindings(context.Background(), ns["projectLilies"])
	if err != nil {
		t.Fatalf("ListBindings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 binding; got %d", len(got))
	}
	rb := got[0]
	if rb.PolicyRef == nil || rb.PolicyRef.GetNamespace() == "" {
		t.Fatalf("expected decoded policy ref; got %+v", rb.PolicyRef)
	}
	if scopeshim.PolicyRefScope(rb.PolicyRef) != scopeshim.ScopeFolder {
		t.Errorf("expected folder scope; got %v", scopeshim.PolicyRefScope(rb.PolicyRef))
	}
	if scopeshim.PolicyRefScopeName(rb.PolicyRef) != "eng" {
		t.Errorf("expected scope_name=eng; got %q", scopeshim.PolicyRefScopeName(rb.PolicyRef))
	}
	if rb.PolicyRef.GetName() != "eng-audit" {
		t.Errorf("expected policy name=eng-audit; got %q", rb.PolicyRef.GetName())
	}
	if len(rb.TargetRefs) != 2 {
		t.Fatalf("expected 2 target refs; got %d", len(rb.TargetRefs))
	}
	kinds := map[consolev1.TemplatePolicyBindingTargetKind]int{}
	for _, tr := range rb.TargetRefs {
		kinds[tr.GetKind()]++
	}
	if kinds[consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT] != 1 ||
		kinds[consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE] != 1 {
		t.Errorf("expected one deployment and one project-template target; got %+v", kinds)
	}
}
