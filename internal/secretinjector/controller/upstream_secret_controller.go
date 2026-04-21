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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	secretsv1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
)

// upstreamSecretSecretRefNameIndex is the field-indexer key used by the
// manager to list UpstreamSecrets that reference a given v1.Secret by name.
// The key mirrors the spec field it tracks so operators and later
// reconcilers can tell from the name alone what the index keys on. The
// index is scoped per-namespace because v1alpha1 admission rejects
// cross-namespace refs on UpstreamSecret.spec.secretRef (HOL-703), which
// means a (namespace, secretName) pair is a unique O(1) lookup.
const upstreamSecretSecretRefNameIndex = "spec.secretRef.name"

// UpstreamSecretReconciler reconciles an UpstreamSecret object. The contract
// mirrors the templates-group reconcilers:
//
//  1. Fetch the object; NotFound -> no-op.
//  2. Build the Accepted / ResolvedRefs component conditions from the
//     current spec and the resolution of spec.secretRef against a sibling
//     v1.Secret.
//  3. Aggregate into Ready.
//  4. Stamp metadata.generation on every condition plus
//     status.observedGeneration.
//  5. Write status ONLY when ObservedGeneration advances or a condition's
//     (Status, Reason, Message) tuple changes — guards against the classic
//     hot-loop where Reconcile keeps writing the same status and the API
//     server keeps firing watch events back at us.
//
// RBAC markers for this reconciler live on the package doc comment in
// rbac.go — controller-gen's rbac generator ignores markers on struct or
// method doc comments.
//
// The reconciler never reads or writes the credential bytes from the
// referenced v1.Secret: resolution is a Get + key-presence check only. The
// injector's hot path (HOL-712) does the byte read at request time. This
// keeps the CR surface free of sensitive material — see
// api/secrets/v1alpha1/doc.go and ADR 031 "no sensitive values on CRs".
type UpstreamSecretReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// SetupWithManager registers the reconciler with the supplied manager. In
// addition to the primary For(&UpstreamSecret{}), it wires:
//
//   - A field indexer on UpstreamSecret.spec.secretRef.name so the
//     Secret watch mapper looks up referencing UpstreamSecrets in O(1)
//     per namespace.
//   - A Watch on v1.Secret that re-enqueues the UpstreamSecrets whose
//     spec.secretRef.name matches the Secret's name in the same
//     namespace. Cross-namespace churn is ignored because admission
//     forbids cross-namespace refs and List is already namespace-scoped.
func (r *UpstreamSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
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
	); err != nil {
		return fmt.Errorf("indexing UpstreamSecret.%s: %w", upstreamSecretSecretRefNameIndex, err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&secretsv1alpha1.UpstreamSecret{}).
		Named("upstream-secret-controller").
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.upstreamSecretsForSecret),
		).
		Complete(r)
}

// Reconcile implements the reconciliation loop for the UpstreamSecret kind.
// See the type doc for the contract summary.
func (r *UpstreamSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var us secretsv1alpha1.UpstreamSecret
	if err := r.Get(ctx, req.NamespacedName, &us); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get UpstreamSecret: %w", err)
	}

	gen := us.Generation

	accepted := upstreamSecretAcceptedCondition(&us)
	resolved, err := r.upstreamSecretResolvedRefsCondition(ctx, &us)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolving secretRef: %w", err)
	}
	components := []metav1.Condition{accepted, resolved}

	proposed := make([]metav1.Condition, 0, len(components)+1)
	for _, c := range components {
		c.ObservedGeneration = gen
		proposed = append(proposed, c)
	}
	ready := aggregateReady(components,
		secretsv1alpha1.UpstreamSecretReasonReady,
		secretsv1alpha1.UpstreamSecretReasonNotReady,
		"UpstreamSecret is accepted and the referenced v1.Secret carries the named key.",
		"UpstreamSecret is not Ready; see component conditions for details.")
	ready.Type = secretsv1alpha1.UpstreamSecretConditionReady
	ready.ObservedGeneration = gen
	proposed = append(proposed, ready)

	target := us.DeepCopy()
	target.Status.ObservedGeneration = gen
	newConds := append([]metav1.Condition(nil), us.Status.Conditions...)
	for _, pc := range proposed {
		mergeCondition(&newConds, gen, pc)
	}
	target.Status.Conditions = newConds

	if us.Status.ObservedGeneration == gen &&
		conditionsEqualIgnoringTransitionTime(us.Status.Conditions, target.Status.Conditions) {
		logger.V(1).Info("UpstreamSecret status unchanged; skipping update", "generation", gen)
		return ctrl.Result{}, nil
	}

	if err := r.Status().Update(ctx, target); err != nil {
		if apierrors.IsConflict(err) {
			// Someone else beat us to a status update. Requeue
			// immediately: controller-runtime will rate-limit
			// the retry for us.
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update UpstreamSecret status: %w", err)
	}
	if r.Recorder != nil {
		if ready.Status == metav1.ConditionTrue {
			r.Recorder.Eventf(target, "Normal", secretsv1alpha1.UpstreamSecretReasonReady, "UpstreamSecret is Ready")
		} else {
			r.Recorder.Eventf(target, "Warning", ready.Reason, "%s", ready.Message)
		}
	}
	return ctrl.Result{}, nil
}

// upstreamSecretAcceptedCondition enforces the minimum spec invariants the
// admission layer does not cover. v1alpha1 admission already rejects the
// known bad shapes (HOL-703), so on well-formed objects this function
// always returns Accepted=True. We still populate the InvalidSpec reason
// path so objects that bypass admission (kubectl --server-side --force,
// direct etcd writes, etc.) surface their failure on .status rather than
// silently sailing through Ready=True.
func upstreamSecretAcceptedCondition(us *secretsv1alpha1.UpstreamSecret) metav1.Condition {
	if us.Spec.SecretRef.Name == "" {
		return metav1.Condition{
			Type:    secretsv1alpha1.UpstreamSecretConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.UpstreamSecretReasonInvalidSpec,
			Message: "spec.secretRef.name must not be empty",
		}
	}
	if us.Spec.SecretRef.Key == "" {
		return metav1.Condition{
			Type:    secretsv1alpha1.UpstreamSecretConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.UpstreamSecretReasonInvalidSpec,
			Message: "spec.secretRef.key must not be empty",
		}
	}
	if us.Spec.Upstream.Host == "" {
		return metav1.Condition{
			Type:    secretsv1alpha1.UpstreamSecretConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.UpstreamSecretReasonInvalidSpec,
			Message: "spec.upstream.host must not be empty",
		}
	}
	if us.Spec.Upstream.Scheme != "http" && us.Spec.Upstream.Scheme != "https" {
		return metav1.Condition{
			Type:    secretsv1alpha1.UpstreamSecretConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.UpstreamSecretReasonInvalidSpec,
			Message: fmt.Sprintf("spec.upstream.scheme %q must be \"http\" or \"https\"", us.Spec.Upstream.Scheme),
		}
	}
	if us.Spec.Injection.Header == "" {
		return metav1.Condition{
			Type:    secretsv1alpha1.UpstreamSecretConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.UpstreamSecretReasonInvalidSpec,
			Message: "spec.injection.header must not be empty",
		}
	}
	return metav1.Condition{
		Type:    secretsv1alpha1.UpstreamSecretConditionAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  secretsv1alpha1.UpstreamSecretReasonAccepted,
		Message: "spec passed reconciler validation",
	}
}

// upstreamSecretResolvedRefsCondition returns the ResolvedRefs condition
// derived from the live cluster state of spec.secretRef. The function is
// bytes-blind: it only asserts the referenced v1.Secret exists in the same
// namespace as the UpstreamSecret and carries the named key in .data. The
// actual credential bytes are read by the injector's hot path at request
// time, not here — per ADR 031's "no sensitive values on CRs" invariant.
//
// When the Accepted condition has already flagged the spec as invalid, the
// secretRef fields we need (Name, Key) may be empty; we short-circuit to
// preserve a useful reason on ResolvedRefs without chasing a nonsense Get.
func (r *UpstreamSecretReconciler) upstreamSecretResolvedRefsCondition(ctx context.Context, us *secretsv1alpha1.UpstreamSecret) (metav1.Condition, error) {
	if us.Spec.SecretRef.Name == "" || us.Spec.SecretRef.Key == "" {
		return metav1.Condition{
			Type:    secretsv1alpha1.UpstreamSecretConditionResolvedRefs,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.UpstreamSecretReasonSecretNotFound,
			Message: "spec.secretRef is incomplete; cannot resolve",
		}, nil
	}

	var secret corev1.Secret
	key := types.NamespacedName{Namespace: us.Namespace, Name: us.Spec.SecretRef.Name}
	if err := r.Get(ctx, key, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return metav1.Condition{
				Type:    secretsv1alpha1.UpstreamSecretConditionResolvedRefs,
				Status:  metav1.ConditionFalse,
				Reason:  secretsv1alpha1.UpstreamSecretReasonSecretNotFound,
				Message: fmt.Sprintf("Secret %s/%s not found", us.Namespace, us.Spec.SecretRef.Name),
			}, nil
		}
		return metav1.Condition{}, fmt.Errorf("get Secret %s: %w", key, err)
	}
	if _, ok := secret.Data[us.Spec.SecretRef.Key]; !ok {
		return metav1.Condition{
			Type:   secretsv1alpha1.UpstreamSecretConditionResolvedRefs,
			Status: metav1.ConditionFalse,
			Reason: secretsv1alpha1.UpstreamSecretReasonSecretKeyMissing,
			Message: fmt.Sprintf("Secret %s/%s exists but key %q is not set in .data",
				us.Namespace, us.Spec.SecretRef.Name, us.Spec.SecretRef.Key),
		}, nil
	}
	return metav1.Condition{
		Type:    secretsv1alpha1.UpstreamSecretConditionResolvedRefs,
		Status:  metav1.ConditionTrue,
		Reason:  secretsv1alpha1.UpstreamSecretReasonResolvedRefs,
		Message: fmt.Sprintf("Secret %s/%s carries key %q", us.Namespace, us.Spec.SecretRef.Name, us.Spec.SecretRef.Key),
	}, nil
}

// upstreamSecretsForSecret returns reconcile requests for every
// UpstreamSecret whose spec.secretRef.name matches the supplied Secret's
// name, scoped to the same namespace. The mapper powers the v1.Secret
// watch registered in SetupWithManager so a late-arriving Secret (or a
// rotated .data payload that only now carries the required key) re-drives
// the UpstreamSecret reconcile.
//
// The field indexer installed in SetupWithManager makes this a single
// namespace-scoped List by spec.secretRef.name, so cross-namespace Secret
// churn on the cluster never enqueues reconciles on bystander
// UpstreamSecrets.
func (r *UpstreamSecretReconciler) upstreamSecretsForSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}
	var list secretsv1alpha1.UpstreamSecretList
	if err := r.List(ctx, &list,
		client.InNamespace(secret.Namespace),
		client.MatchingFields{upstreamSecretSecretRefNameIndex: secret.Name},
	); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0, len(list.Items))
	for _, us := range list.Items {
		out = append(out, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: us.Namespace,
				Name:      us.Name,
			},
		})
	}
	return out
}
