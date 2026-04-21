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
	"reflect"

	istiosecurityv1beta1 "istio.io/api/security/v1beta1"
	istiotypev1beta1 "istio.io/api/type/v1beta1"
	istiosecurityv1 "istio.io/client-go/pkg/apis/security/v1"
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

// bindingNamespaceParentLabel records the binding-namespace's immediate
// parent namespace name. The admission policy
// secretinjectionpolicybinding-policyref-same-namespace-or-ancestor accepts
// spec.policyRef.namespace equal to the value stored here. The label key
// is duplicated from api/v1alpha2/annotations.go rather than imported
// because the secret-injector package deliberately avoids taking a
// dependency on the holos-console api types: the label convention is a
// cluster-wide invariant shared between the two binaries, not a
// module-level coupling. A future ticket may hoist the constant into a
// shared pkg/ once a second consumer appears.
const bindingNamespaceParentLabel = "console.holos.run/parent"

// bindingNamespaceOrganizationLabel records the binding-namespace's
// owning organization short name. The admission policy synthesises
// `holos-org-<label>` from this value before comparing against
// spec.policyRef.namespace; the reconciler applies the same synthesis so
// its resolution matches the admission gate exactly.
const bindingNamespaceOrganizationLabel = "console.holos.run/organization"

// bindingNamespaceOrganizationPrefix is the Namespace-name prefix the
// holos-console resolver applies to the organization short name. The
// reconciler synthesises `<prefix><org-short>` when resolving policies
// against the organization-namespace ancestor path. Mirrors the default
// used by the admission CEL and docs; clusters that override the
// prefix via resolver.Resolver.NamespacePrefix will need both admission
// and reconciler patched in lockstep (HOL-711 envtest guards this).
const bindingNamespaceOrganizationPrefix = "holos-org-"

// SecretInjectionPolicyBindingReconciler reconciles a
// SecretInjectionPolicyBinding. The reconciliation contract:
//
//  1. Fetch the binding; NotFound -> no-op.
//  2. Build the Accepted component condition from the current spec.
//  3. Resolve spec.policyRef along the admission-validated three-path
//     rule (same namespace, parent-label namespace, or synthesised
//     organization namespace). On failure, publish
//     ResolvedRefs=False/Reason=PolicyNotFound and skip AP emission.
//  4. Build a controller-owned security.istio.io/v1 AuthorizationPolicy
//     whose action=CUSTOM and provider.name names the
//     holos-secret-injector ext_authz provider. Create or Update-in-place
//     with a single ownerReference back to the binding.
//  5. Publish Programmed=True/Reason=Programmed on success,
//     Programmed=False/Reason=AuthorizationPolicyWriteFailed on any
//     write failure.
//  6. Aggregate Accepted + ResolvedRefs + Programmed into Ready.
//  7. Stamp metadata.generation on every condition plus
//     status.observedGeneration.
//  8. Write status ONLY when ObservedGeneration advances or a condition's
//     (Status, Reason, Message) tuple changes.
//
// The Accepted condition on the binding never carries sensitive byte
// material — the binding CR is metadata + references only, per the
// api/secrets/v1alpha1/doc.go "no sensitive values on CRs" invariant.
// The ext_authz Check-path enforcement is deferred to M3; this reconciler
// only programs the declarative mesh artifact.
type SecretInjectionPolicyBindingReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// SetupWithManager registers the reconciler with the supplied manager.
// Besides the primary For(&SecretInjectionPolicyBinding{}), the following
// watches are registered:
//
//   - Owns(&AuthorizationPolicy{}) — churn on the controller-owned
//     AuthorizationPolicy (for example an operator accidentally editing
//     it) enqueues the owner so the reconciler re-derives and overwrites
//     the drifted spec.
//   - Watches(&SecretInjectionPolicy{}) via a map function — a
//     late-arriving policy, or a policy whose spec changed, enqueues
//     every binding that references it so ResolvedRefs flips True
//     exactly when the policy appears.
func (r *SecretInjectionPolicyBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&secretsv1alpha1.SecretInjectionPolicyBinding{}).
		Owns(&istiosecurityv1.AuthorizationPolicy{}).
		Named("secretinjectionpolicybinding-controller").
		Watches(
			&secretsv1alpha1.SecretInjectionPolicy{},
			handler.EnqueueRequestsFromMapFunc(r.bindingsForPolicy),
		).
		Complete(r)
}

// Reconcile implements the reconciliation loop for the
// SecretInjectionPolicyBinding kind. See the type doc for the contract
// summary.
func (r *SecretInjectionPolicyBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var binding secretsv1alpha1.SecretInjectionPolicyBinding
	if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get SecretInjectionPolicyBinding: %w", err)
	}

	gen := binding.Generation
	accepted := bindingAcceptedCondition(&binding)

	// Spec failed the belt-and-braces Accepted check — skip resolution
	// and AP emission so an InvalidSpec binding does not produce a
	// bogus ResolvedRefs=True/Programmed=True cascade.
	if accepted.Status != metav1.ConditionTrue {
		resolved := metav1.Condition{
			Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionResolvedRefs,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonInvalidSpec,
			Message: "binding spec was not accepted; see Accepted condition",
		}
		programmed := metav1.Condition{
			Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonInvalidSpec,
			Message: "binding spec was not accepted; see Accepted condition",
		}
		return r.writeStatus(ctx, &binding, gen, []metav1.Condition{accepted, resolved, programmed},
			secretsv1alpha1.SecretInjectionPolicyBindingReasonNotReady,
			"binding spec was not accepted; see Accepted condition")
	}

	// Resolve the referenced SecretInjectionPolicy along the
	// admission-validated three-path rule. A miss surfaces as
	// ResolvedRefs=False/Reason=PolicyNotFound and blocks AP emission.
	policy, resolved, err := r.resolvePolicy(ctx, &binding)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolve policyRef: %w", err)
	}
	if resolved.Status != metav1.ConditionTrue {
		programmed := metav1.Condition{
			Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound,
			Message: "policy has not resolved; see ResolvedRefs condition",
		}
		return r.writeStatus(ctx, &binding, gen, []metav1.Condition{accepted, resolved, programmed},
			secretsv1alpha1.SecretInjectionPolicyBindingReasonNotReady,
			"policyRef has not resolved; see ResolvedRefs condition")
	}

	// Program the AuthorizationPolicy. Failures publish
	// Programmed=False/Reason=AuthorizationPolicyWriteFailed with the
	// underlying error bubbled onto the condition Message so operators
	// see the root cause in-place.
	programmed := r.programAuthorizationPolicy(ctx, &binding, policy)
	if programmed.Status != metav1.ConditionTrue {
		logger.Info("AuthorizationPolicy write failed",
			"binding", client.ObjectKeyFromObject(&binding).String(),
			"reason", programmed.Reason,
			"message", programmed.Message)
	}

	readyReason := secretsv1alpha1.SecretInjectionPolicyBindingReasonReady
	readyMessage := "binding is accepted, policyRef resolves, and the AuthorizationPolicy is programmed"
	if programmed.Status != metav1.ConditionTrue {
		readyReason = secretsv1alpha1.SecretInjectionPolicyBindingReasonNotReady
		readyMessage = "binding is not Ready; see Programmed condition"
	}
	return r.writeStatus(ctx, &binding, gen, []metav1.Condition{accepted, resolved, programmed},
		readyReason, readyMessage)
}

// bindingAcceptedCondition enforces the minimum spec invariants as a
// belt-and-braces against objects that bypassed admission (kubectl
// --server-side --force, direct etcd writes, dry-run errors that
// slipped a partially-populated object through). Returns Accepted=True
// on every well-formed object. Admission (HOL-703) is the primary
// enforcement point; the reconciler re-checks the shape here so a bad
// object at least surfaces its failure on .status.
func bindingAcceptedCondition(binding *secretsv1alpha1.SecretInjectionPolicyBinding) metav1.Condition {
	if binding.Spec.PolicyRef.Namespace == "" || binding.Spec.PolicyRef.Name == "" {
		return metav1.Condition{
			Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonInvalidSpec,
			Message: "spec.policyRef.namespace and spec.policyRef.name must be set",
		}
	}
	if len(binding.Spec.TargetRefs) == 0 {
		return metav1.Condition{
			Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonInvalidSpec,
			Message: "spec.targetRefs must carry at least one entry",
		}
	}
	serviceTargets := 0
	for i, t := range binding.Spec.TargetRefs {
		switch t.Kind {
		case secretsv1alpha1.TargetRefKindServiceAccount, secretsv1alpha1.TargetRefKindService:
		default:
			return metav1.Condition{
				Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonInvalidTargetKind,
				Message: fmt.Sprintf("spec.targetRefs[%d].kind %q is not one of ServiceAccount|Service", i, t.Kind),
			}
		}
		if t.Namespace == "" || t.Name == "" {
			return metav1.Condition{
				Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonInvalidSpec,
				Message: fmt.Sprintf("spec.targetRefs[%d] namespace and name must be set", i),
			}
		}
		// Service targets MUST reside in the binding's own namespace.
		// AuthorizationPolicy objects are namespace-scoped — their
		// spec.selector binds to Pods in the AP's own namespace. A
		// Service target that names a namespace other than
		// binding.Namespace cannot be enforced by the AP the reconciler
		// emits (the AP would silently select same-named Pods in the
		// binding's namespace), so the spec is rejected rather than
		// silently producing a policy that protects the wrong workload.
		// HOL-752 review round 1 flagged this as CRITICAL.
		if t.Kind == secretsv1alpha1.TargetRefKindService {
			serviceTargets++
			if t.Namespace != binding.Namespace {
				return metav1.Condition{
					Type:   secretsv1alpha1.SecretInjectionPolicyBindingConditionAccepted,
					Status: metav1.ConditionFalse,
					Reason: secretsv1alpha1.SecretInjectionPolicyBindingReasonInvalidSpec,
					Message: fmt.Sprintf("spec.targetRefs[%d]: Service target namespace %q must equal the binding's namespace %q (AuthorizationPolicy is namespace-scoped)",
						i, t.Namespace, binding.Namespace),
				}
			}
		}
	}
	// Multiple Service targets on a single binding would require emitting
	// multiple AuthorizationPolicy objects (one per Service selector) —
	// the current 1:1 binding-to-AP ownership model cannot narrow
	// spec.selector to more than one `kubernetes.io/service-name` value
	// via AND semantics, and a nil selector would widen the AP to every
	// Pod in the namespace. We reject the ambiguous case in v1alpha1 and
	// defer multi-Service handling to a later milestone. HOL-752 review
	// round 1 flagged this as CRITICAL.
	if serviceTargets > 1 {
		return metav1.Condition{
			Type:   secretsv1alpha1.SecretInjectionPolicyBindingConditionAccepted,
			Status: metav1.ConditionFalse,
			Reason: secretsv1alpha1.SecretInjectionPolicyBindingReasonInvalidSpec,
			Message: fmt.Sprintf("spec.targetRefs carries %d Service entries; v1alpha1 accepts at most one Service target per binding",
				serviceTargets),
		}
	}
	return metav1.Condition{
		Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonAccepted,
		Message: "spec passed reconciler validation",
	}
}

// resolvePolicy resolves spec.policyRef along the admission-validated
// three-path rule:
//
//  1. Same namespace as the binding.
//  2. The value of the binding namespace's console.holos.run/parent
//     label (immediate parent).
//  3. The synthesised `holos-org-<console.holos.run/organization>`
//     namespace (owning organization root).
//
// The reconciler attempts each path in order and returns the first
// policy it successfully fetches. A miss publishes
// ResolvedRefs=False/Reason=PolicyNotFound with a message that enumerates
// the candidate namespaces so operators can debug label drift without
// digging through admission logs.
//
// The returned condition is the ResolvedRefs condition the caller
// embeds in status. The policy pointer is non-nil iff the condition
// status is True.
func (r *SecretInjectionPolicyBindingReconciler) resolvePolicy(ctx context.Context, binding *secretsv1alpha1.SecretInjectionPolicyBinding) (*secretsv1alpha1.SecretInjectionPolicy, metav1.Condition, error) {
	candidates, err := r.policyCandidateNamespaces(ctx, binding)
	if err != nil {
		return nil, metav1.Condition{}, err
	}

	// spec.policyRef.namespace must equal one of the candidates for
	// admission to have accepted the object. If the reconciler sees a
	// policyRef.namespace outside the candidate set, the object
	// bypassed admission — refuse to resolve rather than silently
	// reading from a namespace admission would have blocked.
	refNs := binding.Spec.PolicyRef.Namespace
	allowed := false
	for _, c := range candidates {
		if c == refNs {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, metav1.Condition{
			Type:   secretsv1alpha1.SecretInjectionPolicyBindingConditionResolvedRefs,
			Status: metav1.ConditionFalse,
			Reason: secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound,
			Message: fmt.Sprintf("spec.policyRef.namespace %q is outside the admission-allowed ancestor chain %v; refusing to resolve",
				refNs, candidates),
		}, nil
	}

	var policy secretsv1alpha1.SecretInjectionPolicy
	key := types.NamespacedName{Namespace: refNs, Name: binding.Spec.PolicyRef.Name}
	switch err := r.Get(ctx, key, &policy); {
	case apierrors.IsNotFound(err):
		return nil, metav1.Condition{
			Type:   secretsv1alpha1.SecretInjectionPolicyBindingConditionResolvedRefs,
			Status: metav1.ConditionFalse,
			Reason: secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound,
			Message: fmt.Sprintf("SecretInjectionPolicy %s/%s not found (admission-allowed ancestor chain %v)",
				refNs, binding.Spec.PolicyRef.Name, candidates),
		}, nil
	case err != nil:
		return nil, metav1.Condition{}, fmt.Errorf("get SecretInjectionPolicy %s: %w", key, err)
	}

	return &policy, metav1.Condition{
		Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionResolvedRefs,
		Status:  metav1.ConditionTrue,
		Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonResolvedRefs,
		Message: fmt.Sprintf("resolved SecretInjectionPolicy %s/%s", refNs, binding.Spec.PolicyRef.Name),
	}, nil
}

// policyCandidateNamespaces returns the admission-allowed namespaces the
// reconciler may resolve spec.policyRef.namespace against, narrowed by
// spec.policyRef.scope.
//
// Scope narrowing (HOL-752 review round 1). The API type documents that
// spec.policyRef.scope "narrows the reconciler's resolution path" — so a
// binding declared with scope=organization must resolve only against the
// synthesised org namespace, and scope=folder must resolve only against
// the direct-parent-label namespace. Without this narrowing, a
// scope=folder binding could accept a same-named org policy and a
// scope=organization binding could accept a folder policy, defeating
// the defence-in-depth contract the scope field exists to enforce.
//
// Admission's three-path rule still applies: the binding's own
// namespace is always a valid same-scope candidate (policies co-located
// with a scope=organization binding in holos-org-acme live at the org
// root; policies co-located with a scope=folder binding in holos-fld-x
// live in that folder). The reconciler returns the subset of the three
// admission candidates that are consistent with scope.
//
// Trust-domain of the returned set. Admission is still the authoritative
// rejector of policyRef.namespace values outside the candidate set — the
// reconciler refuses to resolve anything outside this list, so a
// malicious writer that bypasses admission cannot trick the reconciler
// into a cross-scope or cross-tenant read.
//
// Hard-coded `holos-org-` prefix. The admission CEL expression in
// `config/secret-injector/admission/secretinjectionpolicybinding-policyref-same-namespace-or-ancestor.yaml`
// synthesises `holos-org-<organization>` with the same hard-coded
// prefix. Clusters that override resolver.Resolver.NamespacePrefix or
// OrganizationPrefix MUST patch both the admission CEL and the constant
// below. This coupling is documented in the admission YAML header and
// will be enforced by the envtest gate HOL-753 lands.
func (r *SecretInjectionPolicyBindingReconciler) policyCandidateNamespaces(ctx context.Context, binding *secretsv1alpha1.SecretInjectionPolicyBinding) ([]string, error) {
	// The binding's own namespace is always a same-scope candidate. A
	// policy co-located with the binding matches any scope (a
	// scope=organization binding in holos-org-acme, for instance,
	// treats the same org namespace as both "own" and "org"; the
	// resolvePolicy allowlist collapses to the same namespace).
	candidates := []string{binding.Namespace}

	var ns corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: binding.Namespace}, &ns); err != nil {
		if apierrors.IsNotFound(err) {
			return candidates, nil
		}
		return nil, fmt.Errorf("get Namespace %s: %w", binding.Namespace, err)
	}

	scope := binding.Spec.PolicyRef.Scope
	// scope=folder narrows to the parent-label candidate only. A binding
	// in a project namespace resolves to a folder policy in the direct
	// parent; deeper (grandparent) folders are not covered — pin at
	// the org root with scope=organization instead.
	if scope == secretsv1alpha1.PolicyRefScopeFolder {
		if parent := ns.Labels[bindingNamespaceParentLabel]; parent != "" {
			candidates = appendUnique(candidates, parent)
		}
		return candidates, nil
	}
	// scope=organization narrows to the synthesised organization
	// namespace only. Deeply nested bindings (project inside folder
	// inside org) always reach the org root via the organization label,
	// which the resolver writes on every descendant namespace.
	if scope == secretsv1alpha1.PolicyRefScopeOrganization {
		if orgShort := ns.Labels[bindingNamespaceOrganizationLabel]; orgShort != "" {
			candidates = appendUnique(candidates, bindingNamespaceOrganizationPrefix+orgShort)
		}
		return candidates, nil
	}
	// Unknown / empty scope: fall back to the admission-allowed union.
	// Admission already validates the enum, so this path is only
	// exercised by objects that bypassed admission — we are conservative
	// and still allow the full chain so a scope-field typo does not
	// produce a silent resolution failure.
	if parent := ns.Labels[bindingNamespaceParentLabel]; parent != "" {
		candidates = appendUnique(candidates, parent)
	}
	if orgShort := ns.Labels[bindingNamespaceOrganizationLabel]; orgShort != "" {
		candidates = appendUnique(candidates, bindingNamespaceOrganizationPrefix+orgShort)
	}
	return candidates, nil
}

// appendUnique appends candidate to list iff it is not already present.
// Keeps the policyCandidateNamespaces return slice free of duplicates —
// a binding in `holos-org-acme` that carries a parent label pointing at
// itself (the resolver writes the self-pointer for project roots)
// collapses to a single candidate, which keeps the resolvePolicy
// messages pithy.
func appendUnique(list []string, candidate string) []string {
	for _, existing := range list {
		if existing == candidate {
			return list
		}
	}
	return append(list, candidate)
}

// programAuthorizationPolicy builds the AuthorizationPolicy for the
// (binding, policy) pair, sets the controller-owned ownerReference, and
// Create-or-Update-in-place-s it. Returns the Programmed condition so
// the caller embeds it in status.
//
// Re-entrancy. The Update branch refuses to clobber an
// AuthorizationPolicy that is not owned by the incoming binding (an
// ownership mismatch is a cluster-configuration bug the reconciler must
// not paper over). A hot-loop where the AP churns on every Reconcile is
// prevented by comparing Spec/Labels before calling Update.
func (r *SecretInjectionPolicyBindingReconciler) programAuthorizationPolicy(ctx context.Context, binding *secretsv1alpha1.SecretInjectionPolicyBinding, policy *secretsv1alpha1.SecretInjectionPolicy) metav1.Condition {
	desired, err := buildAuthorizationPolicy(binding, policy)
	if err != nil {
		return metav1.Condition{
			Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonAuthorizationPolicyWriteFailed,
			Message: fmt.Sprintf("build AuthorizationPolicy: %v", err),
		}
	}
	if err := ctrl.SetControllerReference(binding, desired, r.Scheme); err != nil {
		return metav1.Condition{
			Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonAuthorizationPolicyWriteFailed,
			Message: fmt.Sprintf("set controller reference: %v", err),
		}
	}

	var existing istiosecurityv1.AuthorizationPolicy
	key := types.NamespacedName{Namespace: desired.Namespace, Name: desired.Name}
	switch err := r.Get(ctx, key, &existing); {
	case apierrors.IsNotFound(err):
		if err := r.Create(ctx, desired); err != nil {
			return metav1.Condition{
				Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed,
				Status:  metav1.ConditionFalse,
				Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonAuthorizationPolicyWriteFailed,
				Message: fmt.Sprintf("create AuthorizationPolicy %s: %v", key, err),
			}
		}
	case err != nil:
		return metav1.Condition{
			Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed,
			Status:  metav1.ConditionFalse,
			Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonAuthorizationPolicyWriteFailed,
			Message: fmt.Sprintf("get existing AuthorizationPolicy %s: %v", key, err),
		}
	default:
		if !isOwnedByBinding(&existing, binding) {
			return metav1.Condition{
				Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed,
				Status:  metav1.ConditionFalse,
				Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonAuthorizationPolicyWriteFailed,
				Message: fmt.Sprintf("AuthorizationPolicy %s exists but is not owned by this binding; refusing to clobber", key),
			}
		}
		// Only issue Update when the spec or labels actually drifted.
		// Deep-equal the proto Spec + the managed labels — status and
		// other server-managed fields are ignored so we do not spin in
		// an Update loop on server-side status writes.
		if authorizationPoliciesEquivalent(&existing, desired) {
			break
		}
		// Assign Spec fields individually rather than copying the struct
		// value. istio's Spec type embeds a protoimpl.MessageState (which
		// wraps a sync.Mutex); a direct struct-value assignment is
		// correctly flagged by `go vet` as a lock copy. Overwriting the
		// exported fields carries no such risk and keeps the in-cache
		// object's proto bookkeeping intact.
		existing.Spec.Action = desired.Spec.Action
		existing.Spec.ActionDetail = desired.Spec.ActionDetail
		existing.Spec.Selector = desired.Spec.Selector
		existing.Spec.TargetRef = desired.Spec.TargetRef
		existing.Spec.TargetRefs = desired.Spec.TargetRefs
		existing.Spec.Rules = desired.Spec.Rules
		existing.Labels = desired.Labels
		if err := ctrl.SetControllerReference(binding, &existing, r.Scheme); err != nil {
			return metav1.Condition{
				Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed,
				Status:  metav1.ConditionFalse,
				Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonAuthorizationPolicyWriteFailed,
				Message: fmt.Sprintf("set controller reference on update: %v", err),
			}
		}
		if err := r.Update(ctx, &existing); err != nil {
			return metav1.Condition{
				Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed,
				Status:  metav1.ConditionFalse,
				Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonAuthorizationPolicyWriteFailed,
				Message: fmt.Sprintf("update AuthorizationPolicy %s: %v", key, err),
			}
		}
	}

	return metav1.Condition{
		Type:    secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed,
		Status:  metav1.ConditionTrue,
		Reason:  secretsv1alpha1.SecretInjectionPolicyBindingReasonProgrammed,
		Message: fmt.Sprintf("programmed AuthorizationPolicy %s", key),
	}
}

// writeStatus proposes the component conditions + aggregated Ready,
// stamps metadata.generation on every condition plus
// status.observedGeneration, and writes the update only when the
// hot-loop guard detects an actual change. Mirrors the same helper in
// credential_controller.go and the inline logic in
// upstream_secret_controller.go so every reconciler in this package
// funnels status writes through the shared bookkeeping.
func (r *SecretInjectionPolicyBindingReconciler) writeStatus(ctx context.Context, binding *secretsv1alpha1.SecretInjectionPolicyBinding, gen int64, components []metav1.Condition, readyReason, readyMessage string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	ready := aggregateReady(components, readyReason, secretsv1alpha1.SecretInjectionPolicyBindingReasonNotReady,
		readyMessage, readyMessage)
	ready.Type = secretsv1alpha1.SecretInjectionPolicyBindingConditionReady
	ready.ObservedGeneration = gen

	target := binding.DeepCopy()
	target.Status.ObservedGeneration = gen
	newConds := append([]metav1.Condition(nil), binding.Status.Conditions...)
	proposed := make([]metav1.Condition, 0, len(components)+1)
	for _, c := range components {
		c.ObservedGeneration = gen
		proposed = append(proposed, c)
	}
	proposed = append(proposed, ready)
	for _, pc := range proposed {
		mergeCondition(&newConds, gen, pc)
	}
	target.Status.Conditions = newConds

	if binding.Status.ObservedGeneration == gen &&
		conditionsEqualIgnoringTransitionTime(binding.Status.Conditions, target.Status.Conditions) {
		logger.V(1).Info("SecretInjectionPolicyBinding status unchanged; skipping update", "generation", gen)
		return ctrl.Result{}, nil
	}

	if err := r.Status().Update(ctx, target); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update SecretInjectionPolicyBinding status: %w", err)
	}
	if r.Recorder != nil {
		if ready.Status == metav1.ConditionTrue {
			r.Recorder.Eventf(target, "Normal", secretsv1alpha1.SecretInjectionPolicyBindingReasonReady, "SecretInjectionPolicyBinding is Ready")
		} else {
			r.Recorder.Eventf(target, "Warning", ready.Reason, "%s", ready.Message)
		}
	}
	return ctrl.Result{}, nil
}

// bindingsForPolicy returns reconcile requests for every
// SecretInjectionPolicyBinding that references the supplied policy. The
// mapper powers the SecretInjectionPolicy watch registered in
// SetupWithManager so a late-arriving policy or a spec rename enqueues
// every binding that points at it — ResolvedRefs then flips True
// exactly at the moment the policy becomes resolvable.
//
// The lookup is a namespace-naive List filtered client-side. Bindings
// that reference a policy by (scope, namespace, name) live in the same
// scope tree so the steady-state count is small; a namespace-scoped
// List with a field index is an available optimization in HOL-753 when
// the envtest suite measures enqueue fan-out at scale.
func (r *SecretInjectionPolicyBindingReconciler) bindingsForPolicy(ctx context.Context, obj client.Object) []reconcile.Request {
	policy, ok := obj.(*secretsv1alpha1.SecretInjectionPolicy)
	if !ok {
		return nil
	}
	var list secretsv1alpha1.SecretInjectionPolicyBindingList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0, len(list.Items))
	for _, b := range list.Items {
		if b.Spec.PolicyRef.Namespace == policy.Namespace && b.Spec.PolicyRef.Name == policy.Name {
			out = append(out, reconcile.Request{
				NamespacedName: types.NamespacedName{Namespace: b.Namespace, Name: b.Name},
			})
		}
	}
	return out
}

// isOwnedByBinding reports whether ap carries an owner reference
// pointing to binding with controller=true. Matches on UID (not name)
// so a binding deleted and recreated with the same name but a fresh
// UID does not inherit the previous binding's AuthorizationPolicy — the
// new binding re-programs from first principles.
func isOwnedByBinding(ap *istiosecurityv1.AuthorizationPolicy, binding *secretsv1alpha1.SecretInjectionPolicyBinding) bool {
	for _, o := range ap.OwnerReferences {
		if o.UID == binding.UID && o.Controller != nil && *o.Controller {
			return true
		}
	}
	return false
}

// authorizationPoliciesEquivalent reports whether existing and desired
// carry the same Spec and managed labels — i.e. whether a second
// Update would be a no-op. Used by the hot-loop guard in
// programAuthorizationPolicy so the reconciler does not spin in a
// re-Update loop when the API server echoes the same object back.
//
// Spec equality compares the serialisable fields on
// istio.io/api/security/v1beta1.AuthorizationPolicy — Action,
// Selector, Rules, and the Provider ActionDetail. reflect.DeepEqual of
// the whole struct cannot be trusted because the proto types carry
// opaque state/sizeCache/unknownFields fields that are populated by the
// unmarshal layer and vary between an object built in-process and one
// round-tripped through the apiserver. A future envtest follow-up
// (HOL-753) will validate this helper against apiserver-echoed objects.
func authorizationPoliciesEquivalent(existing, desired *istiosecurityv1.AuthorizationPolicy) bool {
	if existing == nil || desired == nil {
		return false
	}
	if !reflect.DeepEqual(existing.Labels, desired.Labels) {
		return false
	}
	return authorizationPolicySpecEqual(&existing.Spec, &desired.Spec)
}

// authorizationPolicySpecEqual compares the subset of
// AuthorizationPolicy.spec fields this reconciler sets. The helper
// avoids reflect.DeepEqual of the whole proto struct because
// proto-internal bookkeeping (`state`, `sizeCache`, `unknownFields`)
// makes a naive DeepEqual unstable across serialisation boundaries —
// for example, a policy round-tripped through the apiserver can grow
// unknown fields or carry a different state value than the locally
// constructed one. Comparing by field name gives a stable, intent-
// level equality the Update hot-loop guard can rely on.
func authorizationPolicySpecEqual(a, b *istiosecurityv1beta1.AuthorizationPolicy) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Action != b.Action {
		return false
	}
	if !selectorEqual(a.Selector, b.Selector) {
		return false
	}
	if !providerEqual(a.GetProvider(), b.GetProvider()) {
		return false
	}
	if !rulesEqual(a.Rules, b.Rules) {
		return false
	}
	return true
}

// selectorEqual compares two WorkloadSelector values by MatchLabels.
// The proto WorkloadSelector has no MatchExpressions on the wire, so
// the MatchLabels comparison is the whole content equality check.
func selectorEqual(a, b *istiotypev1beta1.WorkloadSelector) bool {
	if a == nil || b == nil {
		return a == b
	}
	return reflect.DeepEqual(a.MatchLabels, b.MatchLabels)
}

// providerEqual compares two ExtensionProvider values by Name. The
// ExtensionProvider proto has only a single field, so name equality is
// the whole content equality check.
func providerEqual(a, b *istiosecurityv1beta1.AuthorizationPolicy_ExtensionProvider) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Name == b.Name
}

// rulesEqual compares two []*Rule slices by deep-equaling the embedded
// Source and Operation payloads. Order matters — the builder emits a
// fixed Rule order, so apiserver-returned objects that permuted the
// rules will trigger an Update.
func rulesEqual(a, b []*istiosecurityv1beta1.Rule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !ruleEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// ruleEqual compares two *Rule values. Mirrors the nil-tolerant shape
// of the sibling helpers so callers never panic on a nil pointer in a
// round-tripped slice.
func ruleEqual(a, b *istiosecurityv1beta1.Rule) bool {
	if a == nil || b == nil {
		return a == b
	}
	if !fromsEqual(a.From, b.From) {
		return false
	}
	if !tosEqual(a.To, b.To) {
		return false
	}
	// When is not populated by this reconciler — treat any drift as
	// an Update signal.
	return len(a.When) == len(b.When)
}

func fromsEqual(a, b []*istiosecurityv1beta1.Rule_From) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] == nil || b[i] == nil {
			if a[i] != b[i] {
				return false
			}
			continue
		}
		if !sourceEqual(a[i].Source, b[i].Source) {
			return false
		}
	}
	return true
}

func tosEqual(a, b []*istiosecurityv1beta1.Rule_To) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] == nil || b[i] == nil {
			if a[i] != b[i] {
				return false
			}
			continue
		}
		// This reconciler does not populate Operation (M3 will for
		// Match predicates); treat any apiserver-echoed drift as
		// distinct so the hot-loop guard catches it.
		if !reflect.DeepEqual(a[i].Operation, b[i].Operation) {
			return false
		}
	}
	return true
}

// sourceEqual compares two *Source values by the subset of fields the
// reconciler populates: Principals. Every other Source field remains
// nil on the builder side, so any apiserver-echoed value that would
// not survive a round-trip is treated as distinct.
func sourceEqual(a, b *istiosecurityv1beta1.Source) bool {
	if a == nil || b == nil {
		return a == b
	}
	return reflect.DeepEqual(a.Principals, b.Principals)
}
