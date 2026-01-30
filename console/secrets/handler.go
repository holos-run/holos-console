package secrets

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

// Handler implements the SecretsService.
type Handler struct {
	consolev1connect.UnimplementedSecretsServiceHandler
	k8s          *K8sClient
	groupMapping *rbac.GroupMapping
}

// NewHandler creates a new SecretsService handler.
func NewHandler(k8s *K8sClient, groupMapping *rbac.GroupMapping) *Handler {
	return &Handler{k8s: k8s, groupMapping: groupMapping}
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

	// List secrets from Kubernetes with console label
	secretList, err := h.k8s.ListSecrets(ctx)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Build list with accessibility info for each secret
	var secrets []*consolev1.SecretMetadata
	var accessibleCount int
	for _, secret := range secretList.Items {
		shareUsers, _ := GetShareUsers(&secret)
		shareGroups, _ := GetShareGroups(&secret)
		accessible := CheckListAccessSharing(h.groupMapping, claims.Email, claims.Groups, shareUsers, shareGroups) == nil
		if accessible {
			accessibleCount++
		}
		metadata := buildSecretMetadata(&secret, h.groupMapping, claims)
		secrets = append(secrets, metadata)
	}

	slog.InfoContext(ctx, "secrets listed",
		slog.String("action", "secrets_list"),
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

	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Get secret from Kubernetes
	secret, err := h.k8s.GetSecret(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	return h.returnSecret(ctx, claims, secret)
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

	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Get existing secret to check RBAC
	secret, err := h.k8s.GetSecret(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Check RBAC for delete access (sharing-aware)
	shareUsers, _ := GetShareUsers(secret)
	shareGroups, _ := GetShareGroups(secret)
	if err := CheckDeleteAccessSharing(h.groupMapping, claims.Email, claims.Groups, shareUsers, shareGroups); err != nil {
		slog.WarnContext(ctx, "secret delete denied",
			slog.String("action", "secret_delete_denied"),
			slog.String("secret", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
			slog.Any("user_groups", claims.Groups),
		)
		return nil, err
	}

	// Perform the delete
	if err := h.k8s.DeleteSecret(ctx, req.Msg.Name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "secret deleted",
		slog.String("action", "secret_delete"),
		slog.String("secret", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteSecretResponse{}), nil
}

// CreateSecret creates a new secret with RBAC authorization.
// Since the secret doesn't exist yet, authorization is checked against the user's own roles.
func (h *Handler) CreateSecret(
	ctx context.Context,
	req *connect.Request[consolev1.CreateSecretRequest],
) (*connect.Response[consolev1.CreateSecretResponse], error) {
	// Validate request
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("secret name is required"))
	}

	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Convert proto ShareGrant slices to annotation maps
	shareUsers := shareGrantsToMap(req.Msg.UserGrants)
	shareGroups := shareGrantsToMap(req.Msg.GroupGrants)

	// Check that the user has write permission based on the requested sharing grants.
	if err := CheckWriteAccessSharing(h.groupMapping, claims.Email, claims.Groups, shareUsers, shareGroups); err != nil {
		slog.WarnContext(ctx, "secret create denied",
			slog.String("action", "secret_create_denied"),
			slog.String("secret", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Create the secret
	_, err := h.k8s.CreateSecret(ctx, req.Msg.Name, req.Msg.Data, shareUsers, shareGroups)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "secret created",
		slog.String("action", "secret_create"),
		slog.String("secret", req.Msg.Name),
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
	if len(req.Msg.Data) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("secret data is required"))
	}

	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Get existing secret to check RBAC
	secret, err := h.k8s.GetSecret(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Check RBAC for write access (sharing-aware)
	shareUsers, _ := GetShareUsers(secret)
	shareGroups, _ := GetShareGroups(secret)
	if err := CheckWriteAccessSharing(h.groupMapping, claims.Email, claims.Groups, shareUsers, shareGroups); err != nil {
		logAuditDenied(ctx, claims, secret.Name)
		slog.WarnContext(ctx, "secret update denied",
			slog.String("action", "secret_update_denied"),
			slog.String("secret", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Perform the update
	if _, err := h.k8s.UpdateSecret(ctx, req.Msg.Name, req.Msg.Data); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "secret updated",
		slog.String("action", "secret_update"),
		slog.String("secret", req.Msg.Name),
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

	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Get existing secret to check RBAC
	secret, err := h.k8s.GetSecret(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Check RBAC for admin access (owner only, sharing-aware)
	shareUsers, _ := GetShareUsers(secret)
	shareGroups, _ := GetShareGroups(secret)
	if err := CheckAdminAccessSharing(h.groupMapping, claims.Email, claims.Groups, shareUsers, shareGroups); err != nil {
		slog.WarnContext(ctx, "sharing update denied",
			slog.String("action", "sharing_update_denied"),
			slog.String("secret", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Convert proto ShareGrant slices to annotation maps
	newShareUsers := shareGrantsToMap(req.Msg.UserGrants)
	newShareGroups := shareGrantsToMap(req.Msg.GroupGrants)

	// Persist the sharing annotations
	updated, err := h.k8s.UpdateSharing(ctx, req.Msg.Name, newShareUsers, newShareGroups)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "sharing updated",
		slog.String("action", "sharing_update"),
		slog.String("secret", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	// Build response metadata
	metadata := buildSecretMetadata(updated, h.groupMapping, claims)

	return connect.NewResponse(&consolev1.UpdateSharingResponse{
		Metadata: metadata,
	}), nil
}

// shareGrantsToMap converts a slice of ShareGrant protos to a map[string]string
// suitable for storing as a Kubernetes annotation.
func shareGrantsToMap(grants []*consolev1.ShareGrant) map[string]string {
	result := make(map[string]string, len(grants))
	for _, g := range grants {
		if g.Principal != "" {
			result[g.Principal] = strings.ToLower(g.Role.String()[len("ROLE_"):])
		}
	}
	return result
}

// buildSecretMetadata creates SecretMetadata for a secret from the caller's perspective.
func buildSecretMetadata(secret *corev1.Secret, gm *rbac.GroupMapping, claims *rpc.Claims) *consolev1.SecretMetadata {
	shareUsers, _ := GetShareUsers(secret)
	shareGroups, _ := GetShareGroups(secret)
	accessible := CheckListAccessSharing(gm, claims.Email, claims.Groups, shareUsers, shareGroups) == nil

	// Build user grants
	var userGrants []*consolev1.ShareGrant
	for email, role := range shareUsers {
		userGrants = append(userGrants, &consolev1.ShareGrant{
			Principal: email,
			Role:      protoRoleFromString(role),
		})
	}
	// Build group grants
	var groupGrants []*consolev1.ShareGrant
	for group, role := range shareGroups {
		groupGrants = append(groupGrants, &consolev1.ShareGrant{
			Principal: group,
			Role:      protoRoleFromString(role),
		})
	}

	return &consolev1.SecretMetadata{
		Name:        secret.Name,
		Accessible:  accessible,
		UserGrants:  userGrants,
		GroupGrants: groupGrants,
	}
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
func (h *Handler) returnSecret(ctx context.Context, claims *rpc.Claims, secret *corev1.Secret) (*connect.Response[consolev1.GetSecretResponse], error) {
	// Check RBAC (sharing-aware)
	shareUsers, _ := GetShareUsers(secret)
	shareGroups, _ := GetShareGroups(secret)
	if err := CheckReadAccessSharing(h.groupMapping, claims.Email, claims.Groups, shareUsers, shareGroups); err != nil {
		logAuditDenied(ctx, claims, secret.Name)
		return nil, err
	}

	logAuditAllowed(ctx, claims, secret.Name)

	return connect.NewResponse(&consolev1.GetSecretResponse{
		Data: secret.Data,
	}), nil
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
		slog.String("secret", secret),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Any("user_groups", claims.Groups),
	)
}
