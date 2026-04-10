package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/deployments"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	corev1 "k8s.io/api/core/v1"
)

// ResourceApplier applies K8s resources to a namespace.
type ResourceApplier interface {
	Apply(ctx context.Context, namespace, deploymentName string, resources []unstructured.Unstructured) error
}

// HierarchyWalker walks the namespace hierarchy for the mandatory template applier.
type HierarchyWalker interface {
	WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error)
}

// MandatoryTemplateApplier renders and applies all mandatory+enabled templates
// from the project's ancestor chain (org + folders) to the project namespace.
// This replaces org_templates.MandatoryTemplateApplier from v1alpha1 and
// extends it to walk the full ancestor chain (ADR 021 Decision 3).
type MandatoryTemplateApplier struct {
	k8s      *K8sClient
	walker   HierarchyWalker
	renderer *deployments.CueRenderer
	applier  ResourceApplier
}

// NewMandatoryTemplateApplier creates a MandatoryTemplateApplier.
func NewMandatoryTemplateApplier(k8s *K8sClient, walker HierarchyWalker, renderer *deployments.CueRenderer, applier ResourceApplier) *MandatoryTemplateApplier {
	return &MandatoryTemplateApplier{k8s: k8s, walker: walker, renderer: renderer, applier: applier}
}

// ApplyMandatoryOrgTemplates satisfies the projects.MandatoryTemplateApplier
// interface. It walks the project's ancestor chain (org + folder ancestors) and
// applies all mandatory+enabled templates to the project namespace.
//
// If any template render or apply fails, an error is returned describing which
// template failed. The caller (CreateProject) is responsible for cleanup.
func (a *MandatoryTemplateApplier) ApplyMandatoryOrgTemplates(ctx context.Context, org, project, projectNamespace string, claims *rpc.Claims) error {
	// Walk the ancestor chain starting from the project namespace to collect
	// all mandatory+enabled templates from org and folder ancestors.
	projectNs := a.k8s.Resolver.ProjectNamespace(project)

	var ancestorNSes []*corev1.Namespace
	if a.walker != nil {
		ancestors, err := a.walker.WalkAncestors(ctx, projectNs)
		if err != nil {
			slog.WarnContext(ctx, "failed to walk ancestor chain for mandatory templates, falling back to org-only",
				slog.String("project", project),
				slog.String("namespace", projectNs),
				slog.Any("error", err),
			)
			// Graceful degradation: fall back to org-only.
			orgNs := a.k8s.Resolver.OrgNamespace(org)
			ancestors = []*corev1.Namespace{{}}
			ancestors[0].Name = orgNs
		}
		// Exclude the project namespace itself (ancestors[0]) — we only apply
		// templates from ancestor scopes.
		if len(ancestors) > 1 {
			ancestorNSes = ancestors[1:]
		} else {
			// Only the project namespace in the chain: nothing to apply.
			return nil
		}
	} else {
		// No walker: apply org-level only (backward compatible behavior).
		orgNs := a.k8s.Resolver.OrgNamespace(org)
		orgNsObj := &corev1.Namespace{}
		orgNsObj.Name = orgNs
		ancestorNSes = []*corev1.Namespace{orgNsObj}
	}

	// Walk ancestors from closest (folder) to farthest (org) and collect
	// mandatory+enabled templates from each.
	for _, ns := range ancestorNSes {
		if err := a.applyMandatoryFromNamespace(ctx, ns.Name, project, projectNamespace, claims); err != nil {
			return err
		}
	}

	return nil
}

// applyMandatoryFromNamespace applies all mandatory+enabled templates from the
// given ancestor namespace to the project namespace.
func (a *MandatoryTemplateApplier) applyMandatoryFromNamespace(ctx context.Context, ancestorNs, project, projectNamespace string, claims *rpc.Claims) error {
	cms, err := a.k8s.ListTemplatesInNamespace(ctx, ancestorNs)
	if err != nil {
		// If the namespace doesn't exist or has no templates, treat as empty.
		slog.WarnContext(ctx, "failed to list templates in ancestor namespace, skipping",
			slog.String("namespace", ancestorNs),
			slog.Any("error", err),
		)
		return nil
	}

	for _, cm := range cms {
		mandatory, _ := strconv.ParseBool(cm.Annotations[v1alpha2.AnnotationMandatory])
		enabled, _ := strconv.ParseBool(cm.Annotations[v1alpha2.AnnotationEnabled])
		if !mandatory || !enabled {
			continue
		}

		cueSource := cm.Data[CueTemplateKey]
		if cueSource == "" {
			continue
		}

		slog.InfoContext(ctx, "applying mandatory template",
			slog.String("ancestorNs", ancestorNs),
			slog.String("template", cm.Name),
			slog.String("project", project),
			slog.String("projectNamespace", projectNamespace),
		)

		// Build PlatformInput for the render.
		platformInput := v1alpha1.PlatformInput{
			Project:          project,
			Namespace:        projectNamespace,
			GatewayNamespace: deployments.DefaultGatewayNamespace,
		}
		if claims != nil {
			platformInput.Claims = v1alpha1.Claims{
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

		userInput := mandatoryTemplateProjectInput{}

		platformJSON, err := json.Marshal(platformInput)
		if err != nil {
			return fmt.Errorf("encoding platform input for template %q in %q: %w", cm.Name, ancestorNs, err)
		}
		userJSON, err := json.Marshal(userInput)
		if err != nil {
			return fmt.Errorf("encoding user input for template %q in %q: %w", cm.Name, ancestorNs, err)
		}

		combinedCUE := fmt.Sprintf("platform: %s\ninput: %s", string(platformJSON), string(userJSON))

		resources, err := a.renderer.RenderWithCueInput(ctx, cueSource, combinedCUE)
		if err != nil {
			return fmt.Errorf("rendering mandatory template %q from %q for project %q: %w", cm.Name, ancestorNs, project, err)
		}

		if a.applier == nil {
			slog.WarnContext(ctx, "no resource applier configured, skipping mandatory template apply",
				slog.String("template", cm.Name),
				slog.String("project", project),
			)
			continue
		}

		// Use the template name as the "deployment name" for the ownership label.
		if err := a.applier.Apply(ctx, projectNamespace, cm.Name, resources); err != nil {
			return fmt.Errorf("applying mandatory template %q from %q to project %q: %w", cm.Name, ancestorNs, project, err)
		}

		slog.InfoContext(ctx, "mandatory template applied",
			slog.String("ancestorNs", ancestorNs),
			slog.String("template", cm.Name),
			slog.String("project", project),
			slog.String("projectNamespace", projectNamespace),
			slog.Int("resources", len(resources)),
		)
	}

	return nil
}

// mandatoryTemplateProjectInput carries the user-configurable input for mandatory
// templates applied at project creation time. The fields must match the CUE
// #Input struct field names in the template.
type mandatoryTemplateProjectInput struct{}

// OrgTemplateListerAdapter wraps K8sClient to satisfy the old OrgTemplateLister
// interface used by the pre-unified templates handler. Provides backward
// compatibility for wiring in console.go.
type OrgTemplateListerAdapter struct {
	k8s *K8sClient
}

// NewOrgTemplateListerAdapter creates an adapter.
func NewOrgTemplateListerAdapter(k8s *K8sClient) *OrgTemplateListerAdapter {
	return &OrgTemplateListerAdapter{k8s: k8s}
}

// ListLinkableOrgTemplateInfos returns enabled org-level templates as
// LinkableTemplate proto messages. Satisfies the old OrgTemplateLister interface.
func (a *OrgTemplateListerAdapter) ListLinkableOrgTemplateInfos(ctx context.Context, org string) ([]*consolev1.LinkableTemplate, error) {
	return a.k8s.ListLinkableTemplateInfos(ctx, consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, org)
}
