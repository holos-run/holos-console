package secrets

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

	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

// auditResourceType is the resource_type value for all secret audit log events.
const auditResourceType = "secret"

// ProjectResolver resolves project namespace grants for access checks.
type ProjectResolver interface {
	GetProjectGrants(ctx context.Context, project string) (shareUsers, shareGroups map[string]string, err error)
}

// OrgResolver resolves organization-level grants for access checks.
type OrgResolver interface {
	GetOrgGrantsForProject(ctx context.Context, project string) (shareUsers, shareGroups map[string]string, err error)
}

// Handler implements the SecretsService.
type Handler struct {
	consolev1connect.UnimplementedSecretsServiceHandler
	k8s             *K8sClient
	projectResolver ProjectResolver
	orgResolver     OrgResolver
}

// NewProjectScopedHandler creates a SecretsService handler that resolves access
// from project grants when per-secret grants are insufficient.
func NewProjectScopedHandler(k8s *K8sClient, projectResolver ProjectResolver, orgResolver OrgResolver) *Handler {
	return &Handler{k8s: k8s, projectResolver: projectResolver, orgResolver: orgResolver}
}

// ListSecrets returns all secrets with accessibility info for the current user.
func (h *Handler) ListSecrets(
	ctx context.Context,
	req *connect.Request[consolev1.ListSecretsRequest],
) (*connect.Response[consolev1.ListSecretsResponse], error) {
	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	// Resolve project and org grants for fallback access checks
	projUsers, projGroups := h.resolveProjectGrants(ctx, project)
	orgUsers, orgGroups := h.resolveOrgGrants(ctx, project)

	// List secrets from Kubernetes with console label
	secretList, err := h.k8s.ListSecrets(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Build list with accessibility info for each secret
	now := time.Now()
	var secrets []*consolev1.SecretMetadata
	var accessibleCount int
	for _, secret := range secretList.Items {
		shareUsers, _ := GetShareUsers(&secret)
		shareGroups, _ := GetShareGroups(&secret)
		activeUsers := ActiveGrantsMap(shareUsers, now)
		activeGroups := ActiveGrantsMap(shareGroups, now)
		accessible := h.checkAccess(claims.Email, claims.Groups, activeUsers, activeGroups, projUsers, projGroups, orgUsers, orgGroups, rbac.PermissionSecretsList) == nil
		if accessible {
			accessibleCount++
		}
		metadata := h.buildSecretMetadata(&secret, shareUsers, shareGroups, activeUsers, activeGroups, projUsers, projGroups, orgUsers, orgGroups, claims)
		secrets = append(secrets, metadata)
	}

	slog.InfoContext(ctx, "secrets listed",
		slog.String("action", "secrets_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("total", len(secrets)),
		slog.Int("accessible", accessibleCount),
	)

	return connect.NewResponse(&consolev1.ListSecretsResponse{
		Secrets: secrets,
	}), nil
}

// GetSecret retrieves a secret by name with RBAC authorization.
func (h *Handler) GetSecret(
	ctx context.Context,
	req *connect.Request[consolev1.GetSecretRequest],
) (*connect.Response[consolev1.GetSecretResponse], error) {
	// Validate request
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("secret name is required"))
	}

	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Get secret from Kubernetes
	secret, err := h.k8s.GetSecret(ctx, project, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	return h.returnSecret(ctx, claims, secret, project)
}

// DeleteSecret deletes a secret with RBAC authorization.
func (h *Handler) DeleteSecret(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteSecretRequest],
) (*connect.Response[consolev1.DeleteSecretResponse], error) {
	// Validate request
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("secret name is required"))
	}

	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Get existing secret to check RBAC
	secret, err := h.k8s.GetSecret(ctx, project, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Check RBAC for delete access (per-secret grants, then project grants, then org grants)
	shareUsers, _ := GetShareUsers(secret)
	shareGroups, _ := GetShareGroups(secret)
	now := time.Now()
	activeUsers := ActiveGrantsMap(shareUsers, now)
	activeGroups := ActiveGrantsMap(shareGroups, now)
	projUsers, projGroups := h.resolveProjectGrants(ctx, project)
	orgUsers, orgGroups := h.resolveOrgGrants(ctx, project)
	if err := h.checkAccess(claims.Email, claims.Groups, activeUsers, activeGroups, projUsers, projGroups, orgUsers, orgGroups, rbac.PermissionSecretsDelete); err != nil {
		slog.WarnContext(ctx, "secret delete denied",
			slog.String("action", "secret_delete_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("secret", req.Msg.Name),
			slog.String("project", project),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
			slog.Any("user_groups", claims.Groups),
		)
		return nil, err
	}

	// Perform the delete
	if err := h.k8s.DeleteSecret(ctx, project, req.Msg.Name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "secret deleted",
		slog.String("action", "secret_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("secret", req.Msg.Name),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteSecretResponse{}), nil
}

// CreateSecret creates a new secret with RBAC authorization.
// Since the secret doesn't exist yet, authorization is checked against the user's own roles
// and project grants.
func (h *Handler) CreateSecret(
	ctx context.Context,
	req *connect.Request[consolev1.CreateSecretRequest],
) (*connect.Response[consolev1.CreateSecretResponse], error) {
	// Validate request
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("secret name is required"))
	}

	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Convert proto ShareGrant slices to annotation grants
	shareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	shareGroups := shareGrantsToAnnotations(req.Msg.GroupGrants)

	// Check that the user has write permission based on the requested sharing grants
	// and project/org grants.
	now := time.Now()
	activeUsers := ActiveGrantsMap(shareUsers, now)
	activeGroups := ActiveGrantsMap(shareGroups, now)
	projUsers, projGroups := h.resolveProjectGrants(ctx, project)
	orgUsers, orgGroups := h.resolveOrgGrants(ctx, project)
	if err := h.checkAccess(claims.Email, claims.Groups, activeUsers, activeGroups, projUsers, projGroups, orgUsers, orgGroups, rbac.PermissionSecretsWrite); err != nil {
		slog.WarnContext(ctx, "secret create denied",
			slog.String("action", "secret_create_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("secret", req.Msg.Name),
			slog.String("project", project),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Merge string_data into data (string_data takes precedence)
	data := mergeStringData(req.Msg.Data, req.Msg.StringData)

	// Extract description and url
	var description, url string
	if req.Msg.Description != nil {
		description = *req.Msg.Description
	}
	if req.Msg.Url != nil {
		url = *req.Msg.Url
	}

	// Create the secret
	_, err := h.k8s.CreateSecret(ctx, project, req.Msg.Name, data, shareUsers, shareGroups, description, url)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "secret created",
		slog.String("action", "secret_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("secret", req.Msg.Name),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateSecretResponse{
		Name: req.Msg.Name,
	}), nil
}

// UpdateSecret replaces the data of an existing secret with RBAC authorization.
func (h *Handler) UpdateSecret(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateSecretRequest],
) (*connect.Response[consolev1.UpdateSecretResponse], error) {
	// Validate request
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("secret name is required"))
	}
	if len(req.Msg.Data) == 0 && len(req.Msg.StringData) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("secret data is required"))
	}

	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Get existing secret to check RBAC
	secret, err := h.k8s.GetSecret(ctx, project, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Check RBAC for write access (per-secret grants, then project grants, then org grants)
	shareUsers, _ := GetShareUsers(secret)
	shareGroups, _ := GetShareGroups(secret)
	now := time.Now()
	activeUsers := ActiveGrantsMap(shareUsers, now)
	activeGroups := ActiveGrantsMap(shareGroups, now)
	projUsers, projGroups := h.resolveProjectGrants(ctx, project)
	orgUsers, orgGroups := h.resolveOrgGrants(ctx, project)
	if err := h.checkAccess(claims.Email, claims.Groups, activeUsers, activeGroups, projUsers, projGroups, orgUsers, orgGroups, rbac.PermissionSecretsWrite); err != nil {
		logAuditDenied(ctx, claims, secret.Name)
		slog.WarnContext(ctx, "secret update denied",
			slog.String("action", "secret_update_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("secret", req.Msg.Name),
			slog.String("project", project),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Merge string_data into data (string_data takes precedence)
	data := mergeStringData(req.Msg.Data, req.Msg.StringData)

	// Perform the update
	if _, err := h.k8s.UpdateSecret(ctx, project, req.Msg.Name, data, req.Msg.Description, req.Msg.Url); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "secret updated",
		slog.String("action", "secret_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("secret", req.Msg.Name),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateSecretResponse{}), nil
}

// UpdateSharing updates the sharing grants on a secret without touching its data.
// Requires ROLE_OWNER on the secret (via any grant source).
func (h *Handler) UpdateSharing(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateSharingRequest],
) (*connect.Response[consolev1.UpdateSharingResponse], error) {
	// Validate request
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("secret name is required"))
	}

	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Get existing secret to check RBAC
	secret, err := h.k8s.GetSecret(ctx, project, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Check RBAC for admin access (per-secret grants, then project grants, then org grants)
	shareUsers, _ := GetShareUsers(secret)
	shareGroups, _ := GetShareGroups(secret)
	now := time.Now()
	activeUsers := ActiveGrantsMap(shareUsers, now)
	activeGroups := ActiveGrantsMap(shareGroups, now)
	projUsers, projGroups := h.resolveProjectGrants(ctx, project)
	orgUsers, orgGroups := h.resolveOrgGrants(ctx, project)
	if err := h.checkAccess(claims.Email, claims.Groups, activeUsers, activeGroups, projUsers, projGroups, orgUsers, orgGroups, rbac.PermissionSecretsAdmin); err != nil {
		slog.WarnContext(ctx, "sharing update denied",
			slog.String("action", "sharing_update_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("secret", req.Msg.Name),
			slog.String("project", project),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Convert proto ShareGrant slices to annotation grants
	newShareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	newShareGroups := shareGrantsToAnnotations(req.Msg.GroupGrants)

	// Persist the sharing annotations
	updated, err := h.k8s.UpdateSharing(ctx, project, req.Msg.Name, newShareUsers, newShareGroups)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "sharing updated",
		slog.String("action", "sharing_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("secret", req.Msg.Name),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	// Build response metadata
	updatedUsers, _ := GetShareUsers(updated)
	updatedGroups, _ := GetShareGroups(updated)
	updatedActiveUsers := ActiveGrantsMap(updatedUsers, now)
	updatedActiveGroups := ActiveGrantsMap(updatedGroups, now)
	metadata := h.buildSecretMetadata(updated, updatedUsers, updatedGroups, updatedActiveUsers, updatedActiveGroups, projUsers, projGroups, orgUsers, orgGroups, claims)

	return connect.NewResponse(&consolev1.UpdateSharingResponse{
		Metadata: metadata,
	}), nil
}

// GetSecretRaw retrieves the full Kubernetes Secret object as verbatim JSON.
func (h *Handler) GetSecretRaw(
	ctx context.Context,
	req *connect.Request[consolev1.GetSecretRawRequest],
) (*connect.Response[consolev1.GetSecretRawResponse], error) {
	// Validate request
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("secret name is required"))
	}

	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Get secret from Kubernetes
	secret, err := h.k8s.GetSecret(ctx, project, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Check RBAC (per-secret grants, then project grants, then org grants)
	shareUsers, _ := GetShareUsers(secret)
	shareGroups, _ := GetShareGroups(secret)
	now := time.Now()
	activeUsers := ActiveGrantsMap(shareUsers, now)
	activeGroups := ActiveGrantsMap(shareGroups, now)
	projUsers, projGroups := h.resolveProjectGrants(ctx, project)
	orgUsers, orgGroups := h.resolveOrgGrants(ctx, project)
	if err := h.checkAccess(claims.Email, claims.Groups, activeUsers, activeGroups, projUsers, projGroups, orgUsers, orgGroups, rbac.PermissionSecretsRead); err != nil {
		logAuditDenied(ctx, claims, secret.Name)
		return nil, err
	}

	logAuditAllowed(ctx, claims, secret.Name)

	// Set apiVersion and kind (not populated by client-go on fetched objects)
	secret.APIVersion = "v1"
	secret.Kind = "Secret"

	// Marshal the full object to JSON
	raw, err := json.Marshal(secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("marshaling secret to JSON: %w", err))
	}

	return connect.NewResponse(&consolev1.GetSecretRawResponse{
		Raw: string(raw),
	}), nil
}

// mergeStringData merges string_data values into data. string_data keys take
// precedence over data keys, matching Kubernetes stringData semantics.
func mergeStringData(data map[string][]byte, stringData map[string]string) map[string][]byte {
	if len(stringData) == 0 {
		return data
	}
	if data == nil {
		data = make(map[string][]byte)
	}
	for k, v := range stringData {
		data[k] = []byte(v)
	}
	return data
}

// shareGrantsToAnnotations converts a slice of ShareGrant protos to []AnnotationGrant
// suitable for storing as a Kubernetes annotation.
func shareGrantsToAnnotations(grants []*consolev1.ShareGrant) []AnnotationGrant {
	result := make([]AnnotationGrant, 0, len(grants))
	for _, g := range grants {
		if g.Principal != "" {
			ag := AnnotationGrant{
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

// buildSecretMetadata creates SecretMetadata for a secret from the caller's perspective.
// It receives the full grant slices (for the proto response) and the active maps (for RBAC).
func (h *Handler) buildSecretMetadata(secret *corev1.Secret, shareUsers, shareGroups []AnnotationGrant, activeUsers, activeGroups, projUsers, projGroups, orgUsers, orgGroups map[string]string, claims *rpc.Claims) *consolev1.SecretMetadata {
	accessible := h.checkAccess(claims.Email, claims.Groups, activeUsers, activeGroups, projUsers, projGroups, orgUsers, orgGroups, rbac.PermissionSecretsList) == nil

	// Build user grants (all grants, including expired, for display)
	userGrants := annotationGrantsToProto(shareUsers)
	// Build group grants
	groupGrants := annotationGrantsToProto(shareGroups)

	md := &consolev1.SecretMetadata{
		Name:        secret.Name,
		Accessible:  accessible,
		UserGrants:  userGrants,
		GroupGrants: groupGrants,
	}
	if desc := GetDescription(secret); desc != "" {
		md.Description = &desc
	}
	if u := GetURL(secret); u != "" {
		md.Url = &u
	}
	return md
}

// annotationGrantsToProto converts []AnnotationGrant to []*consolev1.ShareGrant.
func annotationGrantsToProto(grants []AnnotationGrant) []*consolev1.ShareGrant {
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

// protoRoleFromString converts a role name string to the proto Role enum.
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

// returnSecret checks RBAC and returns the secret data.
func (h *Handler) returnSecret(ctx context.Context, claims *rpc.Claims, secret *corev1.Secret, project string) (*connect.Response[consolev1.GetSecretResponse], error) {
	// Check RBAC (per-secret grants, then project grants, then org grants)
	shareUsers, _ := GetShareUsers(secret)
	shareGroups, _ := GetShareGroups(secret)
	now := time.Now()
	activeUsers := ActiveGrantsMap(shareUsers, now)
	activeGroups := ActiveGrantsMap(shareGroups, now)
	projUsers, projGroups := h.resolveProjectGrants(ctx, project)
	orgUsers, orgGroups := h.resolveOrgGrants(ctx, project)
	if err := h.checkAccess(claims.Email, claims.Groups, activeUsers, activeGroups, projUsers, projGroups, orgUsers, orgGroups, rbac.PermissionSecretsRead); err != nil {
		logAuditDenied(ctx, claims, secret.Name)
		return nil, err
	}

	logAuditAllowed(ctx, claims, secret.Name)

	return connect.NewResponse(&consolev1.GetSecretResponse{
		Data: secret.Data,
	}), nil
}

// resolveOrgGrants returns the active org grant maps for the given project.
// Returns nil maps if no org resolver is configured.
func (h *Handler) resolveOrgGrants(ctx context.Context, project string) (map[string]string, map[string]string) {
	if h.orgResolver == nil {
		return nil, nil
	}
	users, groups, err := h.orgResolver.GetOrgGrantsForProject(ctx, project)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve org grants",
			slog.String("project", project),
			slog.Any("error", err),
		)
		return nil, nil
	}
	return users, groups
}

// resolveProjectGrants returns the active grant maps for the given project namespace.
// Returns nil maps if no project resolver is configured.
func (h *Handler) resolveProjectGrants(ctx context.Context, project string) (map[string]string, map[string]string) {
	if h.projectResolver == nil {
		return nil, nil
	}
	users, groups, err := h.projectResolver.GetProjectGrants(ctx, project)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve project grants",
			slog.String("project", project),
			slog.Any("error", err),
		)
		return nil, nil
	}
	return users, groups
}

// checkAccess verifies access using per-secret grants, then project grants, then org grants.
func (h *Handler) checkAccess(
	email string,
	groups []string,
	secretUsers, secretGroups map[string]string,
	projUsers, projGroups map[string]string,
	orgUsers, orgGroups map[string]string,
	permission rbac.Permission,
) error {
	// 1. Check per-secret grants
	if err := rbac.CheckAccessGrants(email, groups, secretUsers, secretGroups, permission); err == nil {
		return nil
	}

	// 2. Check project grants
	if projUsers != nil || projGroups != nil {
		if err := rbac.CheckAccessGrants(email, groups, projUsers, projGroups, permission); err == nil {
			return nil
		}
	}

	// 3. Check organization grants
	if orgUsers != nil || orgGroups != nil {
		if err := rbac.CheckAccessGrants(email, groups, orgUsers, orgGroups, permission); err == nil {
			return nil
		}
	}

	return connect.NewError(
		connect.CodePermissionDenied,
		fmt.Errorf("RBAC: authorization denied"),
	)
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
	return connect.NewError(connect.CodeInternal, err)
}

// logAuditAllowed logs a successful secret access.
func logAuditAllowed(ctx context.Context, claims *rpc.Claims, secret string) {
	slog.InfoContext(ctx, "secret access granted",
		slog.String("action", "secret_access"),
		slog.String("resource_type", auditResourceType),
		slog.String("secret", secret),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Any("groups", claims.Groups),
	)
}

// logAuditDenied logs a denied secret access.
func logAuditDenied(ctx context.Context, claims *rpc.Claims, secret string) {
	slog.WarnContext(ctx, "secret access denied",
		slog.String("action", "secret_access_denied"),
		slog.String("resource_type", auditResourceType),
		slog.String("secret", secret),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Any("user_groups", claims.Groups),
	)
}
