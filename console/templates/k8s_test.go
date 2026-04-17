package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

func testResolver() *resolver.Resolver {
	return &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
}

func projectNS(project string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prj-" + project,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      project,
			},
		},
	}
}

func orgNS(org string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "org-" + org,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
				v1alpha2.LabelOrganization: org,
			},
		},
	}
}

// templateConfigMap builds a v1alpha2-labeled template ConfigMap for tests.
func templateConfigMap(scope consolev1.TemplateScope, scopePrefix, scopeName, name, displayName, description, cueTemplate string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: scopePrefix + scopeName,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: scopeLabelValue(scope),
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: displayName,
				v1alpha2.AnnotationDescription: description,
				v1alpha2.AnnotationEnabled:     "false",
			},
		},
		Data: map[string]string{
			CueTemplateKey: cueTemplate,
		},
	}
}

func projectTemplateConfigMap(project, name, displayName, description, cueTemplate string) *corev1.ConfigMap {
	return templateConfigMap(consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT, "prj-", project, name, displayName, description, cueTemplate)
}

// orgTemplateConfigMap builds a test fixture for an organization-scope
// template. The first boolean parameter was the now-deleted "mandatory"
// annotation (HOL-565); callers pass a value that is ignored so existing test
// call sites continue to compile.
func orgTemplateConfigMap(org, name, displayName, description, cueTemplate string, _ bool, enabled bool) *corev1.ConfigMap {
	cm := templateConfigMap(consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, "org-", org, name, displayName, description, cueTemplate)
	cm.Annotations[v1alpha2.AnnotationEnabled] = boolStr(enabled)
	return cm
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

var projectScope = consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT
var orgScope = consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION
var folderScope = consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER

func folderNS(folder string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fld-" + folder,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelFolder:       folder,
			},
		},
	}
}

// folderTemplateConfigMap builds a test fixture for a folder-scope template.
// The first boolean parameter was the now-deleted "mandatory" annotation
// (HOL-565); callers pass a value that is ignored so existing test call
// sites continue to compile.
func folderTemplateConfigMap(folder, name, displayName, description, cueTemplate string, _ bool, enabled bool) *corev1.ConfigMap {
	cm := templateConfigMap(consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, "fld-", folder, name, displayName, description, cueTemplate)
	cm.Annotations[v1alpha2.AnnotationEnabled] = boolStr(enabled)
	return cm
}

func TestListTemplates(t *testing.T) {
	t.Run("returns empty list when no templates exist", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListTemplates(context.Background(), projectScope, "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 0 {
			t.Errorf("expected 0 templates, got %d", len(cms))
		}
	})

	t.Run("returns templates with correct label", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := projectTemplateConfigMap("my-project", "web-app", "Web App", "A web application template", "#Input: {}\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListTemplates(context.Background(), projectScope, "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 1 {
			t.Fatalf("expected 1 template, got %d", len(cms))
		}
		if cms[0].Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", cms[0].Name)
		}
	})

	t.Run("lists org-scoped templates", func(t *testing.T) {
		ns := orgNS("acme")
		cm := orgTemplateConfigMap("acme", "ref-grant", "ReferenceGrant", "desc", "#Input: {}\n", false, true)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListTemplates(context.Background(), orgScope, "acme")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 1 {
			t.Fatalf("expected 1 org template, got %d", len(cms))
		}
		if cms[0].Name != "ref-grant" {
			t.Errorf("expected name 'ref-grant', got %q", cms[0].Name)
		}
	})
}

func TestGetTemplate(t *testing.T) {
	t.Run("returns existing project template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := projectTemplateConfigMap("my-project", "web-app", "Web App", "A web app", "#Input: {}\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		result, err := k8s.GetTemplate(context.Background(), projectScope, "my-project", "web-app")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result.Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", result.Name)
		}
		if result.Data[CueTemplateKey] != "#Input: {}\n" {
			t.Errorf("expected cue template content, got %q", result.Data[CueTemplateKey])
		}
	})

	t.Run("returns error for nonexistent template", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		_, err := k8s.GetTemplate(context.Background(), projectScope, "my-project", "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestCreateTemplate(t *testing.T) {
	t.Run("creates project template with correct labels", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateTemplate(context.Background(), projectScope, "my-project", "web-app", "Web App", "A web app", "#Input: {}\n", nil, false, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
			t.Error("expected managed-by label")
		}
		if cm.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeTemplate {
			t.Errorf("expected resource-type %q, got %q", v1alpha2.ResourceTypeTemplate, cm.Labels[v1alpha2.LabelResourceType])
		}
		if cm.Labels[v1alpha2.LabelTemplateScope] != v1alpha2.TemplateScopeProject {
			t.Errorf("expected template-scope %q, got %q", v1alpha2.TemplateScopeProject, cm.Labels[v1alpha2.LabelTemplateScope])
		}
		if cm.Annotations[v1alpha2.AnnotationDisplayName] != "Web App" {
			t.Errorf("expected display name 'Web App', got %q", cm.Annotations[v1alpha2.AnnotationDisplayName])
		}
		if cm.Data[CueTemplateKey] != "#Input: {}\n" {
			t.Errorf("expected cue template content, got %q", cm.Data[CueTemplateKey])
		}

		// Verify it was persisted.
		got, err := fakeClient.CoreV1().ConfigMaps("prj-my-project").Get(context.Background(), "web-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected ConfigMap to exist, got %v", err)
		}
		if got.Data[CueTemplateKey] != "#Input: {}\n" {
			t.Errorf("expected persisted cue template, got %q", got.Data[CueTemplateKey])
		}
	})

	t.Run("creates org template with enabled flag", func(t *testing.T) {
		ns := orgNS("acme")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateTemplate(context.Background(), orgScope, "acme", "ref-grant", "ReferenceGrant", "desc", "#Input: {}\n", nil, true, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Labels[v1alpha2.LabelTemplateScope] != v1alpha2.TemplateScopeOrganization {
			t.Errorf("expected org scope label, got %q", cm.Labels[v1alpha2.LabelTemplateScope])
		}
		if cm.Annotations[v1alpha2.AnnotationEnabled] != "true" {
			t.Errorf("expected enabled=true, got %q", cm.Annotations[v1alpha2.AnnotationEnabled])
		}
		// Verify it was stored in org namespace.
		got, err := fakeClient.CoreV1().ConfigMaps("org-acme").Get(context.Background(), "ref-grant", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected ConfigMap in org namespace, got %v", err)
		}
		if got.Namespace != "org-acme" {
			t.Errorf("expected namespace 'org-acme', got %q", got.Namespace)
		}
	})

	t.Run("creates template with defaults stored as JSON", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		defaults := &consolev1.TemplateDefaults{
			Image: "ghcr.io/mccutchen/go-httpbin",
			Tag:   "2.21",
		}
		cm, err := k8s.CreateTemplate(context.Background(), projectScope, "my-project", "web-app", "Web App", "A web app", "#Input: {}\n", defaults, false, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		rawJSON, ok := cm.Data[DefaultsKey]
		if !ok {
			t.Fatalf("expected %q key in ConfigMap data", DefaultsKey)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(rawJSON), &got); err != nil {
			t.Fatalf("defaults.json is not valid JSON: %v", err)
		}
		if got["image"] != "ghcr.io/mccutchen/go-httpbin" {
			t.Errorf("expected image, got %v", got["image"])
		}
	})

	t.Run("creates template without defaults omits defaults.json key", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateTemplate(context.Background(), projectScope, "my-project", "web-app", "Web App", "A web app", "#Input: {}\n", nil, false, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := cm.Data[DefaultsKey]; ok {
			t.Errorf("expected no %q key when defaults is nil", DefaultsKey)
		}
	})
}

func TestUpdateTemplate(t *testing.T) {
	t.Run("updates display name only", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := projectTemplateConfigMap("my-project", "web-app", "Web App", "A web app", "#Input: {}\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newName := "Updated Web App"
		updated, err := k8s.UpdateTemplate(context.Background(), projectScope, "my-project", "web-app", &newName, nil, nil, nil, false, nil, nil, false)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Annotations[v1alpha2.AnnotationDisplayName] != "Updated Web App" {
			t.Errorf("expected updated display name, got %q", updated.Annotations[v1alpha2.AnnotationDisplayName])
		}
		if updated.Annotations[v1alpha2.AnnotationDescription] != "A web app" {
			t.Errorf("expected unchanged description, got %q", updated.Annotations[v1alpha2.AnnotationDescription])
		}
	})

	t.Run("updates cue template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := projectTemplateConfigMap("my-project", "web-app", "Web App", "A web app", "#Input: {}\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newTemplate := "#Input: { name: string }\n"
		updated, err := k8s.UpdateTemplate(context.Background(), projectScope, "my-project", "web-app", nil, nil, &newTemplate, nil, false, nil, nil, false)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Data[CueTemplateKey] != newTemplate {
			t.Errorf("expected updated template, got %q", updated.Data[CueTemplateKey])
		}
	})

	t.Run("updates enabled flag on org template", func(t *testing.T) {
		ns := orgNS("acme")
		cm := orgTemplateConfigMap("acme", "ref-grant", "ReferenceGrant", "desc", "#Input: {}\n", false, false)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		enabled := true
		updated, err := k8s.UpdateTemplate(context.Background(), orgScope, "acme", "ref-grant", nil, nil, nil, nil, false, &enabled, nil, false)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Annotations[v1alpha2.AnnotationEnabled] != "true" {
			t.Errorf("expected enabled=true, got %q", updated.Annotations[v1alpha2.AnnotationEnabled])
		}
	})

	t.Run("returns error for nonexistent template", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		newName := "Updated"
		_, err := k8s.UpdateTemplate(context.Background(), projectScope, "my-project", "nonexistent", &newName, nil, nil, nil, false, nil, nil, false)
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestDeleteTemplate(t *testing.T) {
	t.Run("deletes existing template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := projectTemplateConfigMap("my-project", "web-app", "Web App", "A web app", "#Input: {}\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.DeleteTemplate(context.Background(), projectScope, "my-project", "web-app")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		_, err = fakeClient.CoreV1().ConfigMaps("prj-my-project").Get(context.Background(), "web-app", metav1.GetOptions{})
		if err == nil {
			t.Fatal("expected ConfigMap to be deleted")
		}
	})

	t.Run("returns error for nonexistent template", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.DeleteTemplate(context.Background(), projectScope, "my-project", "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestCloneTemplate(t *testing.T) {
	t.Run("copies CUE template and description from source", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := projectTemplateConfigMap("my-project", "web-app", "Web App", "A web app template", "foo: true\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		cloned, err := k8s.CloneTemplate(context.Background(), projectScope, "my-project", "web-app", "web-app-copy", "Web App Copy")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cloned.Name != "web-app-copy" {
			t.Errorf("expected name 'web-app-copy', got %q", cloned.Name)
		}
		if cloned.Annotations[v1alpha2.AnnotationDisplayName] != "Web App Copy" {
			t.Errorf("expected display name 'Web App Copy', got %q", cloned.Annotations[v1alpha2.AnnotationDisplayName])
		}
		if cloned.Annotations[v1alpha2.AnnotationDescription] != "A web app template" {
			t.Errorf("expected description from source, got %q", cloned.Annotations[v1alpha2.AnnotationDescription])
		}
		if cloned.Data[CueTemplateKey] != "foo: true\n" {
			t.Errorf("expected CUE template from source, got %q", cloned.Data[CueTemplateKey])
		}
	})

	t.Run("returns error when source does not exist", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		_, err := k8s.CloneTemplate(context.Background(), projectScope, "my-project", "nonexistent", "copy", "Copy")
		if err == nil {
			t.Fatal("expected error when source does not exist")
		}
	})
}

func TestLinkedTemplatesAnnotation(t *testing.T) {
	t.Run("CreateTemplate stores linked refs as JSON", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		linked := []*consolev1.LinkedTemplateRef{
			{Scope: orgScope, ScopeName: "acme", Name: "httproute"},
			{Scope: orgScope, ScopeName: "acme", Name: "policy-floor"},
		}
		cm, err := k8s.CreateTemplate(context.Background(), projectScope, "my-project", "web-app", "Web App", "desc", "#Input: {}\n", nil, false, linked)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		raw, ok := cm.Annotations[v1alpha2.AnnotationLinkedTemplates]
		if !ok {
			t.Fatal("expected linked-templates annotation")
		}
		// Parse back via unmarshalLinkedTemplates.
		refs, err := unmarshalLinkedTemplates(raw)
		if err != nil {
			t.Fatalf("failed to parse annotation: %v", err)
		}
		if len(refs) != 2 {
			t.Fatalf("expected 2 refs, got %d", len(refs))
		}
		if refs[0].Name != "httproute" {
			t.Errorf("expected 'httproute', got %q", refs[0].Name)
		}
	})

	t.Run("CreateTemplate with nil linked list omits annotation", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateTemplate(context.Background(), projectScope, "my-project", "web-app", "Web App", "desc", "#Input: {}\n", nil, false, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := cm.Annotations[v1alpha2.AnnotationLinkedTemplates]; ok {
			t.Error("expected no linked-templates annotation when list is nil")
		}
	})
}

// folderLinkedRefWithConstraint builds a folder-scope LinkedTemplateRef with a version constraint.
func folderLinkedRefWithConstraint(folder, name, constraint string) *consolev1.LinkedTemplateRef {
	return &consolev1.LinkedTemplateRef{
		Scope:             consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER,
		ScopeName:         folder,
		Name:              name,
		VersionConstraint: constraint,
	}
}

// stubHierarchyWalker implements RenderHierarchyWalker for testing
// ListEffectiveTemplateSources.
type stubHierarchyWalker struct {
	ancestors []*corev1.Namespace
	err       error
}

func (s *stubHierarchyWalker) WalkAncestors(_ context.Context, _ string) ([]*corev1.Namespace, error) {
	return s.ancestors, s.err
}

// TestListEffectiveTemplateSources exercises the unified ancestor-source helper
// that replaced the three legacy List*TemplateSourcesForRender helpers in
// HOL-564 (Phase 2 of HOL-562). The helper is the single render-time seam for
// effective template resolution across every render path — preview and apply,
// deployments and project-scope templates — so all tests here assert
// identical slices are produced regardless of the TargetKind passed in.
func TestListEffectiveTemplateSources(t *testing.T) {
	orgNsObj := orgNS("my-org")
	fldNsObj := folderNS("payments")
	prjNsObj := projectNS("my-project")
	fullAncestors := []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj}

	t.Run("nil walker returns no sources (no fallback path)", func(t *testing.T) {
		fakeClient := fake.NewClientset(orgNsObj)
		k8s := NewK8sClient(fakeClient, testResolver())

		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", nil, nil, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Errorf("expected 0 sources with nil walker, got %d", len(sources))
		}
	})

	t.Run("folder-only linked refs resolves from folder namespace", func(t *testing.T) {
		folderCue := "// folder payments policy"
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", folderCue, false, true)
		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: folderScope, ScopeName: "payments", Name: "payments-policy"},
		}
		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(sources))
		}
		if sources[0] != folderCue {
			t.Errorf("expected %q, got %q", folderCue, sources[0])
		}
	})

	t.Run("mixed org+folder linked refs resolves from both namespaces", func(t *testing.T) {
		orgCue := "// org httproute"
		orgCM := orgTemplateConfigMap("my-org", "httproute", "HTTPRoute", "", orgCue, false, true)
		folderCue := "// folder payments policy"
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", folderCue, false, true)
		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, orgCM, fldCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: orgScope, ScopeName: "my-org", Name: "httproute"},
			{Scope: folderScope, ScopeName: "payments", Name: "payments-policy"},
		}
		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 2 {
			t.Fatalf("expected 2 sources, got %d", len(sources))
		}
	})

	// HOL-565 removed the (mandatory AND enabled) branch from
	// ListEffectiveTemplateSources: templates that must always participate in
	// a render now come in via TemplatePolicy REQUIRE rules (HOL-567) and
	// are injected by the caller as explicit refs. The assertion therefore
	// flips — a folder template that used to be forced onto every deployment
	// by its `mandatory` annotation is now only included when the caller
	// explicitly links it.
	t.Run("folder template with legacy mandatory annotation is NOT auto-included", func(t *testing.T) {
		mandatoryCue := "// mandatory folder template"
		// The first boolean is now ignored by folderTemplateConfigMap; passing
		// true documents the pre-HOL-565 intent.
		fldCM := folderTemplateConfigMap("payments", "audit-policy", "Audit Policy", "", mandatoryCue, true, true)
		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", nil, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected 0 sources after HOL-565 removed mandatory auto-inclusion, got %d", len(sources))
		}
	})

	t.Run("disabled folder template excluded even when linked", func(t *testing.T) {
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", "// disabled", false, false)
		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: folderScope, ScopeName: "payments", Name: "payments-policy"},
		}
		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected 0 sources for disabled template, got %d", len(sources))
		}
	})

	t.Run("version-constrained folder linked ref resolved from release", func(t *testing.T) {
		liveCue := "// live folder template"
		releaseCue := "// folder release 1.0.0"
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", liveCue, false, true)

		v, _ := ParseVersion("1.0.0")
		releaseCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ReleaseConfigMapName("payments-policy", v),
				Namespace: "fld-payments",
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplateRelease,
					v1alpha2.LabelReleaseOf:     "payments-policy",
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationTemplateVersion: "1.0.0",
				},
			},
			Data: map[string]string{CueTemplateKey: releaseCue},
		}
		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM, releaseCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		refs := []*consolev1.LinkedTemplateRef{
			folderLinkedRefWithConstraint("payments", "payments-policy", ">=1.0.0"),
		}
		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(sources))
		}
		if sources[0] != releaseCue {
			t.Errorf("expected release CUE %q, got %q", releaseCue, sources[0])
		}
	})

	t.Run("walker failure degrades gracefully with empty sources", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{err: fmt.Errorf("walk failed")}

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: folderScope, ScopeName: "payments", Name: "payments-policy"},
		}
		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("expected graceful degradation, got error: %v", err)
		}
		if len(sources) != 0 {
			t.Errorf("expected 0 sources on walker failure, got %d", len(sources))
		}
	})

	t.Run("no linked refs and no mandatory templates returns empty", func(t *testing.T) {
		// Non-mandatory, enabled, but not linked.
		fldCM := folderTemplateConfigMap("payments", "optional", "Optional", "", "// optional", false, true)
		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", nil, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Errorf("expected 0 sources, got %d", len(sources))
		}
	})

	// Dedup regression test: the legacy ListOrgTemplateSourcesForRender
	// deduplicated by template name alone, so a folder template named "foo"
	// and an org template named "foo" would collide and one source would be
	// dropped. The unified helper deduplicates by (scope, scopeName, name),
	// so both survive. Guards HOL-564.
	t.Run("dedup key is (scope, scopeName, name) across scopes", func(t *testing.T) {
		sharedName := "shared"
		orgCue := "// org shared"
		folderCue := "// folder shared"
		orgCM := orgTemplateConfigMap("my-org", sharedName, "OrgShared", "", orgCue, false, true)
		fldCM := folderTemplateConfigMap("payments", sharedName, "FolderShared", "", folderCue, false, true)
		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, orgCM, fldCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: orgScope, ScopeName: "my-org", Name: sharedName},
			{Scope: folderScope, ScopeName: "payments", Name: sharedName},
		}
		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 2 {
			t.Fatalf("expected 2 sources (both scopes of the same name), got %d", len(sources))
		}
		got := map[string]bool{sources[0]: true, sources[1]: true}
		if !got[orgCue] || !got[folderCue] {
			t.Errorf("expected both org and folder sources, got %v", sources)
		}
	})

	// Structural invariant HOL-564 establishes: every TargetKind that travels
	// through the helper must yield an identical source slice for the same
	// inputs. Phase 4 will make TargetKind load-bearing for policy evaluation;
	// until then, callers on the preview path (project templates) and callers
	// on the apply path (deployments) cannot drift.
	t.Run("TargetKind does not alter resolution in Phase 2", func(t *testing.T) {
		orgCue := "// org httproute"
		orgCM := orgTemplateConfigMap("my-org", "httproute", "HTTPRoute", "", orgCue, false, true)
		folderCue := "// folder payments policy"
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", folderCue, true, true)
		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, orgCM, fldCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: orgScope, ScopeName: "my-org", Name: "httproute"},
		}

		deploymentSources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error (deployment): %v", err)
		}
		projectSources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindProjectTemplate, "tmpl", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error (project template): %v", err)
		}
		if len(deploymentSources) != len(projectSources) {
			t.Fatalf("preview-vs-apply slice length drift: deployment=%d projectTemplate=%d", len(deploymentSources), len(projectSources))
		}
		for i := range deploymentSources {
			if deploymentSources[i] != projectSources[i] {
				t.Errorf("preview-vs-apply drift at index %d: %q vs %q", i, deploymentSources[i], projectSources[i])
			}
		}
	})
}
