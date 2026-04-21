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
	"crypto/rand"
	"fmt"
	"time"

	"github.com/segmentio/ksuid"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	secretsv1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
	sicrypto "github.com/holos-run/holos-console/internal/secretinjector/crypto"
)

// credentialHashSaltLength is the per-credential random salt size in bytes
// the reconciler feeds into [sicrypto.KDF.Hash]. 32 bytes matches the
// argon2id recommendation in RFC 9106 §3.1 "salt" and the pepper seed
// length in [sicrypto.PepperSeedLength] — keeping salt and pepper at the
// same entropy floor avoids a surprise asymmetry if the pepper is ever
// rotated to a shorter value.
const credentialHashSaltLength = 32

// credentialHashSecretSuffix is the suffix the reconciler appends to the
// Credential's metadata.name when it materialises the sibling hash
// v1.Secret. The deterministic name lets operators `kubectl get
// secrets <credentialName>-hash` in the Credential's namespace to
// locate the hash material; the Credential's Status.HashSecretRef is the
// authoritative pointer either way. Never hoist the suffix into a Credential
// spec field — the name is a reconciler implementation detail, not
// user-facing API.
const credentialHashSecretSuffix = "-hash"

// credentialHashEnvelopeKey is the .data key on the sibling hash
// v1.Secret carrying the JSON-serialised [sicrypto.Envelope]. JSON (rather
// than the raw argon2 bytes) is deliberate: an operator who opens the
// Secret sees a self-describing record — algorithm, params, pepper
// version, salt, hash — not an opaque blob. This does NOT violate the
// "no sensitive values on CRs" invariant because the bytes still live
// exclusively in the v1.Secret, which has the tighter RBAC surface
// (see api/secrets/v1alpha1/doc.go).
//
// JSON contract — the value at this key is a UTF-8 JSON object produced by
// [sicrypto.MarshalEnvelope]. The canonical shape (schemaVersion=1) is:
//
//	{
//	  "schemaVersion": 1,
//	  "kdf": "argon2id",
//	  "kdfParams": {"time":2,"memory":19456,"parallelism":1,"keyLength":32},
//	  "pepperVersion": "<decimal-integer-string>",
//	  "salt": "<base64-encoded 32-byte random salt>",
//	  "hash": "<base64-encoded 32-byte derived hash>"
//	}
//
// Immutability: the reconciler writes this key once (at materialisation)
// and does not update it on subsequent reconciles unless the hash Secret
// is missing or unowned. Callers MUST NOT mutate the bytes after write;
// an in-place edit breaks the Verify path without surfacing a condition
// change. To re-hash (for example after a cost-bump migration), delete
// the Credential and recreate it so the reconciler mints a fresh envelope.
const credentialHashEnvelopeKey = "envelope"

// credentialExpiryRequeueFloor is the minimum Reconcile.RequeueAfter the
// reconciler uses when scheduling an expiry check. A Credential whose
// spec.expiresAt is already past still produces a requeue of this
// duration rather than zero so controller-runtime's rate limiter can
// collapse adjacent wakeups; the reconciler always promotes phase to
// Expired synchronously when it observes the past deadline.
const credentialExpiryRequeueFloor = time.Second

// CredentialReconciler reconciles a Credential object. It is the anchor
// of M2: on first observation of an Accepted Credential that has not yet
// been minted, the reconciler mints a KSUID credentialID, generates a
// 32-byte per-credential salt, invokes the pluggable [sicrypto.KDF] with
// the active pepper from [sicrypto.Loader], and materialises the
// resulting [sicrypto.Envelope] into a sibling v1.Secret that carries a
// single ownerReference back to the Credential. The Credential itself
// never carries plaintext, hash bytes, salt, pepper bytes, prefix, or
// last-4 — the invariant from api/secrets/v1alpha1/doc.go is the
// dominant invariant of this package.
//
// The reconciler additionally owns Credential lifecycle:
//
//   - Rotation grace: when a successor Credential appears in the same
//     namespace carrying the same value for
//     [secretsv1alpha1.RotationGroupLabel], the predecessor transitions to
//     Phase=Rotating and schedules a requeue after
//     spec.rotation.graceSeconds to transition to Phase=Retired. If
//     GraceSeconds is zero the predecessor retires immediately.
//   - Revocation cascade: spec.revoked=true drives Phase=Revoked
//     (terminal); the hash v1.Secret is deleted via
//     DeletePropagationBackground and Ready=False/Reason=Revoked is
//     published.
//   - Expiry requeue: spec.expiresAt in the future schedules a requeue
//     via ctrl.Result{RequeueAfter}; an elapsed expiresAt drives
//     Phase=Expired with Expired=True / Ready=False.
//
// Plaintext source bridge — BIGGEST M2 SHORTCUT:
//
// Until the issuer RPC lands in M4, the plaintext used for hashing is
// read from the upstream v1.Secret named by spec.upstreamSecretRef
// (namespace defaulted to the Credential's own namespace when unset).
// The read surface is RBAC-protected and the bytes never flow through
// the Credential CR, but this means the "Credential is a hash of a
// caller-facing key, not of an upstream secret" flow is stubbed. See
// the TODO(HOL-675) comment in materialiseHashEnvelope below.
type CredentialReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// KDF derives the hash + salt envelope written to the sibling
	// v1.Secret. Never nil once the reconciler is registered with a
	// manager; [NewManager] wires [sicrypto.Default] by default.
	KDF sicrypto.KDF
	// Pepper resolves the active pepper bytes at Hash time. Never nil
	// once the reconciler is registered; [NewManager] wires a
	// direct-client-backed [sicrypto.SecretLoader] so the read does not
	// require list/watch on core/v1 Secret.
	Pepper sicrypto.Loader
	// Clock is the time source used for expiry comparisons. A real
	// clock in production; tests override with a fake clock to drive
	// expiry transitions deterministically. Nil is treated as
	// clock.RealClock.
	Clock clock.PassiveClock
}

// SetupWithManager registers the reconciler with the supplied manager.
// Besides the primary For(&Credential{}), the Owns(&v1.Secret{}) watch
// enqueues the owner Credential whenever the hash Secret churns — a
// direct-edit or accidental delete of the hash Secret re-drives a
// Reconcile that re-materialises it.
func (r *CredentialReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&secretsv1alpha1.Credential{}).
		Owns(&corev1.Secret{}).
		Named("credential-controller").
		Complete(r)
}

// Reconcile implements the reconciliation loop for the Credential kind.
// See the type doc for the contract summary.
//
// The method dispatches to one of four mutually exclusive lifecycle
// branches, in priority order:
//
//  1. Revocation (spec.revoked=true) is terminal — the hash Secret is
//     deleted and Phase=Revoked is published.
//  2. Expiry (spec.expiresAt elapsed) drives Phase=Expired; a future
//     expiresAt schedules a requeue.
//  3. Rotation (a successor exists in the same rotation group) drives
//     Phase=Rotating; after the grace window the predecessor transitions
//     to Phase=Retired.
//  4. Steady-state materialisation: if Accepted and no hash Secret has
//     been written yet, mint a credentialID, generate salt, Hash, and
//     write the sibling v1.Secret; publish Phase=Active and
//     HashMaterialized=True.
//
// Every branch funnels through writeStatus so the hot-loop guard in
// status.go applies uniformly.
func (r *CredentialReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var cred secretsv1alpha1.Credential
	if err := r.Get(ctx, req.NamespacedName, &cred); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get Credential: %w", err)
	}

	now := r.now()
	gen := cred.Generation

	accepted := credentialAcceptedCondition(&cred)

	// Revocation short-circuits every other branch. The hash Secret is
	// garbage-collected by the ownerReference once the Credential is
	// deleted, but revocation is not deletion — we eagerly delete the
	// Secret here so a Revoked credential cannot be verified against a
	// stale envelope. A phantom hash Secret after a revocation would be a
	// bypass the data plane must not be asked to close.
	if cred.Spec.Revoked {
		if err := r.deleteHashSecret(ctx, &cred); err != nil {
			return ctrl.Result{}, fmt.Errorf("delete hash Secret on revocation: %w", err)
		}
		target := cred.DeepCopy()
		target.Status.Phase = secretsv1alpha1.PhaseRevoked
		target.Status.HashSecretRef = nil
		components := []metav1.Condition{accepted, {
			Type:    secretsv1alpha1.CredentialConditionHashMaterialized,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.CredentialReasonRevoked,
			Message: "Credential is revoked; hash Secret deleted",
		}}
		ready := metav1.Condition{
			Type:    secretsv1alpha1.CredentialConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.CredentialReasonRevoked,
			Message: "Credential is revoked",
		}
		return r.writeStatus(ctx, &cred, target, gen, components, ready)
	}

	// Expiry has next priority: once expiresAt has elapsed the Credential
	// is dead to the data plane no matter what the materialisation state
	// was. Publish Phase=Expired + Expired=True and skip materialisation
	// so a reconciler restart cannot accidentally re-mint a hash Secret
	// for an expired credential.
	if cred.Spec.ExpiresAt != nil && !cred.Spec.ExpiresAt.Time.After(now) {
		target := cred.DeepCopy()
		target.Status.Phase = secretsv1alpha1.PhaseExpired
		expired := metav1.Condition{
			Type:    secretsv1alpha1.CredentialConditionExpired,
			Status:  metav1.ConditionTrue,
			Reason:  secretsv1alpha1.CredentialReasonExpired,
			Message: fmt.Sprintf("spec.expiresAt %s has elapsed", cred.Spec.ExpiresAt.Time.UTC().Format(time.RFC3339)),
		}
		components := []metav1.Condition{accepted, expired}
		ready := metav1.Condition{
			Type:    secretsv1alpha1.CredentialConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.CredentialReasonExpired,
			Message: "Credential has expired",
		}
		return r.writeStatus(ctx, &cred, target, gen, components, ready)
	}

	// Rotation: detect a successor in the same rotation group. When found,
	// the predecessor (older creationTimestamp) steps down to Rotating
	// and, after graceSeconds, to Retired.
	successor, err := r.findRotationSuccessor(ctx, &cred)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("lookup rotation successor: %w", err)
	}
	if successor != nil {
		elapsed := now.Sub(successor.CreationTimestamp.Time)
		grace := time.Duration(cred.Spec.Rotation.GraceSeconds) * time.Second
		target := cred.DeepCopy()
		components := []metav1.Condition{accepted}
		if elapsed >= grace {
			target.Status.Phase = secretsv1alpha1.PhaseRetired
			ready := metav1.Condition{
				Type:    secretsv1alpha1.CredentialConditionReady,
				Status:  metav1.ConditionFalse,
				Reason:  secretsv1alpha1.CredentialReasonNotReady,
				Message: "Credential retired after rotation grace window",
			}
			return r.writeStatus(ctx, &cred, target, gen, components, ready)
		}
		target.Status.Phase = secretsv1alpha1.PhaseRotating
		ready := metav1.Condition{
			Type:    secretsv1alpha1.CredentialConditionReady,
			Status:  metav1.ConditionTrue,
			Reason:  secretsv1alpha1.CredentialReasonReady,
			Message: "Credential is serving inside the rotation grace window",
		}
		res, werr := r.writeStatus(ctx, &cred, target, gen, components, ready)
		if werr != nil {
			return res, werr
		}
		// Schedule the retirement requeue. Controller-runtime picks the
		// earliest of the existing result and the new RequeueAfter, so we
		// can safely stamp it unconditionally.
		remaining := grace - elapsed
		if remaining < credentialExpiryRequeueFloor {
			remaining = credentialExpiryRequeueFloor
		}
		res.RequeueAfter = remaining
		return res, nil
	}

	// Steady-state: if the Credential is Accepted and has not yet been
	// materialised, mint the KSUID, salt, and hash envelope, and persist
	// the sibling v1.Secret. Objects that fail Accepted — for example,
	// an OIDC-typed Credential admitted through a cluster that lacks the
	// admission policy — surface the reason in Accepted and stay Not
	// Ready without entering the materialisation path.
	if accepted.Status != metav1.ConditionTrue {
		target := cred.DeepCopy()
		components := []metav1.Condition{accepted}
		ready := metav1.Condition{
			Type:    secretsv1alpha1.CredentialConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.CredentialReasonNotReady,
			Message: "Credential spec was not accepted; see Accepted condition",
		}
		return r.writeStatus(ctx, &cred, target, gen, components, ready)
	}

	target := cred.DeepCopy()
	if target.Status.CredentialID == "" {
		target.Status.CredentialID = ksuid.New().String()
	}

	if target.Status.HashSecretRef == nil || !r.hashSecretExists(ctx, target) {
		pepperVersion, envelope, err := r.materialiseHashEnvelope(ctx, target)
		if err != nil {
			// A failure to materialise is a retryable error — surface it
			// as HashMaterialized=False so operators see the Reason and
			// return the error so controller-runtime backs off on retry.
			target.Status.Phase = secretsv1alpha1.PhaseActive
			hashMaterialized := metav1.Condition{
				Type:    secretsv1alpha1.CredentialConditionHashMaterialized,
				Status:  metav1.ConditionFalse,
				Reason:  secretsv1alpha1.CredentialReasonHashSecretMissing,
				Message: fmt.Sprintf("materialise hash envelope: %v", err),
			}
			components := []metav1.Condition{accepted, hashMaterialized}
			ready := metav1.Condition{
				Type:    secretsv1alpha1.CredentialConditionReady,
				Status:  metav1.ConditionFalse,
				Reason:  secretsv1alpha1.CredentialReasonNotReady,
				Message: "Credential hash envelope not materialised; see HashMaterialized condition",
			}
			if _, werr := r.writeStatus(ctx, &cred, target, gen, components, ready); werr != nil {
				// Prefer the materialise error over the status write
				// error so the operator sees the root cause.
				logger.Error(werr, "write status after materialise failure")
			}
			return ctrl.Result{}, err
		}
		target.Status.PepperVersion = pepperVersion
		target.Status.HashSecretRef = &secretsv1alpha1.SecretKeyReference{
			Name: envelope.secretName,
			Key:  credentialHashEnvelopeKey,
		}
	}

	target.Status.Phase = secretsv1alpha1.PhaseActive
	hashMaterialized := metav1.Condition{
		Type:    secretsv1alpha1.CredentialConditionHashMaterialized,
		Status:  metav1.ConditionTrue,
		Reason:  secretsv1alpha1.CredentialReasonHashMaterialized,
		Message: fmt.Sprintf("hash envelope materialised at Secret %s/%s", cred.Namespace, target.Status.HashSecretRef.Name),
	}
	components := []metav1.Condition{accepted, hashMaterialized}
	ready := metav1.Condition{
		Type:    secretsv1alpha1.CredentialConditionReady,
		Status:  metav1.ConditionTrue,
		Reason:  secretsv1alpha1.CredentialReasonReady,
		Message: "Credential is accepted and its hash envelope is materialised",
	}
	res, werr := r.writeStatus(ctx, &cred, target, gen, components, ready)
	if werr != nil {
		return res, werr
	}

	// Steady-state Credentials with a future expiresAt schedule a requeue
	// so the reconciler wakes up exactly when the deadline elapses. The
	// requeue is capped by controller-runtime's max-requeue knob on the
	// retry side; the floor keeps an already-imminent deadline from
	// scheduling a zero requeue that would hot-loop.
	if cred.Spec.ExpiresAt != nil {
		until := cred.Spec.ExpiresAt.Time.Sub(now)
		if until < credentialExpiryRequeueFloor {
			until = credentialExpiryRequeueFloor
		}
		res.RequeueAfter = until
	}
	return res, nil
}

// hashEnvelopeResult carries the successful return shape of
// materialiseHashEnvelope. Kept private to the package so the return
// values do not leak onto exported API.
type hashEnvelopeResult struct {
	secretName string
}

// materialiseHashEnvelope runs the full hash materialisation path:
// resolve plaintext from the upstream v1.Secret, fetch the active pepper
// from the [sicrypto.Loader], mint a salt, call [sicrypto.KDF.Hash], and
// write a sibling v1.Secret with a single ownerReference back to the
// Credential. Returns the active pepper version and the materialised
// Secret's name.
//
// TODO(HOL-675): the plaintext source is the upstream v1.Secret named by
// spec.upstreamSecretRef as a bridge until the issuer RPC lands (HOL-675
// M4 plan). When the issuer ships, this read is replaced by an
// authenticated per-request surface and the Credential no longer reads
// the upstream Secret.
func (r *CredentialReconciler) materialiseHashEnvelope(ctx context.Context, cred *secretsv1alpha1.Credential) (int32, hashEnvelopeResult, error) {
	plaintext, err := r.readUpstreamPlaintext(ctx, cred)
	if err != nil {
		return 0, hashEnvelopeResult{}, err
	}

	pepperVersion, pepperBytes, err := r.Pepper.Active(ctx)
	if err != nil {
		return 0, hashEnvelopeResult{}, fmt.Errorf("load active pepper: %w", err)
	}

	salt := make([]byte, credentialHashSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return 0, hashEnvelopeResult{}, fmt.Errorf("generate salt: %w", err)
	}

	envelope, err := r.KDF.Hash(plaintext, salt, pepperBytes, fmt.Sprintf("%d", pepperVersion), r.KDF.DefaultParams())
	if err != nil {
		return 0, hashEnvelopeResult{}, fmt.Errorf("kdf hash: %w", err)
	}
	encoded, err := sicrypto.MarshalEnvelope(envelope)
	if err != nil {
		return 0, hashEnvelopeResult{}, fmt.Errorf("marshal envelope: %w", err)
	}

	secretName := cred.Name + credentialHashSecretSuffix
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: cred.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			credentialHashEnvelopeKey: encoded,
		},
	}
	if err := ctrl.SetControllerReference(cred, secret, r.Scheme); err != nil {
		return 0, hashEnvelopeResult{}, fmt.Errorf("set controller reference: %w", err)
	}

	// Create-or-Update with ownership enforcement: if a Secret with the
	// derived name already exists but is NOT owned by this Credential,
	// the atomicity contract requires us to refuse rather than clobber.
	// A Credential delete only garbage-collects objects it owns, so
	// reusing a foreign-owned Secret would leak hash material past the
	// Credential's lifetime.
	var existing corev1.Secret
	key := types.NamespacedName{Namespace: cred.Namespace, Name: secretName}
	switch err := r.Get(ctx, key, &existing); {
	case apierrors.IsNotFound(err):
		if err := r.Create(ctx, secret); err != nil {
			return 0, hashEnvelopeResult{}, fmt.Errorf("create hash Secret: %w", err)
		}
	case err != nil:
		return 0, hashEnvelopeResult{}, fmt.Errorf("get existing hash Secret: %w", err)
	default:
		if !isOwnedByCredential(&existing, cred) {
			return 0, hashEnvelopeResult{}, fmt.Errorf(
				"hash Secret %s/%s exists but is not owned by Credential %s; refusing to clobber",
				cred.Namespace, secretName, cred.Name,
			)
		}
		existing.Type = corev1.SecretTypeOpaque
		existing.Data = map[string][]byte{
			credentialHashEnvelopeKey: encoded,
		}
		if err := ctrl.SetControllerReference(cred, &existing, r.Scheme); err != nil {
			return 0, hashEnvelopeResult{}, fmt.Errorf("set controller reference on update: %w", err)
		}
		if err := r.Update(ctx, &existing); err != nil {
			return 0, hashEnvelopeResult{}, fmt.Errorf("update hash Secret: %w", err)
		}
	}
	return pepperVersion, hashEnvelopeResult{secretName: secretName}, nil
}

// readUpstreamPlaintext fetches the plaintext the KDF will hash from the
// upstream v1.Secret referenced by spec.upstreamSecretRef. Cross-namespace
// references are rejected at admission (see HOL-703), so an empty
// Namespace field defaults to the Credential's own namespace.
//
// TODO(HOL-675): replace this read with the authenticated per-request
// surface exposed by the issuer RPC. Until HOL-675 lands, this read is
// the single biggest plaintext-path shortcut in the injector's M2 scope.
func (r *CredentialReconciler) readUpstreamPlaintext(ctx context.Context, cred *secretsv1alpha1.Credential) ([]byte, error) {
	ns := cred.Spec.UpstreamSecretRef.Namespace
	if ns == "" {
		ns = cred.Namespace
	}
	name := cred.Spec.UpstreamSecretRef.Name
	key := cred.Spec.UpstreamSecretRef.Key
	if name == "" || key == "" {
		return nil, fmt.Errorf("spec.upstreamSecretRef name and key must be set")
	}
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &secret); err != nil {
		return nil, fmt.Errorf("get upstream Secret %s/%s: %w", ns, name, err)
	}
	plaintext, ok := secret.Data[key]
	if !ok || len(plaintext) == 0 {
		return nil, fmt.Errorf("upstream Secret %s/%s missing key %q", ns, name, key)
	}
	// Return a defensive copy so the KDF cannot see a later mutation of
	// the fake client's map when running in tests.
	out := make([]byte, len(plaintext))
	copy(out, plaintext)
	return out, nil
}

// hashSecretExists reports whether the Secret named by
// cred.Status.HashSecretRef still exists in the Credential's namespace.
// The probe exists so a hash Secret that was manually deleted between
// reconciles triggers re-materialisation rather than leaving a stale
// HashSecretRef pointing at nothing. Not-found and found-but-wrong-owner
// both count as "does not exist" for re-materialisation purposes.
func (r *CredentialReconciler) hashSecretExists(ctx context.Context, cred *secretsv1alpha1.Credential) bool {
	if cred.Status.HashSecretRef == nil {
		return false
	}
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: cred.Namespace,
		Name:      cred.Status.HashSecretRef.Name,
	}, &secret); err != nil {
		return false
	}
	return isOwnedByCredential(&secret, cred)
}

// deleteHashSecret removes the Credential's sibling hash v1.Secret (if
// one exists) so a revoked Credential leaves no verifiable envelope
// behind. DeletePropagationBackground lets the API server reclaim
// dependents asynchronously; the reconciler does not wait.
//
// A not-found response is tolerated — the Credential may have been
// observed before any Secret was materialised, or the Secret may have
// been garbage-collected already.
//
// When Status.HashSecretRef is nil (no materialisation has been recorded
// yet, or a previous revocation reconcile already cleared it) the helper
// still falls back to the deterministic Secret name — this closes the
// window where a revocation races an in-flight materialisation that
// already wrote the Secret but has not yet stamped HashSecretRef.
func (r *CredentialReconciler) deleteHashSecret(ctx context.Context, cred *secretsv1alpha1.Credential) error {
	name := cred.Name + credentialHashSecretSuffix
	if cred.Status.HashSecretRef != nil {
		name = cred.Status.HashSecretRef.Name
	}
	prop := metav1.DeletePropagationBackground
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cred.Namespace,
		},
	}
	if err := r.Delete(ctx, secret, &client.DeleteOptions{PropagationPolicy: &prop}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// findRotationSuccessor returns the Credential in the same namespace
// that carries the same value for [secretsv1alpha1.RotationGroupLabel]
// AND a strictly newer metadata.creationTimestamp than cred. Returns
// (nil, nil) when the label is absent, empty, or no successor exists.
//
// The lookup is a namespace-scoped List by label — which the controller-
// runtime cache-backed client serves from memory without hitting the API
// server in steady state, so enumerating siblings per reconcile is cheap
// even in namespaces with many Credentials.
func (r *CredentialReconciler) findRotationSuccessor(ctx context.Context, cred *secretsv1alpha1.Credential) (*secretsv1alpha1.Credential, error) {
	group := cred.Labels[secretsv1alpha1.RotationGroupLabel]
	if group == "" {
		return nil, nil
	}
	var list secretsv1alpha1.CredentialList
	if err := r.List(ctx, &list,
		client.InNamespace(cred.Namespace),
		client.MatchingLabels{secretsv1alpha1.RotationGroupLabel: group},
	); err != nil {
		return nil, err
	}
	var successor *secretsv1alpha1.Credential
	for i := range list.Items {
		candidate := &list.Items[i]
		if candidate.Name == cred.Name {
			continue
		}
		if !candidate.CreationTimestamp.After(cred.CreationTimestamp.Time) {
			continue
		}
		// Revoked or Retired successors are not a successor for
		// rotation — they cannot carry over the load. Leave the
		// predecessor in steady state if the would-be successor has
		// already been revoked or retired.
		if candidate.Spec.Revoked {
			continue
		}
		if candidate.Status.Phase == secretsv1alpha1.PhaseRetired {
			continue
		}
		if successor == nil || candidate.CreationTimestamp.Before(&successor.CreationTimestamp) {
			successor = candidate
		}
	}
	return successor, nil
}

// writeStatus proposes the component conditions + aggregated Ready,
// stamps metadata.generation on every condition plus
// status.observedGeneration, and writes the update only when the
// hot-loop guard detects an actual change. The helper centralises the
// write so every lifecycle branch in Reconcile funnels through the same
// bookkeeping and the hot-loop assertion in credential_controller_test.go
// covers every branch.
func (r *CredentialReconciler) writeStatus(ctx context.Context, original, target *secretsv1alpha1.Credential, gen int64, components []metav1.Condition, ready metav1.Condition) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	target.Status.ObservedGeneration = gen
	newConds := append([]metav1.Condition(nil), original.Status.Conditions...)
	proposed := make([]metav1.Condition, 0, len(components)+1)
	for _, c := range components {
		c.ObservedGeneration = gen
		proposed = append(proposed, c)
	}
	ready.ObservedGeneration = gen
	proposed = append(proposed, ready)
	for _, pc := range proposed {
		mergeCondition(&newConds, gen, pc)
	}
	target.Status.Conditions = newConds

	if original.Status.ObservedGeneration == gen &&
		original.Status.Phase == target.Status.Phase &&
		original.Status.CredentialID == target.Status.CredentialID &&
		original.Status.PepperVersion == target.Status.PepperVersion &&
		hashRefEqual(original.Status.HashSecretRef, target.Status.HashSecretRef) &&
		conditionsEqualIgnoringTransitionTime(original.Status.Conditions, target.Status.Conditions) {
		logger.V(1).Info("Credential status unchanged; skipping update", "generation", gen)
		return ctrl.Result{}, nil
	}

	if err := r.Status().Update(ctx, target); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update Credential status: %w", err)
	}
	if r.Recorder != nil {
		if ready.Status == metav1.ConditionTrue {
			r.Recorder.Eventf(target, "Normal", secretsv1alpha1.CredentialReasonReady, "Credential is Ready")
		} else {
			r.Recorder.Eventf(target, "Warning", ready.Reason, "%s", ready.Message)
		}
	}
	return ctrl.Result{}, nil
}

// now returns the reconciler's current wall-clock time, preferring the
// injected Clock when non-nil. Extracted so tests override expiry and
// rotation timing deterministically.
func (r *CredentialReconciler) now() time.Time {
	if r.Clock != nil {
		return r.Clock.Now()
	}
	return time.Now()
}

// credentialAcceptedCondition enforces the invariants the admission
// layer covers but the reconciler re-checks as belt-and-braces against
// objects that bypass admission (kubectl --server-side --force, direct
// etcd writes). Returns Accepted=True only when every structural
// requirement holds.
func credentialAcceptedCondition(cred *secretsv1alpha1.Credential) metav1.Condition {
	if cred.Spec.Authentication.Type == "" {
		return metav1.Condition{
			Type:    secretsv1alpha1.CredentialConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.CredentialReasonInvalidSpec,
			Message: "spec.authentication.type must be set",
		}
	}
	if cred.Spec.Authentication.Type == secretsv1alpha1.AuthenticationTypeOIDC {
		return metav1.Condition{
			Type:    secretsv1alpha1.CredentialConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.CredentialReasonOIDCNotSupported,
			Message: "spec.authentication.type=OIDC is reserved for a future version",
		}
	}
	if cred.Spec.Authentication.APIKey == nil || cred.Spec.Authentication.APIKey.HeaderName == "" {
		return metav1.Condition{
			Type:    secretsv1alpha1.CredentialConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.CredentialReasonInvalidSpec,
			Message: "spec.authentication.apiKey.headerName must be set when type=APIKey",
		}
	}
	if cred.Spec.UpstreamSecretRef.Name == "" || cred.Spec.UpstreamSecretRef.Key == "" {
		return metav1.Condition{
			Type:    secretsv1alpha1.CredentialConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.CredentialReasonInvalidSpec,
			Message: "spec.upstreamSecretRef name and key must be set",
		}
	}
	return metav1.Condition{
		Type:    secretsv1alpha1.CredentialConditionAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  secretsv1alpha1.CredentialReasonAccepted,
		Message: "spec passed reconciler validation",
	}
}

// isOwnedByCredential reports whether secret carries an owner reference
// pointing to cred with controller=true. Matches on UID (not name) so a
// Credential deleted and recreated with the same name but a fresh UID
// does not inherit the previous Credential's hash Secret — the new
// Credential materialises its own envelope from first principles.
func isOwnedByCredential(secret *corev1.Secret, cred *secretsv1alpha1.Credential) bool {
	for _, o := range secret.OwnerReferences {
		if o.UID == cred.UID && o.Controller != nil && *o.Controller {
			return true
		}
	}
	return false
}

// hashRefEqual reports whether two *SecretKeyReference values are
// equivalent — nil on both sides, or non-nil with equal Name/Key. Used
// by the hot-loop guard so a no-op reconcile that re-derives the same
// pointer value does not trigger a status write.
func hashRefEqual(a, b *secretsv1alpha1.SecretKeyReference) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Name == b.Name && a.Key == b.Key
}
