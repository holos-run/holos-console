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

// Package controller — TemplateRequirementReconciler.
//
// # Design
//
// TemplateRequirementReconciler watches TemplateRequirement objects stored in
// folder and organization namespaces (enforced at admission by the
// ValidatingAdmissionPolicy from HOL-956). For each reconcile it:
//
//  1. Validates the spec (Accepted condition).
//  2. Lists every Deployment in the cluster whose TemplateRef project matches
//     the TemplateRequirement's targetRefs[] (using the same bindingAppliesTo /
//     nameMatches helpers as the folder resolver for consistency).
//  3. For each matching Deployment, calls EnsureSingletonDependencyDeployment
//     from console/deployments — the shared singleton helper from Phase 5
//     (HOL-959) — so the singleton Requires Deployment is materialised with a
//     non-controller ownerReference.
//  4. Validates that cross-namespace Requires references are authorised by a
//     TemplateGrant (ResolvedRefs condition).
//  5. Aggregates Accepted + ResolvedRefs into the top-level Ready condition and
//     writes status if it has changed.
//
// # Render-order contract (open question 2, HOL-960)
//
// TemplatePolicy.Require runs at render time (unchanged). TemplateRequirement
// materialises sibling Deployments AFTER the dependent's render succeeds.
// The ordering is enforced by watching the Deployment object: the reconciler
// only calls EnsureSingletonDependencyDeployment for Deployments that already
// exist (i.e., whose render has produced a Deployment CR). This avoids races
// where the sibling singleton references rendered output that does not yet
// exist.
//
// # Overlap policy (open question 1, HOL-960)
//
// When two TemplateRequirement objects in the same ancestor chain match the
// same Deployment, each requirement is processed independently. The singleton
// naming is deterministic on (requires.Name, requires.VersionConstraint), so
// two requirements with different Requires fields produce distinct singletons
// — there is no conflict. A "union" of the Requires set therefore emerges
// naturally: each requirement contributes its own singleton Deployment.
// Overlapping requirements pointing at the same (namespace, name, version)
// are idempotent: EnsureSingletonDependencyDeployment fetches the existing
// singleton and adds the ownerReference if it is missing, so the second call
// is a no-op.
//
// Incompatible versionConstraints on the same (namespace, name) template
// produce distinct singleton names (the version suffix differs), so they
// co-exist as separate Deployments. Phase 8 (PreflightCheck, HOL-962)
// surfaces such naming collisions before apply.
//
// # Stylistic reference
//
// This file follows internal/controller/template_dependency_controller.go.
package controller

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	"github.com/holos-run/holos-console/console/deployments"
	"github.com/holos-run/holos-console/console/policyresolver"
)

// TemplateRequirementReconciler reconciles TemplateRequirement objects stored
// in folder and organization namespaces. It calls
// EnsureSingletonDependencyDeployment for each matching Deployment and
// surfaces grant-validation results as Kubernetes conditions.
//
// RBAC markers for this reconciler live on the package doc comment in
// rbac.go — controller-gen's rbac generator ignores markers on struct or
// method doc comments.
type TemplateRequirementReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Validator deployments.Validator
}

// SetupWithManager registers the reconciler with the supplied manager. In
// addition to the primary For(&TemplateRequirement{}), it adds a secondary
// watch on Deployment objects: when a Deployment is created or deleted the
// set of ownerReferences on the managed singletons changes, so the
// TemplateRequirement objects that match must be re-reconciled.
func (r *TemplateRequirementReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.TemplateRequirement{}).
		Named("template-requirement-controller").
		Watches(
			&deploymentsv1alpha1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(r.requirementsForDeployment),
		).
		Complete(r)
}

// Reconcile implements the reconciliation loop for TemplateRequirement.
func (r *TemplateRequirementReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var treq v1alpha1.TemplateRequirement
	if err := r.Get(ctx, req.NamespacedName, &treq); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get TemplateRequirement: %w", err)
	}

	gen := treq.Generation

	accepted := requirementAcceptedCondition(&treq)
	resolved, helperErr := r.requirementResolvedRefsCondition(ctx, &treq)
	components := []metav1.Condition{accepted, resolved}

	proposed := make([]metav1.Condition, 0, 3)
	for _, c := range components {
		c.ObservedGeneration = gen
		proposed = append(proposed, c)
	}
	ready := aggregateReady(components,
		v1alpha1.TemplateRequirementReasonReady,
		v1alpha1.TemplateRequirementReasonNotReady,
		"TemplateRequirement is accepted and all singleton Deployments are materialised.",
		"TemplateRequirement is not Ready; see component conditions for details.")
	ready.Type = v1alpha1.TemplateRequirementConditionReady
	ready.ObservedGeneration = gen
	proposed = append(proposed, ready)

	target := treq.DeepCopy()
	target.Status.ObservedGeneration = gen
	newConds := append([]metav1.Condition(nil), treq.Status.Conditions...)
	for _, pc := range proposed {
		mergeCondition(&newConds, gen, pc)
	}
	target.Status.Conditions = newConds

	if treq.Status.ObservedGeneration == gen &&
		conditionsEqualIgnoringTransitionTime(treq.Status.Conditions, target.Status.Conditions) {
		logger.V(1).Info("TemplateRequirement status unchanged; skipping update", "generation", gen)
		if helperErr != nil {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, nil
	}

	if err := r.Status().Update(ctx, target); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update TemplateRequirement status: %w", err)
	}
	if ready.Status == metav1.ConditionTrue {
		r.Recorder.Eventf(target, "Normal", v1alpha1.TemplateRequirementReasonReady, "TemplateRequirement is Ready")
	} else {
		r.Recorder.Eventf(target, "Warning", ready.Reason, "%s", ready.Message)
	}

	if helperErr != nil {
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// requirementAcceptedCondition validates the TemplateRequirement spec fields
// the reconciler can check without API server calls: non-empty namespace/name
// on the Requires reference and at least one TargetRef entry.
func requirementAcceptedCondition(treq *v1alpha1.TemplateRequirement) metav1.Condition {
	if treq.Spec.Requires.Namespace == "" || treq.Spec.Requires.Name == "" {
		return metav1.Condition{
			Type:    v1alpha1.TemplateRequirementConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplateRequirementReasonInvalidSpec,
			Message: "spec.requires must set both namespace and name",
		}
	}
	if len(treq.Spec.TargetRefs) == 0 {
		return metav1.Condition{
			Type:    v1alpha1.TemplateRequirementConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplateRequirementReasonInvalidSpec,
			Message: "spec.targetRefs must contain at least one entry",
		}
	}
	return metav1.Condition{
		Type:    v1alpha1.TemplateRequirementConditionAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.TemplateRequirementReasonAccepted,
		Message: "spec passed reconciler validation",
	}
}

// requirementResolvedRefsCondition lists all Deployments across all
// namespaces, finds those whose TemplateRef project matches the
// TemplateRequirement's targetRefs[], calls
// EnsureSingletonDependencyDeployment for each, and returns the ResolvedRefs
// condition. The second return value carries the first transient error
// encountered; callers requeue when it is non-nil regardless of whether the
// condition changed.
//
// Grant validation is delegated entirely to EnsureSingletonDependencyDeployment:
// that function checks whether the *Deployment's namespace* is permitted to use
// the requires template, which is the meaningful authorization boundary for
// cross-namespace template materialization. This differs from
// TemplateDependencyReconciler, which validates the grant before listing
// Deployments because the TemplateDependency always lives in the same namespace
// as its Deployments. TemplateRequirement lives in a folder/org namespace and
// the impacted project namespaces are discovered dynamically, so per-Deployment
// validation is the only correct level to perform the check.
func (r *TemplateRequirementReconciler) requirementResolvedRefsCondition(ctx context.Context, treq *v1alpha1.TemplateRequirement) (metav1.Condition, error) {
	// Short-circuit: if Accepted=False the spec is broken.
	if treq.Spec.Requires.Namespace == "" || treq.Spec.Requires.Name == "" ||
		len(treq.Spec.TargetRefs) == 0 {
		return metav1.Condition{
			Type:    v1alpha1.TemplateRequirementConditionResolvedRefs,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplateRequirementReasonInvalidSpec,
			Message: "spec is invalid; see Accepted condition",
		}, nil
	}

	cascadeDelete := true
	if treq.Spec.CascadeDelete != nil {
		cascadeDelete = *treq.Spec.CascadeDelete
	}

	// Build the adapter that bridges TemplateRequirementTargetRef and the
	// ResolvedBinding / bindingAppliesTo matcher.
	resolvedBinding := policyresolver.RequirementTargetRefToResolved(
		treq.Namespace, treq.Name, treq.Spec.TargetRefs,
	)

	// List all Deployments in the cluster. TemplateRequirement objects live
	// in org/folder namespaces and target project-namespace Deployments
	// across the entire tree, so we must list cluster-wide.
	var depList deploymentsv1alpha1.DeploymentList
	if err := r.List(ctx, &depList); err != nil {
		return metav1.Condition{}, fmt.Errorf("list Deployments: %w", err)
	}

	var firstTransient error
	matched := 0
	for i := range depList.Items {
		d := &depList.Items[i]
		project := d.Spec.ProjectName
		deploymentName := d.Name

		if !policyresolver.BindingAppliesToDeployment(resolvedBinding, project, deploymentName) {
			continue
		}
		matched++

		if err := deployments.EnsureSingletonDependencyDeployment(
			ctx, r.Client, r.Validator, treq.Spec.Requires, d, cascadeDelete,
		); err != nil {
			var notFound *deployments.GrantNotFoundError
			if errors.As(err, &notFound) {
				return metav1.Condition{
					Type:    v1alpha1.TemplateRequirementConditionResolvedRefs,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.TemplateRequirementReasonGrantNotFound,
					Message: err.Error(),
				}, nil
			}
			if firstTransient == nil {
				firstTransient = err
			}
		}
	}

	if firstTransient != nil {
		return metav1.Condition{
			Type:    v1alpha1.TemplateRequirementConditionResolvedRefs,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplateRequirementReasonNotReady,
			Message: fmt.Sprintf("transient error ensuring singleton: %v", firstTransient),
		}, firstTransient
	}

	if matched == 0 {
		return metav1.Condition{
			Type:    v1alpha1.TemplateRequirementConditionResolvedRefs,
			Status:  metav1.ConditionTrue,
			Reason:  v1alpha1.TemplateRequirementReasonResolvedRefs,
			Message: "no matching Deployments found; singletons will be created on first matching Deployment",
		}, nil
	}

	return metav1.Condition{
		Type:    v1alpha1.TemplateRequirementConditionResolvedRefs,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.TemplateRequirementReasonResolvedRefs,
		Message: fmt.Sprintf("singleton Deployment ensured for %d matching Deployment(s)", matched),
	}, nil
}

// requirementsForDeployment returns reconcile requests for every
// TemplateRequirement in the cluster whose targetRefs might match the given
// Deployment. Called by the Watches handler in SetupWithManager so that
// creating or deleting a Deployment re-enqueues any affected
// TemplateRequirement objects.
//
// To avoid a full cluster-wide Deployment list inside the mapper itself, we
// enqueue all TemplateRequirements in the cluster and let each individual
// Reconcile call do the targeted match. This is safe because the reconcile
// work is idempotent and the number of TemplateRequirements is small.
func (r *TemplateRequirementReconciler) requirementsForDeployment(ctx context.Context, obj client.Object) []reconcile.Request {
	var list v1alpha1.TemplateRequirementList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0, len(list.Items))
	for _, treq := range list.Items {
		out = append(out, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&treq),
		})
	}
	return out
}
