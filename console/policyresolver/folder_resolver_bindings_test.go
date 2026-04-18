package policyresolver

import (
	"context"
	"encoding/json"
	"sort"
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

// newFolderResolverWithBindingsForTest constructs a resolver wired with both
// rule-side and binding-side deps, so the tests in this file exercise the
// HOL-596 binding evaluation path.
func newFolderResolverWithBindingsForTest(
	policies *policyListerFromClient,
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

// TestFolderResolver_Bindings covers the HOL-596 acceptance criteria:
//   - Binding + no rule.Target → binding matches.
//   - Binding + matching rule.Target → binding wins (glob ignored).
//   - No binding + matching rule.Target → glob still applies.
//   - Binding targets nonexistent policy → resolver logs and no-ops.
func TestFolderResolver_Bindings(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	requireRule := func(scope, scopeName, templateName, projectPattern, deploymentPattern string) storedRuleTest {
		sr := storedRuleTest{Kind: "require"}
		sr.Template.Scope = scope
		sr.Template.ScopeName = scopeName
		sr.Template.Name = templateName
		sr.Target.ProjectPattern = projectPattern
		sr.Target.DeploymentPattern = deploymentPattern
		return sr
	}
	excludeRule := func(scope, scopeName, templateName, projectPattern, deploymentPattern string) storedRuleTest {
		sr := storedRuleTest{Kind: "exclude"}
		sr.Template.Scope = scope
		sr.Template.ScopeName = scopeName
		sr.Template.Name = templateName
		sr.Target.ProjectPattern = projectPattern
		sr.Target.DeploymentPattern = deploymentPattern
		return sr
	}
	orgPolicyRef := func(policyName string) storedPolicyRefTest {
		return storedPolicyRefTest{
			Scope:     v1alpha2.TemplateScopeOrganization,
			ScopeName: "acme",
			Name:      policyName,
		}
	}
	folderPolicyRef := func(folder, policyName string) storedPolicyRefTest {
		return storedPolicyRefTest{
			Scope:     v1alpha2.TemplateScopeFolder,
			ScopeName: folder,
			Name:      policyName,
		}
	}
	deploymentTarget := func(project, name string) storedTargetRefTest {
		return storedTargetRefTest{Kind: "deployment", Name: name, ProjectName: project}
	}
	projectTemplateTarget := func(project, name string) storedTargetRefTest {
		return storedTargetRefTest{Kind: "project-template", Name: name, ProjectName: project}
	}

	type wantRes struct {
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
		want       wantRes
	}{
		{
			// AC: Binding + no matching rule.Target → binding matches. The
			// policy's glob Target is narrowed so the rule would NOT apply
			// on its own; only the binding can select this render target.
			// The test proves the binding path wires the policy onto the
			// render target independently of the glob.
			name:       "binding covers target with non-matching rule glob; REQUIRE injected via binding",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						// project_pattern that does not match lilies.
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "roses", ""),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "audit-to-lilies-api",
						orgPolicyRef("audit"),
						[]storedTargetRefTest{deploymentTarget("lilies", "api")},
						t,
					),
				},
			},
			want: wantRes{names: []string{"audit-policy"}},
		},
		{
			// AC: Binding covers a peer target but the current render
			// target is not on the binding's target list. The policy
			// has a non-matching glob (roses), so the legacy glob path
			// contributes nothing either; the render target sees an
			// empty set.
			name:       "binding covers peer target and glob does not match; no refs",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "worker",
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "roses", ""),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "audit-to-lilies-api",
						orgPolicyRef("audit"),
						[]storedTargetRefTest{deploymentTarget("lilies", "api")},
						t,
					),
				},
			},
			want: wantRes{names: nil},
		},
		{
			// AC: Binding + matching rule.Target → binding wins. The
			// rule's glob Target is "different" than the binding's
			// explicit selection — but because the binding covers the
			// current target, the rule applies regardless of whether
			// the glob would have matched. Using a glob that would
			// otherwise NOT match this target proves "binding wins".
			name:       "binding wins over rule glob that would not have matched",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "nomatch", "nomatch"),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "audit-to-lilies-api",
						orgPolicyRef("audit"),
						[]storedTargetRefTest{deploymentTarget("lilies", "api")},
						t,
					),
				},
			},
			want: wantRes{names: []string{"audit-policy"}},
		},
		{
			// AC: No binding + matching rule.Target → glob still applies.
			// The policy's rule names a template and carries a glob that
			// matches the render target; no binding exists for this
			// policy, so legacy evaluation runs.
			name:       "no binding covering target; matching rule glob still applies",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "*", ""),
					}, t),
				},
			},
			bindings: nil,
			want:     wantRes{names: []string{"audit-policy"}},
		},
		{
			// AC: No binding + matching rule.Target → glob still applies
			// even when a binding exists for a different policy. The
			// binding for policy-A covers this target but does NOT cover
			// policy-B, so policy-B's glob Target is consulted normally.
			name:       "binding for other policy does not suppress a different policy's glob",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "policy-a", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "tmpl-a", "", ""),
					}, t),
					policyCM(ns["org"], "policy-b", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "tmpl-b", "*", ""),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "a-only",
						orgPolicyRef("policy-a"),
						[]storedTargetRefTest{deploymentTarget("lilies", "api")},
						t,
					),
				},
			},
			want: wantRes{names: []string{"tmpl-a", "tmpl-b"}},
		},
		{
			// AC: Binding targets nonexistent policy → no-op. The
			// binding names "missing" which is not in the ancestor
			// chain; the resolver logs a warning and contributes no
			// refs. A legacy glob rule on an unrelated policy still
			// applies — proving the bad binding did not short-circuit
			// the whole evaluation.
			name:       "binding references nonexistent policy; no-op and legacy glob still runs",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "legacy", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "legacy-tmpl", "*", ""),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "bad-binding",
						orgPolicyRef("missing"),
						[]storedTargetRefTest{deploymentTarget("lilies", "api")},
						t,
					),
				},
			},
			want: wantRes{names: []string{"legacy-tmpl"}},
		},
		{
			// PROJECT_TEMPLATE match semantics: a binding with
			// project_name matches only when the render target's
			// project matches.
			name:       "binding matches PROJECT_TEMPLATE by name+project",
			projectNs:  ns["projectLilies"],
			target:     TargetKindProjectTemplate,
			targetName: "baseline",
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "prj-audit", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "baseline-policy", "", ""),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "prj-audit-bind",
						orgPolicyRef("prj-audit"),
						[]storedTargetRefTest{projectTemplateTarget("lilies", "baseline")},
						t,
					),
				},
			},
			want: wantRes{names: []string{"baseline-policy"}},
		},
		{
			// PROJECT_TEMPLATE match semantics: wrong project_name does
			// not match even when the template name matches. The policy's
			// glob is narrowed to a project that does not include lilies,
			// so the legacy fallback also contributes nothing; the render
			// target sees an empty set.
			name:       "binding PROJECT_TEMPLATE project_name mismatch does not apply",
			projectNs:  ns["projectLilies"],
			target:     TargetKindProjectTemplate,
			targetName: "baseline",
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "prj-audit", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "baseline-policy", "roses", ""),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "prj-audit-bind",
						orgPolicyRef("prj-audit"),
						[]storedTargetRefTest{projectTemplateTarget("roses", "baseline")},
						t,
					),
				},
			},
			want: wantRes{names: nil},
		},
		{
			// DEPLOYMENT match semantics: wrong project_name does not
			// match even when name matches. The policy's glob is narrowed
			// to a project that does not include lilies, so the legacy
			// fallback also contributes nothing.
			name:       "binding DEPLOYMENT project_name mismatch does not apply",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "audit", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "roses", ""),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "audit-to-roses-api",
						orgPolicyRef("audit"),
						[]storedTargetRefTest{deploymentTarget("roses", "api")},
						t,
					),
				},
			},
			want: wantRes{names: nil},
		},
		{
			// EXCLUDE via binding: a binding covers an EXCLUDE policy,
			// so the EXCLUDE rule removes a REQUIRE-injected template
			// regardless of the EXCLUDE rule's glob. This verifies the
			// binding path applies to both rule kinds.
			name:       "binding covers EXCLUDE policy; removes REQUIRE-added ref",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]corev1.ConfigMap{
				ns["org"]: {
					policyCM(ns["org"], "req", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "*", ""),
					}, t),
					policyCM(ns["org"], "exc", []storedRuleTest{
						excludeRule(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy", "nomatch", ""),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["org"]: {
					bindingCM(ns["org"], "exc-to-lilies-api",
						orgPolicyRef("exc"),
						[]storedTargetRefTest{deploymentTarget("lilies", "api")},
						t,
					),
				},
			},
			want: wantRes{names: nil},
		},
		{
			// Folder-scoped binding and folder-scoped policy: a binding
			// stored in the eng folder references a folder-scoped
			// policy, covering a deployment in a project directly under
			// that folder. Ancestor walk picks both up; binding
			// evaluation unifies them.
			name:       "folder-scoped binding and policy match nested project deployment",
			projectNs:  ns["projectLilies"],
			target:     TargetKindDeployment,
			targetName: "api",
			policies: map[string][]corev1.ConfigMap{
				ns["folderEng"]: {
					policyCM(ns["folderEng"], "eng-audit", []storedRuleTest{
						requireRule(v1alpha2.TemplateScopeFolder, "eng", "eng-audit-tmpl", "", ""),
					}, t),
				},
			},
			bindings: map[string][]corev1.ConfigMap{
				ns["folderEng"]: {
					bindingCM(ns["folderEng"], "eng-bind",
						folderPolicyRef("eng", "eng-audit"),
						[]storedTargetRefTest{deploymentTarget("lilies", "api")},
						t,
					),
				},
			},
			want: wantRes{names: []string{"eng-audit-tmpl"}},
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

// TestFolderResolver_BindingsNonexistentPolicyIsNoopAndDoesNotError asserts
// a binding whose policy_ref does not resolve to any policy in the ancestor
// chain is treated as a no-op: the resolver does not error, no refs are
// injected, and the explicit refs passed by the caller are returned
// unchanged. This is the degrade-gracefully contract from the HOL-596 AC:
// "resolver logs a warning and treats as no-op (does not fail render)".
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

// TestFolderResolver_BindingsBackwardCompatibleWithoutBindingWireup asserts
// that a resolver constructed via NewFolderResolver (no binding lister or
// unmarshaler) continues to behave exactly like the pre-HOL-596 resolver:
// every rule is evaluated via its glob Target. This keeps pre-binding
// fixtures and wire-ups working unchanged.
func TestFolderResolver_BindingsBackwardCompatibleWithoutBindingWireup(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	policies := map[string][]corev1.ConfigMap{
		ns["org"]: {
			policyCM(ns["org"], "audit", []storedRuleTest{
				{Kind: "require",
					Template: struct {
						Scope             string `json:"scope"`
						ScopeName         string `json:"scope_name"`
						Name              string `json:"name"`
						VersionConstraint string `json:"version_constraint,omitempty"`
					}{Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy"},
					Target: struct {
						ProjectPattern    string `json:"project_pattern"`
						DeploymentPattern string `json:"deployment_pattern,omitempty"`
					}{ProjectPattern: "*"}},
			}, t),
		},
	}

	pl := &policyListerFromClient{items: policies}
	fr := NewFolderResolver(pl, walker, r, RuleUnmarshalerFunc(testUnmarshalRules))

	got, err := fr.Resolve(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 1 || got[0].GetName() != "audit-policy" {
		t.Errorf("expected legacy glob to still apply: got %v", refNames(got))
	}
}

// TestFolderResolver_BindingsEmptyTargetListNeverMatches asserts a binding
// with no target_refs never covers any render target — an empty target list
// contributes zero render targets. The bound policy's rules then fall
// through to legacy glob evaluation.
func TestFolderResolver_BindingsEmptyTargetListNeverMatches(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	policies := map[string][]corev1.ConfigMap{
		ns["org"]: {
			policyCM(ns["org"], "audit", []storedRuleTest{
				{Kind: "require",
					Template: struct {
						Scope             string `json:"scope"`
						ScopeName         string `json:"scope_name"`
						Name              string `json:"name"`
						VersionConstraint string `json:"version_constraint,omitempty"`
					}{Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy"},
					Target: struct {
						ProjectPattern    string `json:"project_pattern"`
						DeploymentPattern string `json:"deployment_pattern,omitempty"`
					}{ProjectPattern: "*"}},
			}, t),
		},
	}
	// Binding with empty target_refs. The JSON wire shape is "[]" which
	// decodes to an empty slice. The binding exists but covers nothing.
	bindings := map[string][]corev1.ConfigMap{
		ns["org"]: {
			bindingCM(ns["org"], "empty-bind",
				storedPolicyRefTest{Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit"},
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
	// The empty binding does not cover the render target; legacy glob
	// fallback fires for policy "audit" whose rule has project_pattern=*.
	if len(got) != 1 || got[0].GetName() != "audit-policy" {
		t.Errorf("expected legacy glob to fire when binding covers nothing: got %v", refNames(got))
	}
}
