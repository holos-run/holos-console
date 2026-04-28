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

// Package controller -- DeploymentReconciler.
//
// The Deployment reconciler is intentionally small in this phase. It watches
// deployments.holos.run/v1alpha1.Deployment objects, acknowledges that the
// spec was accepted, records the observed generation, and emits a Reconciled
// event. Later phases plug the render/apply pipeline into the Pipeline field.
package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	"github.com/holos-run/holos-console/internal/deploymentrender"
)

const (
	deploymentReasonAccepted   = "Accepted"
	deploymentReasonReconciled = "Reconciled"
)

// DeploymentReconciler reconciles Deployment objects using the console
// controller manager's cluster credentials.
//
// RBAC markers for this reconciler live on the package doc comment in
// rbac.go -- controller-gen's rbac generator ignores markers on struct or
// method doc comments.
type DeploymentReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Pipeline *deploymentrender.Pipeline
}

// SetupWithManager registers the reconciler with the supplied manager.
func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deploymentsv1alpha1.Deployment{}).
		Named("deployment-controller").
		Complete(r)
}

// Reconcile acknowledges Deployment objects. Rendering and applying manifests
// is deliberately deferred to the next phase; Pipeline may be nil here.
func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var dep deploymentsv1alpha1.Deployment
	if err := r.Get(ctx, req.NamespacedName, &dep); err != nil {
		if err := client.IgnoreNotFound(err); err != nil {
			return ctrl.Result{}, fmt.Errorf("get Deployment: %w", err)
		}
		return ctrl.Result{}, nil
	}

	gen := dep.Generation
	target := dep.DeepCopy()
	target.Status.ObservedGeneration = gen
	conds := append([]metav1.Condition(nil), dep.Status.Conditions...)
	meta.SetStatusCondition(&conds, metav1.Condition{
		Type:               deploymentsv1alpha1.ConditionTypeAccepted,
		Status:             metav1.ConditionTrue,
		Reason:             deploymentReasonAccepted,
		Message:            "deployment spec accepted; render/apply is not enabled yet",
		ObservedGeneration: gen,
	})
	target.Status.Conditions = conds

	if dep.Status.ObservedGeneration != gen ||
		!conditionsEqualIgnoringTransitionTime(dep.Status.Conditions, target.Status.Conditions) {
		if err := r.Status().Update(ctx, target); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("update Deployment status: %w", err)
		}
	} else {
		logger.V(1).Info("Deployment status unchanged; skipping update", "generation", gen)
	}

	r.Recorder.Eventf(target, "Normal", deploymentReasonReconciled, "Deployment reconciled")
	return ctrl.Result{}, nil
}
