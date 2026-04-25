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

// Package controller — TemplateDependencyReconciler.
//
// # Design
//
// TemplateDependencyReconciler watches TemplateDependency objects scoped to
// project namespaces. For each reconcile it:
//
//  1. Validates the spec (Accepted condition).
//  2. Lists every Deployment in the same namespace whose TemplateRef matches
//     the Dependent template reference.
//  3. For each matching Deployment, calls EnsureSingletonDependencyDeployment
//     from console/deployments — the shared helper that creates or updates the
//     singleton Requires Deployment with a non-controller ownerReference.
//  4. Validates that the Requires cross-namespace reference is authorised by a
//     TemplateGrant (ResolvedRefs condition).
//  5. Aggregates Accepted + ResolvedRefs into the top-level Ready condition and
//     writes status if it has changed.
//
// # Interaction with native GC
//
// Owner-references on the singleton Deployment point to the dependent
// Deployment objects with Controller=false and BlockOwnerDeletion=true. When
// the last dependent Deployment is deleted the Kubernetes garbage collector
// reaps the singleton automatically — no finalizer or explicit delete in this
// reconciler is required.
//
// # Stylistic reference
//
// This file follows internal/controller/template_policy_binding_controller.go.
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
)

// TemplateDependencyReconciler reconciles TemplateDependency objects scoped
// to project namespaces. It calls EnsureSingletonDependencyDeployment for
// each matching dependent Deployment and surfaces grant-validation results as
// Kubernetes conditions.
//
// RBAC markers for this reconciler live on the package doc comment in
// rbac.go — controller-gen's rbac generator ignores markers on struct or
// method doc comments.
type TemplateDependencyReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Validator deployments.Validator
}

// SetupWithManager registers the reconciler with the supplied manager. In
// addition to the primary For(&TemplateDependency{}), it adds a secondary
// watch on Deployment objects: when a Deployment is created or deleted the
// ownerReference set on the singleton changes, so the TemplateDependency
// objects in the same namespace must be re-reconciled.
func (r *TemplateDependencyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.TemplateDependency{}).
		Named("template-dependency-controller").
		Watches(
			&deploymentsv1alpha1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(r.dependenciesForDeployment),
		).
		Complete(r)
}

// Reconcile implements the reconciliation loop for TemplateDependency.
func (r *TemplateDependencyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var dep v1alpha1.TemplateDependency
	if err := r.Get(ctx, req.NamespacedName, &dep); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get TemplateDependency: %w", err)
	}

	gen := dep.Generation

	accepted := dependencyAcceptedCondition(&dep)
	resolved, helperErr := r.dependencyResolvedRefsCondition(ctx, &dep)
	components := []metav1.Condition{accepted, resolved}

	proposed := make([]metav1.Condition, 0, 3)
	for _, c := range components {
		c.ObservedGeneration = gen
		proposed = append(proposed, c)
	}
	ready := aggregateReady(components,
		v1alpha1.TemplateDependencyReasonReady,
		v1alpha1.TemplateDependencyReasonNotReady,
		"TemplateDependency is accepted and the required Deployment is materialised.",
		"TemplateDependency is not Ready; see component conditions for details.")
	ready.Type = v1alpha1.TemplateDependencyConditionReady
	ready.ObservedGeneration = gen
	proposed = append(proposed, ready)

	target := dep.DeepCopy()
	target.Status.ObservedGeneration = gen
	newConds := append([]metav1.Condition(nil), dep.Status.Conditions...)
	for _, pc := range proposed {
		mergeCondition(&newConds, gen, pc)
	}
	target.Status.Conditions = newConds

	if dep.Status.ObservedGeneration == gen &&
		conditionsEqualIgnoringTransitionTime(dep.Status.Conditions, target.Status.Conditions) {
		logger.V(1).Info("TemplateDependency status unchanged; skipping update", "generation", gen)
		// If the helper encountered a transient error (conflict, API server
		// hiccup), requeue even though status did not change.
		if helperErr != nil {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, nil
	}

	if err := r.Status().Update(ctx, target); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update TemplateDependency status: %w", err)
	}
	if ready.Status == metav1.ConditionTrue {
		r.Recorder.Eventf(target, "Normal", v1alpha1.TemplateDependencyReasonReady, "TemplateDependency is Ready")
	} else {
		r.Recorder.Eventf(target, "Warning", ready.Reason, "%s", ready.Message)
	}

	if helperErr != nil {
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// dependencyAcceptedCondition validates the TemplateDependency spec fields
// the reconciler can check without API server calls: non-empty namespace/name
// on both Dependent and Requires references.
func dependencyAcceptedCondition(dep *v1alpha1.TemplateDependency) metav1.Condition {
	if dep.Spec.Dependent.Namespace == "" || dep.Spec.Dependent.Name == "" {
		return metav1.Condition{
			Type:    v1alpha1.TemplateDependencyConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplateDependencyReasonInvalidSpec,
			Message: "spec.dependent must set both namespace and name",
		}
	}
	if dep.Spec.Requires.Namespace == "" || dep.Spec.Requires.Name == "" {
		return metav1.Condition{
			Type:    v1alpha1.TemplateDependencyConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplateDependencyReasonInvalidSpec,
			Message: "spec.requires must set both namespace and name",
		}
	}
	return metav1.Condition{
		Type:    v1alpha1.TemplateDependencyConditionAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.TemplateDependencyReasonAccepted,
		Message: "spec passed reconciler validation",
	}
}

// dependencyResolvedRefsCondition lists matching dependent Deployments,
// calls EnsureSingletonDependencyDeployment for each, and returns the
// ResolvedRefs condition. The second return value carries the first
// transient error encountered (conflict, API server error); callers requeue
// when it is non-nil regardless of whether the condition changed.
func (r *TemplateDependencyReconciler) dependencyResolvedRefsCondition(ctx context.Context, dep *v1alpha1.TemplateDependency) (metav1.Condition, error) {
	// Short-circuit: if Accepted=False the spec is broken; we cannot
	// safely do any reconcile work.
	if dep.Spec.Dependent.Namespace == "" || dep.Spec.Dependent.Name == "" ||
		dep.Spec.Requires.Namespace == "" || dep.Spec.Requires.Name == "" {
		return metav1.Condition{
			Type:    v1alpha1.TemplateDependencyConditionResolvedRefs,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplateDependencyReasonInvalidSpec,
			Message: "spec is invalid; see Accepted condition",
		}, nil
	}

	// Validate cross-namespace grant before listing Deployments.
	dependentRef := v1alpha1.LinkedTemplateRef{
		Namespace: dep.Namespace,
		Name:      dep.Spec.Dependent.Name,
	}
	if err := r.Validator.ValidateGrant(ctx, dependentRef, dep.Spec.Requires); err != nil {
		var notFound *deployments.GrantNotFoundError
		if errors.As(err, &notFound) {
			return metav1.Condition{
				Type:    v1alpha1.TemplateDependencyConditionResolvedRefs,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.TemplateDependencyReasonGrantNotFound,
				Message: err.Error(),
			}, nil
		}
		return metav1.Condition{}, fmt.Errorf("validating grant: %w", err)
	}

	// List Deployments in the TemplateDependency's namespace whose
	// TemplateRef matches the Dependent reference.
	var depList deploymentsv1alpha1.DeploymentList
	if err := r.List(ctx, &depList, client.InNamespace(dep.Namespace)); err != nil {
		return metav1.Condition{}, fmt.Errorf("list Deployments: %w", err)
	}

	cascadeDelete := true
	if dep.Spec.CascadeDelete != nil {
		cascadeDelete = *dep.Spec.CascadeDelete
	}

	var firstTransient error
	matched := 0
	for i := range depList.Items {
		d := &depList.Items[i]
		if d.Spec.TemplateRef.Namespace != dep.Spec.Dependent.Namespace ||
			d.Spec.TemplateRef.Name != dep.Spec.Dependent.Name {
			continue
		}
		matched++
		if err := deployments.EnsureSingletonDependencyDeployment(ctx, r.Client, r.Validator, dep.Spec.Requires, d, cascadeDelete); err != nil {
			var notFound *deployments.GrantNotFoundError
			if errors.As(err, &notFound) {
				// Grant was revoked between the check above and the call
				// here (race). Surface as ResolvedRefs=False.
				return metav1.Condition{
					Type:    v1alpha1.TemplateDependencyConditionResolvedRefs,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.TemplateDependencyReasonGrantNotFound,
					Message: err.Error(),
				}, nil
			}
			// Transient error (conflict, etc.). Record and keep going so
			// all matched Deployments are attempted; we'll requeue at the
			// end.
			if firstTransient == nil {
				firstTransient = err
			}
		}
	}

	if firstTransient != nil {
		// Return a degraded ResolvedRefs condition so the status reflects
		// the partial state rather than silently claiming Ready=True.
		return metav1.Condition{
			Type:    v1alpha1.TemplateDependencyConditionResolvedRefs,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplateDependencyReasonNotReady,
			Message: fmt.Sprintf("transient error ensuring singleton: %v", firstTransient),
		}, firstTransient
	}

	if matched == 0 {
		// No matching Deployments yet — the dependency is registered but
		// no dependent has been deployed. This is Ready=True from the
		// dependency's perspective; the singleton will be materialised on
		// first Deployment creation.
		return metav1.Condition{
			Type:    v1alpha1.TemplateDependencyConditionResolvedRefs,
			Status:  metav1.ConditionTrue,
			Reason:  v1alpha1.TemplateDependencyReasonResolvedRefs,
			Message: "no matching Deployments found; singleton will be created on first Deployment",
		}, nil
	}

	return metav1.Condition{
		Type:    v1alpha1.TemplateDependencyConditionResolvedRefs,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.TemplateDependencyReasonResolvedRefs,
		Message: fmt.Sprintf("singleton Deployment ensured for %d dependent Deployment(s)", matched),
	}, nil
}

// dependenciesForDeployment returns the reconcile requests for every
// TemplateDependency in the same namespace as the Deployment whose Dependent
// reference matches the Deployment's TemplateRef. Called by the Watches
// handler set up in SetupWithManager so that creating or deleting a Deployment
// re-enqueues the affected TemplateDependency objects.
func (r *TemplateDependencyReconciler) dependenciesForDeployment(ctx context.Context, obj client.Object) []reconcile.Request {
	d, ok := obj.(*deploymentsv1alpha1.Deployment)
	if !ok {
		return nil
	}
	var list v1alpha1.TemplateDependencyList
	if err := r.List(ctx, &list, client.InNamespace(d.Namespace)); err != nil {
		return nil
	}
	var out []reconcile.Request
	for _, dep := range list.Items {
		if dep.Spec.Dependent.Namespace != d.Spec.TemplateRef.Namespace ||
			dep.Spec.Dependent.Name != d.Spec.TemplateRef.Name {
			continue
		}
		out = append(out, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&dep),
		})
	}
	return out
}
