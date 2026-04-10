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
	GetProjectGrants(ctx context.Context, project string) (shareUsers, shareRoles map[string]string, err error)
}

// DefaultShareResolver is an optional interface that a ProjectResolver can also implement
// to provide default sharing grants applied to new secrets created in a project.
type DefaultShareResolver interface {
	GetDefaultGrants(ctx context.Context, project string) (defaultUsers, defaultRoles []AnnotationGrant, err error)
}

// Handler implements the SecretsService.
type Handler struct {
	consolev1connect.UnimplementedSecretsServiceHandler
	k8s             *K8sClient
	projectResolver ProjectResolver
}

// NewProjectScopedHandler creates a SecretsService handler that resolves access
// from project grants when per-secret grants are insufficient.
func NewProjectScopedHandler(k8s *K8sClient, projectResolver ProjectResolver) *Handler {
	return &Handler{k8s: k8s, projectResolver: projectResolver}
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

	// Resolve project grants for fallback access checks
	projUsers, projRoles := h.resolveProjectGrants(ctx, project)

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
		shareRoles, _ := GetShareRoles(&secret)
		activeUsers := ActiveGrantsMap(shareUsers, now)
		activeRoles := ActiveGrantsMap(shareRoles, now)
		accessible := h.checkAccess(claims.Email, claims.Roles, activeUsers, activeRoles, projUsers, projRoles, rbac.PermissionSecretsList) == nil
		if accessible {
			accessibleCount++
		}
		metadata := h.buildSecretMetadata(&secret, shareUsers, shareRoles, activeUsers, activeRoles, projUsers, projRoles, claims)
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

	// Check RBAC for delete access (per-secret grants, then project grants)
	shareUsers, _ := GetShareUsers(secret)
	shareRoles, _ := GetShareRoles(secret)
	now := time.Now()
	activeUsers := ActiveGrantsMap(shareUsers, now)
	activeRoles := ActiveGrantsMap(shareRoles, now)
	projUsers, projRoles := h.resolveProjectGrants(ctx, project)

	if err := h.checkAccess(claims.Email, claims.Roles, activeUsers, activeRoles, projUsers, projRoles, rbac.PermissionSecretsDelete); err != nil {
		slog.WarnContext(ctx, "secret delete denied",
			slog.String("action", "secret_delete_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("secret", req.Msg.Name),
			slog.String("project", project),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
			slog.Any("roles", claims.Roles),
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
	shareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)

	// Merge project-level default sharing grants (if the resolver supports it).
	// Request-supplied grants override defaults for the same principal via DeduplicateGrants
	// (highest role wins, so explicit grants at a higher role shadow lower defaults).
	if ds, ok := h.projectResolver.(DefaultShareResolver); ok {
		defaultUsers, defaultRoles, err := ds.GetDefaultGrants(ctx, project)
		if err != nil {
			slog.WarnContext(ctx, "failed to resolve default grants, proceeding without them",
				slog.String("project", project),
				slog.Any("error", err),
			)
		} else {
			// Concatenate: request grants first so they win over defaults for the same principal.
			shareUsers = DeduplicateGrants(append(shareUsers, defaultUsers...))
			shareRoles = DeduplicateGrants(append(shareRoles, defaultRoles...))
		}
	}

	// Check that the user has write permission based on the requested sharing grants
	// and project grants. Access check happens before adding creator-as-owner so that
	// viewers cannot bypass the check by being elevated via ensureCreatorOwner.
	now := time.Now()
	activeUsers := ActiveGrantsMap(shareUsers, now)
	activeRoles := ActiveGrantsMap(shareRoles, now)
	projUsers, projRoles := h.resolveProjectGrants(ctx, project)

	if err := h.checkAccess(claims.Email, claims.Roles, activeUsers, activeRoles, projUsers, projRoles, rbac.PermissionSecretsWrite); err != nil {
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

	// Ensure creator is always an owner after access check passes.
	shareUsers = ensureCreatorOwner(shareUsers, claims.Email)

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
	_, err := h.k8s.CreateSecret(ctx, project, req.Msg.Name, data, shareUsers, shareRoles, description, url)
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

	// Check RBAC for write access (per-secret grants, then project grants)
	shareUsers, _ := GetShareUsers(secret)
	shareRoles, _ := GetShareRoles(secret)
	now := time.Now()
	activeUsers := ActiveGrantsMap(shareUsers, now)
	activeRoles := ActiveGrantsMap(shareRoles, now)
	projUsers, projRoles := h.resolveProjectGrants(ctx, project)
	if err := h.checkAccess(claims.Email, claims.Roles, activeUsers, activeRoles, projUsers, projRoles, rbac.PermissionSecretsWrite); err != nil {
		logAuditDenied(ctx, claims, secret.Name, project)
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

	// Check RBAC for admin access (per-secret grants, then project grants)
	shareUsers, _ := GetShareUsers(secret)
	shareRoles, _ := GetShareRoles(secret)
	now := time.Now()
	activeUsers := ActiveGrantsMap(shareUsers, now)
	activeRoles := ActiveGrantsMap(shareRoles, now)
	projUsers, projRoles := h.resolveProjectGrants(ctx, project)

	if err := h.checkAccess(claims.Email, claims.Roles, activeUsers, activeRoles, projUsers, projRoles, rbac.PermissionSecretsAdmin); err != nil {
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
	newShareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)

	// Persist the sharing annotations
	updated, err := h.k8s.UpdateSharing(ctx, project, req.Msg.Name, newShareUsers, newShareRoles)
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
	updatedRoles, _ := GetShareRoles(updated)
	updatedActiveUsers := ActiveGrantsMap(updatedUsers, now)
	updatedActiveGroups := ActiveGrantsMap(updatedRoles, now)
	metadata := h.buildSecretMetadata(updated, updatedUsers, updatedRoles, updatedActiveUsers, updatedActiveGroups, projUsers, projRoles, claims)

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

	// Check RBAC (per-secret grants, then project grants)
	shareUsers, _ := GetShareUsers(secret)
	shareRoles, _ := GetShareRoles(secret)
	now := time.Now()
	activeUsers := ActiveGrantsMap(shareUsers, now)
	activeRoles := ActiveGrantsMap(shareRoles, now)
	projUsers, projRoles := h.resolveProjectGrants(ctx, project)
	if err := h.checkAccess(claims.Email, claims.Roles, activeUsers, activeRoles, projUsers, projRoles, rbac.PermissionSecretsRead); err != nil {
		logAuditDenied(ctx, claims, secret.Name, project)
		return nil, err
	}

	logAuditAllowed(ctx, claims, secret.Name, project)

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

// ensureCreatorOwner ensures the creator email is in the share-users list as owner.
func ensureCreatorOwner(shareUsers []AnnotationGrant, email string) []AnnotationGrant {
	emailLower := strings.ToLower(email)
	for _, g := range shareUsers {
		if strings.ToLower(g.Principal) == emailLower && strings.ToLower(g.Role) == "owner" {
			return shareUsers
		}
	}
	return DeduplicateGrants(append(shareUsers, AnnotationGrant{Principal: email, Role: "owner"}))
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
	return DeduplicateGrants(result)
}

// buildSecretMetadata creates SecretMetadata for a secret from the caller's perspective.
// It receives the full grant slices (for the proto response) and the active maps (for RBAC).
func (h *Handler) buildSecretMetadata(secret *corev1.Secret, shareUsers, shareRoles []AnnotationGrant, activeUsers, activeRoles, projUsers, projRoles map[string]string, claims *rpc.Claims) *consolev1.SecretMetadata {
	accessible := h.checkAccess(claims.Email, claims.Roles, activeUsers, activeRoles, projUsers, projRoles, rbac.PermissionSecretsList) == nil

	// Build user grants (all grants, including expired, for display)
	userGrants := annotationGrantsToProto(shareUsers)
	// Build role grants
	roleGrants := annotationGrantsToProto(shareRoles)

	md := &consolev1.SecretMetadata{
		Name:       secret.Name,
		Accessible: accessible,
		UserGrants: userGrants,
		RoleGrants: roleGrants,
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
	// Check RBAC (per-secret grants, then project grants)
	shareUsers, _ := GetShareUsers(secret)
	shareRoles, _ := GetShareRoles(secret)
	now := time.Now()
	activeUsers := ActiveGrantsMap(shareUsers, now)
	activeRoles := ActiveGrantsMap(shareRoles, now)
	projUsers, projRoles := h.resolveProjectGrants(ctx, project)
	if err := h.checkAccess(claims.Email, claims.Roles, activeUsers, activeRoles, projUsers, projRoles, rbac.PermissionSecretsRead); err != nil {
		logAuditDenied(ctx, claims, secret.Name, project)
		return nil, err
	}

	logAuditAllowed(ctx, claims, secret.Name, project)

	return connect.NewResponse(&consolev1.GetSecretResponse{
		Data: secret.Data,
	}), nil
}

// resolveProjectGrants returns the active grant maps for the given project namespace.
// Returns nil maps if no project resolver is configured.
func (h *Handler) resolveProjectGrants(ctx context.Context, project string) (map[string]string, map[string]string) {
	if h.projectResolver == nil {
		return nil, nil
	}
	users, roles, err := h.projectResolver.GetProjectGrants(ctx, project)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve project grants",
			slog.String("project", project),
			slog.Any("error", err),
		)
		return nil, nil
	}
	return users, roles
}

// checkAccess verifies access using per-secret grants, then project grants.
//
// # Non-cascading access for secrets (intentional)
//
// Hierarchy walking is used ONLY to collect default-share grants at create
// time (org → folders → project → new secret). It is NOT used for access
// checks on existing secrets. This is a principled design decision:
//
//   - Templates are policy: it is appropriate for an org-level OWNER to apply
//     a platform template everywhere — the template is a policy artifact that
//     must reach every project.
//   - Secrets are data: an org-level OWNER should NOT automatically be able to
//     read a secret stored in a specific project namespace unless they have an
//     explicit grant on that secret or project (ADR 007). Secrets may contain
//     credentials that are intentionally isolated to a team.
//
// The access path for secrets is:
//
//  1. Per-secret grants (full permission via PermissionSecretsRead/Write/Delete).
//  2. Project grants via ProjectCascadeSecretPerms (EDITORs and OWNERs may
//     write; VIEWERs cannot read — PermissionSecretsRead never cascades).
//
// PermissionSecretsRead is NOT in ProjectCascadeSecretPerms. This means even a
// project-level OWNER cannot read a specific secret via cascade — they need an
// explicit per-secret grant. An org-level OWNER (with no project grant) is
// denied at step 2 as well. Organization grants do not cascade at all
// (see docs/adrs/007-org-grants-no-cascade.md).
//
// To gain read access through the default-share mechanism, the secret's
// default-share grants (inherited at create time from ancestors) must include
// the user's email or role — these appear as direct per-secret grants at step 1.
func (h *Handler) checkAccess(
	email string,
	roles []string,
	secretUsers, secretRoles map[string]string,
	projUsers, projRoles map[string]string,
	permission rbac.Permission,
) error {
	// 1. Check per-secret grants (full permission)
	if err := rbac.CheckAccessGrants(email, roles, secretUsers, secretRoles, permission); err == nil {
		return nil
	}

	// 2. Check project grants (role-per-scope cascade table)
	if projUsers != nil || projRoles != nil {
		if err := rbac.CheckCascadeAccess(email, roles, projUsers, projRoles, permission, rbac.ProjectCascadeSecretPerms); err == nil {
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
func logAuditAllowed(ctx context.Context, claims *rpc.Claims, secret, project string) {
	slog.InfoContext(ctx, "secret access granted",
		slog.String("action", "secret_access"),
		slog.String("resource_type", auditResourceType),
		slog.String("secret", secret),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Any("roles", claims.Roles),
	)
}

// logAuditDenied logs a denied secret access.
func logAuditDenied(ctx context.Context, claims *rpc.Claims, secret, project string) {
	slog.WarnContext(ctx, "secret access denied",
		slog.String("action", "secret_access_denied"),
		slog.String("resource_type", auditResourceType),
		slog.String("secret", secret),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Any("roles", claims.Roles),
	)
}
