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

// Render evaluates cueTemplate unified with cueSystemInput and cueInput and
// returns the rendered Kubernetes resource manifests.  cueSystemInput carries
// trusted backend values (project, namespace, claims); cueInput carries
// user-provided deployment parameters.  Both must be valid CUE source;
// cueSystemInput may be empty when the template does not require system values.
func (a *CueRendererAdapter) Render(ctx context.Context, cueTemplate string, cueSystemInput string, cueInput string) ([]RenderResource, error) {
	// Combine cueSystemInput and cueInput into a single CUE document so that
	// both "platform" and "input" top-level fields are available to the template.
	combined := cueSystemInput
	if combined != "" && cueInput != "" {
		combined = combined + "\n" + cueInput
	} else if cueInput != "" {
		combined = cueInput
	}
	resources, err := a.inner.RenderWithCueInput(ctx, cueTemplate, combined)
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
