// Package rpc — errors.go centralises the apierrors -> connect.Code mapping
// so every ConnectRPC handler in console/* surfaces Kubernetes API errors
// with consistent semantics.
//
// Why this exists
//
// Each handler used to open-code its own mapK8sError with subtly different
// branches (some checked IsInvalid, others did not; some mapped IsConflict,
// others mapped Bad Request to CodeInvalidArgument but not Invalid). After
// ADR 036 moved authorization to Kubernetes RBAC with OIDC impersonation,
// the API server returns Forbidden / Unauthorized for the bulk of access
// decisions, so getting that mapping right uniformly across every handler
// matters: the UI relies on connect.CodePermissionDenied to render the
// access-denied toast, and connect.CodeUnauthenticated to trigger a
// re-login flow.
//
// MapK8sError is the canonical mapper. Per-handler mapK8sError shims
// continue to exist so callers can layer in handler-specific sentinels
// (e.g., organizations / folders / projects translate "not managed by"
// strings to CodeNotFound) before delegating here for the apierrors kinds
// they all share.
package rpc

import (
	"errors"

	"connectrpc.com/connect"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// MapK8sError converts a Kubernetes API error into the canonical
// ConnectRPC code. The mapping is:
//
//	apierrors.IsNotFound          -> CodeNotFound
//	apierrors.IsAlreadyExists     -> CodeAlreadyExists
//	apierrors.IsForbidden         -> CodePermissionDenied  (ADR 036: K8s RBAC arbitrates access)
//	apierrors.IsUnauthorized      -> CodeUnauthenticated
//	apierrors.IsConflict          -> CodeFailedPrecondition (resourceVersion clash, etc.)
//	apierrors.IsBadRequest /
//	apierrors.IsInvalid           -> CodeInvalidArgument   (CRD schema, OpenAPI, CEL admission)
//	apierrors.IsTimeout /
//	apierrors.IsServerTimeout     -> CodeDeadlineExceeded
//	apierrors.IsServiceUnavailable -> CodeUnavailable
//	apierrors.IsTooManyRequests   -> CodeResourceExhausted
//	(default)                     -> CodeInternal
//
// If err is already a *connect.Error (e.g., the caller pre-wrapped it),
// MapK8sError returns it unchanged so handler-level sentinels survive.
//
// A nil input returns nil so callers can write `return mapK8sError(err)`
// without an explicit nil-check on the happy path.
func MapK8sError(err error) error {
	if err == nil {
		return nil
	}
	// Preserve any caller-supplied connect.Error so handler-specific
	// sentinel mappings ("not managed by" -> CodeNotFound, business-rule
	// validation, etc.) survive a pass through MapK8sError.
	var ce *connect.Error
	if errors.As(err, &ce) {
		return err
	}
	switch {
	case apierrors.IsNotFound(err):
		return connect.NewError(connect.CodeNotFound, err)
	case apierrors.IsAlreadyExists(err):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case apierrors.IsForbidden(err):
		return connect.NewError(connect.CodePermissionDenied, err)
	case apierrors.IsUnauthorized(err):
		return connect.NewError(connect.CodeUnauthenticated, err)
	case apierrors.IsConflict(err):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case apierrors.IsBadRequest(err), apierrors.IsInvalid(err):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case apierrors.IsTimeout(err), apierrors.IsServerTimeout(err):
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	case apierrors.IsServiceUnavailable(err):
		return connect.NewError(connect.CodeUnavailable, err)
	case apierrors.IsTooManyRequests(err):
		return connect.NewError(connect.CodeResourceExhausted, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}
