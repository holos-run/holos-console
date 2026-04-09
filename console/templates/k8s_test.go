package templates

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

func testResolver() *resolver.Resolver {
	return &resolver.Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
}

func projectNS(project string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prj-" + project,
			Labels: map[string]string{
				v1alpha1.LabelManagedBy:    v1alpha1.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      project,
			},
		},
	}
}

func templateConfigMap(project, name, displayName, description, cueTemplate string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "prj-" + project,
			Labels: map[string]string{
				v1alpha1.LabelManagedBy:    v1alpha1.ManagedByValue,
				v1alpha1.LabelResourceType: v1alpha1.ResourceTypeDeploymentTemplate,
			},
			Annotations: map[string]string{
				v1alpha1.AnnotationDisplayName: displayName,
				v1alpha1.AnnotationDescription: description,
			},
		},
		Data: map[string]string{
			CueTemplateKey: cueTemplate,
		},
	}
}

func TestListTemplates(t *testing.T) {
	t.Run("returns empty list when no templates exist", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListTemplates(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 0 {
			t.Errorf("expected 0 templates, got %d", len(cms))
		}
	})

	t.Run("returns templates with correct label", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web application template", "#Input: {}\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListTemplates(context.Background(), "my-project")
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
}

func TestGetTemplate(t *testing.T) {
	t.Run("returns existing template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", "#Input: {}\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		result, err := k8s.GetTemplate(context.Background(), "my-project", "web-app")
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

		_, err := k8s.GetTemplate(context.Background(), "my-project", "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestCreateTemplate(t *testing.T) {
	t.Run("creates template with correct labels and annotations", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateTemplate(context.Background(), "my-project", "web-app", "Web App", "A web app", "#Input: {}\n", nil, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Labels[v1alpha1.LabelManagedBy] != v1alpha1.ManagedByValue {
			t.Error("expected managed-by label")
		}
		if cm.Labels[v1alpha1.LabelResourceType] != v1alpha1.ResourceTypeDeploymentTemplate {
			t.Error("expected resource-type label")
		}
		if cm.Annotations[v1alpha1.AnnotationDisplayName] != "Web App" {
			t.Errorf("expected display name 'Web App', got %q", cm.Annotations[v1alpha1.AnnotationDisplayName])
		}
		if cm.Annotations[v1alpha1.AnnotationDescription] != "A web app" {
			t.Errorf("expected description 'A web app', got %q", cm.Annotations[v1alpha1.AnnotationDescription])
		}
		if cm.Data[CueTemplateKey] != "#Input: {}\n" {
			t.Errorf("expected cue template content, got %q", cm.Data[CueTemplateKey])
		}

		// Verify it was persisted
		got, err := fakeClient.CoreV1().ConfigMaps("prj-my-project").Get(context.Background(), "web-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected ConfigMap to exist, got %v", err)
		}
		if got.Data[CueTemplateKey] != "#Input: {}\n" {
			t.Errorf("expected persisted cue template, got %q", got.Data[CueTemplateKey])
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
		cm, err := k8s.CreateTemplate(context.Background(), "my-project", "web-app", "Web App", "A web app", "#Input: {}\n", defaults, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Verify defaults.json key was written
		rawJSON, ok := cm.Data[DefaultsKey]
		if !ok {
			t.Fatalf("expected %q key in ConfigMap data", DefaultsKey)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(rawJSON), &got); err != nil {
			t.Fatalf("defaults.json is not valid JSON: %v", err)
		}
		if got["image"] != "ghcr.io/mccutchen/go-httpbin" {
			t.Errorf("expected image 'ghcr.io/mccutchen/go-httpbin', got %v", got["image"])
		}
		if got["tag"] != "2.21" {
			t.Errorf("expected tag '2.21', got %v", got["tag"])
		}
	})

	t.Run("creates template without defaults omits defaults.json key", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateTemplate(context.Background(), "my-project", "web-app", "Web App", "A web app", "#Input: {}\n", nil, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := cm.Data[DefaultsKey]; ok {
			t.Errorf("expected no %q key in ConfigMap data when defaults is nil", DefaultsKey)
		}
	})
}

func TestUpdateTemplate(t *testing.T) {
	t.Run("updates display name only", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", "#Input: {}\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newName := "Updated Web App"
		updated, err := k8s.UpdateTemplate(context.Background(), "my-project", "web-app", &newName, nil, nil, nil, false, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Annotations[v1alpha1.AnnotationDisplayName] != "Updated Web App" {
			t.Errorf("expected updated display name, got %q", updated.Annotations[v1alpha1.AnnotationDisplayName])
		}
		// Description should be unchanged
		if updated.Annotations[v1alpha1.AnnotationDescription] != "A web app" {
			t.Errorf("expected unchanged description, got %q", updated.Annotations[v1alpha1.AnnotationDescription])
		}
	})

	t.Run("updates cue template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", "#Input: {}\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newTemplate := "#Input: { name: string }\n"
		updated, err := k8s.UpdateTemplate(context.Background(), "my-project", "web-app", nil, nil, &newTemplate, nil, false, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Data[CueTemplateKey] != newTemplate {
			t.Errorf("expected updated template, got %q", updated.Data[CueTemplateKey])
		}
	})

	t.Run("adds defaults on update", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", "#Input: {}\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		defaults := &consolev1.TemplateDefaults{Image: "ghcr.io/example/app", Tag: "v1.0"}
		updated, err := k8s.UpdateTemplate(context.Background(), "my-project", "web-app", nil, nil, nil, defaults, false, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		rawJSON, ok := updated.Data[DefaultsKey]
		if !ok {
			t.Fatalf("expected %q key after update with defaults", DefaultsKey)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(rawJSON), &got); err != nil {
			t.Fatalf("defaults.json is not valid JSON: %v", err)
		}
		if got["image"] != "ghcr.io/example/app" {
			t.Errorf("expected image 'ghcr.io/example/app', got %v", got["image"])
		}
	})

	t.Run("clears defaults on update when clearDefaults is true", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", "#Input: {}\n")
		// Pre-populate defaults.json
		cm.Data[DefaultsKey] = `{"image":"old","tag":"old"}`
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		updated, err := k8s.UpdateTemplate(context.Background(), "my-project", "web-app", nil, nil, nil, nil, true, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := updated.Data[DefaultsKey]; ok {
			t.Errorf("expected %q key to be removed after clearDefaults=true", DefaultsKey)
		}
	})

	t.Run("preserves existing defaults when not updating them", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", "#Input: {}\n")
		cm.Data[DefaultsKey] = `{"image":"preserved","tag":"v1"}`
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newName := "New Display Name"
		updated, err := k8s.UpdateTemplate(context.Background(), "my-project", "web-app", &newName, nil, nil, nil, false, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		rawJSON, ok := updated.Data[DefaultsKey]
		if !ok {
			t.Fatalf("expected %q key to be preserved when not updating defaults", DefaultsKey)
		}
		if rawJSON != `{"image":"preserved","tag":"v1"}` {
			t.Errorf("expected defaults to be preserved, got %q", rawJSON)
		}
	})

	t.Run("returns error for nonexistent template", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		newName := "Updated"
		_, err := k8s.UpdateTemplate(context.Background(), "my-project", "nonexistent", &newName, nil, nil, nil, false, nil)
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestDeleteTemplate(t *testing.T) {
	t.Run("deletes existing template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", "#Input: {}\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.DeleteTemplate(context.Background(), "my-project", "web-app")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Verify it was deleted
		_, err = fakeClient.CoreV1().ConfigMaps("prj-my-project").Get(context.Background(), "web-app", metav1.GetOptions{})
		if err == nil {
			t.Fatal("expected ConfigMap to be deleted")
		}
	})

	t.Run("returns error for nonexistent template", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.DeleteTemplate(context.Background(), "my-project", "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestCloneTemplate(t *testing.T) {
	t.Run("copies CUE template and description from source", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app template", "foo: true\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		cloned, err := k8s.CloneTemplate(context.Background(), "my-project", "web-app", "web-app-copy", "Web App Copy")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cloned.Name != "web-app-copy" {
			t.Errorf("expected name 'web-app-copy', got %q", cloned.Name)
		}
		if cloned.Annotations[v1alpha1.AnnotationDisplayName] != "Web App Copy" {
			t.Errorf("expected display name 'Web App Copy', got %q", cloned.Annotations[v1alpha1.AnnotationDisplayName])
		}
		if cloned.Annotations[v1alpha1.AnnotationDescription] != "A web app template" {
			t.Errorf("expected description from source, got %q", cloned.Annotations[v1alpha1.AnnotationDescription])
		}
		if cloned.Data[CueTemplateKey] != "foo: true\n" {
			t.Errorf("expected CUE template from source, got %q", cloned.Data[CueTemplateKey])
		}
	})

	t.Run("returns error when source does not exist", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		_, err := k8s.CloneTemplate(context.Background(), "my-project", "nonexistent", "copy", "Copy")
		if err == nil {
			t.Fatal("expected error when source does not exist")
		}
	})
}

func TestLinkedOrgTemplatesAnnotation(t *testing.T) {
	t.Run("CreateTemplate stores linked list as JSON annotation", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		linked := []string{"archetype-a", "policy-floor"}
		cm, err := k8s.CreateTemplate(context.Background(), "my-project", "web-app", "Web App", "desc", "#Input: {}\n", nil, linked)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		raw, ok := cm.Annotations[v1alpha1.AnnotationLinkedOrgTemplates]
		if !ok {
			t.Fatal("expected linked-org-templates annotation")
		}
		var got []string
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("annotation is not valid JSON: %v", err)
		}
		if len(got) != 2 || got[0] != "archetype-a" || got[1] != "policy-floor" {
			t.Errorf("expected [archetype-a policy-floor], got %v", got)
		}
	})

	t.Run("CreateTemplate with empty linked list omits annotation", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateTemplate(context.Background(), "my-project", "web-app", "Web App", "desc", "#Input: {}\n", nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := cm.Annotations[v1alpha1.AnnotationLinkedOrgTemplates]; ok {
			t.Error("expected no linked-org-templates annotation when linked list is nil")
		}
	})

	t.Run("UpdateTemplate replaces linked list", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "desc", "#Input: {}\n")
		cm.Annotations[v1alpha1.AnnotationLinkedOrgTemplates] = `["old-template"]`
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newLinked := []string{"new-template"}
		updated, err := k8s.UpdateTemplate(context.Background(), "my-project", "web-app", nil, nil, nil, nil, false, newLinked)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		raw, ok := updated.Annotations[v1alpha1.AnnotationLinkedOrgTemplates]
		if !ok {
			t.Fatal("expected linked-org-templates annotation")
		}
		var got []string
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("annotation is not valid JSON: %v", err)
		}
		if len(got) != 1 || got[0] != "new-template" {
			t.Errorf("expected [new-template], got %v", got)
		}
	})

	t.Run("UpdateTemplate clears linked list when empty slice provided", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "desc", "#Input: {}\n")
		cm.Annotations[v1alpha1.AnnotationLinkedOrgTemplates] = `["old-template"]`
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		updated, err := k8s.UpdateTemplate(context.Background(), "my-project", "web-app", nil, nil, nil, nil, false, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := updated.Annotations[v1alpha1.AnnotationLinkedOrgTemplates]; ok {
			t.Error("expected linked-org-templates annotation to be removed on empty slice")
		}
	})

	t.Run("UpdateTemplate leaves linked list unchanged when nil provided", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "desc", "#Input: {}\n")
		cm.Annotations[v1alpha1.AnnotationLinkedOrgTemplates] = `["preserved"]`
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newDisplayName := "Updated"
		updated, err := k8s.UpdateTemplate(context.Background(), "my-project", "web-app", &newDisplayName, nil, nil, nil, false, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		raw, ok := updated.Annotations[v1alpha1.AnnotationLinkedOrgTemplates]
		if !ok {
			t.Fatal("expected linked-org-templates annotation to be preserved")
		}
		var got []string
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("annotation is not valid JSON: %v", err)
		}
		if len(got) != 1 || got[0] != "preserved" {
			t.Errorf("expected [preserved], got %v", got)
		}
	})
}
