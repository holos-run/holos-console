package policyresolver

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// Test helpers: build a fake namespace hierarchy matching the fixture
// described in HOL-567 (org acme, folder eng, folder team-a under eng,
// projects under each folder and under the org directly).
//
// HOL-600 removed the glob-based TemplatePolicyTarget path; render-time
// selection now runs exclusively through TemplatePolicyBinding. These
// tests therefore drive REQUIRE/EXCLUDE through bindings.
//
// HOL-662 migrated the storage shape from ConfigMap + JSON annotations to
// the templates.holos.run/v1alpha1 TemplatePolicy and TemplatePolicyBinding
// CRDs. The helpers below build typed CRD values directly; there is no
// longer a JSON wire shape for tests to vendor.

// policyListerFromClient adapts an in-memory map to the
// PolicyListerInNamespace interface. The production implementation lives in
// console/templatepolicies and is exercised by its own tests; this adapter
// lets the resolver be tested in isolation.
//
// HOL-622 switched the interface return shape to a pointer slice so the
// resolver can forward the cached CRD pointer through without re-addressing
// a copy. The map still holds value slices for readable test fixtures; this
// adapter rewraps them on the way out.
type policyListerFromClient struct {
	items map[string][]templatesv1alpha1.TemplatePolicy
}

func (p *policyListerFromClient) ListPoliciesInNamespace(_ context.Context, ns string) ([]*templatesv1alpha1.TemplatePolicy, error) {
	src := p.items[ns]
	if len(src) == 0 {
		return nil, nil
	}
	out := make([]*templatesv1alpha1.TemplatePolicy, 0, len(src))
	for i := range src {
		out = append(out, &src[i])
	}
	return out, nil
}

// errorPolicyLister returns a hardcoded error for a given namespace and
// forwards all other namespaces to inner.
type errorPolicyLister struct {
	inner   PolicyListerInNamespace
	failFor string
	err     error
}

func (e *errorPolicyLister) ListPoliciesInNamespace(ctx context.Context, ns string) ([]*templatesv1alpha1.TemplatePolicy, error) {
	if ns == e.failFor {
		return nil, e.err
	}
	return e.inner.ListPoliciesInNamespace(ctx, ns)
}

func baseResolver() *resolver.Resolver {
	return &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
}

func mkNs(name, kind, parent string) *corev1.Namespace {
	labels := map[string]string{
		v1alpha2.LabelResourceType: kind,
	}
	if parent != "" {
		labels[v1alpha2.AnnotationParent] = parent
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

// buildFixture returns a fake kubernetes client populated with the canonical
// HOL-567 hierarchy fixture: acme/ (org), acme/eng/ (folder), acme/eng/team-a/
// (folder), projects under each, plus a project directly under the org.
func buildFixture() (*fake.Clientset, *resolver.Resolver, map[string]string) {
	r := baseResolver()
	orgNs := r.OrgNamespace("acme")                 // holos-org-acme
	folderEngNs := r.FolderNamespace("eng")         // holos-fld-eng
	folderTeamANs := r.FolderNamespace("team-a")    // holos-fld-team-a
	projectOrchids := r.ProjectNamespace("orchids") // holos-prj-orchids (under org directly)
	projectLilies := r.ProjectNamespace("lilies")   // holos-prj-lilies (under eng)
	projectRoses := r.ProjectNamespace("roses")     // holos-prj-roses (under team-a)

	objects := []runtime.Object{
		mkNs(orgNs, v1alpha2.ResourceTypeOrganization, ""),
		mkNs(folderEngNs, v1alpha2.ResourceTypeFolder, orgNs),
		mkNs(folderTeamANs, v1alpha2.ResourceTypeFolder, folderEngNs),
		mkNs(projectOrchids, v1alpha2.ResourceTypeProject, orgNs),
		mkNs(projectLilies, v1alpha2.ResourceTypeProject, folderEngNs),
		mkNs(projectRoses, v1alpha2.ResourceTypeProject, folderTeamANs),
	}
	client := fake.NewClientset(objects...)

	namespaces := map[string]string{
		"org":            orgNs,
		"folderEng":      folderEngNs,
		"folderTeamA":    folderTeamANs,
		"projectOrchids": projectOrchids,
		"projectLilies":  projectLilies,
		"projectRoses":   projectRoses,
	}
	return client, r, namespaces
}

// buildCtrlFixture is the HOL-622 counterpart to buildFixture. It returns a
// controller-runtime fake client seeded with the same namespace hierarchy plus
// a corev1 scheme wired for ConfigMap reads. Tests that exercise the
// AppliedRenderStateClient (which migrated from client-go to ctrlclient) call
// this helper instead of buildFixture so the applied-render-set path is
// covered end-to-end by a controller-runtime client surface.
func buildCtrlFixture() (ctrlclient.Client, *resolver.Resolver, map[string]string) {
	r := baseResolver()
	orgNs := r.OrgNamespace("acme")
	folderEngNs := r.FolderNamespace("eng")
	folderTeamANs := r.FolderNamespace("team-a")
	projectOrchids := r.ProjectNamespace("orchids")
	projectLilies := r.ProjectNamespace("lilies")
	projectRoses := r.ProjectNamespace("roses")

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	objects := []ctrlclient.Object{
		mkNs(orgNs, v1alpha2.ResourceTypeOrganization, ""),
		mkNs(folderEngNs, v1alpha2.ResourceTypeFolder, orgNs),
		mkNs(folderTeamANs, v1alpha2.ResourceTypeFolder, folderEngNs),
		mkNs(projectOrchids, v1alpha2.ResourceTypeProject, orgNs),
		mkNs(projectLilies, v1alpha2.ResourceTypeProject, folderEngNs),
		mkNs(projectRoses, v1alpha2.ResourceTypeProject, folderTeamANs),
	}
	client := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

	namespaces := map[string]string{
		"org":            orgNs,
		"folderEng":      folderEngNs,
		"folderTeamA":    folderTeamANs,
		"projectOrchids": projectOrchids,
		"projectLilies":  projectLilies,
		"projectRoses":   projectRoses,
	}
	return client, r, namespaces
}

// objectMetaCRD returns the managed-by / resource-type labels the bindings
// and policies carry in production. Shared by policyCRD and bindingCRD so
// the label shape drifts in one place.
func objectMetaCRD(name, namespace, resourceType string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		Labels: map[string]string{
			v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
			v1alpha2.LabelResourceType: resourceType,
		},
	}
}

// policyCRD returns a TemplatePolicy CR with the given rules encoded into
// spec.rules directly. Post-HOL-662 there is no JSON annotation wire shape.
func policyCRD(namespace, name string, rules []templatesv1alpha1.TemplatePolicyRule) templatesv1alpha1.TemplatePolicy {
	return templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
			},
		},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			Rules: rules,
		},
	}
}

// scopeToNamespace maps the (scope-label, scopeName) pair used by pre-
// HOL-723 tests into the flat namespace identifier the CRD carries now.
func scopeToNamespace(scope, scopeName string) string {
	switch scope {
	case v1alpha2.TemplateScopeOrganization:
		return "holos-org-" + scopeName
	case v1alpha2.TemplateScopeFolder:
		return "holos-fld-" + scopeName
	case v1alpha2.TemplateScopeProject:
		return "holos-prj-" + scopeName
	}
	return ""
}

// requireRuleCRD builds a REQUIRE-kind CRD rule. Bindings select which
// render targets the rule applies to.
func requireRuleCRD(scope, scopeName, name string) templatesv1alpha1.TemplatePolicyRule {
	return templatesv1alpha1.TemplatePolicyRule{
		Kind: templatesv1alpha1.TemplatePolicyKindRequire,
		Template: templatesv1alpha1.LinkedTemplateRef{
			Namespace: scopeToNamespace(scope, scopeName),
			Name:      name,
		},
	}
}

// excludeRuleCRD is the EXCLUDE counterpart to requireRuleCRD.
func excludeRuleCRD(scope, scopeName, name string) templatesv1alpha1.TemplatePolicyRule {
	return templatesv1alpha1.TemplatePolicyRule{
		Kind: templatesv1alpha1.TemplatePolicyKindExclude,
		Template: templatesv1alpha1.LinkedTemplateRef{
			Namespace: scopeToNamespace(scope, scopeName),
			Name:      name,
		},
	}
}

func TestFolderResolver_Resolve(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	type want struct {
		names []string
	}

	tests := []struct {
		name       string
		projectNs  string
		target     TargetKind
		targetName string
		policies   map[string][]templatesv1alpha1.TemplatePolicy
		bindings   map[string][]templatesv1alpha1.TemplatePolicyBinding
		want       want
	}{
		{
			name:       "no policies, no bindings — empty effective set",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			want:       want{names: nil},
		},
		{
			name:       "REQUIRE-only — org policy injects template via binding",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]templatesv1alpha1.TemplatePolicy{
				ns["org"]: {
					policyCRD(ns["org"], "audit", []templatesv1alpha1.TemplatePolicyRule{
						requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					}),
				},
			},
			bindings: map[string][]templatesv1alpha1.TemplatePolicyBinding{
				ns["org"]: {
					bindingCRD(ns["org"], "audit-bind",
						orgPolicyRefCRD("audit"),
						[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
					),
				},
			},
			want: want{names: []string{"audit-policy"}},
		},
		{
			name:       "policy with no binding contributes nothing",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]templatesv1alpha1.TemplatePolicy{
				ns["org"]: {
					policyCRD(ns["org"], "audit", []templatesv1alpha1.TemplatePolicyRule{
						requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					}),
				},
			},
			bindings: nil,
			want:     want{names: nil},
		},
		{
			name:       "binding targets peer, not current render target — no refs",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "worker",
			policies: map[string][]templatesv1alpha1.TemplatePolicy{
				ns["org"]: {
					policyCRD(ns["org"], "audit", []templatesv1alpha1.TemplatePolicyRule{
						requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					}),
				},
			},
			bindings: map[string][]templatesv1alpha1.TemplatePolicyBinding{
				ns["org"]: {
					bindingCRD(ns["org"], "audit-bind",
						orgPolicyRefCRD("audit"),
						[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
					),
				},
			},
			want: want{names: nil},
		},
		{
			name:       "EXCLUDE via binding removes a REQUIRE-injected template",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]templatesv1alpha1.TemplatePolicy{
				ns["org"]: {
					policyCRD(ns["org"], "req", []templatesv1alpha1.TemplatePolicyRule{
						requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					}),
					policyCRD(ns["org"], "exc", []templatesv1alpha1.TemplatePolicyRule{
						excludeRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					}),
				},
			},
			bindings: map[string][]templatesv1alpha1.TemplatePolicyBinding{
				ns["org"]: {
					bindingCRD(ns["org"], "req-bind",
						orgPolicyRefCRD("req"),
						[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
					),
					bindingCRD(ns["org"], "exc-bind",
						orgPolicyRefCRD("exc"),
						[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
					),
				},
			},
			want: want{names: nil},
		},
		{
			name:       "EXCLUDE on a REQUIRE-injected template removes it",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]templatesv1alpha1.TemplatePolicy{
				ns["org"]: {
					policyCRD(ns["org"], "req-httproute", []templatesv1alpha1.TemplatePolicyRule{
						requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "httproute"),
					}),
				},
				ns["folderEng"]: {
					policyCRD(ns["folderEng"], "block-httproute", []templatesv1alpha1.TemplatePolicyRule{
						excludeRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "httproute"),
					}),
				},
			},
			bindings: map[string][]templatesv1alpha1.TemplatePolicyBinding{
				ns["org"]: {
					bindingCRD(ns["org"], "req-bind",
						orgPolicyRefCRD("req-httproute"),
						[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
					),
				},
				ns["folderEng"]: {
					bindingCRD(ns["folderEng"], "block-bind",
						folderPolicyRefCRD("eng", "block-httproute"),
						[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
					),
				},
			},
			want: want{names: nil},
		},
		{
			name:       "REQUIRE + EXCLUDE: REQUIRE injects, EXCLUDE removes one of two",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]templatesv1alpha1.TemplatePolicy{
				ns["org"]: {
					policyCRD(ns["org"], "audit", []templatesv1alpha1.TemplatePolicyRule{
						requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
						requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "extra"),
					}),
				},
				ns["folderEng"]: {
					policyCRD(ns["folderEng"], "drop-extra", []templatesv1alpha1.TemplatePolicyRule{
						excludeRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "extra"),
					}),
				},
			},
			bindings: map[string][]templatesv1alpha1.TemplatePolicyBinding{
				ns["org"]: {
					bindingCRD(ns["org"], "audit-bind",
						orgPolicyRefCRD("audit"),
						[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
					),
				},
				ns["folderEng"]: {
					bindingCRD(ns["folderEng"], "drop-extra-bind",
						folderPolicyRefCRD("eng", "drop-extra"),
						[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
					),
				},
			},
			want: want{names: []string{"audit-policy"}},
		},
		{
			name:       "multi-folder hierarchy: folder policy applies to nested project via binding",
			projectNs:  ns["projectRoses"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]templatesv1alpha1.TemplatePolicy{
				ns["folderEng"]: {
					policyCRD(ns["folderEng"], "eng-audit", []templatesv1alpha1.TemplatePolicyRule{
						requireRuleCRD(v1alpha2.TemplateScopeFolder, "eng", "eng-audit"),
					}),
				},
			},
			bindings: map[string][]templatesv1alpha1.TemplatePolicyBinding{
				ns["folderEng"]: {
					bindingCRD(ns["folderEng"], "eng-audit-bind",
						folderPolicyRefCRD("eng", "eng-audit"),
						[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("roses", "api")},
					),
				},
			},
			want: want{names: []string{"eng-audit"}},
		},
		{
			name:       "REQUIRE on ProjectTemplate target kind via binding",
			projectNs:  ns["projectLilies"],
			target:     TargetKindProjectTemplate,
			targetName: "my-template",
			policies: map[string][]templatesv1alpha1.TemplatePolicy{
				ns["org"]: {
					policyCRD(ns["org"], "audit", []templatesv1alpha1.TemplatePolicyRule{
						requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					}),
				},
			},
			bindings: map[string][]templatesv1alpha1.TemplatePolicyBinding{
				ns["org"]: {
					bindingCRD(ns["org"], "audit-bind",
						orgPolicyRefCRD("audit"),
						[]templatesv1alpha1.TemplatePolicyBindingTargetRef{projectTemplateTargetCRD("lilies", "my-template")},
					),
				},
			},
			want: want{names: []string{"audit-policy"}},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pl := &policyListerFromClient{items: tc.policies}
			bl := &bindingListerFromMap{items: tc.bindings}
			fr := newFolderResolverWithBindingsForTest(pl, bl, walker, r)

			got, err := fr.Resolve(context.Background(), tc.projectNs, tc.target, tc.targetName)
			if err != nil {
				t.Fatalf("Resolve returned error: %v", err)
			}
			gotNames := refNames(got)
			sort.Strings(gotNames)
			wantNames := append([]string(nil), tc.want.names...)
			sort.Strings(wantNames)
			if !equalStringSlices(gotNames, wantNames) {
				t.Errorf("names mismatch: got %v, want %v", gotNames, wantNames)
			}
		})
	}
}

func refNames(refs []*consolev1.LinkedTemplateRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r == nil {
			continue
		}
		out = append(out, r.GetName())
	}
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestFolderResolver_IgnoresProjectNamespacePolicies is the HOL-554
// storage-isolation guardrail: even when a synthetic (forbidden) policy
// CR is seeded in a project namespace, the resolver must NOT pick it
// up. This mirrors the acceptance-criteria bullet in HOL-567 and
// continues to hold after HOL-600 (bindings do not relax it; a binding
// sitting in a folder namespace that points at a policy sitting in a
// project namespace finds no such policy in the ancestor walk).
//
// Post-HOL-662 the CEL ValidatingAdmissionPolicy rejects such a CR at
// admission time; the resolver's own skip logic remains as defense in
// depth.
func TestFolderResolver_IgnoresProjectNamespacePolicies(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	// Put a (forbidden) policy CR directly in the project namespace.
	// The resolver must not consume it — even if a binding in a
	// legitimate (folder) namespace points at a policy with the same
	// name.
	policies := map[string][]templatesv1alpha1.TemplatePolicy{
		ns["projectLilies"]: {
			policyCRD(ns["projectLilies"], "pwned", []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "should-be-ignored"),
			}),
		},
	}
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "pwned-bind",
				orgPolicyRefCRD("pwned"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
			),
		},
	}
	pl := &policyListerFromClient{items: policies}
	bl := &bindingListerFromMap{items: bindings}
	fr := newFolderResolverWithBindingsForTest(pl, bl, walker, r)

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("project-namespace policy leaked: got %v, want empty", refNames(got))
	}
}

// TestFolderResolver_MultiFolderResolvesCorrectOwningFolder ensures that
// projects nested under multiple folders pull policies from every folder
// in the chain, not just the immediate parent. Bindings in each folder
// select the nested project's deployment render target.
func TestFolderResolver_MultiFolderResolvesCorrectOwningFolder(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	policies := map[string][]templatesv1alpha1.TemplatePolicy{
		ns["org"]: {
			policyCRD(ns["org"], "org-p", []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "org-tmpl"),
			}),
		},
		ns["folderEng"]: {
			policyCRD(ns["folderEng"], "eng-p", []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeFolder, "eng", "eng-tmpl"),
			}),
		},
		ns["folderTeamA"]: {
			policyCRD(ns["folderTeamA"], "team-a-p", []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeFolder, "team-a", "team-a-tmpl"),
			}),
		},
	}
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "org-bind",
				orgPolicyRefCRD("org-p"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("roses", "api")},
			),
		},
		ns["folderEng"]: {
			bindingCRD(ns["folderEng"], "eng-bind",
				folderPolicyRefCRD("eng", "eng-p"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("roses", "api")},
			),
		},
		ns["folderTeamA"]: {
			bindingCRD(ns["folderTeamA"], "team-a-bind",
				folderPolicyRefCRD("team-a", "team-a-p"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("roses", "api")},
			),
		},
	}

	pl := &policyListerFromClient{items: policies}
	bl := &bindingListerFromMap{items: bindings}
	fr := newFolderResolverWithBindingsForTest(pl, bl, walker, r)

	// projectRoses is under team-a which is under eng which is under org.
	got, err := fr.Resolve(context.Background(), ns["projectRoses"], TargetKindDeployment, "api")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	names := refNames(got)
	sort.Strings(names)
	want := []string{"eng-tmpl", "org-tmpl", "team-a-tmpl"}
	if !equalStringSlices(names, want) {
		t.Errorf("multi-folder chain: got %v, want %v", names, want)
	}
}

// TestFolderResolver_MisconfiguredFallsOpen ensures a resolver constructed
// with nil dependencies behaves as the noop resolver would. A misconfigured
// bootstrap must fail open (render proceeds with an empty effective set), not
// closed (render errors on every call).
func TestFolderResolver_MisconfiguredFallsOpen(t *testing.T) {
	fr := NewFolderResolver(nil, nil, nil)
	got, err := fr.Resolve(context.Background(), "holos-prj-x", TargetKindDeployment, "api")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("misconfigured resolver did not fall open to empty set: got %v", refNames(got))
	}
}

// TestFolderResolver_NoBindingWireupIsFailOpen is the direct AC for the
// post-HOL-600 resolver: constructing a resolver via NewFolderResolver
// (without binding deps) means no rule can contribute. A policy sitting
// in the ancestor chain is simply ignored and the caller's explicit
// refs pass through unchanged. This locks in the fail-open contract so
// a future refactor cannot accidentally revive the legacy glob path
// without touching this test.
func TestFolderResolver_NoBindingWireupIsFailOpen(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	policies := map[string][]templatesv1alpha1.TemplatePolicy{
		ns["org"]: {
			policyCRD(ns["org"], "audit", []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
			}),
		},
	}
	pl := &policyListerFromClient{items: policies}
	// NewFolderResolver (no bindings) — post-HOL-600 this means "no
	// rules contribute".
	fr := NewFolderResolver(pl, walker, r)

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no refs without binding wire-up, got %v", refNames(got))
	}
}

// TestFolderResolver_PolicyListerErrorIsLogged verifies that a lister
// error for one namespace does not break resolution in other namespaces
// in the chain. The rule that survives still needs a covering binding to
// contribute, matching the post-HOL-600 selection contract.
func TestFolderResolver_PolicyListerErrorIsLogged(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	inner := &policyListerFromClient{
		items: map[string][]templatesv1alpha1.TemplatePolicy{
			ns["org"]: {
				policyCRD(ns["org"], "p", []templatesv1alpha1.TemplatePolicyRule{
					requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "org-tmpl"),
				}),
			},
		},
	}
	lister := &errorPolicyLister{
		inner:   inner,
		failFor: ns["folderEng"],
		err:     errors.New("boom"),
	}
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "p-bind",
				orgPolicyRefCRD("p"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	fr := newFolderResolverWithBindingsForTest(lister, bl, walker, r)

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 1 || got[0].GetName() != "org-tmpl" {
		t.Errorf("lister error on folder broke org-level resolution: got %v", refNames(got))
	}
}

// TestFolderResolver_WalkerErrorFallsOpen: if the walker fails,
// the resolver must not error; it must return an empty effective set so
// the render can still proceed with no policy-injected templates.
func TestFolderResolver_WalkerErrorFallsOpen(t *testing.T) {
	r := baseResolver()
	walker := &failingWalker{err: errors.New("walker exploded")}
	lister := &policyListerFromClient{items: nil}
	fr := NewFolderResolver(lister, walker, r)

	got, err := fr.Resolve(context.Background(), "holos-prj-x", TargetKindDeployment, "api")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty set on walker failure: got %v", refNames(got))
	}
}

type failingWalker struct {
	err error
}

func (f *failingWalker) WalkAncestors(_ context.Context, _ string) ([]*corev1.Namespace, error) {
	return nil, f.err
}

// TestFolderResolver_DedupRespectsFirstRequireWin: when two REQUIRE rules from
// different policies name the same template, the first-seen occurrence wins and
// the duplicate is dropped. This guards the dedup contract.
func TestFolderResolver_DedupRespectsFirstRequireWin(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	// Two REQUIRE rules that inject the same template — the first one listed
	// in the ancestor walk wins (org policy is visited before folder policy).
	policies := map[string][]templatesv1alpha1.TemplatePolicy{
		ns["org"]: {
			policyCRD(ns["org"], "p", []templatesv1alpha1.TemplatePolicyRule{
				{
					Kind: templatesv1alpha1.TemplatePolicyKindRequire,
					Template: templatesv1alpha1.LinkedTemplateRef{
						Namespace:         "holos-org-acme",
						Name:              "httproute",
						VersionConstraint: ">=1.0.0",
					},
				},
			}),
		},
		ns["folderEng"]: {
			policyCRD(ns["folderEng"], "p2", []templatesv1alpha1.TemplatePolicyRule{
				{
					Kind: templatesv1alpha1.TemplatePolicyKindRequire,
					Template: templatesv1alpha1.LinkedTemplateRef{
						Namespace:         "holos-org-acme",
						Name:              "httproute",
						VersionConstraint: "<2.0.0",
					},
				},
			}),
		},
	}
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "p-bind",
				orgPolicyRefCRD("p"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
			),
		},
		ns["folderEng"]: {
			bindingCRD(ns["folderEng"], "p2-bind",
				folderPolicyRefCRD("eng", "p2"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
			),
		},
	}
	pl := &policyListerFromClient{items: policies}
	bl := &bindingListerFromMap{items: bindings}
	fr := newFolderResolverWithBindingsForTest(pl, bl, walker, r)

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 ref (deduped), got %d: %s", len(got), fmt.Sprint(refNames(got)))
	}
	// The first-seen constraint wins — which one is first depends on ancestor
	// walk order; we only assert dedup produces exactly one entry.
	if got[0].GetName() != "httproute" {
		t.Errorf("wrong template name: got %q, want %q", got[0].GetName(), "httproute")
	}
}
