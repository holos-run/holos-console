package deployments

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	testing2 "k8s.io/client-go/testing"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
)

// allTestGVRs lists every GVR that the fake scheme must know about,
// including the gateway.networking.k8s.io group for HTTPRoute and ReferenceGrant.
var allTestGVRs = []struct {
	gvr  schema.GroupVersionResource
	kind string
}{
	{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, "Deployment"},
	{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, "Service"},
	{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}, "ServiceAccount"},
	{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, "Role"},
	{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, "RoleBinding"},
	{schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}, "HTTPRoute"},
	{schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "referencegrants"}, "ReferenceGrant"},
	{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, "ConfigMap"},
	{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, "Secret"},
}

// fakeDynamicScheme builds a scheme with all types used in tests.
func fakeDynamicScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	for _, entry := range allTestGVRs {
		gvk := schema.GroupVersionKind{Group: entry.gvr.Group, Version: entry.gvr.Version, Kind: entry.kind}
		listGVK := schema.GroupVersionKind{Group: entry.gvr.Group, Version: entry.gvr.Version, Kind: entry.kind + "List"}
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
	}
	return scheme
}

// newFakeApplier creates an Applier backed by a fake dynamic client.
// A custom reactor is added so that PATCH (server-side apply) succeeds
// even when the resource does not already exist.
func newFakeApplier() (*Applier, *dynamicfake.FakeDynamicClient) {
	fakeClient := dynamicfake.NewSimpleDynamicClient(fakeDynamicScheme())

	// Server-side apply creates-or-updates; the fake client only patches
	// existing objects. Prepend a reactor that accepts all patch calls.
	fakeClient.PrependReactor("patch", "*", func(action testing2.Action) (bool, runtime.Object, error) {
		return true, &unstructured.Unstructured{}, nil
	})

	applier := NewApplier(fakeClient)
	return applier, fakeClient
}

// makeDeploymentResource creates a minimal Deployment resource for tests.
func makeDeploymentResource(name, namespace string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion("apps/v1")
	u.SetKind("Deployment")
	u.SetName(name)
	u.SetNamespace(namespace)
	u.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "console.holos.run",
	})
	_ = unstructured.SetNestedField(u.Object, map[string]interface{}{}, "spec")
	return u
}

// makeServiceResource creates a minimal Service resource for tests.
func makeServiceResource(name, namespace string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion("v1")
	u.SetKind("Service")
	u.SetName(name)
	u.SetNamespace(namespace)
	u.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "console.holos.run",
	})
	_ = unstructured.SetNestedField(u.Object, map[string]interface{}{}, "spec")
	return u
}

func TestApplier_Apply(t *testing.T) {
	namespace := "prj-my-project"
	deploymentName := "web-app"

	t.Run("apply issues patch action for each resource", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()
		resources := []unstructured.Unstructured{
			makeDeploymentResource("web-app", namespace),
		}

		err := applier.Apply(context.Background(), namespace, deploymentName, resources)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		var patchActions []testing2.PatchAction
		for _, a := range fakeClient.Actions() {
			if pa, ok := a.(testing2.PatchAction); ok {
				patchActions = append(patchActions, pa)
			}
		}
		if len(patchActions) == 0 {
			t.Fatal("expected at least one patch action for server-side apply")
		}
	})

	t.Run("re-apply is idempotent", func(t *testing.T) {
		applier, _ := newFakeApplier()
		resources := []unstructured.Unstructured{
			makeDeploymentResource("web-app", namespace),
		}

		if err := applier.Apply(context.Background(), namespace, deploymentName, resources); err != nil {
			t.Fatalf("first apply failed: %v", err)
		}
		if err := applier.Apply(context.Background(), namespace, deploymentName, resources); err != nil {
			t.Fatalf("second apply failed: %v", err)
		}
	})

	t.Run("ownership label is injected into patch payload", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()
		resources := []unstructured.Unstructured{
			makeDeploymentResource("web-app", namespace),
		}

		if err := applier.Apply(context.Background(), namespace, deploymentName, resources); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		found := false
		for _, a := range fakeClient.Actions() {
			if pa, ok := a.(testing2.PatchAction); ok {
				patch := string(pa.GetPatch())
				if containsStr(patch, v1alpha1.AnnotationDeployment) {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("expected ownership label %q in patch payload", v1alpha1.AnnotationDeployment)
		}
	})

	t.Run("multiple resources each get a patch action", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()
		resources := []unstructured.Unstructured{
			makeDeploymentResource("web-app", namespace),
			makeServiceResource("web-app", namespace),
		}

		if err := applier.Apply(context.Background(), namespace, deploymentName, resources); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		var patchCount int
		for _, a := range fakeClient.Actions() {
			if _, ok := a.(testing2.PatchAction); ok {
				patchCount++
			}
		}
		if patchCount < 2 {
			t.Errorf("expected at least 2 patch actions, got %d", patchCount)
		}
	})
}

func TestApplier_Cleanup(t *testing.T) {
	namespace := "prj-my-project"
	deploymentName := "web-app"
	depGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	t.Run("cleanup deletes resources with ownership label", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()

		// Pre-create a Deployment with the ownership label.
		dep := makeDeploymentResource("web-app", namespace)
		dep.SetLabels(map[string]string{
			"app.kubernetes.io/managed-by": "console.holos.run",
			v1alpha1.AnnotationDeployment:                 deploymentName,
		})
		_, err := fakeClient.Resource(depGVR).Namespace(namespace).Create(
			context.Background(), &dep, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("pre-create failed: %v", err)
		}

		if err := applier.Cleanup(context.Background(), namespace, deploymentName); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Verify a delete action occurred.
		deleted := false
		for _, a := range fakeClient.Actions() {
			if a.GetVerb() == "delete" {
				deleted = true
				break
			}
		}
		if !deleted {
			t.Error("expected at least one delete action during cleanup")
		}
	})

	t.Run("cleanup with no owned resources is a no-op", func(t *testing.T) {
		applier, _ := newFakeApplier()

		if err := applier.Cleanup(context.Background(), namespace, deploymentName); err != nil {
			t.Fatalf("expected no error for empty cleanup, got %v", err)
		}
	})

	t.Run("cleanup does not delete resources owned by other deployments", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()

		// Create a resource owned by "other-app".
		dep := makeDeploymentResource("other-app", namespace)
		dep.SetLabels(map[string]string{
			"app.kubernetes.io/managed-by": "console.holos.run",
			v1alpha1.AnnotationDeployment:                 "other-app",
		})
		_, err := fakeClient.Resource(depGVR).Namespace(namespace).Create(
			context.Background(), &dep, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("pre-create failed: %v", err)
		}

		// Cleanup for "web-app" should not touch "other-app"'s resource.
		if err := applier.Cleanup(context.Background(), namespace, deploymentName); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Verify no delete occurred.
		for _, a := range fakeClient.Actions() {
			if a.GetVerb() == "delete" {
				t.Error("cleanup deleted a resource it does not own")
			}
		}
	})
}

// containsStr checks whether s contains substr.
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
