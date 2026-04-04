package deployments

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
		Data: map[string]string{
			"template.cue": `
input: { name: string, image: string, tag: string, project: string, namespace: string }
resources: []
`,
		},
	}
}

// stubRenderer implements Renderer for tests.
type stubRenderer struct {
	resources []unstructured.Unstructured
	err       error
	called    bool
	lastInput DeploymentInput
}

func (s *stubRenderer) Render(_ context.Context, _ string, input DeploymentInput) ([]unstructured.Unstructured, error) {
	s.called = true
	s.lastInput = input
	return s.resources, s.err
}

// stubApplier implements Applier for tests.
type stubApplier struct {
	applyCalled   bool
	cleanupCalled bool
	applyErr      error
	cleanupErr    error
}

func (s *stubApplier) Apply(_ context.Context, _, _ string, _ []unstructured.Unstructured) error {
	s.applyCalled = true
	return s.applyErr
}

func (s *stubApplier) Cleanup(_ context.Context, _, _ string) error {
	s.cleanupCalled = true
	return s.cleanupErr
}

func defaultHandler(fakeClient *fake.Clientset, pr *stubProjectResolver) *Handler {
	k8s := NewK8sClient(fakeClient, testResolver())
	return NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, &stubRenderer{}, &stubApplier{})
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
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: disabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, &stubRenderer{}, &stubApplier{})

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
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{err: templateErr}, &stubRenderer{}, &stubApplier{})

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

// TestHandler_EnvVarValidation tests env var name validation.
func TestHandler_EnvVarValidation(t *testing.T) {
	t.Run("rejects empty env var name", func(t *testing.T) {
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
			Env: []*consolev1.EnvVar{
				{Name: "", Source: &consolev1.EnvVar_Value{Value: "bar"}},
			},
		})
		_, err := handler.CreateDeployment(ctx, req)
		if err == nil {
			t.Fatal("expected error for empty env var name")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("accepts valid env var name", func(t *testing.T) {
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
			Env: []*consolev1.EnvVar{
				{Name: "MY_VAR", Source: &consolev1.EnvVar_Value{Value: "hello"}},
			},
		})
		_, err := handler.CreateDeployment(ctx, req)
		if err != nil {
			t.Fatalf("expected no error for valid env var name, got %v", err)
		}
	})
}

// TestHandler_EnvVarRoundTrip tests that env vars pass through create/update to the renderer.
func TestHandler_EnvVarRoundTrip(t *testing.T) {
	t.Run("CreateDeployment passes env vars to renderer", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		renderer := &stubRenderer{}
		applier := &stubApplier{}
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, applier)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project:  "my-project",
			Name:     "web-app",
			Image:    "nginx",
			Tag:      "1.25",
			Template: "default",
			Env: []*consolev1.EnvVar{
				{Name: "FOO", Source: &consolev1.EnvVar_Value{Value: "bar"}},
				{Name: "FROM_SECRET", Source: &consolev1.EnvVar_SecretKeyRef{SecretKeyRef: &consolev1.SecretKeyRef{Name: "mysecret", Key: "mykey"}}},
			},
		})
		_, err := handler.CreateDeployment(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(renderer.lastInput.Env) != 2 {
			t.Fatalf("expected 2 env vars in renderer input, got %d", len(renderer.lastInput.Env))
		}
		if renderer.lastInput.Env[0].Name != "FOO" || renderer.lastInput.Env[0].Value != "bar" {
			t.Errorf("unexpected first env var: %+v", renderer.lastInput.Env[0])
		}
		if renderer.lastInput.Env[1].Name != "FROM_SECRET" || renderer.lastInput.Env[1].SecretKeyRef == nil {
			t.Errorf("unexpected second env var: %+v", renderer.lastInput.Env[1])
		}
	})

	t.Run("UpdateDeployment passes stored env vars to renderer", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		cm.Data[EnvKey] = `[{"name":"PORT","value":"8080"}]`
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		renderer := &stubRenderer{}
		applier := &stubApplier{}
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, applier)

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
		if len(renderer.lastInput.Env) != 1 || renderer.lastInput.Env[0].Name != "PORT" {
			t.Errorf("expected env [PORT=8080] from stored data, got %v", renderer.lastInput.Env)
		}
	})
}

// TestHandler_RenderAndApply tests that CreateDeployment and UpdateDeployment
// trigger render+apply and DeleteDeployment triggers cleanup.
func TestHandler_RenderAndApply(t *testing.T) {
	t.Run("CreateDeployment calls renderer and applier", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		renderer := &stubRenderer{}
		applier := &stubApplier{}
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, applier)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project:  "my-project",
			Name:     "web-app",
			Image:    "nginx",
			Tag:      "1.25",
			Template: "default",
		})
		_, err := handler.CreateDeployment(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !renderer.called {
			t.Error("expected renderer to be called on CreateDeployment")
		}
		if !applier.applyCalled {
			t.Error("expected applier.Apply to be called on CreateDeployment")
		}
		if renderer.lastInput.Name != "web-app" {
			t.Errorf("expected input name 'web-app', got %q", renderer.lastInput.Name)
		}
		if renderer.lastInput.Image != "nginx" {
			t.Errorf("expected input image 'nginx', got %q", renderer.lastInput.Image)
		}
		if renderer.lastInput.Tag != "1.25" {
			t.Errorf("expected input tag '1.25', got %q", renderer.lastInput.Tag)
		}
	})

	t.Run("UpdateDeployment calls renderer and applier", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		renderer := &stubRenderer{}
		applier := &stubApplier{}
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, applier)

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
		if !renderer.called {
			t.Error("expected renderer to be called on UpdateDeployment")
		}
		if !applier.applyCalled {
			t.Error("expected applier.Apply to be called on UpdateDeployment")
		}
	})

	t.Run("CreateDeployment passes command and args to renderer", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		renderer := &stubRenderer{}
		applier := &stubApplier{}
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, applier)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project:  "my-project",
			Name:     "web-app",
			Image:    "nginx",
			Tag:      "1.25",
			Template: "default",
			Command:  []string{"myapp"},
			Args:     []string{"--port", "8080"},
		})
		_, err := handler.CreateDeployment(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(renderer.lastInput.Command) != 1 || renderer.lastInput.Command[0] != "myapp" {
			t.Errorf("expected command [myapp], got %v", renderer.lastInput.Command)
		}
		if len(renderer.lastInput.Args) != 2 || renderer.lastInput.Args[0] != "--port" || renderer.lastInput.Args[1] != "8080" {
			t.Errorf("expected args [--port 8080], got %v", renderer.lastInput.Args)
		}
	})

	t.Run("UpdateDeployment passes stored command and args to renderer", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		cm.Data[CommandKey] = `["myapp"]`
		cm.Data[ArgsKey] = `["--port","8080"]`
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		renderer := &stubRenderer{}
		applier := &stubApplier{}
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, applier)

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
		if len(renderer.lastInput.Command) != 1 || renderer.lastInput.Command[0] != "myapp" {
			t.Errorf("expected command [myapp] from stored data, got %v", renderer.lastInput.Command)
		}
		if len(renderer.lastInput.Args) != 2 || renderer.lastInput.Args[0] != "--port" {
			t.Errorf("expected args [--port 8080] from stored data, got %v", renderer.lastInput.Args)
		}
	})

	t.Run("DeleteDeployment calls applier cleanup", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "", "")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "owner"}}
		renderer := &stubRenderer{}
		applier := &stubApplier{}
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, applier)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.DeleteDeploymentRequest{
			Project: "my-project",
			Name:    "web-app",
		})
		_, err := handler.DeleteDeployment(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !applier.cleanupCalled {
			t.Error("expected applier.Cleanup to be called on DeleteDeployment")
		}
	})
}
