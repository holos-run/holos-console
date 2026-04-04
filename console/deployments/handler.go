package deployments

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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

// Renderer evaluates CUE templates with deployment parameters.
type Renderer interface {
	Render(ctx context.Context, cueSource string, input DeploymentInput) ([]unstructured.Unstructured, error)
}

// ResourceApplier applies and cleans up K8s resources for a deployment.
type ResourceApplier interface {
	Apply(ctx context.Context, namespace, deploymentName string, resources []unstructured.Unstructured) error
	Cleanup(ctx context.Context, namespace, deploymentName string) error
}

// Handler implements the DeploymentService.
type Handler struct {
	consolev1connect.UnimplementedDeploymentServiceHandler
	k8s              *K8sClient
	projectResolver  ProjectResolver
	settingsResolver SettingsResolver
	templateResolver TemplateResolver
	renderer         Renderer
	applier          ResourceApplier
	logReader        LogReader
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

	// Validate that the referenced template exists and get its CUE source.
	var cueSource string
	if h.templateResolver != nil {
		tmplCM, err := h.templateResolver.GetTemplate(ctx, project, req.Msg.Template)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("template %q not found in project %q", req.Msg.Template, project))
		}
		cueSource = tmplCM.Data[cueTemplateKey]
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

	_, err = h.k8s.CreateDeployment(ctx, project, name, req.Msg.Image, req.Msg.Tag, req.Msg.Template, displayName, description, req.Msg.Command, req.Msg.Args, envInputs)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Render and apply the deployment resources.
	if h.renderer != nil && h.applier != nil {
		ns := h.k8s.Resolver.ProjectNamespace(project)
		input := DeploymentInput{
			Name:      name,
			Image:     req.Msg.Image,
			Tag:       req.Msg.Tag,
			Project:   project,
			Namespace: ns,
			Command:   req.Msg.Command,
			Args:      req.Msg.Args,
			Env:       envInputs,
		}
		resources, renderErr := h.renderer.Render(ctx, cueSource, input)
		if renderErr != nil {
			slog.WarnContext(ctx, "render failed after creating deployment",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", renderErr),
			)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rendering deployment resources: %w", renderErr))
		}
		if applyErr := h.applier.Apply(ctx, ns, name, resources); applyErr != nil {
			slog.WarnContext(ctx, "apply failed after creating deployment",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", applyErr),
			)
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

	updated, err := h.k8s.UpdateDeployment(ctx, project, name, req.Msg.Image, req.Msg.Tag, req.Msg.DisplayName, req.Msg.Description, req.Msg.Command, req.Msg.Args, envInputs)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Re-render and re-apply deployment resources with updated parameters.
	if h.renderer != nil && h.applier != nil && updated != nil {
		templateName := updated.Data[TemplateKey]
		image := updated.Data[ImageKey]
		tag := updated.Data[TagKey]

		var cueSource string
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
			}
		}

		ns := h.k8s.Resolver.ProjectNamespace(project)
		input := DeploymentInput{
			Name:      name,
			Image:     image,
			Tag:       tag,
			Project:   project,
			Namespace: ns,
			Command:   commandFromConfigMap(updated),
			Args:      argsFromConfigMap(updated),
			Env:       envFromConfigMapAsInputs(updated),
		}
		resources, renderErr := h.renderer.Render(ctx, cueSource, input)
		if renderErr != nil {
			slog.WarnContext(ctx, "render failed during deployment update",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", renderErr),
			)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rendering deployment resources: %w", renderErr))
		}
		if applyErr := h.applier.Apply(ctx, ns, name, resources); applyErr != nil {
			slog.WarnContext(ctx, "apply failed during deployment update",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", applyErr),
			)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("applying deployment resources: %w", applyErr))
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
	return &consolev1.Deployment{
		Name:        cm.Name,
		Project:     project,
		Image:       cm.Data[ImageKey],
		Tag:         cm.Data[TagKey],
		Template:    cm.Data[TemplateKey],
		DisplayName: cm.Annotations[DisplayNameAnnotation],
		Description: cm.Annotations[DescriptionAnnotation],
		Command:     commandFromConfigMap(cm),
		Args:        argsFromConfigMap(cm),
		Env:         envFromConfigMap(cm),
	}
}

// commandFromConfigMap reads the JSON-encoded command slice from a ConfigMap.
func commandFromConfigMap(cm *corev1.ConfigMap) []string {
	return stringSliceFromConfigMap(cm, CommandKey)
}

// argsFromConfigMap reads the JSON-encoded args slice from a ConfigMap.
func argsFromConfigMap(cm *corev1.ConfigMap) []string {
	return stringSliceFromConfigMap(cm, ArgsKey)
}

// envFromConfigMapAsInputs reads the JSON-encoded env vars from a ConfigMap as EnvVarInput slice.
func envFromConfigMapAsInputs(cm *corev1.ConfigMap) []EnvVarInput {
	raw, ok := cm.Data[EnvKey]
	if !ok || raw == "" {
		return nil
	}
	var inputs []EnvVarInput
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
	var inputs []EnvVarInput
	if err := json.Unmarshal([]byte(raw), &inputs); err != nil {
		return nil
	}
	result := make([]*consolev1.EnvVar, 0, len(inputs))
	for _, e := range inputs {
		result = append(result, envVarInputToProto(e))
	}
	return result
}

// envVarInputToProto converts an EnvVarInput to a proto EnvVar message.
func envVarInputToProto(e EnvVarInput) *consolev1.EnvVar {
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

// protoToEnvVarInput converts a proto EnvVar message to an EnvVarInput.
func protoToEnvVarInput(e *consolev1.EnvVar) EnvVarInput {
	input := EnvVarInput{Name: e.GetName()}
	switch src := e.GetSource().(type) {
	case *consolev1.EnvVar_SecretKeyRef:
		if src.SecretKeyRef != nil {
			input.SecretKeyRef = &KeyRefInput{Name: src.SecretKeyRef.GetName(), Key: src.SecretKeyRef.GetKey()}
		}
	case *consolev1.EnvVar_ConfigMapKeyRef:
		if src.ConfigMapKeyRef != nil {
			input.ConfigMapKeyRef = &KeyRefInput{Name: src.ConfigMapKeyRef.GetName(), Key: src.ConfigMapKeyRef.GetKey()}
		}
	case *consolev1.EnvVar_Value:
		input.Value = src.Value
	}
	return input
}

// validateEnvVars validates a list of proto EnvVar messages and converts them to EnvVarInput.
// Returns an error if any env var has an empty name.
func validateEnvVars(envVars []*consolev1.EnvVar) ([]EnvVarInput, error) {
	if len(envVars) == 0 {
		return nil, nil
	}
	result := make([]EnvVarInput, 0, len(envVars))
	for _, e := range envVars {
		if e.GetName() == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("env var name must not be empty"))
		}
		result = append(result, protoToEnvVarInput(e))
	}
	return result, nil
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
