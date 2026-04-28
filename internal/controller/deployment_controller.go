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
// The Deployment reconciler watches deployments.holos.run/v1alpha1.Deployment
// objects, renders their referenced templates, applies the resulting
// Kubernetes manifests, and publishes Accepted/Rendered/Applied conditions.
package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/internal/deploymentrender"
)

const (
	deploymentReasonAccepted   = "Accepted"
	deploymentReasonReconciled = "Reconciled"
)

// DeploymentPolicyDriftRecorder exposes the write side of the applied render
// set store. It intentionally matches deployments.PolicyDriftChecker so the
// store implementation can move from RPC handlers to this reconciler without
// changing interface shape.
type DeploymentPolicyDriftRecorder interface {
	RecordApplied(ctx context.Context, project, deploymentName string, refs []*consolev1.LinkedTemplateRef) error
}

// DeploymentAncestorWalker resolves folder ancestry for PlatformInput.
type DeploymentAncestorWalker interface {
	GetProjectFolders(ctx context.Context, project string) ([]string, error)
}

// DeploymentGatewayResolver resolves the ingress gateway namespace for a
// project. Nil or empty results fall back to deploymentrender's default.
type DeploymentGatewayResolver interface {
	GetGatewayNamespace(ctx context.Context, project string) (string, error)
}

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

	// Pipeline is optional so tests and bootstrap paths can still run with
	// status-only reconciliation. Production wiring configures it before the
	// manager starts.
	Pipeline            *deploymentrender.Pipeline
	PolicyDriftRecorder DeploymentPolicyDriftRecorder
	AncestorWalker      DeploymentAncestorWalker
	GatewayResolver     DeploymentGatewayResolver
}

// SetupWithManager registers the reconciler with the supplied manager.
func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deploymentsv1alpha1.Deployment{}).
		Named("deployment-controller").
		Complete(r)
}

// Reconcile renders and applies Deployment-derived manifests when the pipeline
// is configured, then records status conditions that describe the outcome.
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
	if deploymentObservedApplied(&dep, gen) {
		logger.V(1).Info("Deployment generation already rendered and applied; skipping", "generation", gen)
		return ctrl.Result{}, nil
	}

	conds := append([]metav1.Condition(nil), dep.Status.Conditions...)
	meta.SetStatusCondition(&conds, metav1.Condition{
		Type:               deploymentsv1alpha1.ConditionTypeAccepted,
		Status:             metav1.ConditionTrue,
		Reason:             deploymentReasonAccepted,
		Message:            "deployment spec accepted",
		ObservedGeneration: gen,
	})

	if !r.Pipeline.CanRender() || !r.Pipeline.CanApply() {
		meta.SetStatusCondition(&conds, metav1.Condition{
			Type:               deploymentsv1alpha1.ConditionTypeRendered,
			Status:             metav1.ConditionFalse,
			Reason:             deploymentsv1alpha1.DeploymentReasonRenderFailed,
			Message:            "deployment render/apply pipeline is not configured",
			ObservedGeneration: gen,
		})
		if err := r.updateDeploymentStatus(ctx, &dep, gen, conds); err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info("Deployment render/apply pipeline is not configured; status-only reconcile", "generation", gen)
		return ctrl.Result{}, nil
	}

	template := &templatesv1alpha1.Template{}
	templateKey := client.ObjectKey{Namespace: dep.Spec.TemplateRef.Namespace, Name: dep.Spec.TemplateRef.Name}
	if err := r.Get(ctx, templateKey, template); err != nil {
		reason := deploymentsv1alpha1.DeploymentReasonRenderFailed
		if apierrors.IsNotFound(err) {
			reason = deploymentsv1alpha1.DeploymentReasonAncestorTemplateMissing
		}
		meta.SetStatusCondition(&conds, metav1.Condition{
			Type:               deploymentsv1alpha1.ConditionTypeRendered,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            fmt.Sprintf("referenced template %s/%s is unavailable: %v", templateKey.Namespace, templateKey.Name, err),
			ObservedGeneration: gen,
		})
		meta.SetStatusCondition(&conds, metav1.Condition{
			Type:               deploymentsv1alpha1.ConditionTypeApplied,
			Status:             metav1.ConditionFalse,
			Reason:             deploymentsv1alpha1.DeploymentReasonRenderFailed,
			Message:            "apply skipped because render inputs could not be resolved",
			ObservedGeneration: gen,
		})
		if statusErr := r.updateDeploymentStatus(ctx, &dep, gen, conds); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, fmt.Errorf("resolve Deployment template: %w", err)
	}

	project := dep.Spec.ProjectName
	name := dep.Name
	grouped, effectiveRefs, renderErr := r.Pipeline.Render(ctx, project, name, template.Spec.CueTemplate, r.buildPlatformInput(ctx, &dep), deploymentProjectInput(&dep))
	if renderErr != nil {
		reason := deploymentsv1alpha1.DeploymentReasonRenderFailed
		if isAncestorTemplateMissing(renderErr) {
			reason = deploymentsv1alpha1.DeploymentReasonAncestorTemplateMissing
		}
		meta.SetStatusCondition(&conds, metav1.Condition{
			Type:               deploymentsv1alpha1.ConditionTypeRendered,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            fmt.Sprintf("render failed: %v", renderErr),
			ObservedGeneration: gen,
		})
		meta.SetStatusCondition(&conds, metav1.Condition{
			Type:               deploymentsv1alpha1.ConditionTypeApplied,
			Status:             metav1.ConditionFalse,
			Reason:             deploymentsv1alpha1.DeploymentReasonRenderFailed,
			Message:            "apply skipped because render failed",
			ObservedGeneration: gen,
		})
		if statusErr := r.updateDeploymentStatus(ctx, &dep, gen, conds); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, fmt.Errorf("render Deployment resources: %w", renderErr)
	}

	resources := append(grouped.Platform, grouped.Project...)
	meta.SetStatusCondition(&conds, metav1.Condition{
		Type:               deploymentsv1alpha1.ConditionTypeRendered,
		Status:             metav1.ConditionTrue,
		Reason:             deploymentsv1alpha1.DeploymentReasonRenderSucceeded,
		Message:            fmt.Sprintf("rendered %d Kubernetes resource(s)", len(resources)),
		ObservedGeneration: gen,
	})

	if applyErr := r.Pipeline.Apply(ctx, project, name, resources); applyErr != nil {
		meta.SetStatusCondition(&conds, metav1.Condition{
			Type:               deploymentsv1alpha1.ConditionTypeApplied,
			Status:             metav1.ConditionFalse,
			Reason:             deploymentsv1alpha1.DeploymentReasonApplyFailed,
			Message:            fmt.Sprintf("apply failed: %v", applyErr),
			ObservedGeneration: gen,
		})
		if statusErr := r.updateDeploymentStatus(ctx, &dep, gen, conds); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, fmt.Errorf("apply Deployment resources: %w", applyErr)
	}

	if r.PolicyDriftRecorder != nil {
		refsToRecord := effectiveRefs
		if refsToRecord == nil {
			refsToRecord = []*consolev1.LinkedTemplateRef{}
		}
		if err := r.PolicyDriftRecorder.RecordApplied(ctx, project, name, refsToRecord); err != nil {
			slog.WarnContext(ctx, "failed to record applied render set after controller apply",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", err),
			)
		}
	}
	meta.SetStatusCondition(&conds, metav1.Condition{
		Type:               deploymentsv1alpha1.ConditionTypeApplied,
		Status:             metav1.ConditionTrue,
		Reason:             deploymentsv1alpha1.DeploymentReasonApplySucceeded,
		Message:            fmt.Sprintf("applied %d Kubernetes resource(s)", len(resources)),
		ObservedGeneration: gen,
	})
	if err := r.updateDeploymentStatus(ctx, &dep, gen, conds); err != nil {
		return ctrl.Result{}, err
	}
	r.Recorder.Eventf(&dep, "Normal", deploymentReasonReconciled, "Deployment rendered and applied")
	return ctrl.Result{}, nil
}

func (r *DeploymentReconciler) updateDeploymentStatus(ctx context.Context, dep *deploymentsv1alpha1.Deployment, gen int64, conds []metav1.Condition) error {
	target := dep.DeepCopy()
	target.Status.ObservedGeneration = gen
	target.Status.Conditions = conds

	statusChanged := dep.Status.ObservedGeneration != gen ||
		!conditionsEqualIgnoringTransitionTime(dep.Status.Conditions, target.Status.Conditions)
	if statusChanged {
		if err := r.Status().Update(ctx, target); err != nil {
			if apierrors.IsConflict(err) {
				return fmt.Errorf("update Deployment status conflict: %w", err)
			}
			return fmt.Errorf("update Deployment status: %w", err)
		}
	}
	return nil
}

func (r *DeploymentReconciler) buildPlatformInput(ctx context.Context, dep *deploymentsv1alpha1.Deployment) v1alpha2.PlatformInput {
	project := dep.Spec.ProjectName
	pi := v1alpha2.PlatformInput{
		Project:          project,
		Namespace:        dep.Namespace,
		GatewayNamespace: deploymentrender.DefaultGatewayNamespace,
	}
	if r.GatewayResolver != nil {
		if gw, err := r.GatewayResolver.GetGatewayNamespace(ctx, project); err != nil {
			slog.WarnContext(ctx, "could not resolve org gateway namespace, falling back to default",
				slog.String("project", project),
				slog.String("default", deploymentrender.DefaultGatewayNamespace),
				slog.Any("error", err),
			)
		} else if gw != "" {
			pi.GatewayNamespace = gw
		}
	}
	if r.AncestorWalker != nil {
		folders, err := r.AncestorWalker.GetProjectFolders(ctx, project)
		if err != nil {
			slog.WarnContext(ctx, "could not resolve folder ancestry for platform input",
				slog.String("project", project),
				slog.Any("error", err),
			)
		} else {
			pi.Folders = make([]v1alpha2.FolderInfo, 0, len(folders))
			for _, folder := range folders {
				pi.Folders = append(pi.Folders, v1alpha2.FolderInfo{Name: folder})
			}
		}
	}
	return pi
}

func deploymentProjectInput(dep *deploymentsv1alpha1.Deployment) v1alpha2.ProjectInput {
	port := int(dep.Spec.Port)
	if port == 0 {
		port = 8080
	}
	return v1alpha2.ProjectInput{
		Name:        dep.Name,
		Image:       dep.Spec.Image,
		Tag:         dep.Spec.Tag,
		Command:     dep.Spec.Command,
		Args:        dep.Spec.Args,
		Port:        port,
		Description: dep.Spec.Description,
	}
}

func isAncestorTemplateMissing(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "ancestor") && (strings.Contains(msg, "not found") || strings.Contains(msg, "missing"))
}

func deploymentObservedApplied(dep *deploymentsv1alpha1.Deployment, gen int64) bool {
	rendered := meta.FindStatusCondition(dep.Status.Conditions, deploymentsv1alpha1.ConditionTypeRendered)
	applied := meta.FindStatusCondition(dep.Status.Conditions, deploymentsv1alpha1.ConditionTypeApplied)
	return dep.Status.ObservedGeneration == gen &&
		rendered != nil &&
		rendered.Status == metav1.ConditionTrue &&
		rendered.ObservedGeneration == gen &&
		applied != nil &&
		applied.Status == metav1.ConditionTrue &&
		applied.ObservedGeneration == gen
}
