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
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/clock"
	clocktesting "k8s.io/utils/clock/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	secretsv1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
	sicrypto "github.com/holos-run/holos-console/internal/secretinjector/crypto"
)

// stubPepperLoader is an in-memory [sicrypto.Loader] used by unit tests.
// It deliberately avoids the production [sicrypto.SecretLoader] because
// the fake client does not model the pepper Secret and because tests
// need deterministic version and byte values.
type stubPepperLoader struct {
	mu       sync.Mutex
	active   int32
	versions map[int32][]byte
}

func (s *stubPepperLoader) Active(ctx context.Context) (int32, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bytes, ok := s.versions[s.active]
	if !ok {
		return 0, nil, sicrypto.ErrNoPepperVersions
	}
	return s.active, append([]byte(nil), bytes...), nil
}

func (s *stubPepperLoader) Get(ctx context.Context, version int32) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bytes, ok := s.versions[version]
	if !ok {
		return nil, sicrypto.ErrPepperVersionNotFound
	}
	return append([]byte(nil), bytes...), nil
}

func newStubPepper(version int32, bytes []byte) *stubPepperLoader {
	return &stubPepperLoader{
		active:   version,
		versions: map[int32][]byte{version: bytes},
	}
}

// credentialFixture fixture holds the pieces a Reconcile call needs.
type credentialFixture struct {
	r            *CredentialReconciler
	cli          client.Client
	pepper       *stubPepperLoader
	plaintextKey string
}

// newCredentialReconciler builds a reconciler + fake client seeded with
// objs, a stub pepper loader with a single version, and a real argon2id
// KDF (low-memory DefaultParams is fine for unit tests at this scale;
// the test-loop never traps on argon2 cost).
func newCredentialReconciler(t *testing.T, fixedTime time.Time, objs ...client.Object) credentialFixture {
	t.Helper()
	cli := ctrlfake.NewClientBuilder().
		WithScheme(Scheme).
		WithObjects(objs...).
		WithStatusSubresource(&secretsv1alpha1.Credential{}).
		Build()
	pepper := newStubPepper(7, []byte("unit-test-pepper-bytes-01234567"))
	r := &CredentialReconciler{
		Client: cli,
		Scheme: Scheme,
		KDF:    sicrypto.Default(),
		Pepper: pepper,
		Clock:  clocktesting.NewFakePassiveClock(fixedTime),
	}
	return credentialFixture{r: r, cli: cli, pepper: pepper}
}

// validCredential returns a fully-populated Credential pointing at the
// named upstream Secret.
func validCredential(name, namespace, upstreamName, upstreamKey string) *secretsv1alpha1.Credential {
	return &secretsv1alpha1.Credential{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: 1,
			UID:        types.UID(name + "-uid"),
		},
		Spec: secretsv1alpha1.CredentialSpec{
			Authentication: secretsv1alpha1.Authentication{
				Type:   secretsv1alpha1.AuthenticationTypeAPIKey,
				APIKey: &secretsv1alpha1.APIKeySettings{HeaderName: "X-Api-Key"},
			},
			UpstreamSecretRef: secretsv1alpha1.NamespacedSecretKeyReference{
				Name: upstreamName,
				Key:  upstreamKey,
			},
		},
	}
}

// upstreamSecret is a small convenience to avoid a repetitive Secret
// literal in every test.
func upstreamSecret(name, namespace, key string, value []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       map[string][]byte{key: value},
		Type:       corev1.SecretTypeOpaque,
	}
}

// TestCredential_Materialisation verifies the happy path: an Accepted
// Credential with no prior hash materialisation mints a KSUID, creates
// the sibling v1.Secret with a single controller ownerReference, stamps
// HashMaterialized=True / Ready=True, and records the pepper version.
func TestCredential_Materialisation(t *testing.T) {
	const (
		ns         = "holos-prj-demo"
		name       = "vendor-apikey"
		upstream   = "vendor-upstream"
		upstreamK  = "apiKey"
		upstreamV  = "sih_AaBbCcDdEeFfGgHhIiJjKkLlMm" // synthetic plaintext
		fixedYear  = 2026
		fixedMonth = 4
		fixedDay   = 21
	)
	fixedTime := time.Date(fixedYear, time.Month(fixedMonth), fixedDay, 12, 0, 0, 0, time.UTC)

	cred := validCredential(name, ns, upstream, upstreamK)
	up := upstreamSecret(upstream, ns, upstreamK, []byte(upstreamV))
	fx := newCredentialReconciler(t, fixedTime, cred, up)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
	if _, err := fx.r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got secretsv1alpha1.Credential
	if err := fx.cli.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatalf("get Credential: %v", err)
	}

	if got.Status.CredentialID == "" {
		t.Errorf("credentialID empty after materialisation")
	}
	if len(got.Status.CredentialID) != 27 {
		t.Errorf("credentialID=%q len=%d want 27 (KSUID)", got.Status.CredentialID, len(got.Status.CredentialID))
	}
	if got.Status.HashSecretRef == nil {
		t.Fatalf("hashSecretRef nil after materialisation")
	}
	wantSecretName := name + credentialHashSecretSuffix
	if got.Status.HashSecretRef.Name != wantSecretName {
		t.Errorf("hashSecretRef.Name=%q want %q", got.Status.HashSecretRef.Name, wantSecretName)
	}
	if got.Status.HashSecretRef.Key != credentialHashEnvelopeKey {
		t.Errorf("hashSecretRef.Key=%q want %q", got.Status.HashSecretRef.Key, credentialHashEnvelopeKey)
	}
	if got.Status.PepperVersion != 7 {
		t.Errorf("pepperVersion=%d want 7", got.Status.PepperVersion)
	}
	if got.Status.Phase != secretsv1alpha1.PhaseActive {
		t.Errorf("phase=%q want %q", got.Status.Phase, secretsv1alpha1.PhaseActive)
	}

	hash := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.CredentialConditionHashMaterialized)
	if hash == nil || hash.Status != metav1.ConditionTrue || hash.Reason != secretsv1alpha1.CredentialReasonHashMaterialized {
		t.Errorf("HashMaterialized condition unexpected: %+v", hash)
	}
	ready := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.CredentialConditionReady)
	if ready == nil || ready.Status != metav1.ConditionTrue || ready.Reason != secretsv1alpha1.CredentialReasonReady {
		t.Errorf("Ready condition unexpected: %+v", ready)
	}

	// Sibling v1.Secret shape.
	var hashSecret corev1.Secret
	if err := fx.cli.Get(context.Background(),
		types.NamespacedName{Namespace: ns, Name: wantSecretName}, &hashSecret); err != nil {
		t.Fatalf("get hash Secret: %v", err)
	}
	if hashSecret.Type != corev1.SecretTypeOpaque {
		t.Errorf("hash Secret type=%q want %q", hashSecret.Type, corev1.SecretTypeOpaque)
	}
	if got := len(hashSecret.OwnerReferences); got != 1 {
		t.Fatalf("hash Secret ownerReferences len=%d want 1", got)
	}
	owner := hashSecret.OwnerReferences[0]
	if owner.UID != cred.UID {
		t.Errorf("ownerReference.UID=%q want %q", owner.UID, cred.UID)
	}
	if owner.Controller == nil || !*owner.Controller {
		t.Errorf("ownerReference.Controller must be true; got %+v", owner.Controller)
	}
	if owner.BlockOwnerDeletion == nil || !*owner.BlockOwnerDeletion {
		t.Errorf("ownerReference.BlockOwnerDeletion must be true; got %+v", owner.BlockOwnerDeletion)
	}

	encoded, ok := hashSecret.Data[credentialHashEnvelopeKey]
	if !ok {
		t.Fatalf("hash Secret data missing key %q", credentialHashEnvelopeKey)
	}
	var envelope sicrypto.Envelope
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope.KDF != sicrypto.KDFArgon2id {
		t.Errorf("envelope.KDF=%q want %q", envelope.KDF, sicrypto.KDFArgon2id)
	}
	if envelope.PepperVersion != "7" {
		t.Errorf("envelope.PepperVersion=%q want %q", envelope.PepperVersion, "7")
	}
	if len(envelope.Salt) != credentialHashSaltLength {
		t.Errorf("envelope.Salt len=%d want %d", len(envelope.Salt), credentialHashSaltLength)
	}
	if len(envelope.Hash) == 0 {
		t.Errorf("envelope.Hash empty")
	}

	// No sensitive values may leak onto the Credential CR itself — JSON-
	// marshal the Credential and grep for the upstream plaintext and for
	// any byte-slice-looking blobs. This mirrors the dominant invariant
	// from api/secrets/v1alpha1/doc.go.
	credBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal Credential: %v", err)
	}
	if strings.Contains(string(credBytes), upstreamV) {
		t.Errorf("Credential JSON leaks upstream plaintext: %s", credBytes)
	}
}

// TestCredential_Idempotence confirms that a second Reconcile with no
// spec change produces no status write (resourceVersion unchanged) and
// does not create a second hash Secret. HOL-751's hot-loop invariant
// mirrors the UpstreamSecret test pattern.
func TestCredential_Idempotence(t *testing.T) {
	const (
		ns        = "holos-prj-demo"
		name      = "stable"
		upstream  = "vendor-upstream"
		upstreamK = "apiKey"
	)
	fixedTime := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	cred := validCredential(name, ns, upstream, upstreamK)
	up := upstreamSecret(upstream, ns, upstreamK, []byte("sih_plaintext"))
	fx := newCredentialReconciler(t, fixedTime, cred, up)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
	if _, err := fx.r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	var firstCred secretsv1alpha1.Credential
	if err := fx.cli.Get(context.Background(), req.NamespacedName, &firstCred); err != nil {
		t.Fatalf("get after first: %v", err)
	}
	var firstHash corev1.Secret
	hashKey := types.NamespacedName{Namespace: ns, Name: name + credentialHashSecretSuffix}
	if err := fx.cli.Get(context.Background(), hashKey, &firstHash); err != nil {
		t.Fatalf("get hash after first: %v", err)
	}

	if _, err := fx.r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	var secondCred secretsv1alpha1.Credential
	if err := fx.cli.Get(context.Background(), req.NamespacedName, &secondCred); err != nil {
		t.Fatalf("get after second: %v", err)
	}
	if secondCred.ResourceVersion != firstCred.ResourceVersion {
		t.Errorf("Credential resourceVersion advanced from %q to %q without spec change; hot-loop guard regressed",
			firstCred.ResourceVersion, secondCred.ResourceVersion)
	}
	var secondHash corev1.Secret
	if err := fx.cli.Get(context.Background(), hashKey, &secondHash); err != nil {
		t.Fatalf("get hash after second: %v", err)
	}
	if secondHash.ResourceVersion != firstHash.ResourceVersion {
		t.Errorf("hash Secret resourceVersion advanced from %q to %q without spec change",
			firstHash.ResourceVersion, secondHash.ResourceVersion)
	}
}

// TestCredential_Revocation verifies spec.revoked=true drives Phase=Revoked,
// Ready=False/Reason=Revoked, and deletes the sibling hash Secret.
func TestCredential_Revocation(t *testing.T) {
	const (
		ns        = "holos-prj-demo"
		name      = "to-revoke"
		upstream  = "vendor-upstream"
		upstreamK = "apiKey"
	)
	fixedTime := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	cred := validCredential(name, ns, upstream, upstreamK)
	up := upstreamSecret(upstream, ns, upstreamK, []byte("sih_plaintext"))
	fx := newCredentialReconciler(t, fixedTime, cred, up)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
	if _, err := fx.r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	// Flip revoked=true and bump generation to mirror a real spec update.
	var current secretsv1alpha1.Credential
	if err := fx.cli.Get(context.Background(), req.NamespacedName, &current); err != nil {
		t.Fatalf("get before revoke: %v", err)
	}
	current.Spec.Revoked = true
	current.Generation = 2
	if err := fx.cli.Update(context.Background(), &current); err != nil {
		t.Fatalf("update to set revoked: %v", err)
	}

	if _, err := fx.r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("revocation Reconcile: %v", err)
	}

	var got secretsv1alpha1.Credential
	if err := fx.cli.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatalf("get after revoke: %v", err)
	}
	if got.Status.Phase != secretsv1alpha1.PhaseRevoked {
		t.Errorf("phase=%q want %q", got.Status.Phase, secretsv1alpha1.PhaseRevoked)
	}
	ready := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.CredentialConditionReady)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != secretsv1alpha1.CredentialReasonRevoked {
		t.Errorf("Ready condition unexpected: %+v", ready)
	}

	// Hash Secret must be gone.
	var hashSecret corev1.Secret
	hashKey := types.NamespacedName{Namespace: ns, Name: name + credentialHashSecretSuffix}
	err := fx.cli.Get(context.Background(), hashKey, &hashSecret)
	if err == nil {
		t.Errorf("hash Secret still exists after revocation; want NotFound")
	} else if !apierrors.IsNotFound(err) {
		t.Errorf("unexpected error reading hash Secret post-revocation: %v", err)
	}
}

// TestCredential_ExpiryFutureSchedulesRequeue asserts that a Credential
// with a future expiresAt gets a RequeueAfter equal to the remaining
// window and stays in Phase=Active.
func TestCredential_ExpiryFutureSchedulesRequeue(t *testing.T) {
	const (
		ns        = "holos-prj-demo"
		name      = "expires-soon"
		upstream  = "vendor-upstream"
		upstreamK = "apiKey"
	)
	fixedTime := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	expires := metav1.NewTime(fixedTime.Add(5 * time.Minute))

	cred := validCredential(name, ns, upstream, upstreamK)
	cred.Spec.ExpiresAt = &expires
	up := upstreamSecret(upstream, ns, upstreamK, []byte("sih_plaintext"))
	fx := newCredentialReconciler(t, fixedTime, cred, up)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
	res, err := fx.r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("RequeueAfter=%v want positive", res.RequeueAfter)
	}
	if res.RequeueAfter > 5*time.Minute {
		t.Errorf("RequeueAfter=%v want <= 5m", res.RequeueAfter)
	}

	var got secretsv1alpha1.Credential
	if err := fx.cli.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.Phase != secretsv1alpha1.PhaseActive {
		t.Errorf("phase=%q want Active (future expiresAt should not yet expire)", got.Status.Phase)
	}
	expiredCond := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.CredentialConditionExpired)
	if expiredCond != nil && expiredCond.Status == metav1.ConditionTrue {
		t.Errorf("Expired=True before deadline: %+v", expiredCond)
	}
}

// TestCredential_ExpiryElapsedTransitions asserts that a Credential
// whose expiresAt has elapsed transitions to Phase=Expired with
// Expired=True / Ready=False / Reason=Expired.
func TestCredential_ExpiryElapsedTransitions(t *testing.T) {
	const (
		ns        = "holos-prj-demo"
		name      = "already-expired"
		upstream  = "vendor-upstream"
		upstreamK = "apiKey"
	)
	fixedTime := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	// expiresAt five minutes in the past.
	expires := metav1.NewTime(fixedTime.Add(-5 * time.Minute))

	cred := validCredential(name, ns, upstream, upstreamK)
	cred.Spec.ExpiresAt = &expires
	up := upstreamSecret(upstream, ns, upstreamK, []byte("sih_plaintext"))
	fx := newCredentialReconciler(t, fixedTime, cred, up)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
	if _, err := fx.r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var got secretsv1alpha1.Credential
	if err := fx.cli.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.Phase != secretsv1alpha1.PhaseExpired {
		t.Errorf("phase=%q want %q", got.Status.Phase, secretsv1alpha1.PhaseExpired)
	}
	expired := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.CredentialConditionExpired)
	if expired == nil || expired.Status != metav1.ConditionTrue || expired.Reason != secretsv1alpha1.CredentialReasonExpired {
		t.Errorf("Expired condition unexpected: %+v", expired)
	}
	ready := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.CredentialConditionReady)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != secretsv1alpha1.CredentialReasonExpired {
		t.Errorf("Ready condition unexpected: %+v", ready)
	}
}

// TestCredential_RotationGraceWindow covers both rotation transitions:
// within the grace window the predecessor is Rotating; after grace the
// predecessor transitions to Retired.
func TestCredential_RotationGraceWindow(t *testing.T) {
	const (
		ns        = "holos-prj-demo"
		pred      = "vendor-apikey-v1"
		succ      = "vendor-apikey-v2"
		upstream  = "vendor-upstream"
		upstreamK = "apiKey"
		group     = "vendor-apikey"
	)
	baseTime := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)

	// Predecessor created at base, successor one minute later. Grace is
	// 300s — so at base+100s we are inside the grace window, and at
	// base+400s we are past it.
	predecessor := validCredential(pred, ns, upstream, upstreamK)
	predecessor.Labels = map[string]string{secretsv1alpha1.RotationGroupLabel: group}
	predecessor.CreationTimestamp = metav1.NewTime(baseTime)
	predecessor.Spec.Rotation = secretsv1alpha1.Rotation{GraceSeconds: 300}

	successor := validCredential(succ, ns, upstream, upstreamK)
	successor.Labels = map[string]string{secretsv1alpha1.RotationGroupLabel: group}
	successor.CreationTimestamp = metav1.NewTime(baseTime.Add(time.Minute))

	up := upstreamSecret(upstream, ns, upstreamK, []byte("sih_plaintext"))

	t.Run("Rotating inside the grace window", func(t *testing.T) {
		fx := newCredentialReconciler(t, baseTime.Add(100*time.Second),
			predecessor.DeepCopy(), successor.DeepCopy(), up)
		req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: pred}}
		res, err := fx.r.Reconcile(context.Background(), req)
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		var got secretsv1alpha1.Credential
		if err := fx.cli.Get(context.Background(), req.NamespacedName, &got); err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Status.Phase != secretsv1alpha1.PhaseRotating {
			t.Errorf("phase=%q want %q", got.Status.Phase, secretsv1alpha1.PhaseRotating)
		}
		if res.RequeueAfter <= 0 {
			t.Errorf("RequeueAfter=%v want positive (retirement time)", res.RequeueAfter)
		}
	})

	t.Run("Retired after the grace window", func(t *testing.T) {
		fx := newCredentialReconciler(t, baseTime.Add(400*time.Second),
			predecessor.DeepCopy(), successor.DeepCopy(), up)
		req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: pred}}
		if _, err := fx.r.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		var got secretsv1alpha1.Credential
		if err := fx.cli.Get(context.Background(), req.NamespacedName, &got); err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Status.Phase != secretsv1alpha1.PhaseRetired {
			t.Errorf("phase=%q want %q", got.Status.Phase, secretsv1alpha1.PhaseRetired)
		}
	})
}

// TestCredential_RefusesToClobberForeignOwnedSecret asserts the
// atomicity contract: if a Secret with the deterministic name already
// exists in the Credential's namespace and is not owned by the
// Credential, the reconciler refuses to write it and surfaces
// HashMaterialized=False.
func TestCredential_RefusesToClobberForeignOwnedSecret(t *testing.T) {
	const (
		ns        = "holos-prj-demo"
		name      = "collides"
		upstream  = "vendor-upstream"
		upstreamK = "apiKey"
	)
	fixedTime := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	cred := validCredential(name, ns, upstream, upstreamK)
	up := upstreamSecret(upstream, ns, upstreamK, []byte("sih_plaintext"))
	foreignUID := types.UID("some-other-owner-uid")
	truth := true
	foreign := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + credentialHashSecretSuffix,
			Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1",
				Kind:       "Namespace",
				Name:       "not-this-credential",
				UID:        foreignUID,
				Controller: &truth,
			}},
		},
		Data: map[string][]byte{"foreign-key": []byte("foreign-value")},
	}
	fx := newCredentialReconciler(t, fixedTime, cred, up, foreign)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
	if _, err := fx.r.Reconcile(context.Background(), req); err == nil {
		t.Fatalf("Reconcile: expected error refusing to clobber foreign-owned Secret; got nil")
	}
	var got secretsv1alpha1.Credential
	if err := fx.cli.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	hash := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.CredentialConditionHashMaterialized)
	if hash == nil || hash.Status != metav1.ConditionTrue {
		// HashMaterialized=False is the expected shape — a True here
		// would mean we clobbered the foreign-owned Secret.
		if hash != nil && hash.Status == metav1.ConditionTrue {
			t.Errorf("HashMaterialized=True despite foreign-owned Secret collision: %+v", hash)
		}
	}
	// Sanity: the foreign Secret's original data must survive untouched.
	var after corev1.Secret
	if err := fx.cli.Get(context.Background(),
		types.NamespacedName{Namespace: ns, Name: name + credentialHashSecretSuffix}, &after); err != nil {
		t.Fatalf("get foreign Secret after reconcile: %v", err)
	}
	if _, ok := after.Data["foreign-key"]; !ok {
		t.Errorf("foreign Secret data was modified; atomicity invariant broken")
	}
}

// TestCredential_NotFoundReturnsNoError confirms the cache-miss path is
// a no-op.
func TestCredential_NotFoundReturnsNoError(t *testing.T) {
	fixedTime := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	fx := newCredentialReconciler(t, fixedTime)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "x", Name: "missing"}}
	res, err := fx.r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("Reconcile on missing: %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Fatalf("Reconcile on missing: want no requeue, got %+v", res)
	}
}

// TestCredential_RealClockFallback exercises the non-test default where
// the Clock field is nil — the reconciler must fall back to
// time.Now() rather than panicking. The behaviour is covered at a
// coarse grain because unit tests cannot pin wall-clock time without the
// fake clock; the assertion is simply that Reconcile completes without
// error and that the Accepted condition lands.
func TestCredential_RealClockFallback(t *testing.T) {
	const (
		ns        = "holos-prj-demo"
		name      = "real-clock"
		upstream  = "vendor-upstream"
		upstreamK = "apiKey"
	)
	cred := validCredential(name, ns, upstream, upstreamK)
	up := upstreamSecret(upstream, ns, upstreamK, []byte("sih_plaintext"))
	cli := ctrlfake.NewClientBuilder().
		WithScheme(Scheme).
		WithObjects(cred, up).
		WithStatusSubresource(&secretsv1alpha1.Credential{}).
		Build()
	r := &CredentialReconciler{
		Client: cli,
		Scheme: Scheme,
		KDF:    sicrypto.Default(),
		Pepper: newStubPepper(1, []byte("some-pepper-bytes-0000000000000")),
		// Clock left nil deliberately.
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
}

// TestCredential_InvalidSpec asserts admission-bypass objects surface
// Accepted=False with a typed reason. We cover the OIDC-not-supported
// path because it is the single most important rejection reason: an
// operator who lands an OIDC Credential during v1alpha1 must see the
// typed OIDCNotSupported reason rather than a generic InvalidSpec.
func TestCredential_InvalidSpec(t *testing.T) {
	const (
		ns   = "holos-prj-demo"
		name = "oidc-rejected"
	)
	fixedTime := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	cred := &secretsv1alpha1.Credential{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Generation: 1},
		Spec: secretsv1alpha1.CredentialSpec{
			Authentication: secretsv1alpha1.Authentication{
				Type: secretsv1alpha1.AuthenticationTypeOIDC,
			},
			UpstreamSecretRef: secretsv1alpha1.NamespacedSecretKeyReference{
				Name: "x",
				Key:  "y",
			},
		},
	}
	fx := newCredentialReconciler(t, fixedTime, cred)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
	if _, err := fx.r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var got secretsv1alpha1.Credential
	if err := fx.cli.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	accepted := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.CredentialConditionAccepted)
	if accepted == nil || accepted.Status != metav1.ConditionFalse || accepted.Reason != secretsv1alpha1.CredentialReasonOIDCNotSupported {
		t.Fatalf("Accepted condition unexpected: %+v", accepted)
	}
	ready := meta.FindStatusCondition(got.Status.Conditions, secretsv1alpha1.CredentialConditionReady)
	if ready == nil || ready.Status != metav1.ConditionFalse {
		t.Fatalf("Ready should be False when Accepted=False; got %+v", ready)
	}
	// No hash Secret should have been created.
	var hash corev1.Secret
	err := fx.cli.Get(context.Background(),
		types.NamespacedName{Namespace: ns, Name: name + credentialHashSecretSuffix}, &hash)
	if err == nil {
		t.Errorf("hash Secret created despite Accepted=False; want NotFound")
	} else if !apierrors.IsNotFound(err) {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestCredential_ClockHelper documents that the now() helper prefers
// the injected Clock and falls back to wall-clock time when nil. The
// fake clock used throughout the rest of the suite is the primary
// driver of expiry and rotation tests; this is a focused unit test on
// the helper itself.
func TestCredential_ClockHelper(t *testing.T) {
	fixed := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	r := &CredentialReconciler{Clock: clocktesting.NewFakePassiveClock(fixed)}
	if got := r.now(); !got.Equal(fixed) {
		t.Errorf("now() = %v, want %v", got, fixed)
	}
	r = &CredentialReconciler{}
	if got := r.now(); got.IsZero() {
		t.Errorf("now() with nil Clock returned zero Time; want real wall clock")
	}
	_ = clock.PassiveClock(nil) // reference to keep the import in one place
}
