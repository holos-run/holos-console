package secrets

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
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

	// Check RBAC
	allowedGroups, err := GetAllowedGroups(secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := CheckAccess(claims.Groups, allowedGroups); err != nil {
		return nil, err
	}

	return connect.NewResponse(&consolev1.GetSecretResponse{
		Data: secret.Data,
	}), nil
}

// mapK8sError converts Kubernetes API errors to ConnectRPC errors.
func mapK8sError(err error) error {
	if errors.IsNotFound(err) {
		return connect.NewError(connect.CodeNotFound, err)
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
