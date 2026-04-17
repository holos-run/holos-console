package templates

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// storedRuleFixture mirrors the JSON shape that templatepolicies serializes
// into the AnnotationTemplatePolicyRules annotation. Vendored into the tests
// to avoid a cross-package import from templates → templatepolicies just for
// the decoder (the decoder is also exercised in its owning package).
type storedRuleFixture struct {
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

func fixtureUnmarshalRules(raw string) ([]*consolev1.TemplatePolicyRule, error) {
	if raw == "" {
		return nil, nil
	}
	var stored []storedRuleFixture
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

// policyListerMap implements policyresolver.PolicyListerInNamespace. Tests
// drive it by pre-seeding the ConfigMap list for each folder/org namespace.
type policyListerMap struct {
	items map[string][]corev1.ConfigMap
}

func (p *policyListerMap) ListPoliciesInNamespace(_ context.Context, ns string) ([]corev1.ConfigMap, error) {
	return p.items[ns], nil
}

func mkNamespace(name, kind, parent string) *corev1.Namespace {
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

// policyCM returns a fake TemplatePolicy ConfigMap with the given rules
// encoded into the annotation.
func policyCM(namespace, name string, rules []storedRuleFixture, t *testing.T) corev1.ConfigMap {
	t.Helper()
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

type policyFixture struct {
	resolver   *resolver.Resolver
	walker     *resolver.Walker
	namespaces map[string]string
}

// buildPolicyFixture mirrors folder_resolver_test's canonical hierarchy:
// acme/ (org), acme/eng/ (folder), acme/eng/team-a/ (folder), plus projects
// under each level. Project slugs cover the "under org directly",
// "under folder eng", and "under folder team-a (nested under eng)" cases.
func buildPolicyFixture(t *testing.T) *policyFixture {
	t.Helper()
	r := &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
	orgNs := r.OrgNamespace("acme")              // holos-org-acme
	folderEngNs := r.FolderNamespace("eng")      // holos-fld-eng
	folderTeamANs := r.FolderNamespace("team-a") // holos-fld-team-a
	projectOrchids := r.ProjectNamespace("orchids")
	projectLilies := r.ProjectNamespace("lilies")
	projectRoses := r.ProjectNamespace("roses")
	projectFooAlpha := r.ProjectNamespace("foo-alpha")
	projectBarAlpha := r.ProjectNamespace("bar-alpha")

	objects := []runtime.Object{
		mkNamespace(orgNs, v1alpha2.ResourceTypeOrganization, ""),
		mkNamespace(folderEngNs, v1alpha2.ResourceTypeFolder, orgNs),
		mkNamespace(folderTeamANs, v1alpha2.ResourceTypeFolder, folderEngNs),
		mkNamespace(projectOrchids, v1alpha2.ResourceTypeProject, orgNs),
		mkNamespace(projectLilies, v1alpha2.ResourceTypeProject, folderEngNs),
		mkNamespace(projectRoses, v1alpha2.ResourceTypeProject, folderTeamANs),
		mkNamespace(projectFooAlpha, v1alpha2.ResourceTypeProject, folderEngNs),
		mkNamespace(projectBarAlpha, v1alpha2.ResourceTypeProject, folderEngNs),
	}

	client := fake.NewClientset(objects...)
	walker := &resolver.Walker{Client: client, Resolver: r}

	return &policyFixture{
		resolver: r,
		walker:   walker,
		namespaces: map[string]string{
			"org":             orgNs,
			"folderEng":       folderEngNs,
			"folderTeamA":     folderTeamANs,
			"projectOrchids":  projectOrchids,
			"projectLilies":   projectLilies,
			"projectRoses":    projectRoses,
			"projectFooAlpha": projectFooAlpha,
			"projectBarAlpha": projectBarAlpha,
		},
	}
}

func requireRule(scope, scopeName, templateName, projectPattern, deploymentPattern string) storedRuleFixture {
	sr := storedRuleFixture{Kind: "require"}
	sr.Template.Scope = scope
	sr.Template.ScopeName = scopeName
	sr.Template.Name = templateName
	sr.Target.ProjectPattern = projectPattern
	sr.Target.DeploymentPattern = deploymentPattern
	return sr
}

func excludeRule(scope, scopeName, templateName, projectPattern, deploymentPattern string) storedRuleFixture {
	sr := storedRuleFixture{Kind: "exclude"}
	sr.Template.Scope = scope
	sr.Template.ScopeName = scopeName
	sr.Template.Name = templateName
	sr.Target.ProjectPattern = projectPattern
	sr.Target.DeploymentPattern = deploymentPattern
	return sr
}

func matchNames(matches []RequireRuleMatch) []string {
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.TemplateName)
	}
	sort.Strings(out)
	return out
}

func TestPolicyRequireRuleResolver_ResolveRequiredTemplates(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	tests := []struct {
		name      string
		project   string
		policies  map[string][]corev1.ConfigMap
		wantNames []string
	}{
		{
			name:      "no policies yields zero matches",
			project:   "lilies",
			policies:  nil,
			wantNames: nil,
		},
		{
			name:    "org-scope REQUIRE with project_pattern=* matches every project",
			project: "orchids",
			policies: func() map[string][]corev1.ConfigMap {
				fx := buildPolicyFixture(t)
				return map[string][]corev1.ConfigMap{
					fx.namespaces["org"]: {
						policyCM(fx.namespaces["org"], "audit", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "*", ""),
						}, t),
					},
				}
			}(),
			wantNames: []string{"audit-policy"},
		},
		{
			name:    "folder-scope REQUIRE matches project under that folder",
			project: "lilies",
			policies: func() map[string][]corev1.ConfigMap {
				fx := buildPolicyFixture(t)
				return map[string][]corev1.ConfigMap{
					fx.namespaces["folderEng"]: {
						policyCM(fx.namespaces["folderEng"], "eng-required", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeFolder, "eng", "eng-baseline", "*", ""),
						}, t),
					},
				}
			}(),
			wantNames: []string{"eng-baseline"},
		},
		{
			name:    "folder-scope REQUIRE does not match project under a sibling branch",
			project: "orchids",
			policies: func() map[string][]corev1.ConfigMap {
				fx := buildPolicyFixture(t)
				return map[string][]corev1.ConfigMap{
					fx.namespaces["folderEng"]: {
						policyCM(fx.namespaces["folderEng"], "eng-required", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeFolder, "eng", "eng-baseline", "*", ""),
						}, t),
					},
				}
			}(),
			wantNames: nil, // orchids is under org, not folder eng, so no eng-baseline
		},
		{
			name:    "narrow project_pattern matches prefix only",
			project: "foo-alpha",
			policies: func() map[string][]corev1.ConfigMap {
				fx := buildPolicyFixture(t)
				return map[string][]corev1.ConfigMap{
					fx.namespaces["folderEng"]: {
						policyCM(fx.namespaces["folderEng"], "foo-only", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeFolder, "eng", "foo-baseline", "foo-*", ""),
						}, t),
					},
				}
			}(),
			wantNames: []string{"foo-baseline"},
		},
		{
			name:    "narrow project_pattern does not match other prefix",
			project: "bar-alpha",
			policies: func() map[string][]corev1.ConfigMap {
				fx := buildPolicyFixture(t)
				return map[string][]corev1.ConfigMap{
					fx.namespaces["folderEng"]: {
						policyCM(fx.namespaces["folderEng"], "foo-only", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeFolder, "eng", "foo-baseline", "foo-*", ""),
						}, t),
					},
				}
			}(),
			wantNames: nil,
		},
		{
			name:    "overlapping require rules dedupe by (scope, scopeName, name)",
			project: "roses",
			policies: func() map[string][]corev1.ConfigMap {
				fx := buildPolicyFixture(t)
				// Org and both folders all require the same template; the
				// project sits under team-a (under eng under org) so every
				// level matches. Expect one match, not three.
				return map[string][]corev1.ConfigMap{
					fx.namespaces["org"]: {
						policyCM(fx.namespaces["org"], "org-p", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "*", ""),
						}, t),
					},
					fx.namespaces["folderEng"]: {
						policyCM(fx.namespaces["folderEng"], "eng-p", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "*", ""),
						}, t),
					},
					fx.namespaces["folderTeamA"]: {
						policyCM(fx.namespaces["folderTeamA"], "team-a-p", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "*", ""),
						}, t),
					},
				}
			}(),
			wantNames: []string{"audit-policy"},
		},
		{
			name:    "multiple distinct templates across ancestors all match",
			project: "roses",
			policies: func() map[string][]corev1.ConfigMap {
				fx := buildPolicyFixture(t)
				return map[string][]corev1.ConfigMap{
					fx.namespaces["org"]: {
						policyCM(fx.namespaces["org"], "org-p", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeOrganization, "acme", "org-template", "*", ""),
						}, t),
					},
					fx.namespaces["folderEng"]: {
						policyCM(fx.namespaces["folderEng"], "eng-p", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeFolder, "eng", "eng-template", "*", ""),
						}, t),
					},
					fx.namespaces["folderTeamA"]: {
						policyCM(fx.namespaces["folderTeamA"], "team-a-p", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeFolder, "team-a", "team-a-template", "*", ""),
						}, t),
					},
				}
			}(),
			wantNames: []string{"eng-template", "org-template", "team-a-template"},
		},
		{
			name:    "EXCLUDE rules never contribute to require matches",
			project: "lilies",
			policies: func() map[string][]corev1.ConfigMap {
				fx := buildPolicyFixture(t)
				return map[string][]corev1.ConfigMap{
					fx.namespaces["org"]: {
						policyCM(fx.namespaces["org"], "exclusion", []storedRuleFixture{
							excludeRule(v1alpha2.TemplateScopeOrganization, "acme", "banned-template", "*", ""),
						}, t),
					},
				}
			}(),
			wantNames: nil,
		},
		{
			name:    "REQUIRE with deployment_pattern still matches on project_pattern at project-creation time",
			project: "lilies",
			policies: func() map[string][]corev1.ConfigMap {
				fx := buildPolicyFixture(t)
				// deployment_pattern is observed but must not filter at
				// project-creation time (no candidate deployment name
				// exists yet). project_pattern controls the match.
				return map[string][]corev1.ConfigMap{
					fx.namespaces["org"]: {
						policyCM(fx.namespaces["org"], "api-only", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeOrganization, "acme", "api-template", "*", "api"),
						}, t),
					},
				}
			}(),
			wantNames: []string{"api-template"},
		},
		{
			name:    "REQUIRE with project_pattern that does not match is skipped",
			project: "orchids",
			policies: func() map[string][]corev1.ConfigMap {
				fx := buildPolicyFixture(t)
				return map[string][]corev1.ConfigMap{
					fx.namespaces["org"]: {
						policyCM(fx.namespaces["org"], "lilies-only", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "lilies", ""),
						}, t),
					},
				}
			}(),
			wantNames: nil,
		},
		{
			name:    "rule with empty project_pattern behaves like '*'",
			project: "orchids",
			policies: func() map[string][]corev1.ConfigMap {
				fx := buildPolicyFixture(t)
				return map[string][]corev1.ConfigMap{
					fx.namespaces["org"]: {
						policyCM(fx.namespaces["org"], "no-pattern", []storedRuleFixture{
							requireRule(v1alpha2.TemplateScopeOrganization, "acme", "always-apply", "", ""),
						}, t),
					},
				}
			}(),
			wantNames: []string{"always-apply"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fx := buildPolicyFixture(t)
			lister := policyresolver.NewAncestorPolicyLister(
				&policyListerMap{items: tc.policies},
				fx.walker,
				fx.resolver,
				policyresolver.RuleUnmarshalerFunc(fixtureUnmarshalRules),
			)
			rr := NewPolicyRequireRuleResolver(lister, fx.resolver.ProjectNamespace)

			matches, err := rr.ResolveRequiredTemplates(context.Background(), "acme", tc.project)
			if err != nil {
				t.Fatalf("ResolveRequiredTemplates returned error: %v", err)
			}
			got := matchNames(matches)
			want := append([]string(nil), tc.wantNames...)
			sort.Strings(want)
			if !equalStrings(got, want) {
				t.Errorf("match names: got %v, want %v", got, want)
			}
		})
	}
}

// TestPolicyRequireRuleResolver_PreservesVersionConstraint verifies that a
// REQUIRE rule's template.version_constraint is threaded through to the
// resulting RequireRuleMatch. Dropping it would silently render whichever
// version of the template is live instead of the one the policy author
// pinned via semver band — bypassing the explicit constraint (HOL-571
// review round 3 P2).
func TestPolicyRequireRuleResolver_PreservesVersionConstraint(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	fx := buildPolicyFixture(t)
	rule := storedRuleFixture{Kind: "require"}
	rule.Template.Scope = v1alpha2.TemplateScopeOrganization
	rule.Template.ScopeName = "acme"
	rule.Template.Name = "audit-policy"
	rule.Template.VersionConstraint = ">=1.0.0 <2.0.0"
	rule.Target.ProjectPattern = "*"

	policies := map[string][]corev1.ConfigMap{
		fx.namespaces["org"]: {
			policyCM(fx.namespaces["org"], "pinned", []storedRuleFixture{rule}, t),
		},
	}
	lister := policyresolver.NewAncestorPolicyLister(
		&policyListerMap{items: policies},
		fx.walker,
		fx.resolver,
		policyresolver.RuleUnmarshalerFunc(fixtureUnmarshalRules),
	)
	rr := NewPolicyRequireRuleResolver(lister, fx.resolver.ProjectNamespace)

	matches, err := rr.ResolveRequiredTemplates(context.Background(), "acme", "orchids")
	if err != nil {
		t.Fatalf("ResolveRequiredTemplates returned error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if got := matches[0].VersionConstraint; got != ">=1.0.0 <2.0.0" {
		t.Errorf("expected version_constraint=%q, got %q", ">=1.0.0 <2.0.0", got)
	}
}

// TestPolicyRequireRuleResolver_IgnoresProjectNamespacePolicies is the
// HOL-554 storage-isolation guardrail for project-creation time: a synthetic
// (forbidden) policy ConfigMap seeded in a project namespace must NOT be
// picked up even if it declares a REQUIRE rule with a wildcard pattern.
func TestPolicyRequireRuleResolver_IgnoresProjectNamespacePolicies(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	fx := buildPolicyFixture(t)
	rules := []storedRuleFixture{
		requireRule(v1alpha2.TemplateScopeOrganization, "acme", "should-be-ignored", "*", ""),
	}
	policies := map[string][]corev1.ConfigMap{
		// Forbidden placement: a policy living in a project namespace.
		fx.namespaces["projectLilies"]: {
			policyCM(fx.namespaces["projectLilies"], "pwned", rules, t),
		},
	}

	lister := policyresolver.NewAncestorPolicyLister(
		&policyListerMap{items: policies},
		fx.walker,
		fx.resolver,
		policyresolver.RuleUnmarshalerFunc(fixtureUnmarshalRules),
	)
	rr := NewPolicyRequireRuleResolver(lister, fx.resolver.ProjectNamespace)

	got, err := rr.ResolveRequiredTemplates(context.Background(), "acme", "lilies")
	if err != nil {
		t.Fatalf("ResolveRequiredTemplates returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("project-namespace policy leaked into require matches: got %v", matchNames(got))
	}
}

// TestPolicyRequireRuleResolver_NilResolverIsNoOp exercises the fail-open
// contract: any nil dependency should degrade to (nil, nil) rather than
// panicking or returning an error.
func TestPolicyRequireRuleResolver_NilResolverIsNoOp(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	fx := buildPolicyFixture(t)
	rrNilLister := NewPolicyRequireRuleResolver(nil, fx.resolver.ProjectNamespace)
	got, err := rrNilLister.ResolveRequiredTemplates(context.Background(), "acme", "lilies")
	if err != nil {
		t.Fatalf("expected no error with nil lister, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 matches with nil lister, got %d", len(got))
	}

	lister := policyresolver.NewAncestorPolicyLister(
		&policyListerMap{},
		fx.walker,
		fx.resolver,
		policyresolver.RuleUnmarshalerFunc(fixtureUnmarshalRules),
	)
	rrNilNamespaceFunc := NewPolicyRequireRuleResolver(lister, nil)
	got, err = rrNilNamespaceFunc.ResolveRequiredTemplates(context.Background(), "acme", "lilies")
	if err != nil {
		t.Fatalf("expected no error with nil projectNamespaceFor, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 matches with nil projectNamespaceFor, got %d", len(got))
	}
}

// TestPolicyRequireRuleResolver_EmptyProjectIsNoOp: if the caller passes an
// empty project name, there is nothing to match against and the resolver
// returns no matches without a walker round-trip.
func TestPolicyRequireRuleResolver_EmptyProjectIsNoOp(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	fx := buildPolicyFixture(t)
	lister := policyresolver.NewAncestorPolicyLister(
		&policyListerMap{items: map[string][]corev1.ConfigMap{
			fx.namespaces["org"]: {
				policyCM(fx.namespaces["org"], "p", []storedRuleFixture{
					requireRule(v1alpha2.TemplateScopeOrganization, "acme", "should-never-apply", "*", ""),
				}, t),
			},
		}},
		fx.walker,
		fx.resolver,
		policyresolver.RuleUnmarshalerFunc(fixtureUnmarshalRules),
	)
	rr := NewPolicyRequireRuleResolver(lister, fx.resolver.ProjectNamespace)
	got, err := rr.ResolveRequiredTemplates(context.Background(), "acme", "")
	if err != nil {
		t.Fatalf("expected no error with empty project, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 matches with empty project, got %d", len(got))
	}
}

// TestPolicyRequireRuleResolver_WalkerErrorPropagates: a walker failure is
// a hard error at project-creation time so the caller can decide to roll
// back the project rather than silently skip policy-injected templates.
func TestPolicyRequireRuleResolver_WalkerErrorPropagates(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	r := &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
	lister := policyresolver.NewAncestorPolicyLister(
		&policyListerMap{},
		&failingWalkerFixture{err: errors.New("walker exploded")},
		r,
		policyresolver.RuleUnmarshalerFunc(fixtureUnmarshalRules),
	)
	rr := NewPolicyRequireRuleResolver(lister, r.ProjectNamespace)

	_, err := rr.ResolveRequiredTemplates(context.Background(), "acme", "lilies")
	if err == nil {
		t.Fatal("expected walker error to propagate, got nil")
	}
}

type failingWalkerFixture struct {
	err error
}

func (f *failingWalkerFixture) WalkAncestors(_ context.Context, _ string) ([]*corev1.Namespace, error) {
	return nil, f.err
}

func equalStrings(a, b []string) bool {
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
