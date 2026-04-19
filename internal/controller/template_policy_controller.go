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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// TemplatePolicyReconciler reconciles a TemplatePolicy object. Unlike the
// Template reconciler, the policy surface HOL-620 validates is much smaller:
// the admission policy in config/admission/ rejects creates in
// project-labeled namespaces and the CRD validation enforces the
// `+kubebuilder:validation:MinItems=1` on .spec.rules. The reconciler here
// surfaces the "Accepted" status for any residual post-admission check
// (currently duplicate-rule detection) and publishes the aggregate Ready
// condition downstream consumers read.
//
// RBAC markers for this reconciler live on the package doc comment in
// rbac.go — controller-gen's rbac generator ignores markers on struct or
// method doc comments.
type TemplatePolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// SetupWithManager registers the reconciler with the supplied manager.
func (r *TemplatePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.TemplatePolicy{}).
		Named("template-policy-controller").
		Complete(r)
}

// Reconcile implements the reconciliation loop for TemplatePolicy. See the
// Reconcile doc on TemplateReconciler for the overall contract; the policy
// kind only differs in the component-condition set.
func (r *TemplatePolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var pol v1alpha1.TemplatePolicy
	if err := r.Get(ctx, req.NamespacedName, &pol); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get TemplatePolicy: %w", err)
	}

	gen := pol.Generation
	components := []metav1.Condition{templatePolicyAcceptedCondition(&pol)}
	proposed := make([]metav1.Condition, 0, 2)
	for _, c := range components {
		c.ObservedGeneration = gen
		proposed = append(proposed, c)
	}
	ready := aggregateReady(components,
		v1alpha1.TemplatePolicyReasonReady,
		v1alpha1.TemplatePolicyReasonNotReady,
		"TemplatePolicy is accepted.",
		"TemplatePolicy is not Ready; see component conditions for details.")
	ready.Type = v1alpha1.TemplatePolicyConditionReady
	ready.ObservedGeneration = gen
	proposed = append(proposed, ready)

	target := pol.DeepCopy()
	target.Status.ObservedGeneration = gen
	newConds := append([]metav1.Condition(nil), pol.Status.Conditions...)
	for _, pc := range proposed {
		mergeCondition(&newConds, gen, pc)
	}
	target.Status.Conditions = newConds

	if pol.Status.ObservedGeneration == gen &&
		conditionsEqualIgnoringTransitionTime(pol.Status.Conditions, target.Status.Conditions) {
		logger.V(1).Info("TemplatePolicy status unchanged; skipping update", "generation", gen)
		return ctrl.Result{}, nil
	}

	if err := r.Status().Update(ctx, target); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update TemplatePolicy status: %w", err)
	}
	if ready.Status == metav1.ConditionTrue {
		r.Recorder.Eventf(target, "Normal", v1alpha1.TemplatePolicyReasonReady, "TemplatePolicy is Ready")
	} else {
		r.Recorder.Eventf(target, "Warning", ready.Reason, "%s", ready.Message)
	}
	return ctrl.Result{}, nil
}

// templatePolicyAcceptedCondition checks the invariants the reconciler
// owns: at least one rule, no duplicate (kind, scope, scopeName, name)
// rule tuples. The CRD schema already enforces MinItems=1; we re-check here
// so the condition surface is populated on objects that bypassed the
// admission check.
func templatePolicyAcceptedCondition(pol *v1alpha1.TemplatePolicy) metav1.Condition {
	if len(pol.Spec.Rules) == 0 {
		return metav1.Condition{
			Type:    v1alpha1.TemplatePolicyConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplatePolicyReasonInvalidRules,
			Message: "spec.rules must contain at least one rule",
		}
	}
	seen := make(map[string]struct{}, len(pol.Spec.Rules))
	for i, rule := range pol.Spec.Rules {
		key := fmt.Sprintf("%s|%s|%s|%s", rule.Kind, rule.Template.Scope, rule.Template.ScopeName, rule.Template.Name)
		if _, dup := seen[key]; dup {
			return metav1.Condition{
				Type:    v1alpha1.TemplatePolicyConditionAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.TemplatePolicyReasonInvalidRules,
				Message: fmt.Sprintf("spec.rules[%d] duplicates an earlier rule with kind=%s, template=%s/%s/%s", i, rule.Kind, rule.Template.Scope, rule.Template.ScopeName, rule.Template.Name),
			}
		}
		seen[key] = struct{}{}
	}
	return metav1.Condition{
		Type:    v1alpha1.TemplatePolicyConditionAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.TemplatePolicyReasonAccepted,
		Message: "spec.rules passed reconciler validation",
	}
}
