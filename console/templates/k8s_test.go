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
				v1alpha2.AnnotationMandatory:   "false",
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

func orgTemplateConfigMap(org, name, displayName, description, cueTemplate string, mandatory, enabled bool) *corev1.ConfigMap {
	cm := templateConfigMap(consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, "org-", org, name, displayName, description, cueTemplate)
	cm.Annotations[v1alpha2.AnnotationMandatory] = boolStr(mandatory)
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

func folderTemplateConfigMap(folder, name, displayName, description, cueTemplate string, mandatory, enabled bool) *corev1.ConfigMap {
	cm := templateConfigMap(consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, "fld-", folder, name, displayName, description, cueTemplate)
	cm.Annotations[v1alpha2.AnnotationMandatory] = boolStr(mandatory)
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

		cm, err := k8s.CreateTemplate(context.Background(), projectScope, "my-project", "web-app", "Web App", "A web app", "#Input: {}\n", nil, false, false, nil)
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

	t.Run("creates org template with mandatory and enabled flags", func(t *testing.T) {
		ns := orgNS("acme")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateTemplate(context.Background(), orgScope, "acme", "ref-grant", "ReferenceGrant", "desc", "#Input: {}\n", nil, true, true, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Labels[v1alpha2.LabelTemplateScope] != v1alpha2.TemplateScopeOrganization {
			t.Errorf("expected org scope label, got %q", cm.Labels[v1alpha2.LabelTemplateScope])
		}
		if cm.Annotations[v1alpha2.AnnotationMandatory] != "true" {
			t.Errorf("expected mandatory=true, got %q", cm.Annotations[v1alpha2.AnnotationMandatory])
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
		cm, err := k8s.CreateTemplate(context.Background(), projectScope, "my-project", "web-app", "Web App", "A web app", "#Input: {}\n", defaults, false, false, nil)
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

		cm, err := k8s.CreateTemplate(context.Background(), projectScope, "my-project", "web-app", "Web App", "A web app", "#Input: {}\n", nil, false, false, nil)
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
		updated, err := k8s.UpdateTemplate(context.Background(), projectScope, "my-project", "web-app", &newName, nil, nil, nil, false, nil, nil, nil, false)
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
		updated, err := k8s.UpdateTemplate(context.Background(), projectScope, "my-project", "web-app", nil, nil, &newTemplate, nil, false, nil, nil, nil, false)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Data[CueTemplateKey] != newTemplate {
			t.Errorf("expected updated template, got %q", updated.Data[CueTemplateKey])
		}
	})

	t.Run("updates mandatory and enabled flags on org template", func(t *testing.T) {
		ns := orgNS("acme")
		cm := orgTemplateConfigMap("acme", "ref-grant", "ReferenceGrant", "desc", "#Input: {}\n", false, false)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		mandatory := true
		enabled := true
		updated, err := k8s.UpdateTemplate(context.Background(), orgScope, "acme", "ref-grant", nil, nil, nil, nil, false, &mandatory, &enabled, nil, false)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Annotations[v1alpha2.AnnotationMandatory] != "true" {
			t.Errorf("expected mandatory=true, got %q", updated.Annotations[v1alpha2.AnnotationMandatory])
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
		_, err := k8s.UpdateTemplate(context.Background(), projectScope, "my-project", "nonexistent", &newName, nil, nil, nil, false, nil, nil, nil, false)
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
		cm, err := k8s.CreateTemplate(context.Background(), projectScope, "my-project", "web-app", "Web App", "desc", "#Input: {}\n", nil, false, false, linked)
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

		cm, err := k8s.CreateTemplate(context.Background(), projectScope, "my-project", "web-app", "Web App", "desc", "#Input: {}\n", nil, false, false, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := cm.Annotations[v1alpha2.AnnotationLinkedTemplates]; ok {
			t.Error("expected no linked-templates annotation when list is nil")
		}
	})
}

// orgLinkedRefWithConstraint is a helper to build an org-scope LinkedTemplateRef
// with a version constraint for tests.
func orgLinkedRefWithConstraint(org, name, constraint string) *consolev1.LinkedTemplateRef {
	return &consolev1.LinkedTemplateRef{
		Scope:             consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
		ScopeName:         org,
		Name:              name,
		VersionConstraint: constraint,
	}
}

func TestListOrgTemplateSourcesForRender(t *testing.T) {
	t.Run("mandatory+enabled template always included without linking", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := orgTemplateConfigMap("my-org", "policy", "Policy", "", "// policy", true, true)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(sources))
		}
		if sources[0] != "// policy" {
			t.Errorf("unexpected source: %q", sources[0])
		}
	})

	t.Run("non-mandatory enabled template NOT included when not linked", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := orgTemplateConfigMap("my-org", "archetype", "Archetype", "", "// archetype", false, true)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected 0 sources, got %d", len(sources))
		}
	})

	t.Run("non-mandatory enabled template included when explicitly linked", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := orgTemplateConfigMap("my-org", "archetype", "Archetype", "", "// archetype", false, true)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		refs := []*consolev1.LinkedTemplateRef{orgLinkedRef("my-org", "archetype")}
		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", refs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(sources))
		}
	})

	t.Run("disabled template not included even when linked", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := orgTemplateConfigMap("my-org", "disabled", "Disabled", "", "// disabled", false, false)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		refs := []*consolev1.LinkedTemplateRef{orgLinkedRef("my-org", "disabled")}
		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", refs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected 0 sources, got %d", len(sources))
		}
	})

	t.Run("linked template resolved from release when version constraint provided", func(t *testing.T) {
		ns := orgNS("my-org")
		liveCue := "// live source"
		releaseCue := "// release 1.0.0 source"
		cm := orgTemplateConfigMap("my-org", "httproute", "HTTPRoute", "", liveCue, false, true)
		// Create a release ConfigMap for version 1.0.0.
		v, _ := ParseVersion("1.0.0")
		releaseCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ReleaseConfigMapName("httproute", v),
				Namespace: "org-my-org",
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplateRelease,
					v1alpha2.LabelReleaseOf:     "httproute",
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationTemplateVersion: "1.0.0",
				},
			},
			Data: map[string]string{
				CueTemplateKey: releaseCue,
			},
		}
		fakeClient := fake.NewClientset(ns, cm, releaseCM)
		k8s := NewK8sClient(fakeClient, testResolver())

		refs := []*consolev1.LinkedTemplateRef{orgLinkedRefWithConstraint("my-org", "httproute", ">=1.0.0")}
		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", refs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(sources))
		}
		if sources[0] != releaseCue {
			t.Errorf("expected release CUE source %q, got %q", releaseCue, sources[0])
		}
	})

	t.Run("linked template falls back to live source when no releases exist", func(t *testing.T) {
		ns := orgNS("my-org")
		liveCue := "// live source no releases"
		cm := orgTemplateConfigMap("my-org", "httproute", "HTTPRoute", "", liveCue, false, true)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		refs := []*consolev1.LinkedTemplateRef{orgLinkedRefWithConstraint("my-org", "httproute", ">=1.0.0")}
		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", refs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(sources))
		}
		if sources[0] != liveCue {
			t.Errorf("expected live CUE source %q, got %q", liveCue, sources[0])
		}
	})

	t.Run("mandatory template uses live source not versioned resolution", func(t *testing.T) {
		ns := orgNS("my-org")
		liveCue := "// mandatory live"
		releaseCue := "// mandatory release"
		cm := orgTemplateConfigMap("my-org", "policy", "Policy", "", liveCue, true, true)
		// Create a release (should be ignored for mandatory non-linked templates).
		v, _ := ParseVersion("1.0.0")
		releaseCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ReleaseConfigMapName("policy", v),
				Namespace: "org-my-org",
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplateRelease,
					v1alpha2.LabelReleaseOf:     "policy",
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationTemplateVersion: "1.0.0",
				},
			},
			Data: map[string]string{
				CueTemplateKey: releaseCue,
			},
		}
		fakeClient := fake.NewClientset(ns, cm, releaseCM)
		k8s := NewK8sClient(fakeClient, testResolver())

		// No linked refs — mandatory template is included automatically.
		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(sources))
		}
		if sources[0] != liveCue {
			t.Errorf("expected live CUE source %q for mandatory template, got %q", liveCue, sources[0])
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

// stubHierarchyWalker implements HierarchyWalker for testing
// ListAncestorTemplateSourcesForRender.
type stubHierarchyWalker struct {
	ancestors []*corev1.Namespace
	err       error
}

func (s *stubHierarchyWalker) WalkAncestors(_ context.Context, _ string) ([]*corev1.Namespace, error) {
	return s.ancestors, s.err
}

func TestListAncestorTemplateSourcesForRender(t *testing.T) {
	t.Run("folder-only linked refs resolves sources from folder namespace", func(t *testing.T) {
		orgNsObj := orgNS("my-org")
		fldNsObj := folderNS("payments")
		prjNsObj := projectNS("my-project")

		folderCue := "// folder payments policy"
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", folderCue, false, true)

		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{
			ancestors: []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj},
		}

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: folderScope, ScopeName: "payments", Name: "payments-policy"},
		}
		sources, err := k8s.ListAncestorTemplateSourcesForRender(context.Background(), "prj-my-project", refs, walker)
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

	t.Run("mixed org+folder linked refs resolves sources from both namespaces", func(t *testing.T) {
		orgNsObj := orgNS("my-org")
		fldNsObj := folderNS("payments")
		prjNsObj := projectNS("my-project")

		orgCue := "// org httproute"
		orgCM := orgTemplateConfigMap("my-org", "httproute", "HTTPRoute", "", orgCue, false, true)

		folderCue := "// folder payments policy"
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", folderCue, false, true)

		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, orgCM, fldCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{
			ancestors: []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj},
		}

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: orgScope, ScopeName: "my-org", Name: "httproute"},
			{Scope: folderScope, ScopeName: "payments", Name: "payments-policy"},
		}
		sources, err := k8s.ListAncestorTemplateSourcesForRender(context.Background(), "prj-my-project", refs, walker)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 2 {
			t.Fatalf("expected 2 sources, got %d", len(sources))
		}
	})

	t.Run("mandatory folder template included without explicit linking", func(t *testing.T) {
		orgNsObj := orgNS("my-org")
		fldNsObj := folderNS("payments")
		prjNsObj := projectNS("my-project")

		mandatoryCue := "// mandatory folder template"
		fldCM := folderTemplateConfigMap("payments", "audit-policy", "Audit Policy", "", mandatoryCue, true, true)

		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{
			ancestors: []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj},
		}

		// No linked refs — mandatory folder template should still be included.
		sources, err := k8s.ListAncestorTemplateSourcesForRender(context.Background(), "prj-my-project", nil, walker)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source (mandatory folder template), got %d", len(sources))
		}
		if sources[0] != mandatoryCue {
			t.Errorf("expected %q, got %q", mandatoryCue, sources[0])
		}
	})

	t.Run("disabled folder template excluded even when linked", func(t *testing.T) {
		orgNsObj := orgNS("my-org")
		fldNsObj := folderNS("payments")
		prjNsObj := projectNS("my-project")

		// disabled template
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", "// disabled", false, false)

		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{
			ancestors: []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj},
		}

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: folderScope, ScopeName: "payments", Name: "payments-policy"},
		}
		sources, err := k8s.ListAncestorTemplateSourcesForRender(context.Background(), "prj-my-project", refs, walker)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected 0 sources for disabled template, got %d", len(sources))
		}
	})

	t.Run("version-constrained folder linked ref resolved from release", func(t *testing.T) {
		orgNsObj := orgNS("my-org")
		fldNsObj := folderNS("payments")
		prjNsObj := projectNS("my-project")

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
		walker := &stubHierarchyWalker{
			ancestors: []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj},
		}

		refs := []*consolev1.LinkedTemplateRef{
			folderLinkedRefWithConstraint("payments", "payments-policy", ">=1.0.0"),
		}
		sources, err := k8s.ListAncestorTemplateSourcesForRender(context.Background(), "prj-my-project", refs, walker)
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
		walker := &stubHierarchyWalker{
			err: fmt.Errorf("walk failed"),
		}

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: folderScope, ScopeName: "payments", Name: "payments-policy"},
		}
		sources, err := k8s.ListAncestorTemplateSourcesForRender(context.Background(), "prj-my-project", refs, walker)
		if err != nil {
			t.Fatalf("expected graceful degradation, got error: %v", err)
		}
		if len(sources) != 0 {
			t.Errorf("expected 0 sources on walker failure, got %d", len(sources))
		}
	})

	t.Run("no linked refs and no mandatory templates returns empty", func(t *testing.T) {
		orgNsObj := orgNS("my-org")
		fldNsObj := folderNS("payments")
		prjNsObj := projectNS("my-project")

		// Non-mandatory, enabled, but not linked
		fldCM := folderTemplateConfigMap("payments", "optional", "Optional", "", "// optional", false, true)

		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM)
		k8s := NewK8sClient(fakeClient, testResolver())
		walker := &stubHierarchyWalker{
			ancestors: []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj},
		}

		sources, err := k8s.ListAncestorTemplateSourcesForRender(context.Background(), "prj-my-project", nil, walker)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Errorf("expected 0 sources, got %d", len(sources))
		}
	})
}
