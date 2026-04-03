package templates

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/holos-run/holos-console/console/resolver"
)

func testResolver() *resolver.Resolver {
	return &resolver.Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
}

func projectNS(project string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prj-" + project,
			Labels: map[string]string{
				ManagedByLabel:             ManagedByValue,
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
				ManagedByLabel:    ManagedByValue,
				ResourceTypeLabel: ResourceTypeValue,
			},
			Annotations: map[string]string{
				DisplayNameAnnotation: displayName,
				DescriptionAnnotation: description,
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
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web application template", "package deployment\n")
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
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", "package deployment\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		result, err := k8s.GetTemplate(context.Background(), "my-project", "web-app")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result.Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", result.Name)
		}
		if result.Data[CueTemplateKey] != "package deployment\n" {
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

		cm, err := k8s.CreateTemplate(context.Background(), "my-project", "web-app", "Web App", "A web app", "package deployment\n")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Labels[ManagedByLabel] != ManagedByValue {
			t.Error("expected managed-by label")
		}
		if cm.Labels[ResourceTypeLabel] != ResourceTypeValue {
			t.Error("expected resource-type label")
		}
		if cm.Annotations[DisplayNameAnnotation] != "Web App" {
			t.Errorf("expected display name 'Web App', got %q", cm.Annotations[DisplayNameAnnotation])
		}
		if cm.Annotations[DescriptionAnnotation] != "A web app" {
			t.Errorf("expected description 'A web app', got %q", cm.Annotations[DescriptionAnnotation])
		}
		if cm.Data[CueTemplateKey] != "package deployment\n" {
			t.Errorf("expected cue template content, got %q", cm.Data[CueTemplateKey])
		}

		// Verify it was persisted
		got, err := fakeClient.CoreV1().ConfigMaps("prj-my-project").Get(context.Background(), "web-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected ConfigMap to exist, got %v", err)
		}
		if got.Data[CueTemplateKey] != "package deployment\n" {
			t.Errorf("expected persisted cue template, got %q", got.Data[CueTemplateKey])
		}
	})
}

func TestUpdateTemplate(t *testing.T) {
	t.Run("updates display name only", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", "package deployment\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newName := "Updated Web App"
		updated, err := k8s.UpdateTemplate(context.Background(), "my-project", "web-app", &newName, nil, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Annotations[DisplayNameAnnotation] != "Updated Web App" {
			t.Errorf("expected updated display name, got %q", updated.Annotations[DisplayNameAnnotation])
		}
		// Description should be unchanged
		if updated.Annotations[DescriptionAnnotation] != "A web app" {
			t.Errorf("expected unchanged description, got %q", updated.Annotations[DescriptionAnnotation])
		}
	})

	t.Run("updates cue template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", "package deployment\n")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newTemplate := "package deployment\n\n#Input: { name: string }\n"
		updated, err := k8s.UpdateTemplate(context.Background(), "my-project", "web-app", nil, nil, &newTemplate)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Data[CueTemplateKey] != newTemplate {
			t.Errorf("expected updated template, got %q", updated.Data[CueTemplateKey])
		}
	})

	t.Run("returns error for nonexistent template", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		newName := "Updated"
		_, err := k8s.UpdateTemplate(context.Background(), "my-project", "nonexistent", &newName, nil, nil)
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestDeleteTemplate(t *testing.T) {
	t.Run("deletes existing template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", "package deployment\n")
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
