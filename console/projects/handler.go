package projects

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "project"

// OrgResolver resolves organization-level grants for access checks.
type OrgResolver interface {
	GetOrgGrants(ctx context.Context, org string) (shareUsers, shareGroups map[string]string, err error)
}

// Handler implements the ProjectService.
type Handler struct {
	consolev1connect.UnimplementedProjectServiceHandler
	k8s         *K8sClient
	orgResolver OrgResolver
}

// NewHandler creates a new ProjectService handler.
func NewHandler(k8s *K8sClient, orgResolver OrgResolver) *Handler {
	return &Handler{k8s: k8s, orgResolver: orgResolver}
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

	allProjects, err := h.k8s.ListProjects(ctx, req.Msg.Organization)
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

		// Check project-level grants first, then fall back to org grants
		if err := CheckProjectListAccess(claims.Email, claims.Groups, activeUsers, activeGroups); err != nil {
			orgUsers, orgGroups := h.resolveOrgGrants(ctx, GetOrganization(ns))
			if err := h.checkAccessWithOrg(claims.Email, claims.Groups, activeUsers, activeGroups, orgUsers, orgGroups, rbac.PermissionProjectsList); err != nil {
				continue
			}
		}

		userRole := h.bestRoleWithOrg(claims.Email, claims.Groups, activeUsers, activeGroups, ns)
		result = append(result, h.buildProject(ns, shareUsers, shareGroups, userRole))
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

	orgUsers, orgGroups := h.resolveOrgGrants(ctx, GetOrganization(ns))
	if err := h.checkAccessWithOrg(claims.Email, claims.Groups, activeUsers, activeGroups, orgUsers, orgGroups, rbac.PermissionProjectsRead); err != nil {
		slog.WarnContext(ctx, "project access denied",
			slog.String("action", "project_read_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	userRole := h.bestRoleWithOrg(claims.Email, claims.Groups, activeUsers, activeGroups, ns)

	slog.InfoContext(ctx, "project accessed",
		slog.String("action", "project_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.GetProjectResponse{
		Project: h.buildProject(ns, shareUsers, shareGroups, userRole),
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

	if req.Msg.Organization == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization is required"))
	}

	// Check create access: user must be owner on at least one existing project
	// or have owner grant on the organization
	allProjects, err := h.k8s.ListProjects(ctx, "")
	if err != nil {
		return nil, mapK8sError(err)
	}
	if err := CheckProjectCreateAccess(claims.Email, claims.Groups, allProjects); err != nil {
		// Fall back to org-level grants for create permission
		orgUsers, orgGroups := h.resolveOrgGrants(ctx, req.Msg.Organization)
		if err := rbac.CheckAccessGrants(claims.Email, claims.Groups, orgUsers, orgGroups, rbac.PermissionProjectsCreate); err != nil {
			slog.WarnContext(ctx, "project create denied",
				slog.String("action", "project_create_denied"),
				slog.String("resource_type", auditResourceType),
				slog.String("project", req.Msg.Name),
				slog.String("sub", claims.Sub),
				slog.String("email", claims.Email),
			)
			return nil, err
		}
	}

	// Convert proto grants to annotation grants
	shareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	shareGroups := shareGrantsToAnnotations(req.Msg.GroupGrants)

	// Ensure creator is included as owner
	shareUsers = ensureCreatorOwner(shareUsers, claims.Email)

	_, err = h.k8s.CreateProject(ctx, req.Msg.Name, req.Msg.DisplayName, req.Msg.Description, req.Msg.Organization, shareUsers, shareGroups)
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

	orgUsers, orgGroups := h.resolveOrgGrants(ctx, GetOrganization(ns))
	if err := h.checkAccessWithOrg(claims.Email, claims.Groups, activeUsers, activeGroups, orgUsers, orgGroups, rbac.PermissionProjectsWrite); err != nil {
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

	orgUsers, orgGroups := h.resolveOrgGrants(ctx, GetOrganization(ns))
	if err := h.checkAccessWithOrg(claims.Email, claims.Groups, activeUsers, activeGroups, orgUsers, orgGroups, rbac.PermissionProjectsDelete); err != nil {
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

	orgUsers, orgGroups := h.resolveOrgGrants(ctx, GetOrganization(ns))
	if err := h.checkAccessWithOrg(claims.Email, claims.Groups, activeUsers, activeGroups, orgUsers, orgGroups, rbac.PermissionProjectsAdmin); err != nil {
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
		Project: h.buildProject(updated, updatedUsers, updatedGroups, userRole),
	}), nil
}

// buildProject creates a Project proto message from a namespace.
func (h *Handler) buildProject(ns interface{ GetName() string }, shareUsers, shareGroups []secrets.AnnotationGrant, userRole rbac.Role) *consolev1.Project {
	p := &consolev1.Project{
		Name:        h.k8s.Resolver.ProjectFromNamespace(ns.GetName()),
		UserGrants:  annotationGrantsToProto(shareUsers),
		GroupGrants: annotationGrantsToProto(shareGroups),
		UserRole:    consolev1.Role(userRole),
	}

	// Type-assert to get annotations and labels for metadata
	type annotated interface {
		GetAnnotations() map[string]string
	}
	type labeled interface {
		GetLabels() map[string]string
	}
	if a, ok := ns.(annotated); ok {
		annotations := a.GetAnnotations()
		if annotations != nil {
			p.DisplayName = annotations[DisplayNameAnnotation]
			p.Description = annotations[secrets.DescriptionAnnotation]
		}
	}
	if l, ok := ns.(labeled); ok {
		labels := l.GetLabels()
		if labels != nil {
			p.Organization = labels[resolver.OrganizationLabel]
		}
	}

	return p
}

// resolveOrgGrants returns the active grant maps for the given organization.
// Returns nil maps if no org resolver is configured or org is empty.
func (h *Handler) resolveOrgGrants(ctx context.Context, org string) (map[string]string, map[string]string) {
	if h.orgResolver == nil || org == "" {
		return nil, nil
	}
	users, groups, err := h.orgResolver.GetOrgGrants(ctx, org)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve org grants",
			slog.String("organization", org),
			slog.Any("error", err),
		)
		return nil, nil
	}
	return users, groups
}

// checkAccessWithOrg checks project-level grants first, then org-level grants.
func (h *Handler) checkAccessWithOrg(
	email string,
	groups []string,
	projUsers, projGroups map[string]string,
	orgUsers, orgGroups map[string]string,
	permission rbac.Permission,
) error {
	// 1. Check project grants
	if err := rbac.CheckAccessGrants(email, groups, projUsers, projGroups, permission); err == nil {
		return nil
	}
	// 2. Check org grants
	if orgUsers != nil || orgGroups != nil {
		if err := rbac.CheckAccessGrants(email, groups, orgUsers, orgGroups, permission); err == nil {
			return nil
		}
	}
	return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
}

// bestRoleWithOrg returns the best role from project grants and org grants.
func (h *Handler) bestRoleWithOrg(email string, groups []string, projUsers, projGroups map[string]string, ns *corev1.Namespace) rbac.Role {
	projRole := rbac.BestRoleFromGrants(email, groups, projUsers, projGroups)
	orgUsers, orgGroups := h.resolveOrgGrants(context.Background(), GetOrganization(ns))
	orgRole := rbac.BestRoleFromGrants(email, groups, orgUsers, orgGroups)
	if rbac.RoleLevel(orgRole) > rbac.RoleLevel(projRole) {
		return orgRole
	}
	return projRole
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
