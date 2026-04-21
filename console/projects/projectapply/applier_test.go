/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package projectapply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientgotesting "k8s.io/client-go/testing"

	"github.com/holos-run/holos-console/console/templates"
)

// fakeApplierScheme registers every GVK the applier_test.go fixtures
// produce so the dynamicfake client can route actions correctly.
// Mirrors console/deployments/apply_test.go's fakeDynamicScheme.
func fakeApplierScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	register := func(gvk schema.GroupVersionKind) {
		s.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		s.AddKnownTypeWithName(schema.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind + "List"}, &unstructured.UnstructuredList{})
	}
	register(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"})
	register(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	register(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ServiceAccount"})
	register(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"})
	register(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"})
	register(schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1beta1", Kind: "ReferenceGrant"})
	return s
}

// newFakeApplier wires an Applier against a dynamicfake client with
// patch-succeeds-on-missing-object reactor (SSA semantics on the real
// apiserver create-or-update; the fake client's default patch reactor
// does not). Returns the underlying fake so tests can install extra
// reactors (for transient-failure simulation) and assert actions.
func newFakeApplier(t *testing.T, objects ...runtime.Object) (*Applier, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	fake := dynamicfake.NewSimpleDynamicClient(fakeApplierScheme(), objects...)
	fake.PrependReactor("patch", "*", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.Unstructured{}, nil
	})
	return NewApplier(fake, DefaultGVRResolver{}), fake
}

// ns builds an unstructured Namespace at a well-known name. The test
// result payloads use this for both the base and the "applied" shape.
func ns(name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("v1")
	u.SetKind("Namespace")
	u.SetName(name)
	return u
}

// clusterRole builds a cluster-scoped ClusterRole for the
// ClusterScoped-applied-first assertion.
func clusterRole(name string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion("rbac.authorization.k8s.io/v1")
	u.SetKind("ClusterRole")
	u.SetName(name)
	return u
}

// namespacedCM builds a namespace-scoped ConfigMap in the given
// namespace. The applier's namespaced loop consumes this.
func namespacedCM(name, namespace string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion("v1")
	u.SetKind("ConfigMap")
	u.SetName(name)
	u.SetNamespace(namespace)
	return u
}

// activeNS returns an unstructured Namespace with status.phase=Active
// so the fake client's Get reactor can return a ready namespace.
func activeNS(name string) *unstructured.Unstructured {
	u := ns(name)
	_ = unstructured.SetNestedField(u.Object, string(corev1.NamespaceActive), "status", "phase")
	return u
}

// installActivePhaseGetReactor makes the fake client return an Active
// namespace for any Get against the namespaces resource. The default
// fake-client reactor would return the empty object produced by the
// patch reactor (no status), which never reaches Active.
func installActivePhaseGetReactor(fake *dynamicfake.FakeDynamicClient, name string) {
	fake.PrependReactor("get", "namespaces", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		getAction, ok := action.(clientgotesting.GetAction)
		if !ok {
			return false, nil, nil
		}
		if getAction.GetName() != name {
			return false, nil, nil
		}
		return true, activeNS(name), nil
	})
}

// TestApplier_Apply_HappyPath covers AC (a): cluster-scoped, then
// Namespace, then namespace-scoped, all applied in order against a
// namespace that is already Active on the first poll.
func TestApplier_Apply_HappyPath(t *testing.T) {
	const nsName = "holos-prj-happy"
	applier, fake := newFakeApplier(t)
	installActivePhaseGetReactor(fake, nsName)

	result := &templates.ProjectNamespaceRenderResult{
		Namespace:       ns(nsName),
		ClusterScoped:   []unstructured.Unstructured{clusterRole("prj-reader")},
		NamespaceScoped: []unstructured.Unstructured{namespacedCM("settings", nsName)},
	}

	if err := applier.Apply(context.Background(), result); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Assert apply order via the recorded actions on the fake client.
	patches := patchResources(fake.Actions())
	if got, want := patches, []string{"clusterroles", "namespaces", "configmaps"}; !stringSlicesEqual(got, want) {
		t.Fatalf("patch order = %v, want %v", got, want)
	}
}

// TestApplier_Apply_NamespaceReadyAfterBriefDelay covers AC (b): the
// Namespace is not Active on the first poll, but becomes Active after a
// short delay. The wait path should observe the transition and proceed
// to the namespaced apply. This stresses the PollUntilContextCancel
// branch in waitForNamespaceActive.
func TestApplier_Apply_NamespaceReadyAfterBriefDelay(t *testing.T) {
	const nsName = "holos-prj-delayed"
	applier, fake := newFakeApplier(t)

	// First two Gets return a Pending namespace (phase unset); the third
	// returns Active. Simulates the 250ms controller-reconcile latency
	// the namespace controller typically exhibits.
	var gets atomic.Int32
	fake.PrependReactor("get", "namespaces", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		n := gets.Add(1)
		if n < 3 {
			// No status yet — mirrors a freshly-created namespace the
			// controller has not reconciled yet.
			return true, ns(nsName), nil
		}
		return true, activeNS(nsName), nil
	})

	result := &templates.ProjectNamespaceRenderResult{
		Namespace:       ns(nsName),
		NamespaceScoped: []unstructured.Unstructured{namespacedCM("settings", nsName)},
	}

	if err := applier.Apply(context.Background(), result); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := gets.Load(); got < 3 {
		t.Fatalf("expected at least 3 Gets, got %d", got)
	}
}

// TestApplier_Apply_RetryOnTransientForbidden covers AC (c): the
// namespaced apply returns Forbidden for the first few attempts, then
// succeeds. Stresses retryNamespacedApply's exponential-backoff branch
// and the classifyTransient path.
func TestApplier_Apply_RetryOnTransientForbidden(t *testing.T) {
	const nsName = "holos-prj-forbidden"
	applier, fake := newFakeApplier(t)
	installActivePhaseGetReactor(fake, nsName)

	// First three patches of the namespaced ConfigMap fail Forbidden;
	// the fourth succeeds. 4 attempts is well under the Backoff.Steps
	// ceiling so the retry terminates via success, not Steps exhaustion.
	var patches atomic.Int32
	fake.PrependReactor("patch", "configmaps", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		n := patches.Add(1)
		if n <= 3 {
			return true, nil, apierrors.NewForbidden(
				schema.GroupResource{Group: "", Resource: "configmaps"},
				"settings",
				errors.New("rbac not yet propagated"),
			)
		}
		return true, &unstructured.Unstructured{}, nil
	})

	result := &templates.ProjectNamespaceRenderResult{
		Namespace:       ns(nsName),
		NamespaceScoped: []unstructured.Unstructured{namespacedCM("settings", nsName)},
	}

	start := time.Now()
	if err := applier.Apply(context.Background(), result); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	elapsed := time.Since(start)

	if got := patches.Load(); got != 4 {
		t.Fatalf("expected 4 patch attempts (3 Forbidden + 1 success), got %d", got)
	}

	// Exponential backoff at 250ms * 2^n with Jitter means the cumulative
	// delay for 3 retries is at least 250+500+1000 = 1750ms. The test is
	// tolerant (1000ms floor) to absorb Jitter asymmetry, but confirms
	// the backoff actually ran instead of tight-looping.
	if elapsed < 1000*time.Millisecond {
		t.Fatalf("expected exponential backoff to elapse >= 1s, got %v", elapsed)
	}
}

// TestApplier_Apply_DeadlineExceededOnStubbornForbidden covers AC (d):
// a namespaced apply that never succeeds within the retry window
// produces a *DeadlineExceededError carrying the captured Forbidden
// classifier. The RPC layer uses the struct fields to map to
// connect.CodeDeadlineExceeded with an operator-actionable message.
func TestApplier_Apply_DeadlineExceededOnStubbornForbidden(t *testing.T) {
	const nsName = "holos-prj-stubborn"
	applier, fake := newFakeApplier(t)
	installActivePhaseGetReactor(fake, nsName)

	// Every patch fails Forbidden.
	var patches atomic.Int32
	fake.PrependReactor("patch", "configmaps", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		patches.Add(1)
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Group: "", Resource: "configmaps"},
			"settings",
			errors.New("rbac never propagates"),
		)
	})

	// Short caller deadline so the test does not wait the full 30s
	// ceiling. The retry loop takes min(caller-ctx, 30s), so a 1s
	// caller deadline forces a 1s retry window.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	result := &templates.ProjectNamespaceRenderResult{
		Namespace:       ns(nsName),
		NamespaceScoped: []unstructured.Unstructured{namespacedCM("settings", nsName)},
	}

	err := applier.Apply(ctx, result)
	if err == nil {
		t.Fatal("Apply: want error, got nil")
	}

	// The RPC layer uses errors.As to reach the structured error. Confirm
	// the wrap chain preserves it.
	var dErr *DeadlineExceededError
	if !errors.As(err, &dErr) {
		t.Fatalf("Apply error = %v (%T), want *DeadlineExceededError in chain", err, err)
	}
	if dErr.Kind != "ConfigMap" {
		t.Errorf("DeadlineExceededError.Kind = %q, want %q", dErr.Kind, "ConfigMap")
	}
	if dErr.Name != "settings" {
		t.Errorf("DeadlineExceededError.Name = %q, want %q", dErr.Name, "settings")
	}
	if dErr.Namespace != nsName {
		t.Errorf("DeadlineExceededError.Namespace = %q, want %q", dErr.Namespace, nsName)
	}
	if dErr.Classifier != "Forbidden" {
		t.Errorf("DeadlineExceededError.Classifier = %q, want %q", dErr.Classifier, "Forbidden")
	}
	if dErr.Attempts < 1 {
		t.Errorf("DeadlineExceededError.Attempts = %d, want >= 1", dErr.Attempts)
	}
	if dErr.LastError == nil || !apierrors.IsForbidden(dErr.LastError) {
		t.Errorf("DeadlineExceededError.LastError = %v, want Forbidden", dErr.LastError)
	}
	// The RPC layer relies on errors.Is(err, context.DeadlineExceeded)
	// for generic deadline handling.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false, want true")
	}
}

// TestApplier_Apply_NonTransientErrorFailsFast locks in that a
// non-transient error (e.g. Invalid) does NOT enter the retry loop. A
// regression that treated every apierrors class as retryable would hang
// the RPC for the full 30s ceiling on a bad template — exactly the
// operator-visible behaviour ADR 034 Decision 4 calls out.
func TestApplier_Apply_NonTransientErrorFailsFast(t *testing.T) {
	const nsName = "holos-prj-invalid"
	applier, fake := newFakeApplier(t)
	installActivePhaseGetReactor(fake, nsName)

	var patches atomic.Int32
	fake.PrependReactor("patch", "configmaps", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		patches.Add(1)
		return true, nil, apierrors.NewBadRequest("invalid ConfigMap: data must be a map")
	})

	result := &templates.ProjectNamespaceRenderResult{
		Namespace:       ns(nsName),
		NamespaceScoped: []unstructured.Unstructured{namespacedCM("settings", nsName)},
	}

	start := time.Now()
	err := applier.Apply(context.Background(), result)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Apply: want error, got nil")
	}
	if patches.Load() != 1 {
		t.Errorf("non-transient errors must not retry: attempts = %d, want 1", patches.Load())
	}
	// Fail-fast means we return within a fraction of a second, nowhere
	// near the 30s ceiling.
	if elapsed > 2*time.Second {
		t.Errorf("non-transient error took %v to surface, want <2s (fail-fast)", elapsed)
	}
}

// TestApplier_Apply_NamespaceNotActiveDeadlineSurfacesStructuredError
// covers the wait-path deadline branch: the Namespace exists but the
// namespace controller has never observed it (no status.phase set), so
// the poll keeps going until the caller deadline fires. Returns a
// *DeadlineExceededError with Kind="Namespace" so the RPC surface can
// map it to connect.CodeDeadlineExceeded.
func TestApplier_Apply_NamespaceNotActiveDeadlineSurfacesStructuredError(t *testing.T) {
	const nsName = "holos-prj-never-observed"
	applier, fake := newFakeApplier(t)

	// No .status.phase ever set — mirrors the window between Create and
	// the first namespace-controller reconcile.
	fake.PrependReactor("get", "namespaces", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, ns(nsName), nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	result := &templates.ProjectNamespaceRenderResult{
		Namespace: ns(nsName),
	}
	err := applier.Apply(ctx, result)
	if err == nil {
		t.Fatal("Apply: want error, got nil")
	}
	var dErr *DeadlineExceededError
	if !errors.As(err, &dErr) {
		t.Fatalf("Apply error = %v (%T), want *DeadlineExceededError", err, err)
	}
	if dErr.Kind != "Namespace" {
		t.Errorf("DeadlineExceededError.Kind = %q, want %q", dErr.Kind, "Namespace")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false, want true")
	}
}

// TestApplier_Apply_NamespaceTerminatingFailsFast pins the wait-path
// fail-fast branch: a Namespace observed as Terminating is a real
// operator problem, not a transient. Polling into it until the deadline
// would convert a clear failure into a confusing DeadlineExceeded.
func TestApplier_Apply_NamespaceTerminatingFailsFast(t *testing.T) {
	const nsName = "holos-prj-terminating-fast"
	applier, fake := newFakeApplier(t)

	fake.PrependReactor("get", "namespaces", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		u := ns(nsName)
		_ = unstructured.SetNestedField(u.Object, string(corev1.NamespaceTerminating), "status", "phase")
		return true, u, nil
	})

	start := time.Now()
	err := applier.Apply(context.Background(), &templates.ProjectNamespaceRenderResult{Namespace: ns(nsName)})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Apply: want error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Terminating namespace took %v to surface, want <2s (fail-fast)", elapsed)
	}
	if !strings.Contains(err.Error(), "Terminating") {
		t.Errorf("Error() = %q, want substring %q", err.Error(), "Terminating")
	}
}

// TestApplier_Apply_NilInputsRejected pins the guard on the public API:
// nil result or nil result.Namespace surface as descriptive errors.
func TestApplier_Apply_NilInputsRejected(t *testing.T) {
	applier, _ := newFakeApplier(t)

	if err := applier.Apply(context.Background(), nil); err == nil {
		t.Error("Apply(nil) = nil, want error")
	}
	if err := applier.Apply(context.Background(), &templates.ProjectNamespaceRenderResult{}); err == nil {
		t.Error("Apply(&{Namespace: nil}) = nil, want error")
	}
}

// TestApplier_Apply_EmptyResult pins the trivial-input path: no
// cluster-scoped, no namespaced, only the Namespace. This is the
// simplest shape a CreateProject call with zero matched bindings
// produces (after HOL-810 render). It must succeed.
func TestApplier_Apply_EmptyResult(t *testing.T) {
	const nsName = "holos-prj-empty"
	applier, fake := newFakeApplier(t)
	installActivePhaseGetReactor(fake, nsName)

	err := applier.Apply(context.Background(), &templates.ProjectNamespaceRenderResult{Namespace: ns(nsName)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Exactly one patch: the Namespace itself.
	patches := patchResources(fake.Actions())
	if got, want := patches, []string{"namespaces"}; !stringSlicesEqual(got, want) {
		t.Fatalf("patch actions = %v, want %v", got, want)
	}
}

// TestApplier_ClassifyTransient covers each classifier in the
// retryableStatusCheckers table so a regression that drops one is
// surfaced immediately.
func TestApplier_ClassifyTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "NotFound",
			err:  apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "x"),
			want: "NotFound",
		},
		{
			name: "Forbidden",
			err:  apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "x", errors.New("rbac")),
			want: "Forbidden",
		},
		{
			name: "ServerTimeout",
			err:  apierrors.NewServerTimeout(schema.GroupResource{Resource: "configmaps"}, "patch", 1),
			want: "ServerTimeout",
		},
		{
			name: "InternalError",
			err:  apierrors.NewInternalError(errors.New("etcd hiccup")),
			want: "InternalError",
		},
		{
			name: "non-transient BadRequest is not classified",
			err:  apierrors.NewBadRequest("nope"),
			want: "",
		},
		{
			name: "nil is not classified",
			err:  nil,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := classifyTransient(tc.err)
			if tc.want == "" {
				if ok {
					t.Errorf("classifyTransient = (%q, true), want (_, false)", got)
				}
				return
			}
			if !ok || got != tc.want {
				t.Errorf("classifyTransient = (%q, %v), want (%q, true)", got, ok, tc.want)
			}
		})
	}
}

// TestApplier_Apply_ContextCanceledPropagates is a regression test for
// the cancellation branch: when the caller cancels the context while a
// transient retry is in flight, the returned error must unwrap to
// context.Canceled — not to a DeadlineExceededError. This matters for
// upstream RPC middleware that maps Canceled vs DeadlineExceeded
// differently.
func TestApplier_Apply_ContextCanceledPropagates(t *testing.T) {
	const nsName = "holos-prj-canceled"
	applier, fake := newFakeApplier(t)
	installActivePhaseGetReactor(fake, nsName)

	fake.PrependReactor("patch", "configmaps", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Resource: "configmaps"}, "x", errors.New("forever"))
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	err := applier.Apply(ctx, &templates.ProjectNamespaceRenderResult{
		Namespace:       ns(nsName),
		NamespaceScoped: []unstructured.Unstructured{namespacedCM("settings", nsName)},
	})
	if err == nil {
		t.Fatal("Apply: want error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false, want true (err=%v)", err)
	}
}

// TestApplier_Apply_ResolverError pins the bad-kind path: a resource
// with a GVK the resolver does not know about surfaces a descriptive
// error rather than a generic patch error. In production the render
// layer (HOL-810) validates the kinds, so hitting this in CreateProject
// indicates either a registry drift or a bug.
func TestApplier_Apply_ResolverError(t *testing.T) {
	const nsName = "holos-prj-badkind"
	applier, fake := newFakeApplier(t)
	installActivePhaseGetReactor(fake, nsName)

	// Inject an unknown kind into the cluster-scoped bucket.
	unknown := unstructured.Unstructured{}
	unknown.SetAPIVersion("example.io/v1")
	unknown.SetKind("Weird")
	unknown.SetName("x")

	err := applier.Apply(context.Background(), &templates.ProjectNamespaceRenderResult{
		Namespace:     ns(nsName),
		ClusterScoped: []unstructured.Unstructured{unknown},
	})
	if err == nil {
		t.Fatal("Apply: want error, got nil")
	}
	if !strings.Contains(err.Error(), "Weird") {
		t.Errorf("Error() = %q, want substring %q", err.Error(), "Weird")
	}
}

// patchResources returns the resource names of all Patch actions
// recorded on the fake client, in order, so a test can assert apply
// sequencing (ClusterScoped → Namespace → NamespaceScoped).
func patchResources(actions []clientgotesting.Action) []string {
	var out []string
	for _, a := range actions {
		if a.GetVerb() != "patch" {
			continue
		}
		out = append(out, a.GetResource().Resource)
	}
	return out
}

// stringSlicesEqual compares two string slices for exact equality.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestApplier_DeadlineExceededErrorFormatting pins the Error() strings
// so operator-facing messages stay stable. The RPC layer (HOL-812) may
// inline the Error() string in the connect.Error detail.
func TestApplier_DeadlineExceededErrorFormatting(t *testing.T) {
	nsErr := &DeadlineExceededError{Kind: "Namespace", Name: "foo", LastPhase: "Pending"}
	if got, want := nsErr.Error(), `deadline exceeded waiting for Namespace "foo" to reach Active (last phase: "Pending")`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	applyErr := &DeadlineExceededError{Kind: "ConfigMap", Namespace: "ns", Name: "n", Attempts: 7, Classifier: "Forbidden", LastError: fmt.Errorf("rbac")}
	if got := applyErr.Error(); !strings.Contains(got, "ConfigMap/ns/n") || !strings.Contains(got, "7 attempts") || !strings.Contains(got, "Forbidden") {
		t.Errorf("Error() = %q, missing expected substrings", got)
	}
}
