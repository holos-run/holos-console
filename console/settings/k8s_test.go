package settings

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

func TestGetSettings(t *testing.T) {
	t.Run("returns defaults when no ConfigMap exists", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		settings, err := k8s.GetSettings(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if settings.Project != "my-project" {
			t.Errorf("expected project 'my-project', got %q", settings.Project)
		}
		if !settings.DeploymentsEnabled {
			t.Error("expected deployments_enabled=true by default")
		}
	})

	t.Run("reads existing ConfigMap", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      SettingsConfigMapName,
				Namespace: "prj-my-project",
				Labels: map[string]string{
					ManagedByLabel:    ManagedByValue,
					ResourceTypeLabel: ResourceTypeValue,
				},
			},
			Data: map[string]string{
				SettingsDataKey: `{"project":"my-project","deployments_enabled":false}`,
			},
		}
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		settings, err := k8s.GetSettings(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if settings.Project != "my-project" {
			t.Errorf("expected project 'my-project', got %q", settings.Project)
		}
		if settings.DeploymentsEnabled {
			t.Error("expected deployments_enabled=false from ConfigMap")
		}
	})
}

func TestUpdateSettings(t *testing.T) {
	t.Run("creates ConfigMap when none exists", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		settings := DefaultSettings("my-project")
		settings.DeploymentsEnabled = false

		result, err := k8s.UpdateSettings(context.Background(), settings)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result.DeploymentsEnabled {
			t.Error("expected deployments_enabled=false")
		}

		// Verify ConfigMap was created
		cm, err := fakeClient.CoreV1().ConfigMaps("prj-my-project").Get(context.Background(), SettingsConfigMapName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected ConfigMap to exist, got %v", err)
		}
		if cm.Labels[ManagedByLabel] != ManagedByValue {
			t.Error("expected managed-by label on ConfigMap")
		}
		if cm.Labels[ResourceTypeLabel] != ResourceTypeValue {
			t.Error("expected resource-type label on ConfigMap")
		}
	})

	t.Run("updates existing ConfigMap", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      SettingsConfigMapName,
				Namespace: "prj-my-project",
				Labels: map[string]string{
					ManagedByLabel:    ManagedByValue,
					ResourceTypeLabel: ResourceTypeValue,
				},
			},
			Data: map[string]string{
				SettingsDataKey: `{"project":"my-project","deployments_enabled":false}`,
			},
		}
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		settings := DefaultSettings("my-project")
		settings.DeploymentsEnabled = true

		result, err := k8s.UpdateSettings(context.Background(), settings)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !result.DeploymentsEnabled {
			t.Error("expected deployments_enabled=true after update")
		}

		// Verify round-trip
		readBack, err := k8s.GetSettings(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error on read-back, got %v", err)
		}
		if !readBack.DeploymentsEnabled {
			t.Error("expected deployments_enabled=true on read-back")
		}
	})
}
