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
	"sort"
	"testing"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// projectNamespaceTargetCRD builds a ProjectNamespace-kind target ref. Per
// ADR 034 the handler is expected to store `name="*"` for this kind
// (the namespace is 1:1 with the project), but callers may set any value
// because the resolver ignores `name`. Tests that care about the name-axis
// robustness pass an explicit name string through here.
func projectNamespaceTargetCRD(project, name string) templatesv1alpha1.TemplatePolicyBindingTargetRef {
	return templatesv1alpha1.TemplatePolicyBindingTargetRef{
		Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindProjectNamespace,
		Name:        name,
		ProjectName: project,
	}
}

// newProjectNamespaceResolverForTest wires a ProjectNamespaceResolver around
// the canonical HOL-567 fixture: it plugs the in-memory
// bindingListerFromMap, a real resolver.Walker over the fake clientset, and
// the baseResolver so tests exercise the same ancestor-walk logic that
// production uses.
func newProjectNamespaceResolverForTest(
	bindings *bindingListerFromMap,
	walker WalkerInterface,
	r *resolver.Resolver,
) *ProjectNamespaceResolver {
	return NewProjectNamespaceResolver(NewAncestorBindingLister(bindings, walker, r))
}

// bindingNames returns the binding names in the given slice so tests can
// assert set membership without caring about pointer identity or
// ancestor-walk ordering (which is tested separately by
// TestAncestorBindingLister_*).
func bindingNames(bindings []*ResolvedBinding) []string {
	names := make([]string, 0, len(bindings))
	for _, b := range bindings {
		if b == nil {
			continue
		}
		names = append(names, b.Name)
	}
	sort.Strings(names)
	return names
}

// TestProjectNamespaceResolver_NoBindings asserts the empty fixture
// returns an empty slice without error. This is the "no bindings present"
// AC bullet — a cluster without any TemplatePolicyBinding at all must
// keep CreateProject as a pure pass-through.
func TestProjectNamespaceResolver_NoBindings(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bl := &bindingListerFromMap{items: nil}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	got, err := pnr.Resolve(context.Background(), ns["folderEng"], "new-project")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no matched bindings on empty fixture; got %v", bindingNames(got))
	}
}

// TestProjectNamespaceResolver_ExactProjectNameMatch asserts a literal
// `project_name` on a ProjectNamespace target selects the matching new
// project and rejects a peer project. This is the "exact match" AC bullet.
func TestProjectNamespaceResolver_ExactProjectNameMatch(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "ns-acme-bind",
				orgPolicyRefCRD("ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("acme", "*"),
				},
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	// Exact-match new project under folderEng; the org-namespace binding
	// has project_name="acme" so it matches when newProjectName="acme".
	got, err := pnr.Resolve(context.Background(), ns["folderEng"], "acme")
	if err != nil {
		t.Fatalf("Resolve(acme): %v", err)
	}
	if names := bindingNames(got); len(names) != 1 || names[0] != "ns-acme-bind" {
		t.Errorf("expected [ns-acme-bind] for acme; got %v", names)
	}

	// A different project name must not match the literal.
	got, err = pnr.Resolve(context.Background(), ns["folderEng"], "zephyr")
	if err != nil {
		t.Fatalf("Resolve(zephyr): %v", err)
	}
	if names := bindingNames(got); len(names) != 0 {
		t.Errorf("expected no matches for zephyr against literal acme; got %v", names)
	}
}

// TestProjectNamespaceResolver_WildcardProjectName asserts a binding with
// `project_name: "*"` matches any new project reachable through the
// binding's ancestor-walk. This is the "wildcard match on projectName"
// AC bullet.
func TestProjectNamespaceResolver_WildcardProjectName(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "ns-any-bind",
				orgPolicyRefCRD("ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("*", "*"),
				},
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	// Any project name under the org walk matches.
	for _, project := range []string{"acme", "zephyr", "beta-project"} {
		got, err := pnr.Resolve(context.Background(), ns["folderEng"], project)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", project, err)
		}
		if names := bindingNames(got); len(names) != 1 || names[0] != "ns-any-bind" {
			t.Errorf("project=%q: expected [ns-any-bind]; got %v", project, names)
		}
	}

	// Empty project name is still rejected — HOL-554 guardrail surfaced
	// through the `nameMatches` empty-value guard (see
	// TestBindingAppliesTo_WildcardRejectsEmptyTargetValue).
	got, err := pnr.Resolve(context.Background(), ns["folderEng"], "")
	if err != nil {
		t.Fatalf("Resolve(empty): %v", err)
	}
	if names := bindingNames(got); len(names) != 0 {
		t.Errorf("expected no matches for empty project name; got %v", names)
	}
}

// TestProjectNamespaceResolver_WildcardName asserts the resolver tolerates
// `name="*"` (the handler-enforced canonical value per ADR 034) and also
// any non-canonical `name` literal — the resolver ignores the name axis for
// ProjectNamespace targets because there is exactly one namespace per
// project. This is the "wildcard match on name" AC bullet; the same test
// also confirms that a literal non-"*" name does not filter out a
// matching ProjectNamespace binding.
func TestProjectNamespaceResolver_WildcardName(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			// Canonical ADR 034 shape: name="*", project_name="acme".
			bindingCRD(ns["org"], "canonical",
				orgPolicyRefCRD("ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("acme", "*"),
				},
			),
			// Non-canonical (defensive): any other name literal must
			// still match because the resolver ignores name for
			// ProjectNamespace targets.
			bindingCRD(ns["org"], "non-canonical-name",
				orgPolicyRefCRD("ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("acme", "some-literal"),
				},
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	got, err := pnr.Resolve(context.Background(), ns["folderEng"], "acme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	wantNames := []string{"canonical", "non-canonical-name"}
	if names := bindingNames(got); !equalStringSlices(names, wantNames) {
		t.Errorf("expected %v; got %v", wantNames, names)
	}
}

// TestProjectNamespaceResolver_IgnoresOtherTargetKinds asserts a binding
// that carries only non-ProjectNamespace target_refs is filtered out. The
// resolver never leaks a PROJECT_TEMPLATE / DEPLOYMENT binding into a
// CreateProject flow.
func TestProjectNamespaceResolver_IgnoresOtherTargetKinds(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "deployment-only",
				orgPolicyRefCRD("ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					deploymentTargetCRD("acme", "api"),
				},
			),
			bindingCRD(ns["org"], "project-template-only",
				orgPolicyRefCRD("ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectTemplateTargetCRD("acme", "web"),
				},
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	got, err := pnr.Resolve(context.Background(), ns["folderEng"], "acme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if names := bindingNames(got); len(names) != 0 {
		t.Errorf("expected no matches (no ProjectNamespace targets in fixture); got %v", names)
	}
}

// TestProjectNamespaceResolver_MixedKindsOnSameBinding asserts a binding
// that carries both a ProjectNamespace target AND a non-ProjectNamespace
// target is still selected — the resolver matches if any target_ref is a
// ProjectNamespace-kind ref for the project. Later phases care only about
// the ProjectNamespace targets; returning the whole binding lets them
// dereference the bound policy for namespace rendering. The render
// pipeline already filters target_refs by kind when it picks which
// templates to run against which render target.
func TestProjectNamespaceResolver_MixedKindsOnSameBinding(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "mixed",
				orgPolicyRefCRD("ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					deploymentTargetCRD("acme", "api"),
					projectNamespaceTargetCRD("acme", "*"),
				},
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	got, err := pnr.Resolve(context.Background(), ns["folderEng"], "acme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if names := bindingNames(got); len(names) != 1 || names[0] != "mixed" {
		t.Errorf("expected [mixed]; got %v", names)
	}
}

// TestProjectNamespaceResolver_MissingPolicyRefIsNoop asserts a binding
// with a nil/zero policy_ref is treated as a no-op: it does not appear in
// the resolver's output. Mirrors
// TestFolderResolver_BindingsNonexistentPolicyIsNoopAndDoesNotError — the
// folder resolver logs a warning and contributes no rules; the
// project-namespace resolver drops the binding altogether so later phases
// don't attempt a TemplatePolicy lookup against an empty reference.
func TestProjectNamespaceResolver_MissingPolicyRefIsNoop(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			// Binding targets the project namespace for "acme" but the
			// spec.policyRef is the zero value — the CRD validator would
			// normally reject this, but defensive-in-depth here ensures
			// the resolver doesn't surface a bound-to-nothing binding.
			bindingCRD(ns["org"], "no-policy",
				templatesv1alpha1.LinkedTemplatePolicyRef{},
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("acme", "*"),
				},
			),
			// Peer binding with a real policyRef — must still show up in
			// the result so we know the filter is per-binding, not
			// all-or-nothing.
			bindingCRD(ns["org"], "has-policy",
				orgPolicyRefCRD("ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("acme", "*"),
				},
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	got, err := pnr.Resolve(context.Background(), ns["folderEng"], "acme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if names := bindingNames(got); len(names) != 1 || names[0] != "has-policy" {
		t.Errorf("expected [has-policy] (no-policy binding filtered); got %v", names)
	}
}

// TestProjectNamespaceResolver_EmptyTargetRefsIsNoMatch asserts a binding
// with no target_refs never matches. Mirrors
// TestFolderResolver_BindingsEmptyTargetListContributesNothing — an empty
// target list declares intent to attach zero render targets.
func TestProjectNamespaceResolver_EmptyTargetRefsIsNoMatch(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "empty-targets",
				orgPolicyRefCRD("ns-policy"),
				nil,
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	got, err := pnr.Resolve(context.Background(), ns["folderEng"], "acme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if names := bindingNames(got); len(names) != 0 {
		t.Errorf("expected no matches for empty target list; got %v", names)
	}
}

// TestProjectNamespaceResolver_StorageIsolationSkip is the HOL-554
// guardrail under the new resolver: even when a (forbidden) ProjectNamespace
// binding is seeded in a project namespace, the resolver must not consume it.
// AncestorBindingLister already skips project namespaces; this test nails
// down that the new resolver inherits the guard end-to-end.
func TestProjectNamespaceResolver_StorageIsolationSkip(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		// Forbidden: a ProjectNamespace binding stashed in a project
		// namespace. AncestorBindingLister skips project namespaces.
		ns["projectLilies"]: {
			bindingCRD(ns["projectLilies"], "pwned",
				orgPolicyRefCRD("ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("*", "*"),
				},
			),
		},
		// Legitimate: same binding in the folder namespace.
		ns["folderEng"]: {
			bindingCRD(ns["folderEng"], "legit",
				folderPolicyRefCRD("eng", "ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("*", "*"),
				},
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	// Start the walk at folderEng (the new project's parent). The project
	// namespace with the forbidden binding is NOT on this chain, so the
	// only way the forbidden binding could leak is via some future
	// regression in AncestorBindingLister. Either way, the resolver must
	// not surface it.
	got, err := pnr.Resolve(context.Background(), ns["folderEng"], "new-project")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if names := bindingNames(got); len(names) != 1 || names[0] != "legit" {
		t.Errorf("expected [legit]; got %v (pwned binding must never leak)", names)
	}
}

// TestProjectNamespaceResolver_FolderAncestorChain asserts the walk
// traverses every ancestor: a project being created under team-a must see
// bindings from team-a, eng (team-a's parent), and acme (the org).
//
// Because team-a is a folder namespace, the AncestorBindingLister walks up
// to eng and then to acme; all three contribute when their targets match.
func TestProjectNamespaceResolver_FolderAncestorChain(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "org-wildcard",
				orgPolicyRefCRD("ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("*", "*"),
				},
			),
		},
		ns["folderEng"]: {
			bindingCRD(ns["folderEng"], "eng-wildcard",
				folderPolicyRefCRD("eng", "ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("*", "*"),
				},
			),
		},
		ns["folderTeamA"]: {
			bindingCRD(ns["folderTeamA"], "team-a-literal",
				folderPolicyRefCRD("team-a", "ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("flowers", "*"),
				},
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	// New project "flowers" under team-a: the team-a literal binding
	// matches, the eng wildcard matches, the org wildcard matches.
	got, err := pnr.Resolve(context.Background(), ns["folderTeamA"], "flowers")
	if err != nil {
		t.Fatalf("Resolve(flowers): %v", err)
	}
	wantFlowers := []string{"eng-wildcard", "org-wildcard", "team-a-literal"}
	if names := bindingNames(got); !equalStringSlices(names, wantFlowers) {
		t.Errorf("expected %v; got %v", wantFlowers, names)
	}

	// New project "roses" under team-a: the team-a literal requires
	// project_name="flowers" so it drops out; eng and org wildcards still
	// match.
	got, err = pnr.Resolve(context.Background(), ns["folderTeamA"], "roses")
	if err != nil {
		t.Fatalf("Resolve(roses): %v", err)
	}
	wantRoses := []string{"eng-wildcard", "org-wildcard"}
	if names := bindingNames(got); !equalStringSlices(names, wantRoses) {
		t.Errorf("expected %v; got %v", wantRoses, names)
	}
}

// TestProjectNamespaceResolver_WildcardBindingFolderFanout asserts a binding
// on a sibling folder does not bleed into a new project whose parent is a
// different folder. Mirrors TestFolderResolver_WildcardBindingFolderFanout
// for the new resolver kind. Wildcard reach is bounded by the binding's
// ancestor-walk storage scope (ADR 029 "storage scope bounds reach").
func TestProjectNamespaceResolver_WildcardBindingFolderFanout(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	// A `{*, *}` binding in team-a only reaches projects whose ancestor
	// walk traverses team-a. A new project under a sibling folder (eng)
	// never visits team-a, so team-a's binding must never surface.
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["folderTeamA"]: {
			bindingCRD(ns["folderTeamA"], "team-a-wildcard",
				folderPolicyRefCRD("team-a", "ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("*", "*"),
				},
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	// New project under eng: team-a is NOT on eng's ancestor chain (eng
	// is team-a's parent, not the other way around).
	got, err := pnr.Resolve(context.Background(), ns["folderEng"], "something")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if names := bindingNames(got); len(names) != 0 {
		t.Errorf("sibling-folder wildcard leaked; got %v", names)
	}
}

// TestProjectNamespaceResolver_Misconfigured returns (nil, nil) on any
// nil dependency — the fail-open contract mirrors AncestorBindingLister
// (see TestAncestorBindingLister_Misconfigured).
func TestProjectNamespaceResolver_Misconfigured(t *testing.T) {
	// Nil receiver.
	var nilResolver *ProjectNamespaceResolver
	got, err := nilResolver.Resolve(context.Background(), "holos-fld-eng", "acme")
	if err != nil {
		t.Fatalf("nil receiver: expected nil error; got %v", err)
	}
	if got != nil {
		t.Errorf("nil receiver: expected nil slice; got %v", got)
	}

	// Nil ancestor lister.
	pnr := NewProjectNamespaceResolver(nil)
	got, err = pnr.Resolve(context.Background(), "holos-fld-eng", "acme")
	if err != nil {
		t.Fatalf("nil ancestor lister: expected nil error; got %v", err)
	}
	if got != nil {
		t.Errorf("nil ancestor lister: expected nil slice; got %v", got)
	}
}

// TestProjectNamespaceResolver_EmptyInputs returns (nil, nil) without
// error when either parentNamespace or newProjectName is empty. Same
// guardrail as `nameMatches` refuses to wildcard-match an empty target,
// hoisted up so the walker isn't spun for an impossible request.
func TestProjectNamespaceResolver_EmptyInputs(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "ns-any-bind",
				orgPolicyRefCRD("ns-policy"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{
					projectNamespaceTargetCRD("*", "*"),
				},
			),
		},
	}
	bl := &bindingListerFromMap{items: bindings}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	cases := []struct {
		name   string
		parent string
		newP   string
	}{
		{name: "empty parent", parent: "", newP: "acme"},
		{name: "empty new project name", parent: ns["folderEng"], newP: ""},
		{name: "both empty", parent: "", newP: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := pnr.Resolve(context.Background(), tc.parent, tc.newP)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got != nil {
				t.Errorf("expected nil slice; got %v", bindingNames(got))
			}
		})
	}
}

// TestProjectNamespaceResolver_WalkerErrorPropagates asserts a walker
// failure is surfaced to the caller so CreateProject can fail the RPC
// rather than silently proceeding with an incomplete binding set.
// Mirrors AncestorBindingLister.ListBindings — walker errors are
// non-recoverable; per-namespace lister errors are not.
func TestProjectNamespaceResolver_WalkerErrorPropagates(t *testing.T) {
	_, r, _ := buildFixture()

	wantErr := errors.New("walker-boom")
	walker := &failingWalker{err: wantErr}

	bl := &bindingListerFromMap{items: nil}
	pnr := newProjectNamespaceResolverForTest(bl, walker, r)

	got, err := pnr.Resolve(context.Background(), "holos-fld-eng", "acme")
	if !errors.Is(err, wantErr) {
		t.Errorf("expected walker error to propagate; got err=%v", err)
	}
	if got != nil {
		t.Errorf("expected nil slice on walker failure; got %v", got)
	}
}

// TestProjectNamespaceBindingAppliesTo_KindAndWildcards exercises the
// match helper directly, covering every combination the Resolve loop
// relies on. Going through Resolve exercises the same paths, but the
// direct unit keeps the match logic on a tight feedback loop and pins
// each AC bullet to a single assertion.
func TestProjectNamespaceBindingAppliesTo_KindAndWildcards(t *testing.T) {
	pnTarget := func(project, name string) *consolev1.TemplatePolicyBindingTargetRef {
		return &consolev1.TemplatePolicyBindingTargetRef{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_NAMESPACE,
			Name:        name,
			ProjectName: project,
		}
	}
	deploymentTarget := func(project, name string) *consolev1.TemplatePolicyBindingTargetRef {
		return &consolev1.TemplatePolicyBindingTargetRef{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
			Name:        name,
			ProjectName: project,
		}
	}

	type tc struct {
		name           string
		refs           []*consolev1.TemplatePolicyBindingTargetRef
		newProjectName string
		want           bool
	}
	cases := []tc{
		{
			name:           "nil binding returns false",
			refs:           nil,
			newProjectName: "acme",
			want:           false,
		},
		{
			name:           "ProjectNamespace literal exact match",
			refs:           []*consolev1.TemplatePolicyBindingTargetRef{pnTarget("acme", "*")},
			newProjectName: "acme",
			want:           true,
		},
		{
			name:           "ProjectNamespace literal mismatch",
			refs:           []*consolev1.TemplatePolicyBindingTargetRef{pnTarget("acme", "*")},
			newProjectName: "zephyr",
			want:           false,
		},
		{
			name:           "ProjectNamespace wildcard project matches any non-empty name",
			refs:           []*consolev1.TemplatePolicyBindingTargetRef{pnTarget("*", "*")},
			newProjectName: "anything",
			want:           true,
		},
		{
			name:           "ProjectNamespace wildcard project rejects empty target",
			refs:           []*consolev1.TemplatePolicyBindingTargetRef{pnTarget("*", "*")},
			newProjectName: "",
			want:           false,
		},
		{
			name:           "ProjectNamespace ignores name field (non-canonical literal)",
			refs:           []*consolev1.TemplatePolicyBindingTargetRef{pnTarget("acme", "not-a-wildcard")},
			newProjectName: "acme",
			want:           true,
		},
		{
			name:           "DEPLOYMENT kind never matches (kind is not wildcarded)",
			refs:           []*consolev1.TemplatePolicyBindingTargetRef{deploymentTarget("*", "*")},
			newProjectName: "acme",
			want:           false,
		},
		{
			name: "PROJECT_TEMPLATE kind never matches",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{{
				Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE,
				Name:        "*",
				ProjectName: "*",
			}},
			newProjectName: "acme",
			want:           false,
		},
		{
			name: "multi-ref: fallthrough to ProjectNamespace after DEPLOYMENT",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{
				deploymentTarget("*", "*"),
				pnTarget("acme", "*"),
			},
			newProjectName: "acme",
			want:           true,
		},
		{
			name: "multi-ref: neither ref matches",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{
				deploymentTarget("*", "*"),
				pnTarget("acme", "*"),
			},
			newProjectName: "zephyr",
			want:           false,
		},
		{
			name: "UNSPECIFIED kind never matches",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{{
				Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_UNSPECIFIED,
				Name:        "*",
				ProjectName: "*",
			}},
			newProjectName: "acme",
			want:           false,
		},
		{
			name: "nil entry in target_refs is skipped",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{
				nil,
				pnTarget("acme", "*"),
			},
			newProjectName: "acme",
			want:           true,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			var b *ResolvedBinding
			if c.refs != nil {
				b = &ResolvedBinding{
					Name:       "test",
					Namespace:  "holos-org-acme",
					TargetRefs: c.refs,
				}
			}
			got := projectNamespaceBindingAppliesTo(b, c.newProjectName)
			if got != c.want {
				t.Errorf("projectNamespaceBindingAppliesTo(newProject=%q) = %v, want %v",
					c.newProjectName, got, c.want)
			}
		})
	}
}
