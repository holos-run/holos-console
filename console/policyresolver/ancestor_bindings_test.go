package policyresolver

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// errorBindingLister returns a hardcoded error for a given namespace and
// forwards all other namespaces to inner. Mirrors errorPolicyLister.
type errorBindingLister struct {
	inner   BindingListerInNamespace
	failFor string
	err     error
}

func (e *errorBindingLister) ListBindingsInNamespace(ctx context.Context, ns string) ([]corev1.ConfigMap, error) {
	if ns == e.failFor {
		return nil, e.err
	}
	return e.inner.ListBindingsInNamespace(ctx, ns)
}

// TestAncestorBindingLister_SkipsProjectNamespaces is the HOL-554
// storage-isolation guardrail for bindings: even if a (forbidden) binding
// ConfigMap is seeded in a project namespace, the lister must not pick it
// up. Mirrors the guardrail already covered for TemplatePolicy rules.
func TestAncestorBindingLister_SkipsProjectNamespaces(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	// A forbidden binding stashed in a project namespace and a legitimate
	// binding in the org namespace. Only the org-namespace binding should
	// be returned.
	bindings := map[string][]corev1.ConfigMap{
		ns["projectLilies"]: {
			bindingCM(ns["projectLilies"], "pwned",
				storedPolicyRefTest{Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit"},
				[]storedTargetRefTest{{Kind: "deployment", Name: "api", ProjectName: "lilies"}},
				t,
			),
		},
		ns["org"]: {
			bindingCM(ns["org"], "legit",
				storedPolicyRefTest{Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit"},
				[]storedTargetRefTest{{Kind: "deployment", Name: "api", ProjectName: "lilies"}},
				t,
			),
		},
	}

	lister := NewAncestorBindingLister(
		&bindingListerFromMap{items: bindings},
		walker,
		r,
		BindingUnmarshalerAdapter{
			PolicyRefFunc:  testUnmarshalPolicyRef,
			TargetRefsFunc: testUnmarshalTargetRefs,
		},
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

	inner := &bindingListerFromMap{items: map[string][]corev1.ConfigMap{
		ns["org"]: {
			bindingCM(ns["org"], "org-bind",
				storedPolicyRefTest{Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit"},
				[]storedTargetRefTest{{Kind: "deployment", Name: "api", ProjectName: "lilies"}},
				t,
			),
		},
	}}
	wrapped := &errorBindingLister{inner: inner, failFor: ns["folderEng"], err: errors.New("boom")}

	lister := NewAncestorBindingLister(
		wrapped,
		walker,
		r,
		BindingUnmarshalerAdapter{
			PolicyRefFunc:  testUnmarshalPolicyRef,
			TargetRefsFunc: testUnmarshalTargetRefs,
		},
	)

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
	l := NewAncestorBindingLister(nil, nil, nil, nil)
	got, err := l.ListBindings(context.Background(), "holos-prj-x")
	if err != nil {
		t.Fatalf("expected nil error on misconfigured lister; got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result on misconfigured lister; got %v", got)
	}
}

// TestAncestorBindingLister_ParseErrorSkipsBinding verifies that a binding
// whose policy_ref annotation fails to decode is skipped but does not abort
// the traversal. A second well-formed binding in the same namespace is
// returned unchanged.
func TestAncestorBindingLister_ParseErrorSkipsBinding(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bad := bindingCM(ns["org"], "bad",
		storedPolicyRefTest{Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit"},
		[]storedTargetRefTest{{Kind: "deployment", Name: "api", ProjectName: "lilies"}},
		t,
	)
	bad.Annotations[v1alpha2.AnnotationTemplatePolicyBindingPolicyRef] = "{not valid json"

	good := bindingCM(ns["org"], "good",
		storedPolicyRefTest{Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit"},
		[]storedTargetRefTest{{Kind: "deployment", Name: "api", ProjectName: "lilies"}},
		t,
	)

	bindings := map[string][]corev1.ConfigMap{
		ns["org"]: {bad, good},
	}

	lister := NewAncestorBindingLister(
		&bindingListerFromMap{items: bindings},
		walker,
		r,
		BindingUnmarshalerAdapter{
			PolicyRefFunc:  testUnmarshalPolicyRef,
			TargetRefsFunc: testUnmarshalTargetRefs,
		},
	)

	got, err := lister.ListBindings(context.Background(), ns["projectLilies"])
	if err != nil {
		t.Fatalf("ListBindings: %v", err)
	}
	if len(got) != 1 || got[0].Name != "good" {
		t.Errorf("expected parse-error binding to be skipped and the good one returned; got %+v", got)
	}
}

// TestAncestorBindingLister_DecodesPolicyRefAndTargets asserts end-to-end
// decoding: the returned ResolvedBinding carries a decoded policy_ref and
// a decoded target_refs slice. This pins the wire-shape contract between
// the resolver and the templatepolicybindings package.
func TestAncestorBindingLister_DecodesPolicyRefAndTargets(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bindings := map[string][]corev1.ConfigMap{
		ns["folderEng"]: {
			bindingCM(ns["folderEng"], "eng-bind",
				storedPolicyRefTest{Scope: v1alpha2.TemplateScopeFolder, ScopeName: "eng", Name: "eng-audit"},
				[]storedTargetRefTest{
					{Kind: "deployment", Name: "api", ProjectName: "lilies"},
					{Kind: "project-template", Name: "baseline", ProjectName: "lilies"},
				},
				t,
			),
		},
	}

	lister := NewAncestorBindingLister(
		&bindingListerFromMap{items: bindings},
		walker,
		r,
		BindingUnmarshalerAdapter{
			PolicyRefFunc:  testUnmarshalPolicyRef,
			TargetRefsFunc: testUnmarshalTargetRefs,
		},
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
