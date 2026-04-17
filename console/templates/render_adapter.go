package templates

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/holos-run/holos-console/console/deployments"
)

// CueRendererAdapter wraps deployments.EvaluateGroupedCUE to satisfy
// templates.Renderer. The adapter assembles the full CUE source document
// (template + ancestor sources + raw CUE inputs) on behalf of callers that
// supply their inputs as raw CUE strings (e.g. the RenderTemplate preview
// path). The raw-CUE call sites go away in later phases (see HOL-562).
type CueRendererAdapter struct{}

// NewCueRendererAdapter creates a Renderer backed by the deployments-package
// CUE evaluation core.
func NewCueRendererAdapter() *CueRendererAdapter {
	return &CueRendererAdapter{}
}

// Render evaluates cueTemplate unified with cuePlatformInput and cueInput and
// returns the rendered Kubernetes resource manifests.
//
// The preview path reads both projectResources and platformResources so that
// platform-template-only templates can be previewed (ADR 016 Decision 8 only
// forbids project-level renders from emitting platformResources; that hard
// boundary is enforced at the structured-input entry point in
// deployments.CueRenderer.Render).
func (a *CueRendererAdapter) Render(ctx context.Context, cueTemplate string, cuePlatformInput string, cueInput string) ([]RenderResource, error) {
	combined := combineCueSource(cueTemplate, nil, cuePlatformInput, cueInput)
	grouped, err := deployments.EvaluateGroupedCUE(ctx, combined, true)
	if err != nil {
		return nil, err
	}
	return unstructuredToRenderResources(flattenGroupedResources(grouped))
}

// RenderWithTemplateSources evaluates the template unified with zero or more
// ancestor template CUE sources, then with the CUE input.
func (a *CueRendererAdapter) RenderWithTemplateSources(ctx context.Context, cueTemplate string, templateSources []string, cuePlatformInput string, cueInput string) ([]RenderResource, error) {
	combined := combineCueSource(cueTemplate, templateSources, cuePlatformInput, cueInput)
	grouped, err := deployments.EvaluateGroupedCUE(ctx, combined, true)
	if err != nil {
		return nil, err
	}
	return unstructuredToRenderResources(flattenGroupedResources(grouped))
}

// RenderGrouped evaluates cueTemplate unified with cuePlatformInput and cueInput
// and returns resources grouped by origin (platform vs project).
func (a *CueRendererAdapter) RenderGrouped(ctx context.Context, cueTemplate string, cuePlatformInput string, cueInput string) (*GroupedRenderResources, error) {
	combined := combineCueSource(cueTemplate, nil, cuePlatformInput, cueInput)
	grouped, err := deployments.EvaluateGroupedCUE(ctx, combined, true)
	if err != nil {
		return nil, err
	}
	return groupedUnstructuredToRenderResources(grouped)
}

// RenderGroupedWithTemplateSources evaluates the template unified with ancestor
// template CUE sources and returns resources grouped by origin.
func (a *CueRendererAdapter) RenderGroupedWithTemplateSources(ctx context.Context, cueTemplate string, templateSources []string, cuePlatformInput string, cueInput string) (*GroupedRenderResources, error) {
	combined := combineCueSource(cueTemplate, templateSources, cuePlatformInput, cueInput)
	grouped, err := deployments.EvaluateGroupedCUE(ctx, combined, true)
	if err != nil {
		return nil, err
	}
	return groupedUnstructuredToRenderResources(grouped)
}

// combineCueSource concatenates the deployment template, any ancestor
// template sources, and the raw-CUE platform/input documents into a single
// compilation unit. Ancestor templates are appended after the deployment
// template so they may reference top-level identifiers defined by the
// deployment template; the raw-CUE inputs are appended last so they bind
// values at the "platform" / "input" paths.
func combineCueSource(cueTemplate string, templateSources []string, cuePlatformInput string, cueInput string) string {
	combined := cueTemplate
	for _, src := range templateSources {
		if src != "" {
			combined = combined + "\n" + src
		}
	}
	if cuePlatformInput != "" {
		combined = combined + "\n" + cuePlatformInput
	}
	if cueInput != "" {
		combined = combined + "\n" + cueInput
	}
	return combined
}

// flattenGroupedResources concatenates the platform and project groups into a
// single slice, preserving the "platform-first" ordering historically
// produced by the flat render entry points.
func flattenGroupedResources(g *deployments.GroupedResources) []unstructured.Unstructured {
	out := make([]unstructured.Unstructured, 0, len(g.Platform)+len(g.Project))
	out = append(out, g.Platform...)
	out = append(out, g.Project...)
	return out
}

// groupedUnstructuredToRenderResources converts GroupedResources from the
// deployments package to GroupedRenderResources in the templates package.
func groupedUnstructuredToRenderResources(grouped *deployments.GroupedResources) (*GroupedRenderResources, error) {
	platform, err := unstructuredToRenderResources(grouped.Platform)
	if err != nil {
		return nil, err
	}
	project, err := unstructuredToRenderResources(grouped.Project)
	if err != nil {
		return nil, err
	}
	return &GroupedRenderResources{
		Platform:                    platform,
		Project:                     project,
		DefaultsJSON:                grouped.DefaultsJSON,
		PlatformInputJSON:           grouped.PlatformInputJSON,
		ProjectInputJSON:            grouped.ProjectInputJSON,
		PlatformResourcesStructJSON: grouped.PlatformResourcesStructJSON,
		ProjectResourcesStructJSON:  grouped.ProjectResourcesStructJSON,
	}, nil
}

// unstructuredToRenderResources converts unstructured K8s objects to RenderResource slice.
func unstructuredToRenderResources(resources []unstructured.Unstructured) ([]RenderResource, error) {
	out := make([]RenderResource, 0, len(resources))
	for _, u := range resources {
		b, err := yaml.Marshal(u.Object)
		if err != nil {
			return nil, err
		}
		out = append(out, RenderResource{YAML: string(b), Object: u.Object})
	}
	return out, nil
}
