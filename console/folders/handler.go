package folders

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

const auditResourceType = "folder"

// maxFolderDepth is the maximum number of folder levels between an org and a project.
const maxFolderDepth = 3

// Handler implements the FolderService.
type Handler struct {
	consolev1connect.UnimplementedFolderServiceHandler
	k8s *K8sClient
}

// NewHandler creates a new FolderService handler.
func NewHandler(k8s *K8sClient) *Handler {
	return &Handler{k8s: k8s}
}

// ListFolders returns all folders the user has access to.
func (h *Handler) ListFolders(
	ctx context.Context,
	req *connect.Request[consolev1.ListFoldersRequest],
) (*connect.Response[consolev1.ListFoldersResponse], error) {
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

	allFolders, err := h.k8s.ListFolders(ctx, req.Msg.Organization, parentNs)
	if err != nil {
		return nil, mapK8sError(err)
	}

	now := time.Now()
	var result []*consolev1.Folder
	for _, ns := range allFolders {
		shareUsers, _ := GetShareUsers(ns)
		shareRoles, _ := GetShareRoles(ns)
		activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
		activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

		if err := CheckFolderListAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
			continue
		}

		userRole := rbac.BestRoleFromGrants(claims.Email, claims.Roles, activeUsers, activeRoles)
		result = append(result, buildFolder(h.k8s, ns, shareUsers, shareRoles, userRole))
	}

	slog.InfoContext(ctx, "folders listed",
		slog.String("action", "folder_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("total", len(result)),
	)

	return connect.NewResponse(&consolev1.ListFoldersResponse{
		Folders: result,
	}), nil
}

// GetFolder retrieves a folder by name.
func (h *Handler) GetFolder(
	ctx context.Context,
	req *connect.Request[consolev1.GetFolderRequest],
) (*connect.Response[consolev1.GetFolderResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("folder name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetFolder(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	if err := CheckFolderReadAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
		slog.WarnContext(ctx, "folder access denied",
			slog.String("action", "folder_read_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("folder", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Roles, activeUsers, activeRoles)

	slog.InfoContext(ctx, "folder accessed",
		slog.String("action", "folder_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("folder", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.GetFolderResponse{
		Folder: buildFolder(h.k8s, ns, shareUsers, shareRoles, userRole),
	}), nil
}

// CreateFolder creates a new folder.
// When name is empty, it is derived from display_name using slug generation.
// Uses bounded retry with random suffix on namespace collision.
func (h *Handler) CreateFolder(
	ctx context.Context,
	req *connect.Request[consolev1.CreateFolderRequest],
) (*connect.Response[consolev1.CreateFolderResponse], error) {
	if req.Msg.Organization == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization is required"))
	}
	if req.Msg.ParentName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("parent_name is required"))
	}
	if req.Msg.ParentType == consolev1.ParentType_PARENT_TYPE_UNSPECIFIED {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("parent_type is required"))
	}

	// Derive name from display_name when not explicitly provided.
	name := req.Msg.Name
	if name == "" {
		if req.Msg.DisplayName == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("folder name or display_name is required"))
		}
		prefix := h.k8s.Resolver.NamespacePrefix + h.k8s.Resolver.FolderPrefix
		exists := func(ctx context.Context, nsName string) (bool, error) {
			return h.k8s.NamespaceExists(ctx, nsName)
		}
		generated, err := v1alpha2.GenerateIdentifier(ctx, req.Msg.DisplayName, prefix, exists)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("generating folder identifier: %w", err))
		}
		name = generated
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Resolve parent namespace.
	parentNs, err := h.resolveParentNS(req.Msg.ParentType, req.Msg.ParentName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Fetch parent namespace to verify it exists and check access.
	parentNamespace, err := h.k8s.GetNamespace(ctx, parentNs)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Check create access on the parent.
	parentShareUsers, _ := GetShareUsers(parentNamespace)
	parentShareRoles, _ := GetShareRoles(parentNamespace)
	now := time.Now()
	parentActiveUsers := secrets.ActiveGrantsMap(parentShareUsers, now)
	parentActiveRoles := secrets.ActiveGrantsMap(parentShareRoles, now)

	if err := CheckFolderCreateAccess(claims.Email, claims.Roles, parentActiveUsers, parentActiveRoles); err != nil {
		slog.WarnContext(ctx, "folder create denied",
			slog.String("action", "folder_create_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("folder", name),
			slog.String("parent", parentNs),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Enforce max folder depth (3 folder levels between org and project).
	depth, err := h.computeFolderDepth(ctx, parentNs)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("computing folder depth: %w", err))
	}
	if depth >= maxFolderDepth {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("cannot create folder: maximum folder depth of %d exceeded (current depth: %d)", maxFolderDepth, depth))
	}

	// Build initial grants from request, ensuring creator is owner.
	shareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	shareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)
	shareUsers = ensureCreatorOwner(shareUsers, claims.Email)

	// Merge default-share cascade from all ancestors into initial grants.
	ancestorDefaults, err := h.collectAncestorDefaultShares(ctx, parentNs)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("collecting ancestor default shares: %w", err))
	}
	shareUsers = mergeGrants(ancestorDefaults.users, shareUsers)
	shareRoles = mergeGrants(ancestorDefaults.roles, shareRoles)

	if _, err := h.k8s.CreateFolder(ctx, name, req.Msg.DisplayName, req.Msg.Description,
		req.Msg.Organization, parentNs, claims.Email, shareUsers, shareRoles); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "folder created",
		slog.String("action", "folder_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("folder", name),
		slog.String("parent", parentNs),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateFolderResponse{
		Name: name,
	}), nil
}

// UpdateFolder updates folder metadata and optionally reparents the folder.
func (h *Handler) UpdateFolder(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateFolderRequest],
) (*connect.Response[consolev1.UpdateFolderResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("folder name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetFolder(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	if err := CheckFolderWriteAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
		slog.WarnContext(ctx, "folder update denied",
			slog.String("action", "folder_update_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("folder", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Handle reparenting if parent_type and parent_name are set.
	if req.Msg.ParentType != nil && req.Msg.ParentName != nil {
		if err := h.reparentFolder(ctx, ns, claims, *req.Msg.ParentType, *req.Msg.ParentName); err != nil {
			return nil, err
		}
	}

	if _, err := h.k8s.UpdateFolder(ctx, req.Msg.Name, req.Msg.DisplayName, req.Msg.Description); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "folder updated",
		slog.String("action", "folder_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("folder", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateFolderResponse{}), nil
}

// reparentFolder validates and executes a folder reparent operation.
// Checks PERMISSION_REPARENT on both source and destination parents,
// enforces depth limits, and detects cycles.
func (h *Handler) reparentFolder(
	ctx context.Context,
	ns *corev1.Namespace,
	claims *rpc.Claims,
	newParentType consolev1.ParentType,
	newParentName string,
) error {
	folderName := ns.Labels[v1alpha2.LabelFolder]
	folderNsName := ns.Name

	// Resolve new parent namespace.
	newParentNs, err := h.resolveParentNS(newParentType, newParentName)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Resolve current parent namespace from the folder's label.
	currentParentNs := ns.Labels[v1alpha2.AnnotationParent]
	if currentParentNs == "" {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("folder %q is missing parent label", folderName))
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
	if err := h.checkReparentAccess(ctx, claims, sourceNs, "source"); err != nil {
		slog.WarnContext(ctx, "folder reparent denied on source",
			slog.String("action", "folder_reparent_denied_source"),
			slog.String("resource_type", auditResourceType),
			slog.String("folder", folderName),
			slog.String("source_parent", currentParentNs),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return err
	}

	// Check PERMISSION_REPARENT on the new (destination) parent.
	if err := h.checkReparentAccess(ctx, claims, newParentNamespace, "destination"); err != nil {
		slog.WarnContext(ctx, "folder reparent denied on destination",
			slog.String("action", "folder_reparent_denied_destination"),
			slog.String("resource_type", auditResourceType),
			slog.String("folder", folderName),
			slog.String("dest_parent", newParentNs),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return err
	}

	// Cycle detection: walk new parent's ancestors; if the folder being moved
	// appears, the move would create a cycle.
	if err := h.detectCycle(ctx, folderNsName, newParentNs); err != nil {
		return err
	}

	// Depth enforcement: compute depth at new parent, then compute subtree depth
	// of the folder being moved. Total must not exceed maxFolderDepth.
	newParentDepth, err := h.computeFolderDepth(ctx, newParentNs)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("computing new parent depth: %w", err))
	}
	subtreeDepth, err := h.computeSubtreeDepth(ctx, folderNsName)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("computing subtree depth: %w", err))
	}
	// newParentDepth is the number of folder levels above the new parent.
	// subtreeDepth is the max folder depth below and including the folder being moved.
	// The folder being moved will be at depth newParentDepth+1, and the deepest
	// descendant at newParentDepth+subtreeDepth.
	totalDepth := newParentDepth + subtreeDepth
	if totalDepth > maxFolderDepth {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("reparenting would exceed maximum folder depth of %d (new depth: %d)", maxFolderDepth, totalDepth))
	}

	// Execute the reparent: update the parent label.
	if _, err := h.k8s.UpdateParentLabel(ctx, folderName, newParentNs); err != nil {
		return mapK8sError(err)
	}

	slog.InfoContext(ctx, "folder reparented",
		slog.String("action", "folder_reparent"),
		slog.String("resource_type", auditResourceType),
		slog.String("folder", folderName),
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
func (h *Handler) checkReparentAccess(ctx context.Context, claims *rpc.Claims, parentNs *corev1.Namespace, direction string) error {
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
	org := ""
	if parentNs.Labels != nil {
		org = parentNs.Labels[v1alpha2.LabelOrganization]
	}
	if org != "" {
		orgNsName := h.k8s.Resolver.OrgNamespace(org)
		orgNs, err := h.k8s.GetNamespace(ctx, orgNsName)
		if err == nil {
			orgShareUsers, _ := GetShareUsers(orgNs)
			orgShareRoles, _ := GetShareRoles(orgNs)
			orgActiveUsers := secrets.ActiveGrantsMap(orgShareUsers, now)
			orgActiveRoles := secrets.ActiveGrantsMap(orgShareRoles, now)
			if err := rbac.CheckCascadeAccess(claims.Email, claims.Roles, orgActiveUsers, orgActiveRoles, rbac.PermissionReparent, rbac.ReparentCascadePerms); err == nil {
				return nil
			}
		}
	}

	return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: reparent authorization denied on %s parent", direction))
}

// detectCycle walks from startNs up through ancestors. If folderNsName appears
// in the ancestor chain, the move would create a cycle.
func (h *Handler) detectCycle(ctx context.Context, folderNsName, startNs string) error {
	current := startNs
	for i := 0; i <= maxFolderDepth+1; i++ {
		if current == folderNsName {
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("reparenting would create a cycle: folder is an ancestor of the destination"))
		}
		ns, err := h.k8s.GetNamespace(ctx, current)
		if err != nil {
			return connect.NewError(connect.CodeInternal, fmt.Errorf("cycle detection: getting namespace %q: %w", current, err))
		}
		resourceType := ns.Labels[v1alpha2.LabelResourceType]
		if resourceType == v1alpha2.ResourceTypeOrganization {
			// Reached the org root, no cycle.
			return nil
		}
		parent := ns.Labels[v1alpha2.AnnotationParent]
		if parent == "" {
			return nil
		}
		current = parent
	}
	return nil
}

// computeSubtreeDepth returns the maximum folder depth below (and including)
// the given folder namespace. A leaf folder returns 1.
func (h *Handler) computeSubtreeDepth(ctx context.Context, folderNsName string) (int, error) {
	children, err := h.k8s.ListChildFolders(ctx, folderNsName)
	if err != nil {
		return 0, err
	}
	if len(children) == 0 {
		return 1, nil
	}
	maxChildDepth := 0
	for _, child := range children {
		childDepth, err := h.computeSubtreeDepth(ctx, child.Name)
		if err != nil {
			return 0, err
		}
		if childDepth > maxChildDepth {
			maxChildDepth = childDepth
		}
	}
	return 1 + maxChildDepth, nil
}

// DeleteFolder deletes a folder.
func (h *Handler) DeleteFolder(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteFolderRequest],
) (*connect.Response[consolev1.DeleteFolderResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("folder name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetFolder(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	if err := CheckFolderDeleteAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
		slog.WarnContext(ctx, "folder delete denied",
			slog.String("action", "folder_delete_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("folder", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Check for child folders.
	folderNsName := h.k8s.Resolver.FolderNamespace(req.Msg.Name)
	childFolders, err := h.k8s.ListChildFolders(ctx, folderNsName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("checking for child folders: %w", err))
	}
	if len(childFolders) > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("cannot delete folder %q: %d child folder(s) must be deleted first", req.Msg.Name, len(childFolders)))
	}

	// Check for child projects.
	childProjects, err := h.k8s.ListChildProjects(ctx, folderNsName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("checking for child projects: %w", err))
	}
	if len(childProjects) > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("cannot delete folder %q: %d child project(s) must be deleted first", req.Msg.Name, len(childProjects)))
	}

	if err := h.k8s.DeleteFolder(ctx, req.Msg.Name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "folder deleted",
		slog.String("action", "folder_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("folder", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteFolderResponse{}), nil
}

// UpdateFolderSharing updates the sharing grants on a folder.
func (h *Handler) UpdateFolderSharing(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateFolderSharingRequest],
) (*connect.Response[consolev1.UpdateFolderSharingResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("folder name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetFolder(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	if err := CheckFolderAdminAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
		slog.WarnContext(ctx, "folder sharing update denied",
			slog.String("action", "folder_sharing_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("folder", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	newShareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	newShareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)

	updated, err := h.k8s.UpdateFolderSharing(ctx, req.Msg.Name, newShareUsers, newShareRoles)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "folder sharing updated",
		slog.String("action", "folder_sharing_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("folder", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	updatedUsers, _ := GetShareUsers(updated)
	updatedRoles, _ := GetShareRoles(updated)
	updatedActiveUsers := secrets.ActiveGrantsMap(updatedUsers, now)
	updatedActiveGroups := secrets.ActiveGrantsMap(updatedRoles, now)
	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Roles, updatedActiveUsers, updatedActiveGroups)

	return connect.NewResponse(&consolev1.UpdateFolderSharingResponse{
		Folder: buildFolder(h.k8s, updated, updatedUsers, updatedRoles, userRole),
	}), nil
}

// UpdateFolderDefaultSharing updates the default sharing grants on a folder.
func (h *Handler) UpdateFolderDefaultSharing(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateFolderDefaultSharingRequest],
) (*connect.Response[consolev1.UpdateFolderDefaultSharingResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("folder name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetFolder(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	if err := CheckFolderAdminAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
		slog.WarnContext(ctx, "folder default sharing update denied",
			slog.String("action", "folder_default_sharing_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("folder", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	newDefaultUsers := shareGrantsToAnnotations(req.Msg.DefaultUserGrants)
	newDefaultRoles := shareGrantsToAnnotations(req.Msg.DefaultRoleGrants)

	updated, err := h.k8s.UpdateFolderDefaultSharing(ctx, req.Msg.Name, newDefaultUsers, newDefaultRoles)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "folder default sharing updated",
		slog.String("action", "folder_default_sharing_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("folder", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	updatedShareUsers, _ := GetShareUsers(updated)
	updatedShareRoles, _ := GetShareRoles(updated)
	updatedActiveUsers := secrets.ActiveGrantsMap(updatedShareUsers, now)
	updatedActiveRoles := secrets.ActiveGrantsMap(updatedShareRoles, now)
	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Roles, updatedActiveUsers, updatedActiveRoles)

	return connect.NewResponse(&consolev1.UpdateFolderDefaultSharingResponse{
		Folder: buildFolder(h.k8s, updated, updatedShareUsers, updatedShareRoles, userRole),
	}), nil
}

// GetFolderRaw retrieves the full Kubernetes Namespace object as verbatim JSON.
func (h *Handler) GetFolderRaw(
	ctx context.Context,
	req *connect.Request[consolev1.GetFolderRawRequest],
) (*connect.Response[consolev1.GetFolderRawResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("folder name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetFolder(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	if err := CheckFolderReadAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
		slog.WarnContext(ctx, "folder raw access denied",
			slog.String("action", "folder_raw_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("folder", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	slog.InfoContext(ctx, "folder raw accessed",
		slog.String("action", "folder_raw"),
		slog.String("resource_type", auditResourceType),
		slog.String("folder", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	ns.APIVersion = "v1"
	ns.Kind = "Namespace"

	raw, err := json.Marshal(ns)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("marshaling namespace to JSON: %w", err))
	}

	return connect.NewResponse(&consolev1.GetFolderRawResponse{
		Raw: string(raw),
	}), nil
}

// CheckFolderIdentifier checks whether a proposed folder identifier is available.
func (h *Handler) CheckFolderIdentifier(
	ctx context.Context,
	req *connect.Request[consolev1.CheckFolderIdentifierRequest],
) (*connect.Response[consolev1.CheckFolderIdentifierResponse], error) {
	if req.Msg.Identifier == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("identifier is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	prefix := h.k8s.Resolver.NamespacePrefix + h.k8s.Resolver.FolderPrefix
	exists := func(ctx context.Context, nsName string) (bool, error) {
		return h.k8s.NamespaceExists(ctx, nsName)
	}

	result, err := v1alpha2.CheckIdentifier(ctx, req.Msg.Identifier, prefix, exists)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("checking identifier: %w", err))
	}

	slog.InfoContext(ctx, "folder identifier checked",
		slog.String("action", "folder_check_identifier"),
		slog.String("resource_type", auditResourceType),
		slog.String("identifier", req.Msg.Identifier),
		slog.Bool("available", result.Available),
		slog.String("suggested", result.SuggestedIdentifier),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CheckFolderIdentifierResponse{
		Available:           result.Available,
		SuggestedIdentifier: result.SuggestedIdentifier,
	}), nil
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

// computeFolderDepth counts how many folder levels are between the given parent
// namespace and the root organization. Returns 0 when the parent is an org
// namespace (the new folder would be at depth 1).
func (h *Handler) computeFolderDepth(ctx context.Context, parentNs string) (int, error) {
	depth := 0
	current := parentNs
	for i := 0; i <= maxFolderDepth; i++ {
		ns, err := h.k8s.GetNamespace(ctx, current)
		if err != nil {
			return 0, fmt.Errorf("getting namespace %q: %w", current, err)
		}
		resourceType := ns.Labels[v1alpha2.LabelResourceType]
		if resourceType == v1alpha2.ResourceTypeOrganization {
			return depth, nil
		}
		if resourceType == v1alpha2.ResourceTypeFolder {
			depth++
			parent := ns.Labels[v1alpha2.AnnotationParent]
			if parent == "" {
				return 0, fmt.Errorf("folder namespace %q is missing parent label", current)
			}
			current = parent
			continue
		}
		return 0, fmt.Errorf("unexpected resource type %q on namespace %q", resourceType, current)
	}
	return depth, nil
}

// ancestorDefaultShares holds merged default-share grants from ancestors.
type ancestorDefaultShares struct {
	users []secrets.AnnotationGrant
	roles []secrets.AnnotationGrant
}

// collectAncestorDefaultShares walks from parentNs up to the org and collects
// default-share-users and default-share-roles from each ancestor.
func (h *Handler) collectAncestorDefaultShares(ctx context.Context, parentNs string) (ancestorDefaultShares, error) {
	var result ancestorDefaultShares
	current := parentNs
	for i := 0; i <= maxFolderDepth+1; i++ {
		ns, err := h.k8s.GetNamespace(ctx, current)
		if err != nil {
			return result, fmt.Errorf("getting namespace %q: %w", current, err)
		}
		defaultUsers, _ := GetDefaultShareUsers(ns)
		defaultRoles, _ := GetDefaultShareRoles(ns)
		result.users = mergeGrants(result.users, defaultUsers)
		result.roles = mergeGrants(result.roles, defaultRoles)

		resourceType := ns.Labels[v1alpha2.LabelResourceType]
		if resourceType == v1alpha2.ResourceTypeOrganization {
			break
		}
		parent := ns.Labels[v1alpha2.AnnotationParent]
		if parent == "" {
			break
		}
		current = parent
	}
	return result, nil
}

// mergeGrants merges base grants with override grants; override wins per principal.
// Higher role wins when the same principal appears in both.
func mergeGrants(base, override []secrets.AnnotationGrant) []secrets.AnnotationGrant {
	merged := make(map[string]secrets.AnnotationGrant)
	for _, g := range base {
		merged[strings.ToLower(g.Principal)] = g
	}
	for _, g := range override {
		key := strings.ToLower(g.Principal)
		existing, ok := merged[key]
		if !ok || rbac.RoleLevel(rbac.RoleFromString(g.Role)) > rbac.RoleLevel(rbac.RoleFromString(existing.Role)) {
			merged[key] = g
		}
	}
	result := make([]secrets.AnnotationGrant, 0, len(merged))
	for _, g := range merged {
		result = append(result, g)
	}
	return result
}

// buildFolder creates a Folder proto message from a namespace.
func buildFolder(k8s *K8sClient, ns *corev1.Namespace, shareUsers, shareRoles []secrets.AnnotationGrant, userRole rbac.Role) *consolev1.Folder {
	folder := &consolev1.Folder{
		UserGrants: annotationGrantsToProto(shareUsers),
		RoleGrants: annotationGrantsToProto(shareRoles),
		UserRole:   consolev1.Role(userRole),
	}

	if ns.Labels != nil {
		folder.Name = ns.Labels[v1alpha2.LabelFolder]
		folder.Organization = ns.Labels[v1alpha2.LabelOrganization]

		// Derive parent info from the parent label.
		parentNs := ns.Labels[v1alpha2.AnnotationParent]
		if parentNs != "" {
			kind, name, err := k8s.Resolver.ResourceTypeFromNamespace(parentNs)
			if err == nil {
				folder.ParentName = name
				switch kind {
				case v1alpha2.ResourceTypeOrganization:
					folder.ParentType = consolev1.ParentType_PARENT_TYPE_ORGANIZATION
				case v1alpha2.ResourceTypeFolder:
					folder.ParentType = consolev1.ParentType_PARENT_TYPE_FOLDER
				}
			}
		}
	}

	if ns.Annotations != nil {
		folder.DisplayName = ns.Annotations[v1alpha2.AnnotationDisplayName]
		folder.Description = ns.Annotations[v1alpha2.AnnotationDescription]
		folder.CreatorEmail = ns.Annotations[v1alpha2.AnnotationCreatorEmail]
	}

	if defaultUsers, err := GetDefaultShareUsers(ns); err == nil {
		folder.DefaultUserGrants = annotationGrantsToProto(defaultUsers)
	}
	if defaultRoles, err := GetDefaultShareRoles(ns); err == nil {
		folder.DefaultRoleGrants = annotationGrantsToProto(defaultRoles)
	}
	folder.CreatedAt = ns.CreationTimestamp.UTC().Format(time.RFC3339)

	return folder
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
	if strings.Contains(err.Error(), "not managed by") || strings.Contains(err.Error(), "not a folder") {
		return connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
