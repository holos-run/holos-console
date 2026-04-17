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
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// Test helpers: build a fake namespace hierarchy matching the fixture
// described in HOL-567 (org acme, folder eng, folder team-a under eng,
// projects under each folder and under the org directly).

type storedRuleTest struct {
	Kind     string `json:"kind"`
	Template struct {
		Scope             string `json:"scope"`
		ScopeName         string `json:"scope_name"`
		Name              string `json:"name"`
		VersionConstraint string `json:"version_constraint,omitempty"`
	} `json:"template"`
	Target struct {
		ProjectPattern    string `json:"project_pattern"`
		DeploymentPattern string `json:"deployment_pattern,omitempty"`
	} `json:"target"`
}

// testUnmarshalRules mirrors templatepolicies.UnmarshalRules but avoids the
// cross-package import in tests. The logic must match the production
// unmarshaler exactly: a future drift between the two would silently change
// test assertions, so we vendor the minimal decoder here.
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
		scope := consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED
		switch s.Template.Scope {
		case v1alpha2.TemplateScopeOrganization:
			scope = consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION
		case v1alpha2.TemplateScopeFolder:
			scope = consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER
		case v1alpha2.TemplateScopeProject:
			scope = consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT
		}
		rules = append(rules, &consolev1.TemplatePolicyRule{
			Kind: kind,
			Template: &consolev1.LinkedTemplateRef{
				Scope:             scope,
				ScopeName:         s.Template.ScopeName,
				Name:              s.Template.Name,
				VersionConstraint: s.Template.VersionConstraint,
			},
			Target: &consolev1.TemplatePolicyTarget{
				ProjectPattern:    s.Target.ProjectPattern,
				DeploymentPattern: s.Target.DeploymentPattern,
			},
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

func TestFolderResolver_Resolve(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	orgTmpl := func(name string) *consolev1.LinkedTemplateRef {
		return &consolev1.LinkedTemplateRef{
			Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
			ScopeName: "acme",
			Name:      name,
		}
	}
	folderTmpl := func(folder, name string) *consolev1.LinkedTemplateRef {
		return &consolev1.LinkedTemplateRef{
			Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER,
			ScopeName: folder,
			Name:      name,
		}
	}

	requireRule := func(scope, scopeName, name, projectPattern, deploymentPattern string) storedRuleTest {
		sr := storedRuleTest{Kind: "require"}
		sr.Template.Scope = scope
		sr.Template.ScopeName = scopeName
		sr.Template.Name = name
		sr.Target.ProjectPattern = projectPattern
		sr.Target.DeploymentPattern = deploymentPattern
		return sr
	}
	excludeRule := func(scope, scopeName, name, projectPattern, deploymentPattern string) storedRuleTest {
		sr := storedRuleTest{Kind: "exclude"}
		sr.Template.Scope = scope
		sr.Template.ScopeName = scopeName
		sr.Template.Name = name
		sr.Target.ProjectPattern = projectPattern
		sr.Target.DeploymentPattern = deploymentPattern
		return sr
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
		want       want
	}{
		{
			name:       "no policies — explicit refs pass through",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   []*consolev1.LinkedTemplateRef{orgTmpl("httproute")},
			want:       want{names: []string{"httproute"}},
		},
		{
			name:       "REQUIRE-only — org policy injects template",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "*", ""),
					}, t),
				},
			},
			want: want{names: []string{"audit-policy"}},
		},
		{
			name:       "REQUIRE matches on project_pattern only",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "lilies-only", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "lil*", ""),
					}, t),
				},
			},
			want: want{names: []string{"audit-policy"}},
		},
		{
			name:       "REQUIRE pattern does not match",
			projectNs:  ns["projectRoses"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "lilies-only", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "lilies", ""),
					}, t),
				},
			},
			want: want{names: nil},
		},
		{
			name:       "EXCLUDE-only on a require-injected template removes it",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "*", ""),
					}, t),
				},
				ns["folderEng"]: {
					policyCM(ns["folderEng"], "opt-out", []storedRuleTest{
						excludeRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "lilies", ""),
					}, t),
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
						excludeRule(v1alpha2.TemplateScopeOrganization, "acme", "httproute", "*", ""),
					}, t),
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
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "*", ""),
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "extra", "*", ""),
					}, t),
				},
				ns["folderEng"]: {
					policyCM(ns["folderEng"], "drop-extra", []storedRuleTest{
						excludeRule(v1alpha2.TemplateScopeOrganization, "acme", "extra", "*", ""),
					}, t),
				},
			},
			want: want{names: []string{"httproute", "audit-policy"}},
		},
		{
			name:       "multi-folder hierarchy: folder policy applies to nested project",
			projectNs:  ns["projectRoses"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["folderEng"]: {
					policyCM(ns["folderEng"], "eng-audit", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeFolder, "eng", "eng-audit", "*", ""),
					}, t),
				},
			},
			want: want{names: []string{"eng-audit"}},
		},
		{
			name:       "REQUIRE-equivalent-to-old-mandatory across all projects",
			projectNs:  ns["projectOrchids"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "all-projects", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "reference-grant", "*", ""),
					}, t),
				},
			},
			want: want{names: []string{"reference-grant"}},
		},
		{
			name:       "deployment_pattern narrows REQUIRE to matching deployment",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "api-only", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "api-template", "*", "api"),
					}, t),
				},
			},
			want: want{names: []string{"api-template"}},
		},
		{
			name:       "deployment_pattern not matching skips REQUIRE",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "worker",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "api-only", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "api-template", "*", "api"),
					}, t),
				},
			},
			want: want{names: nil},
		},
		{
			name:       "REQUIRE on ProjectTemplate target kind (no deployment pattern)",
			projectNs:  ns["projectLilies"],
			target:     TargetKindProjectTemplate,
			targetName: "my-template",
			explicit:   []*consolev1.LinkedTemplateRef{folderTmpl("eng", "baseline")},
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "*", ""),
					}, t),
				},
			},
			want: want{names: []string{"baseline", "audit-policy"}},
		},
		{
			name:       "REQUIRE with deployment_pattern never matches ProjectTemplate",
			projectNs:  ns["projectLilies"],
			target:     TargetKindProjectTemplate,
			targetName: "my-template",
			explicit:   nil,
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "api-only", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "api-template", "*", "api"),
					}, t),
				},
			},
			want: want{names: nil},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			lister := &policyListerFromClient{items: tc.policies}
			fr := NewFolderResolver(lister, walker, r, RuleUnmarshalerFunc(testUnmarshalRules))

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
// up. This mirrors the acceptance-criteria bullet in HOL-567.
func TestFolderResolver_IgnoresProjectNamespacePolicies(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	rules := []storedRuleTest{}
	sr := storedRuleTest{Kind: "require"}
	sr.Template.Scope = v1alpha2.TemplateScopeOrganization
	sr.Template.ScopeName = "acme"
	sr.Template.Name = "should-be-ignored"
	sr.Target.ProjectPattern = "*"
	rules = append(rules, sr)

	// Put a (forbidden) policy ConfigMap directly in the project namespace.
	// The resolver must not consume it.
	policies := map[string][]corev1.ConfigMap{
		ns["projectLilies"]: {
			policyCM(ns["projectLilies"], "pwned", rules, t),
		},
	}
	lister := &policyListerFromClient{items: policies}
	fr := NewFolderResolver(lister, walker, r, RuleUnmarshalerFunc(testUnmarshalRules))

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("project-namespace policy leaked: got %v, want empty", refNames(got))
	}
}

// TestFolderResolver_MultiFolderResolvesCorrectOwningFolder ensures that
// projects nested under multiple folders pull policies from every folder in
// the chain, not just the immediate parent.
func TestFolderResolver_MultiFolderResolvesCorrectOwningFolder(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	requireRule := func(scope, scopeName, name, projectPattern string) storedRuleTest {
		sr := storedRuleTest{Kind: "require"}
		sr.Template.Scope = scope
		sr.Template.ScopeName = scopeName
		sr.Template.Name = name
		sr.Target.ProjectPattern = projectPattern
		return sr
	}

	policies := map[string][]corev1.ConfigMap{
		ns["org"]: {
			policyCM(ns["org"], "org-p", []storedRuleTest{
				requireRule(v1alpha2.TemplateScopeOrganization, "acme", "org-tmpl", "*"),
			}, t),
		},
		ns["folderEng"]: {
			policyCM(ns["folderEng"], "eng-p", []storedRuleTest{
				requireRule(v1alpha2.TemplateScopeFolder, "eng", "eng-tmpl", "*"),
			}, t),
		},
		ns["folderTeamA"]: {
			policyCM(ns["folderTeamA"], "team-a-p", []storedRuleTest{
				requireRule(v1alpha2.TemplateScopeFolder, "team-a", "team-a-tmpl", "*"),
			}, t),
		},
	}

	lister := &policyListerFromClient{items: policies}
	fr := NewFolderResolver(lister, walker, r, RuleUnmarshalerFunc(testUnmarshalRules))

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
	orgRef := &consolev1.LinkedTemplateRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
		ScopeName: "acme",
		Name:      "httproute",
	}

	fr := NewFolderResolver(nil, nil, nil, nil)
	got, err := fr.Resolve(context.Background(), "holos-prj-x", TargetKindDeployment, "api", []*consolev1.LinkedTemplateRef{orgRef})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 1 || got[0].GetName() != "httproute" {
		t.Errorf("misconfigured resolver did not fall through: got %v", refNames(got))
	}
}

// TestFolderResolver_PolicyListerErrorIsLogged verifies that a lister error
// for one namespace does not break resolution in other namespaces in the
// chain.
func TestFolderResolver_PolicyListerErrorIsLogged(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	requireRule := func(scope, scopeName, name, projectPattern string) storedRuleTest {
		sr := storedRuleTest{Kind: "require"}
		sr.Template.Scope = scope
		sr.Template.ScopeName = scopeName
		sr.Template.Name = name
		sr.Target.ProjectPattern = projectPattern
		return sr
	}

	inner := &policyListerFromClient{
		items: map[string][]corev1.ConfigMap{
			ns["org"]: {
				policyCM(ns["org"], "p", []storedRuleTest{
					requireRule(v1alpha2.TemplateScopeOrganization, "acme", "org-tmpl", "*"),
				}, t),
			},
		},
	}
	lister := &errorPolicyLister{
		inner:   inner,
		failFor: ns["folderEng"],
		err:     errors.New("boom"),
	}
	fr := NewFolderResolver(lister, walker, r, RuleUnmarshalerFunc(testUnmarshalRules))

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got) != 1 || got[0].GetName() != "org-tmpl" {
		t.Errorf("lister error on folder broke org-level resolution: got %v", refNames(got))
	}
}

// TestFolderResolver_WalkerErrorReturnsExplicitRefs: if the walker fails, the
// resolver must not error; it must return explicit refs so the render can
// still produce a minimal output.
func TestFolderResolver_WalkerErrorReturnsExplicitRefs(t *testing.T) {
	r := baseResolver()
	walker := &failingWalker{err: errors.New("walker exploded")}
	lister := &policyListerFromClient{items: nil}
	fr := NewFolderResolver(lister, walker, r, RuleUnmarshalerFunc(testUnmarshalRules))

	explicit := []*consolev1.LinkedTemplateRef{
		{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "t1"},
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
// REQUIRE rule, the explicit ref survives (version constraint is preserved).
func TestFolderResolver_DedupRespectsExplicit(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	explicit := []*consolev1.LinkedTemplateRef{
		{
			Scope:             consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
			ScopeName:         "acme",
			Name:              "httproute",
			VersionConstraint: ">=1.0.0",
		},
	}
	rules := []storedRuleTest{}
	sr := storedRuleTest{Kind: "require"}
	sr.Template.Scope = v1alpha2.TemplateScopeOrganization
	sr.Template.ScopeName = "acme"
	sr.Template.Name = "httproute"
	sr.Template.VersionConstraint = "<2.0.0"
	sr.Target.ProjectPattern = "*"
	rules = append(rules, sr)

	policies := map[string][]corev1.ConfigMap{
		ns["org"]: {
			policyCM(ns["org"], "p", rules, t),
		},
	}
	lister := &policyListerFromClient{items: policies}
	fr := NewFolderResolver(lister, walker, r, RuleUnmarshalerFunc(testUnmarshalRules))

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
