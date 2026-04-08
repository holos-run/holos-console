package settings

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
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
				"app.kubernetes.io/managed-by":     "console.holos.run",
				resolver.ResourceTypeLabel:         resolver.ResourceTypeProject,
				resolver.ProjectLabel:              project,
			},
		},
	}
}

func projectNSWithAnnotation(project string, deploymentsEnabled bool) *corev1.Namespace {
	ns := projectNS(project)
	settings := DefaultSettings(project)
	settings.DeploymentsEnabled = deploymentsEnabled
	data, _ := json.Marshal(settings)
	ns.Annotations = map[string]string{
		v1alpha1.AnnotationSettings: string(data),
	}
	return ns
}

func TestGetSettings(t *testing.T) {
	t.Run("returns defaults when no annotation exists", func(t *testing.T) {
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
		if settings.DeploymentsEnabled {
			t.Error("expected deployments_enabled=false by default")
		}
	})

	t.Run("reads existing annotation", func(t *testing.T) {
		ns := projectNSWithAnnotation("my-project", true)
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
			t.Error("expected deployments_enabled=true from annotation")
		}
	})

	t.Run("reads annotation with deployments disabled", func(t *testing.T) {
		ns := projectNSWithAnnotation("my-project", false)
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		settings, err := k8s.GetSettings(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if settings.DeploymentsEnabled {
			t.Error("expected deployments_enabled=false from annotation")
		}
	})
}

func TestUpdateSettings(t *testing.T) {
	t.Run("writes annotation to namespace", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		settings := DefaultSettings("my-project")
		settings.DeploymentsEnabled = true

		result, err := k8s.UpdateSettings(context.Background(), settings)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !result.DeploymentsEnabled {
			t.Error("expected deployments_enabled=true")
		}

		// Verify annotation was written
		updated, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "prj-my-project", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected namespace to exist, got %v", err)
		}
		if _, ok := updated.Annotations[v1alpha1.AnnotationSettings]; !ok {
			t.Error("expected settings annotation on namespace")
		}
	})

	t.Run("round-trip read after write", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		settings := DefaultSettings("my-project")
		settings.DeploymentsEnabled = true

		_, err := k8s.UpdateSettings(context.Background(), settings)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		readBack, err := k8s.GetSettings(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error on read-back, got %v", err)
		}
		if !readBack.DeploymentsEnabled {
			t.Error("expected deployments_enabled=true on read-back")
		}
	})
}

func TestGetProjectNamespaceRaw(t *testing.T) {
	t.Run("returns JSON with apiVersion and kind", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		raw, err := k8s.GetProjectNamespaceRaw(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			t.Fatalf("expected valid JSON, got %v", err)
		}
		if obj["apiVersion"] != "v1" {
			t.Errorf("expected apiVersion=v1, got %v", obj["apiVersion"])
		}
		if obj["kind"] != "Namespace" {
			t.Errorf("expected kind=Namespace, got %v", obj["kind"])
		}
	})
}

func TestDefaultSettings(t *testing.T) {
	t.Run("deployments_enabled defaults to false", func(t *testing.T) {
		settings := DefaultSettings("test")
		if settings.DeploymentsEnabled {
			t.Error("expected deployments_enabled=false by default")
		}
		if settings.Project != "test" {
			t.Errorf("expected project 'test', got %q", settings.Project)
		}
	})
}
