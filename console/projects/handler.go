package projects

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "project"

// Handler implements the ProjectService.
type Handler struct {
	consolev1connect.UnimplementedProjectServiceHandler
	k8s *K8sClient
}

// NewHandler creates a new ProjectService handler.
func NewHandler(k8s *K8sClient) *Handler {
	return &Handler{k8s: k8s}
}

// ListProjects returns all projects the user has access to.
func (h *Handler) ListProjects(
	ctx context.Context,
	req *connect.Request[consolev1.ListProjectsRequest],
) (*connect.Response[consolev1.ListProjectsResponse], error) {
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	allProjects, err := h.k8s.ListProjects(ctx)
	if err != nil {
		return nil, mapK8sError(err)
	}

	now := time.Now()
	var result []*consolev1.Project
	for _, ns := range allProjects {
		shareUsers, _ := GetShareUsers(ns)
		shareGroups, _ := GetShareGroups(ns)
		activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
		activeGroups := secrets.ActiveGrantsMap(shareGroups, now)

		if err := CheckProjectListAccess(claims.Email, claims.Groups, activeUsers, activeGroups); err != nil {
			continue
		}

		userRole := rbac.BestRoleFromGrants(claims.Email, claims.Groups, activeUsers, activeGroups)
		result = append(result, buildProject(ns, shareUsers, shareGroups, userRole))
	}

	slog.InfoContext(ctx, "projects listed",
		slog.String("action", "project_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("total", len(result)),
	)

	return connect.NewResponse(&consolev1.ListProjectsResponse{
		Projects: result,
	}), nil
}

// GetProject retrieves a project by name.
func (h *Handler) GetProject(
	ctx context.Context,
	req *connect.Request[consolev1.GetProjectRequest],
) (*connect.Response[consolev1.GetProjectResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetProject(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareGroups, _ := GetShareGroups(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeGroups := secrets.ActiveGrantsMap(shareGroups, now)

	if err := CheckProjectReadAccess(claims.Email, claims.Groups, activeUsers, activeGroups); err != nil {
		slog.WarnContext(ctx, "project access denied",
			slog.String("action", "project_read_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Groups, activeUsers, activeGroups)

	slog.InfoContext(ctx, "project accessed",
		slog.String("action", "project_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.GetProjectResponse{
		Project: buildProject(ns, shareUsers, shareGroups, userRole),
	}), nil
}

// CreateProject creates a new project.
func (h *Handler) CreateProject(
	ctx context.Context,
	req *connect.Request[consolev1.CreateProjectRequest],
) (*connect.Response[consolev1.CreateProjectResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Check create access: user must be owner on at least one existing project
	allProjects, err := h.k8s.ListProjects(ctx)
	if err != nil {
		return nil, mapK8sError(err)
	}
	if err := CheckProjectCreateAccess(claims.Email, claims.Groups, allProjects); err != nil {
		slog.WarnContext(ctx, "project create denied",
			slog.String("action", "project_create_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Convert proto grants to annotation grants
	shareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	shareGroups := shareGrantsToAnnotations(req.Msg.GroupGrants)

	// Ensure creator is included as owner
	shareUsers = ensureCreatorOwner(shareUsers, claims.Email)

	_, err = h.k8s.CreateProject(ctx, req.Msg.Name, req.Msg.DisplayName, req.Msg.Description, shareUsers, shareGroups)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "project created",
		slog.String("action", "project_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateProjectResponse{
		Name: req.Msg.Name,
	}), nil
}

// UpdateProject updates project metadata.
func (h *Handler) UpdateProject(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateProjectRequest],
) (*connect.Response[consolev1.UpdateProjectResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetProject(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareGroups, _ := GetShareGroups(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeGroups := secrets.ActiveGrantsMap(shareGroups, now)

	if err := CheckProjectWriteAccess(claims.Email, claims.Groups, activeUsers, activeGroups); err != nil {
		slog.WarnContext(ctx, "project update denied",
			slog.String("action", "project_update_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	if _, err := h.k8s.UpdateProject(ctx, req.Msg.Name, req.Msg.DisplayName, req.Msg.Description); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "project updated",
		slog.String("action", "project_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateProjectResponse{}), nil
}

// DeleteProject deletes a managed namespace.
func (h *Handler) DeleteProject(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteProjectRequest],
) (*connect.Response[consolev1.DeleteProjectResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetProject(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareGroups, _ := GetShareGroups(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeGroups := secrets.ActiveGrantsMap(shareGroups, now)

	if err := CheckProjectDeleteAccess(claims.Email, claims.Groups, activeUsers, activeGroups); err != nil {
		slog.WarnContext(ctx, "project delete denied",
			slog.String("action", "project_delete_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	if err := h.k8s.DeleteProject(ctx, req.Msg.Name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "project deleted",
		slog.String("action", "project_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteProjectResponse{}), nil
}

// UpdateProjectSharing updates the sharing grants on a project.
func (h *Handler) UpdateProjectSharing(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateProjectSharingRequest],
) (*connect.Response[consolev1.UpdateProjectSharingResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetProject(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareGroups, _ := GetShareGroups(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeGroups := secrets.ActiveGrantsMap(shareGroups, now)

	if err := CheckProjectAdminAccess(claims.Email, claims.Groups, activeUsers, activeGroups); err != nil {
		slog.WarnContext(ctx, "project sharing update denied",
			slog.String("action", "project_sharing_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	newShareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	newShareGroups := shareGrantsToAnnotations(req.Msg.GroupGrants)

	updated, err := h.k8s.UpdateProjectSharing(ctx, req.Msg.Name, newShareUsers, newShareGroups)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "project sharing updated",
		slog.String("action", "project_sharing_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	updatedUsers, _ := GetShareUsers(updated)
	updatedGroups, _ := GetShareGroups(updated)
	updatedActiveUsers := secrets.ActiveGrantsMap(updatedUsers, now)
	updatedActiveGroups := secrets.ActiveGrantsMap(updatedGroups, now)
	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Groups, updatedActiveUsers, updatedActiveGroups)

	return connect.NewResponse(&consolev1.UpdateProjectSharingResponse{
		Project: buildProject(updated, updatedUsers, updatedGroups, userRole),
	}), nil
}

// buildProject creates a Project proto message from a namespace.
func buildProject(ns interface{ GetName() string }, shareUsers, shareGroups []secrets.AnnotationGrant, userRole rbac.Role) *consolev1.Project {
	p := &consolev1.Project{
		Name:        ns.GetName(),
		UserGrants:  annotationGrantsToProto(shareUsers),
		GroupGrants: annotationGrantsToProto(shareGroups),
		UserRole:    consolev1.Role(userRole),
	}

	// Type-assert to get annotations for metadata
	type annotated interface {
		GetAnnotations() map[string]string
	}
	if a, ok := ns.(annotated); ok {
		annotations := a.GetAnnotations()
		if annotations != nil {
			p.DisplayName = annotations[DisplayNameAnnotation]
			p.Description = annotations[secrets.DescriptionAnnotation]
		}
	}

	return p
}

// shareGrantsToAnnotations converts proto ShareGrant slices to annotation grants.
func shareGrantsToAnnotations(grants []*consolev1.ShareGrant) []secrets.AnnotationGrant {
	result := make([]secrets.AnnotationGrant, 0, len(grants))
	for _, g := range grants {
		if g.Principal != "" {
			ag := secrets.AnnotationGrant{
				Principal: g.Principal,
				Role:      strings.ToLower(g.Role.String()[len("ROLE_"):]),
			}
			if g.Nbf != nil {
				nbf := *g.Nbf
				ag.Nbf = &nbf
			}
			if g.Exp != nil {
				exp := *g.Exp
				ag.Exp = &exp
			}
			result = append(result, ag)
		}
	}
	return result
}

// annotationGrantsToProto converts annotation grants to proto ShareGrant slices.
func annotationGrantsToProto(grants []secrets.AnnotationGrant) []*consolev1.ShareGrant {
	result := make([]*consolev1.ShareGrant, 0, len(grants))
	for _, g := range grants {
		sg := &consolev1.ShareGrant{
			Principal: g.Principal,
			Role:      protoRoleFromString(g.Role),
		}
		if g.Nbf != nil {
			nbf := *g.Nbf
			sg.Nbf = &nbf
		}
		if g.Exp != nil {
			exp := *g.Exp
			sg.Exp = &exp
		}
		result = append(result, sg)
	}
	return result
}

func protoRoleFromString(s string) consolev1.Role {
	switch strings.ToLower(s) {
	case "viewer":
		return consolev1.Role_ROLE_VIEWER
	case "editor":
		return consolev1.Role_ROLE_EDITOR
	case "owner":
		return consolev1.Role_ROLE_OWNER
	default:
		return consolev1.Role_ROLE_UNSPECIFIED
	}
}

// ensureCreatorOwner ensures the creator email is in the share-users list as owner.
func ensureCreatorOwner(shareUsers []secrets.AnnotationGrant, email string) []secrets.AnnotationGrant {
	emailLower := strings.ToLower(email)
	for _, g := range shareUsers {
		if strings.ToLower(g.Principal) == emailLower && strings.ToLower(g.Role) == "owner" {
			return shareUsers
		}
	}
	return append(shareUsers, secrets.AnnotationGrant{Principal: email, Role: "owner"})
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
	if errors.IsUnauthorized(err) {
		return connect.NewError(connect.CodeUnauthenticated, err)
	}
	if errors.IsBadRequest(err) {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	// Check for "not managed by" errors from our K8s layer
	if strings.Contains(err.Error(), "not managed by") {
		return connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
