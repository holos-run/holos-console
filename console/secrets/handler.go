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

	k8s := h.requestK8s(ctx)
	secretList, err := k8s.ListSecrets(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}
	shareUsers, shareRoles, err := k8s.ListSharing(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}

	var secrets []*consolev1.SecretMetadata
	for _, secret := range secretList.Items {
		metadata := h.buildSecretMetadata(&secret, displayUserGrants(shareUsers, claims), shareRoles, true)
		secrets = append(secrets, metadata)
	}

	slog.InfoContext(ctx, "secrets listed",
		slog.String("action", "secrets_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("total", len(secrets)),
		slog.Int("accessible", len(secrets)),
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
	secret, err := h.requestK8s(ctx).GetSecret(ctx, project, req.Msg.Name)
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

	if err := h.requestK8s(ctx).DeleteSecret(ctx, project, req.Msg.Name); err != nil {
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

	shareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	shareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)

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
	k8s := h.requestK8s(ctx)
	_, err := k8s.CreateSecret(ctx, project, req.Msg.Name, data, nil, nil, description, url)
	if err != nil {
		return nil, mapK8sError(err)
	}
	if len(shareUsers) > 0 || len(shareRoles) > 0 {
		shareUsers = rbacUserGrantsForClaims(shareUsers, claims)
		if _, err := k8s.UpdateSharing(ctx, project, req.Msg.Name, shareUsers, shareRoles); err != nil {
			return nil, mapK8sError(err)
		}
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

	// Merge string_data into data (string_data takes precedence)
	data := mergeStringData(req.Msg.Data, req.Msg.StringData)

	if _, err := h.requestK8s(ctx).UpdateSecret(ctx, project, req.Msg.Name, data, req.Msg.Description, req.Msg.Url); err != nil {
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

	k8s := h.requestK8s(ctx)

	// Convert proto ShareGrant slices to annotation grants
	newShareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	newShareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)
	newShareUsers = rbacUserGrantsForClaims(newShareUsers, claims)

	updated, err := k8s.UpdateSharing(ctx, project, req.Msg.Name, newShareUsers, newShareRoles)
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

	updatedUsers, updatedRoles, err := k8s.ListSharing(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}
	metadata := h.buildSecretMetadata(updated, displayUserGrants(updatedUsers, claims), updatedRoles, true)

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
	secret, err := h.requestK8s(ctx).GetSecret(ctx, project, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
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

// ensureCreatorOwner ensures the caller principal is in the share-users list as owner.
func ensureCreatorOwner(shareUsers []AnnotationGrant, principal string) []AnnotationGrant {
	if principal == "" {
		return shareUsers
	}
	principalLower := strings.ToLower(principal)
	for _, g := range shareUsers {
		if strings.ToLower(g.Principal) == principalLower && strings.ToLower(g.Role) == "owner" {
			return shareUsers
		}
	}
	return DeduplicateGrants(append(shareUsers, AnnotationGrant{Principal: principal, Role: "owner"}))
}

func rbacUserGrantsForClaims(shareUsers []AnnotationGrant, claims *rpc.Claims) []AnnotationGrant {
	if claims == nil || claims.Sub == "" {
		return shareUsers
	}
	shareUsers = removeGrantPrincipal(shareUsers, claims.Email)
	return ensureCreatorOwner(shareUsers, claims.Sub)
}

func removeGrantPrincipal(grants []AnnotationGrant, principal string) []AnnotationGrant {
	if principal == "" {
		return grants
	}
	principalLower := strings.ToLower(principal)
	filtered := make([]AnnotationGrant, 0, len(grants))
	for _, grant := range grants {
		if strings.ToLower(grant.Principal) == principalLower {
			continue
		}
		filtered = append(filtered, grant)
	}
	return filtered
}

func displayUserGrants(shareUsers []AnnotationGrant, claims *rpc.Claims) []AnnotationGrant {
	if claims == nil || claims.Sub == "" || claims.Email == "" {
		return shareUsers
	}
	out := append([]AnnotationGrant(nil), shareUsers...)
	for i := range out {
		if strings.TrimPrefix(out[i].Principal, "oidc:") == claims.Sub {
			out[i].Principal = claims.Email
		}
	}
	return out
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
func (h *Handler) buildSecretMetadata(secret *corev1.Secret, shareUsers, shareRoles []AnnotationGrant, accessible bool) *consolev1.SecretMetadata {
	// Build user grants (all grants, including expired, for display)
	userGrants := annotationGrantsToProto(shareUsers)
	// Build role grants
	roleGrants := annotationGrantsToProto(shareRoles)

	md := &consolev1.SecretMetadata{
		Name:       secret.Name,
		Accessible: accessible,
		UserGrants: userGrants,
		RoleGrants: roleGrants,
		CreatedAt:  secret.CreationTimestamp.UTC().Format(time.RFC3339),
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
	logAuditAllowed(ctx, claims, secret.Name, project)

	return connect.NewResponse(&consolev1.GetSecretResponse{
		Data: secret.Data,
	}), nil
}

func (h *Handler) requestK8s(ctx context.Context) *K8sClient {
	if !rpc.HasImpersonatedClients(ctx) {
		return h.k8s
	}
	return &K8sClient{
		client:   rpc.ImpersonatedClientsetFromContext(ctx),
		Resolver: h.k8s.Resolver,
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
