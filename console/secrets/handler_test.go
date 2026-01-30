package secrets

import (
	"context"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/holos-run/holos-console/console/rbac"
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
		// Given: Authenticated user in allowed-roles, secret exists
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["viewer","editor"]`,
				},
			},
			Data: map[string][]byte{
				"username": []byte("admin"),
				"password": []byte("secret123"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		// Create authenticated context with matching role group
		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
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
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

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
		// Given: Authenticated user NOT in allowed-roles
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["owner","editor"]`,
				},
			},
			Data: map[string][]byte{
				"username": []byte("admin"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		// Create authenticated context with non-matching role group
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
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

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
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

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
					AllowedRolesAnnotation: `["owner"]`,
				},
			},
			Data: map[string][]byte{
				"key": []byte("value"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		// Capture logs
		logHandler := &testLogHandler{}
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(logHandler))
		defer slog.SetDefault(oldLogger)

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"owner"},
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
					AllowedRolesAnnotation: `["owner","editor"]`,
				},
			},
			Data: map[string][]byte{
				"key": []byte("value"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

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

func TestHandler_DeleteSecret(t *testing.T) {
	t.Run("returns success for authorized owner", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["owner"]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "my-secret"})

		_, err := handler.DeleteSecret(ctx, req)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("returns Unauthenticated for missing auth", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "my-secret"})

		_, err := handler.DeleteSecret(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connectErr.Code())
		}
	})

	t.Run("returns PermissionDenied for editor", func(t *testing.T) {
		// Editor lacks PERMISSION_SECRETS_DELETE
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["owner"]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "my-secret"})

		_, err := handler.DeleteSecret(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connectErr.Code())
		}
	})

	t.Run("returns PermissionDenied for viewer", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["owner"]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "my-secret"})

		_, err := handler.DeleteSecret(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
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
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "missing"})

		_, err := handler.DeleteSecret(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeNotFound {
			t.Errorf("expected CodeNotFound, got %v", connectErr.Code())
		}
	})

	t.Run("returns InvalidArgument for empty name", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: ""})

		_, err := handler.DeleteSecret(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
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

func TestHandler_DeleteSecret_AuditLogging(t *testing.T) {
	t.Run("logs secret_delete on success", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["owner"]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		logHandler := &testLogHandler{}
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(logHandler))
		defer slog.SetDefault(oldLogger)

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "my-secret"})

		_, err := handler.DeleteSecret(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		record := logHandler.findRecord("secret_delete")
		if record == nil {
			t.Fatal("expected log record with action='secret_delete', got none")
		}
		if record.Level != slog.LevelInfo {
			t.Errorf("expected Info level, got %v", record.Level)
		}
	})

	t.Run("logs secret_delete_denied on RBAC failure", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["owner"]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		logHandler := &testLogHandler{}
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(logHandler))
		defer slog.SetDefault(oldLogger)

		claims := &rpc.Claims{
			Sub:    "user-456",
			Email:  "other@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "my-secret"})

		_, err := handler.DeleteSecret(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		record := logHandler.findRecord("secret_delete_denied")
		if record == nil {
			t.Fatal("expected log record with action='secret_delete_denied', got none")
		}
		if record.Level != slog.LevelWarn {
			t.Errorf("expected Warn level, got %v", record.Level)
		}
	})
}

func TestHandler_CreateSecret(t *testing.T) {
	t.Run("returns success with created secret name for authorized editor", func(t *testing.T) {
		// Given: No secrets exist, user is editor
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:         "new-secret",
			Data:         map[string][]byte{"key": []byte("value")},
			AllowedRoles: []string{"editor"},
		})

		// When: CreateSecret RPC is called
		resp, err := handler.CreateSecret(ctx, req)

		// Then: Returns success with name
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Name != "new-secret" {
			t.Errorf("expected name 'new-secret', got %q", resp.Msg.Name)
		}
	})

	t.Run("returns Unauthenticated for missing auth", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:         "new-secret",
			Data:         map[string][]byte{"k": []byte("v")},
			AllowedRoles: []string{"editor"},
		})

		_, err := handler.CreateSecret(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connectErr.Code())
		}
	})

	t.Run("returns PermissionDenied for viewer", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:         "new-secret",
			Data:         map[string][]byte{"k": []byte("v")},
			AllowedRoles: []string{"editor"},
		})

		_, err := handler.CreateSecret(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connectErr.Code())
		}
	})

	t.Run("returns InvalidArgument for empty name", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:         "",
			Data:         map[string][]byte{"k": []byte("v")},
			AllowedRoles: []string{"editor"},
		})

		_, err := handler.CreateSecret(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
		}
	})

	t.Run("returns InvalidArgument for empty allowed_roles", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:         "new-secret",
			Data:         map[string][]byte{"k": []byte("v")},
			AllowedRoles: []string{},
		})

		_, err := handler.CreateSecret(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
		}
	})

	t.Run("returns AlreadyExists for duplicate secret name", func(t *testing.T) {
		existing := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "existing-secret",
				Namespace: "test-namespace",
			},
		}
		fakeClient := fake.NewClientset(existing)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:         "existing-secret",
			Data:         map[string][]byte{"k": []byte("v")},
			AllowedRoles: []string{"editor"},
		})

		_, err := handler.CreateSecret(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeAlreadyExists {
			t.Errorf("expected CodeAlreadyExists, got %v", connectErr.Code())
		}
	})
}

func TestHandler_CreateSecret_AuditLogging(t *testing.T) {
	t.Run("logs secret_create on success", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		logHandler := &testLogHandler{}
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(logHandler))
		defer slog.SetDefault(oldLogger)

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:         "new-secret",
			Data:         map[string][]byte{"k": []byte("v")},
			AllowedRoles: []string{"editor"},
		})

		_, err := handler.CreateSecret(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		record := logHandler.findRecord("secret_create")
		if record == nil {
			t.Fatal("expected log record with action='secret_create', got none")
		}
		if record.Level != slog.LevelInfo {
			t.Errorf("expected Info level, got %v", record.Level)
		}
	})

	t.Run("logs secret_create_denied on RBAC failure", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		logHandler := &testLogHandler{}
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(logHandler))
		defer slog.SetDefault(oldLogger)

		claims := &rpc.Claims{
			Sub:    "user-456",
			Email:  "other@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:         "new-secret",
			Data:         map[string][]byte{"k": []byte("v")},
			AllowedRoles: []string{"editor"},
		})

		_, err := handler.CreateSecret(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		record := logHandler.findRecord("secret_create_denied")
		if record == nil {
			t.Fatal("expected log record with action='secret_create_denied', got none")
		}
		if record.Level != slog.LevelWarn {
			t.Errorf("expected Warn level, got %v", record.Level)
		}
	})
}

func TestHandler_UpdateSecret(t *testing.T) {
	t.Run("returns success for authorized editor", func(t *testing.T) {
		// Given: Managed secret with editor access, user is editor
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["editor"]`,
				},
			},
			Data: map[string][]byte{
				"old-key": []byte("old-value"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name: "my-secret",
			Data: map[string][]byte{
				"new-key": []byte("new-value"),
			},
		})

		// When: UpdateSecret RPC is called
		_, err := handler.UpdateSecret(ctx, req)

		// Then: Returns success
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("returns Unauthenticated for missing auth", func(t *testing.T) {
		// Given: Request without claims
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name: "my-secret",
			Data: map[string][]byte{"k": []byte("v")},
		})

		// When: UpdateSecret RPC is called
		_, err := handler.UpdateSecret(ctx, req)

		// Then: Returns Unauthenticated
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connectErr.Code())
		}
	})

	t.Run("returns PermissionDenied for viewer", func(t *testing.T) {
		// Given: Secret allows editor, user is only viewer
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["editor"]`,
				},
			},
			Data: map[string][]byte{"k": []byte("v")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name: "my-secret",
			Data: map[string][]byte{"k": []byte("v")},
		})

		// When: UpdateSecret RPC is called
		_, err := handler.UpdateSecret(ctx, req)

		// Then: Returns PermissionDenied
		if err == nil {
			t.Fatal("expected error, got nil")
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
		// Given: Secret does not exist
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name: "missing",
			Data: map[string][]byte{"k": []byte("v")},
		})

		// When: UpdateSecret RPC is called
		_, err := handler.UpdateSecret(ctx, req)

		// Then: Returns NotFound
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeNotFound {
			t.Errorf("expected CodeNotFound, got %v", connectErr.Code())
		}
	})

	t.Run("returns InvalidArgument for empty name", func(t *testing.T) {
		// Given: Request with empty name
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name: "",
			Data: map[string][]byte{"k": []byte("v")},
		})

		// When: UpdateSecret RPC is called
		_, err := handler.UpdateSecret(ctx, req)

		// Then: Returns InvalidArgument
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
		}
	})

	t.Run("returns InvalidArgument for empty data", func(t *testing.T) {
		// Given: Request with empty data
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name: "my-secret",
			Data: map[string][]byte{},
		})

		// When: UpdateSecret RPC is called
		_, err := handler.UpdateSecret(ctx, req)

		// Then: Returns InvalidArgument
		if err == nil {
			t.Fatal("expected error, got nil")
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

func TestHandler_UpdateSecret_AuditLogging(t *testing.T) {
	t.Run("logs secret_update on success", func(t *testing.T) {
		// Given: Successful update setup
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["editor"]`,
				},
			},
			Data: map[string][]byte{"k": []byte("v")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		logHandler := &testLogHandler{}
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(logHandler))
		defer slog.SetDefault(oldLogger)

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name: "my-secret",
			Data: map[string][]byte{"new-key": []byte("new-value")},
		})

		// When: UpdateSecret succeeds
		_, err := handler.UpdateSecret(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Then: Logs action=secret_update
		record := logHandler.findRecord("secret_update")
		if record == nil {
			t.Fatal("expected log record with action='secret_update', got none")
		}
		if record.Level != slog.LevelInfo {
			t.Errorf("expected Info level, got %v", record.Level)
		}
	})

	t.Run("logs secret_update_denied on RBAC failure", func(t *testing.T) {
		// Given: User lacks write permission
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["owner"]`,
				},
			},
			Data: map[string][]byte{"k": []byte("v")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		logHandler := &testLogHandler{}
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(logHandler))
		defer slog.SetDefault(oldLogger)

		claims := &rpc.Claims{
			Sub:    "user-456",
			Email:  "other@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name: "my-secret",
			Data: map[string][]byte{"k": []byte("v")},
		})

		// When: UpdateSecret is denied
		_, err := handler.UpdateSecret(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		// Then: Logs action=secret_update_denied
		record := logHandler.findRecord("secret_update_denied")
		if record == nil {
			t.Fatal("expected log record with action='secret_update_denied', got none")
		}
		if record.Level != slog.LevelWarn {
			t.Errorf("expected Warn level, got %v", record.Level)
		}
	})
}

func TestHandler_GetSecret_MultipleKeys(t *testing.T) {
	t.Run("returns secret with multiple data keys", func(t *testing.T) {
		// Given: secret with multiple data keys
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "multi-key-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["owner"]`,
				},
			},
			Data: map[string][]byte{
				"username": []byte("test-user"),
				"password": []byte("test-password"),
				"api-key":  []byte("test-api-key-12345"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "admin",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name: "multi-key-secret",
		})

		// When: GetSecret RPC is called
		resp, err := handler.GetSecret(ctx, req)

		// Then: Returns all secret data keys
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp == nil {
			t.Fatal("expected response, got nil")
		}
		if string(resp.Msg.Data["username"]) != "test-user" {
			t.Errorf("expected username 'test-user', got %q", string(resp.Msg.Data["username"]))
		}
		if string(resp.Msg.Data["password"]) != "test-password" {
			t.Errorf("expected password 'test-password', got %q", string(resp.Msg.Data["password"]))
		}
		if string(resp.Msg.Data["api-key"]) != "test-api-key-12345" {
			t.Errorf("expected api-key 'test-api-key-12345', got %q", string(resp.Msg.Data["api-key"]))
		}
	})
}

func TestHandler_ListSecrets(t *testing.T) {
	t.Run("returns only secrets with console label", func(t *testing.T) {
		// Given: Multiple secrets, some with console label, some without
		secretWithLabel := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "labeled-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["owner"]`,
				},
			},
		}
		secretWithoutLabel := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unlabeled-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["owner"]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secretWithLabel, secretWithoutLabel)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.ListSecretsRequest{})

		// When: ListSecrets RPC is called
		resp, err := handler.ListSecrets(ctx, req)

		// Then: Returns only the labeled secret with accessibility info
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp == nil {
			t.Fatal("expected response, got nil")
		}
		if len(resp.Msg.Secrets) != 1 {
			t.Fatalf("expected 1 secret, got %d", len(resp.Msg.Secrets))
		}
		if resp.Msg.Secrets[0].Name != "labeled-secret" {
			t.Errorf("expected 'labeled-secret', got %q", resp.Msg.Secrets[0].Name)
		}
		if !resp.Msg.Secrets[0].Accessible {
			t.Error("expected secret to be accessible")
		}
		if len(resp.Msg.Secrets[0].AllowedRoles) != 1 || resp.Msg.Secrets[0].AllowedRoles[0] != "owner" {
			t.Errorf("expected allowed_roles=['owner'], got %v", resp.Msg.Secrets[0].AllowedRoles)
		}
	})

	t.Run("returns all secrets with accessibility info", func(t *testing.T) {
		// Given: Two labeled secrets, user can only access one
		accessibleSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "accessible-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["viewer"]`,
				},
			},
		}
		inaccessibleSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "inaccessible-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					AllowedRolesAnnotation: `["owner"]`,
				},
			},
		}
		fakeClient := fake.NewClientset(accessibleSecret, inaccessibleSecret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.ListSecretsRequest{})

		// When: ListSecrets RPC is called
		resp, err := handler.ListSecrets(ctx, req)

		// Then: Returns both secrets with accessibility info
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.Secrets) != 2 {
			t.Fatalf("expected 2 secrets, got %d", len(resp.Msg.Secrets))
		}

		// Find each secret and verify accessibility
		var accessible, inaccessible *consolev1.SecretMetadata
		for _, s := range resp.Msg.Secrets {
			switch s.Name {
			case "accessible-secret":
				accessible = s
			case "inaccessible-secret":
				inaccessible = s
			}
		}

		if accessible == nil {
			t.Fatal("expected to find 'accessible-secret'")
		}
		if !accessible.Accessible {
			t.Error("expected accessible-secret to be accessible")
		}
		if len(accessible.AllowedRoles) != 1 || accessible.AllowedRoles[0] != "viewer" {
			t.Errorf("expected allowed_roles=['viewer'], got %v", accessible.AllowedRoles)
		}

		if inaccessible == nil {
			t.Fatal("expected to find 'inaccessible-secret'")
		}
		if inaccessible.Accessible {
			t.Error("expected inaccessible-secret to not be accessible")
		}
		if len(inaccessible.AllowedRoles) != 1 || inaccessible.AllowedRoles[0] != "owner" {
			t.Errorf("expected allowed_roles=['owner'], got %v", inaccessible.AllowedRoles)
		}
	})

	t.Run("returns Unauthenticated for missing auth", func(t *testing.T) {
		// Given: Request without claims in context
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.ListSecretsRequest{})

		// When: ListSecrets RPC is called
		_, err := handler.ListSecrets(ctx, req)

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

	t.Run("returns empty list when no secrets match", func(t *testing.T) {
		// Given: No secrets with console label
		secretWithoutLabel := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unlabeled-secret",
				Namespace: "test-namespace",
			},
		}
		fakeClient := fake.NewClientset(secretWithoutLabel)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"admin"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.ListSecretsRequest{})

		// When: ListSecrets RPC is called
		resp, err := handler.ListSecrets(ctx, req)

		// Then: Returns empty list
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.Secrets) != 0 {
			t.Errorf("expected 0 secrets, got %d", len(resp.Msg.Secrets))
		}
	})
}

func TestHandler_UpdateSharing(t *testing.T) {
	t.Run("owner can update sharing grants", func(t *testing.T) {
		// Given: Secret with owner share-users grant for the caller
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					ShareUsersAnnotation: `{"alice@example.com":"owner"}`,
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "alice@example.com",
			Groups: []string{},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name: "my-secret",
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "alice@example.com", Role: consolev1.Role_ROLE_OWNER},
				{Principal: "bob@example.com", Role: consolev1.Role_ROLE_VIEWER},
			},
			GroupGrants: []*consolev1.ShareGrant{
				{Principal: "dev-team", Role: consolev1.Role_ROLE_EDITOR},
			},
		})

		// When: UpdateSharing RPC is called
		resp, err := handler.UpdateSharing(ctx, req)

		// Then: Returns success with updated metadata
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Metadata == nil {
			t.Fatal("expected metadata in response")
		}
		if resp.Msg.Metadata.Name != "my-secret" {
			t.Errorf("expected name 'my-secret', got %q", resp.Msg.Metadata.Name)
		}

		// Verify annotations were persisted
		updated, err := k8sClient.GetSecret(ctx, "my-secret")
		if err != nil {
			t.Fatalf("failed to get updated secret: %v", err)
		}
		shareUsers, err := GetShareUsers(updated)
		if err != nil {
			t.Fatalf("failed to parse share-users: %v", err)
		}
		if shareUsers["alice@example.com"] != "owner" {
			t.Errorf("expected alice=owner, got %q", shareUsers["alice@example.com"])
		}
		if shareUsers["bob@example.com"] != "viewer" {
			t.Errorf("expected bob=viewer, got %q", shareUsers["bob@example.com"])
		}
		shareGroups, err := GetShareGroups(updated)
		if err != nil {
			t.Fatalf("failed to parse share-groups: %v", err)
		}
		if shareGroups["dev-team"] != "editor" {
			t.Errorf("expected dev-team=editor, got %q", shareGroups["dev-team"])
		}
	})

	t.Run("non-owner gets PermissionDenied", func(t *testing.T) {
		// Given: Secret where caller is only a viewer
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					ShareUsersAnnotation: `{"bob@example.com":"viewer"}`,
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-456",
			Email:  "bob@example.com",
			Groups: []string{},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name: "my-secret",
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "bob@example.com", Role: consolev1.Role_ROLE_OWNER},
			},
		})

		// When: UpdateSharing RPC is called
		_, err := handler.UpdateSharing(ctx, req)

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

	t.Run("returns Unauthenticated for missing auth", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name: "my-secret",
		})

		_, err := handler.UpdateSharing(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connectErr.Code())
		}
	})

	t.Run("returns InvalidArgument for empty name", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "alice@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name: "",
		})

		_, err := handler.UpdateSharing(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
		}
	})

	t.Run("returns NotFound for non-existent secret", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "alice@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name: "missing-secret",
		})

		_, err := handler.UpdateSharing(ctx, req)

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		connectErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connectErr.Code() != connect.CodeNotFound {
			t.Errorf("expected CodeNotFound, got %v", connectErr.Code())
		}
	})
}

func TestHandler_UpdateSharing_AuditLogging(t *testing.T) {
	t.Run("logs sharing_update on success", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					ShareUsersAnnotation: `{"alice@example.com":"owner"}`,
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		logHandler := &testLogHandler{}
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(logHandler))
		defer slog.SetDefault(oldLogger)

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "alice@example.com",
			Groups: []string{},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name: "my-secret",
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "alice@example.com", Role: consolev1.Role_ROLE_OWNER},
			},
		})

		_, err := handler.UpdateSharing(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		record := logHandler.findRecord("sharing_update")
		if record == nil {
			t.Fatal("expected log record with action='sharing_update', got none")
		}
		if record.Level != slog.LevelInfo {
			t.Errorf("expected Info level, got %v", record.Level)
		}
	})

	t.Run("logs sharing_update_denied on RBAC failure", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					ShareUsersAnnotation: `{"bob@example.com":"viewer"}`,
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		logHandler := &testLogHandler{}
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(logHandler))
		defer slog.SetDefault(oldLogger)

		claims := &rpc.Claims{
			Sub:    "user-456",
			Email:  "bob@example.com",
			Groups: []string{},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name: "my-secret",
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "bob@example.com", Role: consolev1.Role_ROLE_OWNER},
			},
		})

		_, err := handler.UpdateSharing(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		record := logHandler.findRecord("sharing_update_denied")
		if record == nil {
			t.Fatal("expected log record with action='sharing_update_denied', got none")
		}
		if record.Level != slog.LevelWarn {
			t.Errorf("expected Warn level, got %v", record.Level)
		}
	})
}
