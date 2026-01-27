package secrets

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

func TestHandler_GetSecret(t *testing.T) {
	t.Run("returns secret data for authorized user", func(t *testing.T) {
		// Given: Authenticated user in allowed-groups, secret exists
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					AllowedGroupsAnnotation: `["admin","readers"]`,
				},
			},
			Data: map[string][]byte{
				"username": []byte("admin"),
				"password": []byte("secret123"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient)

		// Create authenticated context with matching group
		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"readers"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name: "my-secret",
		})

		// When: GetSecret RPC is called
		resp, err := handler.GetSecret(ctx, req)

		// Then: Returns 200 with secret data map
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp == nil {
			t.Fatal("expected response, got nil")
		}
		if string(resp.Msg.Data["username"]) != "admin" {
			t.Errorf("expected username 'admin', got %q", string(resp.Msg.Data["username"]))
		}
		if string(resp.Msg.Data["password"]) != "secret123" {
			t.Errorf("expected password 'secret123', got %q", string(resp.Msg.Data["password"]))
		}
	})

	t.Run("returns Unauthenticated for missing auth", func(t *testing.T) {
		// Given: Request without claims in context (no Authorization header)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient)

		// Context without claims
		ctx := context.Background()
		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name: "my-secret",
		})

		// When: GetSecret RPC is called
		_, err := handler.GetSecret(ctx, req)

		// Then: Returns Unauthenticated error
		if err == nil {
			t.Fatal("expected Unauthenticated error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connectErr.Code())
		}
	})

	t.Run("returns PermissionDenied for unauthorized user", func(t *testing.T) {
		// Given: Authenticated user NOT in allowed-groups
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					AllowedGroupsAnnotation: `["admin","ops"]`,
				},
			},
			Data: map[string][]byte{
				"username": []byte("admin"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient)

		// Create authenticated context with non-matching group
		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"developers"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name: "my-secret",
		})

		// When: GetSecret RPC is called
		_, err := handler.GetSecret(ctx, req)

		// Then: Returns PermissionDenied with "RBAC: authorization denied" message
		if err == nil {
			t.Fatal("expected PermissionDenied error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connectErr.Code())
		}
	})

	t.Run("returns NotFound for non-existent secret", func(t *testing.T) {
		// Given: Authenticated user, secret does not exist
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient)

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"admin"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name: "missing-secret",
		})

		// When: GetSecret RPC is called
		_, err := handler.GetSecret(ctx, req)

		// Then: Returns NotFound error
		if err == nil {
			t.Fatal("expected NotFound error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeNotFound {
			t.Errorf("expected CodeNotFound, got %v", connectErr.Code())
		}
	})

	t.Run("returns InvalidArgument for empty secret name", func(t *testing.T) {
		// Given: Request with empty secret name
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient)

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"admin"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name: "",
		})

		// When: GetSecret RPC is called
		_, err := handler.GetSecret(ctx, req)

		// Then: Returns InvalidArgument error
		if err == nil {
			t.Fatal("expected InvalidArgument error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
		}
	})
}
