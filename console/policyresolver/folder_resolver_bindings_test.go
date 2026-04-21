package policyresolver

import (
	"context"
	"testing"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// bindingListerFromMap adapts an in-memory map to BindingListerInNamespace.
// Mirrors policyListerFromClient — tests feed per-namespace
// TemplatePolicyBinding CRs and the resolver reads them through the same
// ancestor-walking lister production uses.
//
// HOL-662 switched the return type from corev1.ConfigMap to the CRD shape;
// tests no longer vendor a ConfigMap decoder.
//
// HOL-622 switched the interface return shape to a pointer slice so the
// resolver can forward the cached CRD pointer through without re-addressing
// a copy. The map still holds value slices for readable test fixtures.
type bindingListerFromMap struct {
	items map[string][]templatesv1alpha1.TemplatePolicyBinding
}

func (b *bindingListerFromMap) ListBindingsInNamespace(_ context.Context, ns string) ([]*templatesv1alpha1.TemplatePolicyBinding, error) {
	src := b.items[ns]
	if len(src) == 0 {
		return nil, nil
	}
	out := make([]*templatesv1alpha1.TemplatePolicyBinding, 0, len(src))
	for i := range src {
		out = append(out, &src[i])
	}
	return out, nil
}

// bindingCRD returns a TemplatePolicyBinding CR populated with the given
// policy_ref and target_refs. Mirrors policyCRD. Tests build fixtures from
// this helper rather than constructing raw CRDs inline so the shape stays
// consistent across the suite and so a schema drift surfaces in exactly one
// place.
func bindingCRD(
	namespace, name string,
	policyRef templatesv1alpha1.LinkedTemplatePolicyRef,
	targets []templatesv1alpha1.TemplatePolicyBindingTargetRef,
) templatesv1alpha1.TemplatePolicyBinding {
	return templatesv1alpha1.TemplatePolicyBinding{
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef:  policyRef,
			TargetRefs: targets,
		},
		// metav1.ObjectMeta embedded directly — the CRD exposes the
		// Name/Namespace fields via the anonymous ObjectMeta struct.
		ObjectMeta: objectMetaCRD(name, namespace, v1alpha2.ResourceTypeTemplatePolicyBinding),
	}
}

// orgPolicyRefCRD builds an organization-scoped CRD policy_ref.
func orgPolicyRefCRD(policyName string) templatesv1alpha1.LinkedTemplatePolicyRef {
	return templatesv1alpha1.LinkedTemplatePolicyRef{
		Namespace: "holos-org-acme",
		Name:      policyName,
	}
}

// folderPolicyRefCRD builds a folder-scoped CRD policy_ref.
func folderPolicyRefCRD(folder, policyName string) templatesv1alpha1.LinkedTemplatePolicyRef {
	return templatesv1alpha1.LinkedTemplatePolicyRef{
		Namespace: "holos-fld-" + folder,
		Name:      policyName,
	}
}

// deploymentTargetCRD / projectTemplateTargetCRD return the binding-side
// target refs the table cases use.
func deploymentTargetCRD(project, name string) templatesv1alpha1.TemplatePolicyBindingTargetRef {
	return templatesv1alpha1.TemplatePolicyBindingTargetRef{
		Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
		Name:        name,
		ProjectName: project,
	}
}

func projectTemplateTargetCRD(project, name string) templatesv1alpha1.TemplatePolicyBindingTargetRef {
	return templatesv1alpha1.TemplatePolicyBindingTargetRef{
		Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
		Name:        name,
		ProjectName: project,
	}
}

// newFolderResolverWithBindingsForTest constructs a resolver wired with
// both rule-side and binding-side deps, so the tests in this file and
// folder_resolver_test.go exercise the post-HOL-600 binding evaluation
// path. HOL-662 dropped the unmarshaler seams — construction is now
// 4-arg.
func newFolderResolverWithBindingsForTest(
	policies PolicyListerInNamespace,
	bindings *bindingListerFromMap,
	walker WalkerInterface,
	r *resolver.Resolver,
) PolicyResolver {
	return NewFolderResolverWithBindings(policies, walker, r, bindings)
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
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "orphan",
				orgPolicyRefCRD("missing"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
			),
		},
	}

	explicit := []*consolev1.LinkedTemplateRef{
		orgTemplateRef("explicit"),
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

	policies := map[string][]templatesv1alpha1.TemplatePolicy{
		ns["org"]: {
			policyCRD(ns["org"], "audit", []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
			}),
		},
	}
	// Binding with empty target_refs. The binding exists but covers nothing.
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "empty-bind",
				orgPolicyRefCRD("audit"),
				nil, // zero targets
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

	policies := map[string][]templatesv1alpha1.TemplatePolicy{
		ns["org"]: {
			policyCRD(ns["org"], "audit", []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
			}),
		},
	}
	// Binding targets roses/api, render target is lilies/api — no
	// match on project_name.
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["org"]: {
			bindingCRD(ns["org"], "audit-to-roses",
				orgPolicyRefCRD("audit"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("roses", "api")},
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

// TestBindingAppliesTo_Wildcards exercises every
// `{name, project_name} × {literal, WildcardAny}` combination across both
// PROJECT_TEMPLATE and DEPLOYMENT kinds and asserts:
//
//   - literal/literal is a regression guard (exact match).
//   - name="*" matches any target name within the same project.
//   - project_name="*" matches any project for the same target name.
//   - name="*" AND project_name="*" matches every resource of the given kind.
//   - `kind` is never wildcarded: a DEPLOYMENT ref never matches a
//     PROJECT_TEMPLATE target and vice-versa.
//
// This is a direct unit test of the match function rather than going through
// Resolve: it keeps the match-logic AC bullets in HOL-770 on a tight feedback
// loop and surfaces regressions in a single file when either the sentinel or
// the comparison strategy drifts. The folder cascade behavior is covered
// separately below.
func TestBindingAppliesTo_Wildcards(t *testing.T) {
	type wantMatch struct {
		project    string
		targetKind TargetKind
		targetName string
		match      bool
	}

	projectTemplateRef := func(project, name string) *consolev1.TemplatePolicyBindingTargetRef {
		return &consolev1.TemplatePolicyBindingTargetRef{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE,
			Name:        name,
			ProjectName: project,
		}
	}
	deploymentRef := func(project, name string) *consolev1.TemplatePolicyBindingTargetRef {
		return &consolev1.TemplatePolicyBindingTargetRef{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
			Name:        name,
			ProjectName: project,
		}
	}

	tests := []struct {
		name  string
		refs  []*consolev1.TemplatePolicyBindingTargetRef
		cases []wantMatch
	}{
		// --- PROJECT_TEMPLATE kind ---
		{
			name: "project_template literal name and project",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{projectTemplateRef("acme-prod", "web")},
			cases: []wantMatch{
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "web", match: true},
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "api", match: false},
				{project: "other-project", targetKind: TargetKindProjectTemplate, targetName: "web", match: false},
				// kind mismatch — never matches, even when name+project align.
				{project: "acme-prod", targetKind: TargetKindDeployment, targetName: "web", match: false},
			},
		},
		{
			name: "project_template wildcard name matches every name in project",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{projectTemplateRef("acme-prod", "*")},
			cases: []wantMatch{
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "web", match: true},
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "api", match: true},
				{project: "other-project", targetKind: TargetKindProjectTemplate, targetName: "web", match: false},
				// kind mismatch — never matches.
				{project: "acme-prod", targetKind: TargetKindDeployment, targetName: "web", match: false},
			},
		},
		{
			name: "project_template wildcard project matches every project with named template",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{projectTemplateRef("*", "web")},
			cases: []wantMatch{
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "web", match: true},
				{project: "other-project", targetKind: TargetKindProjectTemplate, targetName: "web", match: true},
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "api", match: false},
				// kind mismatch — never matches.
				{project: "acme-prod", targetKind: TargetKindDeployment, targetName: "web", match: false},
			},
		},
		{
			name: "project_template wildcard name and project matches every template",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{projectTemplateRef("*", "*")},
			cases: []wantMatch{
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "web", match: true},
				{project: "other-project", targetKind: TargetKindProjectTemplate, targetName: "api", match: true},
				// kind mismatch — {*, *} still doesn't bleed across kinds.
				{project: "acme-prod", targetKind: TargetKindDeployment, targetName: "web", match: false},
				{project: "other-project", targetKind: TargetKindDeployment, targetName: "api", match: false},
			},
		},
		// --- DEPLOYMENT kind ---
		{
			name: "deployment literal name and project",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{deploymentRef("acme-prod", "web")},
			cases: []wantMatch{
				{project: "acme-prod", targetKind: TargetKindDeployment, targetName: "web", match: true},
				{project: "acme-prod", targetKind: TargetKindDeployment, targetName: "api", match: false},
				{project: "other-project", targetKind: TargetKindDeployment, targetName: "web", match: false},
				// kind mismatch — never matches.
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "web", match: false},
			},
		},
		{
			name: "deployment wildcard name matches every deployment in project",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{deploymentRef("acme-prod", "*")},
			cases: []wantMatch{
				{project: "acme-prod", targetKind: TargetKindDeployment, targetName: "web", match: true},
				{project: "acme-prod", targetKind: TargetKindDeployment, targetName: "api", match: true},
				{project: "other-project", targetKind: TargetKindDeployment, targetName: "web", match: false},
				// kind mismatch — never matches.
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "web", match: false},
			},
		},
		{
			name: "deployment wildcard project matches every project with named deployment",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{deploymentRef("*", "web")},
			cases: []wantMatch{
				{project: "acme-prod", targetKind: TargetKindDeployment, targetName: "web", match: true},
				{project: "other-project", targetKind: TargetKindDeployment, targetName: "web", match: true},
				{project: "acme-prod", targetKind: TargetKindDeployment, targetName: "api", match: false},
				// kind mismatch — never matches.
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "web", match: false},
			},
		},
		{
			name: "deployment wildcard name and project matches every deployment",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{deploymentRef("*", "*")},
			cases: []wantMatch{
				{project: "acme-prod", targetKind: TargetKindDeployment, targetName: "web", match: true},
				{project: "other-project", targetKind: TargetKindDeployment, targetName: "api", match: true},
				// kind mismatch — {*, *} still doesn't bleed across kinds.
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "web", match: false},
				{project: "other-project", targetKind: TargetKindProjectTemplate, targetName: "api", match: false},
			},
		},
		// --- Multi-ref: wildcard must not short-circuit a following literal
		// refusal and vice versa. The first matching ref wins; a wildcard
		// ref that doesn't match the queried kind must fall through to the
		// next ref in the slice.
		{
			name: "multi-ref deployment wildcard plus project_template literal",
			refs: []*consolev1.TemplatePolicyBindingTargetRef{
				deploymentRef("*", "*"),
				projectTemplateRef("acme-prod", "web"),
			},
			cases: []wantMatch{
				// First ref (deployment wildcard) skipped for PROJECT_TEMPLATE
				// queries; second ref (literal) matches the named template.
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "web", match: true},
				// Second ref doesn't match a different name; first ref's
				// wildcard was the wrong kind — so no match.
				{project: "acme-prod", targetKind: TargetKindProjectTemplate, targetName: "api", match: false},
				// First ref (deployment wildcard) matches any deployment.
				{project: "other-project", targetKind: TargetKindDeployment, targetName: "api", match: true},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			b := &ResolvedBinding{
				Name:       "wildcard-test",
				Namespace:  "holos-org-acme",
				TargetRefs: tc.refs,
			}
			for _, c := range tc.cases {
				got := bindingAppliesTo(b, c.project, c.targetKind, c.targetName)
				if got != c.match {
					t.Errorf("bindingAppliesTo(project=%q, kind=%v, name=%q) = %v, want %v",
						c.project, c.targetKind, c.targetName, got, c.match)
				}
			}
		})
	}
}

// TestBindingAppliesTo_WildcardRejectsEmptyTargetValue pins the HOL-554
// storage-isolation guardrail under the new wildcard semantics: when
// Resolve cannot derive a project slug from the render-target namespace
// (e.g., ProjectFromNamespace fails and the call site passes `project = ""`
// through to bindingAppliesTo), a binding whose `project_name` is the
// wildcard must NOT match the empty target. Same for `name`. Without this
// guard, a wildcard binding would silently cover render targets that have
// no project to attach to.
func TestBindingAppliesTo_WildcardRejectsEmptyTargetValue(t *testing.T) {
	wildcardBoth := &ResolvedBinding{
		Name:      "wildcard-both",
		Namespace: "holos-org-acme",
		TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
			Name:        "*",
			ProjectName: "*",
		}},
	}

	// Empty project: simulates Resolve's fallback when
	// ProjectFromNamespace returns an error. A `{*, *}` binding must not
	// match this "no project" case.
	if bindingAppliesTo(wildcardBoth, "", TargetKindDeployment, "api") {
		t.Error("wildcard project_name must not match empty project value (ProjectFromNamespace failure)")
	}
	// Empty target name: the handler never stores an empty Name on the
	// render-target side, but the resolver is called through multiple
	// layers; belt-and-suspenders check that `name="*"` doesn't match
	// a caller that forgot to populate targetName either.
	if bindingAppliesTo(wildcardBoth, "acme-prod", TargetKindDeployment, "") {
		t.Error("wildcard name must not match empty targetName")
	}
	// Both empty: certainly must not match.
	if bindingAppliesTo(wildcardBoth, "", TargetKindDeployment, "") {
		t.Error("wildcard must not match both-empty target")
	}

	// Sanity check: a single-field wildcard still rejects the empty
	// counterpart on the other axis too.
	wildcardProjectOnly := &ResolvedBinding{
		Name:      "wildcard-project",
		Namespace: "holos-org-acme",
		TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
			Name:        "web",
			ProjectName: "*",
		}},
	}
	if bindingAppliesTo(wildcardProjectOnly, "", TargetKindDeployment, "web") {
		t.Error("wildcard project_name must not match empty project even when name matches literally")
	}
	wildcardNameOnly := &ResolvedBinding{
		Name:      "wildcard-name",
		Namespace: "holos-org-acme",
		TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
			Name:        "*",
			ProjectName: "acme-prod",
		}},
	}
	if bindingAppliesTo(wildcardNameOnly, "acme-prod", TargetKindDeployment, "") {
		t.Error("wildcard name must not match empty targetName even when project matches literally")
	}
}

// TestBindingAppliesTo_NilAndEmptyRefs asserts the defensive rejects:
// a nil binding, a binding with no TargetRefs, and a binding whose
// TargetRefs slice contains a nil entry all degrade to "no match"
// without panic. The zero-value kind (UNSPECIFIED) never matches
// either of the two concrete TargetKinds the resolver queries.
func TestBindingAppliesTo_NilAndEmptyRefs(t *testing.T) {
	if bindingAppliesTo(nil, "acme-prod", TargetKindDeployment, "web") {
		t.Error("nil binding must not match")
	}
	empty := &ResolvedBinding{Name: "empty", Namespace: "holos-org-acme"}
	if bindingAppliesTo(empty, "acme-prod", TargetKindDeployment, "web") {
		t.Error("binding with nil TargetRefs must not match")
	}
	withNilEntry := &ResolvedBinding{
		Name:       "nil-entry",
		Namespace:  "holos-org-acme",
		TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{nil},
	}
	if bindingAppliesTo(withNilEntry, "acme-prod", TargetKindDeployment, "web") {
		t.Error("binding with a nil TargetRef entry must not match")
	}
	// Zero-value (UNSPECIFIED) kind with wildcard name and project must
	// still not match either of the two concrete render-target kinds.
	zeroKind := &ResolvedBinding{
		Name:      "zero-kind",
		Namespace: "holos-org-acme",
		TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_UNSPECIFIED,
			Name:        "*",
			ProjectName: "*",
		}},
	}
	if bindingAppliesTo(zeroKind, "acme-prod", TargetKindDeployment, "web") {
		t.Error("UNSPECIFIED target kind must not match DEPLOYMENT")
	}
	if bindingAppliesTo(zeroKind, "acme-prod", TargetKindProjectTemplate, "web") {
		t.Error("UNSPECIFIED target kind must not match PROJECT_TEMPLATE")
	}
}

// TestFolderResolver_WildcardBindingFolderCascade asserts that a binding
// at folder `team-a` with `{project: "*", name: "*"}` matches resources
// in projects under team-a (roses) but does NOT escape to projects under
// a sibling folder (lilies under eng) or to projects directly under the
// org (orchids).
//
// The ancestor walk caps wildcard reach: when resolving for lilies the
// walk visits lilies → eng → org; the team-a folder is never traversed,
// so its `{*, *}` binding is never even seen. The wildcard changes
// *matching* inside the binding's storage scope, not the storage scope
// itself (HOL-770 AC; ADR 029 "storage scope bounds reach" bullet).
func TestFolderResolver_WildcardBindingFolderCascade(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	policies := map[string][]templatesv1alpha1.TemplatePolicy{
		ns["folderTeamA"]: {
			policyCRD(ns["folderTeamA"], "team-a-audit", []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeFolder, "team-a", "team-a-audit"),
			}),
		},
	}
	// Binding at team-a folder with full wildcard on both fields for
	// DEPLOYMENT. Should match any deployment reachable through the
	// team-a namespace's ancestor walk — i.e., deployments in projects
	// directly under team-a. Projects under sibling folders or under
	// org directly never walk through team-a, so they must not see it.
	bindings := map[string][]templatesv1alpha1.TemplatePolicyBinding{
		ns["folderTeamA"]: {
			bindingCRD(ns["folderTeamA"], "team-a-wildcard",
				folderPolicyRefCRD("team-a", "team-a-audit"),
				[]templatesv1alpha1.TemplatePolicyBindingTargetRef{{
					Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
					Name:        "*",
					ProjectName: "*",
				}},
			),
		},
	}
	pl := &policyListerFromClient{items: policies}
	bl := &bindingListerFromMap{items: bindings}
	fr := newFolderResolverWithBindingsForTest(pl, bl, walker, r)

	ctx := context.Background()

	// projectRoses is directly under team-a: the wildcard binding MUST
	// select every deployment in roses.
	got, err := fr.Resolve(ctx, ns["projectRoses"], TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve(roses/api): %v", err)
	}
	if names := refNames(got); len(names) != 1 || names[0] != "team-a-audit" {
		t.Errorf("roses/api: expected [team-a-audit], got %v", names)
	}
	// Same project, a *different* deployment name — wildcard means both
	// match; this is the "doesn't accidentally pin to one name" check.
	got, err = fr.Resolve(ctx, ns["projectRoses"], TargetKindDeployment, "web", nil)
	if err != nil {
		t.Fatalf("Resolve(roses/web): %v", err)
	}
	if names := refNames(got); len(names) != 1 || names[0] != "team-a-audit" {
		t.Errorf("roses/web: expected [team-a-audit], got %v", names)
	}

	// projectLilies is under eng (sibling of team-a): team-a's binding
	// is NOT on its ancestor chain, so even `{*, *}` must not match.
	got, err = fr.Resolve(ctx, ns["projectLilies"], TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve(lilies/api): %v", err)
	}
	if names := refNames(got); len(names) != 0 {
		t.Errorf("lilies/api: sibling-folder wildcard leaked; got %v, want empty", names)
	}

	// projectOrchids is directly under org (skips eng and team-a
	// entirely): team-a's binding is not on this chain either.
	got, err = fr.Resolve(ctx, ns["projectOrchids"], TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve(orchids/api): %v", err)
	}
	if names := refNames(got); len(names) != 0 {
		t.Errorf("orchids/api: org-sibling wildcard leaked; got %v, want empty", names)
	}

	// A PROJECT_TEMPLATE render target in roses must NOT be matched by
	// the DEPLOYMENT `{*, *}` binding — kind is never wildcarded.
	got, err = fr.Resolve(ctx, ns["projectRoses"], TargetKindProjectTemplate, "any", nil)
	if err != nil {
		t.Fatalf("Resolve(roses/project-template): %v", err)
	}
	if names := refNames(got); len(names) != 0 {
		t.Errorf("roses/project-template: DEPLOYMENT {*,*} must not match PROJECT_TEMPLATE; got %v", names)
	}
}
