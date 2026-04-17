package policyresolver

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// testFixture builds a canonical hierarchy used by the table tests below.
// The layout:
//
//	acme (organization)
//	└── eng (folder)
//	    └── team-a (folder)
//	        └── web (project)
//	        └── web2 (project)
//
// Each level gets its own namespace and the AnnotationParent label is set so
// resolver.Walker.WalkAncestors can climb. Policy rules are attached by
// helper functions so each test can declare its own rules cleanly.
func testFixture() (*resolver.Resolver, *resolver.Walker, *fake.Clientset) {
	r := &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
	orgNs := r.OrgNamespace("acme")
	engNs := r.FolderNamespace("eng")
	teamNs := r.FolderNamespace("team-a")
	webNs := r.ProjectNamespace("web")
	web2Ns := r.ProjectNamespace("web2")

	client := fake.NewClientset(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   orgNs,
				Labels: map[string]string{v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization},
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: engNs,
				Labels: map[string]string{
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
					v1alpha2.AnnotationParent:  orgNs,
				},
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: teamNs,
				Labels: map[string]string{
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
					v1alpha2.AnnotationParent:  engNs,
				},
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: webNs,
				Labels: map[string]string{
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
					v1alpha2.AnnotationParent:  teamNs,
				},
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: web2Ns,
				Labels: map[string]string{
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
					v1alpha2.AnnotationParent:  teamNs,
				},
			},
		},
	)
	walker := &resolver.Walker{Client: client, Resolver: r}
	return r, walker, client
}

// newPolicyCM returns a TemplatePolicy ConfigMap pre-seeded with a rules
// annotation. The test fixture uses this helper to attach policies to the
// fake clientset at arbitrary scopes.
func newPolicyCM(ns, name string, rules []storedRuleWire) *corev1.ConfigMap {
	raw, _ := json.Marshal(rules)
	return &corev1.ConfigMap{
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
	}
}

// storedRuleWire mirrors the JSON shape used by
// console/templatepolicies/k8s.go so the resolver's consumer and producer
// agree on the exact bytes without importing each other's packages.
type storedRuleWire struct {
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

func makeRule(kind, templateScope, templateScopeName, templateName, projectPattern, deploymentPattern string) storedRuleWire {
	var r storedRuleWire
	r.Kind = kind
	r.Template.Scope = templateScope
	r.Template.ScopeName = templateScopeName
	r.Template.Name = templateName
	r.Target.ProjectPattern = projectPattern
	r.Target.DeploymentPattern = deploymentPattern
	return r
}

func linkedRef(scope consolev1.TemplateScope, scopeName, name string) *consolev1.LinkedTemplateRef {
	return &consolev1.LinkedTemplateRef{
		Scope:     scope,
		ScopeName: scopeName,
		Name:      name,
	}
}

func refKeys(refs []*consolev1.LinkedTemplateRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.GetScope().String()+"/"+ref.GetScopeName()+"/"+ref.GetName())
	}
	return out
}

// TestResolveScenarios exercises the resolver against the documented matrix
// of policy combinations. Each scenario runs twice — once with
// TargetKindProjectTemplate and once with TargetKindDeployment — to lock in
// the HOL-557 criterion that both target kinds see identical behavior when
// patterns match.
func TestResolveScenarios(t *testing.T) {
	type scenario struct {
		name          string
		baseLinks     []*consolev1.LinkedTemplateRef
		policies      []*corev1.ConfigMap
		wantRefKeys   []string
		project       string
		targetName    string
	}

	r, walker, _ := testFixture()
	orgNs := r.OrgNamespace("acme")
	engNs := r.FolderNamespace("eng")
	teamNs := r.FolderNamespace("team-a")
	webNs := r.ProjectNamespace("web")

	// Common refs used across scenarios.
	orgRefGrant := linkedRef(consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, "acme", "reference-grant")
	orgFluentBit := linkedRef(consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, "acme", "fluent-bit")
	folderIstio := linkedRef(consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, "eng", "istio-gateway")

	scenarios := []scenario{
		{
			name:        "no-policies-baseline-passthrough",
			baseLinks:   []*consolev1.LinkedTemplateRef{orgRefGrant},
			wantRefKeys: []string{"TEMPLATE_SCOPE_ORGANIZATION/acme/reference-grant"},
			project:     "web",
			targetName:  "app",
		},
		{
			name:      "require-only-adds-ancestor",
			baseLinks: nil,
			policies: []*corev1.ConfigMap{
				newPolicyCM(orgNs, "require-ref-grant", []storedRuleWire{
					makeRule("require", "organization", "acme", "reference-grant", "*", ""),
				}),
			},
			wantRefKeys: []string{"TEMPLATE_SCOPE_ORGANIZATION/acme/reference-grant"},
			project:     "web",
			targetName:  "app",
		},
		{
			name: "exclude-only-no-baseline-no-require",
			baseLinks: nil,
			policies: []*corev1.ConfigMap{
				newPolicyCM(orgNs, "exclude-ref-grant", []storedRuleWire{
					makeRule("exclude", "organization", "acme", "reference-grant", "*", ""),
				}),
			},
			wantRefKeys: []string{},
			project:     "web",
			targetName:  "app",
		},
		{
			name:      "require-plus-exclude-cancels",
			baseLinks: nil,
			policies: []*corev1.ConfigMap{
				newPolicyCM(orgNs, "require-ref-grant", []storedRuleWire{
					makeRule("require", "organization", "acme", "reference-grant", "*", ""),
				}),
				newPolicyCM(engNs, "exclude-ref-grant", []storedRuleWire{
					makeRule("exclude", "organization", "acme", "reference-grant", "*", ""),
				}),
			},
			wantRefKeys: []string{},
			project:     "web",
			targetName:  "app",
		},
		{
			name:      "multi-folder-hierarchy-merges",
			baseLinks: nil,
			policies: []*corev1.ConfigMap{
				newPolicyCM(orgNs, "org-require", []storedRuleWire{
					makeRule("require", "organization", "acme", "reference-grant", "*", ""),
				}),
				newPolicyCM(engNs, "folder-require", []storedRuleWire{
					makeRule("require", "folder", "eng", "istio-gateway", "*", ""),
				}),
				newPolicyCM(teamNs, "team-require", []storedRuleWire{
					makeRule("require", "organization", "acme", "fluent-bit", "web", ""),
				}),
			},
			// Resolve walks ancestors child→parent (team-a first, then eng,
			// then org) so REQUIREs harvested at the closer ancestor land in
			// the effective set first. Callers that need a canonical
			// org→project order re-sort downstream; the resolver itself
			// preserves walk order so the result is deterministic.
			wantRefKeys: []string{
				"TEMPLATE_SCOPE_ORGANIZATION/acme/fluent-bit",
				"TEMPLATE_SCOPE_FOLDER/eng/istio-gateway",
				"TEMPLATE_SCOPE_ORGANIZATION/acme/reference-grant",
			},
			project:    "web",
			targetName: "app",
		},
		{
			name: "project-pattern-miss",
			baseLinks: nil,
			policies: []*corev1.ConfigMap{
				newPolicyCM(orgNs, "require-other", []storedRuleWire{
					makeRule("require", "organization", "acme", "reference-grant", "other-*", ""),
				}),
			},
			wantRefKeys: []string{},
			project:     "web",
			targetName:  "app",
		},
		{
			name: "require-equivalent-to-mandatory",
			baseLinks: []*consolev1.LinkedTemplateRef{folderIstio},
			policies: []*corev1.ConfigMap{
				// Org-scope REQUIRE with project_pattern=* and
				// deployment_pattern=* — the HOL-557 acceptance criterion
				// "REQUIRE with wildcard patterns matches the old mandatory
				// behavior."
				newPolicyCM(orgNs, "mandatory-equivalent", []storedRuleWire{
					makeRule("require", "organization", "acme", "reference-grant", "*", "*"),
				}),
			},
			wantRefKeys: []string{
				"TEMPLATE_SCOPE_FOLDER/eng/istio-gateway",
				"TEMPLATE_SCOPE_ORGANIZATION/acme/reference-grant",
			},
			project:    "web",
			targetName: "app",
		},
		{
			name:      "exclude-on-explicit-link-is-defense-in-depth-only",
			baseLinks: []*consolev1.LinkedTemplateRef{orgRefGrant},
			policies: []*corev1.ConfigMap{
				// Even if a bad policy slipped past the handler validator,
				// the resolver still refuses to remove an explicitly linked
				// template.
				newPolicyCM(orgNs, "bad-exclude", []storedRuleWire{
					makeRule("exclude", "organization", "acme", "reference-grant", "*", ""),
				}),
			},
			wantRefKeys: []string{"TEMPLATE_SCOPE_ORGANIZATION/acme/reference-grant"},
			project:     "web",
			targetName:  "app",
		},
		{
			name:      "require-deduplicates-with-baseline",
			baseLinks: []*consolev1.LinkedTemplateRef{orgFluentBit},
			policies: []*corev1.ConfigMap{
				newPolicyCM(orgNs, "duplicate-require", []storedRuleWire{
					makeRule("require", "organization", "acme", "fluent-bit", "*", ""),
				}),
			},
			wantRefKeys: []string{"TEMPLATE_SCOPE_ORGANIZATION/acme/fluent-bit"},
			project:     "web",
			targetName:  "app",
		},
	}

	for _, tt := range scenarios {
		for _, kind := range []TargetKind{TargetKindProjectTemplate, TargetKindDeployment} {
			kind := kind
			tt := tt
			t.Run(tt.name+"_"+kind.String(), func(t *testing.T) {
				_, walker, client := testFixture()
				_ = webNs // keep webNs referenced so future rules can land there
				for _, cm := range tt.policies {
					if _, err := client.CoreV1().ConfigMaps(cm.Namespace).Create(context.Background(), cm, metav1.CreateOptions{}); err != nil {
						t.Fatalf("seeding policy: %v", err)
					}
				}
				res := &Resolver{Client: client, Walker: walker, Resolver: r}
				got, err := res.Resolve(
					context.Background(),
					consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
					tt.project,
					kind,
					tt.targetName,
					tt.baseLinks,
				)
				if err != nil {
					t.Fatalf("Resolve: %v", err)
				}
				gotKeys := refKeys(got)
				if len(gotKeys) != len(tt.wantRefKeys) {
					t.Fatalf("want %d refs %v, got %d %v", len(tt.wantRefKeys), tt.wantRefKeys, len(gotKeys), gotKeys)
				}
				for i, want := range tt.wantRefKeys {
					if gotKeys[i] != want {
						t.Errorf("ref[%d]: want %q, got %q (full: %v)", i, want, gotKeys[i], gotKeys)
					}
				}
			})
		}
	}
	_ = walker // suppress unused-variable warning in case the loop above shadows the outer walker
}

// TestResolveSkipsProjectNamespacePolicies is the linchpin of the
// storage-isolation invariant: a TemplatePolicy ConfigMap planted in a
// project namespace MUST NOT influence the resolver's output.
func TestResolveSkipsProjectNamespacePolicies(t *testing.T) {
	r, walker, client := testFixture()
	projectNs := r.ProjectNamespace("web")

	// Plant a malicious policy directly in the project namespace.
	badCM := newPolicyCM(projectNs, "bad-policy", []storedRuleWire{
		makeRule("require", "organization", "acme", "reference-grant", "*", ""),
	})
	if _, err := client.CoreV1().ConfigMaps(projectNs).Create(context.Background(), badCM, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seeding rogue policy: %v", err)
	}

	res := &Resolver{Client: client, Walker: walker, Resolver: r}
	got, err := res.Resolve(
		context.Background(),
		consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		"web",
		TargetKindDeployment,
		"app",
		nil,
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("project-namespace policy leaked: want 0 refs, got %v", refKeys(got))
	}
}

// TestDiffRefsAndHasDrift pins the diff helper's behavior so callers (the
// PolicyState RPCs) can rely on stable output ordering.
func TestDiffRefsAndHasDrift(t *testing.T) {
	a := linkedRef(consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, "acme", "reference-grant")
	b := linkedRef(consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, "acme", "fluent-bit")
	c := linkedRef(consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, "eng", "istio-gateway")

	applied := []*consolev1.LinkedTemplateRef{a, b}
	current := []*consolev1.LinkedTemplateRef{a, c}

	if !HasDrift(applied, current) {
		t.Fatal("expected drift between applied={a,b} and current={a,c}")
	}
	added, removed := DiffRefs(applied, current)
	if len(added) != 1 || added[0].GetName() != "istio-gateway" {
		t.Errorf("added: want [istio-gateway], got %v", refKeys(added))
	}
	if len(removed) != 1 || removed[0].GetName() != "fluent-bit" {
		t.Errorf("removed: want [fluent-bit], got %v", refKeys(removed))
	}

	// Same set, different order → no drift (keys are the only thing that
	// matters).
	unordered := []*consolev1.LinkedTemplateRef{b, a}
	if HasDrift(applied, unordered) {
		t.Fatal("want no drift for set-equal slices")
	}
}
