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

// Package policyresolver — render_state_deps.go.
//
// This file contains the helper that computes the RenderStateDependency slice
// recorded on a RenderState at write time (HOL-961). The helper lists every
// TemplateDependency in the project namespace and every TemplateRequirement in
// the owning folder/organization namespace, filters to those that apply to the
// named render target, and builds the typed dependency edges that populate
// RenderStateSpec.Dependencies.
//
// # Why collection happens at write time
//
// TemplateDependency (Phase 5) and TemplateRequirement (Phase 6) reconcilers
// produce singleton Deployments asynchronously. The render handlers (Create/
// UpdateDeployment) call RecordAppliedRenderSet after a successful apply, at
// which point the singleton Deployments already exist in the project namespace.
// Collecting the originating CRD objects at that moment gives a consistent
// snapshot: whatever TemplateDependency/TemplateRequirement objects are active
// in the cluster at the time the render was applied.
//
// # Filtering
//
// For TargetKindDeployment, the function first fetches the Deployment object to
// recover its TemplateRef (namespace, name). TemplateDependency objects are then
// filtered to those whose Dependent matches that TemplateRef; TemplateRequirement
// objects are filtered to those whose TargetRefs apply to the (project,
// deploymentName) pair via BindingAppliesToDeployment.
//
// For TargetKindProjectTemplate, TemplateDependency objects are filtered by
// matching the Dependent ref against the project namespace + template name.
// TemplateRequirement matching uses the same binding matcher with
// TargetKindProjectTemplate.
//
// # Fail-open
//
// A failure to list TemplateDependency or TemplateRequirement objects is treated
// as non-fatal: the function logs at warn level and returns an empty slice so
// the RenderState write still succeeds with an empty Dependencies field. An
// empty Dependencies field is distinct from no RenderState at all; the drift
// checker will correctly report drift if a subsequent render produces edges.
package policyresolver

import (
	"context"
	"log/slog"

	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// collectDependencies returns the RenderStateDependency slice for the named
// render target by listing the TemplateDependency and TemplateRequirement
// objects active at write time. projectNs is the project namespace, folderNs
// is the owning folder/organization namespace (already computed by the caller),
// and project is the plain project slug (e.g. "myproj") used for TargetRef
// matching.
//
// Errors are logged but not returned; the function is fail-open to avoid
// blocking a successful render apply on a dependency-collection failure.
func collectDependencies(
	ctx context.Context,
	c ctrlclient.Client,
	projectNs, folderNs, project string,
	targetKind TargetKind,
	targetName string,
) []templatesv1alpha1.RenderStateDependency {
	if c == nil {
		return nil
	}

	var deps []templatesv1alpha1.RenderStateDependency

	// --- Phase 5: TemplateDependency objects in the project namespace ----
	templateDeps, err := listTemplateDependencies(ctx, c, projectNs, targetKind, targetName)
	if err != nil {
		slog.WarnContext(ctx, "failed to list TemplateDependency objects; dependency edges will be empty",
			slog.String("namespace", projectNs),
			slog.Int("targetKind", int(targetKind)),
			slog.String("targetName", targetName),
			slog.Any("error", err),
		)
	}
	deps = append(deps, templateDeps...)

	// --- Phase 6: TemplateRequirement objects in the folder/org namespace ---
	templateReqs, err := listTemplateRequirements(ctx, c, folderNs, project, targetKind, targetName)
	if err != nil {
		slog.WarnContext(ctx, "failed to list TemplateRequirement objects; some dependency edges may be missing",
			slog.String("namespace", folderNs),
			slog.Int("targetKind", int(targetKind)),
			slog.String("targetName", targetName),
			slog.Any("error", err),
		)
	}
	deps = append(deps, templateReqs...)

	if len(deps) == 0 {
		return nil
	}
	return deps
}

// listTemplateDependencies collects RenderStateDependency entries from
// TemplateDependency objects stored in the project namespace. Each
// TemplateDependency encodes a (Dependent → Requires) edge; only those where
// the Dependent ref matches the render target's template ref are included.
func listTemplateDependencies(
	ctx context.Context,
	c ctrlclient.Client,
	projectNs string,
	targetKind TargetKind,
	targetName string,
) ([]templatesv1alpha1.RenderStateDependency, error) {
	// Resolve the template ref for the target so we can filter TemplateDependency
	// objects to those where Dependent matches this target's template.
	dependentRef, err := templateRefForTarget(ctx, c, projectNs, targetKind, targetName)
	if err != nil {
		// Target may not exist (race between render and reconcile). Return
		// empty rather than an error so the write still succeeds.
		slog.WarnContext(ctx, "could not resolve template ref for render target; TemplateDependency edges will be empty",
			slog.String("namespace", projectNs),
			slog.Int("targetKind", int(targetKind)),
			slog.String("targetName", targetName),
			slog.Any("error", err),
		)
		return nil, nil
	}

	var list templatesv1alpha1.TemplateDependencyList
	if err := c.List(ctx, &list, ctrlclient.InNamespace(projectNs)); err != nil {
		return nil, err
	}

	var out []templatesv1alpha1.RenderStateDependency
	for i := range list.Items {
		td := &list.Items[i]
		dep := td.Spec.Dependent
		// Filter to TemplateDependency objects whose Dependent matches the
		// render target's template ref.
		if dep.Namespace != dependentRef.Namespace || dep.Name != dependentRef.Name {
			continue
		}
		out = append(out, templatesv1alpha1.RenderStateDependency{
			Template: td.Spec.Requires,
			Source:   templatesv1alpha1.RenderStateDependencySourceTemplateDependency,
			OriginatingObject: templatesv1alpha1.RenderStateDependencyOriginatingRef{
				Namespace: td.Namespace,
				Name:      td.Name,
				Kind:      templatesv1alpha1.RenderStateDependencySourceTemplateDependency,
			},
		})
	}
	return out, nil
}

// listTemplateRequirements collects RenderStateDependency entries from
// TemplateRequirement objects stored in the folder or organization namespace.
// Only those whose TargetRefs apply to the named render target (project +
// targetName) are included.
func listTemplateRequirements(
	ctx context.Context,
	c ctrlclient.Client,
	folderNs, project string,
	targetKind TargetKind,
	targetName string,
) ([]templatesv1alpha1.RenderStateDependency, error) {
	var list templatesv1alpha1.TemplateRequirementList
	if err := c.List(ctx, &list, ctrlclient.InNamespace(folderNs)); err != nil {
		return nil, err
	}

	var out []templatesv1alpha1.RenderStateDependency
	for i := range list.Items {
		treq := &list.Items[i]
		resolved := RequirementTargetRefToResolved(treq.Namespace, treq.Name, treq.Spec.TargetRefs)
		var matches bool
		switch targetKind {
		case TargetKindDeployment:
			matches = BindingAppliesToDeployment(resolved, project, targetName)
		case TargetKindProjectTemplate:
			matches = bindingAppliesTo(resolved, project, TargetKindProjectTemplate, targetName)
		}
		if !matches {
			continue
		}
		out = append(out, templatesv1alpha1.RenderStateDependency{
			Template: treq.Spec.Requires,
			Source:   templatesv1alpha1.RenderStateDependencySourceTemplateRequirement,
			OriginatingObject: templatesv1alpha1.RenderStateDependencyOriginatingRef{
				Namespace: treq.Namespace,
				Name:      treq.Name,
				Kind:      templatesv1alpha1.RenderStateDependencySourceTemplateRequirement,
			},
		})
	}
	return out, nil
}

// templateRefForTarget fetches the render target object and returns its
// (namespace, name) template reference. For Deployments, the ref is read from
// spec.templateRef. For ProjectTemplates, the template itself is the target: the
// ref is (projectNs, targetName) so TemplateDependency filtering uses the same
// (namespace, name) comparison that works for Deployments.
func templateRefForTarget(
	ctx context.Context,
	c ctrlclient.Client,
	projectNs string,
	targetKind TargetKind,
	targetName string,
) (templatesv1alpha1.LinkedTemplateRef, error) {
	switch targetKind {
	case TargetKindDeployment:
		var dep deploymentsv1alpha1.Deployment
		if err := c.Get(ctx, types.NamespacedName{Namespace: projectNs, Name: targetName}, &dep); err != nil {
			return templatesv1alpha1.LinkedTemplateRef{}, err
		}
		return templatesv1alpha1.LinkedTemplateRef{
			Namespace:         dep.Spec.TemplateRef.Namespace,
			Name:              dep.Spec.TemplateRef.Name,
			VersionConstraint: dep.Spec.VersionConstraint,
		}, nil
	default:
		// For ProjectTemplate, the Dependent field in a TemplateDependency that
		// targets a project-scope template is (projectNs, templateName). This
		// mirrors how the reconciler expects to find matching objects.
		return templatesv1alpha1.LinkedTemplateRef{
			Namespace: projectNs,
			Name:      targetName,
		}, nil
	}
}
