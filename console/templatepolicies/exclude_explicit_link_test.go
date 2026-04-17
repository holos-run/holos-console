package templatepolicies

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// ============================================================================
// HOL-570 test fixtures
//
// These tests exercise the "EXCLUDE cannot contradict an explicit link"
// guardrail in CreateTemplatePolicy / UpdateTemplatePolicy. The fixture
// mirrors the HOL-567 namespace hierarchy used elsewhere in the codebase
// (org `acme`, folder `eng` under the org, folder `team-a` under `eng`,
// projects under each level) so the test shape tracks the policy resolver's
// own table-driven tests.
// ============================================================================

// hol570Fixture holds the named fake namespaces for tests below. The namespace
// names match the production resolver prefixes (`holos-`, `org-`, `fld-`,
// `prj-`) so namespace classification errors would surface exactly as they
// would in a real cluster.
type hol570Fixture struct {
	orgNs         string
	folderEngNs   string
	folderTeamANs string
	projLilies    string // project under folder eng
	projRoses     string // project under folder team-a
	projOrchids   string // project directly under org
}

// hol570Namespaces returns the canonical fixture namespace names. The
// values match the newTestResolver() prefixes so a test that passes these
// strings back through the resolver round-trips cleanly.
func hol570Namespaces() hol570Fixture {
	return hol570Fixture{
		orgNs:         "holos-org-acme",
		folderEngNs:   "holos-fld-eng",
		folderTeamANs: "holos-fld-team-a",
		projLilies:    "holos-prj-lilies",
		projRoses:     "holos-prj-roses",
		projOrchids:   "holos-prj-orchids",
	}
}

// mkNsForFixture constructs a managed namespace with the expected label set
// (resource-type + parent). Keeping the helper in this file mirrors the
// fake-client fixture used by console/policyresolver/folder_resolver_test.go
// without sharing a cross-package testing util.
func mkNsForFixture(name, kind, parent string) *corev1.Namespace {
	labels := map[string]string{
		v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
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

// mkLinkedTemplatesAnnotation marshals a list of (scope, scope_name, name)
// triples into the linked-templates JSON wire shape.
func mkLinkedTemplatesAnnotation(t *testing.T, refs ...linkedTripleForTest) string {
	t.Helper()
	type storedRef struct {
		Scope     string `json:"scope"`
		ScopeName string `json:"scope_name"`
		Name      string `json:"name"`
	}
	stored := make([]storedRef, 0, len(refs))
	for _, r := range refs {
		stored = append(stored, storedRef{Scope: r.scope, ScopeName: r.scopeName, Name: r.name})
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshaling linked-templates: %v", err)
	}
	return string(raw)
}

type linkedTripleForTest struct {
	scope     string // e.g. v1alpha2.TemplateScopeOrganization
	scopeName string
	name      string
}

// mkDeployment constructs a Deployment ConfigMap with the given
// linked-templates annotation.
func mkDeployment(namespace, name, linkedTemplatesJSON string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeDeployment,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationLinkedTemplates: linkedTemplatesJSON,
			},
		},
	}
}

// mkProjectTemplate constructs a project-scope Template ConfigMap with the
// given linked-templates annotation.
func mkProjectTemplate(namespace, name, linkedTemplatesJSON string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationLinkedTemplates: linkedTemplatesJSON,
			},
		},
	}
}

// fakeWalker is a lightweight ancestor walker backed by any
// kubernetes.Interface. It follows the same parent-label contract as
// *resolver.Walker without dragging in its max-depth / cycle-detection
// machinery — the test fixtures in this file are acyclic and shallow.
type fakeWalker struct {
	client kubernetes.Interface
}

func (w *fakeWalker) WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error) {
	var chain []*corev1.Namespace
	current := startNs
	for {
		ns, err := w.client.CoreV1().Namespaces().Get(ctx, current, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		chain = append(chain, ns)
		if ns.Labels[v1alpha2.LabelResourceType] == v1alpha2.ResourceTypeOrganization {
			return chain, nil
		}
		parent := ns.Labels[v1alpha2.AnnotationParent]
		if parent == "" {
			return chain, nil
		}
		current = parent
	}
}

// buildHol570Fixture returns a fake client seeded with the canonical
// namespace hierarchy plus the supplied ProjectTemplate / Deployment
// ConfigMaps. Callers pass the Templates + Deployments they want on each
// project so a test case can isolate the annotation state it needs.
func buildHol570Fixture(resources ...runtime.Object) (*fake.Clientset, hol570Fixture) {
	fx := hol570Namespaces()
	objects := []runtime.Object{
		mkNsForFixture(fx.orgNs, v1alpha2.ResourceTypeOrganization, ""),
		mkNsForFixture(fx.folderEngNs, v1alpha2.ResourceTypeFolder, fx.orgNs),
		mkNsForFixture(fx.folderTeamANs, v1alpha2.ResourceTypeFolder, fx.folderEngNs),
		mkNsForFixture(fx.projLilies, v1alpha2.ResourceTypeProject, fx.folderEngNs),
		mkNsForFixture(fx.projRoses, v1alpha2.ResourceTypeProject, fx.folderTeamANs),
		mkNsForFixture(fx.projOrchids, v1alpha2.ResourceTypeProject, fx.orgNs),
	}
	objects = append(objects, resources...)
	return fake.NewClientset(objects...), fx
}

// orgTemplateRef is a short constructor for an org-scope LinkedTemplateRef
// used as an EXCLUDE target in the test table.
func orgTemplateRef(scopeName, name string) *consolev1.LinkedTemplateRef {
	return &consolev1.LinkedTemplateRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
		ScopeName: scopeName,
		Name:      name,
	}
}

// excludeRuleFor builds a TemplatePolicyRule with kind EXCLUDE pointing at
// the given template ref and target patterns.
func excludeRuleFor(tmpl *consolev1.LinkedTemplateRef, projectPattern, deploymentPattern string) *consolev1.TemplatePolicyRule {
	return &consolev1.TemplatePolicyRule{
		Kind:     consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE,
		Template: tmpl,
		Target: &consolev1.TemplatePolicyTarget{
			ProjectPattern:    projectPattern,
			DeploymentPattern: deploymentPattern,
		},
	}
}

// requireRuleFor mirrors excludeRuleFor for REQUIRE rules used in the
// "REQUIRE is unaffected" test case.
func requireRuleFor(tmpl *consolev1.LinkedTemplateRef, projectPattern, deploymentPattern string) *consolev1.TemplatePolicyRule {
	return &consolev1.TemplatePolicyRule{
		Kind:     consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE,
		Template: tmpl,
		Target: &consolev1.TemplatePolicyTarget{
			ProjectPattern:    projectPattern,
			DeploymentPattern: deploymentPattern,
		},
	}
}

// makeHol570Fixture is a convenience wrapper that builds the client + walker
// adapter and returns a wired Handler. The fake client stays accessible via
// the returned *Handler (through its K8sClient) but tests that only need the
// Handler itself can ignore the other return values.
func makeHol570Fixture(t *testing.T, resources ...runtime.Object) *Handler {
	t.Helper()
	client, _ := buildHol570Fixture(resources...)
	return newHol570HandlerFromClient(t, client)
}

func newHol570HandlerFromClient(t *testing.T, client *fake.Clientset) *Handler {
	t.Helper()
	r := newTestResolver()
	k := NewK8sClient(client, r)
	topology := NewK8sResourceTopology(client, r, &fakeWalker{client: client})
	return NewHandler(k, r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: map[string]string{"owner@example.com": "owner"}}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: map[string]string{"owner@example.com": "owner"}}).
		WithResourceTopologyResolver(topology)
}

// ============================================================================
// Tests
// ============================================================================

// TestCreateRejectsExcludeOnExplicitlyLinkedDeployment is the primary HOL-570
// positive test: a wildcard-wildcard EXCLUDE rule against a template that is
// explicitly linked by one existing Deployment must be rejected with
// FailedPrecondition, and the error message must name the offending
// deployment.
func TestCreateRejectsExcludeOnExplicitlyLinkedDeployment(t *testing.T) {
	linkedJSON := mkLinkedTemplatesAnnotation(t, linkedTripleForTest{
		scope: v1alpha2.TemplateScopeOrganization, scopeName: "acme", name: "httproute",
	})
	h := makeHol570Fixture(t,
		mkDeployment("holos-prj-lilies", "web", linkedJSON),
	)

	ctx := authedCtx("owner@example.com", nil)
	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope: newOrgScope("acme"),
		Policy: &consolev1.TemplatePolicy{
			Name:     "block-httproute",
			ScopeRef: newOrgScope("acme"),
			Rules: []*consolev1.TemplatePolicyRule{
				excludeRuleFor(orgTemplateRef("acme", "httproute"), "*", "*"),
			},
		},
	}))
	if err == nil {
		t.Fatal("expected FailedPrecondition")
	}
	if got := connect.CodeOf(err); got != connect.CodeFailedPrecondition {
		t.Fatalf("expected CodeFailedPrecondition, got %v: %v", got, err)
	}
	if !strings.Contains(err.Error(), "deployment lilies/web") {
		t.Errorf("expected error to name deployment lilies/web, got: %v", err)
	}
	if !strings.Contains(err.Error(), "rule 0") {
		t.Errorf("expected error to cite rule index 0, got: %v", err)
	}
	if !strings.Contains(err.Error(), "httproute") {
		t.Errorf("expected error to cite template name, got: %v", err)
	}
}

// TestCreateRejectsExcludeOnExplicitlyLinkedProjectTemplate asserts the same
// rejection path fires for a ProjectTemplate owner-link, not just
// Deployments. The fixture places the explicit link on a project-scope
// Template ConfigMap so the error message must name that resource instead.
func TestCreateRejectsExcludeOnExplicitlyLinkedProjectTemplate(t *testing.T) {
	linkedJSON := mkLinkedTemplatesAnnotation(t, linkedTripleForTest{
		scope: v1alpha2.TemplateScopeOrganization, scopeName: "acme", name: "httproute",
	})
	h := makeHol570Fixture(t,
		mkProjectTemplate("holos-prj-lilies", "web-tmpl", linkedJSON),
	)

	ctx := authedCtx("owner@example.com", nil)
	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope: newOrgScope("acme"),
		Policy: &consolev1.TemplatePolicy{
			Name:     "block-httproute",
			ScopeRef: newOrgScope("acme"),
			Rules: []*consolev1.TemplatePolicyRule{
				excludeRuleFor(orgTemplateRef("acme", "httproute"), "*", "*"),
			},
		},
	}))
	if err == nil {
		t.Fatal("expected FailedPrecondition")
	}
	if got := connect.CodeOf(err); got != connect.CodeFailedPrecondition {
		t.Fatalf("expected CodeFailedPrecondition, got %v: %v", got, err)
	}
	if !strings.Contains(err.Error(), "project-template lilies/web-tmpl") {
		t.Errorf("expected error to name project-template lilies/web-tmpl, got: %v", err)
	}
}

// TestCreateAllowsExcludeWhenNoExplicitLinkExists verifies the allow-path:
// an EXCLUDE rule against a template that no existing resource explicitly
// links is accepted. The deployment in the fixture links a *different*
// template than the one the EXCLUDE targets, so there is no conflict.
func TestCreateAllowsExcludeWhenNoExplicitLinkExists(t *testing.T) {
	linkedJSON := mkLinkedTemplatesAnnotation(t, linkedTripleForTest{
		scope: v1alpha2.TemplateScopeOrganization, scopeName: "acme", name: "different-template",
	})
	h := makeHol570Fixture(t,
		mkDeployment("holos-prj-lilies", "web", linkedJSON),
	)

	ctx := authedCtx("owner@example.com", nil)
	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope: newOrgScope("acme"),
		Policy: &consolev1.TemplatePolicy{
			Name:     "block-httproute",
			ScopeRef: newOrgScope("acme"),
			Rules: []*consolev1.TemplatePolicyRule{
				excludeRuleFor(orgTemplateRef("acme", "httproute"), "*", "*"),
			},
		},
	}))
	if err != nil {
		t.Fatalf("expected EXCLUDE against unlinked template to be accepted: %v", err)
	}
}

// TestCreateAllowsExcludeWhenDeploymentPatternMissesConflict verifies the
// narrow-pattern allow-path. A deployment_pattern that does not match the
// only project/deployment holding the explicit link must accept the rule.
func TestCreateAllowsExcludeWhenDeploymentPatternMissesConflict(t *testing.T) {
	linkedJSON := mkLinkedTemplatesAnnotation(t, linkedTripleForTest{
		scope: v1alpha2.TemplateScopeOrganization, scopeName: "acme", name: "httproute",
	})
	h := makeHol570Fixture(t,
		mkDeployment("holos-prj-lilies", "web", linkedJSON),
	)

	ctx := authedCtx("owner@example.com", nil)
	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope: newOrgScope("acme"),
		Policy: &consolev1.TemplatePolicy{
			Name:     "block-httproute-for-other-pattern",
			ScopeRef: newOrgScope("acme"),
			Rules: []*consolev1.TemplatePolicyRule{
				// Pattern "api" does not match "web", so there is no conflict
				// even though the template IS linked on lilies/web.
				excludeRuleFor(orgTemplateRef("acme", "httproute"), "*", "api"),
			},
		},
	}))
	if err != nil {
		t.Fatalf("expected narrow deployment_pattern to avoid conflict: %v", err)
	}
}

// TestCreateAllowsRequireAgainstExplicitlyLinkedTemplate confirms the
// guardrail is scoped to EXCLUDE rules only. A REQUIRE rule carrying the
// identical template + target pattern must be accepted even though the
// deployment explicitly links the template.
func TestCreateAllowsRequireAgainstExplicitlyLinkedTemplate(t *testing.T) {
	linkedJSON := mkLinkedTemplatesAnnotation(t, linkedTripleForTest{
		scope: v1alpha2.TemplateScopeOrganization, scopeName: "acme", name: "httproute",
	})
	h := makeHol570Fixture(t,
		mkDeployment("holos-prj-lilies", "web", linkedJSON),
	)

	ctx := authedCtx("owner@example.com", nil)
	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope: newOrgScope("acme"),
		Policy: &consolev1.TemplatePolicy{
			Name:     "require-httproute",
			ScopeRef: newOrgScope("acme"),
			Rules: []*consolev1.TemplatePolicyRule{
				requireRuleFor(orgTemplateRef("acme", "httproute"), "*", "*"),
			},
		},
	}))
	if err != nil {
		t.Fatalf("REQUIRE against explicitly-linked template must be accepted: %v", err)
	}
}

// TestCreateRejectsOffendingExcludeAmongMultipleRules asserts the per-rule
// error shape. A policy with one innocuous REQUIRE and one offending
// EXCLUDE rejects with the EXCLUDE rule's index (1, not 0).
func TestCreateRejectsOffendingExcludeAmongMultipleRules(t *testing.T) {
	linkedJSON := mkLinkedTemplatesAnnotation(t, linkedTripleForTest{
		scope: v1alpha2.TemplateScopeOrganization, scopeName: "acme", name: "httproute",
	})
	h := makeHol570Fixture(t,
		mkDeployment("holos-prj-lilies", "web", linkedJSON),
	)

	ctx := authedCtx("owner@example.com", nil)
	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope: newOrgScope("acme"),
		Policy: &consolev1.TemplatePolicy{
			Name:     "mixed",
			ScopeRef: newOrgScope("acme"),
			Rules: []*consolev1.TemplatePolicyRule{
				requireRuleFor(orgTemplateRef("acme", "audit-log"), "*", "*"),
				excludeRuleFor(orgTemplateRef("acme", "httproute"), "*", "*"),
			},
		},
	}))
	if err == nil {
		t.Fatal("expected FailedPrecondition")
	}
	if got := connect.CodeOf(err); got != connect.CodeFailedPrecondition {
		t.Fatalf("expected CodeFailedPrecondition, got %v: %v", got, err)
	}
	if !strings.Contains(err.Error(), "rule 1") {
		t.Errorf("expected error to cite rule index 1 (the EXCLUDE), got: %v", err)
	}
}

// TestCreateAllowsExcludeOnEmptyScope asserts the empty-scope permissive
// rule: when there are no candidate resources under the policy scope, any
// EXCLUDE rule is accepted because there is nothing to conflict with.
func TestCreateAllowsExcludeOnEmptyScope(t *testing.T) {
	// Fixture carries the namespace hierarchy but zero Deployments or
	// ProjectTemplates — so no resource can hold the linked-templates
	// annotation.
	h := makeHol570Fixture(t)

	ctx := authedCtx("owner@example.com", nil)
	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope: newOrgScope("acme"),
		Policy: &consolev1.TemplatePolicy{
			Name:     "preemptive-block",
			ScopeRef: newOrgScope("acme"),
			Rules: []*consolev1.TemplatePolicyRule{
				excludeRuleFor(orgTemplateRef("acme", "httproute"), "*", "*"),
			},
		},
	}))
	if err != nil {
		t.Fatalf("empty scope must accept EXCLUDE: %v", err)
	}
}

// TestUpdateRejectsOffendingExcludeAndLeavesStoredRulesUnchanged asserts the
// Update path: an existing policy is on disk, the caller submits new rules
// that include an offending EXCLUDE, the Update rejects with
// FailedPrecondition, and the stored ConfigMap's rules annotation is NOT
// rewritten. This proves the guardrail runs BEFORE k8s.UpdatePolicy, so a
// rejected call cannot partially mutate state.
func TestUpdateRejectsOffendingExcludeAndLeavesStoredRulesUnchanged(t *testing.T) {
	// Seed an offending Deployment first.
	linkedJSON := mkLinkedTemplatesAnnotation(t, linkedTripleForTest{
		scope: v1alpha2.TemplateScopeOrganization, scopeName: "acme", name: "httproute",
	})
	client, _ := buildHol570Fixture(
		mkDeployment("holos-prj-lilies", "web", linkedJSON),
	)
	// Seed an initial policy via the K8s client directly — we want the
	// Update code path under test, not the Create path.
	initialRules := []byte(`[{"kind":"require","template":{"scope":"organization","scope_name":"acme","name":"audit"},"target":{"project_pattern":"*"}}]`)
	initial := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy",
			Namespace: "holos-org-acme",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplatePolicy,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationTemplatePolicyRules: string(initialRules),
				v1alpha2.AnnotationDisplayName:         "original",
				v1alpha2.AnnotationDescription:         "original description",
				v1alpha2.AnnotationCreatorEmail:        "seed@example.com",
			},
		},
	}
	if _, err := client.CoreV1().ConfigMaps("holos-org-acme").Create(context.Background(), initial, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seeding policy: %v", err)
	}

	h := newHol570HandlerFromClient(t, client)
	ctx := authedCtx("owner@example.com", nil)
	_, err := h.UpdateTemplatePolicy(ctx, connect.NewRequest(&consolev1.UpdateTemplatePolicyRequest{
		Scope: newOrgScope("acme"),
		Policy: &consolev1.TemplatePolicy{
			Name:     "policy",
			ScopeRef: newOrgScope("acme"),
			Rules: []*consolev1.TemplatePolicyRule{
				excludeRuleFor(orgTemplateRef("acme", "httproute"), "*", "*"),
			},
		},
	}))
	if err == nil {
		t.Fatal("expected Update to be rejected with FailedPrecondition")
	}
	if got := connect.CodeOf(err); got != connect.CodeFailedPrecondition {
		t.Fatalf("expected CodeFailedPrecondition, got %v: %v", got, err)
	}

	// The rejection MUST short-circuit before k8s.UpdatePolicy runs, so the
	// stored rules annotation stays byte-for-byte equal to the seed.
	after, err := client.CoreV1().ConfigMaps("holos-org-acme").Get(context.Background(), "policy", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("fetching stored policy: %v", err)
	}
	if got, want := after.Annotations[v1alpha2.AnnotationTemplatePolicyRules], string(initialRules); got != want {
		t.Errorf("rules annotation mutated by rejected Update:\n got: %s\nwant: %s", got, want)
	}
	if got, want := after.Annotations[v1alpha2.AnnotationDisplayName], "original"; got != want {
		t.Errorf("display name mutated by rejected Update: got %q want %q", got, want)
	}
}

// TestCreateAllowsExcludeWhenNoTopologyResolverWired guards the unit-test
// ergonomic carve-out: a Handler constructed without
// WithResourceTopologyResolver must accept EXCLUDE rules without error so
// tests that never exercise the guardrail do not need to stub the
// topology resolver. The default newTestHandler() helper used by the rest
// of the suite intentionally leaves topologyResolver unset, so this case
// documents that contract.
func TestCreateAllowsExcludeWhenNoTopologyResolverWired(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	// Explicitly do NOT call WithResourceTopologyResolver.

	ctx := authedCtx("owner@example.com", nil)
	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope: newOrgScope("acme"),
		Policy: &consolev1.TemplatePolicy{
			Name:     "optimistic-block",
			ScopeRef: newOrgScope("acme"),
			Rules: []*consolev1.TemplatePolicyRule{
				excludeRuleFor(orgTemplateRef("acme", "httproute"), "*", "*"),
			},
		},
	}))
	if err != nil {
		t.Fatalf("handler with no topology resolver must accept EXCLUDE: %v", err)
	}
}

// TestCreateRejectsExcludeAtFolderScope asserts the guardrail honors the
// policy's owning scope: a folder-scope policy must consider only projects
// under that folder (including nested folders). Here the offending
// Deployment lives under folder eng; a policy at folder eng MUST reject it,
// but a policy at folder team-a (a sibling leaf) would accept it because
// lilies is not under team-a. Demonstrates the ancestor-chain traversal
// correctly narrows to descendants.
func TestCreateRejectsExcludeAtFolderScope(t *testing.T) {
	linkedJSON := mkLinkedTemplatesAnnotation(t, linkedTripleForTest{
		scope: v1alpha2.TemplateScopeOrganization, scopeName: "acme", name: "httproute",
	})
	h := makeHol570Fixture(t,
		mkDeployment("holos-prj-lilies", "web", linkedJSON),
	)

	ctx := authedCtx("owner@example.com", nil)

	// Folder eng — lilies is under eng, so the rule conflicts.
	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope: newFolderScope("eng"),
		Policy: &consolev1.TemplatePolicy{
			Name:     "block-httproute",
			ScopeRef: newFolderScope("eng"),
			Rules: []*consolev1.TemplatePolicyRule{
				excludeRuleFor(orgTemplateRef("acme", "httproute"), "*", "*"),
			},
		},
	}))
	if err == nil {
		t.Fatal("expected folder-eng policy to reject EXCLUDE conflicting with lilies/web")
	}
	if got := connect.CodeOf(err); got != connect.CodeFailedPrecondition {
		t.Fatalf("expected CodeFailedPrecondition, got %v", got)
	}

	// Folder team-a — lilies is NOT under team-a, so the rule must be
	// accepted.
	_, err = h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope: newFolderScope("team-a"),
		Policy: &consolev1.TemplatePolicy{
			Name:     "block-httproute",
			ScopeRef: newFolderScope("team-a"),
			Rules: []*consolev1.TemplatePolicyRule{
				excludeRuleFor(orgTemplateRef("acme", "httproute"), "*", "*"),
			},
		},
	}))
	if err != nil {
		t.Fatalf("folder-team-a policy should not see lilies/web: %v", err)
	}
}
