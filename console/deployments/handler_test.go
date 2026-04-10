package deployments

import (
	"context"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
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
			// Stub template content — not evaluated by the real renderer in handler tests
			// because tests use stubRenderer. Matches the structured output format for
			// consistency with the production template.
			"template.cue": `
input: { name: string, image: string, tag: string, project: string, namespace: string }
namespaced: {}
cluster: {}
`,
		},
	}
}

// stubRenderer implements Renderer for tests.
type stubRenderer struct {
	resources    []unstructured.Unstructured
	err          error
	called       bool
	lastPlatform v1alpha1.PlatformInput
	lastProject  v1alpha1.ProjectInput
}

func (s *stubRenderer) Render(_ context.Context, _ string, platform v1alpha1.PlatformInput, project v1alpha1.ProjectInput) ([]unstructured.Unstructured, error) {
	s.called = true
	s.lastPlatform = platform
	s.lastProject = project
	return s.resources, s.err
}

func (s *stubRenderer) RenderWithAncestorTemplates(_ context.Context, _ string, _ []string, platform v1alpha1.PlatformInput, project v1alpha1.ProjectInput) ([]unstructured.Unstructured, error) {
	s.called = true
	s.lastPlatform = platform
	s.lastProject = project
	return s.resources, s.err
}

// stubApplier implements ResourceApplier for tests.
type stubApplier struct {
	applyCalled     bool
	reconcileCalled bool
	cleanupCalled   bool
	applyErr        error
	reconcileErr    error
	cleanupErr      error
}

func (s *stubApplier) Apply(_ context.Context, _, _ string, _ []unstructured.Unstructured) error {
	s.applyCalled = true
	return s.applyErr
}

func (s *stubApplier) Reconcile(_ context.Context, _, _ string, _ []unstructured.Unstructured) error {
	s.reconcileCalled = true
	return s.reconcileErr
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

// TestHandler_ListNamespaceSecrets tests the ListNamespaceSecrets RPC.
func TestHandler_ListNamespaceSecrets(t *testing.T) {
	t.Run("editor can list namespace secrets", func(t *testing.T) {
		ns := projectNS("my-project")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "prj-my-project",
			},
			Data: map[string][]byte{
				"password": []byte("s3cr3t"),
			},
		}
		fakeClient := fake.NewClientset(ns, secret)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListNamespaceSecretsRequest{Project: "my-project"})
		resp, err := handler.ListNamespaceSecrets(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.Secrets) != 1 {
			t.Fatalf("expected 1 secret, got %d", len(resp.Msg.Secrets))
		}
		if resp.Msg.Secrets[0].Name != "my-secret" {
			t.Errorf("expected name 'my-secret', got %q", resp.Msg.Secrets[0].Name)
		}
		if len(resp.Msg.Secrets[0].Keys) != 1 || resp.Msg.Secrets[0].Keys[0] != "password" {
			t.Errorf("expected keys [password], got %v", resp.Msg.Secrets[0].Keys)
		}
	})

	t.Run("viewer cannot list namespace secrets", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListNamespaceSecretsRequest{Project: "my-project"})
		_, err := handler.ListNamespaceSecrets(ctx, req)
		if err == nil {
			t.Fatal("expected error for viewer listing namespace secrets")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		handler := defaultHandler(fakeClient, &stubProjectResolver{})

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.ListNamespaceSecretsRequest{Project: "my-project"})
		_, err := handler.ListNamespaceSecrets(ctx, req)
		if err == nil {
			t.Fatal("expected error for unauthenticated request")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects empty project", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		handler := defaultHandler(fakeClient, &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}})

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListNamespaceSecretsRequest{Project: ""})
		_, err := handler.ListNamespaceSecrets(ctx, req)
		if err == nil {
			t.Fatal("expected error for empty project")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})
}

// TestHandler_ListNamespaceConfigMaps tests the ListNamespaceConfigMaps RPC.
func TestHandler_ListNamespaceConfigMaps(t *testing.T) {
	t.Run("editor can list namespace configmaps", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-config",
				Namespace: "prj-my-project",
			},
			Data: map[string]string{
				"key": "value",
			},
		}
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListNamespaceConfigMapsRequest{Project: "my-project"})
		resp, err := handler.ListNamespaceConfigMaps(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.ConfigMaps) != 1 {
			t.Fatalf("expected 1 configmap, got %d", len(resp.Msg.ConfigMaps))
		}
		if resp.Msg.ConfigMaps[0].Name != "my-config" {
			t.Errorf("expected name 'my-config', got %q", resp.Msg.ConfigMaps[0].Name)
		}
		if len(resp.Msg.ConfigMaps[0].Keys) != 1 || resp.Msg.ConfigMaps[0].Keys[0] != "key" {
			t.Errorf("expected keys [key], got %v", resp.Msg.ConfigMaps[0].Keys)
		}
	})

	t.Run("viewer cannot list namespace configmaps", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListNamespaceConfigMapsRequest{Project: "my-project"})
		_, err := handler.ListNamespaceConfigMaps(ctx, req)
		if err == nil {
			t.Fatal("expected error for viewer listing namespace configmaps")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		handler := defaultHandler(fakeClient, &stubProjectResolver{})

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.ListNamespaceConfigMapsRequest{Project: "my-project"})
		_, err := handler.ListNamespaceConfigMaps(ctx, req)
		if err == nil {
			t.Fatal("expected error for unauthenticated request")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects empty project", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		handler := defaultHandler(fakeClient, &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}})

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListNamespaceConfigMapsRequest{Project: ""})
		_, err := handler.ListNamespaceConfigMaps(ctx, req)
		if err == nil {
			t.Fatal("expected error for empty project")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
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
		if len(renderer.lastProject.Env) != 2 {
			t.Fatalf("expected 2 env vars in renderer input, got %d", len(renderer.lastProject.Env))
		}
		if renderer.lastProject.Env[0].Name != "FOO" || renderer.lastProject.Env[0].Value != "bar" {
			t.Errorf("unexpected first env var: %+v", renderer.lastProject.Env[0])
		}
		if renderer.lastProject.Env[1].Name != "FROM_SECRET" || renderer.lastProject.Env[1].SecretKeyRef == nil {
			t.Errorf("unexpected second env var: %+v", renderer.lastProject.Env[1])
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
		if len(renderer.lastProject.Env) != 1 || renderer.lastProject.Env[0].Name != "PORT" {
			t.Errorf("expected env [PORT=8080] from stored data, got %v", renderer.lastProject.Env)
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
		if renderer.lastProject.Name != "web-app" {
			t.Errorf("expected input name 'web-app', got %q", renderer.lastProject.Name)
		}
		if renderer.lastProject.Image != "nginx" {
			t.Errorf("expected input image 'nginx', got %q", renderer.lastProject.Image)
		}
		if renderer.lastProject.Tag != "1.25" {
			t.Errorf("expected input tag '1.25', got %q", renderer.lastProject.Tag)
		}
	})

	t.Run("UpdateDeployment calls renderer and Reconcile", func(t *testing.T) {
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
		if !applier.reconcileCalled {
			t.Error("expected applier.Reconcile to be called on UpdateDeployment")
		}
		if applier.applyCalled {
			t.Error("expected applier.Apply NOT to be called on UpdateDeployment (Reconcile is used instead)")
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
		if len(renderer.lastProject.Command) != 1 || renderer.lastProject.Command[0] != "myapp" {
			t.Errorf("expected command [myapp], got %v", renderer.lastProject.Command)
		}
		if len(renderer.lastProject.Args) != 2 || renderer.lastProject.Args[0] != "--port" || renderer.lastProject.Args[1] != "8080" {
			t.Errorf("expected args [--port 8080], got %v", renderer.lastProject.Args)
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
		if len(renderer.lastProject.Command) != 1 || renderer.lastProject.Command[0] != "myapp" {
			t.Errorf("expected command [myapp] from stored data, got %v", renderer.lastProject.Command)
		}
		if len(renderer.lastProject.Args) != 2 || renderer.lastProject.Args[0] != "--port" {
			t.Errorf("expected args [--port 8080] from stored data, got %v", renderer.lastProject.Args)
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

// TestHandler_CreateDeploymentRollback tests that CreateDeployment rolls back on failure.
func TestHandler_CreateDeploymentRollback(t *testing.T) {
	t.Run("rolls back ConfigMap when apply fails", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		renderer := &stubRenderer{}
		applier := &stubApplier{applyErr: fmt.Errorf("simulated apply failure")}
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
		if err == nil {
			t.Fatal("expected error when apply fails")
		}

		// Cleanup should have been called to remove partial K8s resources.
		if !applier.cleanupCalled {
			t.Error("expected applier.Cleanup to be called on apply failure for rollback")
		}

		// The deployment ConfigMap should have been deleted (rolled back).
		cms, listErr := fakeClient.CoreV1().ConfigMaps("prj-my-project").List(ctx, metav1.ListOptions{})
		if listErr != nil {
			t.Fatalf("listing configmaps: %v", listErr)
		}
		for _, cm := range cms.Items {
			if cm.Name == "web-app" {
				t.Error("expected deployment ConfigMap to be deleted after apply failure (rollback)")
			}
		}
	})

	t.Run("rolls back ConfigMap when render fails", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		renderer := &stubRenderer{err: fmt.Errorf("simulated render failure")}
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
		if err == nil {
			t.Fatal("expected error when render fails")
		}

		// Cleanup should have been called.
		if !applier.cleanupCalled {
			t.Error("expected applier.Cleanup to be called on render failure for rollback")
		}

		// The deployment ConfigMap should have been deleted (rolled back).
		cms, listErr := fakeClient.CoreV1().ConfigMaps("prj-my-project").List(ctx, metav1.ListOptions{})
		if listErr != nil {
			t.Fatalf("listing configmaps: %v", listErr)
		}
		for _, cm := range cms.Items {
			if cm.Name == "web-app" {
				t.Error("expected deployment ConfigMap to be deleted after render failure (rollback)")
			}
		}
	})
}

// TestHandler_GetDeploymentRenderPreview tests the GetDeploymentRenderPreview RPC.
func TestHandler_GetDeploymentRenderPreview(t *testing.T) {
	t.Run("viewer can get render preview", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentRenderPreviewRequest{
			Project: "my-project",
			Name:    "web-app",
		})
		resp, err := handler.GetDeploymentRenderPreview(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.CueTemplate == "" {
			t.Error("expected non-empty cue_template")
		}
		if resp.Msg.CuePlatformInput == "" {
			t.Error("expected non-empty cue_platform_input")
		}
		if resp.Msg.CueProjectInput == "" {
			t.Error("expected non-empty cue_user_input")
		}
	})

	t.Run("returns NotFound for missing deployment", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentRenderPreviewRequest{
			Project: "my-project",
			Name:    "does-not-exist",
		})
		_, err := handler.GetDeploymentRenderPreview(ctx, req)
		if err == nil {
			t.Fatal("expected error for missing deployment")
		}
		if connect.CodeOf(err) != connect.CodeNotFound {
			t.Errorf("expected CodeNotFound, got %v", connect.CodeOf(err))
		}
	})

	t.Run("returns NotFound when template not found", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		k8s := NewK8sClient(fakeClient, testResolver())
		errResolver := &stubTemplateResolver{err: fmt.Errorf("template not found")}
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, errResolver, &stubRenderer{}, &stubApplier{})

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentRenderPreviewRequest{
			Project: "my-project",
			Name:    "web-app",
		})
		_, err := handler.GetDeploymentRenderPreview(ctx, req)
		if err == nil {
			t.Fatal("expected error when template not found")
		}
		if connect.CodeOf(err) != connect.CodeNotFound {
			t.Errorf("expected CodeNotFound, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		handler := defaultHandler(fakeClient, &stubProjectResolver{})

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.GetDeploymentRenderPreviewRequest{
			Project: "my-project",
			Name:    "web-app",
		})
		_, err := handler.GetDeploymentRenderPreview(ctx, req)
		if err == nil {
			t.Fatal("expected error for unauthenticated request")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connect.CodeOf(err))
		}
	})

	t.Run("response contains system and user input fields", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentRenderPreviewRequest{
			Project: "my-project",
			Name:    "web-app",
		})
		resp, err := handler.GetDeploymentRenderPreview(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Platform input should contain project name.
		if !containsStr(resp.Msg.CuePlatformInput, "my-project") {
			t.Errorf("expected cue_platform_input to contain project name, got: %q", resp.Msg.CuePlatformInput)
		}
		// User input should contain deployment name and image.
		if !containsStr(resp.Msg.CueProjectInput, "web-app") {
			t.Errorf("expected cue_user_input to contain deployment name, got: %q", resp.Msg.CueProjectInput)
		}
		if !containsStr(resp.Msg.CueProjectInput, "nginx") {
			t.Errorf("expected cue_user_input to contain image, got: %q", resp.Msg.CueProjectInput)
		}
	})
}

