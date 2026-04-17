package policyresolver

import (
	"context"
	"fmt"

	"k8s.io/client-go/kubernetes"

	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// Augmentor bridges the resolver to the `PolicyAugmentor` interface expected
// by console/templates.AncestorTemplateResolver. It exists so console/templates
// does not have to import console/policyresolver (which would create a cycle
// if the resolver ever wanted to inspect Template ConfigMaps directly).
//
// The Augmentor also helpfully extracts the project name from a project
// namespace string using the attached resolver, so the render call sites can
// pass the namespace they already have without re-deriving the logical name.
type Augmentor struct {
	// Resolver runs the REQUIRE/EXCLUDE walk.
	Resolver *Resolver
}

// NewAugmentor returns an Augmentor backed by the given policy Resolver.
func NewAugmentor(r *Resolver) *Augmentor {
	return &Augmentor{Resolver: r}
}

// AugmentForDeployment expands baseLinkedRefs with REQUIRE/EXCLUDE output
// scoped to the deployment target. `deploymentName` may be empty for
// callers that don't know the target name yet (e.g. pre-save render
// previews); in that case the resolver treats deployment_pattern as a
// "matches any deployment" wildcard, matching the pre-policy behavior.
func (a *Augmentor) AugmentForDeployment(ctx context.Context, projectNs, deploymentName string, baseLinkedRefs []*consolev1.LinkedTemplateRef) ([]*consolev1.LinkedTemplateRef, error) {
	if a == nil || a.Resolver == nil {
		return baseLinkedRefs, nil
	}
	project, err := projectFromNamespace(a.Resolver.Resolver, projectNs)
	if err != nil {
		return nil, err
	}
	return a.Resolver.Resolve(
		ctx,
		consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		project,
		TargetKindDeployment,
		deploymentName,
		baseLinkedRefs,
	)
}

// AugmentForProjectTemplate expands baseLinkedRefs with REQUIRE/EXCLUDE
// output scoped to a project-template target. `templateName` is the project
// template's DNS label slug.
func (a *Augmentor) AugmentForProjectTemplate(ctx context.Context, projectNs, templateName string, baseLinkedRefs []*consolev1.LinkedTemplateRef) ([]*consolev1.LinkedTemplateRef, error) {
	if a == nil || a.Resolver == nil {
		return baseLinkedRefs, nil
	}
	project, err := projectFromNamespace(a.Resolver.Resolver, projectNs)
	if err != nil {
		return nil, err
	}
	return a.Resolver.Resolve(
		ctx,
		consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		project,
		TargetKindProjectTemplate,
		templateName,
		baseLinkedRefs,
	)
}

// projectFromNamespace extracts the project name from a project namespace.
// Returns an error when the namespace is not classified as project by the
// resolver — a signal that the caller is asking the augmentor to run against
// something it cannot reason about.
func projectFromNamespace(r *resolver.Resolver, projectNs string) (string, error) {
	kind, name, err := r.ResourceTypeFromNamespace(projectNs)
	if err != nil {
		return "", fmt.Errorf("policyresolver.Augmentor: classifying namespace %q: %w", projectNs, err)
	}
	if kind != "project" {
		return "", fmt.Errorf("policyresolver.Augmentor: namespace %q is not a project namespace (kind=%q)", projectNs, kind)
	}
	return name, nil
}

// DeploymentPolicyStateProvider satisfies deployments.PolicyStateProvider by
// pairing a Resolver (current-set computation) with the folder-namespace
// drift-store helpers (applied-set read/write).
//
// Wiring a single provider instance keeps all three methods in sync —
// CurrentRenderSet, AppliedRenderSet, and RecordAppliedRenderSet share the
// same resolver, walker, and ResourceType classifier so a mismatch between
// "where drift state is stored" and "where policies are read" cannot arise.
type DeploymentPolicyStateProvider struct {
	// Resolver runs the REQUIRE/EXCLUDE walk for CurrentRenderSet.
	Resolver *Resolver
	// Client writes and reads the folder-namespace drift store. Typically
	// the same clientset passed to Resolver; exposed separately so tests can
	// substitute a fake without reaching through the resolver.
	Client kubernetes.Interface
}

// NewDeploymentPolicyStateProvider returns a provider bound to the given
// resolver and client.
func NewDeploymentPolicyStateProvider(r *Resolver, client kubernetes.Interface) *DeploymentPolicyStateProvider {
	return &DeploymentPolicyStateProvider{Resolver: r, Client: client}
}

// CurrentRenderSet evaluates the policy resolver for a deployment target.
func (p *DeploymentPolicyStateProvider) CurrentRenderSet(ctx context.Context, project, deploymentName string, baseLinkedRefs []*consolev1.LinkedTemplateRef) ([]*consolev1.LinkedTemplateRef, error) {
	if p == nil || p.Resolver == nil {
		return baseLinkedRefs, nil
	}
	return p.Resolver.Resolve(
		ctx,
		consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		project,
		TargetKindDeployment,
		deploymentName,
		baseLinkedRefs,
	)
}

// AppliedRenderSet reads the last-applied render set from the
// folder-namespace drift store.
func (p *DeploymentPolicyStateProvider) AppliedRenderSet(ctx context.Context, project, deploymentName string) ([]*consolev1.LinkedTemplateRef, error) {
	if p == nil || p.Resolver == nil {
		return nil, nil
	}
	return ReadAppliedRenderSet(ctx, p.Client, p.Resolver.Walker, p.Resolver.Resolver, project, TargetKindDeployment, deploymentName)
}

// RecordAppliedRenderSet writes the last-applied render set to the
// folder-namespace drift store.
func (p *DeploymentPolicyStateProvider) RecordAppliedRenderSet(ctx context.Context, project, deploymentName string, refs []*consolev1.LinkedTemplateRef) error {
	if p == nil || p.Resolver == nil {
		return nil
	}
	return RecordAppliedRenderSet(ctx, p.Client, p.Resolver.Walker, p.Resolver.Resolver, project, TargetKindDeployment, deploymentName, refs)
}

// ProjectTemplatePolicyStateProvider satisfies the template handler's
// PolicyStateProvider contract for project-scope templates. Structurally
// identical to DeploymentPolicyStateProvider but with a
// TargetKindProjectTemplate kind so REQUIRE/EXCLUDE rules evaluate against
// the template target instead of the deployment target.
type ProjectTemplatePolicyStateProvider struct {
	Resolver *Resolver
	Client   kubernetes.Interface
}

// NewProjectTemplatePolicyStateProvider returns a provider bound to the
// given resolver and client.
func NewProjectTemplatePolicyStateProvider(r *Resolver, client kubernetes.Interface) *ProjectTemplatePolicyStateProvider {
	return &ProjectTemplatePolicyStateProvider{Resolver: r, Client: client}
}

// CurrentRenderSet evaluates the policy resolver for a project-template target.
func (p *ProjectTemplatePolicyStateProvider) CurrentRenderSet(ctx context.Context, project, templateName string, baseLinkedRefs []*consolev1.LinkedTemplateRef) ([]*consolev1.LinkedTemplateRef, error) {
	if p == nil || p.Resolver == nil {
		return baseLinkedRefs, nil
	}
	return p.Resolver.Resolve(
		ctx,
		consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		project,
		TargetKindProjectTemplate,
		templateName,
		baseLinkedRefs,
	)
}

// AppliedRenderSet reads the last-applied render set for a project-template
// target from the folder-namespace drift store.
func (p *ProjectTemplatePolicyStateProvider) AppliedRenderSet(ctx context.Context, project, templateName string) ([]*consolev1.LinkedTemplateRef, error) {
	if p == nil || p.Resolver == nil {
		return nil, nil
	}
	return ReadAppliedRenderSet(ctx, p.Client, p.Resolver.Walker, p.Resolver.Resolver, project, TargetKindProjectTemplate, templateName)
}

// RecordAppliedRenderSet writes the last-applied render set for a
// project-template target.
func (p *ProjectTemplatePolicyStateProvider) RecordAppliedRenderSet(ctx context.Context, project, templateName string, refs []*consolev1.LinkedTemplateRef) error {
	if p == nil || p.Resolver == nil {
		return nil
	}
	return RecordAppliedRenderSet(ctx, p.Client, p.Resolver.Walker, p.Resolver.Resolver, project, TargetKindProjectTemplate, templateName, refs)
}
