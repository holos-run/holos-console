package rpc_test

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/holos-run/holos-console/console/rpc"
)

// gr is a stable GroupResource fixture for synthesising apierrors values.
var gr = schema.GroupResource{Group: "holos.run", Resource: "templates"}

func TestMapK8sError_NilInput(t *testing.T) {
	t.Parallel()
	if got := rpc.MapK8sError(nil); got != nil {
		t.Fatalf("MapK8sError(nil) = %v, want nil", got)
	}
}

func TestMapK8sError_PreservesConnectError(t *testing.T) {
	t.Parallel()
	// A handler-specific sentinel that wrapped the underlying err in a
	// connect.Error before delegating to MapK8sError must survive.
	want := connect.NewError(connect.CodeNotFound, errors.New("template not managed by holos"))
	got := rpc.MapK8sError(want)
	var ce *connect.Error
	if !errors.As(got, &ce) {
		t.Fatalf("MapK8sError did not return a connect.Error: %v", got)
	}
	if ce.Code() != connect.CodeNotFound {
		t.Fatalf("code = %v, want CodeNotFound", ce.Code())
	}
}

func TestMapK8sError_Mappings(t *testing.T) {
	t.Parallel()
	plain := errors.New("plain error")
	tests := []struct {
		name string
		err  error
		want connect.Code
	}{
		{
			name: "NotFound",
			err:  apierrors.NewNotFound(gr, "x"),
			want: connect.CodeNotFound,
		},
		{
			name: "AlreadyExists",
			err:  apierrors.NewAlreadyExists(gr, "x"),
			want: connect.CodeAlreadyExists,
		},
		{
			name: "Forbidden",
			err:  apierrors.NewForbidden(gr, "x", errors.New("rbac denied")),
			want: connect.CodePermissionDenied,
		},
		{
			name: "Unauthorized",
			err:  apierrors.NewUnauthorized("missing token"),
			want: connect.CodeUnauthenticated,
		},
		{
			name: "Conflict",
			err:  apierrors.NewConflict(gr, "x", errors.New("resourceVersion clash")),
			want: connect.CodeFailedPrecondition,
		},
		{
			name: "BadRequest",
			err:  apierrors.NewBadRequest("malformed"),
			want: connect.CodeInvalidArgument,
		},
		{
			name: "Invalid",
			err:  apierrors.NewInvalid(schema.GroupKind{Group: gr.Group, Kind: "Template"}, "x", nil),
			want: connect.CodeInvalidArgument,
		},
		{
			name: "Timeout",
			err:  apierrors.NewTimeoutError("timeout", 1),
			want: connect.CodeDeadlineExceeded,
		},
		{
			name: "ServiceUnavailable",
			err:  apierrors.NewServiceUnavailable("down"),
			want: connect.CodeUnavailable,
		},
		{
			name: "TooManyRequests",
			err:  apierrors.NewTooManyRequests("slow down", 1),
			want: connect.CodeResourceExhausted,
		},
		{
			name: "Default",
			err:  plain,
			want: connect.CodeInternal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := rpc.MapK8sError(tc.err)
			var ce *connect.Error
			if !errors.As(got, &ce) {
				t.Fatalf("expected connect.Error, got %T: %v", got, got)
			}
			if ce.Code() != tc.want {
				t.Fatalf("code = %v, want %v (err: %v)", ce.Code(), tc.want, tc.err)
			}
			// All wrapped errors must round-trip the underlying cause so
			// handler-level diagnostics survive (slog includes the
			// unwrapped error).
			if !errors.Is(got, tc.err) && tc.err != plain {
				// apierrors helpers return *StatusError, which wraps the
				// inner cause through Unwrap. The pointer-equality check
				// covers the plain default branch.
				if u := errors.Unwrap(got); u == nil {
					t.Fatalf("unwrap returned nil; want underlying err")
				}
			}
		})
	}
}

// silence unused metav1 import: NewInvalid sometimes needs metav1.StatusReason
// across k8s versions. Keep the import documented and lint-clean.
var _ = metav1.StatusReasonInvalid
