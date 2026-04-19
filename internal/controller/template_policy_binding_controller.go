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

	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

// bindingAncestorMaxDepth matches console/resolver/walker.go's maxWalkDepth.
// The reconciler mirrors the walker's invariant (organization is the root; at
// most maxWalkDepth hops before we give up) so reconciler-level reachability
// stays consistent with the RPC handler check.
const bindingAncestorMaxDepth = 5

// TemplatePolicyBindingReconciler reconciles a TemplatePolicyBinding. The
// binding contract mirrors the Gateway API HTTPRoute status surface: an
// Accepted condition that reflects spec sanity, a ResolvedRefs condition
// that reflects whether every target and policy reference names a real
// object, and a top-level Ready aggregation.
//
// Every Template change enqueues the bindings whose .spec.targetRefs match.
// HOL-620 resolves ProjectTemplate target references by looking up a
// Template in the namespace whose ProjectName matches the target's
// ProjectName. Deployment targets are NOT resolved against Template objects
// (a Deployment target refers to an apps/v1.Deployment, not a Template) —
// they only contribute to Accepted via kind validation. The binding also
// resolves spec.policyRef against a TemplatePolicy in the scope namespace
// implied by PolicyRef.Scope ("organization" or "folder") and reports
// PolicyNotFound on the ResolvedRefs condition when the referenced policy
// does not exist.
//
// RBAC markers for this reconciler live on the package doc comment in
// rbac.go — controller-gen's rbac generator ignores markers on struct or
// method doc comments.
type TemplatePolicyBindingReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// ProjectNamespacePrefix, OrganizationPrefix, FolderPrefix, and
	// ProjectPrefix mirror the resolver.Resolver used elsewhere in the
	// console. They default to empty + "org-"/"fld-"/"prj-" when the
	// caller leaves them blank. HOL-620 only needs ProjectNamespace
	// prefixing for ProjectTemplate target-ref resolution; the rest are
	// here so HOL-621 storage wiring can reuse the same struct.
	NamespacePrefix    string
	OrganizationPrefix string
	FolderPrefix       string
	ProjectPrefix      string
}

// SetupWithManager registers the reconciler with the supplied manager. In
// addition to the primary For(&TemplatePolicyBinding{}), it adds three
// secondary Watches:
//
//   - Template: ResolvedRefs depends on whether each ProjectTemplate
//     target_ref resolves to an existing Template, so Template create/delete
//     events must re-enqueue the binding.
//   - TemplatePolicy: ResolvedRefs also depends on whether spec.policyRef
//     resolves. TemplatePolicy appearing / being renamed / being deleted
//     would otherwise leave the binding's Ready status stale until someone
//     edits the binding spec.
//   - Namespace: the ancestor-chain reachability check in
//     policyNamespaceInAncestorChain reads `console.holos.run/parent` and
//     `console.holos.run/resource-type` labels from Namespaces. An admin
//     repairing a missing or wrong parent label must re-reconcile every
//     binding whose policy lookup could be affected, and there is no
//     cheap way to know which binding is affected without a list, so we
//     re-enqueue every binding on any namespace change.
func (r *TemplatePolicyBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.applyPrefixDefaults()
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.TemplatePolicyBinding{}).
		Named("template-policy-binding-controller").
		Watches(
			&v1alpha1.Template{},
			handler.EnqueueRequestsFromMapFunc(r.bindingsForTemplate),
		).
		Watches(
			&v1alpha1.TemplatePolicy{},
			handler.EnqueueRequestsFromMapFunc(r.bindingsForTemplatePolicy),
		).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.bindingsForNamespace),
		).
		Complete(r)
}

func (r *TemplatePolicyBindingReconciler) applyPrefixDefaults() {
	if r.OrganizationPrefix == "" {
		r.OrganizationPrefix = "org-"
	}
	if r.FolderPrefix == "" {
		r.FolderPrefix = "fld-"
	}
	if r.ProjectPrefix == "" {
		r.ProjectPrefix = "prj-"
	}
}

// Reconcile implements the reconciliation loop for TemplatePolicyBinding.
// See TemplateReconciler.Reconcile for the overall contract; the binding
// kind differs in that ResolvedRefs is a first-class component condition.
func (r *TemplatePolicyBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	r.applyPrefixDefaults()

	var binding v1alpha1.TemplatePolicyBinding
	if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get TemplatePolicyBinding: %w", err)
	}

	gen := binding.Generation

	accepted := bindingAcceptedCondition(&binding)
	resolved, err := r.bindingResolvedRefsCondition(ctx, &binding)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolving target refs: %w", err)
	}
	components := []metav1.Condition{accepted, resolved}

	proposed := make([]metav1.Condition, 0, 3)
	for _, c := range components {
		c.ObservedGeneration = gen
		proposed = append(proposed, c)
	}
	ready := aggregateReady(components,
		v1alpha1.TemplatePolicyBindingReasonReady,
		v1alpha1.TemplatePolicyBindingReasonNotReady,
		"TemplatePolicyBinding is accepted and every referenced Template exists.",
		"TemplatePolicyBinding is not Ready; see component conditions for details.")
	ready.Type = v1alpha1.TemplatePolicyBindingConditionReady
	ready.ObservedGeneration = gen
	proposed = append(proposed, ready)

	target := binding.DeepCopy()
	target.Status.ObservedGeneration = gen
	newConds := append([]metav1.Condition(nil), binding.Status.Conditions...)
	for _, pc := range proposed {
		mergeCondition(&newConds, gen, pc)
	}
	target.Status.Conditions = newConds

	if binding.Status.ObservedGeneration == gen &&
		conditionsEqualIgnoringTransitionTime(binding.Status.Conditions, target.Status.Conditions) {
		logger.V(1).Info("TemplatePolicyBinding status unchanged; skipping update", "generation", gen)
		return ctrl.Result{}, nil
	}

	if err := r.Status().Update(ctx, target); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update TemplatePolicyBinding status: %w", err)
	}
	if ready.Status == metav1.ConditionTrue {
		r.Recorder.Eventf(target, "Normal", v1alpha1.TemplatePolicyBindingReasonReady, "TemplatePolicyBinding is Ready")
	} else {
		r.Recorder.Eventf(target, "Warning", ready.Reason, "%s", ready.Message)
	}
	return ctrl.Result{}, nil
}

// bindingAcceptedCondition enforces the invariants the reconciler owns: at
// least one target_ref, every target_ref kind is one of the allowed enum
// values, and no duplicate (kind, projectName, name) tuples. The CRD schema
// enforces MinItems=1 + the kind enum; we re-check here so the condition
// surface is populated on objects that bypassed the admission path.
func bindingAcceptedCondition(binding *v1alpha1.TemplatePolicyBinding) metav1.Condition {
	if len(binding.Spec.TargetRefs) == 0 {
		return metav1.Condition{
			Type:    v1alpha1.TemplatePolicyBindingConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplatePolicyBindingReasonInvalidSpec,
			Message: "spec.targetRefs must contain at least one target",
		}
	}
	seen := make(map[string]struct{}, len(binding.Spec.TargetRefs))
	for i, ref := range binding.Spec.TargetRefs {
		switch ref.Kind {
		case v1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
			v1alpha1.TemplatePolicyBindingTargetKindDeployment:
			// OK
		default:
			return metav1.Condition{
				Type:    v1alpha1.TemplatePolicyBindingConditionAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.TemplatePolicyBindingReasonInvalidSpec,
				Message: fmt.Sprintf("spec.targetRefs[%d].kind %q is not a valid TemplatePolicyBindingTargetKind", i, ref.Kind),
			}
		}
		if ref.Name == "" || ref.ProjectName == "" {
			return metav1.Condition{
				Type:    v1alpha1.TemplatePolicyBindingConditionAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.TemplatePolicyBindingReasonInvalidSpec,
				Message: fmt.Sprintf("spec.targetRefs[%d] must set both name and projectName", i),
			}
		}
		key := fmt.Sprintf("%s|%s|%s", ref.Kind, ref.ProjectName, ref.Name)
		if _, dup := seen[key]; dup {
			return metav1.Condition{
				Type:    v1alpha1.TemplatePolicyBindingConditionAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.TemplatePolicyBindingReasonInvalidSpec,
				Message: fmt.Sprintf("spec.targetRefs[%d] duplicates an earlier target with kind=%s, projectName=%s, name=%s", i, ref.Kind, ref.ProjectName, ref.Name),
			}
		}
		seen[key] = struct{}{}
	}
	if binding.Spec.PolicyRef.Name == "" || binding.Spec.PolicyRef.ScopeName == "" {
		return metav1.Condition{
			Type:    v1alpha1.TemplatePolicyBindingConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplatePolicyBindingReasonInvalidSpec,
			Message: "spec.policyRef must set both name and scopeName",
		}
	}
	// Scope must name a hierarchy level owning a TemplatePolicy. The CRD
	// enum pins this to {organization, folder}, but objects created through
	// paths that bypass CRD validation (tests, direct apiserver writes,
	// legacy imports) can still surface invalid scopes. Fail Accepted here
	// so the object does not fall through to ResolvedRefs=True on a
	// malformed spec.
	switch strings.ToLower(binding.Spec.PolicyRef.Scope) {
	case "organization", "folder":
		// OK
	default:
		return metav1.Condition{
			Type:    v1alpha1.TemplatePolicyBindingConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplatePolicyBindingReasonInvalidSpec,
			Message: fmt.Sprintf("spec.policyRef.scope %q is not a valid scope (expected organization or folder)", binding.Spec.PolicyRef.Scope),
		}
	}
	return metav1.Condition{
		Type:    v1alpha1.TemplatePolicyBindingConditionAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.TemplatePolicyBindingReasonAccepted,
		Message: "spec passed reconciler validation",
	}
}

// bindingResolvedRefsCondition checks whether every ProjectTemplate target
// ref resolves to an existing Template. The check reads through the
// client.Client handed to the reconciler by the manager, so it consults
// the informer cache HOL-620 just populated. Deployment target refs do
// not resolve against Template objects — they only check kind validity,
// which Accepted already covers.
func (r *TemplatePolicyBindingReconciler) bindingResolvedRefsCondition(ctx context.Context, binding *v1alpha1.TemplatePolicyBinding) (metav1.Condition, error) {
	// If the spec is already rejected by bindingAcceptedCondition, short-
	// circuit ResolvedRefs with InvalidTargetKind so the condition pair
	// makes sense end-to-end. We still return True-for-none rather than
	// an error: ResolvedRefs reports what we *can* observe.
	for _, ref := range binding.Spec.TargetRefs {
		if ref.Kind != v1alpha1.TemplatePolicyBindingTargetKindProjectTemplate &&
			ref.Kind != v1alpha1.TemplatePolicyBindingTargetKindDeployment {
			return metav1.Condition{
				Type:    v1alpha1.TemplatePolicyBindingConditionResolvedRefs,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.TemplatePolicyBindingReasonInvalidTargetKind,
				Message: fmt.Sprintf("targetRefs[*].kind %q is not a valid TemplatePolicyBindingTargetKind", ref.Kind),
			}, nil
		}
	}

	for _, ref := range binding.Spec.TargetRefs {
		if ref.Kind != v1alpha1.TemplatePolicyBindingTargetKindProjectTemplate {
			continue
		}
		ns := r.projectNamespace(ref.ProjectName)
		var tmpl v1alpha1.Template
		err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: ref.Name}, &tmpl)
		if err == nil {
			continue
		}
		if apierrors.IsNotFound(err) {
			return metav1.Condition{
				Type:    v1alpha1.TemplatePolicyBindingConditionResolvedRefs,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.TemplatePolicyBindingReasonTemplateNotFound,
				Message: fmt.Sprintf("ProjectTemplate %s/%s not found", ref.ProjectName, ref.Name),
			}, nil
		}
		return metav1.Condition{}, fmt.Errorf("get Template %s/%s: %w", ns, ref.Name, err)
	}

	// spec.policyRef: the referenced TemplatePolicy must exist in the
	// namespace implied by PolicyRef.Scope + PolicyRef.ScopeName AND live
	// in a namespace reachable from the binding's own namespace via the
	// console.holos.run/parent ancestor chain. bindingAcceptedCondition
	// has already screened out bad scope discriminators; policyRefNamespace
	// returning ok=false here would mean we raced with an Accepted=False
	// surface, so we only walk the chain when we can compute a target
	// namespace.
	policyNs, ok := r.policyRefNamespace(binding.Spec.PolicyRef)
	if ok {
		var policy v1alpha1.TemplatePolicy
		err := r.Get(ctx, types.NamespacedName{Namespace: policyNs, Name: binding.Spec.PolicyRef.Name}, &policy)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return metav1.Condition{
					Type:   v1alpha1.TemplatePolicyBindingConditionResolvedRefs,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.TemplatePolicyBindingReasonPolicyNotFound,
					Message: fmt.Sprintf("TemplatePolicy %s/%s (scope=%s) not found",
						policyNs, binding.Spec.PolicyRef.Name, binding.Spec.PolicyRef.Scope),
				}, nil
			}
			return metav1.Condition{}, fmt.Errorf("get TemplatePolicy %s/%s: %w", policyNs, binding.Spec.PolicyRef.Name, err)
		}

		// Reachability: walk the binding's ancestor chain and require
		// that policyNs appears in it. A folder binding can name its
		// own folder or any ancestor organization; an organization
		// binding can only name its own organization. This mirrors the
		// RPC handler's AncestorChainResolver gate so the reconciler
		// does not report Ready=True on objects the write path rejects.
		reachable, err := r.policyNamespaceInAncestorChain(ctx, binding.Namespace, policyNs)
		if err != nil {
			return metav1.Condition{}, fmt.Errorf("walking ancestor chain from %q: %w", binding.Namespace, err)
		}
		if !reachable {
			return metav1.Condition{
				Type:   v1alpha1.TemplatePolicyBindingConditionResolvedRefs,
				Status: metav1.ConditionFalse,
				Reason: v1alpha1.TemplatePolicyBindingReasonPolicyNotFound,
				Message: fmt.Sprintf("TemplatePolicy %s/%s (scope=%s) is not reachable from binding namespace %q via the ancestor chain",
					policyNs, binding.Spec.PolicyRef.Name, binding.Spec.PolicyRef.Scope, binding.Namespace),
			}, nil
		}
	}

	return metav1.Condition{
		Type:    v1alpha1.TemplatePolicyBindingConditionResolvedRefs,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.TemplatePolicyBindingReasonResolvedRefs,
		Message: "every target_ref and policyRef resolves to an existing object",
	}, nil
}

// policyRefNamespace maps a LinkedTemplatePolicyRef to the namespace the
// referenced TemplatePolicy must live in. Returns ok=false for scope values
// that do not map to a namespace — callers leave ResolvedRefs to fall through
// to the policy-independent "refs ok" branch because scope validity is an
// Accepted-level concern, not a ResolvedRefs-level concern.
func (r *TemplatePolicyBindingReconciler) policyRefNamespace(ref v1alpha1.LinkedTemplatePolicyRef) (string, bool) {
	switch strings.ToLower(ref.Scope) {
	case "organization":
		return r.NamespacePrefix + r.OrganizationPrefix + ref.ScopeName, true
	case "folder":
		return r.NamespacePrefix + r.FolderPrefix + ref.ScopeName, true
	default:
		return "", false
	}
}

// policyNamespaceInAncestorChain walks the console.holos.run/parent label
// chain starting at bindingNamespace and reports whether policyNamespace
// appears anywhere in the chain (including bindingNamespace itself). The walk
// terminates when a namespace with resource-type=organization is seen, an
// iteration cap is hit (mirrors resolver.Walker's maxWalkDepth = 5), or a
// cycle is detected. Namespace reads go through the cache-backed client the
// manager already primes in NewManager.
//
// Missing namespaces in the middle of the chain are treated as "not
// reachable" rather than an error: the chain can only resolve what the cache
// currently holds, and the binding reconciler is re-enqueued naturally when
// any Namespace changes via the Namespace informer controller-runtime starts
// for us. Bubbling a transient apierror would flip ResolvedRefs=False with a
// misleading reason.
func (r *TemplatePolicyBindingReconciler) policyNamespaceInAncestorChain(ctx context.Context, bindingNamespace, policyNamespace string) (bool, error) {
	if bindingNamespace == policyNamespace {
		return true, nil
	}
	visited := make(map[string]bool, bindingAncestorMaxDepth+1)
	current := bindingNamespace
	for i := 0; i <= bindingAncestorMaxDepth; i++ {
		if visited[current] {
			// Cycle in the label graph. Surface "not reachable" — the
			// upstream data is broken, and the reconciler should not
			// claim Ready=True on a cluster in that state.
			return false, nil
		}
		visited[current] = true

		var ns corev1.Namespace
		if err := r.Get(ctx, types.NamespacedName{Name: current}, &ns); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("get Namespace %q: %w", current, err)
		}
		if current == policyNamespace {
			return true, nil
		}
		if ns.Labels[v1alpha2.LabelResourceType] == v1alpha2.ResourceTypeOrganization {
			// Top of the tree and we still have not seen policyNs.
			return false, nil
		}
		parent := ns.Labels[v1alpha2.AnnotationParent]
		if parent == "" {
			// Non-organization namespace without a parent label is
			// malformed for the console's hierarchy. Treat as not
			// reachable; an admin fix will re-trigger reconcile on
			// the label write.
			return false, nil
		}
		current = parent
	}
	// Hit the depth cap without finding policyNs or an organization root.
	return false, nil
}

// projectNamespace maps a project name to the Kubernetes namespace the
// project owns. Mirrors console/resolver.Resolver.ProjectNamespace — kept
// as a thin local helper so the controller does not depend on the heavier
// resolver package during HOL-620. HOL-621 replaces this with a shared
// resolver once the cache becomes the authoritative read path.
func (r *TemplatePolicyBindingReconciler) projectNamespace(projectName string) string {
	return r.NamespacePrefix + r.ProjectPrefix + projectName
}

// bindingsForTemplate returns the reconcile requests for every binding
// whose spec.targetRefs includes a ProjectTemplate ref to the supplied
// Template object. Called by the Watches handler set up in SetupWithManager
// so that a Template appearing/disappearing re-enqueues the affected
// bindings and their ResolvedRefs condition flips in the same reconcile
// cycle as the watch event.
func (r *TemplatePolicyBindingReconciler) bindingsForTemplate(ctx context.Context, obj client.Object) []reconcile.Request {
	tmpl, ok := obj.(*v1alpha1.Template)
	if !ok {
		return nil
	}
	projectName := r.projectNameForNamespace(tmpl.Namespace)
	if projectName == "" {
		// The Template does not live in a project-labeled namespace,
		// so no ProjectTemplate target can reference it. We still
		// check via a label lookup below for the future case where
		// a binding names a non-project-namespace target.
	}

	var list v1alpha1.TemplatePolicyBindingList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var out []reconcile.Request
	for _, b := range list.Items {
		for _, ref := range b.Spec.TargetRefs {
			if ref.Kind != v1alpha1.TemplatePolicyBindingTargetKindProjectTemplate {
				continue
			}
			if ref.Name != tmpl.Name {
				continue
			}
			if projectName != "" && ref.ProjectName != projectName {
				continue
			}
			out = append(out, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: b.Namespace,
					Name:      b.Name,
				},
			})
			break
		}
	}
	return out
}

// bindingsForTemplatePolicy returns the reconcile requests for every binding
// whose spec.policyRef matches the supplied TemplatePolicy. Scope + scopeName
// are compared against the policy's own namespace (org-/fld- prefix stripped)
// so that a policy create/delete event re-enqueues the bindings whose
// ResolvedRefs condition depends on it.
func (r *TemplatePolicyBindingReconciler) bindingsForTemplatePolicy(ctx context.Context, obj client.Object) []reconcile.Request {
	policy, ok := obj.(*v1alpha1.TemplatePolicy)
	if !ok {
		return nil
	}
	var list v1alpha1.TemplatePolicyBindingList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var out []reconcile.Request
	for _, b := range list.Items {
		ns, ok := r.policyRefNamespace(b.Spec.PolicyRef)
		if !ok {
			continue
		}
		if ns != policy.Namespace || b.Spec.PolicyRef.Name != policy.Name {
			continue
		}
		out = append(out, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: b.Namespace,
				Name:      b.Name,
			},
		})
	}
	return out
}

// bindingsForNamespace re-enqueues every TemplatePolicyBinding on any
// Namespace create/update/delete. ResolvedRefs depends on the ancestor-chain
// walk, which reads Namespace labels (`console.holos.run/parent` and
// `console.holos.run/resource-type`). An admin repairing a broken parent
// label must re-reconcile every binding whose walk touches that namespace.
// Because we cannot cheaply determine which bindings are affected without a
// list (the label graph is the source of truth), we take the list + enqueue
// path; controller-runtime's predicate filtering and the reconciler's own
// no-change guard keep the cost bounded.
func (r *TemplatePolicyBindingReconciler) bindingsForNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	var list v1alpha1.TemplatePolicyBindingList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0, len(list.Items))
	for _, b := range list.Items {
		out = append(out, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: b.Namespace,
				Name:      b.Name,
			},
		})
	}
	return out
}

// projectNameForNamespace inverts the projectNamespace function: given a
// namespace name, it returns the project name if and only if the namespace
// matches <NamespacePrefix><ProjectPrefix><projectName>. Returns empty
// string otherwise. Intentionally prefix-only: HOL-620 does not consult
// the Namespace cache for a console.holos.run/resource-type=project label
// check — HOL-621 layers that in once the cache is the authoritative read
// path.
func (r *TemplatePolicyBindingReconciler) projectNameForNamespace(namespace string) string {
	prefix := r.NamespacePrefix + r.ProjectPrefix
	if !strings.HasPrefix(namespace, prefix) {
		return ""
	}
	return strings.TrimPrefix(namespace, prefix)
}

// Explicit reference to corev1 keeps the namespace informer primed by
// manager.go importable — otherwise goimports would strip the import when
// the file gets fmt-ed.
var _ = corev1.SchemeGroupVersion
