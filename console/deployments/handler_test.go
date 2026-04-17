package deployments

import (
	"context"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
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
	lastPlatform v1alpha2.PlatformInput
	lastProject  v1alpha2.ProjectInput
	// outputJSON, when non-nil, is copied onto GroupedResources.OutputJSON so
	// tests can simulate a template producing an `output` block.
	outputJSON *string
}

// Render implements Renderer for tests. The stub returns a GroupedResources
// whose Project group carries the stubbed resources; callers that care about
// the Platform group can wrap the stub.
func (s *stubRenderer) Render(_ context.Context, _ string, _ []string, inputs RenderInputs) (*GroupedResources, error) {
	s.called = true
	s.lastPlatform = inputs.Platform
	s.lastProject = inputs.Project
	if s.err != nil {
		return nil, s.err
	}
	return &GroupedResources{Project: s.resources, OutputJSON: s.outputJSON}, nil
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

func (s *stubApplier) Reconcile(_ context.Context, _, _ string, _ []unstructured.Unstructured, _ ...string) error {
	s.reconcileCalled = true
	return s.reconcileErr
}

func (s *stubApplier) Cleanup(_ context.Context, _ []string, _, _ string) error {
	s.cleanupCalled = true
	return s.cleanupErr
}

func (s *stubApplier) DiscoverNamespaces(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
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

	t.Run("populates status_summary from cache", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		// Fail on any unexpected live Deployment/Pod/Event list or get — status
		// must come purely from the cache on the listing hot path.
		fakeClient.PrependReactor("*", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("unexpected live deployments action on list path: %s %s", action.GetVerb(), action.GetResource().Resource)
		})
		fakeClient.PrependReactor("*", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("unexpected live pods action on list path: %s", action.GetVerb())
		})
		fakeClient.PrependReactor("*", "events", func(action ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("unexpected live events action on list path: %s", action.GetVerb())
		})

		cache := newFakeStatusCache()
		cache.set("prj-my-project", "web-app", &consolev1.DeploymentStatusSummary{
			Phase:           consolev1.DeploymentPhase_DEPLOYMENT_PHASE_RUNNING,
			ReadyReplicas:   3,
			DesiredReplicas: 3,
		})
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr).WithStatusCache(cache)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListDeploymentsRequest{Project: "my-project"})
		resp, err := handler.ListDeployments(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Msg.Deployments) != 1 {
			t.Fatalf("expected 1 deployment, got %d", len(resp.Msg.Deployments))
		}
		got := resp.Msg.Deployments[0].StatusSummary
		if got == nil {
			t.Fatal("status_summary: got nil, want populated from cache")
		}
		if got.Phase != consolev1.DeploymentPhase_DEPLOYMENT_PHASE_RUNNING {
			t.Errorf("phase: got %v, want RUNNING", got.Phase)
		}
		if got.ReadyReplicas != 3 || got.DesiredReplicas != 3 {
			t.Errorf("replicas: got %d/%d, want 3/3", got.ReadyReplicas, got.DesiredReplicas)
		}
	})

	t.Run("cache miss leaves status_summary nil", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		cache := newFakeStatusCache() // no entries
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr).WithStatusCache(cache)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListDeploymentsRequest{Project: "my-project"})
		resp, err := handler.ListDeployments(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Msg.Deployments) != 1 {
			t.Fatalf("expected 1 deployment, got %d", len(resp.Msg.Deployments))
		}
		if resp.Msg.Deployments[0].StatusSummary != nil {
			t.Errorf("status_summary: got %v, want nil on cache miss", resp.Msg.Deployments[0].StatusSummary)
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
			t.Error("expected non-empty cue_project_input")
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
			t.Errorf("expected cue_project_input to contain deployment name, got: %q", resp.Msg.CueProjectInput)
		}
		if !containsStr(resp.Msg.CueProjectInput, "nginx") {
			t.Errorf("expected cue_project_input to contain image, got: %q", resp.Msg.CueProjectInput)
		}
	})

	t.Run("response.Output is nil when template has no output section", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		// defaultHandler uses a stubRenderer with no outputJSON set, so the
		// handler should see OutputJSON=nil and leave Output unset.
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
		if resp.Msg.Output != nil {
			t.Errorf("expected response.Output to be nil when template has no output, got %+v", resp.Msg.Output)
		}
	})

	t.Run("response.Output.Url is populated when template sets output.url", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		k8s := NewK8sClient(fakeClient, testResolver())
		outputJSON := `{"url":"https://web-app.example.com"}`
		renderer := &stubRenderer{outputJSON: &outputJSON}
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, &stubApplier{})

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentRenderPreviewRequest{
			Project: "my-project",
			Name:    "web-app",
		})
		resp, err := handler.GetDeploymentRenderPreview(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Output == nil {
			t.Fatal("expected response.Output to be set, got nil")
		}
		if resp.Msg.Output.Url != "https://web-app.example.com" {
			t.Errorf("expected Output.Url = https://web-app.example.com, got %q", resp.Msg.Output.Url)
		}
	})

	t.Run("response.Output is set with empty Url when template declares output without url", func(t *testing.T) {
		// Pitfall guard: `output: {}` produces an OutputJSON of `{}`; the
		// backend must surface DeploymentOutput{Url: ""} (present-but-empty)
		// and let the frontend decide whether to render.
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		k8s := NewK8sClient(fakeClient, testResolver())
		outputJSON := `{}`
		renderer := &stubRenderer{outputJSON: &outputJSON}
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, &stubApplier{})

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentRenderPreviewRequest{
			Project: "my-project",
			Name:    "web-app",
		})
		resp, err := handler.GetDeploymentRenderPreview(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Output == nil {
			t.Fatal("expected response.Output to be set (non-nil) for empty output block, got nil")
		}
		if resp.Msg.Output.Url != "" {
			t.Errorf("expected Output.Url to be empty, got %q", resp.Msg.Output.Url)
		}
	})

	t.Run("response.Output is nil when OutputJSON is not valid JSON", func(t *testing.T) {
		// Malformed OutputJSON must not fail the RPC; the handler logs a
		// warning and leaves Output unset.
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		k8s := NewK8sClient(fakeClient, testResolver())
		bad := `not json`
		renderer := &stubRenderer{outputJSON: &bad}
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, &stubApplier{})

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentRenderPreviewRequest{
			Project: "my-project",
			Name:    "web-app",
		})
		resp, err := handler.GetDeploymentRenderPreview(ctx, req)
		if err != nil {
			t.Fatalf("expected no error for malformed OutputJSON, got %v", err)
		}
		if resp.Msg.Output != nil {
			t.Errorf("expected response.Output to be nil for malformed OutputJSON, got %+v", resp.Msg.Output)
		}
	})
}

// stubAncestorTemplateProvider implements AncestorTemplateProvider for tests.
type stubAncestorTemplateProvider struct {
	sources []string
	err     error
	called  bool
}

func (s *stubAncestorTemplateProvider) ListAncestorTemplateSources(_ context.Context, _ string, _ []*consolev1.LinkedTemplateRef) ([]string, error) {
	s.called = true
	return s.sources, s.err
}

// trackingDeploymentRenderer extends stubRenderer to record whether Render
// was called with ancestor sources. After the render-path collapse (HOL-563)
// the renderer exposes exactly one entry point, so the "was this the ancestor
// path" question is answered by inspecting lastAncestorSources rather than
// by counting separate method calls.
type trackingDeploymentRenderer struct {
	stubRenderer
	calledRender        bool
	lastAncestorSources []string
}

func (r *trackingDeploymentRenderer) Render(_ context.Context, _ string, ancestorSources []string, inputs RenderInputs) (*GroupedResources, error) {
	r.calledRender = true
	r.lastAncestorSources = ancestorSources
	r.lastPlatform = inputs.Platform
	r.lastProject = inputs.Project
	if r.err != nil {
		return nil, r.err
	}
	return &GroupedResources{Project: r.resources, OutputJSON: r.outputJSON}, nil
}

// renderedWithAncestors reports whether the last Render call passed a
// non-empty ancestor-sources slice. Tests use this to assert on which render
// path was taken, replacing the pre-HOL-563 distinction between separate
// "with ancestor" entry points and the plain Render.
func (r *trackingDeploymentRenderer) renderedWithAncestors() bool {
	return len(r.lastAncestorSources) > 0
}

func TestRenderResourcesWithAncestorProvider(t *testing.T) {
	t.Run("prefers ancestor provider when configured", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		renderer := &trackingDeploymentRenderer{}
		atp := &stubAncestorTemplateProvider{sources: []string{"// folder template"}}

		handler := NewHandler(k8s, &stubProjectResolver{}, &stubSettingsResolver{}, &stubTemplateResolver{}, renderer, nil).
			WithAncestorTemplateProvider(atp)

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, ScopeName: "payments", Name: "policy"},
		}
		_, err := handler.renderResources(context.Background(), "my-project", "// template", v1alpha2.PlatformInput{}, v1alpha2.ProjectInput{}, refs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !atp.called {
			t.Error("expected ancestor provider to be called")
		}
		if !renderer.calledRender {
			t.Error("expected Render to be called")
		}
		if !renderer.renderedWithAncestors() {
			t.Error("expected Render to be called with ancestor sources")
		}
		if len(renderer.lastAncestorSources) != 1 {
			t.Fatalf("expected 1 ancestor source, got %d", len(renderer.lastAncestorSources))
		}
	})

	t.Run("falls back to plain render when no ancestor provider configured", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		renderer := &trackingDeploymentRenderer{}

		handler := NewHandler(k8s, &stubProjectResolver{}, &stubSettingsResolver{}, &stubTemplateResolver{}, renderer, nil)

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "httproute"},
		}
		_, err := handler.renderResources(context.Background(), "my-project", "// template", v1alpha2.PlatformInput{}, v1alpha2.ProjectInput{}, refs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !renderer.calledRender {
			t.Error("expected Render to be called when no ancestor provider configured")
		}
		if renderer.renderedWithAncestors() {
			t.Error("expected Render to be called without ancestor sources when no provider is configured")
		}
	})

	t.Run("ancestor provider error falls back to plain render", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		renderer := &trackingDeploymentRenderer{}
		atp := &stubAncestorTemplateProvider{err: fmt.Errorf("walk failed")}

		handler := NewHandler(k8s, &stubProjectResolver{}, &stubSettingsResolver{}, &stubTemplateResolver{}, renderer, nil).
			WithAncestorTemplateProvider(atp)

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "httproute"},
		}
		_, err := handler.renderResources(context.Background(), "my-project", "// template", v1alpha2.PlatformInput{}, v1alpha2.ProjectInput{}, refs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !atp.called {
			t.Error("expected ancestor provider to be called first")
		}
		// Should fall back to plain render without platform templates.
		if !renderer.calledRender {
			t.Error("expected Render to be called after ancestor provider error")
		}
		if renderer.renderedWithAncestors() {
			t.Error("expected Render to be called without ancestor sources after provider error")
		}
	})
}

func TestRenderResourcesGroupedWithAncestorProvider(t *testing.T) {
	t.Run("prefers ancestor provider for grouped rendering", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		renderer := &trackingDeploymentRenderer{}
		atp := &stubAncestorTemplateProvider{sources: []string{"// folder template"}}

		handler := NewHandler(k8s, &stubProjectResolver{}, &stubSettingsResolver{}, &stubTemplateResolver{}, renderer, nil).
			WithAncestorTemplateProvider(atp)

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, ScopeName: "payments", Name: "policy"},
		}
		_, err := handler.renderResourcesGrouped(context.Background(), "my-project", "// template", v1alpha2.PlatformInput{}, v1alpha2.ProjectInput{}, refs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !atp.called {
			t.Error("expected ancestor provider to be called")
		}
		if !renderer.calledRender {
			t.Error("expected Render to be called")
		}
		if !renderer.renderedWithAncestors() {
			t.Error("expected Render to be called with ancestor sources")
		}
	})

	t.Run("empty ancestor sources renders without ancestors", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		renderer := &trackingDeploymentRenderer{}
		atp := &stubAncestorTemplateProvider{sources: nil}

		handler := NewHandler(k8s, &stubProjectResolver{}, &stubSettingsResolver{}, &stubTemplateResolver{}, renderer, nil).
			WithAncestorTemplateProvider(atp)

		refs := []*consolev1.LinkedTemplateRef{
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, ScopeName: "payments", Name: "policy"},
		}
		_, err := handler.renderResourcesGrouped(context.Background(), "my-project", "// template", v1alpha2.PlatformInput{}, v1alpha2.ProjectInput{}, refs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !renderer.calledRender {
			t.Error("expected Render to be called")
		}
		if renderer.renderedWithAncestors() {
			t.Error("expected Render to be called without ancestor sources when provider returns empty")
		}
	})
}

// TestHandler_OutputURLAnnotation exercises the output-url annotation cache
// that backs the "Open" link button on the deployments listing page. The
// annotation is authored by the handler at create/update time from the
// rendered CUE `output.url`, and read back during ListDeployments,
// GetDeployment, and GetDeploymentStatusSummary so those lightweight paths
// can surface the URL without re-running the render pipeline per call.
func TestHandler_OutputURLAnnotation(t *testing.T) {
	// fetchCM reloads the deployment ConfigMap so tests can assert on the
	// annotation set after the RPC returns.
	fetchCM := func(t *testing.T, fakeClient *fake.Clientset, project, name string) *corev1.ConfigMap {
		t.Helper()
		ns := "prj-" + project
		cm, err := fakeClient.CoreV1().ConfigMaps(ns).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to fetch deployment ConfigMap: %v", err)
		}
		return cm
	}

	t.Run("CreateDeployment caches output.url on annotation when template publishes a URL", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		k8s := NewK8sClient(fakeClient, testResolver())
		outputJSON := `{"url":"https://web-app.example.com"}`
		renderer := &stubRenderer{outputJSON: &outputJSON}
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, &stubApplier{})

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project: "my-project", Name: "web-app", Image: "nginx", Tag: "1.25", Template: "default",
		})
		if _, err := handler.CreateDeployment(ctx, req); err != nil {
			t.Fatalf("CreateDeployment: unexpected error: %v", err)
		}

		cm := fetchCM(t, fakeClient, "my-project", "web-app")
		got := cm.Annotations[OutputURLAnnotation]
		if got != "https://web-app.example.com" {
			t.Errorf("annotation: got %q, want %q", got, "https://web-app.example.com")
		}
	})

	t.Run("CreateDeployment skips annotation when template has no output block", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		k8s := NewK8sClient(fakeClient, testResolver())
		// outputJSON is nil — template did not declare an output section.
		renderer := &stubRenderer{}
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, &stubApplier{})

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project: "my-project", Name: "web-app", Image: "nginx", Tag: "1.25", Template: "default",
		})
		if _, err := handler.CreateDeployment(ctx, req); err != nil {
			t.Fatalf("CreateDeployment: unexpected error: %v", err)
		}

		cm := fetchCM(t, fakeClient, "my-project", "web-app")
		if _, ok := cm.Annotations[OutputURLAnnotation]; ok {
			t.Errorf("annotation should not be set when template has no output, got %q", cm.Annotations[OutputURLAnnotation])
		}
	})

	t.Run("CreateDeployment skips annotation when output block has empty url", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		k8s := NewK8sClient(fakeClient, testResolver())
		outputJSON := `{}`
		renderer := &stubRenderer{outputJSON: &outputJSON}
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, &stubApplier{})

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
			Project: "my-project", Name: "web-app", Image: "nginx", Tag: "1.25", Template: "default",
		})
		if _, err := handler.CreateDeployment(ctx, req); err != nil {
			t.Fatalf("CreateDeployment: unexpected error: %v", err)
		}

		cm := fetchCM(t, fakeClient, "my-project", "web-app")
		if _, ok := cm.Annotations[OutputURLAnnotation]; ok {
			t.Errorf("annotation should not be set for empty output.url, got %q", cm.Annotations[OutputURLAnnotation])
		}
	})

	t.Run("UpdateDeployment refreshes annotation when template publishes a new URL", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		cm.Annotations[OutputURLAnnotation] = "https://old.example.com"
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		k8s := NewK8sClient(fakeClient, testResolver())
		outputJSON := `{"url":"https://new.example.com"}`
		renderer := &stubRenderer{outputJSON: &outputJSON}
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, &stubApplier{})

		ctx := authedCtx("alice@example.com", nil)
		newTag := "1.26"
		req := connect.NewRequest(&consolev1.UpdateDeploymentRequest{
			Project: "my-project", Name: "web-app", Tag: &newTag,
		})
		if _, err := handler.UpdateDeployment(ctx, req); err != nil {
			t.Fatalf("UpdateDeployment: unexpected error: %v", err)
		}

		got := fetchCM(t, fakeClient, "my-project", "web-app").Annotations[OutputURLAnnotation]
		if got != "https://new.example.com" {
			t.Errorf("annotation: got %q, want %q", got, "https://new.example.com")
		}
	})

	t.Run("UpdateDeployment clears annotation when template drops output block", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		cm.Annotations[OutputURLAnnotation] = "https://stale.example.com"
		fakeClient := fake.NewClientset(ns, cm)
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
		k8s := NewK8sClient(fakeClient, testResolver())
		// outputJSON nil — template no longer declares an output section.
		renderer := &stubRenderer{}
		handler := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, &stubApplier{})

		ctx := authedCtx("alice@example.com", nil)
		newTag := "1.26"
		req := connect.NewRequest(&consolev1.UpdateDeploymentRequest{
			Project: "my-project", Name: "web-app", Tag: &newTag,
		})
		if _, err := handler.UpdateDeployment(ctx, req); err != nil {
			t.Fatalf("UpdateDeployment: unexpected error: %v", err)
		}

		got := fetchCM(t, fakeClient, "my-project", "web-app").Annotations
		if _, ok := got[OutputURLAnnotation]; ok {
			t.Errorf("annotation should be cleared after template drops output, got %q", got[OutputURLAnnotation])
		}
	})

	t.Run("ListDeployments populates statusSummary.output.url from annotation", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "desc")
		cm.Annotations[OutputURLAnnotation] = "https://web-app.example.com"
		fakeClient := fake.NewClientset(ns, cm)
		cache := newFakeStatusCache()
		cache.set("prj-my-project", "web-app", &consolev1.DeploymentStatusSummary{
			Phase:           consolev1.DeploymentPhase_DEPLOYMENT_PHASE_RUNNING,
			ReadyReplicas:   1,
			DesiredReplicas: 1,
		})
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr).WithStatusCache(cache)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListDeploymentsRequest{Project: "my-project"})
		resp, err := handler.ListDeployments(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Msg.Deployments) != 1 {
			t.Fatalf("expected 1 deployment, got %d", len(resp.Msg.Deployments))
		}
		summary := resp.Msg.Deployments[0].StatusSummary
		if summary == nil {
			t.Fatal("expected statusSummary to be populated, got nil")
		}
		if summary.Output == nil {
			t.Fatal("expected statusSummary.output to be populated from annotation, got nil")
		}
		if summary.Output.Url != "https://web-app.example.com" {
			t.Errorf("output.url: got %q, want %q", summary.Output.Url, "https://web-app.example.com")
		}
	})

	t.Run("ListDeployments leaves output nil when annotation is absent", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "desc")
		// No OutputURLAnnotation set.
		fakeClient := fake.NewClientset(ns, cm)
		cache := newFakeStatusCache()
		cache.set("prj-my-project", "web-app", &consolev1.DeploymentStatusSummary{
			Phase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_RUNNING,
		})
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr).WithStatusCache(cache)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListDeploymentsRequest{Project: "my-project"})
		resp, err := handler.ListDeployments(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		summary := resp.Msg.Deployments[0].StatusSummary
		if summary != nil && summary.Output != nil {
			t.Errorf("expected statusSummary.output to be nil when annotation is absent, got %+v", summary.Output)
		}
	})

	t.Run("ListDeployments surfaces annotation even on cache miss by synthesizing UNSPECIFIED summary", func(t *testing.T) {
		// A fresh deployment may have the output-url annotation set before
		// the status informer has observed the apps/v1.Deployment. Listing
		// should still surface the cached URL so the UI link appears.
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "desc")
		cm.Annotations[OutputURLAnnotation] = "https://web-app.example.com"
		fakeClient := fake.NewClientset(ns, cm)
		cache := newFakeStatusCache() // empty — intentional cache miss
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr).WithStatusCache(cache)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListDeploymentsRequest{Project: "my-project"})
		resp, err := handler.ListDeployments(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		summary := resp.Msg.Deployments[0].StatusSummary
		if summary == nil {
			t.Fatal("expected synthesized statusSummary when annotation is present on cache miss, got nil")
		}
		if summary.Phase != consolev1.DeploymentPhase_DEPLOYMENT_PHASE_UNSPECIFIED {
			t.Errorf("phase on synthesized summary: got %v, want UNSPECIFIED", summary.Phase)
		}
		if summary.Output == nil || summary.Output.Url != "https://web-app.example.com" {
			t.Errorf("expected output.url to be surfaced on cache miss, got %+v", summary.Output)
		}
	})

	t.Run("GetDeployment populates statusSummary.output.url from annotation", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "desc")
		cm.Annotations[OutputURLAnnotation] = "https://web-app.example.com"
		fakeClient := fake.NewClientset(ns, cm)
		cache := newFakeStatusCache()
		cache.set("prj-my-project", "web-app", &consolev1.DeploymentStatusSummary{
			Phase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_RUNNING,
		})
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr).WithStatusCache(cache)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentRequest{Project: "my-project", Name: "web-app"})
		resp, err := handler.GetDeployment(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		summary := resp.Msg.Deployment.StatusSummary
		if summary == nil || summary.Output == nil {
			t.Fatal("expected statusSummary.output to be populated, got nil")
		}
		if summary.Output.Url != "https://web-app.example.com" {
			t.Errorf("output.url: got %q, want %q", summary.Output.Url, "https://web-app.example.com")
		}
	})

	t.Run("GetDeploymentStatusSummary populates output.url from annotation", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "desc")
		cm.Annotations[OutputURLAnnotation] = "https://web-app.example.com"
		fakeClient := fake.NewClientset(ns, cm)
		cache := newFakeStatusCache()
		cache.set("prj-my-project", "web-app", &consolev1.DeploymentStatusSummary{
			Phase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_RUNNING,
		})
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := defaultHandler(fakeClient, pr).WithStatusCache(cache)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentStatusSummaryRequest{Project: "my-project", Name: "web-app"})
		resp, err := handler.GetDeploymentStatusSummary(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Msg.Summary == nil || resp.Msg.Summary.Output == nil {
			t.Fatal("expected summary.output to be populated, got nil")
		}
		if resp.Msg.Summary.Output.Url != "https://web-app.example.com" {
			t.Errorf("output.url: got %q, want %q", resp.Msg.Summary.Output.Url, "https://web-app.example.com")
		}
	})
}
