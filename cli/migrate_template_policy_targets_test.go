package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// testResolver returns the canonical default-prefix resolver the tests use.
// Keeping the construction in a helper lets the tests read like the
// production wire-up the CLI subcommand builds from flag values.
func testResolver() *resolver.Resolver {
	return &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
}

// testRule mirrors the JSON wire shape stored under
// AnnotationTemplatePolicyRules. The field names match the on-disk shape
// exactly so tests can fabricate fixtures without importing the unexported
// helpers in console/templatepolicies.
type testRule struct {
	Kind     string           `json:"kind"`
	Template testRuleTemplate `json:"template"`
	Target   testRuleTarget   `json:"target"`
}

type testRuleTemplate struct {
	Scope             string `json:"scope"`
	ScopeName         string `json:"scope_name"`
	Name              string `json:"name"`
	VersionConstraint string `json:"version_constraint,omitempty"`
}

type testRuleTarget struct {
	ProjectPattern    string `json:"project_pattern"`
	DeploymentPattern string `json:"deployment_pattern,omitempty"`
}

// fixtureOption mutates a fake-clientset fixture. Each test case declares
// the options it needs (namespaces, policies, deployments, templates,
// pre-existing bindings) so the table stays a compact data description of
// the scenario rather than a prose run-book.
type fixtureOption func(*fixtureBuilder)

// fixtureBuilder accumulates objects and is flushed to a fake.Clientset at
// the start of each subtest.
type fixtureBuilder struct {
	objects []runtime.Object
	r       *resolver.Resolver
	t       *testing.T
}

func newFixtureBuilder(t *testing.T) *fixtureBuilder {
	return &fixtureBuilder{r: testResolver(), t: t}
}

func (b *fixtureBuilder) build() *fake.Clientset {
	return fake.NewClientset(b.objects...)
}

// withOrgNamespace adds a managed organization namespace.
func withOrgNamespace(name string) fixtureOption {
	return func(b *fixtureBuilder) {
		b.objects = append(b.objects, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: b.r.OrgNamespace(name),
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
				},
			},
		})
	}
}

// withFolderNamespace adds a managed folder namespace whose parent label
// points at parent's namespace.
func withFolderNamespace(name, parent string) fixtureOption {
	return func(b *fixtureBuilder) {
		b.objects = append(b.objects, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: b.r.FolderNamespace(name),
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
					v1alpha2.AnnotationParent:  parentNamespace(b.r, parent),
					v1alpha2.LabelOrganization: rootOrgFromParent(parent),
				},
			},
		})
	}
}

// withProjectNamespace adds a managed project namespace whose parent label
// points at parent's namespace.
func withProjectNamespace(name, parent string) fixtureOption {
	return func(b *fixtureBuilder) {
		b.objects = append(b.objects, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: b.r.ProjectNamespace(name),
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
					v1alpha2.AnnotationParent:  parentNamespace(b.r, parent),
					v1alpha2.LabelOrganization: rootOrgFromParent(parent),
				},
			},
		})
	}
}

// parentNamespace resolves a parent reference by best-effort name lookup:
// a name matching a known organization slug becomes the org namespace; any
// other non-empty value becomes the folder namespace. The tests use
// distinct slugs across kinds so the ambiguity never arises in practice.
func parentNamespace(r *resolver.Resolver, parent string) string {
	switch parent {
	case "":
		return ""
	case "acme", "contoso", "globex":
		return r.OrgNamespace(parent)
	default:
		return r.FolderNamespace(parent)
	}
}

// rootOrgFromParent returns the organization slug associated with a parent
// reference. Used to label folder and project namespaces so the migrator's
// classification reflects production shape.
func rootOrgFromParent(parent string) string {
	switch parent {
	case "acme", "contoso", "globex":
		return parent
	default:
		return "acme"
	}
}

// withPolicy adds a TemplatePolicy ConfigMap in ns with the given rules.
func withPolicy(ns, name string, rules []testRule) fixtureOption {
	return func(b *fixtureBuilder) {
		raw, err := json.Marshal(rules)
		if err != nil {
			b.t.Fatalf("marshal rules: %v", err)
		}
		b.objects = append(b.objects, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicy,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationTemplatePolicyRules: string(raw),
				},
			},
		})
	}
}

// withProjectTemplate adds a project-scope Template ConfigMap. The migrator
// consumes these when a rule's deployment_pattern is empty.
func withProjectTemplate(ns, name string) fixtureOption {
	return func(b *fixtureBuilder) {
		b.objects = append(b.objects, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
				},
			},
		})
	}
}

// withDeployment adds a Deployment ConfigMap. The migrator consumes these
// when a rule's deployment_pattern matches.
func withDeployment(ns, name string) fixtureOption {
	return func(b *fixtureBuilder) {
		b.objects = append(b.objects, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeDeployment,
				},
			},
		})
	}
}

// withProjectNamespaceOrphan adds a managed project namespace whose parent
// label is missing. This is an ancestry-error fixture: the migrator must
// refuse to enumerate descendants for policies that could be affected by
// the broken chain rather than silently dropping the project.
func withProjectNamespaceOrphan(name string) fixtureOption {
	return func(b *fixtureBuilder) {
		b.objects = append(b.objects, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: b.r.ProjectNamespace(name),
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
					// No AnnotationParent — this is the
					// orphan we are testing against.
				},
			},
		})
	}
}

// withCollidingConfigMap adds a ConfigMap in ns whose name matches what
// the migrator synthesizes for a binding but whose resource-type label
// identifies it as something else (e.g. a leftover Template).
func withCollidingConfigMap(ns, name, resourceType string) fixtureOption {
	return func(b *fixtureBuilder) {
		b.objects = append(b.objects, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: resourceType,
				},
			},
		})
	}
}

// withTerminatingProjectNamespace adds a managed project namespace whose
// metadata.deletionTimestamp is non-nil — i.e. the namespace is in the
// process of being deleted. The migrator must skip such namespaces to
// mirror K8sResourceTopology.ListProjectsUnderScope (HOL-570).
func withTerminatingProjectNamespace(name, parent string) fixtureOption {
	return func(b *fixtureBuilder) {
		now := metav1.Now()
		b.objects = append(b.objects, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:              b.r.ProjectNamespace(name),
				DeletionTimestamp: &now,
				// Kubernetes requires a finalizer when
				// DeletionTimestamp is set; the fake client
				// does not enforce this, but a real cluster
				// would.
				Finalizers: []string{"kubernetes"},
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
					v1alpha2.AnnotationParent:  parentNamespace(b.r, parent),
					v1alpha2.LabelOrganization: rootOrgFromParent(parent),
				},
			},
		})
	}
}

// withBinding adds a pre-existing TemplatePolicyBinding ConfigMap. Used by
// the idempotency test to simulate "the migration already ran once".
func withBinding(ns, name, policyRefJSON, targetRefsJSON string) fixtureOption {
	return func(b *fixtureBuilder) {
		b.objects = append(b.objects, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicyBinding,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationTemplatePolicyBindingPolicyRef:  policyRefJSON,
					v1alpha2.AnnotationTemplatePolicyBindingTargetRefs: targetRefsJSON,
				},
			},
		})
	}
}

// wantTarget is a compact shape for asserting which render targets the
// migrator selected. Test tables declare it in an order-insensitive slice
// and the assertion helper normalizes both sides before comparing.
type wantTarget struct {
	Kind    consolev1.TemplatePolicyBindingTargetKind
	Name    string
	Project string
}

// sortTargets yields a stable order the assertion can compare byte-for-
// byte, eliminating flakiness caused by map iteration on either side.
func sortTargets(in []wantTarget) []wantTarget {
	out := append([]wantTarget(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// extractTargets reduces a plan's TargetRefs to the comparable wantTarget
// shape. Keeps the assertion decoupled from proto internals.
func extractTargets(refs []*consolev1.TemplatePolicyBindingTargetRef) []wantTarget {
	out := make([]wantTarget, 0, len(refs))
	for _, r := range refs {
		out = append(out, wantTarget{Kind: r.GetKind(), Name: r.GetName(), Project: r.GetProjectName()})
	}
	return out
}

// runArgs selects the mode MigrateTemplatePolicyTargets runs in.
type runArgs struct {
	apply bool
}

// assertions is the expectation block for a single migration pass. Hoisted
// to package scope so the helper functions below can take a value receiver
// instead of passing 5+ discrete parameters around.
type assertions struct {
	bindingsCreated int
	policiesUpdated int
	skipped         int
	conflicts       int
	wantPlans       int
	wantTargets     map[string][]wantTarget // binding full path -> expected targets
	// wantNoBindingIn lists (namespace, name) pairs we must not find
	// after the run. Used by the dry-run row to assert zero mutations.
	wantNoBindingIn [][2]string
	// wantPolicyCleared lists (namespace, name) pairs whose Target globs
	// must be empty after the run. Used to pin the "clear after
	// migration" AC.
	wantPolicyCleared [][2]string
	// wantPolicyUnchanged lists (namespace, name) pairs whose Target
	// globs must still be non-empty after the run (dry-run and conflict
	// rows).
	wantPolicyUnchanged [][2]string
}

// TestMigrateTemplatePolicyTargets is the single table-driven test that
// exercises every acceptance-criterion scenario from HOL-599:
//
//   - Empty cluster → no bindings written.
//   - Policy with one deployment_pattern matching two deployments across
//     two projects → one binding with two target_refs.
//   - Idempotency: a second run finds an existing binding with matching
//     targets and writes nothing new.
//
// A handful of additional rows cover the behavioral edges the main scenarios
// do not exercise (empty deployment_pattern selecting both kinds, dry-run
// mutating nothing, conflict detection).
func TestMigrateTemplatePolicyTargets(t *testing.T) {
	r := testResolver()

	tests := []struct {
		name    string
		options []fixtureOption
		run     runArgs
		// twoRuns triggers a second apply pass with the same fixture
		// after the first pass mutates it. Used by the idempotency row.
		twoRuns bool
		// wantError asserts the first pass returns an error; use
		// errorMatch to pin the error message substring. When set, no
		// assertions run against the (nil) MigrationResult, but the
		// cluster-state assertions on `first` still run so tests can
		// confirm nothing was mutated before the error surfaced.
		wantError  bool
		errorMatch string
		// first describes the first-pass expectations. Every scenario
		// has exactly one first-pass assertion. Idempotent scenarios
		// add a second.
		first assertions
		// second describes the expected state after a second apply
		// pass, when twoRuns is true. Both passes operate on the same
		// fake client.
		second assertions
	}{
		{
			name:    "empty cluster writes no bindings",
			options: nil,
			run:     runArgs{apply: true},
			first: assertions{
				bindingsCreated: 0,
				policiesUpdated: 0,
				skipped:         0,
				conflicts:       0,
				wantPlans:       0,
			},
		},
		{
			name: "policy with one deployment_pattern matching two deployments across two projects",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withProjectNamespace("lilies", "acme"),
				withProjectNamespace("roses", "acme"),
				withDeployment(r.ProjectNamespace("lilies"), "api"),
				withDeployment(r.ProjectNamespace("roses"), "api"),
				// A deployment that should NOT match by name.
				withDeployment(r.ProjectNamespace("lilies"), "worker"),
				withPolicy(r.OrgNamespace("acme"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "*", DeploymentPattern: "api"},
					},
				}),
			},
			run: runArgs{apply: true},
			first: assertions{
				bindingsCreated: 1,
				policiesUpdated: 1,
				wantPlans:       1,
				wantTargets: map[string][]wantTarget{
					r.OrgNamespace("acme") + "/audit" + migrateBindingNameSuffix: {
						{Kind: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT, Name: "api", Project: "lilies"},
						{Kind: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT, Name: "api", Project: "roses"},
					},
				},
				wantPolicyCleared: [][2]string{{r.OrgNamespace("acme"), "audit"}},
			},
		},
		{
			name: "idempotency: second run skips policies already cleared",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withProjectNamespace("lilies", "acme"),
				withDeployment(r.ProjectNamespace("lilies"), "api"),
				withPolicy(r.OrgNamespace("acme"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "*", DeploymentPattern: "api"},
					},
				}),
			},
			run:     runArgs{apply: true},
			twoRuns: true,
			first: assertions{
				bindingsCreated: 1,
				policiesUpdated: 1,
				wantPlans:       1,
				wantTargets: map[string][]wantTarget{
					r.OrgNamespace("acme") + "/audit" + migrateBindingNameSuffix: {
						{Kind: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT, Name: "api", Project: "lilies"},
					},
				},
				wantPolicyCleared: [][2]string{{r.OrgNamespace("acme"), "audit"}},
			},
			second: assertions{
				bindingsCreated: 0,
				policiesUpdated: 0,
				skipped:         1,
				wantPlans:       0,
			},
		},
		{
			name: "dry-run plans but does not mutate",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withProjectNamespace("lilies", "acme"),
				withDeployment(r.ProjectNamespace("lilies"), "api"),
				withPolicy(r.OrgNamespace("acme"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "*", DeploymentPattern: "api"},
					},
				}),
			},
			run: runArgs{apply: false},
			first: assertions{
				bindingsCreated:     0,
				policiesUpdated:     0,
				wantPlans:           1,
				wantNoBindingIn:     [][2]string{{r.OrgNamespace("acme"), "audit" + migrateBindingNameSuffix}},
				wantPolicyUnchanged: [][2]string{{r.OrgNamespace("acme"), "audit"}},
			},
		},
		{
			name: "empty deployment_pattern matches both project templates and deployments",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withProjectNamespace("lilies", "acme"),
				withDeployment(r.ProjectNamespace("lilies"), "api"),
				withProjectTemplate(r.ProjectNamespace("lilies"), "stack"),
				withPolicy(r.OrgNamespace("acme"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "lilies", DeploymentPattern: ""},
					},
				}),
			},
			run: runArgs{apply: true},
			first: assertions{
				bindingsCreated: 1,
				policiesUpdated: 1,
				wantPlans:       1,
				wantTargets: map[string][]wantTarget{
					r.OrgNamespace("acme") + "/audit" + migrateBindingNameSuffix: {
						{Kind: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT, Name: "api", Project: "lilies"},
						{Kind: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE, Name: "stack", Project: "lilies"},
					},
				},
			},
		},
		{
			name: "folder scope only selects descendant projects",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withFolderNamespace("eng", "acme"),
				withProjectNamespace("lilies", "eng"),
				withProjectNamespace("orchids", "acme"),
				withDeployment(r.ProjectNamespace("lilies"), "api"),
				withDeployment(r.ProjectNamespace("orchids"), "api"),
				withPolicy(r.FolderNamespace("eng"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeFolder, ScopeName: "eng", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "*", DeploymentPattern: "api"},
					},
				}),
			},
			run: runArgs{apply: true},
			first: assertions{
				bindingsCreated: 1,
				policiesUpdated: 1,
				wantPlans:       1,
				wantTargets: map[string][]wantTarget{
					r.FolderNamespace("eng") + "/audit" + migrateBindingNameSuffix: {
						{Kind: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT, Name: "api", Project: "lilies"},
					},
				},
			},
		},
		{
			name: "policy with already-empty targets is skipped",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withProjectNamespace("lilies", "acme"),
				withPolicy(r.OrgNamespace("acme"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "", DeploymentPattern: ""},
					},
				}),
			},
			run: runArgs{apply: true},
			first: assertions{
				bindingsCreated: 0,
				policiesUpdated: 0,
				skipped:         1,
				wantPlans:       0,
			},
		},
		{
			name: "existing binding with matching targets is left in place, policy still cleared",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withProjectNamespace("lilies", "acme"),
				withDeployment(r.ProjectNamespace("lilies"), "api"),
				withPolicy(r.OrgNamespace("acme"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "*", DeploymentPattern: "api"},
					},
				}),
				withBinding(
					r.OrgNamespace("acme"),
					"audit"+migrateBindingNameSuffix,
					`{"scope":"organization","scopeName":"acme","name":"audit"}`,
					`[{"kind":"deployment","name":"api","projectName":"lilies"}]`,
				),
			},
			run: runArgs{apply: true},
			first: assertions{
				bindingsCreated:   0,
				policiesUpdated:   1,
				wantPlans:         1,
				wantPolicyCleared: [][2]string{{r.OrgNamespace("acme"), "audit"}},
			},
		},
		{
			name: "existing binding with different targets is a conflict",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withProjectNamespace("lilies", "acme"),
				withDeployment(r.ProjectNamespace("lilies"), "api"),
				withPolicy(r.OrgNamespace("acme"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "*", DeploymentPattern: "api"},
					},
				}),
				withBinding(
					r.OrgNamespace("acme"),
					"audit"+migrateBindingNameSuffix,
					`{"scope":"organization","scopeName":"acme","name":"audit"}`,
					`[{"kind":"deployment","name":"worker","projectName":"lilies"}]`,
				),
			},
			run: runArgs{apply: true},
			first: assertions{
				bindingsCreated:     0,
				policiesUpdated:     0,
				conflicts:           1,
				wantPlans:           1,
				wantPolicyUnchanged: [][2]string{{r.OrgNamespace("acme"), "audit"}},
			},
		},
		{
			name: "two rules with disjoint targets produce a single binding with the union",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withProjectNamespace("lilies", "acme"),
				withDeployment(r.ProjectNamespace("lilies"), "api"),
				withDeployment(r.ProjectNamespace("lilies"), "worker"),
				withPolicy(r.OrgNamespace("acme"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "*", DeploymentPattern: "api"},
					},
					{
						Kind: "exclude",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "legacy-policy",
						},
						Target: testRuleTarget{ProjectPattern: "*", DeploymentPattern: "worker"},
					},
				}),
			},
			run: runArgs{apply: true},
			first: assertions{
				bindingsCreated: 1,
				policiesUpdated: 1,
				wantPlans:       1,
				wantTargets: map[string][]wantTarget{
					r.OrgNamespace("acme") + "/audit" + migrateBindingNameSuffix: {
						{Kind: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT, Name: "api", Project: "lilies"},
						{Kind: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT, Name: "worker", Project: "lilies"},
					},
				},
				wantPolicyCleared: [][2]string{{r.OrgNamespace("acme"), "audit"}},
			},
		},
		{
			// Codex round-1 review HOL-599: a policy whose globs
			// match no live render targets must not produce a
			// binding with target_refs=[] (the binding handler
			// rejects empty lists and the resulting artifact would
			// be uneditable). The migrator still clears the policy
			// globs because they matched nothing under the legacy
			// path either — clearing is semantics-preserving.
			name: "globs matching no targets skip binding creation but still clear policy",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withProjectNamespace("lilies", "acme"),
				withPolicy(r.OrgNamespace("acme"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "nonexistent", DeploymentPattern: "nothing"},
					},
				}),
			},
			run: runArgs{apply: true},
			first: assertions{
				bindingsCreated:   0,
				policiesUpdated:   1,
				wantPlans:         1,
				wantPolicyCleared: [][2]string{{r.OrgNamespace("acme"), "audit"}},
				wantNoBindingIn:   [][2]string{{r.OrgNamespace("acme"), "audit" + migrateBindingNameSuffix}},
			},
		},
		{
			// Codex round-2 review HOL-599: a ConfigMap named
			// "<policy>-migrated" that is NOT a
			// TemplatePolicyBinding (e.g. a leftover Template by
			// the same name) must not be confused for a pre-
			// existing binding. Without the resource-type check
			// the migrator would report a permanent conflict that
			// no binding-side operator action could resolve.
			name: "non-binding ConfigMap with the same name as the target binding is a conflict",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withProjectNamespace("lilies", "acme"),
				withDeployment(r.ProjectNamespace("lilies"), "api"),
				withPolicy(r.OrgNamespace("acme"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "*", DeploymentPattern: "api"},
					},
				}),
				withCollidingConfigMap(
					r.OrgNamespace("acme"),
					"audit"+migrateBindingNameSuffix,
					v1alpha2.ResourceTypeTemplate,
				),
			},
			run: runArgs{apply: true},
			first: assertions{
				bindingsCreated:     0,
				policiesUpdated:     0,
				conflicts:           1,
				wantPlans:           1,
				wantPolicyUnchanged: [][2]string{{r.OrgNamespace("acme"), "audit"}},
			},
		},
		{
			// Codex round-2 review HOL-599: a project namespace
			// whose parent label references a namespace not in
			// the managed index is an ancestry error. Silently
			// dropping the project would let the migrator clear
			// policy Target globs while skipping legitimate
			// bindings — a permanent coverage-loss path once
			// HOL-600 lands. Fail loudly so the operator fixes
			// ancestry before the migration proceeds.
			name: "broken parent label fails loudly instead of dropping the project",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withProjectNamespace("lilies", "acme"),
				withProjectNamespaceOrphan("roses"),
				withDeployment(r.ProjectNamespace("lilies"), "api"),
				withDeployment(r.ProjectNamespace("roses"), "api"),
				withPolicy(r.OrgNamespace("acme"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "*", DeploymentPattern: "api"},
					},
				}),
			},
			run:        runArgs{apply: true},
			wantError:  true,
			errorMatch: "parent annotation",
			first: assertions{
				// No assertions run when wantError is true.
				wantPolicyUnchanged: [][2]string{{r.OrgNamespace("acme"), "audit"}},
			},
		},
		{
			// Codex round-1 review HOL-599: a namespace with a
			// non-nil DeletionTimestamp is being reclaimed, so
			// K8sResourceTopology.ListProjectsUnderScope excludes
			// it. The migrator must match the topology contract so
			// it does not bind targets the legacy glob evaluation
			// path would never have activated.
			name: "terminating project namespaces are excluded from target enumeration",
			options: []fixtureOption{
				withOrgNamespace("acme"),
				withProjectNamespace("lilies", "acme"),
				withTerminatingProjectNamespace("roses", "acme"),
				withDeployment(r.ProjectNamespace("lilies"), "api"),
				// The terminating project still has a live
				// deployment ConfigMap — the migrator must
				// skip the whole project because the runtime
				// topology does, not cherry-pick its
				// deployments.
				withDeployment(r.ProjectNamespace("roses"), "api"),
				withPolicy(r.OrgNamespace("acme"), "audit", []testRule{
					{
						Kind: "require",
						Template: testRuleTemplate{
							Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "audit-policy",
						},
						Target: testRuleTarget{ProjectPattern: "*", DeploymentPattern: "api"},
					},
				}),
			},
			run: runArgs{apply: true},
			first: assertions{
				bindingsCreated: 1,
				policiesUpdated: 1,
				wantPlans:       1,
				wantTargets: map[string][]wantTarget{
					r.OrgNamespace("acme") + "/audit" + migrateBindingNameSuffix: {
						{Kind: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT, Name: "api", Project: "lilies"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newFixtureBuilder(t)
			for _, opt := range tt.options {
				opt(b)
			}
			client := b.build()

			var buf bytes.Buffer
			opts := MigrateTemplatePolicyTargetsOptions{
				Client:   client,
				Resolver: r,
				Apply:    tt.run.apply,
				Out:      &buf,
			}
			res, err := MigrateTemplatePolicyTargets(context.Background(), opts)
			if tt.wantError {
				if err == nil {
					t.Fatalf("migrate succeeded; wanted error matching %q", tt.errorMatch)
				}
				if tt.errorMatch != "" && !strings.Contains(err.Error(), tt.errorMatch) {
					t.Fatalf("migrate error = %q, want substring %q", err.Error(), tt.errorMatch)
				}
				// Cluster-state assertions still apply: an
				// error must not have left partial mutations
				// behind.
				assertClusterState(t, client, tt.first)
				return
			}
			if err != nil {
				t.Fatalf("migrate failed: %v", err)
			}
			assertResult(t, "first pass", res, tt.first)
			assertClusterState(t, client, tt.first)

			if !tt.twoRuns {
				return
			}
			res2, err := MigrateTemplatePolicyTargets(context.Background(), opts)
			if err != nil {
				t.Fatalf("second migrate failed: %v", err)
			}
			assertResult(t, "second pass", res2, tt.second)
			assertClusterState(t, client, tt.second)
		})
	}
}

// assertResult compares a MigrationResult against the expected counts and
// per-binding targets the row declared.
func assertResult(t *testing.T, label string, got *MigrationResult, want assertions) {
	t.Helper()
	if got.BindingsCreated != want.bindingsCreated {
		t.Errorf("%s: BindingsCreated = %d, want %d", label, got.BindingsCreated, want.bindingsCreated)
	}
	if got.PoliciesUpdated != want.policiesUpdated {
		t.Errorf("%s: PoliciesUpdated = %d, want %d", label, got.PoliciesUpdated, want.policiesUpdated)
	}
	if got.Skipped != want.skipped {
		t.Errorf("%s: Skipped = %d, want %d", label, got.Skipped, want.skipped)
	}
	if got.Conflicts != want.conflicts {
		t.Errorf("%s: Conflicts = %d, want %d", label, got.Conflicts, want.conflicts)
	}
	if len(got.Plans) != want.wantPlans {
		t.Errorf("%s: len(Plans) = %d, want %d", label, len(got.Plans), want.wantPlans)
	}
	for fullName, wantRefs := range want.wantTargets {
		plan := findPlanByFullName(got.Plans, fullName)
		if plan == nil {
			t.Errorf("%s: expected plan %q not found", label, fullName)
			continue
		}
		gotRefs := sortTargets(extractTargets(plan.TargetRefs))
		wantSorted := sortTargets(wantRefs)
		if len(gotRefs) != len(wantSorted) {
			t.Errorf("%s: plan %q target count = %d, want %d (got=%v want=%v)",
				label, fullName, len(gotRefs), len(wantSorted), gotRefs, wantSorted)
			continue
		}
		for i := range gotRefs {
			if gotRefs[i] != wantSorted[i] {
				t.Errorf("%s: plan %q target[%d] = %+v, want %+v", label, fullName, i, gotRefs[i], wantSorted[i])
			}
		}
	}
}

// findPlanByFullName locates a plan by "namespace/bindingName". Returns nil
// if no plan matches — the caller turns the nil into a test failure.
func findPlanByFullName(plans []*PolicyMigrationPlan, fullName string) *PolicyMigrationPlan {
	for _, plan := range plans {
		if plan.PolicyNamespace+"/"+plan.BindingName == fullName {
			return plan
		}
	}
	return nil
}

// assertClusterState verifies the mutations declared by wantPolicyCleared,
// wantPolicyUnchanged, and wantNoBindingIn match what is actually on the
// fake cluster after the run.
func assertClusterState(t *testing.T, client *fake.Clientset, want assertions) {
	t.Helper()
	ctx := context.Background()
	for _, pair := range want.wantPolicyCleared {
		ns, name := pair[0], pair[1]
		cm, err := client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Errorf("policy %s/%s: get failed: %v", ns, name, err)
			continue
		}
		raw := cm.Annotations[v1alpha2.AnnotationTemplatePolicyRules]
		if raw == "" {
			continue
		}
		var rules []testRule
		if err := json.Unmarshal([]byte(raw), &rules); err != nil {
			t.Errorf("policy %s/%s: unmarshal failed: %v", ns, name, err)
			continue
		}
		for i, rule := range rules {
			if rule.Target.ProjectPattern != "" || rule.Target.DeploymentPattern != "" {
				t.Errorf("policy %s/%s rule[%d]: Target still populated (%+v)", ns, name, i, rule.Target)
			}
		}
	}
	for _, pair := range want.wantPolicyUnchanged {
		ns, name := pair[0], pair[1]
		cm, err := client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Errorf("policy %s/%s: get failed: %v", ns, name, err)
			continue
		}
		raw := cm.Annotations[v1alpha2.AnnotationTemplatePolicyRules]
		if raw == "" {
			t.Errorf("policy %s/%s: expected unchanged rules but annotation is empty", ns, name)
			continue
		}
		var rules []testRule
		if err := json.Unmarshal([]byte(raw), &rules); err != nil {
			t.Errorf("policy %s/%s: unmarshal failed: %v", ns, name, err)
			continue
		}
		populated := false
		for _, rule := range rules {
			if rule.Target.ProjectPattern != "" || rule.Target.DeploymentPattern != "" {
				populated = true
				break
			}
		}
		if !populated {
			t.Errorf("policy %s/%s: Target globs unexpectedly cleared", ns, name)
		}
	}
	for _, pair := range want.wantNoBindingIn {
		ns, name := pair[0], pair[1]
		_, err := client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			t.Errorf("binding %s/%s: unexpectedly present after dry-run", ns, name)
		}
	}
}

// TestMigrateTemplatePolicyTargetsRejectsMissingDeps guards the small defensive
// checks in the entry point: callers that wire the migrator without a
// client, a resolver, or an output writer must be told explicitly rather
// than seeing a nil-deref in the middle of a partial run.
func TestMigrateTemplatePolicyTargetsRejectsMissingDeps(t *testing.T) {
	r := testResolver()
	var buf bytes.Buffer
	cases := []struct {
		name string
		opts MigrateTemplatePolicyTargetsOptions
	}{
		{
			name: "missing client",
			opts: MigrateTemplatePolicyTargetsOptions{Resolver: r, Out: &buf},
		},
		{
			name: "missing resolver",
			opts: MigrateTemplatePolicyTargetsOptions{Client: fake.NewClientset(), Out: &buf},
		},
		{
			name: "missing out writer",
			opts: MigrateTemplatePolicyTargetsOptions{Client: fake.NewClientset(), Resolver: r},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MigrateTemplatePolicyTargets(context.Background(), tt.opts)
			if err == nil {
				t.Fatal("expected error for missing dependency; got nil")
			}
		})
	}
}

// TestMigrateCommandRegistered confirms `migrate template-policy-targets`
// is wired into the root cobra tree. A subcommand that is not registered
// is invisible to operators, so the regression test pins the registration
// separately from the migrator's internal behavior.
func TestMigrateCommandRegistered(t *testing.T) {
	cmd := Command()
	migrate, _, err := cmd.Find([]string{"migrate"})
	if err != nil || migrate == nil {
		t.Fatalf("migrate subcommand not registered: %v", err)
	}
	sub, _, err := cmd.Find([]string{"migrate", "template-policy-targets"})
	if err != nil || sub == nil {
		t.Fatalf("migrate template-policy-targets subcommand not registered: %v", err)
	}
	if flag := sub.Flags().Lookup("apply"); flag == nil {
		t.Fatal("--apply flag not declared on migrate template-policy-targets")
	} else if flag.DefValue != "false" {
		t.Fatalf("--apply default = %q, want %q", flag.DefValue, "false")
	}
}
