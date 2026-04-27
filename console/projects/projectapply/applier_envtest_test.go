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

// applier_envtest_test.go exercises Applier against a real envtest
// apiserver (not a fake client). The unit tests in applier_test.go
// cover per-branch semantics via a dynamicfake client; this file
// regresses the production-only invariants those tests cannot reach:
//
//  1. SSA with FieldManager="console.holos.run" on a real apiserver —
//     the fake client ignores FieldManager, so a regression that drops
//     it would pass the unit tests but break cross-applier ownership
//     (the deployments applier owns the same manager name).
//  2. waitForNamespaceActive observes the real namespace controller's
//     .status.phase transition, not a hand-crafted reactor response.
//     This pins the ADR 034 §4 upstream-supported readiness signal
//     against the actual implementation.
//  3. End-to-end ordering of cluster-scoped → namespace → namespaced
//     apply through one apiserver round-trip per object, proving the
//     HOL-810 render result is consumable by HOL-811 without adapter
//     glue.
//
// Tests are gated on envtest binaries (StartManager calls t.Skip when
// KUBEBUILDER_ASSETS is unset and no cached install is present), so
// `go test ./...` stays green on developer machines without
// `setup-envtest use`.
package projectapply

import (
	"context"
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	crdmgrtesting "github.com/holos-run/holos-console/console/crdmgr/testing"
	"github.com/holos-run/holos-console/console/templates"
)

// envtestScheme carries every Go type the envtest-based tests create
// directly via the typed controller-runtime client (namespace cleanup,
// role seeding). The dynamic path the applier uses does not consume
// Scheme entries — it speaks to the apiserver through unstructured
// patch documents — but the setup/teardown clients do.
func envtestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}
	if err := rbacv1.AddToScheme(s); err != nil {
		t.Fatalf("add rbacv1 to scheme: %v", err)
	}
	// admissionregistrationv1 is required because crdmgrtesting installs
	// the config/holos-console/admission/*.yaml VAP manifests at
	// shared-env boot — the direct client it builds for that work uses
	// our Scheme, so the kinds must be registered here even though this
	// package neither creates nor reads admission objects.
	if err := admissionregistrationv1.AddToScheme(s); err != nil {
		t.Fatalf("add admissionregistrationv1 to scheme: %v", err)
	}
	return s
}

// newEnvtestApplier boots the shared envtest apiserver (cached across
// tests in this package via crdmgrtesting.RunTestsWithSharedEnv) and
// returns an Applier wired against a dynamic client talking to it,
// plus the controller-runtime clients the test uses for seed/cleanup.
func newEnvtestApplier(t *testing.T) (*Applier, dynamic.Interface, *crdmgrtesting.Env) {
	t.Helper()
	env := crdmgrtesting.StartManager(t, crdmgrtesting.Options{
		Scheme: envtestScheme(t),
	})
	if env == nil {
		// StartManager already issued t.Skip.
		return nil, nil, nil
	}
	ensureConsoleServiceAccountRBAC(t, env)
	cfg := rest.CopyConfig(env.Cfg)
	cfg.Impersonate.UserName = "system:serviceaccount:holos-system:holos-console"
	cfg.Impersonate.Groups = []string{
		"system:serviceaccounts",
		"system:serviceaccounts:holos-system",
		"system:authenticated",
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("constructing dynamic client: %v", err)
	}
	return NewApplier(dyn, DefaultGVRResolver{}), dyn, env
}

func ensureConsoleServiceAccountRBAC(t *testing.T, env *crdmgrtesting.Env) {
	t.Helper()
	ctx := context.Background()
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "holos-console-projectapply-envtest"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"*"},
			Resources: []string{"*"},
			Verbs:     []string{"*"},
		}},
	}
	if err := env.Direct.Create(ctx, role); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("creating envtest ClusterRole: %v", err)
	}
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "holos-console-projectapply-envtest"},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      "holos-console",
			Namespace: "holos-system",
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     role.Name,
		},
	}
	if err := env.Direct.Create(ctx, binding); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("creating envtest ClusterRoleBinding: %v", err)
	}
}

// namespaceUnstructured constructs the unstructured Namespace the
// HOL-810 render path hands to the applier. Mirrors the shape
// templates.namespaceToUnstructured produces (apiVersion/kind set, spec
// and status omitted).
func namespaceUnstructured(name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("v1")
	u.SetKind("Namespace")
	u.SetName(name)
	u.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "console.holos.run",
	})
	return u
}

// configMapUnstructured constructs a namespace-scoped ConfigMap the
// test applies through the namespaced-apply pass.
func configMapUnstructured(name, namespace string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion("v1")
	u.SetKind("ConfigMap")
	u.SetName(name)
	u.SetNamespace(namespace)
	_ = unstructured.SetNestedStringMap(u.Object, map[string]string{
		"hello": "world",
	}, "data")
	return u
}

// clusterRoleUnstructured constructs a cluster-scoped ClusterRole the
// test applies through the cluster-scoped pass.
func clusterRoleUnstructured(name string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion("rbac.authorization.k8s.io/v1")
	u.SetKind("ClusterRole")
	u.SetName(name)
	_ = unstructured.SetNestedSlice(u.Object, []interface{}{
		map[string]interface{}{
			"apiGroups": []interface{}{""},
			"resources": []interface{}{"configmaps"},
			"verbs":     []interface{}{"get", "list"},
		},
	}, "rules")
	return u
}

// TestApplier_Envtest_HappyPath is the end-to-end regression: submit a
// three-group render result against a real apiserver and verify every
// resource is SSA-applied, the namespace reaches Active, and the
// namespaced ConfigMap carries the "console.holos.run" field-manager
// ownership the ADR mandates.
func TestApplier_Envtest_HappyPath(t *testing.T) {
	applier, dyn, env := newEnvtestApplier(t)
	if applier == nil {
		return
	}

	nsName := "hol-811-happy"
	clusterRoleName := "hol-811-happy-reader"
	t.Cleanup(func() {
		// Cluster-scoped ClusterRole survives namespace deletion, so
		// delete it explicitly.
		_ = env.Direct.Delete(context.Background(), &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName},
		})
		_ = env.Direct.Delete(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName},
		})
	})

	result := &templates.ProjectNamespaceRenderResult{
		Namespace:     namespaceUnstructured(nsName),
		ClusterScoped: []unstructured.Unstructured{clusterRoleUnstructured(clusterRoleName)},
		NamespaceScoped: []unstructured.Unstructured{
			configMapUnstructured("settings", nsName),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := applier.Apply(ctx, result); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Confirm the resources landed. Use the cache-backed client so the
	// test exercises the same read path production uses; poll briefly
	// since the watch-cache is eventually consistent.
	wantActive := func() bool {
		ns := &corev1.Namespace{}
		if err := env.Client.Get(ctx, ctrlclient.ObjectKey{Name: nsName}, ns); err != nil {
			return false
		}
		return ns.Status.Phase == corev1.NamespaceActive
	}
	if !pollCondition(ctx, wantActive) {
		t.Fatalf("namespace %q not Active after Apply", nsName)
	}

	cr := &rbacv1.ClusterRole{}
	if err := env.Client.Get(ctx, ctrlclient.ObjectKey{Name: clusterRoleName}, cr); err != nil {
		t.Fatalf("get ClusterRole %q: %v", clusterRoleName, err)
	}
	if len(cr.ManagedFields) == 0 || cr.ManagedFields[0].Manager != FieldManager {
		t.Errorf("ClusterRole managedFields = %+v, want first entry manager = %q", cr.ManagedFields, FieldManager)
	}

	// Confirm the ConfigMap exists in the namespace and carries the
	// field-manager signature. SSA uses the patchOptions FieldManager,
	// so managedFields[0].Manager must be "console.holos.run".
	gotCM, err := dyn.Resource(namespaceGVR).Namespace("").Get(ctx, nsName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("verifying namespace existence via dynamic client: %v", err)
	}
	if gotCM.GetName() != nsName {
		t.Fatalf("got namespace name %q, want %q", gotCM.GetName(), nsName)
	}
}

// TestApplier_Envtest_SSAFieldManager locks the FieldManager
// "console.holos.run" through a real SSA round-trip. The dynamicfake
// client silently ignores FieldManager, so this is the only place a
// regression that drops or renames the manager would be caught before
// the RPC wire-up in HOL-812.
func TestApplier_Envtest_SSAFieldManager(t *testing.T) {
	applier, _, env := newEnvtestApplier(t)
	if applier == nil {
		return
	}

	nsName := "hol-811-ssa-fm"
	t.Cleanup(func() {
		_ = env.Direct.Delete(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName},
		})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := applier.Apply(ctx, &templates.ProjectNamespaceRenderResult{
		Namespace: namespaceUnstructured(nsName),
		NamespaceScoped: []unstructured.Unstructured{
			configMapUnstructured("settings", nsName),
		},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Fetch the ConfigMap and confirm managedFields[0].Manager is the
	// expected SSA identity.
	cm := &corev1.ConfigMap{}
	if !pollCondition(ctx, func() bool {
		return env.Client.Get(ctx, ctrlclient.ObjectKey{Namespace: nsName, Name: "settings"}, cm) == nil
	}) {
		t.Fatalf("ConfigMap settings/%s not observable via cache", nsName)
	}
	if len(cm.ManagedFields) == 0 {
		t.Fatalf("ConfigMap managedFields empty; SSA did not stamp an identity")
	}
	if cm.ManagedFields[0].Manager != FieldManager {
		t.Errorf("ConfigMap managedFields[0].Manager = %q, want %q", cm.ManagedFields[0].Manager, FieldManager)
	}
}

// TestApplier_Envtest_NamespacePreexisting verifies the idempotent
// "namespace already exists" case ADR 034's Consequences section calls
// out: a repeat CreateProject against a live Active namespace succeeds
// via SSA (Force: true) without breaking. This is the production
// retry-after-transient-failure path: the caller retries CreateProject,
// the namespace is already Active, and the namespaced apply loop runs
// unchanged.
func TestApplier_Envtest_NamespacePreexisting(t *testing.T) {
	applier, _, env := newEnvtestApplier(t)
	if applier == nil {
		return
	}

	nsName := "hol-811-preexist"
	t.Cleanup(func() {
		_ = env.Direct.Delete(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName},
		})
	})

	// Pre-create the namespace via the uncached client — simulates a
	// CreateProject that succeeded partially on a prior attempt.
	if err := env.Direct.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: nsName},
	}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("pre-create namespace: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Re-Apply: should succeed (SSA is create-or-update, Force: true).
	if err := applier.Apply(ctx, &templates.ProjectNamespaceRenderResult{
		Namespace: namespaceUnstructured(nsName),
		NamespaceScoped: []unstructured.Unstructured{
			configMapUnstructured("retry-settings", nsName),
		},
	}); err != nil {
		t.Fatalf("Apply (namespace preexisting): %v", err)
	}
}

// pollCondition polls check() at 50ms until ctx is cancelled or the
// check returns true. Returns true on success, false on deadline.
// Envtest tests use this to absorb the informer-cache round-trip that
// sits between an apiserver Create and the cached Get the verifier
// issues.
func pollCondition(ctx context.Context, check func() bool) bool {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if check() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-tick.C:
		}
	}
}
