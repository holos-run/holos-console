package templatepolicies

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	corev1 "k8s.io/api/core/v1"
)

func newTestResolver() *resolver.Resolver {
	return &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
}

func newTestK8s() *K8sClient {
	return NewK8sClient(fake.NewClientset(), newTestResolver())
}

func TestNamespaceForScopeRejectsProject(t *testing.T) {
	k := newTestK8s()
	tests := []struct {
		name      string
		scope     consolev1.TemplateScope
		scopeName string
		wantErr   bool
		wantNs    string
	}{
		{
			name:      "org scope resolves to org namespace",
			scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
			scopeName: "acme",
			wantNs:    "holos-org-acme",
		},
		{
			name:      "folder scope resolves to folder namespace",
			scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER,
			scopeName: "payments",
			wantNs:    "holos-fld-payments",
		},
		{
			name:      "project scope is rejected as ProjectNamespaceError",
			scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
			scopeName: "payments-web",
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns, err := k.namespaceForScope(tt.scope, tt.scopeName)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got namespace %q", ns)
				}
				var pne *ProjectNamespaceError
				if !errors.As(err, &pne) {
					t.Fatalf("expected ProjectNamespaceError, got %T: %v", err, err)
				}
				if pne.Namespace != "holos-prj-"+tt.scopeName {
					t.Errorf("expected offending namespace to include project ns, got %q", pne.Namespace)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ns != tt.wantNs {
				t.Errorf("expected namespace %q, got %q", tt.wantNs, ns)
			}
		})
	}
}

// TestCreatePolicyRejectsProjectNamespace locks in the folder-only-storage
// invariant: a CreatePolicy call targeting a project scope must fail before
// it ever touches the Kubernetes API. The fake clientset records every
// request, so we can also assert no ConfigMap was created in the project
// namespace as a belt-and-suspenders check.
func TestCreatePolicyRejectsProjectNamespace(t *testing.T) {
	fakeClient := fake.NewClientset()
	k := NewK8sClient(fakeClient, newTestResolver())

	_, err := k.CreatePolicy(
		context.Background(),
		consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		"billing-web",
		"policy-test",
		"Test",
		"",
		"creator@example.com",
		[]*consolev1.TemplatePolicyRule{sampleRule()},
	)
	if err == nil {
		t.Fatal("expected project-namespace rejection, got nil")
	}
	var pne *ProjectNamespaceError
	if !errors.As(err, &pne) {
		t.Fatalf("expected ProjectNamespaceError, got %T: %v", err, err)
	}
	if pne.Namespace != "holos-prj-billing-web" {
		t.Errorf("expected error to name the project namespace, got %q", pne.Namespace)
	}

	cms, listErr := fakeClient.CoreV1().ConfigMaps("holos-prj-billing-web").List(context.Background(), metav1.ListOptions{})
	if listErr != nil {
		t.Fatalf("listing project ns configmaps: %v", listErr)
	}
	if len(cms.Items) != 0 {
		t.Errorf("expected 0 configmaps created in project namespace, got %d", len(cms.Items))
	}
}

func TestUpdatePolicyRejectsProjectNamespace(t *testing.T) {
	k := newTestK8s()
	_, err := k.UpdatePolicy(
		context.Background(),
		consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		"billing-web",
		"policy-test",
		nil, nil, nil, false,
	)
	if err == nil {
		t.Fatal("expected project-namespace rejection")
	}
	var pne *ProjectNamespaceError
	if !errors.As(err, &pne) {
		t.Fatalf("expected ProjectNamespaceError, got %T", err)
	}
}

func TestDeletePolicyRejectsProjectNamespace(t *testing.T) {
	k := newTestK8s()
	err := k.DeletePolicy(
		context.Background(),
		consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		"billing-web",
		"policy-test",
	)
	if err == nil {
		t.Fatal("expected project-namespace rejection")
	}
	var pne *ProjectNamespaceError
	if !errors.As(err, &pne) {
		t.Fatalf("expected ProjectNamespaceError, got %T", err)
	}
}

func TestListPolicyRejectsProjectNamespace(t *testing.T) {
	k := newTestK8s()
	_, err := k.ListPolicies(
		context.Background(),
		consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		"billing-web",
	)
	if err == nil {
		t.Fatal("expected project-namespace rejection on list")
	}
	var pne *ProjectNamespaceError
	if !errors.As(err, &pne) {
		t.Fatalf("expected ProjectNamespaceError, got %T", err)
	}
}

func TestGetPolicyRejectsProjectNamespace(t *testing.T) {
	k := newTestK8s()
	_, err := k.GetPolicy(
		context.Background(),
		consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		"billing-web",
		"policy-test",
	)
	if err == nil {
		t.Fatal("expected project-namespace rejection on get")
	}
	var pne *ProjectNamespaceError
	if !errors.As(err, &pne) {
		t.Fatalf("expected ProjectNamespaceError, got %T", err)
	}
}

// TestCreatePolicyWritesConfigMap verifies the happy path: a create at folder
// scope produces a ConfigMap with the managed-by / template-policy labels and
// a JSON rules annotation that round-trips via unmarshalRules.
func TestCreatePolicyWritesConfigMap(t *testing.T) {
	fakeClient := fake.NewClientset()
	k := NewK8sClient(fakeClient, newTestResolver())

	rules := []*consolev1.TemplatePolicyRule{sampleRule()}
	cm, err := k.CreatePolicy(
		context.Background(),
		consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER,
		"payments",
		"require-httproute",
		"Require HTTPRoute",
		"Force reference-grant into every project",
		"creator@example.com",
		rules,
	)
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	if cm.Namespace != "holos-fld-payments" {
		t.Errorf("expected namespace holos-fld-payments, got %q", cm.Namespace)
	}
	if cm.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
		t.Errorf("expected managed-by label, got %q", cm.Labels[v1alpha2.LabelManagedBy])
	}
	if cm.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeTemplatePolicy {
		t.Errorf("expected resource-type=template-policy, got %q", cm.Labels[v1alpha2.LabelResourceType])
	}
	if cm.Labels[v1alpha2.LabelTemplateScope] != v1alpha2.TemplateScopeFolder {
		t.Errorf("expected scope label 'folder', got %q", cm.Labels[v1alpha2.LabelTemplateScope])
	}
	if cm.Annotations[v1alpha2.AnnotationCreatorEmail] != "creator@example.com" {
		t.Errorf("creator annotation missing: %q", cm.Annotations[v1alpha2.AnnotationCreatorEmail])
	}
	raw := cm.Annotations[v1alpha2.AnnotationTemplatePolicyRules]
	if raw == "" {
		t.Fatal("expected non-empty rules annotation")
	}
	parsed, err := unmarshalRules(raw)
	if err != nil {
		t.Fatalf("round-tripping rules annotation: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 rule after round trip, got %d", len(parsed))
	}
	if parsed[0].GetKind() != consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE {
		t.Errorf("expected REQUIRE, got %v", parsed[0].GetKind())
	}
	if parsed[0].GetTemplate().GetName() != "reference-grant" {
		t.Errorf("expected reference-grant template, got %q", parsed[0].GetTemplate().GetName())
	}
}

// TestUpdatePolicyPreservesExistingAnnotations confirms partial updates keep
// unspecified fields intact; this is the property relied on by the handler
// when the UI sends a rules-only update.
func TestUpdatePolicyPreservesExistingAnnotations(t *testing.T) {
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplatePolicy,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:         "Existing Name",
				v1alpha2.AnnotationDescription:         "Existing Desc",
				v1alpha2.AnnotationCreatorEmail:        "creator@example.com",
				v1alpha2.AnnotationTemplatePolicyRules: `[]`,
			},
		},
	}
	fakeClient := fake.NewClientset(existing)
	k := NewK8sClient(fakeClient, newTestResolver())

	updated, err := k.UpdatePolicy(
		context.Background(),
		consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER,
		"payments",
		"policy",
		nil, nil,
		[]*consolev1.TemplatePolicyRule{sampleRule()}, true,
	)
	if err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}
	if updated.Annotations[v1alpha2.AnnotationDisplayName] != "Existing Name" {
		t.Errorf("display name clobbered: %q", updated.Annotations[v1alpha2.AnnotationDisplayName])
	}
	if updated.Annotations[v1alpha2.AnnotationDescription] != "Existing Desc" {
		t.Errorf("description clobbered: %q", updated.Annotations[v1alpha2.AnnotationDescription])
	}
	raw := updated.Annotations[v1alpha2.AnnotationTemplatePolicyRules]
	rules, err := unmarshalRules(raw)
	if err != nil {
		t.Fatalf("unmarshalRules: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected rules to be replaced with 1 entry, got %d", len(rules))
	}
}

// TestRulesAnnotationRoundtrip verifies the JSON wire format for a rules
// annotation so external tooling (or future migrations) see a stable shape.
func TestRulesAnnotationRoundtrip(t *testing.T) {
	rules := []*consolev1.TemplatePolicyRule{
		{
			Kind: consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE,
			Template: &consolev1.LinkedTemplateRef{
				Scope:             consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
				ScopeName:         "acme",
				Name:              "reference-grant",
				VersionConstraint: ">=1.0",
			},
			Target: &consolev1.TemplatePolicyTarget{
				ProjectPattern:    "*",
				DeploymentPattern: "web-*",
			},
		},
	}
	raw, err := marshalRules(rules)
	if err != nil {
		t.Fatalf("marshalRules: %v", err)
	}
	// Assert a stable wire shape so we notice any accidental field rename.
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding JSON: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(decoded))
	}
	if decoded[0]["kind"] != "require" {
		t.Errorf("expected kind=require, got %v", decoded[0]["kind"])
	}
	tmpl, _ := decoded[0]["template"].(map[string]any)
	if tmpl["scope"] != "organization" || tmpl["name"] != "reference-grant" {
		t.Errorf("template wire shape changed: %+v", tmpl)
	}
	target, _ := decoded[0]["target"].(map[string]any)
	if target["project_pattern"] != "*" {
		t.Errorf("target wire shape changed: %+v", target)
	}
	// Round trip must yield semantically equal rules.
	parsed, err := unmarshalRules(string(raw))
	if err != nil {
		t.Fatalf("unmarshalRules: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 rule after round trip, got %d", len(parsed))
	}
	if parsed[0].GetTemplate().GetVersionConstraint() != ">=1.0" {
		t.Errorf("version constraint dropped on round trip")
	}
}

// TestPackageDoesNotCallProjectNamespace is the grep-based regression test
// called out by the HOL-556 acceptance criteria. It walks every Go source
// file in this package and fails if any file references
// Resolver.ProjectNamespace. The test itself intentionally contains only the
// literal substring it searches for in this comment; bare references in
// other files would still be caught because the test excludes the test
// file itself from the search.
//
// Why this is stricter than a lint rule: the check is scoped to this
// package, so a future refactor that needs project namespaces elsewhere is
// unaffected, but no one can quietly re-introduce project-scope storage
// here without the test failing.
func TestPackageDoesNotCallProjectNamespace(t *testing.T) {
	const target = "Resolver.ProjectNamespace"
	matches := []string{}
	err := filepath.Walk(".", func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip this test file since its doc comment references the exact
		// identifier being searched for.
		if strings.HasSuffix(path, "k8s_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), target) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking package sources: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("package must not call %s — found in: %v", target, matches)
	}
}

// sampleRule returns a minimal valid rule suitable for fixtures.
func sampleRule() *consolev1.TemplatePolicyRule {
	return &consolev1.TemplatePolicyRule{
		Kind: consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE,
		Template: &consolev1.LinkedTemplateRef{
			Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
			ScopeName: "acme",
			Name:      "reference-grant",
		},
		Target: &consolev1.TemplatePolicyTarget{
			ProjectPattern: "*",
		},
	}
}
