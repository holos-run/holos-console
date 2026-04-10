package templates

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/holos-run/holos-console/console/deployments"
)

// CueRendererAdapter wraps deployments.CueRenderer to satisfy templates.Renderer.
type CueRendererAdapter struct {
	inner *deployments.CueRenderer
}

// NewCueRendererAdapter creates a Renderer backed by deployments.CueRenderer.
func NewCueRendererAdapter() *CueRendererAdapter {
	return &CueRendererAdapter{inner: &deployments.CueRenderer{}}
}

// Render evaluates cueTemplate unified with cuePlatformInput and cueInput and
// returns the rendered Kubernetes resource manifests.
func (a *CueRendererAdapter) Render(ctx context.Context, cueTemplate string, cuePlatformInput string, cueInput string) ([]RenderResource, error) {
	combined := cuePlatformInput
	if combined != "" && cueInput != "" {
		combined = combined + "\n" + cueInput
	} else if cueInput != "" {
		combined = cueInput
	}
	resources, err := a.inner.RenderWithCueInput(ctx, cueTemplate, combined)
	if err != nil {
		return nil, err
	}
	return unstructuredToRenderResources(resources)
}

// RenderWithTemplateSources evaluates the template unified with zero or more
// ancestor template CUE sources, then with the CUE input.
func (a *CueRendererAdapter) RenderWithTemplateSources(ctx context.Context, cueTemplate string, templateSources []string, cuePlatformInput string, cueInput string) ([]RenderResource, error) {
	combinedInput := cuePlatformInput
	if combinedInput != "" && cueInput != "" {
		combinedInput = combinedInput + "\n" + cueInput
	} else if cueInput != "" {
		combinedInput = cueInput
	}
	combined := cueTemplate
	for _, src := range templateSources {
		if src != "" {
			combined = combined + "\n" + src
		}
	}
	resources, err := a.inner.RenderWithCueInput(ctx, combined, combinedInput)
	if err != nil {
		return nil, err
	}
	return unstructuredToRenderResources(resources)
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
