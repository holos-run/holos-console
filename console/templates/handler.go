package templates

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	"connectrpc.com/connect"
	"cuelang.org/go/cue/parser"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "deployment-template"

// dnsLabelRe validates template names as DNS labels.
var dnsLabelRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// ProjectResolver resolves project namespace grants for access checks.
type ProjectResolver interface {
	GetProjectGrants(ctx context.Context, project string) (shareUsers, shareRoles map[string]string, err error)
}

// Handler implements the DeploymentTemplateService.
type Handler struct {
	consolev1connect.UnimplementedDeploymentTemplateServiceHandler
	k8s             *K8sClient
	projectResolver ProjectResolver
}

// NewHandler creates a DeploymentTemplateService handler.
func NewHandler(k8s *K8sClient, projectResolver ProjectResolver) *Handler {
	return &Handler{k8s: k8s, projectResolver: projectResolver}
}

// ListDeploymentTemplates returns all templates in a project.
func (h *Handler) ListDeploymentTemplates(
	ctx context.Context,
	req *connect.Request[consolev1.ListDeploymentTemplatesRequest],
) (*connect.Response[consolev1.ListDeploymentTemplatesResponse], error) {
	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentTemplatesList); err != nil {
		return nil, err
	}

	cms, err := h.k8s.ListTemplates(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}

	templates := make([]*consolev1.DeploymentTemplate, 0, len(cms))
	for _, cm := range cms {
		templates = append(templates, configMapToTemplate(&cm, project))
	}

	slog.InfoContext(ctx, "deployment templates listed",
		slog.String("action", "deployment_templates_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(templates)),
	)

	return connect.NewResponse(&consolev1.ListDeploymentTemplatesResponse{
		Templates: templates,
	}), nil
}

// GetDeploymentTemplate returns a single template by name.
func (h *Handler) GetDeploymentTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.GetDeploymentTemplateRequest],
) (*connect.Response[consolev1.GetDeploymentTemplateResponse], error) {
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

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentTemplatesRead); err != nil {
		return nil, err
	}

	cm, err := h.k8s.GetTemplate(ctx, project, name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "deployment template read",
		slog.String("action", "deployment_template_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
	)

	return connect.NewResponse(&consolev1.GetDeploymentTemplateResponse{
		Template: configMapToTemplate(cm, project),
	}), nil
}

// CreateDeploymentTemplate creates a new template.
func (h *Handler) CreateDeploymentTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.CreateDeploymentTemplateRequest],
) (*connect.Response[consolev1.CreateDeploymentTemplateResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if err := validateTemplateName(name); err != nil {
		return nil, err
	}
	if err := validateCueSyntax(req.Msg.CueTemplate); err != nil {
		return nil, err
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentTemplatesWrite); err != nil {
		return nil, err
	}

	_, err := h.k8s.CreateTemplate(ctx, project, name, req.Msg.DisplayName, req.Msg.Description, req.Msg.CueTemplate)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "deployment template created",
		slog.String("action", "deployment_template_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateDeploymentTemplateResponse{
		Name: name,
	}), nil
}

// UpdateDeploymentTemplate updates an existing template.
func (h *Handler) UpdateDeploymentTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateDeploymentTemplateRequest],
) (*connect.Response[consolev1.UpdateDeploymentTemplateResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	// Validate CUE syntax if a new template is provided
	if req.Msg.CueTemplate != nil {
		if err := validateCueSyntax(*req.Msg.CueTemplate); err != nil {
			return nil, err
		}
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentTemplatesWrite); err != nil {
		return nil, err
	}

	_, err := h.k8s.UpdateTemplate(ctx, project, name, req.Msg.DisplayName, req.Msg.Description, req.Msg.CueTemplate)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "deployment template updated",
		slog.String("action", "deployment_template_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateDeploymentTemplateResponse{}), nil
}

// DeleteDeploymentTemplate deletes a template.
func (h *Handler) DeleteDeploymentTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteDeploymentTemplateRequest],
) (*connect.Response[consolev1.DeleteDeploymentTemplateResponse], error) {
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

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentTemplatesDelete); err != nil {
		return nil, err
	}

	if err := h.k8s.DeleteTemplate(ctx, project, name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "deployment template deleted",
		slog.String("action", "deployment_template_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteDeploymentTemplateResponse{}), nil
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
	return rbac.CheckCascadeAccess(claims.Email, claims.Roles, users, roles, permission, rbac.ProjectCascadeTemplatePerms)
}

// validateTemplateName checks that the name is a valid DNS label.
func validateTemplateName(name string) error {
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

// validateCueSyntax parses the CUE source to verify it is syntactically valid.
func validateCueSyntax(source string) error {
	_, err := parser.ParseFile("template.cue", source)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid CUE syntax: %w", err))
	}
	return nil
}

// configMapToTemplate converts a Kubernetes ConfigMap to a DeploymentTemplate protobuf message.
func configMapToTemplate(cm *corev1.ConfigMap, project string) *consolev1.DeploymentTemplate {
	return &consolev1.DeploymentTemplate{
		Name:        cm.Name,
		Project:     project,
		DisplayName: cm.Annotations[DisplayNameAnnotation],
		Description: cm.Annotations[DescriptionAnnotation],
		CueTemplate: cm.Data[CueTemplateKey],
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
