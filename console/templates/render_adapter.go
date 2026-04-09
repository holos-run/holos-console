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
// returns the rendered Kubernetes resource manifests.  cuePlatformInput carries
// trusted backend values (project, namespace, claims); cueInput carries
// user-provided deployment parameters.  Both must be valid CUE source;
// cuePlatformInput may be empty when the template does not require platform values.
func (a *CueRendererAdapter) Render(ctx context.Context, cueTemplate string, cuePlatformInput string, cueInput string) ([]RenderResource, error) {
	// Combine cuePlatformInput and cueInput into a single CUE document so that
	// both "platform" and "input" top-level fields are available to the template.
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

// RenderWithOrgTemplateSources evaluates the deployment template unified with
// zero or more platform template CUE sources, then with the CUE input.
// Used by the RenderDeploymentTemplate preview RPC when linked_org_templates
// is provided so draft templates can preview their effective unified output.
func (a *CueRendererAdapter) RenderWithOrgTemplateSources(ctx context.Context, cueTemplate string, orgTemplateSources []string, cuePlatformInput string, cueInput string) ([]RenderResource, error) {
	// Combine cuePlatformInput and cueInput into a single CUE document.
	combinedInput := cuePlatformInput
	if combinedInput != "" && cueInput != "" {
		combinedInput = combinedInput + "\n" + cueInput
	} else if cueInput != "" {
		combinedInput = cueInput
	}
	// Append org template sources to the deployment template CUE, then evaluate
	// with the combined input. This mirrors evaluateWithOrgTemplates but uses the
	// CUE-input path so that the raw CUE input document provides platform values.
	combined := cueTemplate
	for _, src := range orgTemplateSources {
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
