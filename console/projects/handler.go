package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "project"

// OrgResolver resolves organization-level grants for access checks.
type OrgResolver interface {
	GetOrgGrants(ctx context.Context, org string) (users, roles map[string]string, err error)
}

// OrgDefaultShareResolver is an optional interface that an OrgResolver can also
// implement to provide default sharing grants from the organization.  These
// defaults are merged into new projects created within the organization.
type OrgDefaultShareResolver interface {
	GetOrgDefaultGrants(ctx context.Context, org string) (defaultUsers, defaultRoles []secrets.AnnotationGrant, err error)
}

// MandatoryTemplateApplier renders and applies all mandatory platform templates
// into a newly created project namespace.
type MandatoryTemplateApplier interface {
	ApplyMandatoryOrgTemplates(ctx context.Context, org, project, projectNamespace string, claims *rpc.Claims) error
}

// Handler implements the ProjectService.
type Handler struct {
	consolev1connect.UnimplementedProjectServiceHandler
	k8s                      *K8sClient
	orgResolver              OrgResolver
	mandatoryTemplateApplier MandatoryTemplateApplier
}

// NewHandler creates a new ProjectService handler.
func NewHandler(k8s *K8sClient, orgResolver OrgResolver) *Handler {
	return &Handler{k8s: k8s, orgResolver: orgResolver}
}

// WithMandatoryTemplateApplier sets the MandatoryTemplateApplier for the handler.
// When set, mandatory platform templates are applied to new project namespaces at creation time.
func (h *Handler) WithMandatoryTemplateApplier(applier MandatoryTemplateApplier) *Handler {
	h.mandatoryTemplateApplier = applier
	return h
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

	// Resolve parent namespace filter when parent_type+parent_name are set.
	var parentNs string
	if req.Msg.ParentType != consolev1.ParentType_PARENT_TYPE_UNSPECIFIED && req.Msg.ParentName != "" {
		var err error
		parentNs, err = h.resolveParentNS(req.Msg.ParentType, req.Msg.ParentName)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}

	allProjects, err := h.k8s.ListProjects(ctx, req.Msg.Organization, parentNs)
	if err != nil {
		return nil, mapK8sError(err)
	}

	now := time.Now()
	var result []*consolev1.Project
	for _, ns := range allProjects {
		shareUsers, _ := GetShareUsers(ns)
		shareRoles, _ := GetShareRoles(ns)
		activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
		activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

		// Check project-level grants
		if err := CheckProjectListAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
			if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsList); err != nil {
				continue
			}
		}

		userRole := h.bestRoleWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, ns)
		result = append(result, h.buildProject(ns, shareUsers, shareRoles, userRole))
	}

	slog.InfoContext(ctx, "projects listed",
		slog.String("action", "project_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Organization),
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
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	org := GetOrganization(ns)
	if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsRead); err != nil {
		slog.WarnContext(ctx, "project access denied",
			slog.String("action", "project_read_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("organization", org),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	userRole := h.bestRoleWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, ns)

	slog.InfoContext(ctx, "project accessed",
		slog.String("action", "project_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("organization", org),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.GetProjectResponse{
		Project: h.buildProject(ns, shareUsers, shareRoles, userRole),
	}), nil
}

// CreateProject creates a new project.
// When name is empty, it is derived from display_name using slug generation.
// Uses bounded retry with random suffix on namespace collision.
func (h *Handler) CreateProject(
	ctx context.Context,
	req *connect.Request[consolev1.CreateProjectRequest],
) (*connect.Response[consolev1.CreateProjectResponse], error) {
	// Derive name from display_name when not explicitly provided.
	name := req.Msg.Name
	if name == "" {
		if req.Msg.DisplayName == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name or display_name is required"))
		}
		prefix := h.k8s.Resolver.NamespacePrefix + h.k8s.Resolver.ProjectPrefix
		exists := func(ctx context.Context, nsName string) (bool, error) {
			return h.k8s.NamespaceExists(ctx, nsName)
		}
		generated, err := v1alpha2.GenerateIdentifier(ctx, req.Msg.DisplayName, prefix, exists)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("generating project identifier: %w", err))
		}
		name = generated
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Resolve the immediate parent namespace.
	// When no explicit parent is specified, check the org's default folder.
	// Falls back to org root if no default folder is configured or the folder is missing.
	parentName := req.Msg.ParentName
	parentType := req.Msg.ParentType
	if parentName == "" {
		parentName, parentType = h.resolveDefaultParent(ctx, req.Msg.Organization)
	}
	if parentType == consolev1.ParentType_PARENT_TYPE_UNSPECIFIED {
		parentType = consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	}
	parentNs, err := h.resolveParentNS(parentType, parentName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("resolving parent: %w", err))
	}

	// Check create access: user must be owner on at least one existing project
	// or have owner grant on the organization
	allProjects, err := h.k8s.ListProjects(ctx, "", "")
	if err != nil {
		return nil, mapK8sError(err)
	}
	if err := CheckProjectCreateAccess(claims.Email, claims.Roles, allProjects); err != nil {
		// Fall back to org-level grants for create permission
		orgUsers, orgRoles := h.resolveOrgGrants(ctx, req.Msg.Organization)
		if err := rbac.CheckAccessGrants(claims.Email, claims.Roles, orgUsers, orgRoles, rbac.PermissionProjectsCreate); err != nil {
			slog.WarnContext(ctx, "project create denied",
				slog.String("action", "project_create_denied"),
				slog.String("resource_type", auditResourceType),
				slog.String("project", name),
				slog.String("organization", req.Msg.Organization),
				slog.String("sub", claims.Sub),
				slog.String("email", claims.Email),
			)
			return nil, err
		}
	}

	// Convert proto grants to annotation grants
	shareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	shareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)

	// Merge organization-level default sharing grants (if the resolver supports it).
	// Request-supplied grants override defaults for the same principal via DeduplicateGrants
	// (highest role wins, so explicit grants at a higher role shadow lower defaults).
	// Also copy org defaults as the project's default sharing so new secrets inherit them.
	var defaultShareUsers, defaultShareRoles []secrets.AnnotationGrant
	if req.Msg.Organization != "" {
		if ds, ok := h.orgResolver.(OrgDefaultShareResolver); ok {
			orgDefaultUsers, orgDefaultRoles, err := ds.GetOrgDefaultGrants(ctx, req.Msg.Organization)
			if err != nil {
				slog.WarnContext(ctx, "failed to resolve org default grants, proceeding without them",
					slog.String("organization", req.Msg.Organization),
					slog.Any("error", err),
				)
			} else {
				// Concatenate: request grants first so they win over defaults for the same principal.
				shareUsers = secrets.DeduplicateGrants(append(shareUsers, orgDefaultUsers...))
				shareRoles = secrets.DeduplicateGrants(append(shareRoles, orgDefaultRoles...))
				// Copy org defaults as project defaults so new secrets also inherit them.
				defaultShareUsers = orgDefaultUsers
				defaultShareRoles = orgDefaultRoles
			}
		}
	}

	// Ensure creator is included as owner
	shareUsers = ensureCreatorOwner(shareUsers, claims.Email)

	_, err = h.k8s.CreateProject(ctx, name, req.Msg.DisplayName, req.Msg.Description, req.Msg.Organization, parentNs, claims.Email, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Apply mandatory platform templates into the new project namespace.
	// On failure, attempt a best-effort cleanup of the project namespace to
	// avoid leaving orphaned resources, then return the error.
	if h.mandatoryTemplateApplier != nil && req.Msg.Organization != "" {
		projectNamespace := h.k8s.Resolver.ProjectNamespace(name)
		if err := h.mandatoryTemplateApplier.ApplyMandatoryOrgTemplates(ctx, req.Msg.Organization, name, projectNamespace, claims); err != nil {
			slog.ErrorContext(ctx, "mandatory platform template apply failed, cleaning up project",
				slog.String("project", name),
				slog.String("organization", req.Msg.Organization),
				slog.Any("error", err),
			)
			if cleanupErr := h.k8s.DeleteProject(ctx, name); cleanupErr != nil {
				slog.WarnContext(ctx, "failed to clean up project after mandatory template apply failure",
					slog.String("project", name),
					slog.Any("cleanup_error", cleanupErr),
				)
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("applying mandatory platform templates to project %q: %w", name, err))
		}
	}

	slog.InfoContext(ctx, "project created",
		slog.String("action", "project_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", name),
		slog.String("organization", req.Msg.Organization),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateProjectResponse{
		Name: name,
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
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	org := GetOrganization(ns)
	if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsWrite); err != nil {
		slog.WarnContext(ctx, "project update denied",
			slog.String("action", "project_update_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("organization", org),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Handle reparenting if parent_type and parent_name are set.
	if req.Msg.ParentType != nil && req.Msg.ParentName != nil {
		if err := h.reparentProject(ctx, ns, claims, *req.Msg.ParentType, *req.Msg.ParentName); err != nil {
			return nil, err
		}
	}

	if _, err := h.k8s.UpdateProject(ctx, req.Msg.Name, req.Msg.DisplayName, req.Msg.Description); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "project updated",
		slog.String("action", "project_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("organization", org),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateProjectResponse{}), nil
}

// reparentProject validates and executes a project reparent operation.
// Checks PERMISSION_REPARENT on both source and destination parents.
// Projects have no children so no depth or cycle checks are needed.
func (h *Handler) reparentProject(
	ctx context.Context,
	ns *corev1.Namespace,
	claims *rpc.Claims,
	newParentType consolev1.ParentType,
	newParentName string,
) error {
	projectName := ns.Labels[v1alpha2.LabelProject]
	org := GetOrganization(ns)

	// Resolve new parent namespace.
	newParentNs, err := h.resolveParentNS(newParentType, newParentName)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Resolve current parent namespace from the project's label.
	currentParentNs := ns.Labels[v1alpha2.AnnotationParent]
	if currentParentNs == "" {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("project %q is missing parent label", projectName))
	}

	// No-op: same parent, return success without a K8s write.
	if currentParentNs == newParentNs {
		return nil
	}

	// Verify new parent namespace exists.
	newParentNamespace, err := h.k8s.GetNamespace(ctx, newParentNs)
	if err != nil {
		return mapK8sError(err)
	}

	// Check PERMISSION_REPARENT on the current (source) parent.
	sourceNs, err := h.k8s.GetNamespace(ctx, currentParentNs)
	if err != nil {
		return mapK8sError(err)
	}
	if err := h.checkReparentAccess(ctx, claims, sourceNs, org, "source"); err != nil {
		slog.WarnContext(ctx, "project reparent denied on source",
			slog.String("action", "project_reparent_denied_source"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", projectName),
			slog.String("source_parent", currentParentNs),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return err
	}

	// Check PERMISSION_REPARENT on the new (destination) parent.
	// Use the destination parent's org for cascade, not the source org.
	destOrg := newParentNamespace.Labels[v1alpha2.LabelOrganization]
	if err := h.checkReparentAccess(ctx, claims, newParentNamespace, destOrg, "destination"); err != nil {
		slog.WarnContext(ctx, "project reparent denied on destination",
			slog.String("action", "project_reparent_denied_destination"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", projectName),
			slog.String("dest_parent", newParentNs),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return err
	}

	// Execute the reparent: update the parent label.
	if _, err := h.k8s.UpdateParentLabel(ctx, projectName, newParentNs); err != nil {
		return mapK8sError(err)
	}

	slog.InfoContext(ctx, "project reparented",
		slog.String("action", "project_reparent"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", projectName),
		slog.String("from_parent", currentParentNs),
		slog.String("to_parent", newParentNs),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return nil
}

// checkReparentAccess verifies that the user has PERMISSION_REPARENT on the given
// parent namespace. Uses direct grants on the parent plus org-level cascade via
// ReparentCascadePerms.
func (h *Handler) checkReparentAccess(ctx context.Context, claims *rpc.Claims, parentNs *corev1.Namespace, org, direction string) error {
	shareUsers, _ := GetShareUsers(parentNs)
	shareRoles, _ := GetShareRoles(parentNs)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	// Check direct grants on the parent resource.
	if err := rbac.CheckAccessGrants(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionReparent); err == nil {
		return nil
	}

	// Check org-level cascade: org OWNERs get REPARENT on all children via ReparentCascadePerms.
	if org != "" {
		orgUsers, orgRoles := h.resolveOrgGrants(ctx, org)
		if err := rbac.CheckCascadeAccess(claims.Email, claims.Roles, orgUsers, orgRoles, rbac.PermissionReparent, rbac.ReparentCascadePerms); err == nil {
			return nil
		}
	}

	return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: reparent authorization denied on %s parent", direction))
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
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	org := GetOrganization(ns)
	if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsDelete); err != nil {
		slog.WarnContext(ctx, "project delete denied",
			slog.String("action", "project_delete_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("organization", org),
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
		slog.String("organization", org),
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
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	org := GetOrganization(ns)
	if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsAdmin); err != nil {
		slog.WarnContext(ctx, "project sharing update denied",
			slog.String("action", "project_sharing_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("organization", org),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	newShareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	newShareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)

	updated, err := h.k8s.UpdateProjectSharing(ctx, req.Msg.Name, newShareUsers, newShareRoles)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "project sharing updated",
		slog.String("action", "project_sharing_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("organization", org),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	updatedUsers, _ := GetShareUsers(updated)
	updatedRoles, _ := GetShareRoles(updated)
	updatedActiveUsers := secrets.ActiveGrantsMap(updatedUsers, now)
	updatedActiveGroups := secrets.ActiveGrantsMap(updatedRoles, now)
	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Roles, updatedActiveUsers, updatedActiveGroups)

	return connect.NewResponse(&consolev1.UpdateProjectSharingResponse{
		Project: h.buildProject(updated, updatedUsers, updatedRoles, userRole),
	}), nil
}

// UpdateProjectDefaultSharing updates the default sharing grants on a project.
func (h *Handler) UpdateProjectDefaultSharing(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateProjectDefaultSharingRequest],
) (*connect.Response[consolev1.UpdateProjectDefaultSharingResponse], error) {
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
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	org := GetOrganization(ns)
	if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsAdmin); err != nil {
		slog.WarnContext(ctx, "project default sharing update denied",
			slog.String("action", "project_default_sharing_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("organization", org),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	newDefaultUsers := shareGrantsToAnnotations(req.Msg.DefaultUserGrants)
	newDefaultRoles := shareGrantsToAnnotations(req.Msg.DefaultRoleGrants)

	updated, err := h.k8s.UpdateProjectDefaultSharing(ctx, req.Msg.Name, newDefaultUsers, newDefaultRoles)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "project default sharing updated",
		slog.String("action", "project_default_sharing_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("organization", org),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	updatedShareUsers, _ := GetShareUsers(updated)
	updatedShareRoles, _ := GetShareRoles(updated)
	updatedActiveUsers := secrets.ActiveGrantsMap(updatedShareUsers, now)
	updatedActiveRoles := secrets.ActiveGrantsMap(updatedShareRoles, now)
	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Roles, updatedActiveUsers, updatedActiveRoles)

	return connect.NewResponse(&consolev1.UpdateProjectDefaultSharingResponse{
		Project: h.buildProject(updated, updatedShareUsers, updatedShareRoles, userRole),
	}), nil
}

// GetProjectRaw retrieves the full Kubernetes Namespace object as verbatim JSON.
func (h *Handler) GetProjectRaw(
	ctx context.Context,
	req *connect.Request[consolev1.GetProjectRawRequest],
) (*connect.Response[consolev1.GetProjectRawResponse], error) {
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
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	org := GetOrganization(ns)
	if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsRead); err != nil {
		slog.WarnContext(ctx, "project raw access denied",
			slog.String("action", "project_raw_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("organization", org),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	slog.InfoContext(ctx, "project raw accessed",
		slog.String("action", "project_raw"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("organization", org),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	// Set apiVersion and kind (not populated by client-go on fetched objects)
	ns.APIVersion = "v1"
	ns.Kind = "Namespace"

	raw, err := json.Marshal(ns)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("marshaling namespace to JSON: %w", err))
	}

	return connect.NewResponse(&consolev1.GetProjectRawResponse{
		Raw: string(raw),
	}), nil
}

// CheckProjectIdentifier checks whether a proposed project identifier is available.
func (h *Handler) CheckProjectIdentifier(
	ctx context.Context,
	req *connect.Request[consolev1.CheckProjectIdentifierRequest],
) (*connect.Response[consolev1.CheckProjectIdentifierResponse], error) {
	if req.Msg.Identifier == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("identifier is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	prefix := h.k8s.Resolver.NamespacePrefix + h.k8s.Resolver.ProjectPrefix
	exists := func(ctx context.Context, nsName string) (bool, error) {
		return h.k8s.NamespaceExists(ctx, nsName)
	}

	result, err := v1alpha2.CheckIdentifier(ctx, req.Msg.Identifier, prefix, exists)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("checking identifier: %w", err))
	}

	slog.InfoContext(ctx, "project identifier checked",
		slog.String("action", "project_check_identifier"),
		slog.String("resource_type", auditResourceType),
		slog.String("identifier", req.Msg.Identifier),
		slog.Bool("available", result.Available),
		slog.String("suggested", result.SuggestedIdentifier),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CheckProjectIdentifierResponse{
		Available:           result.Available,
		SuggestedIdentifier: result.SuggestedIdentifier,
	}), nil
}

// buildProject creates a Project proto message from a namespace.
func (h *Handler) buildProject(ns *corev1.Namespace, shareUsers, shareRoles []secrets.AnnotationGrant, userRole rbac.Role) *consolev1.Project {
	p := &consolev1.Project{
		UserGrants: annotationGrantsToProto(shareUsers),
		RoleGrants: annotationGrantsToProto(shareRoles),
		UserRole:   consolev1.Role(userRole),
	}

	if ns.Labels != nil {
		p.Organization = ns.Labels[v1alpha2.LabelOrganization]
		p.Name = ns.Labels[v1alpha2.LabelProject]

		// Derive parent info from the parent label.
		parentNs := ns.Labels[v1alpha2.AnnotationParent]
		if parentNs != "" {
			kind, name, err := h.k8s.Resolver.ResourceTypeFromNamespace(parentNs)
			if err == nil {
				p.ParentName = name
				switch kind {
				case v1alpha2.ResourceTypeOrganization:
					p.ParentType = consolev1.ParentType_PARENT_TYPE_ORGANIZATION
				case v1alpha2.ResourceTypeFolder:
					p.ParentType = consolev1.ParentType_PARENT_TYPE_FOLDER
				}
			}
		}
	}

	// Fallback: derive project name from namespace if label is missing (pre-label namespaces)
	if p.Name == "" {
		name, err := h.k8s.Resolver.ProjectFromNamespace(ns.GetName())
		if err != nil {
			slog.Warn("project namespace missing label and prefix mismatch",
				slog.String("namespace", ns.GetName()),
				slog.String("label", v1alpha2.LabelProject),
				slog.Any("error", err),
			)
		} else {
			p.Name = name
			slog.Warn("project namespace missing label, falling back to namespace parsing",
				slog.String("namespace", ns.GetName()),
				slog.String("label", v1alpha2.LabelProject),
			)
		}
	}

	if ns.Annotations != nil {
		p.DisplayName = ns.Annotations[v1alpha2.AnnotationDisplayName]
		p.Description = ns.Annotations[v1alpha2.AnnotationDescription]
		p.CreatorEmail = ns.Annotations[v1alpha2.AnnotationCreatorEmail]
	}

	if defaultUsers, err := GetDefaultShareUsers(ns); err == nil {
		p.DefaultUserGrants = annotationGrantsToProto(defaultUsers)
	}
	if defaultRoles, err := GetDefaultShareRoles(ns); err == nil {
		p.DefaultRoleGrants = annotationGrantsToProto(defaultRoles)
	}
	p.CreatedAt = ns.CreationTimestamp.UTC().Format(time.RFC3339)

	return p
}

// resolveDefaultParent determines the default parent for a new project when
// no explicit parent is specified. It reads the org's default-folder annotation
// and, if the referenced folder exists, returns it as the parent. Otherwise
// it falls back to the organization as the parent (legacy behavior).
func (h *Handler) resolveDefaultParent(ctx context.Context, org string) (string, consolev1.ParentType) {
	if org == "" {
		return org, consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	}

	// Look up the org namespace to read the default-folder annotation.
	orgNsName := h.k8s.Resolver.OrgNamespace(org)
	orgNs, err := h.k8s.GetNamespace(ctx, orgNsName)
	if err != nil {
		slog.WarnContext(ctx, "failed to read org namespace for default folder resolution, falling back to org root",
			slog.String("organization", org),
			slog.Any("error", err),
		)
		return org, consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	}

	defaultFolder := orgNs.Annotations[v1alpha2.AnnotationDefaultFolder]
	if defaultFolder == "" {
		// No default-folder annotation — legacy org, fall back to org root.
		return org, consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	}

	// Check that the referenced folder namespace actually exists.
	folderNsName := h.k8s.Resolver.FolderNamespace(defaultFolder)
	exists, err := h.k8s.NamespaceExists(ctx, folderNsName)
	if err != nil {
		slog.WarnContext(ctx, "error checking default folder existence, falling back to org root",
			slog.String("action", "default_folder_not_found"),
			slog.String("organization", org),
			slog.String("default_folder", defaultFolder),
			slog.Any("error", err),
		)
		return org, consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	}
	if !exists {
		slog.WarnContext(ctx, "default folder referenced by org does not exist, falling back to org root",
			slog.String("action", "default_folder_not_found"),
			slog.String("organization", org),
			slog.String("default_folder", defaultFolder),
		)
		return org, consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	}

	return defaultFolder, consolev1.ParentType_PARENT_TYPE_FOLDER
}

// resolveParentNS converts a ParentType+ParentName pair to a Kubernetes namespace name.
func (h *Handler) resolveParentNS(parentType consolev1.ParentType, parentName string) (string, error) {
	switch parentType {
	case consolev1.ParentType_PARENT_TYPE_ORGANIZATION:
		return h.k8s.Resolver.OrgNamespace(parentName), nil
	case consolev1.ParentType_PARENT_TYPE_FOLDER:
		return h.k8s.Resolver.FolderNamespace(parentName), nil
	default:
		return "", fmt.Errorf("unknown parent_type %v", parentType)
	}
}

// resolveOrgGrants returns the active grant maps for the given organization.
// Returns nil maps if no org resolver is configured or org is empty.
func (h *Handler) resolveOrgGrants(ctx context.Context, org string) (map[string]string, map[string]string) {
	if h.orgResolver == nil || org == "" {
		return nil, nil
	}
	users, roles, err := h.orgResolver.GetOrgGrants(ctx, org)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve org grants",
			slog.String("organization", org),
			slog.Any("error", err),
		)
		return nil, nil
	}
	return users, roles
}

// checkAccessWithOrg checks project-level grants. Organization grants do not
// cascade to project operations (see docs/adrs/007-org-grants-no-cascade.md).
func (h *Handler) checkAccessWithOrg(
	email string,
	roles []string,
	projUsers, projRoles map[string]string,
	permission rbac.Permission,
) error {
	if err := rbac.CheckAccessGrants(email, roles, projUsers, projRoles, permission); err == nil {
		return nil
	}
	return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
}

// bestRoleWithOrg returns the best role from project grants and org grants.
func (h *Handler) bestRoleWithOrg(email string, roles []string, projUsers, projRoles map[string]string, ns *corev1.Namespace) rbac.Role {
	projRole := rbac.BestRoleFromGrants(email, roles, projUsers, projRoles)
	orgUsers, orgRoles := h.resolveOrgGrants(context.Background(), GetOrganization(ns))
	orgRole := rbac.BestRoleFromGrants(email, roles, orgUsers, orgRoles)
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
	return secrets.DeduplicateGrants(result)
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
