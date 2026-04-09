package org_templates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
	"github.com/holos-run/holos-console/console/deployments"
	"github.com/holos-run/holos-console/console/rpc"
)

// ResourceApplier applies K8s resources to a namespace.
type ResourceApplier interface {
	Apply(ctx context.Context, namespace, deploymentName string, resources []unstructured.Unstructured) error
}

// MandatoryTemplateApplier renders and applies all mandatory platform templates
// (code: SystemTemplate) for an org into a project namespace.
type MandatoryTemplateApplier struct {
	k8s      *K8sClient
	renderer *deployments.CueRenderer
	applier  ResourceApplier
}

// NewMandatoryTemplateApplier creates a MandatoryTemplateApplier.
func NewMandatoryTemplateApplier(k8s *K8sClient, renderer *deployments.CueRenderer, applier ResourceApplier) *MandatoryTemplateApplier {
	return &MandatoryTemplateApplier{k8s: k8s, renderer: renderer, applier: applier}
}

// ApplyMandatoryOrgTemplates lists all mandatory platform templates for the
// org, renders each one using PlatformInput derived from the project and caller
// claims, and applies the rendered resources to the project namespace.
//
// If any template render or apply fails, an error is returned describing which
// template failed. The caller (CreateProject) is responsible for cleanup.
func (a *MandatoryTemplateApplier) ApplyMandatoryOrgTemplates(ctx context.Context, org, project, projectNamespace string, claims *rpc.Claims) error {
	templates, err := a.k8s.ListOrgTemplates(ctx, org)
	if err != nil {
		return fmt.Errorf("listing platform templates for org %q: %w", org, err)
	}

	for _, cm := range templates {
		// Only apply templates that are both mandatory AND enabled.
		tmpl := configMapToOrgTemplate(&cm, org)
		if !tmpl.Mandatory || !tmpl.Enabled {
			continue
		}

		slog.InfoContext(ctx, "applying mandatory platform template",
			slog.String("org", org),
			slog.String("project", project),
			slog.String("namespace", projectNamespace),
			slog.String("template", tmpl.Name),
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

		// Build project input for the template.
		userInput := systemTemplateProjectInput{}

		// Encode both inputs as a combined CUE value.
		platformJSON, err := json.Marshal(platformInput)
		if err != nil {
			return fmt.Errorf("encoding platform input for template %q: %w", tmpl.Name, err)
		}
		userJSON, err := json.Marshal(userInput)
		if err != nil {
			return fmt.Errorf("encoding user input for template %q: %w", tmpl.Name, err)
		}

		// Combine as CUE source: platform: {...}, input: {...}
		combinedCUE := fmt.Sprintf("platform: %s\ninput: %s", string(platformJSON), string(userJSON))

		resources, err := a.renderer.RenderWithCueInput(ctx, tmpl.CueTemplate, combinedCUE)
		if err != nil {
			return fmt.Errorf("rendering mandatory platform template %q for project %q: %w", tmpl.Name, project, err)
		}

		if a.applier == nil {
			slog.WarnContext(ctx, "no resource applier configured, skipping mandatory platform template apply",
				slog.String("template", tmpl.Name),
				slog.String("project", project),
			)
			continue
		}

		// Use the template name as the "deployment name" for the ownership label.
		if err := a.applier.Apply(ctx, projectNamespace, tmpl.Name, resources); err != nil {
			return fmt.Errorf("applying mandatory platform template %q to project %q: %w", tmpl.Name, project, err)
		}

		slog.InfoContext(ctx, "mandatory platform template applied",
			slog.String("org", org),
			slog.String("project", project),
			slog.String("namespace", projectNamespace),
			slog.String("template", tmpl.Name),
			slog.Int("resources", len(resources)),
		)
	}

	return nil
}

// systemTemplateProjectInput carries the user-configurable input for platform templates.
// The field name must match the CUE #Input struct field name in the template.
type systemTemplateProjectInput struct{}
