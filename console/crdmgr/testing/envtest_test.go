// envtest_test.go smoke-tests the shared envtest bootstrap introduced
// in HOL-663. The storage suites in console/templates,
// console/templatepolicies, and console/templatepolicybindings exercise
// the real behavior (cache-backed reads, admission-policy enforcement);
// this test just confirms that StartManager returns a working cache,
// direct client, and core client, and that repeat calls within the same
// process reuse the underlying envtest rather than booting a second
// apiserver.
package crdmgrtesting

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// testScheme returns a runtime.Scheme registered with core + templates
// v1alpha1 types, mirroring what the storage suites build in their
// testhelpers_test.go files.
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

// TestStartManager_BasicCRUD confirms the cache-backed client returned
// by StartManager reflects writes issued through the direct client
// within the informer watch window. This exercises the same
// write-direct / read-through-cache contract every storage suite
// relies on.
func TestStartManager_BasicCRUD(t *testing.T) {
	scheme := testScheme(t)
	env := StartManager(t, Options{
		Scheme:          scheme,
		InformerObjects: []ctrlclient.Object{&templatesv1alpha1.Template{}},
	})
	if env == nil {
		return // Skipped: envtest binaries not installed.
	}

	nsName := "crdmgrtesting-smoke-basic"
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	if err := env.Direct.Create(context.Background(), ns); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	tmpl := &templatesv1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "probe", Namespace: nsName},
		Spec: templatesv1alpha1.TemplateSpec{
			DisplayName: "Probe",
			Enabled:     true,
			CueTemplate: "package holos\n",
		},
	}
	if err := env.Direct.Create(context.Background(), tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}

	// Poll the cache-backed client until it observes the seed write.
	// A single immediate read would race the watch propagation window.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var list templatesv1alpha1.TemplateList
		if err := env.Client.List(context.Background(), &list, ctrlclient.InNamespace(nsName)); err != nil {
			t.Fatalf("cache list: %v", err)
		}
		if len(list.Items) >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache-backed list did not observe seed template within deadline")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if env.Core == nil {
		t.Fatalf("Env.Core was nil; Core is required for Release ConfigMap paths")
	}
	if env.Cfg == nil {
		t.Fatalf("Env.Cfg was nil; Cfg is required for multi-manager fixtures")
	}
}

// TestStartManager_ReusesSharedEnv verifies that two StartManager calls
// within the same test binary process reuse the same apiserver. We
// assert this indirectly: both calls must surface the same Cfg.Host.
// If sharedEnvOnce were broken, the second StartManager would boot a
// second kube-apiserver on a different random port.
func TestStartManager_ReusesSharedEnv(t *testing.T) {
	scheme := testScheme(t)
	env1 := StartManager(t, Options{
		Scheme:          scheme,
		InformerObjects: []ctrlclient.Object{&templatesv1alpha1.Template{}},
	})
	if env1 == nil {
		return // Skipped.
	}
	env2 := StartManager(t, Options{
		Scheme:          scheme,
		InformerObjects: []ctrlclient.Object{&templatesv1alpha1.Template{}},
	})
	if env2 == nil {
		t.Fatalf("second StartManager call unexpectedly skipped")
	}
	if env1.Cfg.Host != env2.Cfg.Host {
		t.Fatalf("StartManager booted a second apiserver: first host=%q second host=%q",
			env1.Cfg.Host, env2.Cfg.Host)
	}
}
