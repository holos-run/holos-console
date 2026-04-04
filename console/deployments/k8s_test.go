package deployments

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

func deploymentConfigMap(project, name, image, tag, tmpl, displayName, description string) *corev1.ConfigMap {
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
			ImageKey:    image,
			TagKey:      tag,
			TemplateKey: tmpl,
		},
	}
}

func TestListDeployments(t *testing.T) {
	t.Run("returns empty list when no deployments exist", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListDeployments(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 0 {
			t.Errorf("expected 0 deployments, got %d", len(cms))
		}
	})

	t.Run("returns deployments with correct label", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "A web application")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListDeployments(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 1 {
			t.Fatalf("expected 1 deployment, got %d", len(cms))
		}
		if cms[0].Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", cms[0].Name)
		}
	})

	t.Run("does not return unlabeled configmaps", func(t *testing.T) {
		ns := projectNS("my-project")
		unlabeled := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-cm",
				Namespace: "prj-my-project",
			},
		}
		fakeClient := fake.NewClientset(ns, unlabeled)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListDeployments(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 0 {
			t.Errorf("expected 0 deployments (unlabeled CM should not appear), got %d", len(cms))
		}
	})
}

func TestGetDeployment(t *testing.T) {
	t.Run("returns deployment by name", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		got, err := k8s.GetDeployment(context.Background(), "my-project", "web-app")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got.Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", got.Name)
		}
		if got.Data[ImageKey] != "nginx" {
			t.Errorf("expected image 'nginx', got %q", got.Data[ImageKey])
		}
		if got.Data[TagKey] != "1.25" {
			t.Errorf("expected tag '1.25', got %q", got.Data[TagKey])
		}
	})

	t.Run("returns error for non-existent deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		_, err := k8s.GetDeployment(context.Background(), "my-project", "does-not-exist")
		if err == nil {
			t.Fatal("expected error for non-existent deployment")
		}
	})
}

func TestCreateDeployment(t *testing.T) {
	t.Run("creates deployment with correct fields", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateDeployment(context.Background(), "my-project", "web-app", "nginx", "1.25", "default", "Web App", "A web app", nil, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", cm.Name)
		}
		if cm.Labels[ResourceTypeLabel] != ResourceTypeValue {
			t.Errorf("expected label %q=%q, got %q", ResourceTypeLabel, ResourceTypeValue, cm.Labels[ResourceTypeLabel])
		}
		if cm.Labels[ManagedByLabel] != ManagedByValue {
			t.Errorf("expected label %q=%q, got %q", ManagedByLabel, ManagedByValue, cm.Labels[ManagedByLabel])
		}
		if cm.Data[ImageKey] != "nginx" {
			t.Errorf("expected image 'nginx', got %q", cm.Data[ImageKey])
		}
		if cm.Data[TagKey] != "1.25" {
			t.Errorf("expected tag '1.25', got %q", cm.Data[TagKey])
		}
		if cm.Data[TemplateKey] != "default" {
			t.Errorf("expected template 'default', got %q", cm.Data[TemplateKey])
		}
		if cm.Annotations[DisplayNameAnnotation] != "Web App" {
			t.Errorf("expected displayName 'Web App', got %q", cm.Annotations[DisplayNameAnnotation])
		}
		if cm.Annotations[DescriptionAnnotation] != "A web app" {
			t.Errorf("expected description 'A web app', got %q", cm.Annotations[DescriptionAnnotation])
		}
	})

	t.Run("stores command and args as JSON", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cmd := []string{"myapp"}
		args := []string{"--port", "8080"}
		cm, err := k8s.CreateDeployment(context.Background(), "my-project", "web-app", "nginx", "1.25", "default", "", "", cmd, args)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Data[CommandKey] != `["myapp"]` {
			t.Errorf("expected command JSON %q, got %q", `["myapp"]`, cm.Data[CommandKey])
		}
		if cm.Data[ArgsKey] != `["--port","8080"]` {
			t.Errorf("expected args JSON %q, got %q", `["--port","8080"]`, cm.Data[ArgsKey])
		}
	})

	t.Run("omits command and args keys when empty", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateDeployment(context.Background(), "my-project", "web-app", "nginx", "1.25", "default", "", "", nil, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := cm.Data[CommandKey]; ok {
			t.Error("expected command key to be absent when nil")
		}
		if _, ok := cm.Data[ArgsKey]; ok {
			t.Error("expected args key to be absent when nil")
		}
	})
}

func TestUpdateDeployment(t *testing.T) {
	t.Run("updates only non-nil fields", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "original desc")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newTag := "1.26"
		newDesc := "updated desc"
		updated, err := k8s.UpdateDeployment(context.Background(), "my-project", "web-app", nil, &newTag, nil, &newDesc, nil, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Data[ImageKey] != "nginx" {
			t.Errorf("expected image unchanged 'nginx', got %q", updated.Data[ImageKey])
		}
		if updated.Data[TagKey] != "1.26" {
			t.Errorf("expected tag '1.26', got %q", updated.Data[TagKey])
		}
		if updated.Annotations[DisplayNameAnnotation] != "Web App" {
			t.Errorf("expected displayName unchanged 'Web App', got %q", updated.Annotations[DisplayNameAnnotation])
		}
		if updated.Annotations[DescriptionAnnotation] != "updated desc" {
			t.Errorf("expected description 'updated desc', got %q", updated.Annotations[DescriptionAnnotation])
		}
	})

	t.Run("returns error for non-existent deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		newTag := "1.26"
		_, err := k8s.UpdateDeployment(context.Background(), "my-project", "does-not-exist", nil, &newTag, nil, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error for non-existent deployment")
		}
	})
}

func TestDeleteDeployment(t *testing.T) {
	t.Run("deletes existing deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "", "")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.DeleteDeployment(context.Background(), "my-project", "web-app")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Verify it was deleted.
		_, err = k8s.GetDeployment(context.Background(), "my-project", "web-app")
		if err == nil {
			t.Fatal("expected error after deletion")
		}
	})

	t.Run("returns error for non-existent deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.DeleteDeployment(context.Background(), "my-project", "does-not-exist")
		if err == nil {
			t.Fatal("expected error for non-existent deployment")
		}
	})
}
