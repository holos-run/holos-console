package templates

import (
	"context"

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

// Render evaluates cueTemplate unified with cueInput at the "input" CUE path
// and returns the rendered Kubernetes resource manifests.  cueInput must be
// valid CUE source that supplies concrete values for the template parameters.
func (a *CueRendererAdapter) Render(ctx context.Context, cueTemplate string, cueInput string) ([]RenderResource, error) {
	resources, err := a.inner.RenderWithCueInput(ctx, cueTemplate, cueInput)
	if err != nil {
		return nil, err
	}
	out := make([]RenderResource, len(resources))
	for i, u := range resources {
		b, err := yaml.Marshal(u.Object)
		if err != nil {
			return nil, err
		}
		out[i] = RenderResource{YAML: string(b), Object: u.Object}
	}
	return out, nil
}
