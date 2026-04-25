// cr_writer_test.go exercises the CR ↔ proto-store consistency requirement
// introduced in HOL-957. Tests use the shared envtest helper from
// console/crdmgr/testing so the Deployment CRD is installed against a real
// kube-apiserver.
//
// Coverage:
//  1. Create via proto-store → CR appears with matching spec fields.
//  2. Update via proto-store → CR spec reflects updated values.
//  3. Delete via proto-store → CR is removed.
//  4. ownerReferences written onto the CR by an external actor survive an
//     UpdateDeployment call (SSA field-manager isolation).
//  5. Lazy creation: ApplyOnUpdate creates the CR when the CR is absent (the
//     ConfigMap already exists but the CR was never written).
package deployments

import (
	"context"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	crdmgrtesting "github.com/holos-run/holos-console/console/crdmgr/testing"
	"github.com/holos-run/holos-console/console/resolver"
)

// TestMain hooks the shared envtest shutdown into this package's test run.
// Without it, the apiserver subprocess launched by the first StartManager call
// leaks until the OS kills it.
func TestMain(m *testing.M) {
	os.Exit(crdmgrtesting.RunTestsWithSharedEnv(m))
}

// testDeploymentScheme returns a runtime.Scheme with core and
// deployments.holos.run/v1alpha1 types registered — enough for the
// controller-runtime client to CRUD Deployment CRs.
func testDeploymentScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("register clientgo scheme: %v", err)
	}
	if err := deploymentsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("register deployments scheme: %v", err)
	}
	return s
}

// newEnvtestCRWriter builds a CRWriter backed by the shared envtest bootstrap.
// Returns the crdmgrtesting.Env (for direct client access) and the CRWriter.
func newEnvtestCRWriter(t *testing.T) (*crdmgrtesting.Env, *CRWriter) {
	t.Helper()
	env := crdmgrtesting.StartManager(t, crdmgrtesting.Options{
		Scheme: testDeploymentScheme(t),
		InformerObjects: []ctrlclient.Object{
			&deploymentsv1alpha1.Deployment{},
		},
	})
	if env == nil {
		t.SkipNow()
	}
	r := &resolver.Resolver{ProjectPrefix: "prj-"}
	return env, NewCRWriter(env.Client, r)
}

// ensureProjectNamespace creates the project namespace in the envtest
// apiserver when it does not already exist. Deployment CRs are namespaced
// (the resolver maps a project name to a "prj-<project>" namespace), so the
// namespace must exist before any CR write.
func ensureProjectNamespace(t *testing.T, c ctrlclient.Client, project string) {
	t.Helper()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "prj-" + project},
	}
	if err := c.Create(context.Background(), ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace prj-%s: %v", project, err)
	}
}

// eventuallyGetCR polls the direct client until the named Deployment CR
// appears or the deadline expires. Necessary because the SSA patch applied by
// CRWriter goes through the apiserver; the cache-backed read might lag by one
// watch event.
func eventuallyGetCR(t *testing.T, c ctrlclient.Client, ns, name string) *deploymentsv1alpha1.Deployment {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	key := types.NamespacedName{Namespace: ns, Name: name}
	for {
		var dep deploymentsv1alpha1.Deployment
		if err := c.Get(context.Background(), key, &dep); err == nil {
			return &dep
		} else if !apierrors.IsNotFound(err) {
			t.Fatalf("unexpected error fetching CR %s/%s: %v", ns, name, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("CR %s/%s did not appear within deadline", ns, name)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// eventuallyAbsentCR polls the direct client until the named Deployment CR
// is gone or the deadline expires.
func eventuallyAbsentCR(t *testing.T, c ctrlclient.Client, ns, name string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	key := types.NamespacedName{Namespace: ns, Name: name}
	for {
		var dep deploymentsv1alpha1.Deployment
		err := c.Get(context.Background(), key, &dep)
		if apierrors.IsNotFound(err) {
			return
		}
		if err != nil {
			t.Fatalf("unexpected error checking absence of CR %s/%s: %v", ns, name, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("CR %s/%s was not deleted within deadline", ns, name)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestCRWriter_ApplyOnCreate_CreatesMatchingCR verifies that after
// ApplyOnCreate the Deployment CR exists with spec fields matching the
// proto-store inputs.
func TestCRWriter_ApplyOnCreate_CreatesMatchingCR(t *testing.T) {
	env, w := newEnvtestCRWriter(t)
	project := "cr-create"
	ensureProjectNamespace(t, env.Direct, project)

	if err := w.ApplyOnCreate(
		context.Background(),
		project, "web-app",
		"nginx", "latest",
		"default-template",
		"Web App", "A web application",
		nil, nil, nil,
		8080,
	); err != nil {
		t.Fatalf("ApplyOnCreate: %v", err)
	}

	cr := eventuallyGetCR(t, env.Direct, "prj-"+project, "web-app")

	if cr.Spec.ProjectName != project {
		t.Errorf("spec.projectName=%q want %q", cr.Spec.ProjectName, project)
	}
	if cr.Spec.Image != "nginx" {
		t.Errorf("spec.image=%q want %q", cr.Spec.Image, "nginx")
	}
	if cr.Spec.Tag != "latest" {
		t.Errorf("spec.tag=%q want %q", cr.Spec.Tag, "latest")
	}
	if cr.Spec.DisplayName != "Web App" {
		t.Errorf("spec.displayName=%q want %q", cr.Spec.DisplayName, "Web App")
	}
	if cr.Spec.Port != 8080 {
		t.Errorf("spec.port=%d want 8080", cr.Spec.Port)
	}
}

// TestCRWriter_ApplyOnUpdate_UpdatesCR verifies that after ApplyOnUpdate the
// Deployment CR spec reflects the new values.
func TestCRWriter_ApplyOnUpdate_UpdatesCR(t *testing.T) {
	env, w := newEnvtestCRWriter(t)
	project := "cr-update"
	ensureProjectNamespace(t, env.Direct, project)

	// Seed with initial values.
	if err := w.ApplyOnCreate(context.Background(), project, "api-svc", "my-image", "v1", "tmpl", "", "", nil, nil, nil, 9000); err != nil {
		t.Fatalf("ApplyOnCreate (seed): %v", err)
	}
	eventuallyGetCR(t, env.Direct, "prj-"+project, "api-svc")

	// Update with new image and tag.
	if err := w.ApplyOnUpdate(context.Background(), project, "api-svc", "my-image", "v2", "tmpl", "", "", nil, nil, 9000); err != nil {
		t.Fatalf("ApplyOnUpdate: %v", err)
	}

	// Poll until the CR reflects the new tag.
	deadline := time.Now().Add(10 * time.Second)
	key := types.NamespacedName{Namespace: "prj-" + project, Name: "api-svc"}
	for {
		var cr deploymentsv1alpha1.Deployment
		if err := env.Direct.Get(context.Background(), key, &cr); err != nil {
			t.Fatalf("re-get CR after update: %v", err)
		}
		if cr.Spec.Tag == "v2" {
			return // pass
		}
		if time.Now().After(deadline) {
			t.Fatalf("CR spec.tag did not update to %q within deadline; got %q", "v2", cr.Spec.Tag)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestCRWriter_DeleteCR_RemovesCR verifies that after DeleteCR the Deployment
// CR is gone from the apiserver.
func TestCRWriter_DeleteCR_RemovesCR(t *testing.T) {
	env, w := newEnvtestCRWriter(t)
	project := "cr-delete"
	ensureProjectNamespace(t, env.Direct, project)

	// Seed a CR.
	if err := w.ApplyOnCreate(context.Background(), project, "svc-to-delete", "img", "v1", "tmpl", "", "", nil, nil, nil, 0); err != nil {
		t.Fatalf("ApplyOnCreate (seed): %v", err)
	}
	eventuallyGetCR(t, env.Direct, "prj-"+project, "svc-to-delete")

	// Delete it.
	if err := w.DeleteCR(context.Background(), project, "svc-to-delete"); err != nil {
		t.Fatalf("DeleteCR: %v", err)
	}
	eventuallyAbsentCR(t, env.Direct, "prj-"+project, "svc-to-delete")
}

// TestCRWriter_DeleteCR_NotFoundIsIdempotent verifies that calling DeleteCR
// when the CR does not exist returns no error. This covers the lazy-creation
// invariant: old proto-store records that were never dual-written must
// not block deletion.
func TestCRWriter_DeleteCR_NotFoundIsIdempotent(t *testing.T) {
	env, w := newEnvtestCRWriter(t)
	project := "cr-delete-notfound"
	ensureProjectNamespace(t, env.Direct, project)

	// DeleteCR on a non-existent CR should not return an error.
	if err := w.DeleteCR(context.Background(), project, "does-not-exist"); err != nil {
		t.Fatalf("DeleteCR on absent CR returned error: %v", err)
	}
}

// TestCRWriter_OwnerRefsPreservedAcrossUpdate verifies that ownerReferences
// written onto the Deployment CR by an external actor (simulating a
// dependency reconciler from Phase 5–6) are not removed by a subsequent
// ApplyOnUpdate call.
//
// SSA field-manager isolation is the mechanism: the CRWriter owns spec.* via
// the "holos-console-deployment-writer" field manager; ownerReferences are
// owned by the reconciler's field manager. A Force=true SSA apply for one
// manager cannot clear fields managed by another.
func TestCRWriter_OwnerRefsPreservedAcrossUpdate(t *testing.T) {
	env, w := newEnvtestCRWriter(t)
	project := "cr-ownerref"
	ensureProjectNamespace(t, env.Direct, project)

	// Step 1: create the CR via the writer.
	if err := w.ApplyOnCreate(context.Background(), project, "svc", "img", "v1", "tmpl", "", "", nil, nil, nil, 0); err != nil {
		t.Fatalf("ApplyOnCreate: %v", err)
	}
	cr := eventuallyGetCR(t, env.Direct, "prj-"+project, "svc")

	// Step 2: an external actor (dependency reconciler) adds an ownerReference
	// via a direct Patch to simulate Phase 5–6 behavior. We use the same SSA
	// patch mechanism with a different field manager so the ownership is
	// correctly attributed.
	ownerUID := types.UID("fake-owner-uid-1")
	// Use MergeFrom patch to add an ownerReference without clobbering the spec.
	// In a real reconciler this would be an SSA patch; for the test a strategic
	// merge patch on metadata is simpler and equivalent for ownerRef mutation.
	patchedCR := cr.DeepCopy()
	patchedCR.OwnerReferences = append(patchedCR.OwnerReferences, metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       "parent-cm",
		UID:        ownerUID,
	})
	if err := env.Direct.Update(context.Background(), patchedCR); err != nil {
		t.Fatalf("adding ownerReference: %v", err)
	}

	// Step 3: apply an update through the CRWriter (simulates a
	// deployments-service update).
	if err := w.ApplyOnUpdate(context.Background(), project, "svc", "img", "v2", "tmpl", "", "", nil, nil, 0); err != nil {
		t.Fatalf("ApplyOnUpdate: %v", err)
	}

	// Step 4: assert the ownerReference survived the update.
	deadline := time.Now().Add(10 * time.Second)
	key := types.NamespacedName{Namespace: "prj-" + project, Name: "svc"}
	for {
		var got deploymentsv1alpha1.Deployment
		if err := env.Direct.Get(context.Background(), key, &got); err != nil {
			t.Fatalf("re-get after update: %v", err)
		}
		if got.Spec.Tag != "v2" {
			// Update has not landed yet; keep polling.
			if time.Now().After(deadline) {
				t.Fatalf("CR spec.tag did not update to v2 within deadline")
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		// Update landed — now check ownerReferences.
		found := false
		for _, ref := range got.OwnerReferences {
			if ref.UID == ownerUID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ownerReference (uid=%s) was removed by ApplyOnUpdate; ownerRefs=%+v", ownerUID, got.OwnerReferences)
		}
		return // pass
	}
}

// TestCRWriter_LazyCreate_ApplyOnUpdateCreatesAbsentCR verifies that calling
// ApplyOnUpdate when the CR does not yet exist creates it (lazy creation per
// ADR Decision 12). This is the migration path: a Deployment already in the
// proto-store that was created before the dual-write was wired gets its CR
// lazily on the next update.
func TestCRWriter_LazyCreate_ApplyOnUpdateCreatesAbsentCR(t *testing.T) {
	env, w := newEnvtestCRWriter(t)
	project := "cr-lazy"
	ensureProjectNamespace(t, env.Direct, project)

	// No prior Create call — simulate an existing proto-store record.
	if err := w.ApplyOnUpdate(context.Background(), project, "legacy-svc", "img", "v1", "tmpl", "Legacy", "Desc", nil, nil, 0); err != nil {
		t.Fatalf("ApplyOnUpdate (lazy create): %v", err)
	}

	cr := eventuallyGetCR(t, env.Direct, "prj-"+project, "legacy-svc")
	if cr.Spec.Image != "img" {
		t.Errorf("lazy-created CR spec.image=%q want %q", cr.Spec.Image, "img")
	}
}
