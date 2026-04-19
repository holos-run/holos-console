package policyresolver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// Test helpers: build a fake namespace hierarchy matching the fixture
// described in HOL-567 (org acme, folder eng, folder team-a under eng,
// projects under each folder and under the org directly).
//
// HOL-600 removed the glob-based TemplatePolicyTarget path; render-time
// selection now runs exclusively through TemplatePolicyBinding. These
// tests therefore drive REQUIRE/EXCLUDE through bindings and also
// assert that a legacy "target" payload lingering in the stored
// annotation is ignored gracefully.

// storedRuleTest mirrors the on-disk JSON wire shape of a
// TemplatePolicyRule — including the legacy "target" field. Keeping the
// field here lets tests seed stale ConfigMaps and assert the resolver
// ignores them (see TestFolderResolver_IgnoresLegacyTargetField).
type storedRuleTest struct {
	Kind     string `json:"kind"`
	Template struct {
		Scope             string `json:"scope"`
		ScopeName         string `json:"scope_name"`
		Name              string `json:"name"`
		VersionConstraint string `json:"version_constraint,omitempty"`
	} `json:"template"`
	Target *legacyTargetForTest `json:"target,omitempty"`
}

// legacyTargetForTest captures the pre-HOL-600 "target" payload that the
// resolver is required to ignore gracefully. Keeping it optional via a
// pointer means tests can seed both shapes (with and without a legacy
// target) without changing the JSON shape of the other fields.
type legacyTargetForTest struct {
	ProjectPattern    string `json:"project_pattern"`
	DeploymentPattern string `json:"deployment_pattern,omitempty"`
}

// testUnmarshalRules mirrors templatepolicies.UnmarshalRules but avoids
// the cross-package import in tests. The legacy `target` field is parsed
// defensively by storedRuleTest above so pre-migration annotations still
// decode cleanly; the decoded value is discarded because the runtime
// proto no longer carries a Target field.
func testUnmarshalRules(raw string) ([]*consolev1.TemplatePolicyRule, error) {
	if raw == "" {
		return nil, nil
	}
	var stored []storedRuleTest
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, err
	}
	rules := make([]*consolev1.TemplatePolicyRule, 0, len(stored))
	for _, s := range stored {
		kind := consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_UNSPECIFIED
		switch s.Kind {
		case "require":
			kind = consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE
		case "exclude":
			kind = consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE
		}
		scope := scopeshim.ScopeUnspecified
		switch s.Template.Scope {
		case v1alpha2.TemplateScopeOrganization:
			scope = scopeshim.ScopeOrganization
		case v1alpha2.TemplateScopeFolder:
			scope = scopeshim.ScopeFolder
		case v1alpha2.TemplateScopeProject:
			scope = scopeshim.ScopeProject
		}
		rules = append(rules, &consolev1.TemplatePolicyRule{
			Kind: kind,
			Template: scopeshim.NewLinkedTemplateRef(
				scope,
				s.Template.ScopeName,
				s.Template.Name,
				s.Template.VersionConstraint,
			),
		})
	}
	return rules, nil
}

// policyListerFromClient adapts a fake kubernetes client to the
// PolicyListerInNamespace interface expected by folderResolver. The
// production implementation lives in console/templatepolicies and is
// exercised by its own tests; this adapter lets the resolver be tested in
// isolation.
type policyListerFromClient struct {
	items map[string][]corev1.ConfigMap
}

func (p *policyListerFromClient) ListPoliciesInNamespace(_ context.Context, ns string) ([]corev1.ConfigMap, error) {
	return p.items[ns], nil
}

// errorPolicyLister returns a hardcoded error for a given namespace and
// forwards all other namespaces to inner.
type errorPolicyLister struct {
	inner   PolicyListerInNamespace
	failFor string
	err     error
}

func (e *errorPolicyLister) ListPoliciesInNamespace(ctx context.Context, ns string) ([]corev1.ConfigMap, error) {
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

// policyCM returns a fake TemplatePolicy ConfigMap with the given rules
// encoded into the annotation.
func policyCM(namespace, name string, rules []storedRuleTest, t *testing.T) corev1.ConfigMap {
	raw, err := json.Marshal(rules)
	if err != nil {
		t.Fatalf("marshal rules: %v", err)
	}
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicy,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationTemplatePolicyRules: string(raw),
			},
		},
	}
}

// requireRuleStored builds a REQUIRE-kind stored rule with no legacy
// target. Bindings select which render targets the rule applies to.
func requireRuleStored(scope, scopeName, name string) storedRuleTest {
	sr := storedRuleTest{Kind: "require"}
	sr.Template.Scope = scope
	sr.Template.ScopeName = scopeName
	sr.Template.Name = name
	return sr
}

// excludeRuleStored is the EXCLUDE counterpart to requireRuleStored.
func excludeRuleStored(scope, scopeName, name string) storedRuleTest {
	sr := storedRuleTest{Kind: "exclude"}
	sr.Template.Scope = scope
	sr.Template.ScopeName = scopeName
	sr.Template.Name = name
	return sr
}

// withLegacyTarget attaches a pre-HOL-600 legacy target payload to a
// stored rule so tests can assert the resolver ignores it.
func withLegacyTarget(rule storedRuleTest, projectPattern, deploymentPattern string) storedRuleTest {
	rule.Target = &legacyTargetForTest{
		ProjectPattern:    projectPattern,
		DeploymentPattern: deploymentPattern,
	}
	return rule
}

// orgPolicyRefStored builds an organization-scoped policy_ref wire value
// for binding fixtures.
func orgPolicyRefStored(policyName string) storedPolicyRefTest {
	return storedPolicyRefTest{
		Scope:     v1alpha2.TemplateScopeOrganization,
		ScopeName: "acme",
		Name:      policyName,
	}
}

// folderPolicyRefStored is the folder-scoped counterpart of
// orgPolicyRefStored.
func folderPolicyRefStored(folder, policyName string) storedPolicyRefTest {
	return storedPolicyRefTest{
		Scope:     v1alpha2.TemplateScopeFolder,
		ScopeName: folder,
		Name:      policyName,
	}
}

// deploymentTargetStored / projectTemplateTargetStored return the
// binding-side target refs the table cases use. The binding handlers'
// wire shape is the same one exercised by the bindings tests.
func deploymentTargetStored(project, name string) storedTargetRefTest {
	return storedTargetRefTest{Kind: "deployment", Name: name, ProjectName: project}
}

func projectTemplateTargetStored(project, name string) storedTargetRefTest {
	return storedTargetRefTest{Kind: "project-template", Name: name, ProjectName: project}
}

func TestFolderResolver_Resolve(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	orgTmpl := func(name string) *consolev1.LinkedTemplateRef {
		return scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, "acme", name, "")
	}
	folderTmpl := func(folder, name string) *consolev1.LinkedTemplateRef {
		return scopeshim.NewLinkedTemplateRef(scopeshim.ScopeFolder, folder, name, "")
	}

	type want struct {
		names []string
	}

	tests := []struct {
		name       string
		projectNs  string
		target     TargetKind
		targetName string
		explicit   []*consolev1.LinkedTemplateRef
		policies   map[string][]corev1.ConfigMap
		bindings   map[string][]corev1.ConfigMap
		want       want
	}{
		{
			name:       "no policies, no bindings — explicit refs pass through",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   []*consolev1.LinkedTemplateRef{orgTmpl("httproute")},
			want:       want{names: []string{"httproute"}},
		},
		{
			name:       "REQUIRE-only — org policy injects template via binding",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "audit-bind",
						orgPolicyRefStored("audit"),
						[]storedTargetRefTest{deploymentTargetStored("lilies", "api")},
						t,
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
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					}, t),
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
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "audit-bind",
						orgPolicyRefStored("audit"),
						[]storedTargetRefTest{deploymentTargetStored("lilies", "api")},
						t,
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
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "req", []storedRuleTest{
						requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					}, t),
					policyCM(ns["org"], "exc", []storedRuleTest{
						excludeRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "req-bind",
						orgPolicyRefStored("req"),
						[]storedTargetRefTest{deploymentTargetStored("lilies", "api")},
						t,
					),
					bindingCM(ns["org"], "exc-bind",
						orgPolicyRefStored("exc"),
						[]storedTargetRefTest{deploymentTargetStored("lilies", "api")},
						t,
					),
				},
			},
			want: want{names: nil},
		},
		{
			name:       "EXCLUDE cannot remove owner-linked template",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   []*consolev1.LinkedTemplateRef{orgTmpl("httproute")},
			policies: map[string][]corev1.ConfigMap{
				ns["folderEng"]: {
					policyCM(ns["folderEng"], "block-httproute", []storedRuleTest{
						excludeRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "httproute"),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["folderEng"]: {
					bindingCM(ns["folderEng"], "block-bind",
						folderPolicyRefStored("eng", "block-httproute"),
						[]storedTargetRefTest{deploymentTargetStored("lilies", "api")},
						t,
					),
				},
			},
			want: want{names: []string{"httproute"}},
		},
		{
			name:       "REQUIRE + EXCLUDE: REQUIRE injects, EXCLUDE removes",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   []*consolev1.LinkedTemplateRef{orgTmpl("httproute")},
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
						requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "extra"),
					}, t),
				},
				ns["folderEng"]: {
					policyCM(ns["folderEng"], "drop-extra", []storedRuleTest{
						excludeRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "extra"),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "audit-bind",
						orgPolicyRefStored("audit"),
						[]storedTargetRefTest{deploymentTargetStored("lilies", "api")},
						t,
					),
				},
				ns["folderEng"]: {
					bindingCM(ns["folderEng"], "drop-extra-bind",
						folderPolicyRefStored("eng", "drop-extra"),
						[]storedTargetRefTest{deploymentTargetStored("lilies", "api")},
						t,
					),
				},
			},
			want: want{names: []string{"httproute", "audit-policy"}},
		},
		{
			name:       "multi-folder hierarchy: folder policy applies to nested project via binding",
			projectNs:  ns["projectRoses"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["folderEng"]: {
					policyCM(ns["folderEng"], "eng-audit", []storedRuleTest{
						requireRuleStored(v1alpha2.TemplateScopeFolder, "eng", "eng-audit"),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["folderEng"]: {
					bindingCM(ns["folderEng"], "eng-audit-bind",
						folderPolicyRefStored("eng", "eng-audit"),
						[]storedTargetRefTest{deploymentTargetStored("roses", "api")},
						t,
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
			explicit:   []*consolev1.LinkedTemplateRef{folderTmpl("eng", "baseline")},
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "audit-bind",
						orgPolicyRefStored("audit"),
						[]storedTargetRefTest{projectTemplateTargetStored("lilies", "my-template")},
						t,
					),
				},
			},
			want: want{names: []string{"baseline", "audit-policy"}},
		},
		{
			// AC (HOL-600): legacy "target" payloads lingering on a
			// stored ConfigMap must be ignored. This seeds a stale
			// rule with project_pattern="*" that the pre-HOL-600
			// resolver would have injected everywhere; without a
			// matching binding post-HOL-600, nothing contributes.
			name:       "legacy rule.target annotation is ignored without a covering binding",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "stale", []storedRuleTest{
						withLegacyTarget(
							requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "stale-tmpl"),
							"*", "",
						),
					}, t),
				},
			},
			bindings: nil,
			want:     want{names: nil},
		},
		{
			// Companion to the previous case: the stale rule.target
			// payload is still ignored when a matching binding
			// exists. The rule contributes because the binding
			// selected it — the legacy glob had no bearing.
			name:       "legacy rule.target is ignored even when a binding selects the rule",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "stale", []storedRuleTest{
						withLegacyTarget(
							requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "stale-tmpl"),
							"nomatch", "nomatch",
						),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "stale-bind",
						orgPolicyRefStored("stale"),
						[]storedTargetRefTest{deploymentTargetStored("lilies", "api")},
						t,
					),
				},
			},
			want: want{names: []string{"stale-tmpl"}},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pl := &policyListerFromClient{items: tc.policies}
			bl := &bindingListerFromMap{items: tc.bindings}
			fr := newFolderResolverWithBindingsForTest(pl, bl, walker, r)

			got, err := fr.Resolve(context.Background(), tc.projectNs, tc.target, tc.targetName, tc.explicit)
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
// ConfigMap is seeded in a project namespace, the resolver must NOT pick it
// up. This mirrors the acceptance-criteria bullet in HOL-567 and
// continues to hold after HOL-600 (bindings do not relax it; a binding
// sitting in a folder namespace that points at a policy sitting in a
// project namespace finds no such policy in the ancestor walk).
func TestFolderResolver_IgnoresProjectNamespacePolicies(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	// Put a (forbidden) policy ConfigMap directly in the project namespace.
	// The resolver must not consume it — even if a binding in a legitimate
	// (folder) namespace points at a policy with the same name.
	policies := map[string][]corev1.ConfigMap{
		ns["projectLilies"]: {
			policyCM(ns["projectLilies"], "pwned", []storedRuleTest{
				requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "should-be-ignored"),
			}, t),
		},
	}
	bindings := map[string][]corev1.ConfigMap{
		ns["org"]: {
			bindingCM(ns["org"], "pwned-bind",
				orgPolicyRefStored("pwned"),
				[]storedTargetRefTest{deploymentTargetStored("lilies", "api")},
				t,
			),
		},
	}
	pl := &policyListerFromClient{items: policies}
	bl := &bindingListerFromMap{items: bindings}
	fr := newFolderResolverWithBindingsForTest(pl, bl, walker, r)

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", nil)
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

	policies := map[string][]corev1.ConfigMap{
		ns["org"]: {
			policyCM(ns["org"], "org-p", []storedRuleTest{
				requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "org-tmpl"),
			}, t),
		},
		ns["folderEng"]: {
			policyCM(ns["folderEng"], "eng-p", []storedRuleTest{
				requireRuleStored(v1alpha2.TemplateScopeFolder, "eng", "eng-tmpl"),
			}, t),
		},
		ns["folderTeamA"]: {
			policyCM(ns["folderTeamA"], "team-a-p", []storedRuleTest{
				requireRuleStored(v1alpha2.TemplateScopeFolder, "team-a", "team-a-tmpl"),
			}, t),
		},
	}
	bindings := map[string][]corev1.ConfigMap{
		ns["org"]: {
			bindingCM(ns["org"], "org-bind",
				orgPolicyRefStored("org-p"),
				[]storedTargetRefTest{deploymentTargetStored("roses", "api")},
				t,
			),
		},
		ns["folderEng"]: {
			bindingCM(ns["folderEng"], "eng-bind",
				folderPolicyRefStored("eng", "eng-p"),
				[]storedTargetRefTest{deploymentTargetStored("roses", "api")},
				t,
			),
		},
		ns["folderTeamA"]: {
			bindingCM(ns["folderTeamA"], "team-a-bind",
				folderPolicyRefStored("team-a", "team-a-p"),
				[]storedTargetRefTest{deploymentTargetStored("roses", "api")},
				t,
			),
		},
	}

	pl := &policyListerFromClient{items: policies}
	bl := &bindingListerFromMap{items: bindings}
	fr := newFolderResolverWithBindingsForTest(pl, bl, walker, r)

	// projectRoses is under team-a which is under eng which is under org.
	got, err := fr.Resolve(context.Background(), ns["projectRoses"], TargetKindDeployment, "api", nil)
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
// bootstrap must fail open (render proceeds with explicit refs only), not
// closed (render errors on every call).
func TestFolderResolver_MisconfiguredFallsOpen(t *testing.T) {
	orgRef := scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, "acme", "httproute", "")

	fr := NewFolderResolver(nil, nil, nil, nil)
	got, err := fr.Resolve(context.Background(), "holos-prj-x", TargetKindDeployment, "api", []*consolev1.LinkedTemplateRef{orgRef})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 1 || got[0].GetName() != "httproute" {
		t.Errorf("misconfigured resolver did not fall through: got %v", refNames(got))
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

	policies := map[string][]corev1.ConfigMap{
		ns["org"]: {
			policyCM(ns["org"], "audit", []storedRuleTest{
				// Seed a legacy target that would have matched
				// every project pre-HOL-600.
				withLegacyTarget(
					requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
					"*", "",
				),
			}, t),
		},
	}
	pl := &policyListerFromClient{items: policies}
	// NewFolderResolver (no bindings) — post-HOL-600 this means "no
	// rules contribute".
	fr := NewFolderResolver(pl, walker, r, RuleUnmarshalerFunc(testUnmarshalRules))

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", nil)
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
		items: map[string][]corev1.ConfigMap{
			ns["org"]: {
				policyCM(ns["org"], "p", []storedRuleTest{
					requireRuleStored(v1alpha2.TemplateScopeOrganization, "acme", "org-tmpl"),
				}, t),
			},
		},
	}
	lister := &errorPolicyLister{
		inner:   inner,
		failFor: ns["folderEng"],
		err:     errors.New("boom"),
	}
	bindings := map[string][]corev1.ConfigMap{
		ns["org"]: {
			bindingCM(ns["org"], "p-bind",
				orgPolicyRefStored("p"),
				[]storedTargetRefTest{deploymentTargetStored("lilies", "api")},
				t,
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	fr := newFolderResolverWithBindingsForTest(lister, bl, walker, r)

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 1 || got[0].GetName() != "org-tmpl" {
		t.Errorf("lister error on folder broke org-level resolution: got %v", refNames(got))
	}
}

// TestFolderResolver_WalkerErrorReturnsExplicitRefs: if the walker fails,
// the resolver must not error; it must return explicit refs so the
// render can still produce a minimal output.
func TestFolderResolver_WalkerErrorReturnsExplicitRefs(t *testing.T) {
	r := baseResolver()
	walker := &failingWalker{err: errors.New("walker exploded")}
	lister := &policyListerFromClient{items: nil}
	fr := NewFolderResolver(lister, walker, r, RuleUnmarshalerFunc(testUnmarshalRules))

	explicit := []*consolev1.LinkedTemplateRef{
		scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, "acme", "t1", ""),
	}
	got, err := fr.Resolve(context.Background(), "holos-prj-x", TargetKindDeployment, "api", explicit)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 1 || got[0].GetName() != "t1" {
		t.Errorf("expected explicit refs pass-through on walker failure: got %v", refNames(got))
	}
}

type failingWalker struct {
	err error
}

func (f *failingWalker) WalkAncestors(_ context.Context, _ string) ([]*corev1.Namespace, error) {
	return nil, f.err
}

// TestFolderResolver_DedupRespectsExplicit: when an explicit ref matches a
// REQUIRE rule (selected via binding), the explicit ref survives with
// its version constraint intact. This guards the first-seen-wins dedup
// contract.
func TestFolderResolver_DedupRespectsExplicit(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	explicit := []*consolev1.LinkedTemplateRef{
		scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, "acme", "httproute", ">=1.0.0"),
	}
	policies := map[string][]corev1.ConfigMap{
		ns["org"]: {
			policyCM(ns["org"], "p", []storedRuleTest{
				{
					Kind: "require",
					Template: struct {
						Scope             string `json:"scope"`
						ScopeName         string `json:"scope_name"`
						Name              string `json:"name"`
						VersionConstraint string `json:"version_constraint,omitempty"`
					}{
						Scope:             v1alpha2.TemplateScopeOrganization,
						ScopeName:         "acme",
						Name:              "httproute",
						VersionConstraint: "<2.0.0",
					},
				},
			}, t),
		},
	}
	bindings := map[string][]corev1.ConfigMap{
		ns["org"]: {
			bindingCM(ns["org"], "p-bind",
				orgPolicyRefStored("p"),
				[]storedTargetRefTest{deploymentTargetStored("lilies", "api")},
				t,
			),
		},
	}
	pl := &policyListerFromClient{items: policies}
	bl := &bindingListerFromMap{items: bindings}
	fr := newFolderResolverWithBindingsForTest(pl, bl, walker, r)

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", explicit)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 ref, got %d: %s", len(got), fmt.Sprint(refNames(got)))
	}
	if got[0].GetVersionConstraint() != ">=1.0.0" {
		t.Errorf("explicit ref's version constraint was overridden: got %q, want %q",
			got[0].GetVersionConstraint(), ">=1.0.0")
	}
}
