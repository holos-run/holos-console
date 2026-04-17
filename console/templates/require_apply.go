package templates

import (
	"context"
	"fmt"
	"log/slog"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/deployments"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// ResourceApplier applies rendered Kubernetes resources to the cluster using
// each resource's own namespace.
//
// ApplyRequiredTemplate stamps a *required-template* ownership identity
// (project + required-template label, NOT project + deployment) so that a
// future deployment whose name matches a required-template name cannot
// adopt or delete required-template resources via the deployment
// reconcile/cleanup label selector (HOL-571 review round 1). Production
// wires deployments.Applier here, which keeps deployment-owned resources
// and required-template-owned resources in disjoint label namespaces.
type ResourceApplier interface {
	ApplyRequiredTemplate(ctx context.Context, project, templateName string, resources []unstructured.Unstructured) error
}

// RequireRuleMatch describes a single template that a TemplatePolicy REQUIRE
// rule selected for application to a newly created project. The resolver is
// introduced for real in Phase 5 (HOL-567) — for Phase 3 the concrete
// RequireRuleResolver implementation returns an empty slice unconditionally
// because no REQUIRE rules exist in production yet.
type RequireRuleMatch struct {
	// Scope is the scope of the template selected by the rule.
	Scope consolev1.TemplateScope
	// ScopeName is the org or folder name that owns the template.
	ScopeName string
	// TemplateName is the name of the template ConfigMap to render.
	TemplateName string
	// VersionConstraint carries the policy-author-declared semver
	// constraint (if any) so applyMatch can pin the required template
	// to the rule's version band at render time. Empty string means "use
	// the live template source" — identical to the behavior of a
	// LinkedTemplateRef with no version_constraint.
	VersionConstraint string
}

// RequireRuleResolver evaluates TemplatePolicy REQUIRE rules for a project at
// project-creation time and returns the templates that must be applied to the
// new project namespace. Phase 5 swaps the empty stub for a real resolver
// backed by the TemplatePolicyService.
type RequireRuleResolver interface {
	ResolveRequiredTemplates(ctx context.Context, org, project string) ([]RequireRuleMatch, error)
}

// emptyRequireRuleResolver always returns an empty slice of matches. It is the
// Phase 3 placeholder implementation — Phase 5 replaces it with a real
// resolver that queries TemplatePolicy ConfigMaps. The code path downstream of
// the resolver is exercised by tests that inject a non-empty stub.
type emptyRequireRuleResolver struct{}

// ResolveRequiredTemplates always returns nil, nil — no REQUIRE rules in
// production until Phase 5 lands.
func (emptyRequireRuleResolver) ResolveRequiredTemplates(_ context.Context, _, _ string) ([]RequireRuleMatch, error) {
	return nil, nil
}

// NewEmptyRequireRuleResolver returns a RequireRuleResolver that matches no
// templates. Wire this at server-startup time until Phase 5 (HOL-567)
// introduces the real resolver.
func NewEmptyRequireRuleResolver() RequireRuleResolver {
	return emptyRequireRuleResolver{}
}

// RequiredTemplateApplier renders and applies the templates selected by
// TemplatePolicy REQUIRE rules to a newly created project namespace. It
// satisfies the projects.RequiredTemplateApplier interface.
//
// The applier walks the project's ancestor chain to collect template sources
// (identical to the ancestor-template resolution path used during deployment
// renders, see ListEffectiveTemplateSources) and then runs the unified
// CueRenderer.Render + ResourceApplier.Apply path on a synthetic empty CUE
// source. This matches ADR 021 Decision 3 and replaces the v1alpha1-era
// parallel render path deleted in HOL-565.
type RequiredTemplateApplier struct {
	k8s            *K8sClient
	walker         RenderHierarchyWalker
	renderer       *deployments.CueRenderer
	applier        ResourceApplier
	resolver       RequireRuleResolver
	policyResolver policyresolver.PolicyResolver
}

// NewRequiredTemplateApplier creates a RequiredTemplateApplier. resolver must
// not be nil — use NewEmptyRequireRuleResolver for the Phase 3 placeholder.
// policyResolver is the HOL-566 Phase 4 TemplatePolicy resolution seam;
// callers should pass policyresolver.NewNoopResolver() until Phase 5 wires
// a real implementation.
func NewRequiredTemplateApplier(
	k8s *K8sClient,
	walker RenderHierarchyWalker,
	renderer *deployments.CueRenderer,
	applier ResourceApplier,
	resolver RequireRuleResolver,
	policyResolver policyresolver.PolicyResolver,
) *RequiredTemplateApplier {
	return &RequiredTemplateApplier{
		k8s:            k8s,
		walker:         walker,
		renderer:       renderer,
		applier:        applier,
		resolver:       resolver,
		policyResolver: policyResolver,
	}
}

// ApplyRequiredTemplates evaluates REQUIRE rules for the newly created project
// and renders each matched template into the project namespace via the
// unified CueRenderer.Render + ResourceApplier.Apply path.
//
// The synthetic empty-source render base is an empty CUE document; ancestor
// sources plus the matched template are unified on top through
// ListEffectiveTemplateSources.
func (a *RequiredTemplateApplier) ApplyRequiredTemplates(ctx context.Context, org, project, projectNamespace string, claims *rpc.Claims) error {
	if a.resolver == nil {
		return nil
	}

	matches, err := a.resolver.ResolveRequiredTemplates(ctx, org, project)
	if err != nil {
		return fmt.Errorf("resolving required templates for project %q: %w", project, err)
	}
	if len(matches) == 0 {
		return nil
	}

	if a.applier == nil {
		slog.WarnContext(ctx, "required template resolver matched templates but no resource applier is configured; skipping",
			slog.String("project", project),
			slog.Int("matches", len(matches)),
		)
		return nil
	}

	projectNs := a.k8s.Resolver.ProjectNamespace(project)
	platformInput := v1alpha2.PlatformInput{
		Project:          project,
		Namespace:        projectNamespace,
		Organization:     org,
		GatewayNamespace: deployments.DefaultGatewayNamespace,
	}
	// Populate PlatformInput.Folders from the project's ancestor chain so
	// folder/org-scope templates that reference `platform.folders` render
	// correctly. The deployment render path does the same thing via
	// AncestorWalker.GetProjectFolders; at project-creation time we have
	// the RenderHierarchyWalker already wired for ancestor-template
	// resolution, so walk with it and extract folder-kind ancestors.
	// Failures here log a warning but do not abort the create — a missing
	// Folders value may render the wrong manifests for a template that
	// relies on folder ancestry, but refusing to create the project is
	// worse when the ancestor chain is otherwise intact. Templates that
	// require folder ancestry can enforce presence via CUE constraints.
	if a.walker != nil {
		ancestors, walkErr := a.walker.WalkAncestors(ctx, projectNs)
		if walkErr != nil {
			slog.WarnContext(ctx, "could not resolve folder ancestry for required-template platform input",
				slog.String("project", project),
				slog.Any("error", walkErr),
			)
		} else {
			// ancestors is child→parent order (project first, org last).
			// Reverse so PlatformInput.Folders is org→project (matches the
			// contract documented on v1alpha2.PlatformInput.Folders).
			var folders []v1alpha2.FolderInfo
			for i := len(ancestors) - 1; i >= 0; i-- {
				ns := ancestors[i]
				if ns == nil {
					continue
				}
				kind, name, err := a.k8s.Resolver.ResourceTypeFromNamespace(ns.Name)
				if err != nil {
					continue
				}
				if kind == v1alpha2.ResourceTypeFolder {
					folders = append(folders, v1alpha2.FolderInfo{Name: name})
				}
			}
			platformInput.Folders = folders
		}
	}
	if claims != nil {
		platformInput.Claims = v1alpha2.Claims{
			Iss:           claims.Iss,
			Sub:           claims.Sub,
			Exp:           claims.Exp,
			Iat:           claims.Iat,
			Email:         claims.Email,
			EmailVerified: claims.EmailVerified,
			Name:          claims.Name,
			Groups:        claims.Roles,
		}
	}

	for _, m := range matches {
		if err := a.applyMatch(ctx, m, project, projectNs, platformInput); err != nil {
			return err
		}
	}
	return nil
}

// applyMatch renders a single REQUIRE-rule match and applies the resulting
// resources. Errors are wrapped with the template identifier so the caller can
// roll back the project-creation transaction with a useful message.
func (a *RequiredTemplateApplier) applyMatch(
	ctx context.Context,
	match RequireRuleMatch,
	project, projectNs string,
	platformInput v1alpha2.PlatformInput,
) error {
	templateRef := &consolev1.LinkedTemplateRef{
		Scope:             match.Scope,
		ScopeName:         match.ScopeName,
		Name:              match.TemplateName,
		VersionConstraint: match.VersionConstraint,
	}

	ancestorSources, effectiveRefs, err := a.k8s.ListEffectiveTemplateSources(
		ctx,
		projectNs,
		TargetKindProjectTemplate,
		match.TemplateName,
		[]*consolev1.LinkedTemplateRef{templateRef},
		a.walker,
		a.policyResolver,
	)
	if err != nil {
		return fmt.Errorf("listing ancestor sources for required template %q (%s/%s): %w",
			match.TemplateName, match.Scope, match.ScopeName, err)
	}
	// HOL-571 review round 3 P1: fail closed when ancestor lookup
	// degrades to the nil-source signal. ListEffectiveTemplateSources
	// returns (nil, nil, nil) on walker failure or when the walker is
	// not configured — both cases mean "we could not confirm the
	// required-template source unified into the render". Project
	// creation must refuse rather than apply an empty manifest, because
	// a policy-REQUIRE'd template that silently renders empty defeats
	// the enforcement boundary this path exists for.
	if ancestorSources == nil && effectiveRefs == nil {
		return fmt.Errorf("required template %q (%s/%s) source lookup returned empty; refusing to create project %q without policy-required manifests",
			match.TemplateName, match.Scope, match.ScopeName, project)
	}

	grouped, err := a.renderer.Render(ctx, "", ancestorSources, deployments.RenderInputs{
		Platform:              platformInput,
		Project:               v1alpha2.ProjectInput{},
		ReadPlatformResources: true,
	})
	if err != nil {
		return fmt.Errorf("rendering required template %q (%s/%s) for project %q: %w",
			match.TemplateName, match.Scope, match.ScopeName, project, err)
	}

	resources := make([]unstructured.Unstructured, 0, len(grouped.Platform)+len(grouped.Project))
	resources = append(resources, grouped.Platform...)
	resources = append(resources, grouped.Project...)

	if err := a.applier.ApplyRequiredTemplate(ctx, project, match.TemplateName, resources); err != nil {
		return fmt.Errorf("applying required template %q (%s/%s) to project %q: %w",
			match.TemplateName, match.Scope, match.ScopeName, project, err)
	}

	slog.InfoContext(ctx, "required template applied",
		slog.String("template", match.TemplateName),
		slog.String("scope", match.Scope.String()),
		slog.String("scopeName", match.ScopeName),
		slog.String("project", project),
		slog.Int("resources", len(resources)),
	)
	return nil
}
