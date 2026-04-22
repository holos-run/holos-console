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

package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	istiosecurityv1beta1 "istio.io/api/security/v1beta1"
	istiosecurityv1 "istio.io/client-go/pkg/apis/security/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	secretsv1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
)

// bindingTestNamespace is the namespace every binding-controller test
// places its binding object into. A project-scoped namespace name is
// intentionally used because project-scope is the tightest admission
// path — resolution tests that want a parent or organization policy
// attach the relevant labels.
const bindingTestNamespace = "holos-prj-demo"

// validBinding returns a fully-populated SecretInjectionPolicyBinding
// rooted in the bindingTestNamespace project-scoped namespace. Shared
// by every positive and negative test case so individual tables only
// need to mutate the one field under test.
//
// Scope defaults to organization — the commonest v1alpha1 shape —
// callers that exercise folder-scope or same-namespace resolution
// override the spec field after construction.
func validBinding(name string, targets []secretsv1alpha1.TargetRef, policyNamespace, policyName string) *secretsv1alpha1.SecretInjectionPolicyBinding {
	return validBindingWithScope(name, targets, policyNamespace, policyName, secretsv1alpha1.PolicyRefScopeOrganization)
}

// validBindingWithScope is the scope-aware constructor used by the
// ancestry table. scope=organization narrows candidate namespaces to the
// synthesised holos-org-<org> entry plus the binding's own namespace;
// scope=folder narrows to the parent label plus own. See
// policyCandidateNamespaces for the contract.
func validBindingWithScope(name string, targets []secretsv1alpha1.TargetRef, policyNamespace, policyName string, scope secretsv1alpha1.PolicyRefScope) *secretsv1alpha1.SecretInjectionPolicyBinding {
	return &secretsv1alpha1.SecretInjectionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  bindingTestNamespace,
			Generation: 1,
			UID:        types.UID("test-uid-" + name),
		},
		Spec: secretsv1alpha1.SecretInjectionPolicyBindingSpec{
			PolicyRef: secretsv1alpha1.PolicyRef{
				Scope:     scope,
				Namespace: policyNamespace,
				Name:      policyName,
			},
			TargetRefs: targets,
		},
	}
}

// validPolicy returns a minimal SecretInjectionPolicy suitable for
// resolution tests. The policy is intentionally bare — the reconciler
// in this phase does not project any field from the policy onto the
// emitted AuthorizationPolicy (M3 populates the Match subset); as long
// as the object resolves, the Programmed path runs.
func validPolicy(name, namespace string) *secretsv1alpha1.SecretInjectionPolicy {
	return &secretsv1alpha1.SecretInjectionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: secretsv1alpha1.SecretInjectionPolicySpec{
			Direction: secretsv1alpha1.DirectionIngress,
			CallerAuth: secretsv1alpha1.CallerAuth{
				Type: secretsv1alpha1.AuthenticationTypeAPIKey,
			},
			UpstreamRef: secretsv1alpha1.UpstreamRef{
				Scope:     secretsv1alpha1.UpstreamScopeProject,
				ScopeName: "demo",
				Name:      "upstream-creds",
			},
		},
	}
}

// namespaceWithLabels returns a Namespace carrying the supplied
// console.holos.run/* labels so the resolvePolicy ancestor walk can
// follow the parent / organization chain during unit tests.
func namespaceWithLabels(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

// bindingTestScheme returns a fresh scheme populated with the types
// the binding reconciler touches: corev1 (Namespace), secretsv1alpha1
// (Binding + Policy), istiosecurityv1 (AuthorizationPolicy). Using a
// fresh scheme per test keeps the package-global Scheme from accumulating
// test-only registrations.
func bindingTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("register corev1: %v", err)
	}
	if err := secretsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("register secrets: %v", err)
	}
	if err := istiosecurityv1.AddToScheme(s); err != nil {
		t.Fatalf("register istio security: %v", err)
	}
	return s
}

// newBindingTestReconciler builds a SecretInjectionPolicyBindingReconciler
// backed by a fake controller-runtime client seeded with objs, with the
// status subresource registered for SecretInjectionPolicyBinding. A nil
// Recorder is deliberate — the reconciler must not panic when the event
// recorder is unset.
func newBindingTestReconciler(t *testing.T, objs ...client.Object) (*SecretInjectionPolicyBindingReconciler, client.Client) {
	t.Helper()
	s := bindingTestScheme(t)
	cli := ctrlfake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(&secretsv1alpha1.SecretInjectionPolicyBinding{}).
		Build()
	r := &SecretInjectionPolicyBindingReconciler{
		Client:      cli,
		Scheme:      s,
		TrustDomain: defaultBindingAuthzTrustDomain,
	}
	return r, cli
}

// newBindingTestReconcilerWithInterceptor is identical to
// newBindingTestReconciler but threads interceptor.Funcs through the
// fake client so a test can simulate a Create error path without having
// to stand up an envtest cluster. Used by the
// AuthorizationPolicyWriteFailed test.
func newBindingTestReconcilerWithInterceptor(t *testing.T, funcs interceptor.Funcs, objs ...client.Object) (*SecretInjectionPolicyBindingReconciler, client.Client) {
	t.Helper()
	s := bindingTestScheme(t)
	cli := ctrlfake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(&secretsv1alpha1.SecretInjectionPolicyBinding{}).
		WithInterceptorFuncs(funcs).
		Build()
	r := &SecretInjectionPolicyBindingReconciler{
		Client:      cli,
		Scheme:      s,
		TrustDomain: defaultBindingAuthzTrustDomain,
	}
	return r, cli
}

// requireCondition asserts the named condition on b is present with the
// expected status, and returns the condition so callers can assert on
// the Reason/Message fields. Mirrors the upstream_secret_controller_test
// helper so the assertions read identically.
func requireCondition(t *testing.T, b *secretsv1alpha1.SecretInjectionPolicyBinding, condType string, want metav1.ConditionStatus) metav1.Condition {
	t.Helper()
	c := meta.FindStatusCondition(b.Status.Conditions, condType)
	if c == nil {
		t.Fatalf("condition %q not set on binding; conditions=%+v", condType, b.Status.Conditions)
	}
	if c.Status != want {
		t.Fatalf("condition %q status=%s; want %s (reason=%s, message=%s)", condType, c.Status, want, c.Reason, c.Message)
	}
	return *c
}

// TestBinding_ResolvePolicy_Ancestry exercises the three admission-
// validated resolution paths mandated by HOL-752: same namespace,
// parent label, and synthesised organization namespace. Each subtest
// seeds one resolution path and asserts the Ready condition flips True
// and the AuthorizationPolicy appears with the expected owner reference.
func TestBinding_ResolvePolicy_Ancestry(t *testing.T) {
	targets := []secretsv1alpha1.TargetRef{{
		Kind:      secretsv1alpha1.TargetRefKindServiceAccount,
		Namespace: bindingTestNamespace,
		Name:      "demo-sa",
	}}

	cases := []struct {
		name            string
		nsLabels        map[string]string
		policyNamespace string
		scope           secretsv1alpha1.PolicyRefScope
	}{
		{
			// Same-namespace resolution. Scope defaults to organization
			// — a scope=organization binding in a project namespace
			// should still accept a co-located policy because the own
			// namespace is always a same-scope candidate (see
			// policyCandidateNamespaces).
			name:            "same-namespace resolves directly",
			nsLabels:        map[string]string{},
			policyNamespace: bindingTestNamespace,
			scope:           secretsv1alpha1.PolicyRefScopeOrganization,
		},
		{
			// Folder-scope resolution via the direct-parent label.
			name: "folder-scope resolves via console.holos.run/parent label",
			nsLabels: map[string]string{
				"console.holos.run/parent": "holos-fld-eng",
			},
			policyNamespace: "holos-fld-eng",
			scope:           secretsv1alpha1.PolicyRefScopeFolder,
		},
		{
			// Organization-scope resolution via the synthesised
			// holos-org-<organization> namespace.
			name: "organization-scope resolves via synthesised holos-org-<name>",
			nsLabels: map[string]string{
				"console.holos.run/organization": "acme",
			},
			policyNamespace: "holos-org-acme",
			scope:           secretsv1alpha1.PolicyRefScopeOrganization,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binding := validBindingWithScope("binding-a", targets, tc.policyNamespace, "policy-a", tc.scope)
			policy := validPolicy("policy-a", tc.policyNamespace)
			ns := namespaceWithLabels(bindingTestNamespace, tc.nsLabels)
			r, cli := newBindingTestReconciler(t, binding, policy, ns)

			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: binding.Namespace,
				Name:      binding.Name,
			}}
			if _, err := r.Reconcile(context.Background(), req); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}

			var out secretsv1alpha1.SecretInjectionPolicyBinding
			if err := cli.Get(context.Background(), req.NamespacedName, &out); err != nil {
				t.Fatalf("Get binding: %v", err)
			}
			requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionAccepted, metav1.ConditionTrue)
			requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionResolvedRefs, metav1.ConditionTrue)
			requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed, metav1.ConditionTrue)
			requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionReady, metav1.ConditionTrue)

			var ap istiosecurityv1.AuthorizationPolicy
			apKey := types.NamespacedName{
				Namespace: binding.Namespace,
				Name:      authorizationPolicyName(binding.Name),
			}
			if err := cli.Get(context.Background(), apKey, &ap); err != nil {
				t.Fatalf("Get AuthorizationPolicy: %v", err)
			}
			if !isOwnedByBinding(&ap, binding) {
				t.Fatalf("AuthorizationPolicy not owned by binding; owners=%+v", ap.OwnerReferences)
			}
			if ap.Spec.Action != istiosecurityv1beta1.AuthorizationPolicy_CUSTOM {
				t.Fatalf("AuthorizationPolicy.spec.action=%s; want CUSTOM", ap.Spec.Action)
			}
			if got := ap.Spec.GetProvider().GetName(); got != bindingAuthzProviderName {
				t.Fatalf("AuthorizationPolicy.spec.provider.name=%q; want %q", got, bindingAuthzProviderName)
			}
		})
	}
}

// TestBinding_ResolvePolicy_PolicyNotFound verifies the two
// PolicyNotFound surfaces: (1) spec.policyRef.namespace is in the
// ancestor chain but the policy object does not exist, and (2)
// spec.policyRef.namespace is OUTSIDE the ancestor chain (a binding
// that bypassed admission). Both surface ResolvedRefs=False/Reason=
// PolicyNotFound and suppress AP emission.
func TestBinding_ResolvePolicy_PolicyNotFound(t *testing.T) {
	targets := []secretsv1alpha1.TargetRef{{
		Kind:      secretsv1alpha1.TargetRefKindServiceAccount,
		Namespace: bindingTestNamespace,
		Name:      "demo-sa",
	}}

	t.Run("policy object missing in allowed namespace", func(t *testing.T) {
		binding := validBinding("binding-missing", targets, bindingTestNamespace, "no-such-policy")
		ns := namespaceWithLabels(bindingTestNamespace, nil)
		r, cli := newBindingTestReconciler(t, binding, ns)

		req := ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: binding.Namespace, Name: binding.Name,
		}}
		if _, err := r.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}

		var out secretsv1alpha1.SecretInjectionPolicyBinding
		if err := cli.Get(context.Background(), req.NamespacedName, &out); err != nil {
			t.Fatalf("Get binding: %v", err)
		}
		resolved := requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionResolvedRefs, metav1.ConditionFalse)
		if resolved.Reason != secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound {
			t.Fatalf("ResolvedRefs reason=%q; want %q", resolved.Reason, secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound)
		}
		requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionReady, metav1.ConditionFalse)

		var ap istiosecurityv1.AuthorizationPolicy
		apKey := types.NamespacedName{Namespace: binding.Namespace, Name: authorizationPolicyName(binding.Name)}
		if err := cli.Get(context.Background(), apKey, &ap); err == nil {
			t.Fatalf("AuthorizationPolicy was created despite PolicyNotFound; name=%s", ap.Name)
		} else if !apierrors.IsNotFound(err) {
			t.Fatalf("expected NotFound getting AuthorizationPolicy; got %v", err)
		}
	})

	t.Run("policyRef.namespace outside the allowed ancestor chain", func(t *testing.T) {
		// Binding lives in the project namespace; the policyRef points
		// at a sibling folder namespace that is neither the own NS,
		// the parent label, nor the synthesised org. Admission would
		// reject this; the reconciler refuses to resolve if a writer
		// bypassed admission.
		binding := validBinding("binding-bypass", targets, "holos-fld-other", "policy-a")
		policy := validPolicy("policy-a", "holos-fld-other")
		ns := namespaceWithLabels(bindingTestNamespace, map[string]string{
			"console.holos.run/organization": "acme",
		})
		r, cli := newBindingTestReconciler(t, binding, policy, ns)

		req := ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: binding.Namespace, Name: binding.Name,
		}}
		if _, err := r.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}

		var out secretsv1alpha1.SecretInjectionPolicyBinding
		if err := cli.Get(context.Background(), req.NamespacedName, &out); err != nil {
			t.Fatalf("Get binding: %v", err)
		}
		resolved := requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionResolvedRefs, metav1.ConditionFalse)
		if resolved.Reason != secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound {
			t.Fatalf("ResolvedRefs reason=%q; want %q", resolved.Reason, secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound)
		}
		if !strings.Contains(resolved.Message, "outside the admission-allowed ancestor chain") {
			t.Fatalf("ResolvedRefs message did not cite the bypass; got %q", resolved.Message)
		}
	})
}

// TestBinding_ResolvePolicy_ScopeNarrow asserts that spec.policyRef.scope
// narrows the candidate-namespace set: a scope=folder binding must not
// accept a policy sitting in the synthesised org namespace, and a
// scope=organization binding must not accept a policy sitting in the
// parent-label folder. This is the defence-in-depth contract the scope
// field exists to enforce, called out by HOL-752 review round 1.
func TestBinding_ResolvePolicy_ScopeNarrow(t *testing.T) {
	targets := []secretsv1alpha1.TargetRef{{
		Kind:      secretsv1alpha1.TargetRefKindServiceAccount,
		Namespace: bindingTestNamespace,
		Name:      "demo-sa",
	}}

	t.Run("scope=folder rejects an org-level policy", func(t *testing.T) {
		// Binding points at holos-org-acme (the org root) but declares
		// scope=folder. Admission would permit this namespace (it is the
		// synthesised org), but the reconciler's scope-narrowing must
		// reject the cross-scope read.
		binding := validBindingWithScope("binding-folder-cross", targets, "holos-org-acme", "policy-org", secretsv1alpha1.PolicyRefScopeFolder)
		policy := validPolicy("policy-org", "holos-org-acme")
		ns := namespaceWithLabels(bindingTestNamespace, map[string]string{
			"console.holos.run/parent":       "holos-fld-eng",
			"console.holos.run/organization": "acme",
		})
		r, cli := newBindingTestReconciler(t, binding, policy, ns)

		req := ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: binding.Namespace, Name: binding.Name,
		}}
		if _, err := r.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}

		var out secretsv1alpha1.SecretInjectionPolicyBinding
		if err := cli.Get(context.Background(), req.NamespacedName, &out); err != nil {
			t.Fatalf("Get binding: %v", err)
		}
		resolved := requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionResolvedRefs, metav1.ConditionFalse)
		if resolved.Reason != secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound {
			t.Fatalf("ResolvedRefs reason=%q; want %q", resolved.Reason, secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound)
		}
		requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionReady, metav1.ConditionFalse)
	})

	t.Run("scope=organization rejects a parent-folder policy", func(t *testing.T) {
		// Binding points at the parent-label folder but declares
		// scope=organization. Admission accepts the namespace (it is
		// the direct parent), but the reconciler must not resolve a
		// folder policy for an org-scope binding.
		binding := validBindingWithScope("binding-org-cross", targets, "holos-fld-eng", "policy-fld", secretsv1alpha1.PolicyRefScopeOrganization)
		policy := validPolicy("policy-fld", "holos-fld-eng")
		ns := namespaceWithLabels(bindingTestNamespace, map[string]string{
			"console.holos.run/parent":       "holos-fld-eng",
			"console.holos.run/organization": "acme",
		})
		r, cli := newBindingTestReconciler(t, binding, policy, ns)

		req := ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: binding.Namespace, Name: binding.Name,
		}}
		if _, err := r.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}

		var out secretsv1alpha1.SecretInjectionPolicyBinding
		if err := cli.Get(context.Background(), req.NamespacedName, &out); err != nil {
			t.Fatalf("Get binding: %v", err)
		}
		resolved := requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionResolvedRefs, metav1.ConditionFalse)
		if resolved.Reason != secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound {
			t.Fatalf("ResolvedRefs reason=%q; want %q", resolved.Reason, secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound)
		}
	})
}

// TestBinding_Reconcile_Idempotent exercises the hot-loop guard: a
// second Reconcile with no spec change must not write status (the
// conditions are already up to date) and must not churn the
// AuthorizationPolicy (the existing one is equivalent). The test
// invokes Reconcile twice and asserts the binding's
// resourceVersion does not advance on the second call, and that the
// AuthorizationPolicy retains its resourceVersion.
func TestBinding_Reconcile_Idempotent(t *testing.T) {
	targets := []secretsv1alpha1.TargetRef{{
		Kind:      secretsv1alpha1.TargetRefKindServiceAccount,
		Namespace: bindingTestNamespace,
		Name:      "demo-sa",
	}}
	binding := validBinding("binding-idem", targets, bindingTestNamespace, "policy-a")
	policy := validPolicy("policy-a", bindingTestNamespace)
	ns := namespaceWithLabels(bindingTestNamespace, nil)
	r, cli := newBindingTestReconciler(t, binding, policy, ns)

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: binding.Namespace, Name: binding.Name,
	}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile 1: %v", err)
	}

	var firstBinding secretsv1alpha1.SecretInjectionPolicyBinding
	if err := cli.Get(context.Background(), req.NamespacedName, &firstBinding); err != nil {
		t.Fatalf("Get binding 1: %v", err)
	}
	var firstAP istiosecurityv1.AuthorizationPolicy
	apKey := types.NamespacedName{Namespace: binding.Namespace, Name: authorizationPolicyName(binding.Name)}
	if err := cli.Get(context.Background(), apKey, &firstAP); err != nil {
		t.Fatalf("Get AuthorizationPolicy 1: %v", err)
	}

	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile 2: %v", err)
	}

	var secondBinding secretsv1alpha1.SecretInjectionPolicyBinding
	if err := cli.Get(context.Background(), req.NamespacedName, &secondBinding); err != nil {
		t.Fatalf("Get binding 2: %v", err)
	}
	var secondAP istiosecurityv1.AuthorizationPolicy
	if err := cli.Get(context.Background(), apKey, &secondAP); err != nil {
		t.Fatalf("Get AuthorizationPolicy 2: %v", err)
	}

	if firstBinding.ResourceVersion != secondBinding.ResourceVersion {
		t.Fatalf("binding resourceVersion changed between reconciles: %s -> %s", firstBinding.ResourceVersion, secondBinding.ResourceVersion)
	}
	if firstAP.ResourceVersion != secondAP.ResourceVersion {
		t.Fatalf("AuthorizationPolicy resourceVersion changed between reconciles: %s -> %s", firstAP.ResourceVersion, secondAP.ResourceVersion)
	}
}

// TestBinding_DeleteCascade verifies the single ownerReference the
// reconciler stamps on the AuthorizationPolicy so the API server
// garbage-collects the AP atomically when the binding is deleted. The
// fake client does not exercise apiserver GC, so this test asserts the
// ownerReference shape directly (controller=true, blockOwnerDeletion=true,
// matching UID) — the same invariant envtest (HOL-753) will promote to a
// true GC assertion.
func TestBinding_DeleteCascade(t *testing.T) {
	targets := []secretsv1alpha1.TargetRef{{
		Kind:      secretsv1alpha1.TargetRefKindService,
		Namespace: bindingTestNamespace,
		Name:      "demo-svc",
	}}
	binding := validBinding("binding-gc", targets, bindingTestNamespace, "policy-a")
	policy := validPolicy("policy-a", bindingTestNamespace)
	ns := namespaceWithLabels(bindingTestNamespace, nil)
	r, cli := newBindingTestReconciler(t, binding, policy, ns)

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: binding.Namespace, Name: binding.Name,
	}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var ap istiosecurityv1.AuthorizationPolicy
	apKey := types.NamespacedName{Namespace: binding.Namespace, Name: authorizationPolicyName(binding.Name)}
	if err := cli.Get(context.Background(), apKey, &ap); err != nil {
		t.Fatalf("Get AuthorizationPolicy: %v", err)
	}
	if got := len(ap.OwnerReferences); got != 1 {
		t.Fatalf("AuthorizationPolicy owner reference count=%d; want 1", got)
	}
	owner := ap.OwnerReferences[0]
	if owner.UID != binding.UID {
		t.Fatalf("owner UID=%q; want %q", owner.UID, binding.UID)
	}
	if owner.Controller == nil || !*owner.Controller {
		t.Fatalf("owner Controller flag not true: %+v", owner)
	}
	if owner.BlockOwnerDeletion == nil || !*owner.BlockOwnerDeletion {
		t.Fatalf("owner BlockOwnerDeletion flag not true: %+v", owner)
	}
	if owner.Kind != "SecretInjectionPolicyBinding" {
		t.Fatalf("owner Kind=%q; want %q", owner.Kind, "SecretInjectionPolicyBinding")
	}
}

// TestBinding_AuthorizationPolicyWriteFailed verifies the
// Programmed=False/Reason=AuthorizationPolicyWriteFailed path by
// injecting a Create error through the fake client's interceptor. The
// binding must end up with Programmed=False and Ready=False but the
// Accepted and ResolvedRefs conditions must remain unaffected so
// operators can tell the resolution path worked and only the write
// failed.
func TestBinding_AuthorizationPolicyWriteFailed(t *testing.T) {
	targets := []secretsv1alpha1.TargetRef{{
		Kind:      secretsv1alpha1.TargetRefKindServiceAccount,
		Namespace: bindingTestNamespace,
		Name:      "demo-sa",
	}}
	binding := validBinding("binding-fail", targets, bindingTestNamespace, "policy-a")
	policy := validPolicy("policy-a", bindingTestNamespace)
	ns := namespaceWithLabels(bindingTestNamespace, nil)

	funcs := interceptor.Funcs{
		Create: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*istiosecurityv1.AuthorizationPolicy); ok {
				return fmt.Errorf("simulated create failure")
			}
			return cli.Create(ctx, obj, opts...)
		},
	}
	r, cli := newBindingTestReconcilerWithInterceptor(t, funcs, binding, policy, ns)

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: binding.Namespace, Name: binding.Name,
	}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var out secretsv1alpha1.SecretInjectionPolicyBinding
	if err := cli.Get(context.Background(), req.NamespacedName, &out); err != nil {
		t.Fatalf("Get binding: %v", err)
	}
	requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionAccepted, metav1.ConditionTrue)
	requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionResolvedRefs, metav1.ConditionTrue)
	programmed := requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed, metav1.ConditionFalse)
	if programmed.Reason != secretsv1alpha1.SecretInjectionPolicyBindingReasonAuthorizationPolicyWriteFailed {
		t.Fatalf("Programmed reason=%q; want %q", programmed.Reason, secretsv1alpha1.SecretInjectionPolicyBindingReasonAuthorizationPolicyWriteFailed)
	}
	if !strings.Contains(programmed.Message, "simulated create failure") {
		t.Fatalf("Programmed message did not surface the error: %q", programmed.Message)
	}
	requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionReady, metav1.ConditionFalse)
}

// TestBinding_InvalidSpec_NoAPEmission verifies that an InvalidSpec
// binding (an object that bypassed admission) does not produce a stray
// AuthorizationPolicy. Accepted=False short-circuits the resolve /
// program path so ResolvedRefs and Programmed both surface the
// InvalidSpec reason.
func TestBinding_InvalidSpec_NoAPEmission(t *testing.T) {
	binding := &secretsv1alpha1.SecretInjectionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "binding-invalid",
			Namespace:  bindingTestNamespace,
			Generation: 1,
			UID:        types.UID("invalid-uid"),
		},
		Spec: secretsv1alpha1.SecretInjectionPolicyBindingSpec{
			// Missing policyRef.
			TargetRefs: []secretsv1alpha1.TargetRef{{
				Kind:      secretsv1alpha1.TargetRefKindServiceAccount,
				Namespace: bindingTestNamespace,
				Name:      "demo-sa",
			}},
		},
	}
	ns := namespaceWithLabels(bindingTestNamespace, nil)
	r, cli := newBindingTestReconciler(t, binding, ns)

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: binding.Namespace, Name: binding.Name,
	}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var out secretsv1alpha1.SecretInjectionPolicyBinding
	if err := cli.Get(context.Background(), req.NamespacedName, &out); err != nil {
		t.Fatalf("Get binding: %v", err)
	}
	accepted := requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionAccepted, metav1.ConditionFalse)
	if accepted.Reason != secretsv1alpha1.SecretInjectionPolicyBindingReasonInvalidSpec {
		t.Fatalf("Accepted reason=%q; want %q", accepted.Reason, secretsv1alpha1.SecretInjectionPolicyBindingReasonInvalidSpec)
	}
	requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionReady, metav1.ConditionFalse)

	var ap istiosecurityv1.AuthorizationPolicy
	apKey := types.NamespacedName{Namespace: binding.Namespace, Name: authorizationPolicyName(binding.Name)}
	if err := cli.Get(context.Background(), apKey, &ap); err == nil {
		t.Fatalf("AuthorizationPolicy created for InvalidSpec binding: name=%s", ap.Name)
	} else if !apierrors.IsNotFound(err) {
		t.Fatalf("expected NotFound; got %v", err)
	}
}

// TestBinding_BuildAuthorizationPolicy_ServiceAccount asserts the
// translation from a ServiceAccount target to
// spec.rules[].from[].source.principals. Kept as a pure-Go test so
// every target-shape permutation can be covered cheaply without a
// client.
func TestBinding_BuildAuthorizationPolicy_ServiceAccount(t *testing.T) {
	binding := validBinding("b", []secretsv1alpha1.TargetRef{
		{Kind: secretsv1alpha1.TargetRefKindServiceAccount, Namespace: "ns-a", Name: "sa-a"},
		{Kind: secretsv1alpha1.TargetRefKindServiceAccount, Namespace: "ns-b", Name: "sa-b"},
	}, bindingTestNamespace, "p")
	policy := validPolicy("p", bindingTestNamespace)

	ap, err := buildAuthorizationPolicy(binding, policy, defaultBindingAuthzTrustDomain)
	if err != nil {
		t.Fatalf("buildAuthorizationPolicy: %v", err)
	}
	if len(ap.Spec.Rules) != 1 {
		t.Fatalf("rules count=%d; want 1", len(ap.Spec.Rules))
	}
	from := ap.Spec.Rules[0].From
	if len(from) != 1 || from[0].Source == nil {
		t.Fatalf("unexpected rule from shape: %+v", from)
	}
	got := from[0].Source.Principals
	want := []string{
		"cluster.local/ns/ns-a/sa/sa-a",
		"cluster.local/ns/ns-b/sa/sa-b",
	}
	if len(got) != len(want) {
		t.Fatalf("principals=%v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("principals[%d]=%q; want %q", i, got[i], want[i])
		}
	}
	if ap.Spec.Selector != nil {
		t.Fatalf("selector populated for pure-ServiceAccount target set: %+v", ap.Spec.Selector)
	}
}

// TestBinding_BuildAuthorizationPolicy_Service asserts the translation
// from a Service target to spec.selector. No principals are emitted
// when every target is a Service because the selector is the whole
// caller-allow surface.
func TestBinding_BuildAuthorizationPolicy_Service(t *testing.T) {
	binding := validBinding("b", []secretsv1alpha1.TargetRef{
		{Kind: secretsv1alpha1.TargetRefKindService, Namespace: bindingTestNamespace, Name: "svc-a"},
	}, bindingTestNamespace, "p")
	policy := validPolicy("p", bindingTestNamespace)

	ap, err := buildAuthorizationPolicy(binding, policy, defaultBindingAuthzTrustDomain)
	if err != nil {
		t.Fatalf("buildAuthorizationPolicy: %v", err)
	}
	if ap.Spec.Selector == nil {
		t.Fatalf("selector not populated for Service target")
	}
	if got := ap.Spec.Selector.MatchLabels["kubernetes.io/service-name"]; got != "svc-a" {
		t.Fatalf("selector kubernetes.io/service-name=%q; want %q", got, "svc-a")
	}
	if len(ap.Spec.Rules) != 1 {
		t.Fatalf("rules count=%d; want 1", len(ap.Spec.Rules))
	}
	if len(ap.Spec.Rules[0].From) != 0 {
		t.Fatalf("unexpected from entries for pure-Service target set: %+v", ap.Spec.Rules[0].From)
	}
}

// TestBinding_BuildAuthorizationPolicy_WorkloadSelector asserts the
// optional spec.workloadSelector overlay copies matchLabels onto the
// emitted AP selector.
func TestBinding_BuildAuthorizationPolicy_WorkloadSelector(t *testing.T) {
	binding := validBinding("b", []secretsv1alpha1.TargetRef{
		{Kind: secretsv1alpha1.TargetRefKindServiceAccount, Namespace: "ns", Name: "sa"},
	}, bindingTestNamespace, "p")
	binding.Spec.WorkloadSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "demo", "version": "v1"},
	}
	policy := validPolicy("p", bindingTestNamespace)

	ap, err := buildAuthorizationPolicy(binding, policy, defaultBindingAuthzTrustDomain)
	if err != nil {
		t.Fatalf("buildAuthorizationPolicy: %v", err)
	}
	if ap.Spec.Selector == nil {
		t.Fatalf("selector not populated for workloadSelector overlay")
	}
	if got := ap.Spec.Selector.MatchLabels["app"]; got != "demo" {
		t.Fatalf("selector app=%q; want demo", got)
	}
	if got := ap.Spec.Selector.MatchLabels["version"]; got != "v1" {
		t.Fatalf("selector version=%q; want v1", got)
	}
}

// TestBinding_ServiceTarget_CrossNamespace_Rejected asserts that a
// Service target whose namespace differs from the binding's own
// namespace is rejected by bindingAcceptedCondition rather than
// silently producing an AP that would select the wrong workload (HOL-752
// review round 1 CRITICAL). The AP must not be created.
func TestBinding_ServiceTarget_CrossNamespace_Rejected(t *testing.T) {
	binding := validBinding("binding-svc-xns", []secretsv1alpha1.TargetRef{{
		Kind:      secretsv1alpha1.TargetRefKindService,
		Namespace: "holos-prj-other",
		Name:      "svc-a",
	}}, bindingTestNamespace, "policy-a")
	policy := validPolicy("policy-a", bindingTestNamespace)
	ns := namespaceWithLabels(bindingTestNamespace, nil)
	r, cli := newBindingTestReconciler(t, binding, policy, ns)

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: binding.Namespace, Name: binding.Name,
	}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var out secretsv1alpha1.SecretInjectionPolicyBinding
	if err := cli.Get(context.Background(), req.NamespacedName, &out); err != nil {
		t.Fatalf("Get binding: %v", err)
	}
	accepted := requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionAccepted, metav1.ConditionFalse)
	if accepted.Reason != secretsv1alpha1.SecretInjectionPolicyBindingReasonInvalidSpec {
		t.Fatalf("Accepted reason=%q; want InvalidSpec", accepted.Reason)
	}
	if !strings.Contains(accepted.Message, "holos-prj-other") {
		t.Fatalf("Accepted message did not cite the foreign namespace: %q", accepted.Message)
	}

	var ap istiosecurityv1.AuthorizationPolicy
	apKey := types.NamespacedName{Namespace: binding.Namespace, Name: authorizationPolicyName(binding.Name)}
	if err := cli.Get(context.Background(), apKey, &ap); err == nil {
		t.Fatalf("AuthorizationPolicy created despite cross-namespace Service target: name=%s", ap.Name)
	} else if !apierrors.IsNotFound(err) {
		t.Fatalf("expected NotFound; got %v", err)
	}
}

// TestBinding_MultipleServiceTargets_Rejected asserts that a binding
// carrying more than one Service target is rejected by
// bindingAcceptedCondition. A single-AP-per-binding model cannot
// enforce disjunctive service-name selectors (AND semantics across the
// same label key is unsatisfiable) so the current code would silently
// widen the AP to a namespace-wide selector; HOL-752 review round 1
// flagged this as CRITICAL and we defer multi-Service emission to a
// later milestone.
func TestBinding_MultipleServiceTargets_Rejected(t *testing.T) {
	binding := validBinding("binding-multi-svc", []secretsv1alpha1.TargetRef{
		{Kind: secretsv1alpha1.TargetRefKindService, Namespace: bindingTestNamespace, Name: "svc-a"},
		{Kind: secretsv1alpha1.TargetRefKindService, Namespace: bindingTestNamespace, Name: "svc-b"},
	}, bindingTestNamespace, "policy-a")
	policy := validPolicy("policy-a", bindingTestNamespace)
	ns := namespaceWithLabels(bindingTestNamespace, nil)
	r, cli := newBindingTestReconciler(t, binding, policy, ns)

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: binding.Namespace, Name: binding.Name,
	}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var out secretsv1alpha1.SecretInjectionPolicyBinding
	if err := cli.Get(context.Background(), req.NamespacedName, &out); err != nil {
		t.Fatalf("Get binding: %v", err)
	}
	accepted := requireCondition(t, &out, secretsv1alpha1.SecretInjectionPolicyBindingConditionAccepted, metav1.ConditionFalse)
	if accepted.Reason != secretsv1alpha1.SecretInjectionPolicyBindingReasonInvalidSpec {
		t.Fatalf("Accepted reason=%q; want InvalidSpec", accepted.Reason)
	}
	if !strings.Contains(accepted.Message, "at most one Service target") {
		t.Fatalf("Accepted message did not cite the multi-Service cap: %q", accepted.Message)
	}

	var ap istiosecurityv1.AuthorizationPolicy
	apKey := types.NamespacedName{Namespace: binding.Namespace, Name: authorizationPolicyName(binding.Name)}
	if err := cli.Get(context.Background(), apKey, &ap); err == nil {
		t.Fatalf("AuthorizationPolicy created despite multi-Service spec: name=%s", ap.Name)
	} else if !apierrors.IsNotFound(err) {
		t.Fatalf("expected NotFound; got %v", err)
	}
}

// TestRuleEqual_WhenDrift pins the HOL-839 tightening of ruleEqual. The
// reconciler never populates Rule.When, so any non-empty When on either
// side of the compare is drift — including an apiserver-echoed mutation
// that preserves the slice length. The previous slice-length compare
// masked the in-place case.
func TestRuleEqual_WhenDrift(t *testing.T) {
	conditionA := &istiosecurityv1beta1.Condition{Key: "request.headers[x-foo]", Values: []string{"a"}}
	conditionB := &istiosecurityv1beta1.Condition{Key: "request.headers[x-bar]", Values: []string{"b"}}

	cases := []struct {
		name string
		a    *istiosecurityv1beta1.Rule
		b    *istiosecurityv1beta1.Rule
		want bool
	}{
		{
			name: "both empty — equal",
			a:    &istiosecurityv1beta1.Rule{},
			b:    &istiosecurityv1beta1.Rule{},
			want: true,
		},
		{
			name: "desired empty, existing non-empty — drift",
			a:    &istiosecurityv1beta1.Rule{},
			b:    &istiosecurityv1beta1.Rule{When: []*istiosecurityv1beta1.Condition{conditionA}},
			want: false,
		},
		{
			name: "desired non-empty, existing empty — drift",
			a:    &istiosecurityv1beta1.Rule{When: []*istiosecurityv1beta1.Condition{conditionA}},
			b:    &istiosecurityv1beta1.Rule{},
			want: false,
		},
		{
			name: "same length, different contents — drift (the regression gate)",
			a:    &istiosecurityv1beta1.Rule{When: []*istiosecurityv1beta1.Condition{conditionA}},
			b:    &istiosecurityv1beta1.Rule{When: []*istiosecurityv1beta1.Condition{conditionB}},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ruleEqual(tc.a, tc.b); got != tc.want {
				t.Fatalf("ruleEqual=%v; want %v", got, tc.want)
			}
		})
	}
}
