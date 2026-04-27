package permissions

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	authzv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

func TestPermissionKey(t *testing.T) {
	cases := []struct {
		name string
		in   *consolev1.ResourceAttributes
		want string
	}{
		{
			name: "core resource cluster scoped",
			in:   &consolev1.ResourceAttributes{Verb: "list", Resource: "namespaces"},
			want: "list:namespaces",
		},
		{
			name: "namespaced",
			in: &consolev1.ResourceAttributes{
				Verb: "get", Group: "templates.holos.run",
				Resource: "templates", Namespace: "ns",
			},
			want: "get:templates.holos.run/templates:ns",
		},
		{
			name: "with name and subresource",
			in: &consolev1.ResourceAttributes{
				Verb: "update", Group: "apps", Resource: "deployments",
				Subresource: "status", Namespace: "ns", Name: "demo",
			},
			want: "update:apps/deployments/status:ns:demo",
		},
		{
			name: "nil safe",
			in:   nil,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PermissionKey(tc.in); got != tc.want {
				t.Fatalf("PermissionKey() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestListResourcePermissions_Allowed(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("create", "selfsubjectaccessreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		create, ok := action.(clienttesting.CreateAction)
		if !ok {
			return false, nil, nil
		}
		ssar := create.GetObject().(*authzv1.SelfSubjectAccessReview)
		ssar.Status = authzv1.SubjectAccessReviewStatus{Allowed: true, Reason: "ok"}
		return true, ssar, nil
	})

	ctx := rpc.ContextWithClaims(context.Background(), &rpc.Claims{Sub: "alice", Email: "alice@example.com"})
	ctx = rpc.ContextWithImpersonatedClients(ctx, &rpc.ImpersonatedClients{Clientset: clientset})

	h := NewHandler()
	resp, err := h.ListResourcePermissions(ctx, connect.NewRequest(&consolev1.ListResourcePermissionsRequest{
		Attributes: []*consolev1.ResourceAttributes{
			{Verb: "list", Resource: "namespaces"},
			{Verb: "get", Group: "templates.holos.run", Resource: "templates", Namespace: "ns", Name: "x"},
		},
	}))
	if err != nil {
		t.Fatalf("ListResourcePermissions returned %v", err)
	}
	if got := len(resp.Msg.Permissions); got != 2 {
		t.Fatalf("permissions length = %d, want 2", got)
	}
	for _, p := range resp.Msg.Permissions {
		if !p.Allowed {
			t.Fatalf("expected allowed=true, got %+v", p)
		}
		if p.Reason != "ok" {
			t.Fatalf("expected reason=ok, got %q", p.Reason)
		}
		if p.Key == "" {
			t.Fatalf("expected non-empty key")
		}
	}
	if resp.Msg.Permissions[0].Key != "list:namespaces" {
		t.Fatalf("key[0] = %q, want list:namespaces", resp.Msg.Permissions[0].Key)
	}
	if resp.Msg.Permissions[1].Key != "get:templates.holos.run/templates:ns:x" {
		t.Fatalf("key[1] = %q, want get:templates.holos.run/templates:ns:x", resp.Msg.Permissions[1].Key)
	}
}

func TestListResourcePermissions_Denied(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("create", "selfsubjectaccessreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		ssar := action.(clienttesting.CreateAction).GetObject().(*authzv1.SelfSubjectAccessReview)
		ssar.Status = authzv1.SubjectAccessReviewStatus{Allowed: false, Denied: true, Reason: "no rolebinding"}
		return true, ssar, nil
	})
	ctx := rpc.ContextWithClaims(context.Background(), &rpc.Claims{Sub: "alice"})
	ctx = rpc.ContextWithImpersonatedClients(ctx, &rpc.ImpersonatedClients{Clientset: clientset})

	h := NewHandler()
	resp, err := h.ListResourcePermissions(ctx, connect.NewRequest(&consolev1.ListResourcePermissionsRequest{
		Attributes: []*consolev1.ResourceAttributes{{Verb: "delete", Resource: "secrets", Namespace: "ns"}},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := resp.Msg.Permissions[0]
	if got.Allowed || !got.Denied {
		t.Fatalf("expected denied, got %+v", got)
	}
}

func TestListResourcePermissions_Unauthenticated(t *testing.T) {
	h := NewHandler()
	_, err := h.ListResourcePermissions(context.Background(), connect.NewRequest(&consolev1.ListResourcePermissionsRequest{}))
	if err == nil {
		t.Fatalf("expected error")
	}
	var ce *connect.Error
	if !errors.As(err, &ce) || ce.Code() != connect.CodeUnauthenticated {
		t.Fatalf("expected CodeUnauthenticated, got %v", err)
	}
}
