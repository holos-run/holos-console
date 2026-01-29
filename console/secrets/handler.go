package secrets

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

// Handler implements the SecretsService.
type Handler struct {
	consolev1connect.UnimplementedSecretsServiceHandler
	k8s *K8sClient
}

// NewHandler creates a new SecretsService handler.
func NewHandler(k8s *K8sClient) *Handler {
	return &Handler{k8s: k8s}
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
		allowedRoles, err := GetAllowedRoles(&secret)
		if err != nil {
			// Skip secrets with invalid annotations
			continue
		}
		accessible := CheckListAccess(claims.Groups, allowedRoles) == nil
		if accessible {
			accessibleCount++
		}
		secrets = append(secrets, &consolev1.SecretMetadata{
			Name:         secret.Name,
			Accessible:   accessible,
			AllowedRoles: allowedRoles,
		})
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

	// Check RBAC for delete access
	allowedRoles, err := GetAllowedRoles(secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := CheckDeleteAccess(claims.Groups, allowedRoles); err != nil {
		slog.WarnContext(ctx, "secret delete denied",
			slog.String("action", "secret_delete_denied"),
			slog.String("secret", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
			slog.Any("user_groups", claims.Groups),
			slog.Any("allowed_roles", allowedRoles),
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
	if len(req.Msg.AllowedRoles) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("allowed_roles is required"))
	}

	// Get claims from context (set by AuthInterceptor)
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Check that the user has write permission based on their own roles.
	// Use the requested allowed_roles as the resource roles for the access check.
	if err := CheckWriteAccess(claims.Groups, req.Msg.AllowedRoles); err != nil {
		slog.WarnContext(ctx, "secret create denied",
			slog.String("action", "secret_create_denied"),
			slog.String("secret", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Create the secret
	_, err := h.k8s.CreateSecret(ctx, req.Msg.Name, req.Msg.Data, req.Msg.AllowedRoles)
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

	// Check RBAC for write access
	allowedRoles, err := GetAllowedRoles(secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := CheckWriteAccess(claims.Groups, allowedRoles); err != nil {
		logAuditDenied(ctx, claims, secret.Name, allowedRoles)
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

// returnSecret checks RBAC and returns the secret data.
func (h *Handler) returnSecret(ctx context.Context, claims *rpc.Claims, secret *corev1.Secret) (*connect.Response[consolev1.GetSecretResponse], error) {
	// Check RBAC
	allowedRoles, err := GetAllowedRoles(secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := CheckReadAccess(claims.Groups, allowedRoles); err != nil {
		logAuditDenied(ctx, claims, secret.Name, allowedRoles)
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
func logAuditDenied(ctx context.Context, claims *rpc.Claims, secret string, allowedRoles []string) {
	slog.WarnContext(ctx, "secret access denied",
		slog.String("action", "secret_access_denied"),
		slog.String("secret", secret),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Any("user_groups", claims.Groups),
		slog.Any("allowed_roles", allowedRoles),
	)
}
