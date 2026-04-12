package deployments

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	testing2 "k8s.io/client-go/testing"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
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

		err := applier.Apply(context.Background(), deploymentName, resources)
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

		if err := applier.Apply(context.Background(), deploymentName, resources); err != nil {
			t.Fatalf("first apply failed: %v", err)
		}
		if err := applier.Apply(context.Background(), deploymentName, resources); err != nil {
			t.Fatalf("second apply failed: %v", err)
		}
	})

	t.Run("ownership label is injected into patch payload", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()
		resources := []unstructured.Unstructured{
			makeDeploymentResource("web-app", namespace),
		}

		if err := applier.Apply(context.Background(), deploymentName, resources); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		found := false
		for _, a := range fakeClient.Actions() {
			if pa, ok := a.(testing2.PatchAction); ok {
				patch := string(pa.GetPatch())
				if containsStr(patch, v1alpha2.AnnotationDeployment) {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("expected ownership label %q in patch payload", v1alpha2.AnnotationDeployment)
		}
	})

	t.Run("multiple resources each get a patch action", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()
		resources := []unstructured.Unstructured{
			makeDeploymentResource("web-app", namespace),
			makeServiceResource("web-app", namespace),
		}

		if err := applier.Apply(context.Background(), deploymentName, resources); err != nil {
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
			v1alpha2.AnnotationDeployment:  deploymentName,
		})
		_, err := fakeClient.Resource(depGVR).Namespace(namespace).Create(
			context.Background(), &dep, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("pre-create failed: %v", err)
		}

		if err := applier.Cleanup(context.Background(), []string{namespace}, deploymentName); err != nil {
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

		if err := applier.Cleanup(context.Background(), []string{namespace}, deploymentName); err != nil {
			t.Fatalf("expected no error for empty cleanup, got %v", err)
		}
	})

	t.Run("cleanup does not delete resources owned by other deployments", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()

		// Create a resource owned by "other-app".
		dep := makeDeploymentResource("other-app", namespace)
		dep.SetLabels(map[string]string{
			"app.kubernetes.io/managed-by": "console.holos.run",
			v1alpha2.AnnotationDeployment:  "other-app",
		})
		_, err := fakeClient.Resource(depGVR).Namespace(namespace).Create(
			context.Background(), &dep, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("pre-create failed: %v", err)
		}

		// Cleanup for "web-app" should not touch "other-app"'s resource.
		if err := applier.Cleanup(context.Background(), []string{namespace}, deploymentName); err != nil {
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

func TestApplier_Reconcile(t *testing.T) {
	namespace := "prj-my-project"
	deploymentName := "web-app"
	depGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	svcGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}

	t.Run("applies resources and cleans orphans", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()

		// Pre-create an orphaned Service with the ownership label (it was in the
		// old desired set but is no longer in the new one).
		orphan := makeServiceResource("old-svc", namespace)
		orphan.SetLabels(map[string]string{
			v1alpha2.AnnotationDeployment: deploymentName,
		})
		_, err := fakeClient.Resource(svcGVR).Namespace(namespace).Create(
			context.Background(), &orphan, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("pre-create orphan failed: %v", err)
		}

		// New desired set: only a Deployment.
		resources := []unstructured.Unstructured{
			makeDeploymentResource("web-app", namespace),
		}

		if err := applier.Reconcile(context.Background(), deploymentName, resources); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// The orphaned Service should have been deleted.
		deleted := false
		for _, a := range fakeClient.Actions() {
			if a.GetVerb() == "delete" && a.GetResource() == svcGVR {
				deleted = true
				break
			}
		}
		if !deleted {
			t.Error("expected the orphaned Service to be deleted")
		}
	})

	t.Run("does not delete resources in the desired set", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()

		// Pre-create a Deployment that IS in the desired set.
		existing := makeDeploymentResource("web-app", namespace)
		existing.SetLabels(map[string]string{
			v1alpha2.AnnotationDeployment: deploymentName,
		})
		_, err := fakeClient.Resource(depGVR).Namespace(namespace).Create(
			context.Background(), &existing, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("pre-create failed: %v", err)
		}

		// Reconcile with the same Deployment in the desired set.
		resources := []unstructured.Unstructured{
			makeDeploymentResource("web-app", namespace),
		}

		if err := applier.Reconcile(context.Background(), deploymentName, resources); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// No delete actions should have occurred.
		for _, a := range fakeClient.Actions() {
			if a.GetVerb() == "delete" {
				t.Errorf("expected no delete for resource in desired set, got delete action on %s", a.GetResource().Resource)
			}
		}
	})

	t.Run("does not delete resources owned by other deployments", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()

		// Pre-create a resource owned by "other-app" (a different deployment).
		other := makeDeploymentResource("other-app", namespace)
		other.SetLabels(map[string]string{
			v1alpha2.AnnotationDeployment: "other-app",
		})
		_, err := fakeClient.Resource(depGVR).Namespace(namespace).Create(
			context.Background(), &other, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("pre-create failed: %v", err)
		}

		// Reconcile for "web-app" with an empty desired set.
		if err := applier.Reconcile(context.Background(), deploymentName, nil); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// The resource owned by "other-app" must not be deleted.
		for _, a := range fakeClient.Actions() {
			if a.GetVerb() == "delete" {
				t.Error("reconcile deleted a resource owned by a different deployment")
			}
		}
	})

	t.Run("apply failure skips orphan cleanup", func(t *testing.T) {
		// Build a client that always fails patch (simulate apply failure) but has
		// an orphaned Service pre-created so we can observe whether a delete occurs.
		failClient := dynamicfake.NewSimpleDynamicClient(fakeDynamicScheme())
		failClient.PrependReactor("patch", "*", func(action testing2.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("simulated apply failure")
		})

		orphan := makeServiceResource("old-svc", namespace)
		orphan.SetLabels(map[string]string{
			v1alpha2.AnnotationDeployment: deploymentName,
		})
		_, err := failClient.Resource(svcGVR).Namespace(namespace).Create(
			context.Background(), &orphan, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("pre-create orphan in failClient failed: %v", err)
		}

		failApplier := NewApplier(failClient)
		resources := []unstructured.Unstructured{
			makeDeploymentResource("web-app", namespace),
		}

		// Reconcile should return an error (apply failed).
		if err := failApplier.Reconcile(context.Background(), deploymentName, resources); err == nil {
			t.Fatal("expected an error from Reconcile when apply fails")
		}

		// No delete actions should have been attempted (orphan cleanup skipped).
		for _, a := range failClient.Actions() {
			if a.GetVerb() == "delete" {
				t.Error("expected no delete when apply fails, but delete was called")
			}
		}
	})

	t.Run("multi-namespace: reconcile detects orphans across namespaces", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()

		// Pre-create an orphaned Service in namespace "ns-a".
		orphan := makeServiceResource("old-svc", "ns-a")
		orphan.SetLabels(map[string]string{
			v1alpha2.AnnotationDeployment: deploymentName,
		})
		_, err := fakeClient.Resource(svcGVR).Namespace("ns-a").Create(
			context.Background(), &orphan, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("pre-create orphan failed: %v", err)
		}

		// New desired set: a Deployment in "ns-b" (different namespace).
		resources := []unstructured.Unstructured{
			makeDeploymentResource("web-app", "ns-b"),
		}

		// Pass "ns-a" as a previous namespace so the reconciler scans it.
		if err := applier.Reconcile(context.Background(), deploymentName, resources, "ns-a"); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// The orphaned Service in "ns-a" should have been deleted.
		deleted := false
		for _, a := range fakeClient.Actions() {
			if a.GetVerb() == "delete" && a.GetResource() == svcGVR && a.GetNamespace() == "ns-a" {
				deleted = true
				break
			}
		}
		if !deleted {
			t.Error("expected the orphaned Service in ns-a to be deleted when desired set is in ns-b")
		}
	})

	t.Run("multi-namespace: resource moved between namespaces", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()

		// Pre-create a Service in the old namespace "ns-old".
		oldSvc := makeServiceResource("my-svc", "ns-old")
		oldSvc.SetLabels(map[string]string{
			v1alpha2.AnnotationDeployment: deploymentName,
		})
		_, err := fakeClient.Resource(svcGVR).Namespace("ns-old").Create(
			context.Background(), &oldSvc, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("pre-create old svc failed: %v", err)
		}

		// Desired set: same Service name but in "ns-new".
		resources := []unstructured.Unstructured{
			makeServiceResource("my-svc", "ns-new"),
		}

		// Pass "ns-old" as a previous namespace so the reconciler cleans it up.
		if err := applier.Reconcile(context.Background(), deploymentName, resources, "ns-old"); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// The old-namespace copy should be deleted.
		deleted := false
		for _, a := range fakeClient.Actions() {
			if a.GetVerb() == "delete" && a.GetResource() == svcGVR && a.GetNamespace() == "ns-old" {
				deleted = true
				break
			}
		}
		if !deleted {
			t.Error("expected old-namespace Service to be deleted when it moves to a new namespace")
		}
	})
}

func TestApplier_MultiNamespace(t *testing.T) {
	deploymentName := "web-app"
	svcGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
	depGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	t.Run("apply: resources applied to their own namespaces", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()

		resources := []unstructured.Unstructured{
			makeDeploymentResource("web-app", "ns-a"),
			makeServiceResource("web-app", "ns-b"),
		}

		if err := applier.Apply(context.Background(), deploymentName, resources); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Verify patch actions target the correct namespaces.
		var patchActions []testing2.PatchAction
		for _, a := range fakeClient.Actions() {
			if pa, ok := a.(testing2.PatchAction); ok {
				patchActions = append(patchActions, pa)
			}
		}
		if len(patchActions) != 2 {
			t.Fatalf("expected 2 patch actions, got %d", len(patchActions))
		}

		// First patch (Deployment) should target ns-a.
		if patchActions[0].GetNamespace() != "ns-a" {
			t.Errorf("expected first patch namespace ns-a, got %s", patchActions[0].GetNamespace())
		}
		// Second patch (Service) should target ns-b.
		if patchActions[1].GetNamespace() != "ns-b" {
			t.Errorf("expected second patch namespace ns-b, got %s", patchActions[1].GetNamespace())
		}
	})

	t.Run("cleanup: removes resources from multiple namespaces", func(t *testing.T) {
		applier, fakeClient := newFakeApplier()

		// Pre-create resources in two different namespaces.
		depA := makeDeploymentResource("web-app", "ns-a")
		depA.SetLabels(map[string]string{
			v1alpha2.AnnotationDeployment: deploymentName,
		})
		_, err := fakeClient.Resource(depGVR).Namespace("ns-a").Create(
			context.Background(), &depA, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("pre-create dep in ns-a failed: %v", err)
		}

		svcB := makeServiceResource("web-app", "ns-b")
		svcB.SetLabels(map[string]string{
			v1alpha2.AnnotationDeployment: deploymentName,
		})
		_, err = fakeClient.Resource(svcGVR).Namespace("ns-b").Create(
			context.Background(), &svcB, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("pre-create svc in ns-b failed: %v", err)
		}

		namespaces := []string{"ns-a", "ns-b"}
		if err := applier.Cleanup(context.Background(), namespaces, deploymentName); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Verify delete actions occurred in both namespaces.
		deletedNS := make(map[string]bool)
		for _, a := range fakeClient.Actions() {
			if a.GetVerb() == "delete" {
				deletedNS[a.GetNamespace()] = true
			}
		}
		if !deletedNS["ns-a"] {
			t.Error("expected delete in ns-a")
		}
		if !deletedNS["ns-b"] {
			t.Error("expected delete in ns-b")
		}
	})

	t.Run("cleanup: empty namespace set is a no-op", func(t *testing.T) {
		applier, _ := newFakeApplier()

		if err := applier.Cleanup(context.Background(), nil, deploymentName); err != nil {
			t.Fatalf("expected no error for empty cleanup, got %v", err)
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
