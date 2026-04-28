package deploymentrender

import (
	"context"
	"fmt"
	"log/slog"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Renderer evaluates CUE templates with deployment parameters.
type Renderer interface {
	Render(ctx context.Context, cueSource string, ancestorSources []string, inputs RenderInputs) (*GroupedResources, error)
}

// ResourceApplier applies and cleans up K8s resources for a deployment.
type ResourceApplier interface {
	Apply(ctx context.Context, project, deploymentName string, resources []unstructured.Unstructured) error
	Reconcile(ctx context.Context, project, deploymentName string, resources []unstructured.Unstructured, previousNamespaces ...string) error
	Cleanup(ctx context.Context, namespaces []string, project, deploymentName string) error
	DiscoverNamespaces(ctx context.Context, project, deploymentName string) ([]string, error)
}

// AncestorTemplateProvider resolves platform template CUE sources from the
// full ancestor chain for render.
type AncestorTemplateProvider interface {
	ListAncestorTemplateSources(ctx context.Context, projectNs, deploymentName string) ([]string, []*consolev1.LinkedTemplateRef, error)
}

// ProjectNamespaceResolver maps a project name to its Kubernetes namespace.
type ProjectNamespaceResolver interface {
	ProjectNamespace(project string) string
}

// Pipeline owns the render/apply path used by deployment RPCs today and the
// DeploymentReconciler in later phases.
type Pipeline struct {
	projectNamespaces        ProjectNamespaceResolver
	renderer                 Renderer
	applier                  ResourceApplier
	ancestorTemplateProvider AncestorTemplateProvider
}

// NewPipeline constructs a deployment render/apply pipeline from injected
// clients and collaborators. The controller-runtime client parameter reserves
// the controller integration seam; current RPC behavior delegates Kubernetes
// object apply operations to the injected ResourceApplier.
func NewPipeline(_ ctrlclient.Client, projectNamespaces ProjectNamespaceResolver, renderer Renderer, applier ResourceApplier) *Pipeline {
	return &Pipeline{
		projectNamespaces: projectNamespaces,
		renderer:          renderer,
		applier:           applier,
	}
}

// WithAncestorTemplateProvider configures platform template source resolution
// for organization/folder-level renders.
func (p *Pipeline) WithAncestorTemplateProvider(provider AncestorTemplateProvider) *Pipeline {
	p.ancestorTemplateProvider = provider
	return p
}

// CanRender reports whether the pipeline has a renderer configured.
func (p *Pipeline) CanRender() bool {
	return p != nil && p.renderer != nil
}

// CanApply reports whether the pipeline has an applier configured.
func (p *Pipeline) CanApply() bool {
	return p != nil && p.applier != nil
}

// Render evaluates deployment resources and returns them grouped by origin.
// When an ancestor-template provider is configured, this is an
// organization/folder-level render and the returned ref slice is the
// policy-effective template set that produced the sources.
func (p *Pipeline) Render(ctx context.Context, project, deploymentName, cueSource string, platform v1alpha2.PlatformInput, projectInput v1alpha2.ProjectInput) (*GroupedResources, []*consolev1.LinkedTemplateRef, error) {
	if p == nil || p.renderer == nil {
		return nil, nil, fmt.Errorf("deployment render pipeline has no renderer")
	}

	var ancestorSources []string
	var effectiveRefs []*consolev1.LinkedTemplateRef
	readPlatformResources := false
	if sources, refs, ok := p.resolveAncestorTemplateSources(ctx, project, deploymentName); ok {
		ancestorSources = sources
		effectiveRefs = refs
		readPlatformResources = true
	}
	grouped, err := p.renderer.Render(ctx, cueSource, ancestorSources, RenderInputs{
		Platform:              platform,
		Project:               projectInput,
		ReadPlatformResources: readPlatformResources,
	})
	if err != nil {
		return nil, nil, err
	}
	return grouped, effectiveRefs, nil
}

// Apply performs server-side apply for rendered resources.
func (p *Pipeline) Apply(ctx context.Context, project, deploymentName string, resources []unstructured.Unstructured) error {
	if p == nil || p.applier == nil {
		return fmt.Errorf("deployment render pipeline has no applier")
	}
	return p.applier.Apply(ctx, project, deploymentName, resources)
}

// Reconcile applies desired resources and cleans up no-longer-rendered owned
// resources.
func (p *Pipeline) Reconcile(ctx context.Context, project, deploymentName string, resources []unstructured.Unstructured, previousNamespaces ...string) error {
	if p == nil || p.applier == nil {
		return fmt.Errorf("deployment render pipeline has no applier")
	}
	return p.applier.Reconcile(ctx, project, deploymentName, resources, previousNamespaces...)
}

// Cleanup deletes all owned resources in the supplied namespaces.
func (p *Pipeline) Cleanup(ctx context.Context, namespaces []string, project, deploymentName string) error {
	if p == nil || p.applier == nil {
		return fmt.Errorf("deployment render pipeline has no applier")
	}
	return p.applier.Cleanup(ctx, namespaces, project, deploymentName)
}

// DiscoverNamespaces scans for namespaces containing resources owned by a
// deployment.
func (p *Pipeline) DiscoverNamespaces(ctx context.Context, project, deploymentName string) ([]string, error) {
	if p == nil || p.applier == nil {
		return nil, fmt.Errorf("deployment render pipeline has no applier")
	}
	return p.applier.DiscoverNamespaces(ctx, project, deploymentName)
}

func (p *Pipeline) resolveAncestorTemplateSources(ctx context.Context, project, deploymentName string) ([]string, []*consolev1.LinkedTemplateRef, bool) {
	if p.ancestorTemplateProvider == nil || p.projectNamespaces == nil {
		return nil, nil, false
	}
	projectNs := p.projectNamespaces.ProjectNamespace(project)
	sources, effectiveRefs, err := p.ancestorTemplateProvider.ListAncestorTemplateSources(ctx, projectNs, deploymentName)
	if err != nil {
		slog.WarnContext(ctx, "ancestor template resolution failed, skipping platform template unification",
			slog.String("project", project),
			slog.String("deployment", deploymentName),
			slog.Any("error", err),
		)
		return nil, nil, false
	}
	return sources, effectiveRefs, true
}
