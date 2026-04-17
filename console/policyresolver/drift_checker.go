package policyresolver

import (
	"context"
	"fmt"

	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// DriftChecker is a small adapter that combines a PolicyResolver with an
// AppliedRenderStateClient to serve the policy-drift surfaces introduced in
// HOL-567:
//
//   - DeploymentStatusSummary.policy_drift (cheap bool on the list view)
//   - GetDeploymentPolicyState (full diff snapshot)
//   - GetProjectTemplatePolicyState (full diff snapshot, project-scope
//     templates)
//   - The Create/Update write-through that persists the resolved render set
//     to the folder-namespace drift store on successful renders
//
// Because the deployments and templates handler packages define their own
// narrowly-scoped interfaces (PolicyDriftChecker and
// ProjectTemplateDriftChecker) to avoid importing this package and creating
// cycles, DriftChecker's methods are named to match both interfaces where the
// semantics coincide and dispatched on TargetKind where they diverge.
type DriftChecker struct {
	Resolver  PolicyResolver
	State     *AppliedRenderStateClient
	NsResolver *resolver.Resolver
}

// NewDriftChecker wires a DriftChecker against a real resolver and applied-
// state client. Callers pass this to `*deployments.Handler.WithPolicyDrift
// Checker` as a DeploymentDriftAdapter and to `*templates.Handler.With
// ProjectTemplateDriftChecker` as a ProjectTemplateDriftAdapter.
func NewDriftChecker(resolver PolicyResolver, state *AppliedRenderStateClient, r *resolver.Resolver) *DriftChecker {
	return &DriftChecker{Resolver: resolver, State: state, NsResolver: r}
}

// DeploymentDriftAdapter wraps a DriftChecker and projects it onto the shape
// the deployments handler expects. Returning a separate value from an adapter
// function keeps the handler package free of references to this package.
type DeploymentDriftAdapter struct{ inner *DriftChecker }

// NewDeploymentDriftAdapter returns an adapter suitable for
// `*deployments.Handler.WithPolicyDriftChecker`.
func NewDeploymentDriftAdapter(d *DriftChecker) *DeploymentDriftAdapter {
	return &DeploymentDriftAdapter{inner: d}
}

// Drift reports whether the currently resolved render set for the deployment
// differs from the applied set stored in the folder namespace. Returns
// (drift, hasAppliedState, error).
//
// On any error (applied-set read or resolver failure) the returned drift and
// hasAppliedState are both false; hasAppliedState is only true when the full
// diff completed successfully. This matches PolicyState's error contract
// (nil, err) so callers can switch between the two methods without tracking
// different partial-result semantics.
func (a *DeploymentDriftAdapter) Drift(ctx context.Context, project, deploymentName string, explicitRefs []*consolev1.LinkedTemplateRef) (bool, bool, error) {
	if a == nil || a.inner == nil {
		return false, false, nil
	}
	projectNs := a.inner.NsResolver.ProjectNamespace(project)
	applied, ok, err := a.inner.State.ReadAppliedRenderSet(ctx, projectNs, TargetKindDeployment, deploymentName)
	if err != nil {
		return false, false, err
	}
	if !ok {
		return false, false, nil
	}
	current, resolveErr := a.inner.Resolver.Resolve(ctx, projectNs, TargetKindDeployment, deploymentName, explicitRefs)
	if resolveErr != nil {
		return false, false, fmt.Errorf("resolving current render set: %w", resolveErr)
	}
	_, _, drifted := DiffRenderSets(applied, current)
	return drifted, true, nil
}

// PolicyState returns the full PolicyState snapshot for the deployment.
func (a *DeploymentDriftAdapter) PolicyState(ctx context.Context, project, deploymentName string, explicitRefs []*consolev1.LinkedTemplateRef) (*consolev1.PolicyState, error) {
	if a == nil || a.inner == nil {
		return &consolev1.PolicyState{}, nil
	}
	projectNs := a.inner.NsResolver.ProjectNamespace(project)
	applied, hasApplied, err := a.inner.State.ReadAppliedRenderSet(ctx, projectNs, TargetKindDeployment, deploymentName)
	if err != nil {
		return nil, fmt.Errorf("reading applied render set: %w", err)
	}
	current, err := a.inner.Resolver.Resolve(ctx, projectNs, TargetKindDeployment, deploymentName, explicitRefs)
	if err != nil {
		return nil, fmt.Errorf("resolving current render set: %w", err)
	}
	added, removed, drift := DiffRenderSets(applied, current)
	return &consolev1.PolicyState{
		AppliedSet:      applied,
		CurrentSet:     current,
		AddedRefs:      added,
		RemovedRefs:    removed,
		Drift:          drift,
		HasAppliedState: hasApplied,
	}, nil
}

// RecordApplied persists the effective render set for the deployment. Called
// from the deployments handler on successful Create/Update.
func (a *DeploymentDriftAdapter) RecordApplied(ctx context.Context, project, deploymentName string, refs []*consolev1.LinkedTemplateRef) error {
	if a == nil || a.inner == nil {
		return nil
	}
	projectNs := a.inner.NsResolver.ProjectNamespace(project)
	return a.inner.State.RecordAppliedRenderSet(ctx, projectNs, TargetKindDeployment, deploymentName, refs)
}

// ProjectTemplateDriftAdapter wraps a DriftChecker for the templates handler.
type ProjectTemplateDriftAdapter struct{ inner *DriftChecker }

// NewProjectTemplateDriftAdapter returns an adapter suitable for
// `*templates.Handler.WithProjectTemplateDriftChecker`.
func NewProjectTemplateDriftAdapter(d *DriftChecker) *ProjectTemplateDriftAdapter {
	return &ProjectTemplateDriftAdapter{inner: d}
}

// PolicyState returns the full PolicyState snapshot for a project-scope
// template.
func (a *ProjectTemplateDriftAdapter) PolicyState(ctx context.Context, project, templateName string, explicitRefs []*consolev1.LinkedTemplateRef) (*consolev1.PolicyState, error) {
	if a == nil || a.inner == nil {
		return &consolev1.PolicyState{}, nil
	}
	projectNs := a.inner.NsResolver.ProjectNamespace(project)
	applied, hasApplied, err := a.inner.State.ReadAppliedRenderSet(ctx, projectNs, TargetKindProjectTemplate, templateName)
	if err != nil {
		return nil, fmt.Errorf("reading applied render set: %w", err)
	}
	current, err := a.inner.Resolver.Resolve(ctx, projectNs, TargetKindProjectTemplate, templateName, explicitRefs)
	if err != nil {
		return nil, fmt.Errorf("resolving current render set: %w", err)
	}
	added, removed, drift := DiffRenderSets(applied, current)
	return &consolev1.PolicyState{
		AppliedSet:      applied,
		CurrentSet:     current,
		AddedRefs:      added,
		RemovedRefs:    removed,
		Drift:          drift,
		HasAppliedState: hasApplied,
	}, nil
}

// RecordApplied persists the effective render set for a project-scope
// template. Called from the templates handler on successful
// CreateTemplate/UpdateTemplate at project scope.
func (a *ProjectTemplateDriftAdapter) RecordApplied(ctx context.Context, project, templateName string, refs []*consolev1.LinkedTemplateRef) error {
	if a == nil || a.inner == nil {
		return nil
	}
	projectNs := a.inner.NsResolver.ProjectNamespace(project)
	return a.inner.State.RecordAppliedRenderSet(ctx, projectNs, TargetKindProjectTemplate, templateName, refs)
}
