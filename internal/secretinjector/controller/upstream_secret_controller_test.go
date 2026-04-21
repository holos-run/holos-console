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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	secretsv1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
)

// validUpstreamSecret returns a fully-populated UpstreamSecret pointing at
// the named sibling Secret. Shared by every positive and negative test case
// so individual tables only need to mutate the one field under test.
func validUpstreamSecret(name, namespace, secretName, secretKey string) *secretsv1alpha1.UpstreamSecret {
	return &secretsv1alpha1.UpstreamSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: 1,
		},
		Spec: secretsv1alpha1.UpstreamSecretSpec{
			SecretRef: secretsv1alpha1.SecretKeyReference{
				Name: secretName,
				Key:  secretKey,
			},
			Upstream: secretsv1alpha1.Upstream{
				Host:   "api.example.com",
				Scheme: "https",
			},
			Injection: secretsv1alpha1.Injection{
				Header: "Authorization",
			},
		},
	}
}

// newTestReconciler builds a UpstreamSecretReconciler backed by a fake
// controller-runtime client seeded with objs and a status subresource
// registered for UpstreamSecret. A nil Recorder is deliberate — the
// reconciler must not panic when the event recorder is unset in tests.
func newTestReconciler(t *testing.T, objs ...client.Object) (*UpstreamSecretReconciler, client.Client) {
	t.Helper()
	// Index the fake client on spec.secretRef.name so the
	// upstreamSecretsForSecret mapper's MatchingFields lookup returns the
	// right objects. The production cache registers the same index via
	// mgr.GetFieldIndexer() in SetupWithManager.
	cli := ctrlfake.NewClientBuilder().
		WithScheme(Scheme).
		WithObjects(objs...).
		WithStatusSubresource(&secretsv1alpha1.UpstreamSecret{}).
		WithIndex(
			&secretsv1alpha1.UpstreamSecret{},
			upstreamSecretSecretRefNameIndex,
			func(obj client.Object) []string {
				us, ok := obj.(*secretsv1alpha1.UpstreamSecret)
				if !ok {
					return nil
				}
				if us.Spec.SecretRef.Name == "" {
					return nil
				}
				return []string{us.Spec.SecretRef.Name}
			},
		).
		Build()
	r := &UpstreamSecretReconciler{
		Client: cli,
		Scheme: Scheme,
	}
	return r, cli
}

// TestUpstreamSecret_ResolvedRefs_Outcomes covers the three ResolvedRefs
// outcomes mandated by HOL-750's acceptance criteria: ResolvedRefs=True
// when the secret+key exist, ResolvedRefs=False/SecretNotFound when the
// Secret is absent, ResolvedRefs=False/SecretKeyMissing when the Secret
// exists but .data does not carry the named key.
func TestUpstreamSecret_ResolvedRefs_Outcomes(t *testing.T) {
	const (
		ns         = "holos-prj-demo"
		name       = "example"
		secretName = "creds"
		secretKey  = "api-key"
	)

	cases := []struct {
		name       string
		secret     *corev1.Secret
		wantStatus metav1.ConditionStatus
		wantReason string
		wantReady  metav1.ConditionStatus
	}{
		{
			name: "ResolvedRefs=True when Secret carries the key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: ns},
				Data:       map[string][]byte{secretKey: []byte("s3cret")},
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: secretsv1alpha1.UpstreamSecretReasonResolvedRefs,
			wantReady:  metav1.ConditionTrue,
		},
		{
			name:       "ResolvedRefs=False/SecretNotFound when Secret is absent",
			secret:     nil,
			wantStatus: metav1.ConditionFalse,
			wantReason: secretsv1alpha1.UpstreamSecretReasonSecretNotFound,
			wantReady:  metav1.ConditionFalse,
		},
		{
			name: "ResolvedRefs=False/SecretKeyMissing when key is not set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: ns},
				Data:       map[string][]byte{"other-key": []byte("s3cret")},
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: secretsv1alpha1.UpstreamSecretReasonSecretKeyMissing,
			wantReady:  metav1.ConditionFalse,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			us := validUpstreamSecret(name, ns, secretName, secretKey)
			objs := []client.Object{us}
			if tc.secret != nil {
				objs = append(objs, tc.secret)
			}
			r, cli := newTestReconciler(t, objs...)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
			if _, err := r.Reconcile(context.Background(), req); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}

			var got secretsv1alpha1.UpstreamSecret
			if err := cli.Get(context.Background(), req.NamespacedName, &got); err != nil {
				t.Fatalf("get UpstreamSecret: %v", err)
			}

			resolved := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.UpstreamSecretConditionResolvedRefs)
			if resolved == nil {
				t.Fatalf("ResolvedRefs condition missing; conditions=%+v", got.Status.Conditions)
			}
			if resolved.Status != tc.wantStatus {
				t.Errorf("ResolvedRefs.Status=%s want %s", resolved.Status, tc.wantStatus)
			}
			if resolved.Reason != tc.wantReason {
				t.Errorf("ResolvedRefs.Reason=%s want %s", resolved.Reason, tc.wantReason)
			}
			if got.Status.ObservedGeneration != got.Generation {
				t.Errorf("observedGeneration=%d want %d", got.Status.ObservedGeneration, got.Generation)
			}

			accepted := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.UpstreamSecretConditionAccepted)
			if accepted == nil || accepted.Status != metav1.ConditionTrue {
				t.Errorf("Accepted should be True for well-formed spec; got %+v", accepted)
			}

			ready := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.UpstreamSecretConditionReady)
			if ready == nil {
				t.Fatalf("Ready condition missing; conditions=%+v", got.Status.Conditions)
			}
			if ready.Status != tc.wantReady {
				t.Errorf("Ready.Status=%s want %s", ready.Status, tc.wantReady)
			}
			wantReadyReason := secretsv1alpha1.UpstreamSecretReasonReady
			if tc.wantReady != metav1.ConditionTrue {
				wantReadyReason = secretsv1alpha1.UpstreamSecretReasonNotReady
			}
			if ready.Reason != wantReadyReason {
				t.Errorf("Ready.Reason=%s want %s", ready.Reason, wantReadyReason)
			}

			// No sensitive bytes may leak onto the CR — regardless of the
			// resolved-refs outcome, the Secret data ("s3cret") must
			// never appear in any condition Message or in the serialised
			// status. This mirrors the doc.go invariant that ADR 031
			// elevated to a first-class rule.
			for _, c := range got.Status.Conditions {
				if strings.Contains(c.Message, "s3cret") {
					t.Errorf("condition %s message leaks Secret bytes: %q", c.Type, c.Message)
				}
			}
		})
	}
}

// TestUpstreamSecret_HotLoopGuard verifies the reconciler does not re-write
// status when the generation is unchanged and every condition's (Status,
// Reason, Message) tuple matches the last observation. HOL-675's hot-loop
// contract forbids this, and a regression here would cause the API server
// to fire a watch event for every reconcile, causing it to reconcile
// again, and so on.
func TestUpstreamSecret_HotLoopGuard(t *testing.T) {
	const (
		ns         = "holos-prj-demo"
		name       = "stable"
		secretName = "creds"
		secretKey  = "api-key"
	)
	us := validUpstreamSecret(name, ns, secretName, secretKey)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: ns},
		Data:       map[string][]byte{secretKey: []byte("s3cret")},
	}
	r, cli := newTestReconciler(t, us, secret)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	var afterFirst secretsv1alpha1.UpstreamSecret
	if err := cli.Get(context.Background(), req.NamespacedName, &afterFirst); err != nil {
		t.Fatalf("get after first reconcile: %v", err)
	}
	firstRV := afterFirst.ResourceVersion
	if firstRV == "" {
		t.Fatalf("resourceVersion empty after first reconcile")
	}

	// Second reconcile with no spec change must not write status — if it
	// did, the fake client would bump resourceVersion. This is exactly the
	// check the envtest suite performs in HOL-753 at the API-server level.
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	var afterSecond secretsv1alpha1.UpstreamSecret
	if err := cli.Get(context.Background(), req.NamespacedName, &afterSecond); err != nil {
		t.Fatalf("get after second reconcile: %v", err)
	}
	if afterSecond.ResourceVersion != firstRV {
		t.Fatalf("resourceVersion advanced from %q to %q without spec change; hot-loop guard regressed",
			firstRV, afterSecond.ResourceVersion)
	}
}

// TestUpstreamSecret_ObservedGenerationAdvances creates an UpstreamSecret,
// reconciles, bumps spec.generation (simulated by an Update that changes
// the injection header), and confirms observedGeneration catches up and
// the conditions all carry the new generation.
func TestUpstreamSecret_ObservedGenerationAdvances(t *testing.T) {
	const (
		ns         = "holos-prj-demo"
		name       = "updater"
		secretName = "creds"
		secretKey  = "api-key"
	)
	us := validUpstreamSecret(name, ns, secretName, secretKey)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: ns},
		Data:       map[string][]byte{secretKey: []byte("s3cret")},
	}
	r, cli := newTestReconciler(t, us, secret)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	var read secretsv1alpha1.UpstreamSecret
	if err := cli.Get(context.Background(), req.NamespacedName, &read); err != nil {
		t.Fatalf("get after first reconcile: %v", err)
	}
	if read.Status.ObservedGeneration != 1 {
		t.Fatalf("observedGeneration=%d want 1", read.Status.ObservedGeneration)
	}

	// Mutate the spec and bump generation explicitly. The controller-
	// runtime fake client does not model the API server's
	// generation-on-spec-change behavior, so we mirror that by bumping
	// Generation ourselves. Envtest in HOL-753 exercises the real
	// generation-advance path end-to-end.
	read.Spec.Injection.Header = "X-Api-Key"
	read.Generation = 2
	if err := cli.Update(context.Background(), &read); err != nil {
		t.Fatalf("update UpstreamSecret: %v", err)
	}
	var bumped secretsv1alpha1.UpstreamSecret
	if err := cli.Get(context.Background(), req.NamespacedName, &bumped); err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if bumped.Generation <= 1 {
		t.Fatalf("expected generation to advance past 1; got %d", bumped.Generation)
	}
	wantGen := bumped.Generation

	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	var final secretsv1alpha1.UpstreamSecret
	if err := cli.Get(context.Background(), req.NamespacedName, &final); err != nil {
		t.Fatalf("get after second reconcile: %v", err)
	}
	if final.Status.ObservedGeneration != wantGen {
		t.Fatalf("observedGeneration=%d want %d", final.Status.ObservedGeneration, wantGen)
	}
	for _, c := range final.Status.Conditions {
		if c.ObservedGeneration != wantGen {
			t.Errorf("condition %s observedGeneration=%d want %d", c.Type, c.ObservedGeneration, wantGen)
		}
	}
}

// TestUpstreamSecret_InvalidSpecReason exercises the Accepted=False path
// for objects that bypass admission. We zero the secretRef fields directly
// on an already-created object so the reconciler sees an invalid spec and
// must surface the InvalidSpec reason on both Accepted and Ready.
func TestUpstreamSecret_InvalidSpecReason(t *testing.T) {
	const (
		ns   = "holos-prj-demo"
		name = "invalid"
	)
	us := &secretsv1alpha1.UpstreamSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Generation: 1,
		},
		Spec: secretsv1alpha1.UpstreamSecretSpec{
			// secretRef.Name deliberately empty — admission would reject
			// but our reconciler is belt-and-braces.
			Upstream: secretsv1alpha1.Upstream{
				Host:   "api.example.com",
				Scheme: "https",
			},
			Injection: secretsv1alpha1.Injection{
				Header: "Authorization",
			},
		},
	}
	r, cli := newTestReconciler(t, us)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var got secretsv1alpha1.UpstreamSecret
	if err := cli.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	accepted := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.UpstreamSecretConditionAccepted)
	if accepted == nil || accepted.Status != metav1.ConditionFalse || accepted.Reason != secretsv1alpha1.UpstreamSecretReasonInvalidSpec {
		t.Fatalf("want Accepted=False/InvalidSpec, got %+v", accepted)
	}
	ready := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.UpstreamSecretConditionReady)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != secretsv1alpha1.UpstreamSecretReasonNotReady {
		t.Fatalf("want Ready=False/NotReady, got %+v", ready)
	}
}

// TestUpstreamSecret_NotFound_ReturnsNoError exercises the cache-miss path:
// a request for an object that does not exist should return no error and
// no requeue so controller-runtime does not hot-loop on a deletion.
func TestUpstreamSecret_NotFound_ReturnsNoError(t *testing.T) {
	r, _ := newTestReconciler(t)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "x", Name: "missing"}}
	res, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("Reconcile on missing: %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Fatalf("Reconcile on missing: want no requeue, got %+v", res)
	}
}

// TestUpstreamSecret_MapFunc_EnqueuesReferencers covers the v1.Secret
// watch's mapper: a Secret create/update enqueues exactly the
// UpstreamSecrets that reference it by (namespace, secretName), and
// ignores bystander UpstreamSecrets that either reference a different
// Secret or live in a different namespace.
func TestUpstreamSecret_MapFunc_EnqueuesReferencers(t *testing.T) {
	const (
		nsA        = "holos-prj-a"
		nsB        = "holos-prj-b"
		secretName = "creds"
	)

	target := validUpstreamSecret("refers-to-creds", nsA, secretName, "k")
	sameSecretDiffNS := validUpstreamSecret("other-ns-same-name", nsB, secretName, "k")
	otherSecretSameNS := validUpstreamSecret("other-name-same-ns", nsA, "other", "k")

	r, _ := newTestReconciler(t, target, sameSecretDiffNS, otherSecretSameNS)

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: nsA}}
	reqs := r.upstreamSecretsForSecret(context.Background(), secret)

	if len(reqs) != 1 {
		t.Fatalf("upstreamSecretsForSecret returned %d requests; want exactly 1", len(reqs))
	}
	got := reqs[0]
	want := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: nsA, Name: "refers-to-creds"}}
	if got != want {
		t.Fatalf("upstreamSecretsForSecret returned %v; want %v", got, want)
	}
}

// TestUpstreamSecret_MapFunc_IgnoresNonSecretObjects asserts the mapper
// tolerates a non-Secret object gracefully (controller-runtime guarantees
// the event source emits Secrets, but defense-in-depth against watch
// misregistration is cheap).
func TestUpstreamSecret_MapFunc_IgnoresNonSecretObjects(t *testing.T) {
	r, _ := newTestReconciler(t)
	reqs := r.upstreamSecretsForSecret(context.Background(), &corev1.ConfigMap{})
	if len(reqs) != 0 {
		t.Fatalf("upstreamSecretsForSecret on ConfigMap returned %d requests; want 0", len(reqs))
	}
}
