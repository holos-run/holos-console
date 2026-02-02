package organizations

import (
	"context"
	"encoding/json"
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

const auditResourceType = "organization"

// Handler implements the OrganizationService.
type Handler struct {
	consolev1connect.UnimplementedOrganizationServiceHandler
	k8s            *K8sClient
	creatorUsers   []string
	creatorGroups  []string
}

// NewHandler creates a new OrganizationService handler.
// creatorUsers and creatorGroups are the email addresses and OIDC group names
// allowed to create organizations (configured via CLI flags).
func NewHandler(k8s *K8sClient, creatorUsers, creatorGroups []string) *Handler {
	return &Handler{k8s: k8s, creatorUsers: creatorUsers, creatorGroups: creatorGroups}
}

// ListOrganizations returns all organizations the user has access to.
func (h *Handler) ListOrganizations(
	ctx context.Context,
	req *connect.Request[consolev1.ListOrganizationsRequest],
) (*connect.Response[consolev1.ListOrganizationsResponse], error) {
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	allOrgs, err := h.k8s.ListOrganizations(ctx)
	if err != nil {
		return nil, mapK8sError(err)
	}

	now := time.Now()
	var result []*consolev1.Organization
	for _, ns := range allOrgs {
		shareUsers, _ := GetShareUsers(ns)
		shareGroups, _ := GetShareGroups(ns)
		activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
		activeGroups := secrets.ActiveGrantsMap(shareGroups, now)

		if err := CheckOrgListAccess(claims.Email, claims.Groups, activeUsers, activeGroups); err != nil {
			continue
		}

		userRole := rbac.BestRoleFromGrants(claims.Email, claims.Groups, activeUsers, activeGroups)
		result = append(result, buildOrganization(h.k8s, ns, shareUsers, shareGroups, userRole))
	}

	slog.InfoContext(ctx, "organizations listed",
		slog.String("action", "organization_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("total", len(result)),
	)

	return connect.NewResponse(&consolev1.ListOrganizationsResponse{
		Organizations: result,
	}), nil
}

// GetOrganization retrieves an organization by name.
func (h *Handler) GetOrganization(
	ctx context.Context,
	req *connect.Request[consolev1.GetOrganizationRequest],
) (*connect.Response[consolev1.GetOrganizationResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetOrganization(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareGroups, _ := GetShareGroups(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeGroups := secrets.ActiveGrantsMap(shareGroups, now)

	if err := CheckOrgReadAccess(claims.Email, claims.Groups, activeUsers, activeGroups); err != nil {
		slog.WarnContext(ctx, "organization access denied",
			slog.String("action", "organization_read_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("organization", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Groups, activeUsers, activeGroups)

	slog.InfoContext(ctx, "organization accessed",
		slog.String("action", "organization_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.GetOrganizationResponse{
		Organization: buildOrganization(h.k8s, ns, shareUsers, shareGroups, userRole),
	}), nil
}

// CreateOrganization creates a new organization.
func (h *Handler) CreateOrganization(
	ctx context.Context,
	req *connect.Request[consolev1.CreateOrganizationRequest],
) (*connect.Response[consolev1.CreateOrganizationResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Check create access: caller must be in --org-creator-users or --org-creator-groups
	if !h.isOrgCreator(claims.Email, claims.Groups) {
		slog.WarnContext(ctx, "organization create denied",
			slog.String("action", "organization_create_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("organization", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: not authorized to create organizations"))
	}

	shareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	shareGroups := shareGrantsToAnnotations(req.Msg.GroupGrants)

	// Ensure creator is included as owner
	shareUsers = ensureCreatorOwner(shareUsers, claims.Email)

	if _, err := h.k8s.CreateOrganization(ctx, req.Msg.Name, req.Msg.DisplayName, req.Msg.Description, shareUsers, shareGroups); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "organization created",
		slog.String("action", "organization_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateOrganizationResponse{
		Name: req.Msg.Name,
	}), nil
}

// UpdateOrganization updates organization metadata.
func (h *Handler) UpdateOrganization(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateOrganizationRequest],
) (*connect.Response[consolev1.UpdateOrganizationResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetOrganization(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareGroups, _ := GetShareGroups(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeGroups := secrets.ActiveGrantsMap(shareGroups, now)

	if err := CheckOrgWriteAccess(claims.Email, claims.Groups, activeUsers, activeGroups); err != nil {
		slog.WarnContext(ctx, "organization update denied",
			slog.String("action", "organization_update_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("organization", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	if _, err := h.k8s.UpdateOrganization(ctx, req.Msg.Name, req.Msg.DisplayName, req.Msg.Description); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "organization updated",
		slog.String("action", "organization_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateOrganizationResponse{}), nil
}

// DeleteOrganization deletes a managed organization.
func (h *Handler) DeleteOrganization(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteOrganizationRequest],
) (*connect.Response[consolev1.DeleteOrganizationResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetOrganization(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareGroups, _ := GetShareGroups(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeGroups := secrets.ActiveGrantsMap(shareGroups, now)

	if err := CheckOrgDeleteAccess(claims.Email, claims.Groups, activeUsers, activeGroups); err != nil {
		slog.WarnContext(ctx, "organization delete denied",
			slog.String("action", "organization_delete_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("organization", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	if err := h.k8s.DeleteOrganization(ctx, req.Msg.Name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "organization deleted",
		slog.String("action", "organization_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteOrganizationResponse{}), nil
}

// UpdateOrganizationSharing updates the sharing grants on an organization.
func (h *Handler) UpdateOrganizationSharing(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateOrganizationSharingRequest],
) (*connect.Response[consolev1.UpdateOrganizationSharingResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetOrganization(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareGroups, _ := GetShareGroups(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeGroups := secrets.ActiveGrantsMap(shareGroups, now)

	if err := CheckOrgAdminAccess(claims.Email, claims.Groups, activeUsers, activeGroups); err != nil {
		slog.WarnContext(ctx, "organization sharing update denied",
			slog.String("action", "organization_sharing_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("organization", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	newShareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	newShareGroups := shareGrantsToAnnotations(req.Msg.GroupGrants)

	updated, err := h.k8s.UpdateOrganizationSharing(ctx, req.Msg.Name, newShareUsers, newShareGroups)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "organization sharing updated",
		slog.String("action", "organization_sharing_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	updatedUsers, _ := GetShareUsers(updated)
	updatedGroups, _ := GetShareGroups(updated)
	updatedActiveUsers := secrets.ActiveGrantsMap(updatedUsers, now)
	updatedActiveGroups := secrets.ActiveGrantsMap(updatedGroups, now)
	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Groups, updatedActiveUsers, updatedActiveGroups)

	return connect.NewResponse(&consolev1.UpdateOrganizationSharingResponse{
		Organization: buildOrganization(h.k8s, updated, updatedUsers, updatedGroups, userRole),
	}), nil
}

// GetOrganizationRaw retrieves the full Kubernetes Namespace object as verbatim JSON.
func (h *Handler) GetOrganizationRaw(
	ctx context.Context,
	req *connect.Request[consolev1.GetOrganizationRawRequest],
) (*connect.Response[consolev1.GetOrganizationRawResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetOrganization(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareGroups, _ := GetShareGroups(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeGroups := secrets.ActiveGrantsMap(shareGroups, now)

	if err := CheckOrgReadAccess(claims.Email, claims.Groups, activeUsers, activeGroups); err != nil {
		slog.WarnContext(ctx, "organization raw access denied",
			slog.String("action", "organization_raw_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("organization", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	slog.InfoContext(ctx, "organization raw accessed",
		slog.String("action", "organization_raw"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
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

	return connect.NewResponse(&consolev1.GetOrganizationRawResponse{
		Raw: string(raw),
	}), nil
}

// isOrgCreator checks whether the caller is authorized to create organizations
// based on the CLI-configured creator lists.
func (h *Handler) isOrgCreator(email string, groups []string) bool {
	emailLower := strings.ToLower(email)
	for _, u := range h.creatorUsers {
		if strings.ToLower(u) == emailLower {
			return true
		}
	}
	for _, g := range groups {
		gLower := strings.ToLower(g)
		for _, cg := range h.creatorGroups {
			if strings.ToLower(cg) == gLower {
				return true
			}
		}
	}
	return false
}

// buildOrganization creates an Organization proto message from a namespace.
func buildOrganization(k8s *K8sClient, ns interface{ GetName() string }, shareUsers, shareGroups []secrets.AnnotationGrant, userRole rbac.Role) *consolev1.Organization {
	org := &consolev1.Organization{
		Name:        k8s.resolver.OrgFromNamespace(ns.GetName()),
		UserGrants:  annotationGrantsToProto(shareUsers),
		GroupGrants: annotationGrantsToProto(shareGroups),
		UserRole:    consolev1.Role(userRole),
	}

	type annotated interface {
		GetAnnotations() map[string]string
	}
	if a, ok := ns.(annotated); ok {
		annotations := a.GetAnnotations()
		if annotations != nil {
			org.DisplayName = annotations[DisplayNameAnnotation]
			org.Description = annotations[secrets.DescriptionAnnotation]
		}
	}

	return org
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
	if strings.Contains(err.Error(), "not managed by") || strings.Contains(err.Error(), "not an organization") {
		return connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
