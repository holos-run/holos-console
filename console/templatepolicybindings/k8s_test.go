package templatepolicybindings

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
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

// TestNamespaceForScopeRejectsProject is the core guardrail for HOL-554:
// bindings must never resolve to a project namespace. The table covers the
// two valid scopes and the project scope that must always fail.
func TestNamespaceForScopeRejectsProject(t *testing.T) {
	k := newTestK8s()
	tests := []struct {
		name      string
		scope     scopeshim.Scope
		scopeName string
		wantErr   bool
		wantNs    string
	}{
		{
			name:      "org scope resolves to org namespace",
			scope:     scopeshim.ScopeOrganization,
			scopeName: "acme",
			wantNs:    "holos-org-acme",
		},
		{
			name:      "folder scope resolves to folder namespace",
			scope:     scopeshim.ScopeFolder,
			scopeName: "payments",
			wantNs:    "holos-fld-payments",
		},
		{
			name:      "project scope is rejected as ProjectNamespaceError",
			scope:     scopeshim.ScopeProject,
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

// TestCreateBindingRejectsProjectNamespace locks in the folder-only-storage
// invariant: a CreateBinding call targeting a project scope must fail
// before it ever touches the Kubernetes API. The fake clientset records
// every request, so we can also assert no ConfigMap was created in the
// project namespace as a belt-and-suspenders check.
func TestCreateBindingRejectsProjectNamespace(t *testing.T) {
	fakeClient := fake.NewClientset()
	k := NewK8sClient(fakeClient, newTestResolver())

	_, err := k.CreateBinding(
		context.Background(),
		scopeshim.ScopeProject,
		"billing-web",
		"binding-test",
		"Test",
		"",
		"creator@example.com",
		samplePolicyRef(),
		[]*consolev1.TemplatePolicyBindingTargetRef{sampleTargetRef()},
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

func TestUpdateBindingRejectsProjectNamespace(t *testing.T) {
	k := newTestK8s()
	_, err := k.UpdateBinding(
		context.Background(),
		scopeshim.ScopeProject,
		"billing-web",
		"binding-test",
		nil, nil, nil, false, nil, false,
	)
	if err == nil {
		t.Fatal("expected project-namespace rejection")
	}
	var pne *ProjectNamespaceError
	if !errors.As(err, &pne) {
		t.Fatalf("expected ProjectNamespaceError, got %T", err)
	}
}

func TestDeleteBindingRejectsProjectNamespace(t *testing.T) {
	k := newTestK8s()
	err := k.DeleteBinding(
		context.Background(),
		scopeshim.ScopeProject,
		"billing-web",
		"binding-test",
	)
	if err == nil {
		t.Fatal("expected project-namespace rejection")
	}
	var pne *ProjectNamespaceError
	if !errors.As(err, &pne) {
		t.Fatalf("expected ProjectNamespaceError, got %T", err)
	}
}

func TestListBindingsRejectsProjectNamespace(t *testing.T) {
	k := newTestK8s()
	_, err := k.ListBindings(
		context.Background(),
		scopeshim.ScopeProject,
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

func TestGetBindingRejectsProjectNamespace(t *testing.T) {
	k := newTestK8s()
	_, err := k.GetBinding(
		context.Background(),
		scopeshim.ScopeProject,
		"billing-web",
		"binding-test",
	)
	if err == nil {
		t.Fatal("expected project-namespace rejection on get")
	}
	var pne *ProjectNamespaceError
	if !errors.As(err, &pne) {
		t.Fatalf("expected ProjectNamespaceError, got %T", err)
	}
}

// TestCreateBindingWritesConfigMap verifies the happy path: a create at
// folder scope produces a ConfigMap with the managed-by /
// template-policy-binding labels and JSON policy-ref + target-refs
// annotations that round-trip via the package unmarshal helpers.
func TestCreateBindingWritesConfigMap(t *testing.T) {
	fakeClient := fake.NewClientset()
	k := NewK8sClient(fakeClient, newTestResolver())

	policy := samplePolicyRef()
	targets := []*consolev1.TemplatePolicyBindingTargetRef{sampleTargetRef()}
	cm, err := k.CreateBinding(
		context.Background(),
		scopeshim.ScopeFolder,
		"payments",
		"bind-reference-grant",
		"Bind reference grant",
		"Attach reference-grant to payments web deployments",
		"creator@example.com",
		policy,
		targets,
	)
	if err != nil {
		t.Fatalf("CreateBinding: %v", err)
	}
	if cm.Namespace != "holos-fld-payments" {
		t.Errorf("expected namespace holos-fld-payments, got %q", cm.Namespace)
	}
	if cm.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
		t.Errorf("expected managed-by label, got %q", cm.Labels[v1alpha2.LabelManagedBy])
	}
	if cm.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeTemplatePolicyBinding {
		t.Errorf("expected resource-type=template-policy-binding, got %q", cm.Labels[v1alpha2.LabelResourceType])
	}
	if cm.Labels[v1alpha2.LabelTemplateScope] != v1alpha2.TemplateScopeFolder {
		t.Errorf("expected scope label 'folder', got %q", cm.Labels[v1alpha2.LabelTemplateScope])
	}
	if cm.Annotations[v1alpha2.AnnotationCreatorEmail] != "creator@example.com" {
		t.Errorf("creator annotation missing: %q", cm.Annotations[v1alpha2.AnnotationCreatorEmail])
	}

	rawPolicy := cm.Annotations[v1alpha2.AnnotationTemplatePolicyBindingPolicyRef]
	if rawPolicy == "" {
		t.Fatal("expected non-empty policy-ref annotation")
	}
	parsedPolicy, err := unmarshalPolicyRef(rawPolicy)
	if err != nil {
		t.Fatalf("round-tripping policy-ref annotation: %v", err)
	}
	if parsedPolicy.GetName() != "require-http-route" {
		t.Errorf("expected policy name require-http-route, got %q", parsedPolicy.GetName())
	}
	if scopeshim.PolicyRefScope(parsedPolicy) != scopeshim.ScopeOrganization {
		t.Errorf("expected org scope, got %v", scopeshim.PolicyRefScope(parsedPolicy))
	}
	if scopeshim.PolicyRefScopeName(parsedPolicy) != "acme" {
		t.Errorf("expected policy scope name acme, got %q", scopeshim.PolicyRefScopeName(parsedPolicy))
	}

	rawTargets := cm.Annotations[v1alpha2.AnnotationTemplatePolicyBindingTargetRefs]
	if rawTargets == "" {
		t.Fatal("expected non-empty target-refs annotation")
	}
	parsedTargets, err := unmarshalTargetRefs(rawTargets)
	if err != nil {
		t.Fatalf("round-tripping target-refs annotation: %v", err)
	}
	if len(parsedTargets) != 1 {
		t.Fatalf("expected 1 target after round trip, got %d", len(parsedTargets))
	}
	if parsedTargets[0].GetKind() != consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT {
		t.Errorf("expected DEPLOYMENT kind, got %v", parsedTargets[0].GetKind())
	}
	if parsedTargets[0].GetProjectName() != "payments-web" {
		t.Errorf("expected project payments-web, got %q", parsedTargets[0].GetProjectName())
	}
	if parsedTargets[0].GetName() != "api" {
		t.Errorf("expected target name api, got %q", parsedTargets[0].GetName())
	}
}

// TestUpdateBindingPreservesExistingAnnotations confirms partial updates
// keep unspecified fields intact; this is the property relied on by the
// handler when the UI sends a display-only or target-only update.
func TestUpdateBindingPreservesExistingAnnotations(t *testing.T) {
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "binding",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplatePolicyBinding,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:                     "Existing Name",
				v1alpha2.AnnotationDescription:                     "Existing Desc",
				v1alpha2.AnnotationCreatorEmail:                    "creator@example.com",
				v1alpha2.AnnotationTemplatePolicyBindingPolicyRef:  `{"scope":"organization","scopeName":"acme","name":"require-http-route"}`,
				v1alpha2.AnnotationTemplatePolicyBindingTargetRefs: `[]`,
			},
		},
	}
	fakeClient := fake.NewClientset(existing)
	k := NewK8sClient(fakeClient, newTestResolver())

	// Update only target-refs; display name, description, and policy-ref
	// should remain intact.
	updated, err := k.UpdateBinding(
		context.Background(),
		scopeshim.ScopeFolder,
		"payments",
		"binding",
		nil, nil,
		nil, false,
		[]*consolev1.TemplatePolicyBindingTargetRef{sampleTargetRef()}, true,
	)
	if err != nil {
		t.Fatalf("UpdateBinding: %v", err)
	}
	if updated.Annotations[v1alpha2.AnnotationDisplayName] != "Existing Name" {
		t.Errorf("display name clobbered: %q", updated.Annotations[v1alpha2.AnnotationDisplayName])
	}
	if updated.Annotations[v1alpha2.AnnotationDescription] != "Existing Desc" {
		t.Errorf("description clobbered: %q", updated.Annotations[v1alpha2.AnnotationDescription])
	}
	if updated.Annotations[v1alpha2.AnnotationTemplatePolicyBindingPolicyRef] != `{"scope":"organization","scopeName":"acme","name":"require-http-route"}` {
		t.Errorf("policy-ref clobbered: %q", updated.Annotations[v1alpha2.AnnotationTemplatePolicyBindingPolicyRef])
	}
	rawTargets := updated.Annotations[v1alpha2.AnnotationTemplatePolicyBindingTargetRefs]
	targets, err := unmarshalTargetRefs(rawTargets)
	if err != nil {
		t.Fatalf("unmarshalTargetRefs: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected targets to be replaced with 1 entry, got %d", len(targets))
	}
}

// TestUpdateBindingPolicyRef verifies the policy-ref update path swaps the
// stored annotation without disturbing other fields.
func TestUpdateBindingPolicyRef(t *testing.T) {
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "binding",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplatePolicyBinding,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:                     "Name",
				v1alpha2.AnnotationTemplatePolicyBindingPolicyRef:  `{"scope":"organization","scopeName":"acme","name":"old-policy"}`,
				v1alpha2.AnnotationTemplatePolicyBindingTargetRefs: `[]`,
			},
		},
	}
	fakeClient := fake.NewClientset(existing)
	k := NewK8sClient(fakeClient, newTestResolver())

	newRef := scopeshim.NewLinkedTemplatePolicyRef(scopeshim.ScopeFolder, "payments", "new-policy")
	updated, err := k.UpdateBinding(
		context.Background(),
		scopeshim.ScopeFolder,
		"payments",
		"binding",
		nil, nil,
		newRef, true,
		nil, false,
	)
	if err != nil {
		t.Fatalf("UpdateBinding: %v", err)
	}
	parsed, err := unmarshalPolicyRef(updated.Annotations[v1alpha2.AnnotationTemplatePolicyBindingPolicyRef])
	if err != nil {
		t.Fatalf("unmarshalPolicyRef: %v", err)
	}
	if parsed.GetName() != "new-policy" {
		t.Errorf("expected policy name new-policy after update, got %q", parsed.GetName())
	}
	if scopeshim.PolicyRefScope(parsed) != scopeshim.ScopeFolder {
		t.Errorf("expected scope folder after update, got %v", scopeshim.PolicyRefScope(parsed))
	}
	if updated.Annotations[v1alpha2.AnnotationTemplatePolicyBindingTargetRefs] != `[]` {
		t.Errorf("target-refs clobbered: %q", updated.Annotations[v1alpha2.AnnotationTemplatePolicyBindingTargetRefs])
	}
}

// TestListBindingsReturnsManagedBindings verifies the label selector on
// List matches only TemplatePolicyBinding ConfigMaps and ignores other
// resources in the same namespace (templates, policies, etc.).
func TestListBindingsReturnsManagedBindings(t *testing.T) {
	ns := "holos-fld-payments"
	binding := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bind-a",
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicyBinding,
			},
		},
	}
	// A policy in the same namespace must NOT be returned.
	policy := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy-a",
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicy,
			},
		},
	}
	fakeClient := fake.NewClientset(binding, policy)
	k := NewK8sClient(fakeClient, newTestResolver())

	list, err := k.ListBindings(
		context.Background(),
		scopeshim.ScopeFolder,
		"payments",
	)
	if err != nil {
		t.Fatalf("ListBindings: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(list))
	}
	if list[0].Name != "bind-a" {
		t.Errorf("expected bind-a, got %q", list[0].Name)
	}
}

// TestListBindingsInNamespace verifies the namespace-direct variant skips
// scope resolution and refuses an empty namespace.
func TestListBindingsInNamespace(t *testing.T) {
	k := newTestK8s()
	if _, err := k.ListBindingsInNamespace(context.Background(), ""); err == nil {
		t.Fatal("expected error on empty namespace")
	}
	if _, err := k.ListBindingsInNamespace(context.Background(), "holos-fld-payments"); err != nil {
		t.Fatalf("unexpected error on populated namespace: %v", err)
	}
}

// TestPolicyRefAnnotationRoundtrip locks in the JSON wire format for the
// policy-ref annotation so external tooling (or future migrations) see a
// stable shape.
func TestPolicyRefAnnotationRoundtrip(t *testing.T) {
	ref := scopeshim.NewLinkedTemplatePolicyRef(scopeshim.ScopeFolder, "payments", "policy-a")
	raw, err := marshalPolicyRef(ref)
	if err != nil {
		t.Fatalf("marshalPolicyRef: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding JSON: %v", err)
	}
	if decoded["scope"] != "folder" {
		t.Errorf("expected scope=folder, got %v", decoded["scope"])
	}
	if decoded["scopeName"] != "payments" {
		t.Errorf("expected scopeName=payments, got %v", decoded["scopeName"])
	}
	if decoded["name"] != "policy-a" {
		t.Errorf("expected name=policy-a, got %v", decoded["name"])
	}
	parsed, err := unmarshalPolicyRef(string(raw))
	if err != nil {
		t.Fatalf("unmarshalPolicyRef: %v", err)
	}
	if scopeshim.PolicyRefScope(parsed) != scopeshim.ScopeFolder {
		t.Errorf("scope lost on round trip: %v", scopeshim.PolicyRefScope(parsed))
	}
}

// TestPolicyRefEmptyString verifies that the empty-string input decodes as
// a nil ref so callers can treat "no stored ref" as a validation failure.
func TestPolicyRefEmptyString(t *testing.T) {
	parsed, err := unmarshalPolicyRef("")
	if err != nil {
		t.Fatalf("unmarshalPolicyRef(\"\"): %v", err)
	}
	if parsed != nil {
		t.Errorf("expected nil for empty input, got %+v", parsed)
	}
}

// TestTargetRefsAnnotationRoundtrip locks in the JSON wire format for the
// target-refs annotation, including both PROJECT_TEMPLATE and DEPLOYMENT
// kinds and a project_name disambiguator.
func TestTargetRefsAnnotationRoundtrip(t *testing.T) {
	refs := []*consolev1.TemplatePolicyBindingTargetRef{
		{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE,
			Name:        "shared-service",
			ProjectName: "payments-web",
		},
		{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
			Name:        "api",
			ProjectName: "payments-web",
		},
	}
	raw, err := marshalTargetRefs(refs)
	if err != nil {
		t.Fatalf("marshalTargetRefs: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding JSON: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(decoded))
	}
	if decoded[0]["kind"] != "project-template" {
		t.Errorf("expected kind=project-template, got %v", decoded[0]["kind"])
	}
	if decoded[1]["kind"] != "deployment" {
		t.Errorf("expected kind=deployment, got %v", decoded[1]["kind"])
	}
	if decoded[0]["projectName"] != "payments-web" {
		t.Errorf("expected projectName=payments-web, got %v", decoded[0]["projectName"])
	}
	if decoded[1]["name"] != "api" {
		t.Errorf("expected name=api, got %v", decoded[1]["name"])
	}
	parsed, err := unmarshalTargetRefs(string(raw))
	if err != nil {
		t.Fatalf("unmarshalTargetRefs: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 parsed refs, got %d", len(parsed))
	}
	if parsed[0].GetKind() != consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE {
		t.Errorf("kind lost on round trip: %v", parsed[0].GetKind())
	}
}

// TestTargetRefsEmptyString verifies that the empty-string input decodes
// as nil so callers don't crash on freshly-initialized annotations.
func TestTargetRefsEmptyString(t *testing.T) {
	parsed, err := unmarshalTargetRefs("")
	if err != nil {
		t.Fatalf("unmarshalTargetRefs(\"\"): %v", err)
	}
	if parsed != nil {
		t.Errorf("expected nil for empty input, got %+v", parsed)
	}
}

// TestTargetRefsSkipsNilEntries ensures nil entries in the input slice do
// not produce JSON null entries in the serialized annotation.
func TestTargetRefsSkipsNilEntries(t *testing.T) {
	refs := []*consolev1.TemplatePolicyBindingTargetRef{
		nil,
		sampleTargetRef(),
		nil,
	}
	raw, err := marshalTargetRefs(refs)
	if err != nil {
		t.Fatalf("marshalTargetRefs: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding JSON: %v", err)
	}
	if len(decoded) != 1 {
		t.Errorf("expected nil entries to be skipped, got %d entries", len(decoded))
	}
}

// TestPackageDoesNotCallProjectNamespace is the grep-based regression test
// called out by the HOL-594 acceptance criteria. It walks every Go source
// file in this package and fails if any file references
// Resolver.ProjectNamespace. The test itself intentionally contains only
// the literal substring it searches for in this comment; bare references
// in other files would still be caught because the test excludes the test
// file itself from the search.
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

// samplePolicyRef returns a minimal valid policy ref suitable for fixtures.
func samplePolicyRef() *consolev1.LinkedTemplatePolicyRef {
	return scopeshim.NewLinkedTemplatePolicyRef(scopeshim.ScopeOrganization, "acme", "require-http-route")
}

// sampleTargetRef returns a minimal valid target ref suitable for fixtures.
func sampleTargetRef() *consolev1.TemplatePolicyBindingTargetRef {
	return &consolev1.TemplatePolicyBindingTargetRef{
		Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
		Name:        "api",
		ProjectName: "payments-web",
	}
}
