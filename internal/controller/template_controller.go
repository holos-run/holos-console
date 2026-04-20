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

	"cuelang.org/go/cue/parser"
	"github.com/Masterminds/semver/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// TemplateReconciler reconciles a Template object. It validates the CUE
// payload, resolves linked template references against the same cache
// (HOL-621 widens the resolution scope), and publishes the well-known
// condition set defined in HOL-618 (ADR 030). HOL-620 lands the reconciler;
// HOL-621 rewires the RPC read path through the cache this reconciler fills.
//
// RBAC for this reconciler and the two sibling reconcilers lives on the
// package doc comment in rbac.go so controller-gen emits a single
// ClusterRole artifact keyed on the console service account.
type TemplateReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// SetupWithManager registers the reconciler with the supplied manager. The
// For(&Template{}) call establishes the primary watch; .Complete wires the
// reconciler into the controller's work queue.
func (r *TemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Template{}).
		Named("template-controller").
		Complete(r)
}

// Reconcile implements the reconciliation loop for the Template kind. See
// HOL-620 for the full contract; in summary:
//
//  1. Fetch the object; NotFound -> no-op.
//  2. Build the Accepted / CUEValid / LinkedRefsResolved component
//     conditions from the current spec.
//  3. Aggregate into Ready.
//  4. Stamp metadata.generation on every condition plus
//     status.observedGeneration.
//  5. Write status ONLY when ObservedGeneration advances or a condition's
//     (Status, Reason, Message) tuple changes — guards against the classic
//     hot-loop where Reconcile keeps writing the same status and the API
//     server keeps firing watch events back at us.
func (r *TemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var tmpl v1alpha1.Template
	if err := r.Get(ctx, req.NamespacedName, &tmpl); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get Template: %w", err)
	}

	gen := tmpl.Generation
	components := buildTemplateConditions(&tmpl)

	proposed := make([]metav1.Condition, 0, len(components)+1)
	for _, c := range components {
		c.ObservedGeneration = gen
		proposed = append(proposed, c)
	}
	ready := aggregateReady(components,
		v1alpha1.TemplateReasonReady,
		v1alpha1.TemplateReasonNotReady,
		"Template is accepted, the CUE payload is valid, and every linked template reference resolves.",
		"Template is not Ready; see component conditions for details.")
	ready.Type = v1alpha1.TemplateConditionReady
	ready.ObservedGeneration = gen
	proposed = append(proposed, ready)

	// Build the target status by cloning the existing conditions and
	// merging the proposed conditions idempotently. meta.SetStatusCondition
	// handles LastTransitionTime bookkeeping.
	target := tmpl.DeepCopy()
	target.Status.ObservedGeneration = gen
	newConds := append([]metav1.Condition(nil), tmpl.Status.Conditions...)
	for _, pc := range proposed {
		mergeCondition(&newConds, gen, pc)
	}
	target.Status.Conditions = newConds

	if tmpl.Status.ObservedGeneration == gen &&
		conditionsEqualIgnoringTransitionTime(tmpl.Status.Conditions, target.Status.Conditions) {
		// No-op: the existing status already reflects this generation
		// with the same component conditions. Skipping the write
		// prevents the hot-loop HOL-620 calls out explicitly.
		logger.V(1).Info("Template status unchanged; skipping update", "generation", gen)
		return ctrl.Result{}, nil
	}

	if err := r.Status().Update(ctx, target); err != nil {
		if apierrors.IsConflict(err) {
			// Someone else beat us to a status update. Requeue
			// immediately: controller-runtime will rate-limit
			// the retry for us.
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update Template status: %w", err)
	}
	if ready.Status == metav1.ConditionTrue {
		r.Recorder.Eventf(target, "Normal", v1alpha1.TemplateReasonReady, "Template is Ready")
	} else {
		r.Recorder.Eventf(target, "Warning", ready.Reason, "%s", ready.Message)
	}
	return ctrl.Result{}, nil
}

// buildTemplateConditions computes the Accepted, CUEValid, and
// LinkedRefsResolved component conditions from the current spec. The caller
// aggregates them into Ready and stamps observedGeneration.
//
// Callers receive conditions in the canonical order:
// Accepted, CUEValid, LinkedRefsResolved. Keeping a stable order simplifies
// test assertions.
func buildTemplateConditions(tmpl *v1alpha1.Template) []metav1.Condition {
	conds := make([]metav1.Condition, 0, 3)
	conds = append(conds, templateAcceptedCondition(tmpl))
	conds = append(conds, templateCUEValidCondition(tmpl))
	conds = append(conds, templateLinkedRefsCondition(tmpl))
	return conds
}

// templateAcceptedCondition enforces the minimum spec invariants the
// ValidatingAdmissionPolicy in config/admission/ does not cover: the
// template must carry a non-empty CUE payload (empty templates contribute
// nothing at render time), and the declared Version — when present —
// must parse as semver. These invariants are checked on write by admission
// and on reconcile here; the reconciler is the belt-and-braces path so
// operators that bypass admission (for example, via kubectl --server-side
// --force) still see the InvalidSpec reason surfaced on .status.
func templateAcceptedCondition(tmpl *v1alpha1.Template) metav1.Condition {
	if tmpl.Spec.CueTemplate == "" {
		return metav1.Condition{
			Type:    v1alpha1.TemplateConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplateReasonInvalidSpec,
			Message: "spec.cueTemplate must not be empty",
		}
	}
	if tmpl.Spec.Version != "" {
		if _, err := semver.NewVersion(tmpl.Spec.Version); err != nil {
			return metav1.Condition{
				Type:    v1alpha1.TemplateConditionAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.TemplateReasonInvalidSpec,
				Message: fmt.Sprintf("spec.version %q is not a valid semver string: %v", tmpl.Spec.Version, err),
			}
		}
	}
	return metav1.Condition{
		Type:    v1alpha1.TemplateConditionAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.TemplateReasonAccepted,
		Message: "spec passed reconciler validation",
	}
}

// templateCUEValidCondition parses the CUE payload so the status surface
// matches what the render path will see at runtime. HOL-620 implements the
// parse check (reason=CUEParseError on failure). Deeper type-checking
// against the generated v1alpha2 schema (reason=CUETypeError) is wired in
// during HOL-621 once the reconciler has access to the render path.
func templateCUEValidCondition(tmpl *v1alpha1.Template) metav1.Condition {
	if tmpl.Spec.CueTemplate == "" {
		// Accepted=False already captures the empty case — report
		// CUEValid as False with the same invalid-spec reason so a
		// human reading just the condition list is not misled.
		return metav1.Condition{
			Type:    v1alpha1.TemplateConditionCUEValid,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplateReasonCUEParseError,
			Message: "spec.cueTemplate is empty",
		}
	}
	if _, err := parser.ParseFile("template.cue", tmpl.Spec.CueTemplate); err != nil {
		return metav1.Condition{
			Type:    v1alpha1.TemplateConditionCUEValid,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.TemplateReasonCUEParseError,
			Message: fmt.Sprintf("CUE parse error: %v", err),
		}
	}
	return metav1.Condition{
		Type:    v1alpha1.TemplateConditionCUEValid,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.TemplateReasonCUEValid,
		Message: "CUE payload parses successfully",
	}
}

// templateLinkedRefsCondition validates the version constraints carried on
// each LinkedTemplateRef. HOL-620 intentionally does NOT look up the
// referenced templates in the cache yet — that cross-namespace read path
// wires in during HOL-622. For now, a malformed version constraint flips
// the condition False with LinkedRefVersionMismatch so operators see the
// failure surface even before the full resolver lands. An empty
// LinkedTemplates list is True with ResolvedRefs — nothing to resolve.
func templateLinkedRefsCondition(tmpl *v1alpha1.Template) metav1.Condition {
	for _, ref := range tmpl.Spec.LinkedTemplates {
		if ref.VersionConstraint == "" {
			continue
		}
		if _, err := semver.NewConstraint(ref.VersionConstraint); err != nil {
			return metav1.Condition{
				Type:    v1alpha1.TemplateConditionLinkedRefsResolved,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.TemplateReasonLinkedRefVersionMismatch,
				Message: fmt.Sprintf("linkedTemplates[%s/%s].versionConstraint is not a valid semver constraint: %v", ref.Namespace, ref.Name, err),
			}
		}
	}
	return metav1.Condition{
		Type:    v1alpha1.TemplateConditionLinkedRefsResolved,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.TemplateReasonResolvedRefs,
		Message: "every linkedTemplates version constraint parses",
	}
}
