package secrets

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

// findAttr returns the string value of the named attribute on the record, or "" if not found.
func findAttr(r *slog.Record, key string) string {
	var val string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			val = a.Value.String()
			return false
		}
		return true
	})
	return val
}

// assertResourceType checks that the log record has resource_type="secret".
func assertResourceType(t *testing.T, r *slog.Record) {
	t.Helper()
	got := findAttr(r, "resource_type")
	if got != "secret" {
		t.Errorf("expected resource_type='secret', got %q", got)
	}
}

func TestHandler_GetSecret(t *testing.T) {
	t.Run("returns secret data for authorized user", func(t *testing.T) {
		// Given: Authenticated user in share-users, secret exists
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"viewer"}]`,
				},
			},
			Data: map[string][]byte{
				"username": []byte("admin"),
				"password": []byte("secret123"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		// Create authenticated context with matching email
		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name:    "my-secret",
			Project: "test-namespace",
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		// Context without claims
		ctx := context.Background()
		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name:    "my-secret",
			Project: "test-namespace",
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
		// Given: Authenticated user NOT in sharing annotations
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"other@example.com","role":"owner"}]`,
				},
			},
			Data: map[string][]byte{
				"username": []byte("admin"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		// Create authenticated context with non-matching email
		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"developers"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name:    "my-secret",
			Project: "test-namespace",
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"admin"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name:    "missing-secret",
			Project: "test-namespace",
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"admin"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name:    "",
			Project: "test-namespace",
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
					ShareGroupsAnnotation: `[{"principal":"owner","role":"owner"}]`,
				},
			},
			Data: map[string][]byte{
				"key": []byte("value"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
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
			Name:    "my-secret",
			Project: "test-namespace",
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
		assertResourceType(t, record)
	})

	t.Run("logs denied access with action secret_access_denied", func(t *testing.T) {
		// Given: Denied access (RBAC failure)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"alice@example.com","role":"owner"}]`,
				},
			},
			Data: map[string][]byte{
				"key": []byte("value"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
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
			Name:    "my-secret",
			Project: "test-namespace",
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
		assertResourceType(t, record)
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
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"owner"}]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "my-secret", Project: "test-namespace"})

		_, err := handler.DeleteSecret(ctx, req)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("returns Unauthenticated for missing auth", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "my-secret", Project: "test-namespace"})

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
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"editor"}]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "my-secret", Project: "test-namespace"})

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
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"viewer"}]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "my-secret", Project: "test-namespace"})

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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "missing", Project: "test-namespace"})

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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "", Project: "test-namespace"})

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
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"owner"}]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
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

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "my-secret", Project: "test-namespace"})

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
		assertResourceType(t, record)
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
					ShareUsersAnnotation: `[{"principal":"other@example.com","role":"editor"}]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
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

		req := connect.NewRequest(&consolev1.DeleteSecretRequest{Name: "my-secret", Project: "test-namespace"})

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
		assertResourceType(t, record)
	})
}

func TestHandler_CreateSecret(t *testing.T) {
	t.Run("returns success with created secret name for authorized editor", func(t *testing.T) {
		// Given: No secrets exist, user is editor
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:    "new-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{"key": []byte("value")},
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "user@example.com", Role: consolev1.Role_ROLE_EDITOR},
			},
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:    "new-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{"k": []byte("v")},
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "user@example.com", Role: consolev1.Role_ROLE_EDITOR},
			},
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		// Grants give viewer role to the caller — insufficient for write
		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:    "new-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{"k": []byte("v")},
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "user@example.com", Role: consolev1.Role_ROLE_VIEWER},
			},
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:    "",
			Project: "test-namespace",
			Data:    map[string][]byte{"k": []byte("v")},
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "user@example.com", Role: consolev1.Role_ROLE_EDITOR},
			},
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

	t.Run("returns PermissionDenied for empty grants and no platform role", func(t *testing.T) {
		// No per-secret grants and user not in any platform role group
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"some-other-group"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:    "new-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{"k": []byte("v")},
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

	t.Run("returns success for platform editor with no per-secret grants", func(t *testing.T) {
		// Given: User is in the "editor" OIDC group (platform editor role),
		// and provides grants for the new secret (required for the secret itself)
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "platformeditor@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:    "platform-editor-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{"key": []byte("value")},
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "platformeditor@example.com", Role: consolev1.Role_ROLE_EDITOR},
			},
		})

		// When: CreateSecret RPC is called
		resp, err := handler.CreateSecret(ctx, req)

		// Then: Returns success — platform editor role grants write permission
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Name != "platform-editor-secret" {
			t.Errorf("expected name 'platform-editor-secret', got %q", resp.Msg.Name)
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:    "existing-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{"k": []byte("v")},
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "user@example.com", Role: consolev1.Role_ROLE_EDITOR},
			},
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
		k8sClient := NewK8sClient(fakeClient)
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
			Name:    "new-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{"k": []byte("v")},
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "user@example.com", Role: consolev1.Role_ROLE_EDITOR},
			},
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
		assertResourceType(t, record)
	})

	t.Run("logs secret_create_denied on RBAC failure", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient)
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

		// Grants give viewer role — insufficient for write
		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:    "new-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{"k": []byte("v")},
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "other@example.com", Role: consolev1.Role_ROLE_VIEWER},
			},
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
		assertResourceType(t, record)
	})
}

func TestHandler_UpdateSecret(t *testing.T) {
	t.Run("returns success for authorized editor", func(t *testing.T) {
		// Given: Managed secret with editor share-users grant, user is editor
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"editor"}]`,
				},
			},
			Data: map[string][]byte{
				"old-key": []byte("old-value"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name:    "my-secret",
			Project: "test-namespace",
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name:    "my-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{"k": []byte("v")},
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
		// Given: Secret shared with user as viewer, user lacks editor permission
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"viewer"}]`,
				},
			},
			Data: map[string][]byte{"k": []byte("v")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name:    "my-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{"k": []byte("v")},
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name:    "missing",
			Project: "test-namespace",
			Data:    map[string][]byte{"k": []byte("v")},
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name:    "",
			Project: "test-namespace",
			Data:    map[string][]byte{"k": []byte("v")},
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name:    "my-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{},
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
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"editor"}]`,
				},
			},
			Data: map[string][]byte{"k": []byte("v")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
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
			Name:    "my-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{"new-key": []byte("new-value")},
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
		assertResourceType(t, record)
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
					ShareUsersAnnotation: `[{"principal":"alice@example.com","role":"owner"}]`,
				},
			},
			Data: map[string][]byte{"k": []byte("v")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
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
			Name:    "my-secret",
			Project: "test-namespace",
			Data:    map[string][]byte{"k": []byte("v")},
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
		assertResourceType(t, record)
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
					ShareGroupsAnnotation: `[{"principal":"owner","role":"owner"}]`,
				},
			},
			Data: map[string][]byte{
				"username": []byte("test-user"),
				"password": []byte("test-password"),
				"api-key":  []byte("test-api-key-12345"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "admin",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRequest{
			Name:    "multi-key-secret",
			Project: "test-namespace",
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
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"owner"}]`,
				},
			},
		}
		secretWithoutLabel := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unlabeled-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"owner"}]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secretWithLabel, secretWithoutLabel)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.ListSecretsRequest{Project: "test-namespace"})

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
	})

	t.Run("returns all secrets with accessibility info", func(t *testing.T) {
		// Given: Two labeled secrets, user can only access one (no platform role)
		accessibleSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "accessible-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"viewer"}]`,
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
					ShareUsersAnnotation: `[{"principal":"other@example.com","role":"owner"}]`,
				},
			},
		}
		fakeClient := fake.NewClientset(accessibleSecret, inaccessibleSecret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"some-team"}, // Not a platform role group
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.ListSecretsRequest{Project: "test-namespace"})

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

		if inaccessible == nil {
			t.Fatal("expected to find 'inaccessible-secret'")
		}
		if inaccessible.Accessible {
			t.Error("expected inaccessible-secret to not be accessible")
		}
	})

	t.Run("returns Unauthenticated for missing auth", func(t *testing.T) {
		// Given: Request without claims in context
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.ListSecretsRequest{Project: "test-namespace"})

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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"admin"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.ListSecretsRequest{Project: "test-namespace"})

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
					ShareUsersAnnotation: `[{"principal":"alice@example.com","role":"owner"}]`,
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "alice@example.com",
			Groups: []string{},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name:    "my-secret",
			Project: "test-namespace",
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
		updated, err := k8sClient.GetSecret(ctx, "test-namespace", "my-secret")
		if err != nil {
			t.Fatalf("failed to get updated secret: %v", err)
		}
		shareUsers, err := GetShareUsers(updated)
		if err != nil {
			t.Fatalf("failed to parse share-users: %v", err)
		}
		userMap := make(map[string]string)
		for _, g := range shareUsers {
			userMap[g.Principal] = g.Role
		}
		if userMap["alice@example.com"] != "owner" {
			t.Errorf("expected alice=owner, got %q", userMap["alice@example.com"])
		}
		if userMap["bob@example.com"] != "viewer" {
			t.Errorf("expected bob=viewer, got %q", userMap["bob@example.com"])
		}
		shareGroups, err := GetShareGroups(updated)
		if err != nil {
			t.Fatalf("failed to parse share-groups: %v", err)
		}
		groupMap := make(map[string]string)
		for _, g := range shareGroups {
			groupMap[g.Principal] = g.Role
		}
		if groupMap["dev-team"] != "editor" {
			t.Errorf("expected dev-team=editor, got %q", groupMap["dev-team"])
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
					ShareUsersAnnotation: `[{"principal":"bob@example.com","role":"viewer"}]`,
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-456",
			Email:  "bob@example.com",
			Groups: []string{},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name:    "my-secret",
			Project: "test-namespace",
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name:    "my-secret",
			Project: "test-namespace",
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "alice@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name:    "",
			Project: "test-namespace",
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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "alice@example.com",
			Groups: []string{"owner"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name:    "missing-secret",
			Project: "test-namespace",
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

func TestHandler_GetSecretRaw(t *testing.T) {
	t.Run("returns valid JSON with apiVersion and kind", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"viewer"}]`,
				},
			},
			Data: map[string][]byte{
				"username": []byte("admin"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRawRequest{Name: "my-secret", Project: "test-namespace"})

		resp, err := handler.GetSecretRaw(ctx, req)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp == nil || resp.Msg.Raw == "" {
			t.Fatal("expected non-empty raw JSON response")
		}

		// Parse and verify apiVersion and kind
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(resp.Msg.Raw), &parsed); err != nil {
			t.Fatalf("expected valid JSON, got parse error: %v", err)
		}
		if parsed["apiVersion"] != "v1" {
			t.Errorf("expected apiVersion 'v1', got %v", parsed["apiVersion"])
		}
		if parsed["kind"] != "Secret" {
			t.Errorf("expected kind 'Secret', got %v", parsed["kind"])
		}
	})

	t.Run("response includes server-managed fields", func(t *testing.T) {
		uid := "abc-123-def"
		rv := "12345"
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "my-secret",
				Namespace:         "test-namespace",
				UID:               types.UID(uid),
				ResourceVersion:   rv,
				CreationTimestamp: metav1.Now(),
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"viewer"}]`,
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRawRequest{Name: "my-secret", Project: "test-namespace"})

		resp, err := handler.GetSecretRaw(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(resp.Msg.Raw), &parsed); err != nil {
			t.Fatalf("expected valid JSON: %v", err)
		}
		metadata, ok := parsed["metadata"].(map[string]interface{})
		if !ok {
			t.Fatal("expected metadata object in JSON")
		}
		if metadata["uid"] != uid {
			t.Errorf("expected uid %q, got %v", uid, metadata["uid"])
		}
		if metadata["resourceVersion"] != rv {
			t.Errorf("expected resourceVersion %q, got %v", rv, metadata["resourceVersion"])
		}
		if metadata["creationTimestamp"] == nil {
			t.Error("expected creationTimestamp to be present")
		}
	})

	t.Run("returns PermissionDenied without Viewer grant", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"other@example.com","role":"owner"}]`,
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "unauthorized@example.com",
			Groups: []string{"developers"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRawRequest{Name: "my-secret", Project: "test-namespace"})

		_, err := handler.GetSecretRaw(ctx, req)

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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.GetSecretRawRequest{Name: "my-secret", Project: "test-namespace"})

		_, err := handler.GetSecretRaw(ctx, req)

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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRawRequest{Name: "", Project: "test-namespace"})

		_, err := handler.GetSecretRaw(ctx, req)

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
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"viewer"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.GetSecretRawRequest{Name: "missing", Project: "test-namespace"})

		_, err := handler.GetSecretRaw(ctx, req)

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

func TestHandler_CreateSecret_StringData(t *testing.T) {
	t.Run("string_data values are base64-encoded into data", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:       "sd-secret",
			Project:    "test-namespace",
			StringData: map[string]string{"username": "admin", "password": "secret123"},
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "user@example.com", Role: consolev1.Role_ROLE_EDITOR},
			},
		})

		resp, err := handler.CreateSecret(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Name != "sd-secret" {
			t.Errorf("expected name 'sd-secret', got %q", resp.Msg.Name)
		}

		// Verify the stored secret has the data encoded as bytes
		stored, err := k8sClient.GetSecret(ctx, "test-namespace", "sd-secret")
		if err != nil {
			t.Fatalf("failed to get stored secret: %v", err)
		}
		if string(stored.Data["username"]) != "admin" {
			t.Errorf("expected username 'admin', got %q", string(stored.Data["username"]))
		}
		if string(stored.Data["password"]) != "secret123" {
			t.Errorf("expected password 'secret123', got %q", string(stored.Data["password"]))
		}
	})

	t.Run("string_data takes precedence over data for same key", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:       "precedence-secret",
			Project:    "test-namespace",
			Data:       map[string][]byte{"key": []byte("from-data")},
			StringData: map[string]string{"key": "from-string-data"},
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "user@example.com", Role: consolev1.Role_ROLE_EDITOR},
			},
		})

		_, err := handler.CreateSecret(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		stored, err := k8sClient.GetSecret(ctx, "test-namespace", "precedence-secret")
		if err != nil {
			t.Fatalf("failed to get stored secret: %v", err)
		}
		if string(stored.Data["key"]) != "from-string-data" {
			t.Errorf("expected string_data to take precedence, got %q", string(stored.Data["key"]))
		}
	})
}

func TestHandler_UpdateSecret_StringData(t *testing.T) {
	t.Run("string_data values are merged into data on update", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"editor"}]`,
				},
			},
			Data: map[string][]byte{"old-key": []byte("old-value")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name:       "my-secret",
			Project:    "test-namespace",
			Data:       map[string][]byte{"existing": []byte("value")},
			StringData: map[string]string{"new-key": "new-value"},
		})

		_, err := handler.UpdateSecret(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		stored, err := k8sClient.GetSecret(ctx, "test-namespace", "my-secret")
		if err != nil {
			t.Fatalf("failed to get stored secret: %v", err)
		}
		if string(stored.Data["existing"]) != "value" {
			t.Errorf("expected existing='value', got %q", string(stored.Data["existing"]))
		}
		if string(stored.Data["new-key"]) != "new-value" {
			t.Errorf("expected new-key='new-value', got %q", string(stored.Data["new-key"]))
		}
	})

	t.Run("string_data takes precedence over data on update", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"editor"}]`,
				},
			},
			Data: map[string][]byte{"k": []byte("v")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{
			Sub:    "user-123",
			Email:  "user@example.com",
			Groups: []string{"editor"},
		}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name:       "my-secret",
			Project:    "test-namespace",
			Data:       map[string][]byte{"key": []byte("from-data")},
			StringData: map[string]string{"key": "from-string-data"},
		})

		_, err := handler.UpdateSecret(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		stored, err := k8sClient.GetSecret(ctx, "test-namespace", "my-secret")
		if err != nil {
			t.Fatalf("failed to get stored secret: %v", err)
		}
		if string(stored.Data["key"]) != "from-string-data" {
			t.Errorf("expected string_data to take precedence, got %q", string(stored.Data["key"]))
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
					ShareUsersAnnotation: `[{"principal":"alice@example.com","role":"owner"}]`,
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
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
			Name:    "my-secret",
			Project: "test-namespace",
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
		assertResourceType(t, record)
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
					ShareUsersAnnotation: `[{"principal":"bob@example.com","role":"viewer"}]`,
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
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
			Name:    "my-secret",
			Project: "test-namespace",
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
		assertResourceType(t, record)
	})
}

func TestHandler_ListSecrets_AuditLogging(t *testing.T) {
	t.Run("logs secrets_list with resource_type", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"owner"}]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
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

		req := connect.NewRequest(&consolev1.ListSecretsRequest{Project: "test-namespace"})

		_, err := handler.ListSecrets(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		record := logHandler.findRecord("secrets_list")
		if record == nil {
			t.Fatal("expected log record with action='secrets_list', got none")
		}
		if record.Level != slog.LevelInfo {
			t.Errorf("expected Info level, got %v", record.Level)
		}
		assertResourceType(t, record)
	})
}

func TestHandler_DescriptionAndURL(t *testing.T) {
	t.Run("ListSecrets returns description from annotation", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels:    map[string]string{ManagedByLabel: ManagedByValue},
				Annotations: map[string]string{
					ShareUsersAnnotation:  `[{"principal":"user@example.com","role":"owner"}]`,
					DescriptionAnnotation: "Database credentials",
					URLAnnotation:         "https://db.example.com",
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{Sub: "u1", Email: "user@example.com", Groups: []string{}}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		resp, err := handler.ListSecrets(ctx, connect.NewRequest(&consolev1.ListSecretsRequest{Project: "test-namespace"}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Msg.Secrets) != 1 {
			t.Fatalf("expected 1 secret, got %d", len(resp.Msg.Secrets))
		}
		md := resp.Msg.Secrets[0]
		if md.Description == nil || *md.Description != "Database credentials" {
			t.Errorf("expected description 'Database credentials', got %v", md.Description)
		}
		if md.Url == nil || *md.Url != "https://db.example.com" {
			t.Errorf("expected url 'https://db.example.com', got %v", md.Url)
		}
	})

	t.Run("ListSecrets omits description and url when not set", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels:    map[string]string{ManagedByLabel: ManagedByValue},
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"user@example.com","role":"owner"}]`,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{Sub: "u1", Email: "user@example.com", Groups: []string{}}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		resp, err := handler.ListSecrets(ctx, connect.NewRequest(&consolev1.ListSecretsRequest{Project: "test-namespace"}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		md := resp.Msg.Secrets[0]
		if md.Description != nil {
			t.Errorf("expected nil description, got %v", md.Description)
		}
		if md.Url != nil {
			t.Errorf("expected nil url, got %v", md.Url)
		}
	})

	t.Run("CreateSecret stores description and url", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{Sub: "u1", Email: "user@example.com", Groups: []string{}}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		desc := "API key for staging"
		url := "https://staging.example.com"
		req := connect.NewRequest(&consolev1.CreateSecretRequest{
			Name:       "new-secret",
			Project:    "test-namespace",
			StringData: map[string]string{"key": "value"},
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "user@example.com", Role: consolev1.Role_ROLE_OWNER},
			},
			Description: &desc,
			Url:         &url,
		})

		_, err := handler.CreateSecret(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify annotations persisted by reading back via K8s
		secret, err := k8sClient.GetSecret(context.Background(), "test-namespace", "new-secret")
		if err != nil {
			t.Fatalf("failed to get created secret: %v", err)
		}
		if GetDescription(secret) != "API key for staging" {
			t.Errorf("expected description 'API key for staging', got %q", GetDescription(secret))
		}
		if GetURL(secret) != "https://staging.example.com" {
			t.Errorf("expected URL 'https://staging.example.com', got %q", GetURL(secret))
		}
	})

	t.Run("UpdateSecret updates description and url", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels:    map[string]string{ManagedByLabel: ManagedByValue},
				Annotations: map[string]string{
					ShareUsersAnnotation:  `[{"principal":"user@example.com","role":"owner"}]`,
					DescriptionAnnotation: "Old description",
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{Sub: "u1", Email: "user@example.com", Groups: []string{}}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		desc := "New description"
		url := "https://new.example.com"
		req := connect.NewRequest(&consolev1.UpdateSecretRequest{
			Name:        "my-secret",
			Project:     "test-namespace",
			StringData:  map[string]string{"key": "value"},
			Description: &desc,
			Url:         &url,
		})

		_, err := handler.UpdateSecret(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		updated, err := k8sClient.GetSecret(context.Background(), "test-namespace", "my-secret")
		if err != nil {
			t.Fatalf("failed to get updated secret: %v", err)
		}
		if GetDescription(updated) != "New description" {
			t.Errorf("expected 'New description', got %q", GetDescription(updated))
		}
		if GetURL(updated) != "https://new.example.com" {
			t.Errorf("expected 'https://new.example.com', got %q", GetURL(updated))
		}
	})

	t.Run("UpdateSharing returns description and url in metadata", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels:    map[string]string{ManagedByLabel: ManagedByValue},
				Annotations: map[string]string{
					ShareUsersAnnotation:  `[{"principal":"user@example.com","role":"owner"}]`,
					DescriptionAnnotation: "Important secret",
					URLAnnotation:         "https://important.example.com",
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient)
		handler := NewHandler(k8sClient, rbac.NewGroupMapping(nil, nil, nil))

		claims := &rpc.Claims{Sub: "u1", Email: "user@example.com", Groups: []string{}}
		ctx := rpc.ContextWithClaims(context.Background(), claims)

		req := connect.NewRequest(&consolev1.UpdateSharingRequest{
			Name:    "my-secret",
			Project: "test-namespace",
			UserGrants: []*consolev1.ShareGrant{
				{Principal: "user@example.com", Role: consolev1.Role_ROLE_OWNER},
			},
		})

		resp, err := handler.UpdateSharing(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		md := resp.Msg.Metadata
		if md.Description == nil || *md.Description != "Important secret" {
			t.Errorf("expected description 'Important secret', got %v", md.Description)
		}
		if md.Url == nil || *md.Url != "https://important.example.com" {
			t.Errorf("expected url 'https://important.example.com', got %v", md.Url)
		}
	})
}
