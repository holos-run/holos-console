package settings

import (
	"context"
	"encoding/json"
	"testing"

	"connectrpc.com/connect"
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

// stubOrgResolver implements OrgResolver for tests.
type stubOrgResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubOrgResolver) GetOrgGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	return s.users, s.roles, s.err
}

// stubProjectOrgResolver implements ProjectOrgResolver for tests.
type stubProjectOrgResolver struct {
	org string
	err error
}

func (s *stubProjectOrgResolver) GetProjectOrganization(_ context.Context, _ string) (string, error) {
	return s.org, s.err
}

func authedCtx(email string, roles []string) context.Context {
	return rpc.ContextWithClaims(context.Background(), &rpc.Claims{
		Sub:   "user-123",
		Email: email,
		Roles: roles,
	})
}

func TestHandler_GetProjectSettings(t *testing.T) {
	t.Run("returns defaults for authorized user when no annotation exists", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := NewHandler(k8s, pr, nil, nil)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetProjectSettingsRequest{Project: "my-project"})
		resp, err := handler.GetProjectSettings(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Settings.DeploymentsEnabled {
			t.Error("expected deployments_enabled=false by default")
		}
		if resp.Msg.Settings.Project != "my-project" {
			t.Errorf("expected project 'my-project', got %q", resp.Msg.Settings.Project)
		}
	})

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, &stubProjectResolver{}, nil, nil)

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
		handler := NewHandler(k8s, pr, nil, nil)

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
		handler := NewHandler(k8s, &stubProjectResolver{}, nil, nil)

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

	t.Run("reads existing annotation settings", func(t *testing.T) {
		ns := projectNSWithAnnotation("my-project", true)
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := NewHandler(k8s, pr, nil, nil)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetProjectSettingsRequest{Project: "my-project"})
		resp, err := handler.GetProjectSettings(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !resp.Msg.Settings.DeploymentsEnabled {
			t.Error("expected deployments_enabled=true from existing annotation")
		}
	})
}

func TestHandler_UpdateProjectSettings(t *testing.T) {
	t.Run("org-level owner can update settings", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"owner@example.com": "owner"}}
		orgR := &stubOrgResolver{users: map[string]string{"owner@example.com": "owner"}}
		projOrgR := &stubProjectOrgResolver{org: "my-org"}
		handler := NewHandler(k8s, pr, orgR, projOrgR)

		ctx := authedCtx("owner@example.com", nil)
		req := connect.NewRequest(&consolev1.UpdateProjectSettingsRequest{
			Project: "my-project",
			Settings: &consolev1.ProjectSettings{
				Project:            "my-project",
				DeploymentsEnabled: true,
			},
		})
		resp, err := handler.UpdateProjectSettings(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !resp.Msg.Settings.DeploymentsEnabled {
			t.Error("expected deployments_enabled=true after update")
		}
	})

	t.Run("project-level owner cannot update settings (not org owner)", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"projowner@example.com": "owner"}}
		orgR := &stubOrgResolver{users: map[string]string{"projowner@example.com": "editor"}} // only editor at org level
		projOrgR := &stubProjectOrgResolver{org: "my-org"}
		handler := NewHandler(k8s, pr, orgR, projOrgR)

		ctx := authedCtx("projowner@example.com", nil)
		req := connect.NewRequest(&consolev1.UpdateProjectSettingsRequest{
			Project: "my-project",
			Settings: &consolev1.ProjectSettings{
				Project:            "my-project",
				DeploymentsEnabled: true,
			},
		})
		_, err := handler.UpdateProjectSettings(ctx, req)
		if err == nil {
			t.Fatal("expected error for project-level owner (not org owner)")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})

	t.Run("editor cannot update settings", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"editor@example.com": "editor"}}
		orgR := &stubOrgResolver{users: map[string]string{"editor@example.com": "editor"}}
		projOrgR := &stubProjectOrgResolver{org: "my-org"}
		handler := NewHandler(k8s, pr, orgR, projOrgR)

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
		orgR := &stubOrgResolver{users: map[string]string{"viewer@example.com": "viewer"}}
		projOrgR := &stubProjectOrgResolver{org: "my-org"}
		handler := NewHandler(k8s, pr, orgR, projOrgR)

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
		handler := NewHandler(k8s, &stubProjectResolver{}, nil, nil)

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
		orgR := &stubOrgResolver{users: map[string]string{"owner@example.com": "owner"}}
		projOrgR := &stubProjectOrgResolver{org: "my-org"}
		handler := NewHandler(k8s, pr, orgR, projOrgR)

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
}

func TestHandler_GetProjectSettingsRaw(t *testing.T) {
	t.Run("returns raw namespace JSON for authorized user", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := NewHandler(k8s, pr, nil, nil)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetProjectSettingsRawRequest{Project: "my-project"})
		resp, err := handler.GetProjectSettingsRaw(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(resp.Msg.Raw), &obj); err != nil {
			t.Fatalf("expected valid JSON, got %v", err)
		}
		if obj["apiVersion"] != "v1" {
			t.Errorf("expected apiVersion=v1, got %v", obj["apiVersion"])
		}
		if obj["kind"] != "Namespace" {
			t.Errorf("expected kind=Namespace, got %v", obj["kind"])
		}
	})

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, &stubProjectResolver{}, nil, nil)

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.GetProjectSettingsRawRequest{Project: "my-project"})
		_, err := handler.GetProjectSettingsRaw(ctx, req)
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
		handler := NewHandler(k8s, pr, nil, nil)

		ctx := authedCtx("nobody@example.com", nil)
		req := connect.NewRequest(&consolev1.GetProjectSettingsRawRequest{Project: "my-project"})
		_, err := handler.GetProjectSettingsRaw(ctx, req)
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
		handler := NewHandler(k8s, &stubProjectResolver{}, nil, nil)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetProjectSettingsRawRequest{Project: ""})
		_, err := handler.GetProjectSettingsRaw(ctx, req)
		if err == nil {
			t.Fatal("expected error for empty project")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})
}
