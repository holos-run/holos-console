package settings

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

// stubProjectResolver implements ProjectResolver for tests.
type stubProjectResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubProjectResolver) GetProjectGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	return s.users, s.roles, s.err
}

func authedCtx(email string, roles []string) context.Context {
	return rpc.ContextWithClaims(context.Background(), &rpc.Claims{
		Sub:   "user-123",
		Email: email,
		Roles: roles,
	})
}

func TestHandler_GetProjectSettings(t *testing.T) {
	t.Run("returns defaults for authorized user when no ConfigMap exists", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetProjectSettingsRequest{Project: "my-project"})
		resp, err := handler.GetProjectSettings(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !resp.Msg.Settings.DeploymentsEnabled {
			t.Error("expected deployments_enabled=true by default")
		}
		if resp.Msg.Settings.Project != "my-project" {
			t.Errorf("expected project 'my-project', got %q", resp.Msg.Settings.Project)
		}
	})

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, &stubProjectResolver{})

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.GetProjectSettingsRequest{Project: "my-project"})
		_, err := handler.GetProjectSettings(ctx, req)
		if err == nil {
			t.Fatal("expected error for unauthenticated request")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects unauthorized user", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{} // no grants
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("nobody@example.com", nil)
		req := connect.NewRequest(&consolev1.GetProjectSettingsRequest{Project: "my-project"})
		_, err := handler.GetProjectSettings(ctx, req)
		if err == nil {
			t.Fatal("expected error for unauthorized user")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects empty project", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, &stubProjectResolver{})

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetProjectSettingsRequest{Project: ""})
		_, err := handler.GetProjectSettings(ctx, req)
		if err == nil {
			t.Fatal("expected error for empty project")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})
}

func TestHandler_UpdateProjectSettings(t *testing.T) {
	t.Run("owner can update settings", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"owner@example.com": "owner"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("owner@example.com", nil)
		req := connect.NewRequest(&consolev1.UpdateProjectSettingsRequest{
			Project: "my-project",
			Settings: &consolev1.ProjectSettings{
				Project:            "my-project",
				DeploymentsEnabled: false,
			},
		})
		resp, err := handler.UpdateProjectSettings(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Settings.DeploymentsEnabled {
			t.Error("expected deployments_enabled=false after update")
		}
	})

	t.Run("editor cannot update settings", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"editor@example.com": "editor"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("editor@example.com", nil)
		req := connect.NewRequest(&consolev1.UpdateProjectSettingsRequest{
			Project: "my-project",
			Settings: &consolev1.ProjectSettings{
				Project:            "my-project",
				DeploymentsEnabled: false,
			},
		})
		_, err := handler.UpdateProjectSettings(ctx, req)
		if err == nil {
			t.Fatal("expected error for editor updating settings")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})

	t.Run("viewer cannot update settings", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"viewer@example.com": "viewer"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("viewer@example.com", nil)
		req := connect.NewRequest(&consolev1.UpdateProjectSettingsRequest{
			Project: "my-project",
			Settings: &consolev1.ProjectSettings{
				Project:            "my-project",
				DeploymentsEnabled: false,
			},
		})
		_, err := handler.UpdateProjectSettings(ctx, req)
		if err == nil {
			t.Fatal("expected error for viewer updating settings")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, &stubProjectResolver{})

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.UpdateProjectSettingsRequest{
			Project: "my-project",
			Settings: &consolev1.ProjectSettings{
				Project:            "my-project",
				DeploymentsEnabled: false,
			},
		})
		_, err := handler.UpdateProjectSettings(ctx, req)
		if err == nil {
			t.Fatal("expected error for unauthenticated request")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects nil settings", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"owner@example.com": "owner"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("owner@example.com", nil)
		req := connect.NewRequest(&consolev1.UpdateProjectSettingsRequest{
			Project:  "my-project",
			Settings: nil,
		})
		_, err := handler.UpdateProjectSettings(ctx, req)
		if err == nil {
			t.Fatal("expected error for nil settings")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("reads existing ConfigMap settings", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      SettingsConfigMapName,
				Namespace: "prj-my-project",
				Labels: map[string]string{
					ManagedByLabel:    ManagedByValue,
					ResourceTypeLabel: ResourceTypeValue,
				},
			},
			Data: map[string]string{
				SettingsDataKey: `{"project":"my-project","deployments_enabled":false}`,
			},
		}
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetProjectSettingsRequest{Project: "my-project"})
		resp, err := handler.GetProjectSettings(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Settings.DeploymentsEnabled {
			t.Error("expected deployments_enabled=false from existing ConfigMap")
		}
	})
}
