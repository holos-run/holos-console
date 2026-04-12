package deployments

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const (
	auditResourceType = "deployment"
	// cueTemplateKey is the ConfigMap data key holding the CUE template source.
	// Mirrors templates.CueTemplateKey to avoid a cross-package import cycle.
	cueTemplateKey = "template.cue"
)

// dnsLabelRe validates deployment names as DNS labels.
var dnsLabelRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// ProjectResolver resolves project namespace grants for access checks.
type ProjectResolver interface {
	GetProjectGrants(ctx context.Context, project string) (shareUsers, shareRoles map[string]string, err error)
}

// SettingsResolver checks if deployments are enabled for a project.
type SettingsResolver interface {
	GetSettings(ctx context.Context, project string) (*consolev1.ProjectSettings, error)
}

// TemplateResolver validates that a referenced template exists and returns its CUE source.
type TemplateResolver interface {
	GetTemplate(ctx context.Context, project, name string) (*corev1.ConfigMap, error)
}

// DefaultGatewayNamespace is the default namespace for the ingress gateway.
const DefaultGatewayNamespace = "istio-ingress"

// Renderer evaluates CUE templates with deployment parameters.
type Renderer interface {
	Render(ctx context.Context, cueSource string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) ([]unstructured.Unstructured, error)
	// RenderWithAncestorTemplates unifies one or more ancestor (organization- and
	// folder-level) template CUE sources with the deployment template before
	// filling in platform and project inputs.
	RenderWithAncestorTemplates(ctx context.Context, deploymentCUE string, ancestorTemplateCUESources []string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) ([]unstructured.Unstructured, error)
	// RenderGrouped evaluates the CUE template and returns resources grouped by
	// origin (platform vs project). Project-level path: platform group is empty.
	RenderGrouped(ctx context.Context, cueSource string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) (*GroupedResources, error)
	// RenderGroupedWithAncestorTemplates evaluates the deployment template unified
	// with ancestor templates and returns resources grouped by origin.
	RenderGroupedWithAncestorTemplates(ctx context.Context, deploymentCUE string, ancestorTemplateCUESources []string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) (*GroupedResources, error)
}

// AncestorWalker resolves the folder ancestry for a project namespace.
// It returns the list of folder user-facing names from the organization down
// to (but not including) the project (i.e. org → folder1 → folder2 → project
// yields ["folder1", "folder2"] when folder namespaces exist). Used to
// populate PlatformInput.Folders so CUE templates can reference platform.folders.
type AncestorWalker interface {
	GetProjectFolders(ctx context.Context, project string) ([]string, error)
}

// AncestorTemplateProvider resolves platform template CUE sources from the
// full ancestor chain (org + folders) for render. The projectNs is the
// starting namespace for the ancestor walk.
type AncestorTemplateProvider interface {
	ListAncestorTemplateSources(ctx context.Context, projectNs string, linkedRefs []*consolev1.LinkedTemplateRef) ([]string, error)
}

// ResourceApplier applies and cleans up K8s resources for a deployment.
type ResourceApplier interface {
	Apply(ctx context.Context, namespace, deploymentName string, resources []unstructured.Unstructured) error
	// Reconcile applies desired resources via SSA then deletes owned resources
	// that are no longer in the desired set (orphan cleanup). Use for updates.
	Reconcile(ctx context.Context, namespace, deploymentName string, resources []unstructured.Unstructured) error
	Cleanup(ctx context.Context, namespace, deploymentName string) error
}

// Handler implements the DeploymentService.
type Handler struct {
	consolev1connect.UnimplementedDeploymentServiceHandler
	k8s                      *K8sClient
	projectResolver          ProjectResolver
	settingsResolver         SettingsResolver
	templateResolver         TemplateResolver
	renderer                 Renderer
	applier                  ResourceApplier
	logReader                LogReader
	ancestorWalker           AncestorWalker
	ancestorTemplateProvider AncestorTemplateProvider
}

// NewHandler creates a DeploymentService handler.
func NewHandler(k8s *K8sClient, projectResolver ProjectResolver, settingsResolver SettingsResolver, templateResolver TemplateResolver, renderer Renderer, applier ResourceApplier) *Handler {
	return &Handler{
		k8s:              k8s,
		projectResolver:  projectResolver,
		settingsResolver: settingsResolver,
		templateResolver: templateResolver,
		renderer:         renderer,
		applier:          applier,
	}
}

// WithAncestorWalker configures the handler with an AncestorWalker for
// resolving folder ancestry. When set, PlatformInput.Folders is populated
// so CUE templates can reference platform.folders.
func (h *Handler) WithAncestorWalker(aw AncestorWalker) *Handler {
	h.ancestorWalker = aw
	return h
}

// WithAncestorTemplateProvider configures the handler with an
// AncestorTemplateProvider for resolving platform template CUE sources from
// the full ancestor chain (org + folders) at render time.
func (h *Handler) WithAncestorTemplateProvider(atp AncestorTemplateProvider) *Handler {
	h.ancestorTemplateProvider = atp
	return h
}

// resolveAncestorTemplateSources resolves platform template CUE sources from
// the full ancestor chain when an AncestorTemplateProvider is configured.
// Returns (sources, true) on success, or (nil, false) when no provider is
// configured or the walk fails.
func (h *Handler) resolveAncestorTemplateSources(ctx context.Context, project string, linkedRefs []*consolev1.LinkedTemplateRef) ([]string, bool) {
	if h.ancestorTemplateProvider == nil {
		return nil, false
	}
	projectNs := h.k8s.Resolver.ProjectNamespace(project)
	sources, err := h.ancestorTemplateProvider.ListAncestorTemplateSources(ctx, projectNs, linkedRefs)
	if err != nil {
		slog.WarnContext(ctx, "ancestor template resolution failed, skipping platform template unification",
			slog.String("project", project),
			slog.Any("error", err),
		)
		return nil, false
	}
	return sources, true
}

// renderResources renders deployment resources, unifying with platform
// templates from the full ancestor chain (org + folders) when an
// AncestorTemplateProvider is configured.
//
// linkedRefs contains the explicit linking list from the deployment template
// annotation (console.holos.run/linked-templates). Each ref carries a version
// constraint so the provider can resolve versioned release sources (ADR 024).
// The effective template set at render time is the union of mandatory+enabled
// templates and the explicitly linked enabled templates (ADR 019).
//
// When no AncestorTemplateProvider is configured, falls back to Render
// (deployment template only, no platform template unification).
func (h *Handler) renderResources(ctx context.Context, project, cueSource string, platform v1alpha2.PlatformInput, projectInput v1alpha2.ProjectInput, linkedRefs []*consolev1.LinkedTemplateRef) ([]unstructured.Unstructured, error) {
	if sources, ok := h.resolveAncestorTemplateSources(ctx, project, linkedRefs); ok {
		if len(sources) > 0 {
			return h.renderer.RenderWithAncestorTemplates(ctx, cueSource, sources, platform, projectInput)
		}
		return h.renderer.Render(ctx, cueSource, platform, projectInput)
	}

	// No AncestorTemplateProvider configured; render without platform templates.
	return h.renderer.Render(ctx, cueSource, platform, projectInput)
}

// renderResourcesGrouped mirrors renderResources but returns resources grouped
// by origin (platform vs project). Used by GetDeploymentRenderPreview to populate
// the per-collection response fields.
func (h *Handler) renderResourcesGrouped(ctx context.Context, project, cueSource string, platform v1alpha2.PlatformInput, projectInput v1alpha2.ProjectInput, linkedRefs []*consolev1.LinkedTemplateRef) (*GroupedResources, error) {
	if sources, ok := h.resolveAncestorTemplateSources(ctx, project, linkedRefs); ok {
		if len(sources) > 0 {
			return h.renderer.RenderGroupedWithAncestorTemplates(ctx, cueSource, sources, platform, projectInput)
		}
		return h.renderer.RenderGrouped(ctx, cueSource, platform, projectInput)
	}

	// No AncestorTemplateProvider configured; render without platform templates.
	return h.renderer.RenderGrouped(ctx, cueSource, platform, projectInput)
}

// ListDeployments returns all deployments in a project.
func (h *Handler) ListDeployments(
	ctx context.Context,
	req *connect.Request[consolev1.ListDeploymentsRequest],
) (*connect.Response[consolev1.ListDeploymentsResponse], error) {
	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsList); err != nil {
		return nil, err
	}

	cms, err := h.k8s.ListDeployments(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}

	deployments := make([]*consolev1.Deployment, 0, len(cms))
	for _, cm := range cms {
		deployments = append(deployments, configMapToDeployment(&cm, project))
	}

	slog.InfoContext(ctx, "deployments listed",
		slog.String("action", "deployments_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(deployments)),
	)

	return connect.NewResponse(&consolev1.ListDeploymentsResponse{
		Deployments: deployments,
	}), nil
}

// GetDeployment returns a single deployment by name.
func (h *Handler) GetDeployment(
	ctx context.Context,
	req *connect.Request[consolev1.GetDeploymentRequest],
) (*connect.Response[consolev1.GetDeploymentResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsRead); err != nil {
		return nil, err
	}

	cm, err := h.k8s.GetDeployment(ctx, project, name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "deployment read",
		slog.String("action", "deployment_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
	)

	return connect.NewResponse(&consolev1.GetDeploymentResponse{
		Deployment: configMapToDeployment(cm, project),
	}), nil
}

// CreateDeployment creates a new deployment.
func (h *Handler) CreateDeployment(
	ctx context.Context,
	req *connect.Request[consolev1.CreateDeploymentRequest],
) (*connect.Response[consolev1.CreateDeploymentResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if err := validateDeploymentName(name); err != nil {
		return nil, err
	}
	if req.Msg.Image == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("image is required"))
	}
	if req.Msg.Tag == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("tag is required"))
	}
	if req.Msg.Template == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("template is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsWrite); err != nil {
		return nil, err
	}

	// Check that deployments are enabled in project settings.
	if h.settingsResolver != nil {
		s, err := h.settingsResolver.GetSettings(ctx, project)
		if err != nil {
			slog.WarnContext(ctx, "failed to resolve project settings",
				slog.String("project", project),
				slog.Any("error", err),
			)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check project settings"))
		}
		if !s.DeploymentsEnabled {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("deployments are not enabled for project %q", project))
		}
	}

	// Validate that the referenced template exists and get its CUE source
	// along with the explicit linking list (ADR 019, ADR 024).
	var cueSource string
	var linkedOrgTemplates []*consolev1.LinkedTemplateRef
	if h.templateResolver != nil {
		tmplCM, err := h.templateResolver.GetTemplate(ctx, project, req.Msg.Template)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("template %q not found in project %q", req.Msg.Template, project))
		}
		cueSource = tmplCM.Data[cueTemplateKey]
		linkedOrgTemplates = linkedTemplateRefsFromAnnotation(tmplCM)
	}

	envInputs, err := validateEnvVars(req.Msg.Env)
	if err != nil {
		return nil, err
	}

	displayName := ""
	if req.Msg.DisplayName != nil {
		displayName = *req.Msg.DisplayName
	}
	description := ""
	if req.Msg.Description != nil {
		description = *req.Msg.Description
	}

	_, err = h.k8s.CreateDeployment(ctx, project, name, req.Msg.Image, req.Msg.Tag, req.Msg.Template, displayName, description, req.Msg.Command, req.Msg.Args, envInputs, req.Msg.Port)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Render and apply the deployment resources. On any failure, roll back by
	// cleaning up partial K8s resources and deleting the deployment ConfigMap so
	// the operation is all-or-nothing.
	if h.renderer != nil && h.applier != nil {
		ns := h.k8s.Resolver.ProjectNamespace(project)
		platformIn := h.buildPlatformInput(ctx, project, ns, claims)
		projectIn := v1alpha2.ProjectInput{
			Name:    name,
			Image:   req.Msg.Image,
			Tag:     req.Msg.Tag,
			Command: req.Msg.Command,
			Args:    req.Msg.Args,
			Env:     envInputs,
			Port:    defaultPort(int(req.Msg.Port)),
		}
		resources, renderErr := h.renderResources(ctx, project, cueSource, platformIn, projectIn, linkedOrgTemplates)
		if renderErr != nil {
			slog.WarnContext(ctx, "render failed after creating deployment — rolling back",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", renderErr),
			)
			h.rollbackCreate(ctx, ns, project, name)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rendering deployment resources: %w", renderErr))
		}
		if applyErr := h.applier.Apply(ctx, ns, name, resources); applyErr != nil {
			slog.WarnContext(ctx, "apply failed after creating deployment — rolling back",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", applyErr),
			)
			h.rollbackCreate(ctx, ns, project, name)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("applying deployment resources: %w", applyErr))
		}
	}

	slog.InfoContext(ctx, "deployment created",
		slog.String("action", "deployment_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateDeploymentResponse{
		Name: name,
	}), nil
}

// rollbackCreate attempts to undo a partially-applied CreateDeployment:
//  1. Calls Cleanup to remove any K8s resources already applied.
//  2. Deletes the deployment ConfigMap metadata record.
//
// Rollback errors are logged at warn level but do not replace the original error.
func (h *Handler) rollbackCreate(ctx context.Context, ns, project, name string) {
	if cleanupErr := h.applier.Cleanup(ctx, ns, name); cleanupErr != nil {
		slog.WarnContext(ctx, "rollback: cleanup failed",
			slog.String("project", project),
			slog.String("name", name),
			slog.Any("error", cleanupErr),
		)
	}
	if deleteErr := h.k8s.DeleteDeployment(ctx, project, name); deleteErr != nil {
		slog.WarnContext(ctx, "rollback: delete ConfigMap failed",
			slog.String("project", project),
			slog.String("name", name),
			slog.Any("error", deleteErr),
		)
	}
}

// UpdateDeployment updates an existing deployment.
func (h *Handler) UpdateDeployment(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateDeploymentRequest],
) (*connect.Response[consolev1.UpdateDeploymentResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsWrite); err != nil {
		return nil, err
	}

	envInputs, err := validateEnvVars(req.Msg.Env)
	if err != nil {
		return nil, err
	}

	updated, err := h.k8s.UpdateDeployment(ctx, project, name, req.Msg.Image, req.Msg.Tag, req.Msg.DisplayName, req.Msg.Description, req.Msg.Command, req.Msg.Args, envInputs, req.Msg.Port)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Re-render and reconcile deployment resources with updated parameters.
	// Reconcile applies the new desired set via SSA then deletes any previously
	// owned resources that are no longer in the desired set (orphan cleanup).
	if h.renderer != nil && h.applier != nil && updated != nil {
		templateName := updated.Data[TemplateKey]
		image := updated.Data[ImageKey]
		tag := updated.Data[TagKey]

		var cueSource string
		var linkedOrgTemplatesUpdate []*consolev1.LinkedTemplateRef
		if h.templateResolver != nil && templateName != "" {
			tmplCM, tmplErr := h.templateResolver.GetTemplate(ctx, project, templateName)
			if tmplErr != nil {
				slog.WarnContext(ctx, "template not found during update re-render",
					slog.String("project", project),
					slog.String("template", templateName),
					slog.Any("error", tmplErr),
				)
			} else {
				cueSource = tmplCM.Data[cueTemplateKey]
				linkedOrgTemplatesUpdate = linkedTemplateRefsFromAnnotation(tmplCM)
			}
		}

		ns := h.k8s.Resolver.ProjectNamespace(project)
		platformIn := h.buildPlatformInput(ctx, project, ns, claims)
		projectIn := v1alpha2.ProjectInput{
			Name:    name,
			Image:   image,
			Tag:     tag,
			Command: commandFromConfigMap(updated),
			Args:    argsFromConfigMap(updated),
			Env:     envFromConfigMapAsV1alpha2(updated),
			Port:    defaultPort(portFromConfigMap(updated)),
		}
		resources, renderErr := h.renderResources(ctx, project, cueSource, platformIn, projectIn, linkedOrgTemplatesUpdate)
		if renderErr != nil {
			slog.WarnContext(ctx, "render failed during deployment update",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", renderErr),
			)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rendering deployment resources: %w", renderErr))
		}
		// Use Reconcile instead of Apply so orphaned resources from template
		// changes (e.g. a removed HTTPRoute) are cleaned up after a successful
		// apply.
		if reconcileErr := h.applier.Reconcile(ctx, ns, name, resources); reconcileErr != nil {
			slog.WarnContext(ctx, "reconcile failed during deployment update",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", reconcileErr),
			)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("reconciling deployment resources: %w", reconcileErr))
		}
	}

	slog.InfoContext(ctx, "deployment updated",
		slog.String("action", "deployment_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateDeploymentResponse{}), nil
}

// DeleteDeployment deletes a deployment.
func (h *Handler) DeleteDeployment(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteDeploymentRequest],
) (*connect.Response[consolev1.DeleteDeploymentResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsDelete); err != nil {
		return nil, err
	}

	// Clean up all K8s resources owned by this deployment before removing the record.
	if h.applier != nil {
		ns := h.k8s.Resolver.ProjectNamespace(project)
		if cleanupErr := h.applier.Cleanup(ctx, ns, name); cleanupErr != nil {
			slog.WarnContext(ctx, "cleanup failed during deployment delete",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", cleanupErr),
			)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("cleaning up deployment resources: %w", cleanupErr))
		}
	}

	if err := h.k8s.DeleteDeployment(ctx, project, name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "deployment deleted",
		slog.String("action", "deployment_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteDeploymentResponse{}), nil
}

// ListNamespaceSecrets lists Kubernetes Secrets in the project namespace available for env var references.
func (h *Handler) ListNamespaceSecrets(
	ctx context.Context,
	req *connect.Request[consolev1.ListNamespaceSecretsRequest],
) (*connect.Response[consolev1.ListNamespaceSecretsResponse], error) {
	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsWrite); err != nil {
		return nil, err
	}

	items, err := h.k8s.ListNamespaceSecrets(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}

	secrets := make([]*consolev1.NamespaceResource, 0, len(items))
	for _, item := range items {
		secrets = append(secrets, &consolev1.NamespaceResource{
			Name: item.Name,
			Keys: item.Keys,
		})
	}

	slog.InfoContext(ctx, "namespace secrets listed",
		slog.String("action", "namespace_secrets_list"),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(secrets)),
	)

	return connect.NewResponse(&consolev1.ListNamespaceSecretsResponse{
		Secrets: secrets,
	}), nil
}

// ListNamespaceConfigMaps lists Kubernetes ConfigMaps in the project namespace available for env var references.
func (h *Handler) ListNamespaceConfigMaps(
	ctx context.Context,
	req *connect.Request[consolev1.ListNamespaceConfigMapsRequest],
) (*connect.Response[consolev1.ListNamespaceConfigMapsResponse], error) {
	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsWrite); err != nil {
		return nil, err
	}

	items, err := h.k8s.ListNamespaceConfigMaps(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}

	configMaps := make([]*consolev1.NamespaceResource, 0, len(items))
	for _, item := range items {
		configMaps = append(configMaps, &consolev1.NamespaceResource{
			Name: item.Name,
			Keys: item.Keys,
		})
	}

	slog.InfoContext(ctx, "namespace configmaps listed",
		slog.String("action", "namespace_configmaps_list"),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(configMaps)),
	)

	return connect.NewResponse(&consolev1.ListNamespaceConfigMapsResponse{
		ConfigMaps: configMaps,
	}), nil
}

// GetDeploymentRenderPreview returns the CUE template source, platform input,
// project input, and rendered output for a deployment.
func (h *Handler) GetDeploymentRenderPreview(
	ctx context.Context,
	req *connect.Request[consolev1.GetDeploymentRenderPreviewRequest],
) (*connect.Response[consolev1.GetDeploymentRenderPreviewResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsRead); err != nil {
		return nil, err
	}

	// Look up the deployment record.
	cm, err := h.k8s.GetDeployment(ctx, project, name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Look up the template CUE source.
	templateName := cm.Data[TemplateKey]
	if templateName == "" {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("deployment %q has no template configured", name))
	}
	if h.templateResolver == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("template resolver not configured"))
	}
	tmplCM, err := h.templateResolver.GetTemplate(ctx, project, templateName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("template %q not found in project %q", templateName, project))
	}
	cueTemplate := tmplCM.Data[cueTemplateKey]
	linkedOrgTemplatesPreview := linkedTemplateRefsFromAnnotation(tmplCM)

	// Build platform input from authenticated claims and resolved namespace.
	ns := h.k8s.Resolver.ProjectNamespace(project)
	platformIn := h.buildPlatformInput(ctx, project, ns, claims)

	// Build project input from the deployment's stored fields.
	projectIn := v1alpha2.ProjectInput{
		Name:    name,
		Image:   cm.Data[ImageKey],
		Tag:     cm.Data[TagKey],
		Command: commandFromConfigMap(cm),
		Args:    argsFromConfigMap(cm),
		Env:     envFromConfigMapAsV1alpha2(cm),
		Port:    defaultPort(portFromConfigMap(cm)),
	}

	// Format platform and project inputs as CUE strings.
	platformJSON, err := json.Marshal(platformIn)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encoding platform input: %w", err))
	}
	projectJSON, err := json.Marshal(projectIn)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encoding project input: %w", err))
	}
	cuePlatformInput := fmt.Sprintf("platform: %s", string(platformJSON))
	cueProjectInput := fmt.Sprintf("input: %s", string(projectJSON))

	// Render the template to produce YAML and JSON output, including linked
	// platform templates (ADR 019). Uses renderResourcesGrouped so the same
	// linking logic applies at preview time as at deploy time, and the per-
	// collection fields are populated.
	var renderedYAML, renderedJSON string
	var platformResourcesYAML, platformResourcesJSON string
	var projectResourcesYAML, projectResourcesJSON string
	var grouped *GroupedResources
	if h.renderer != nil {
		var renderErr error
		grouped, renderErr = h.renderResourcesGrouped(ctx, project, cueTemplate, platformIn, projectIn, linkedOrgTemplatesPreview)
		if renderErr != nil {
			slog.WarnContext(ctx, "render failed during deployment preview",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", renderErr),
			)
			// Return the inputs even if render fails — the frontend can display the error.
			return connect.NewResponse(&consolev1.GetDeploymentRenderPreviewResponse{
				CueTemplate:      cueTemplate,
				CuePlatformInput: cuePlatformInput,
				CueProjectInput:  cueProjectInput,
			}), nil
		}

		// Serialize per-collection resources.
		platformResourcesYAML, platformResourcesJSON = serializeUnstructured(grouped.Platform)
		projectResourcesYAML, projectResourcesJSON = serializeUnstructured(grouped.Project)

		// Produce the unified rendered output by combining both collections.
		allResources := append(grouped.Platform, grouped.Project...)
		renderedYAML, renderedJSON = serializeUnstructured(allResources)
	}

	slog.InfoContext(ctx, "deployment render preview",
		slog.String("action", "deployment_render_preview"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
	)

	// Extract structured JSON fields from the grouped result if render succeeded.
	var defaultsJSON, platformInputJSON, projectInputJSON *string
	var platformResourcesStructJSON, projectResourcesStructJSON *string
	if grouped != nil {
		defaultsJSON = grouped.DefaultsJSON
		platformInputJSON = grouped.PlatformInputJSON
		projectInputJSON = grouped.ProjectInputJSON
		platformResourcesStructJSON = grouped.PlatformResourcesStructJSON
		projectResourcesStructJSON = grouped.ProjectResourcesStructJSON
	}

	return connect.NewResponse(&consolev1.GetDeploymentRenderPreviewResponse{
		CueTemplate:                     cueTemplate,
		CuePlatformInput:                cuePlatformInput,
		CueProjectInput:                 cueProjectInput,
		RenderedYaml:                    renderedYAML,
		RenderedJson:                    renderedJSON,
		PlatformResourcesYaml:          platformResourcesYAML,
		PlatformResourcesJson:          platformResourcesJSON,
		ProjectResourcesYaml:           projectResourcesYAML,
		ProjectResourcesJson:           projectResourcesJSON,
		DefaultsJson:                   defaultsJSON,
		PlatformInputJson:              platformInputJSON,
		ProjectInputJson:               projectInputJSON,
		PlatformResourcesStructuredJson: platformResourcesStructJSON,
		ProjectResourcesStructuredJson:  projectResourcesStructJSON,
	}), nil
}

// checkProjectAccess verifies that the user has the given permission via project cascade grants.
func (h *Handler) checkProjectAccess(ctx context.Context, claims *rpc.Claims, project string, permission rbac.Permission) error {
	if h.projectResolver == nil {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	users, roles, err := h.projectResolver.GetProjectGrants(ctx, project)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve project grants",
			slog.String("project", project),
			slog.Any("error", err),
		)
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	return rbac.CheckCascadeAccess(claims.Email, claims.Roles, users, roles, permission, rbac.ProjectCascadeDeploymentPerms)
}

// serializeUnstructured converts a slice of unstructured Kubernetes resources
// into a multi-document YAML string (separated by "---\n") and a JSON array
// string. Returns an empty YAML string and "[]" for an empty or nil slice so
// that JSON fields are always valid parseable JSON arrays.
func serializeUnstructured(resources []unstructured.Unstructured) (yamlStr, jsonStr string) {
	if len(resources) == 0 {
		return "", "[]"
	}
	var buf strings.Builder
	objects := make([]map[string]any, 0, len(resources))
	for i, r := range resources {
		if i > 0 {
			buf.WriteString("---\n")
		}
		yamlBytes, yamlErr := yaml.Marshal(r.Object)
		if yamlErr == nil {
			buf.WriteString(string(yamlBytes))
		}
		if r.Object != nil {
			objects = append(objects, r.Object)
		}
	}
	jsonBytes, jsonErr := json.MarshalIndent(objects, "", "  ")
	if jsonErr == nil {
		jsonStr = string(jsonBytes)
	}
	return buf.String(), jsonStr
}

// validateDeploymentName checks that the name is a valid DNS label.
func validateDeploymentName(name string) error {
	if name == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	if len(name) > 63 {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name must be at most 63 characters"))
	}
	if !dnsLabelRe.MatchString(name) {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name must be a valid DNS label (lowercase alphanumeric and hyphens, starting with a letter)"))
	}
	return nil
}

// configMapToDeployment converts a Kubernetes ConfigMap to a Deployment protobuf message.
func configMapToDeployment(cm *corev1.ConfigMap, project string) *consolev1.Deployment {
	dep := &consolev1.Deployment{
		Name:        cm.Name,
		Project:     project,
		Image:       cm.Data[ImageKey],
		Tag:         cm.Data[TagKey],
		Template:    cm.Data[TemplateKey],
		DisplayName: cm.Annotations[v1alpha2.AnnotationDisplayName],
		Description: cm.Annotations[v1alpha2.AnnotationDescription],
		Command:     commandFromConfigMap(cm),
		Args:        argsFromConfigMap(cm),
		Env:         envFromConfigMap(cm),
	}
	if raw, ok := cm.Data[PortKey]; ok && raw != "" {
		if p, err := strconv.ParseInt(raw, 10, 32); err == nil {
			dep.Port = int32(p)
		}
	}
	return dep
}

// commandFromConfigMap reads the JSON-encoded command slice from a ConfigMap.
func commandFromConfigMap(cm *corev1.ConfigMap) []string {
	return stringSliceFromConfigMap(cm, CommandKey)
}

// argsFromConfigMap reads the JSON-encoded args slice from a ConfigMap.
func argsFromConfigMap(cm *corev1.ConfigMap) []string {
	return stringSliceFromConfigMap(cm, ArgsKey)
}

// envFromConfigMapAsV1alpha2 reads the JSON-encoded env vars from a ConfigMap as v1alpha2.EnvVar slice.
func envFromConfigMapAsV1alpha2(cm *corev1.ConfigMap) []v1alpha2.EnvVar {
	raw, ok := cm.Data[EnvKey]
	if !ok || raw == "" {
		return nil
	}
	var inputs []v1alpha2.EnvVar
	if err := json.Unmarshal([]byte(raw), &inputs); err != nil {
		return nil
	}
	return inputs
}

// envFromConfigMap reads the JSON-encoded env vars from a ConfigMap and converts them to proto messages.
func envFromConfigMap(cm *corev1.ConfigMap) []*consolev1.EnvVar {
	raw, ok := cm.Data[EnvKey]
	if !ok || raw == "" {
		return nil
	}
	var inputs []v1alpha2.EnvVar
	if err := json.Unmarshal([]byte(raw), &inputs); err != nil {
		return nil
	}
	result := make([]*consolev1.EnvVar, 0, len(inputs))
	for _, e := range inputs {
		result = append(result, envVarToProto(e))
	}
	return result
}

// envVarToProto converts a v1alpha2.EnvVar to a proto EnvVar message.
func envVarToProto(e v1alpha2.EnvVar) *consolev1.EnvVar {
	ev := &consolev1.EnvVar{Name: e.Name}
	switch {
	case e.SecretKeyRef != nil:
		ev.Source = &consolev1.EnvVar_SecretKeyRef{
			SecretKeyRef: &consolev1.SecretKeyRef{Name: e.SecretKeyRef.Name, Key: e.SecretKeyRef.Key},
		}
	case e.ConfigMapKeyRef != nil:
		ev.Source = &consolev1.EnvVar_ConfigMapKeyRef{
			ConfigMapKeyRef: &consolev1.ConfigMapKeyRef{Name: e.ConfigMapKeyRef.Name, Key: e.ConfigMapKeyRef.Key},
		}
	default:
		ev.Source = &consolev1.EnvVar_Value{Value: e.Value}
	}
	return ev
}

// protoToEnvVar converts a proto EnvVar message to a v1alpha2.EnvVar.
func protoToEnvVar(e *consolev1.EnvVar) v1alpha2.EnvVar {
	input := v1alpha2.EnvVar{Name: e.GetName()}
	switch src := e.GetSource().(type) {
	case *consolev1.EnvVar_SecretKeyRef:
		if src.SecretKeyRef != nil {
			input.SecretKeyRef = &v1alpha2.KeyRef{Name: src.SecretKeyRef.GetName(), Key: src.SecretKeyRef.GetKey()}
		}
	case *consolev1.EnvVar_ConfigMapKeyRef:
		if src.ConfigMapKeyRef != nil {
			input.ConfigMapKeyRef = &v1alpha2.KeyRef{Name: src.ConfigMapKeyRef.GetName(), Key: src.ConfigMapKeyRef.GetKey()}
		}
	case *consolev1.EnvVar_Value:
		input.Value = src.Value
	}
	return input
}

// validateEnvVars validates a list of proto EnvVar messages and converts them to v1alpha2.EnvVar.
// Returns an error if any env var has an empty name.
func validateEnvVars(envVars []*consolev1.EnvVar) ([]v1alpha2.EnvVar, error) {
	if len(envVars) == 0 {
		return nil, nil
	}
	result := make([]v1alpha2.EnvVar, 0, len(envVars))
	for _, e := range envVars {
		if e.GetName() == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("env var name must not be empty"))
		}
		result = append(result, protoToEnvVar(e))
	}
	return result, nil
}

// portFromConfigMap reads the port integer from a ConfigMap data key.
func portFromConfigMap(cm *corev1.ConfigMap) int {
	raw, ok := cm.Data[PortKey]
	if !ok || raw == "" {
		return 0
	}
	p, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0
	}
	return int(p)
}

// defaultPort returns port if non-zero, otherwise returns 8080.
func defaultPort(port int) int {
	if port == 0 {
		return 8080
	}
	return port
}

// buildPlatformInput constructs a v1alpha2.PlatformInput from handler context.
// When an AncestorWalker is configured, Folders is populated with the ordered
// list of folder names in the ancestor chain (org → folders → project) so CUE
// templates can reference platform.folders.
func (h *Handler) buildPlatformInput(ctx context.Context, project, namespace string, claims *rpc.Claims) v1alpha2.PlatformInput {
	pi := v1alpha2.PlatformInput{
		Project:          project,
		Namespace:        namespace,
		GatewayNamespace: DefaultGatewayNamespace,
	}
	if claims != nil {
		pi.Claims = v1alpha2.Claims{
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
	if h.ancestorWalker != nil {
		folders, err := h.ancestorWalker.GetProjectFolders(ctx, project)
		if err != nil {
			slog.WarnContext(ctx, "could not resolve folder ancestry for platform input",
				slog.String("project", project),
				slog.Any("error", err),
			)
		} else {
			folderInfos := make([]v1alpha2.FolderInfo, 0, len(folders))
			for _, name := range folders {
				folderInfos = append(folderInfos, v1alpha2.FolderInfo{Name: name})
			}
			pi.Folders = folderInfos
		}
	}
	return pi
}

// linkedTemplateRefsFromAnnotation reads the linked template refs from a
// deployment template ConfigMap. It prefers the v1alpha2
// console.holos.run/linked-templates annotation (JSON array of
// {scope, scope_name, name, version_constraint} objects) and falls back to the
// legacy console.holos.run/linked-org-templates annotation (JSON array of bare
// name strings, converted to org-scope refs with no version constraint).
// Returns nil if no annotation is present or if parsing fails.
func linkedTemplateRefsFromAnnotation(cm *corev1.ConfigMap) []*consolev1.LinkedTemplateRef {
	if cm == nil || cm.Annotations == nil {
		return nil
	}

	// v1alpha2: console.holos.run/linked-templates — JSON array of {scope, scope_name, name, version_constraint}
	if raw, ok := cm.Annotations[v1alpha2.AnnotationLinkedTemplates]; ok && raw != "" {
		var refs []struct {
			Scope             string `json:"scope"`
			ScopeName         string `json:"scope_name"`
			Name              string `json:"name"`
			VersionConstraint string `json:"version_constraint,omitempty"`
		}
		if err := json.Unmarshal([]byte(raw), &refs); err != nil {
			slog.Warn("failed to parse linked-templates annotation",
				slog.String("name", cm.Name),
				slog.String("namespace", cm.Namespace),
				slog.Any("error", err),
			)
		} else {
			result := make([]*consolev1.LinkedTemplateRef, 0, len(refs))
			for _, ref := range refs {
				if ref.Name != "" {
					result = append(result, &consolev1.LinkedTemplateRef{
						Scope:             scopeFromLabel(ref.Scope),
						ScopeName:         ref.ScopeName,
						Name:              ref.Name,
						VersionConstraint: ref.VersionConstraint,
					})
				}
			}
			return result
		}
	}

	// Legacy v1alpha2: console.holos.run/linked-org-templates — JSON array of strings.
	// Convert to LinkedTemplateRef with organization scope and no version constraint.
	if raw, ok := cm.Annotations[v1alpha2.AnnotationLinkedOrgTemplates]; ok && raw != "" {
		var names []string
		if err := json.Unmarshal([]byte(raw), &names); err != nil {
			slog.Warn("failed to parse linked-org-templates annotation",
				slog.String("name", cm.Name),
				slog.String("namespace", cm.Namespace),
				slog.Any("error", err),
			)
			return nil
		}
		result := make([]*consolev1.LinkedTemplateRef, 0, len(names))
		for _, n := range names {
			result = append(result, &consolev1.LinkedTemplateRef{
				Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
				Name:  n,
			})
		}
		return result
	}

	return nil
}

// stringSliceFromConfigMap decodes a JSON string slice from the given ConfigMap data key.
func stringSliceFromConfigMap(cm *corev1.ConfigMap, key string) []string {
	raw, ok := cm.Data[key]
	if !ok || raw == "" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil
	}
	return result
}

// scopeFromLabel converts a label string back to a TemplateScope enum value.
func scopeFromLabel(label string) consolev1.TemplateScope {
	switch label {
	case v1alpha2.TemplateScopeOrganization:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION
	case v1alpha2.TemplateScopeFolder:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER
	case v1alpha2.TemplateScopeProject:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT
	default:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED
	}
}

// mapK8sError converts Kubernetes API errors to ConnectRPC errors.
func mapK8sError(err error) error {
	if errors.IsNotFound(err) {
		return connect.NewError(connect.CodeNotFound, err)
	}
	if errors.IsAlreadyExists(err) {
		return connect.NewError(connect.CodeAlreadyExists, err)
	}
	if errors.IsForbidden(err) {
		return connect.NewError(connect.CodePermissionDenied, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
