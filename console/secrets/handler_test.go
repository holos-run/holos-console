package secrets

import (
	"context"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// testLogHandler captures log records for testing.
type testLogHandler struct {
	records []slog.Record
}

func (h *testLogHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *testLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func (h *testLogHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *testLogHandler) WithGroup(_ string) slog.Handler {
	return h
}

func (h *testLogHandler) findRecord(action string) *slog.Record {
	for _, r := range h.records {
		var foundAction string
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "action" {
				foundAction = a.Value.String()
				return false
			}
			return true
		})
		if foundAction == action {
			return &r
		}
	}
	return nil
}

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

func TestHandler_AuditLogging(t *testing.T) {
	t.Run("logs successful access with action secret_access", func(t *testing.T) {
		// Given: Successful secret access
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					AllowedGroupsAnnotation: `["admin"]`,
				},
			},
			Data: map[string][]byte{
				"key": []byte("value"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient)

		// Capture logs
		logHandler := &testLogHandler{}
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(logHandler))
		defer slog.SetDefault(oldLogger)

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"admin"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name: "my-secret",
		})

		// When: Request completes successfully
		_, err := handler.GetSecret(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Then: slog.Info with action="secret_access"
		record := logHandler.findRecord("secret_access")
		if record == nil {
			t.Fatal("expected log record with action='secret_access', got none")
		}
		if record.Level != slog.LevelInfo {
			t.Errorf("expected Info level, got %v", record.Level)
		}

		// Verify required attributes
		var foundSecret, foundSub, foundEmail string
		record.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case "secret":
				foundSecret = a.Value.String()
			case "sub":
				foundSub = a.Value.String()
			case "email":
				foundEmail = a.Value.String()
			}
			return true
		})
		if foundSecret != "my-secret" {
			t.Errorf("expected secret='my-secret', got %q", foundSecret)
		}
		if foundSub != "user-123" {
			t.Errorf("expected sub='user-123', got %q", foundSub)
		}
		if foundEmail != "user@example.com" {
			t.Errorf("expected email='user@example.com', got %q", foundEmail)
		}
	})

	t.Run("logs denied access with action secret_access_denied", func(t *testing.T) {
		// Given: Denied access (RBAC failure)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					AllowedGroupsAnnotation: `["admin","ops"]`,
				},
			},
			Data: map[string][]byte{
				"key": []byte("value"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient)

		// Capture logs
		logHandler := &testLogHandler{}
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(logHandler))
		defer slog.SetDefault(oldLogger)

		claims := &rpc.Claims{
			Sub:    "user-456",
			Email:  "other@example.com",
			Groups: []string{"developers"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name: "my-secret",
		})

		// When: Request is denied
		_, err := handler.GetSecret(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		// Then: slog.Warn with action="secret_access_denied"
		record := logHandler.findRecord("secret_access_denied")
		if record == nil {
			t.Fatal("expected log record with action='secret_access_denied', got none")
		}
		if record.Level != slog.LevelWarn {
			t.Errorf("expected Warn level, got %v", record.Level)
		}

		// Verify required attributes
		var foundSecret, foundSub, foundEmail string
		record.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case "secret":
				foundSecret = a.Value.String()
			case "sub":
				foundSub = a.Value.String()
			case "email":
				foundEmail = a.Value.String()
			}
			return true
		})
		if foundSecret != "my-secret" {
			t.Errorf("expected secret='my-secret', got %q", foundSecret)
		}
		if foundSub != "user-456" {
			t.Errorf("expected sub='user-456', got %q", foundSub)
		}
		if foundEmail != "other@example.com" {
			t.Errorf("expected email='other@example.com', got %q", foundEmail)
		}
	})
}

func TestHandler_DummySecret(t *testing.T) {
	t.Run("returns dummy secret when user in owner group", func(t *testing.T) {
		// Given: request for secret named "dummy-secret"
		fakeClient := fake.NewClientset() // No secrets in K8s
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient)

		// User in owner group (matches Dex connector default)
		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "admin",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name: DummySecretName,
		})

		// When: GetSecret RPC is called
		resp, err := handler.GetSecret(ctx, req)

		// Then: Returns in-memory dummy secret data
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp == nil {
			t.Fatal("expected response, got nil")
		}
		if string(resp.Msg.Data["username"]) != "dummy-user" {
			t.Errorf("expected username 'dummy-user', got %q", string(resp.Msg.Data["username"]))
		}
		if string(resp.Msg.Data["password"]) != "dummy-password" {
			t.Errorf("expected password 'dummy-password', got %q", string(resp.Msg.Data["password"]))
		}
		if string(resp.Msg.Data["api-key"]) != "dummy-api-key-12345" {
			t.Errorf("expected api-key 'dummy-api-key-12345', got %q", string(resp.Msg.Data["api-key"]))
		}
	})

	t.Run("returns PermissionDenied for dummy secret when user not in owner group", func(t *testing.T) {
		// Given: request for "dummy-secret"
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient)

		// User NOT in owner group
		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"developers"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name: DummySecretName,
		})

		// When: GetSecret RPC is called
		_, err := handler.GetSecret(ctx, req)

		// Then: Returns PermissionDenied
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

	t.Run("real K8s secrets work alongside dummy secret", func(t *testing.T) {
		// Given: request for real secret "real-secret" that exists in K8s
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "real-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					AllowedGroupsAnnotation: `["owner"]`,
				},
			},
			Data: map[string][]byte{
				"real-key": []byte("real-value"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient)

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "admin",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name: "real-secret",
		})

		// When: GetSecret RPC is called
		resp, err := handler.GetSecret(ctx, req)

		// Then: Returns the real secret from K8s
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp == nil {
			t.Fatal("expected response, got nil")
		}
		if string(resp.Msg.Data["real-key"]) != "real-value" {
			t.Errorf("expected real-key 'real-value', got %q", string(resp.Msg.Data["real-key"]))
		}
	})
}
