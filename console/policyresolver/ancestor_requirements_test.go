/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package policyresolver

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	"github.com/holos-run/holos-console/console/resolver"
)

// requirementListerFromMap adapts an in-memory map to
// RequirementListerInNamespace. Mirrors bindingListerFromMap.
type requirementListerFromMap struct {
	items map[string][]templatesv1alpha1.TemplateRequirement
}

func (r *requirementListerFromMap) ListRequirementsInNamespace(_ context.Context, ns string) ([]*templatesv1alpha1.TemplateRequirement, error) {
	src := r.items[ns]
	if len(src) == 0 {
		return nil, nil
	}
	out := make([]*templatesv1alpha1.TemplateRequirement, 0, len(src))
	for i := range src {
		out = append(out, &src[i])
	}
	return out, nil
}

// errorRequirementLister returns a hardcoded error for a given namespace and
// forwards all other namespaces to inner. Mirrors errorBindingLister.
type errorRequirementLister struct {
	inner   RequirementListerInNamespace
	failFor string
	err     error
}

func (e *errorRequirementLister) ListRequirementsInNamespace(ctx context.Context, ns string) ([]*templatesv1alpha1.TemplateRequirement, error) {
	if ns == e.failFor {
		return nil, e.err
	}
	return e.inner.ListRequirementsInNamespace(ctx, ns)
}

// requirementCRD builds a minimal TemplateRequirement CRD for testing.
func requirementCRD(
	ns, name string,
	requires templatesv1alpha1.LinkedTemplateRef,
	targetRefs []templatesv1alpha1.TemplateRequirementTargetRef,
	cascadeDelete *bool,
) templatesv1alpha1.TemplateRequirement {
	return templatesv1alpha1.TemplateRequirement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: templatesv1alpha1.TemplateRequirementSpec{
			Requires:      requires,
			TargetRefs:    targetRefs,
			CascadeDelete: cascadeDelete,
		},
	}
}

// deploymentRequirementTargetRef returns a DEPLOYMENT-kind target ref for
// use in TemplateRequirement specs.
func deploymentRequirementTargetRef(project, name string) templatesv1alpha1.TemplateRequirementTargetRef {
	return templatesv1alpha1.TemplateRequirementTargetRef{
		Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
		Name:        name,
		ProjectName: project,
	}
}

// wildcardRequirementTargetRef returns a DEPLOYMENT-kind target ref with
// the projectName: "*" wildcard.
func wildcardRequirementTargetRef() templatesv1alpha1.TemplateRequirementTargetRef {
	return templatesv1alpha1.TemplateRequirementTargetRef{
		Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
		Name:        WildcardAny,
		ProjectName: WildcardAny,
	}
}

// TestAncestorRequirementLister_SkipsProjectNamespaces is the HOL-554
// storage-isolation guardrail for requirements: even if a (forbidden)
// TemplateRequirement CR is seeded in a project namespace, the lister must
// not pick it up.
func TestAncestorRequirementLister_SkipsProjectNamespaces(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	boolTrue := true
	requiresRef := templatesv1alpha1.LinkedTemplateRef{Namespace: ns["org"], Name: "waypoint"}

	reqs := map[string][]templatesv1alpha1.TemplateRequirement{
		// Forbidden: requirement in a project namespace.
		ns["projectLilies"]: {
			requirementCRD(ns["projectLilies"], "pwned", requiresRef,
				[]templatesv1alpha1.TemplateRequirementTargetRef{wildcardRequirementTargetRef()},
				&boolTrue),
		},
		// Legitimate: requirement in the org namespace.
		ns["org"]: {
			requirementCRD(ns["org"], "legit", requiresRef,
				[]templatesv1alpha1.TemplateRequirementTargetRef{wildcardRequirementTargetRef()},
				&boolTrue),
		},
	}

	lister := NewAncestorRequirementLister(
		&requirementListerFromMap{items: reqs},
		walker,
		r,
	)

	got, err := lister.ListRequirements(context.Background(), ns["projectLilies"])
	if err != nil {
		t.Fatalf("ListRequirements: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 requirement (the org-namespace one); got %d: %+v", len(got), got)
	}
	if got[0].Name != "legit" {
		t.Errorf("expected the legit org-namespace requirement; got %q", got[0].Name)
	}
	if got[0].Namespace != ns["org"] {
		t.Errorf("expected namespace %q; got %q", ns["org"], got[0].Namespace)
	}
}

// TestAncestorRequirementLister_PerNamespaceErrorIsLogged: a lister error for
// one namespace should not break traversal. Peer-namespace requirements still
// flow through. Mirrors TestAncestorBindingLister_PerNamespaceErrorIsLogged.
func TestAncestorRequirementLister_PerNamespaceErrorIsLogged(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	boolTrue := true
	requiresRef := templatesv1alpha1.LinkedTemplateRef{Namespace: ns["org"], Name: "waypoint"}

	inner := &requirementListerFromMap{items: map[string][]templatesv1alpha1.TemplateRequirement{
		ns["org"]: {
			requirementCRD(ns["org"], "org-req", requiresRef,
				[]templatesv1alpha1.TemplateRequirementTargetRef{wildcardRequirementTargetRef()},
				&boolTrue),
		},
	}}
	wrapped := &errorRequirementLister{inner: inner, failFor: ns["folderEng"], err: errors.New("boom")}

	lister := NewAncestorRequirementLister(wrapped, walker, r)

	got, err := lister.ListRequirements(context.Background(), ns["projectLilies"])
	if err != nil {
		t.Fatalf("ListRequirements: %v", err)
	}
	if len(got) != 1 || got[0].Name != "org-req" {
		t.Errorf("expected org-req to survive folder-namespace error; got %+v", got)
	}
}

// TestAncestorRequirementLister_Misconfigured returns nil without error on
// any nil dependency — the fail-open contract mirrors AncestorBindingLister.
func TestAncestorRequirementLister_Misconfigured(t *testing.T) {
	l := NewAncestorRequirementLister(nil, nil, nil)
	got, err := l.ListRequirements(context.Background(), "holos-prj-x")
	if err != nil {
		t.Fatalf("expected nil error on misconfigured lister; got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result on misconfigured lister; got %v", got)
	}
}

// TestAncestorRequirementLister_WildcardCoversMultipleProjects verifies that
// a single TemplateRequirement with projectName: "*" appears in the output
// for projects under the same ancestor — the resolver simply returns the
// requirement and lets the controller evaluate matching at reconcile time.
// This is the core property required by the HOL-960 AC: "projectName: '*'
// covers every project under the ancestor."
func TestAncestorRequirementLister_WildcardCoversMultipleProjects(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	boolTrue := true
	requiresRef := templatesv1alpha1.LinkedTemplateRef{Namespace: ns["org"], Name: "cert-manager"}

	orgReq := requirementCRD(ns["org"], "cert-manager-req", requiresRef,
		[]templatesv1alpha1.TemplateRequirementTargetRef{wildcardRequirementTargetRef()},
		&boolTrue)

	lister := NewAncestorRequirementLister(
		&requirementListerFromMap{items: map[string][]templatesv1alpha1.TemplateRequirement{
			ns["org"]: {orgReq},
		}},
		walker,
		r,
	)

	// Both projectLilies (under folderEng) and projectOrchids (direct child
	// of org) should see the wildcard requirement.
	for _, startNs := range []string{ns["projectLilies"], ns["projectOrchids"]} {
		t.Run(startNs, func(t *testing.T) {
			got, err := lister.ListRequirements(context.Background(), startNs)
			if err != nil {
				t.Fatalf("ListRequirements(%s): %v", startNs, err)
			}
			if len(got) != 1 {
				t.Fatalf("expected 1 requirement; got %d: %+v", len(got), got)
			}
			if got[0].Name != "cert-manager-req" {
				t.Errorf("expected cert-manager-req; got %q", got[0].Name)
			}
			if len(got[0].TargetRefs) != 1 {
				t.Fatalf("expected 1 targetRef; got %d", len(got[0].TargetRefs))
			}
			if got[0].TargetRefs[0].ProjectName != WildcardAny {
				t.Errorf("expected wildcard projectName; got %q", got[0].TargetRefs[0].ProjectName)
			}
		})
	}
}

// TestRequirementTargetRefAdapter verifies that requirementTargetRefToResolved
// converts a TemplateRequirementTargetRef slice into the ResolvedBinding shape
// that bindingAppliesTo expects, preserving Kind, Name, and ProjectName.
func TestRequirementTargetRefAdapter(t *testing.T) {
	boolTrue := true
	refs := []templatesv1alpha1.TemplateRequirementTargetRef{
		wildcardRequirementTargetRef(),
		deploymentRequirementTargetRef("alpha", "api-server"),
	}

	rb := RequirementTargetRefToResolved("holos-org-acme", "test-req", refs)
	if rb == nil {
		t.Fatal("expected non-nil ResolvedBinding")
	}
	if len(rb.TargetRefs) != 2 {
		t.Fatalf("expected 2 TargetRefs; got %d", len(rb.TargetRefs))
	}

	// Wildcard ref should match a non-empty deployment target.
	binding := RequirementTargetRefToResolved("holos-org-acme", "wildcard-req", refs[:1])
	if !BindingAppliesToDeployment(binding, "beta", "web") {
		t.Error("wildcard ref should apply to any project/deployment")
	}
	if BindingAppliesToDeployment(binding, "", "web") {
		t.Error("wildcard ref must NOT apply when project is empty (HOL-554 guard)")
	}

	// Exact ref should only match the named project and deployment.
	exact := RequirementTargetRefToResolved("holos-org-acme", "exact-req", refs[1:])
	if !BindingAppliesToDeployment(exact, "alpha", "api-server") {
		t.Error("exact ref should apply to the named target")
	}
	if BindingAppliesToDeployment(exact, "alpha", "other") {
		t.Error("exact ref should NOT apply to a different deployment name")
	}
	// Suppress the boolTrue warning
	_ = boolTrue
}
