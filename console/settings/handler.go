package settings

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "project-settings"

// ProjectResolver resolves project namespace grants for access checks.
type ProjectResolver interface {
	GetProjectGrants(ctx context.Context, project string) (shareUsers, shareRoles map[string]string, err error)
}

// Handler implements the ProjectSettingsService.
type Handler struct {
	consolev1connect.UnimplementedProjectSettingsServiceHandler
	k8s             *K8sClient
	projectResolver ProjectResolver
}

// NewHandler creates a ProjectSettingsService handler.
func NewHandler(k8s *K8sClient, projectResolver ProjectResolver) *Handler {
	return &Handler{k8s: k8s, projectResolver: projectResolver}
}

// GetProjectSettings returns the settings for a project.
func (h *Handler) GetProjectSettings(
	ctx context.Context,
	req *connect.Request[consolev1.GetProjectSettingsRequest],
) (*connect.Response[consolev1.GetProjectSettingsResponse], error) {
	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Check RBAC: requires PROJECT_SETTINGS_READ via project cascade grants
	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionProjectSettingsRead); err != nil {
		return nil, err
	}

	settings, err := h.k8s.GetSettings(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "project settings read",
		slog.String("action", "project_settings_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.GetProjectSettingsResponse{
		Settings: settings,
	}), nil
}

// UpdateProjectSettings updates the settings for a project.
func (h *Handler) UpdateProjectSettings(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateProjectSettingsRequest],
) (*connect.Response[consolev1.UpdateProjectSettingsResponse], error) {
	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if req.Msg.Settings == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("settings is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Check RBAC: requires PROJECT_SETTINGS_WRITE via project cascade grants (Owner-only)
	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionProjectSettingsWrite); err != nil {
		slog.WarnContext(ctx, "project settings update denied",
			slog.String("action", "project_settings_update_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", project),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Ensure the project field matches the request
	settings := req.Msg.Settings
	settings.Project = project

	result, err := h.k8s.UpdateSettings(ctx, settings)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "project settings updated",
		slog.String("action", "project_settings_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Bool("deployments_enabled", result.DeploymentsEnabled),
	)

	return connect.NewResponse(&consolev1.UpdateProjectSettingsResponse{
		Settings: result,
	}), nil
}

// checkProjectAccess verifies that the user has the given permission via project grants.
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
	return rbac.CheckAccessGrants(claims.Email, claims.Roles, users, roles, permission)
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
