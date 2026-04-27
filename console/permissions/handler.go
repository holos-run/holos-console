// Package permissions implements the PermissionsService Connect handler.
//
// The handler resolves the per-request impersonating Kubernetes client from
// rpc.ImpersonatedClientsetFromContext and fans the inbound list of
// ResourceAttributes out across SelfSubjectAccessReview, returning one
// decision per attribute. The API server is the single arbiter of access per
// ADR 036; this RPC exists so the frontend can decide which action buttons
// to render before the user clicks anything (the optional UI-gating contract
// from the ADR).
package permissions

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

// Handler implements consolev1connect.PermissionsServiceHandler.
type Handler struct {
	consolev1connect.UnimplementedPermissionsServiceHandler
}

// NewHandler returns a PermissionsService handler. The handler is stateless;
// every request resolves its Kubernetes client from the request context.
func NewHandler() *Handler { return &Handler{} }

// ListResourcePermissions issues one SelfSubjectAccessReview per requested
// attribute against the impersonating client and returns the decisions in the
// same order.
func (h *Handler) ListResourcePermissions(
	ctx context.Context,
	req *connect.Request[consolev1.ListResourcePermissionsRequest],
) (*connect.Response[consolev1.ListResourcePermissionsResponse], error) {
	if req == nil || req.Msg == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("request is required"))
	}
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}
	clientset := rpc.ImpersonatedClientsetFromContext(ctx)
	if clientset == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("no kubernetes client on request context"))
	}

	out := &consolev1.ListResourcePermissionsResponse{
		Permissions: make([]*consolev1.ResourcePermission, 0, len(req.Msg.Attributes)),
	}
	for _, attr := range req.Msg.Attributes {
		perm, err := selfSubjectAccessReview(ctx, clientset, attr)
		if err != nil {
			return nil, rpc.MapK8sError(err)
		}
		out.Permissions = append(out.Permissions, perm)
	}
	return connect.NewResponse(out), nil
}

// PermissionKey returns the deterministic lookup key the frontend uses to
// index the response. Exported so tests and the frontend hook can build the
// same key off the same input shape.
func PermissionKey(attr *consolev1.ResourceAttributes) string {
	if attr == nil {
		return ""
	}
	groupResource := attr.Group + "/" + attr.Resource
	if attr.Group == "" {
		groupResource = attr.Resource
	}
	if attr.Subresource != "" {
		groupResource = groupResource + "/" + attr.Subresource
	}
	parts := []string{attr.Verb, groupResource}
	if attr.Namespace != "" {
		parts = append(parts, attr.Namespace)
	}
	if attr.Name != "" {
		parts = append(parts, attr.Name)
	}
	return strings.Join(parts, ":")
}

func selfSubjectAccessReview(
	ctx context.Context,
	clientset kubernetes.Interface,
	attr *consolev1.ResourceAttributes,
) (*consolev1.ResourcePermission, error) {
	if attr == nil {
		return nil, fmt.Errorf("attributes are required")
	}
	ssar := &authzv1.SelfSubjectAccessReview{
		Spec: authzv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authzv1.ResourceAttributes{
				Verb:        attr.Verb,
				Group:       attr.Group,
				Resource:    attr.Resource,
				Subresource: attr.Subresource,
				Namespace:   attr.Namespace,
				Name:        attr.Name,
			},
		},
	}
	created, err := clientset.AuthorizationV1().
		SelfSubjectAccessReviews().
		Create(ctx, ssar, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return &consolev1.ResourcePermission{
		Attributes: attr,
		Allowed:    created.Status.Allowed,
		Denied:     created.Status.Denied,
		Reason:     created.Status.Reason,
		Key:        PermissionKey(attr),
	}, nil
}
