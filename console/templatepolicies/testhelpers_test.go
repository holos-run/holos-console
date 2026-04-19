// testhelpers_test.go provides shared test fixtures for the HOL-662
// rewrite. K8sClient now reads and writes TemplatePolicy CRDs through a
// controller-runtime client.Client; tests construct either a fake
// ctrlclient (for the handler-level suite that still speaks in terms of
// ConfigMap fixtures) or a cache-backed envtest client (for k8s_test.go).
package templatepolicies

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// testScheme returns a runtime.Scheme registered with core and templates
// v1alpha1 types — enough for the fake controller-runtime client and the
// envtest direct/cache-backed clients to List/Get/Create/Update/Delete
// TemplatePolicy objects.
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("register clientgo scheme: %v", err)
	}
	if err := templatesv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("register templates scheme: %v", err)
	}
	return s
}

// newFakeCtrlClient returns a fake controller-runtime client preloaded
// with the given CRD objects. The scheme registers core + templates
// v1alpha1 so the client accepts TemplatePolicy CRs.
func newFakeCtrlClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	return ctrlfake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(objs...).
		Build()
}
