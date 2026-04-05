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

func (a *CueRendererAdapter) Render(ctx context.Context, cueSource string, in RenderInput) ([]RenderResource, error) {
	resources, err := a.inner.Render(ctx, cueSource, deployments.DeploymentInput{
		Name:      in.Name,
		Image:     in.Image,
		Tag:       in.Tag,
		Project:   in.Project,
		Namespace: in.Namespace,
	})
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
