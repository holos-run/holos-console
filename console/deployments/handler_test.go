package deployments

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

// stubSettingsResolver implements SettingsResolver for tests.
type stubSettingsResolver struct {
	settings *consolev1.ProjectSettings
	err      error
}

func (s *stubSettingsResolver) GetSettings(_ context.Context, _ string) (*consolev1.ProjectSettings, error) {
	return s.settings, s.err
}

// stubTemplateResolver implements TemplateResolver for tests.
type stubTemplateResolver struct {
	cm  *corev1.ConfigMap
	err error
}

func (s *stubTemplateResolver) GetTemplate(_ context.Context, _, _ string) (*corev1.ConfigMap, error) {
	return s.cm, s.err
}

func authedCtx(email string, roles []string) context.Context {
	return rpc.ContextWithClaims(context.Background(), &rpc.Claims{
		Sub:   "user-123",
		Email: email,
		Roles: roles,
	})
}

func enabledSettings() *consolev1.ProjectSettings {
	return &consolev1.ProjectSettings{
		Project:            "my-project",
		DeploymentsEnabled: true,
	}
}

func disabledSettings() *consolev1.ProjectSettings {
	return &consolev1.ProjectSettings{
		Project:            "my-project",
		DeploymentsEnabled: false,
	}
}

func fakeTemplate(name string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "prj-my-project",
		},
	}
}

func defaultHandler(fakeClient *fake.Clientset, pr *stubProjectResolver) *Handler {
	k8s := NewK8sClient(fakeClient, testResolver())
	return NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, nil)
}

// TestHandler_ListDeployments tests the ListDeployments RPC.
func TestHandler_ListDeployments(t *testing.T) {
	t.Run("viewer can list deployments", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListDeploymentsRequest{Project: "my-project"})
		resp, err := handler.ListDeployments(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.Deployments) != 1 {
			t.Fatalf("expected 1 deployment, got %d", len(resp.Msg.Deployments))
		}
		if resp.Msg.Deployments[0].Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", resp.Msg.Deployments[0].Name)
		}
	})

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		handler := defaultHandler(fakeClient, &stubProjectResolver{})

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.ListDeploymentsRequest{Project: "my-project"})
		_, err := handler.ListDeployments(ctx, req)
		if err == nil {
			t.Fatal("expected error for unauthenticated request")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects unauthorized user", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		handler := defaultHandler(fakeClient, &stubProjectResolver{})

		ctx := authedCtx("nobody@example.com", nil)
		req := connect.NewRequest(&consolev1.ListDeploymentsRequest{Project: "my-project"})
		_, err := handler.ListDeployments(ctx, req)
		if err == nil {
			t.Fatal("expected error for unauthorized user")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects empty project", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		handler := defaultHandler(fakeClient, &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}})

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListDeploymentsRequest{Project: ""})
		_, err := handler.ListDeployments(ctx, req)
		if err == nil {
			t.Fatal("expected error for empty project")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})
}

// TestHandler_GetDeployment tests the GetDeployment RPC.
func TestHandler_GetDeployment(t *testing.T) {
	t.Run("viewer can get deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentRequest{Project: "my-project", Name: "web-app"})
		resp, err := handler.GetDeployment(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Deployment.Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", resp.Msg.Deployment.Name)
		}
		if resp.Msg.Deployment.Image != "nginx" {
			t.Errorf("expected image 'nginx', got %q", resp.Msg.Deployment.Image)
		}
	})

	t.Run("returns NotFound for missing deployment", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentRequest{Project: "my-project", Name: "does-not-exist"})
		_, err := handler.GetDeployment(ctx, req)
		if err == nil {
			t.Fatal("expected error for missing deployment")
		}
		if connect.CodeOf(err) != connect.CodeNotFound {
			t.Errorf("expected CodeNotFound, got %v", connect.CodeOf(err))
		}
	})
}

// TestHandler_CreateDeployment tests the CreateDeployment RPC.
func TestHandler_CreateDeployment(t *testing.T) {
	t.Run("editor can create deployment", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project:  "my-project",
			Name:     "web-app",
			Image:    "nginx",
			Tag:      "1.25",
			Template: "default",
		})
		resp, err := handler.CreateDeployment(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", resp.Msg.Name)
		}
	})

	t.Run("viewer cannot create deployment", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project:  "my-project",
			Name:     "web-app",
			Image:    "nginx",
			Tag:      "1.25",
			Template: "default",
		})
		_, err := handler.CreateDeployment(ctx, req)
		if err == nil {
			t.Fatal("expected error for viewer creating deployment")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})

	t.Run("returns FailedPrecondition when deployments disabled", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: disabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, nil)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project:  "my-project",
			Name:     "web-app",
			Image:    "nginx",
			Tag:      "1.25",
			Template: "default",
		})
		_, err := handler.CreateDeployment(ctx, req)
		if err == nil {
			t.Fatal("expected error when deployments disabled")
		}
		if connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Errorf("expected CodeFailedPrecondition, got %v", connect.CodeOf(err))
		}
	})

	t.Run("returns NotFound when template does not exist", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		k8s := NewK8sClient(fakeClient, testResolver())
		templateErr := connect.NewError(connect.CodeNotFound, nil)
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{err: templateErr}, nil)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project:  "my-project",
			Name:     "web-app",
			Image:    "nginx",
			Tag:      "1.25",
			Template: "no-such-template",
		})
		_, err := handler.CreateDeployment(ctx, req)
		if err == nil {
			t.Fatal("expected error when template does not exist")
		}
		if connect.CodeOf(err) != connect.CodeNotFound {
			t.Errorf("expected CodeNotFound, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects invalid name", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project:  "my-project",
			Name:     "Invalid_Name!",
			Image:    "nginx",
			Tag:      "1.25",
			Template: "default",
		})
		_, err := handler.CreateDeployment(ctx, req)
		if err == nil {
			t.Fatal("expected error for invalid name")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects empty image", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project:  "my-project",
			Name:     "web-app",
			Image:    "",
			Tag:      "1.25",
			Template: "default",
		})
		_, err := handler.CreateDeployment(ctx, req)
		if err == nil {
			t.Fatal("expected error for empty image")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects empty tag", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project:  "my-project",
			Name:     "web-app",
			Image:    "nginx",
			Tag:      "",
			Template: "default",
		})
		_, err := handler.CreateDeployment(ctx, req)
		if err == nil {
			t.Fatal("expected error for empty tag")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})
}

// TestHandler_UpdateDeployment tests the UpdateDeployment RPC.
func TestHandler_UpdateDeployment(t *testing.T) {
	t.Run("editor can update deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		newTag := "1.26"
		req := connect.NewRequest(&consolev1.UpdateDeploymentRequest{
			Project: "my-project",
			Name:    "web-app",
			Tag:     &newTag,
		})
		_, err := handler.UpdateDeployment(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("viewer cannot update deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		newTag := "1.26"
		req := connect.NewRequest(&consolev1.UpdateDeploymentRequest{
			Project: "my-project",
			Name:    "web-app",
			Tag:     &newTag,
		})
		_, err := handler.UpdateDeployment(ctx, req)
		if err == nil {
			t.Fatal("expected error for viewer updating deployment")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})
}

// TestHandler_DeleteDeployment tests the DeleteDeployment RPC.
func TestHandler_DeleteDeployment(t *testing.T) {
	t.Run("owner can delete deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "", "")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "owner"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.DeleteDeploymentRequest{
			Project: "my-project",
			Name:    "web-app",
		})
		_, err := handler.DeleteDeployment(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("editor cannot delete deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "", "")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.DeleteDeploymentRequest{
			Project: "my-project",
			Name:    "web-app",
		})
		_, err := handler.DeleteDeployment(ctx, req)
		if err == nil {
			t.Fatal("expected error for editor deleting deployment")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})
}
