package templates

import (
	"context"
	"testing"

	"connectrpc.com/connect"
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

const validCue = `package deployment

#Input: {
	name: string
}
`

func TestHandler_ListDeploymentTemplates(t *testing.T) {
	t.Run("viewer can list templates", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", validCue)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.ListDeploymentTemplatesRequest{Project: "my-project"})
		resp, err := handler.ListDeploymentTemplates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.Templates) != 1 {
			t.Fatalf("expected 1 template, got %d", len(resp.Msg.Templates))
		}
		if resp.Msg.Templates[0].Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", resp.Msg.Templates[0].Name)
		}
	})

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, &stubProjectResolver{})

		ctx := context.Background()
		req := connect.NewRequest(&consolev1.ListDeploymentTemplatesRequest{Project: "my-project"})
		_, err := handler.ListDeploymentTemplates(ctx, req)
		if err == nil {
			t.Fatal("expected error for unauthenticated request")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects unauthorized user", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{} // no grants
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("nobody@example.com", nil)
		req := connect.NewRequest(&consolev1.ListDeploymentTemplatesRequest{Project: "my-project"})
		_, err := handler.ListDeploymentTemplates(ctx, req)
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
		req := connect.NewRequest(&consolev1.ListDeploymentTemplatesRequest{Project: ""})
		_, err := handler.ListDeploymentTemplates(ctx, req)
		if err == nil {
			t.Fatal("expected error for empty project")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})
}

func TestHandler_GetDeploymentTemplate(t *testing.T) {
	t.Run("viewer can get template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", validCue)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("alice@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentTemplateRequest{Project: "my-project", Name: "web-app"})
		resp, err := handler.GetDeploymentTemplate(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Template.Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", resp.Msg.Template.Name)
		}
		if resp.Msg.Template.DisplayName != "Web App" {
			t.Errorf("expected display name 'Web App', got %q", resp.Msg.Template.DisplayName)
		}
		if resp.Msg.Template.CueTemplate != validCue {
			t.Errorf("expected cue template, got %q", resp.Msg.Template.CueTemplate)
		}
	})

	t.Run("rejects unauthorized user", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, &stubProjectResolver{})

		ctx := authedCtx("nobody@example.com", nil)
		req := connect.NewRequest(&consolev1.GetDeploymentTemplateRequest{Project: "my-project", Name: "web-app"})
		_, err := handler.GetDeploymentTemplate(ctx, req)
		if err == nil {
			t.Fatal("expected error for unauthorized user")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})
}

func TestHandler_CreateDeploymentTemplate(t *testing.T) {
	t.Run("editor can create template", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"editor@example.com": "editor"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("editor@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentTemplateRequest{
			Project:     "my-project",
			Name:        "web-app",
			DisplayName: "Web App",
			Description: "A web app",
			CueTemplate: validCue,
		})
		resp, err := handler.CreateDeploymentTemplate(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", resp.Msg.Name)
		}

		// Verify it was created in K8s
		_, err = fakeClient.CoreV1().ConfigMaps("prj-my-project").Get(context.Background(), "web-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected ConfigMap to exist, got %v", err)
		}
	})

	t.Run("viewer cannot create template", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"viewer@example.com": "viewer"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("viewer@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentTemplateRequest{
			Project:     "my-project",
			Name:        "web-app",
			DisplayName: "Web App",
			CueTemplate: validCue,
		})
		_, err := handler.CreateDeploymentTemplate(ctx, req)
		if err == nil {
			t.Fatal("expected error for viewer creating template")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects invalid template name", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"editor@example.com": "editor"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("editor@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentTemplateRequest{
			Project:     "my-project",
			Name:        "Invalid-Name!",
			DisplayName: "Bad Name",
			CueTemplate: validCue,
		})
		_, err := handler.CreateDeploymentTemplate(ctx, req)
		if err == nil {
			t.Fatal("expected error for invalid template name")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects invalid CUE syntax", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"editor@example.com": "editor"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("editor@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentTemplateRequest{
			Project:     "my-project",
			Name:        "web-app",
			DisplayName: "Web App",
			CueTemplate: "this is not valid {{ cue",
		})
		_, err := handler.CreateDeploymentTemplate(ctx, req)
		if err == nil {
			t.Fatal("expected error for invalid CUE syntax")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects empty name", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"editor@example.com": "editor"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("editor@example.com", nil)
		req := connect.NewRequest(&consolev1.CreateDeploymentTemplateRequest{
			Project:     "my-project",
			Name:        "",
			CueTemplate: validCue,
		})
		_, err := handler.CreateDeploymentTemplate(ctx, req)
		if err == nil {
			t.Fatal("expected error for empty name")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})
}

func TestHandler_UpdateDeploymentTemplate(t *testing.T) {
	t.Run("editor can update template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", validCue)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"editor@example.com": "editor"}}
		handler := NewHandler(k8s, pr)

		newDesc := "Updated description"
		ctx := authedCtx("editor@example.com", nil)
		req := connect.NewRequest(&consolev1.UpdateDeploymentTemplateRequest{
			Project:     "my-project",
			Name:        "web-app",
			Description: &newDesc,
		})
		_, err := handler.UpdateDeploymentTemplate(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("viewer cannot update template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", validCue)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"viewer@example.com": "viewer"}}
		handler := NewHandler(k8s, pr)

		newDesc := "Updated description"
		ctx := authedCtx("viewer@example.com", nil)
		req := connect.NewRequest(&consolev1.UpdateDeploymentTemplateRequest{
			Project:     "my-project",
			Name:        "web-app",
			Description: &newDesc,
		})
		_, err := handler.UpdateDeploymentTemplate(ctx, req)
		if err == nil {
			t.Fatal("expected error for viewer updating template")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects invalid CUE on update", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", validCue)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"editor@example.com": "editor"}}
		handler := NewHandler(k8s, pr)

		badCue := "this is not valid {{ cue"
		ctx := authedCtx("editor@example.com", nil)
		req := connect.NewRequest(&consolev1.UpdateDeploymentTemplateRequest{
			Project:     "my-project",
			Name:        "web-app",
			CueTemplate: &badCue,
		})
		_, err := handler.UpdateDeploymentTemplate(ctx, req)
		if err == nil {
			t.Fatal("expected error for invalid CUE syntax")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})
}

func TestHandler_DeleteDeploymentTemplate(t *testing.T) {
	t.Run("owner can delete template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", validCue)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"owner@example.com": "owner"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("owner@example.com", nil)
		req := connect.NewRequest(&consolev1.DeleteDeploymentTemplateRequest{Project: "my-project", Name: "web-app"})
		_, err := handler.DeleteDeploymentTemplate(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("editor cannot delete template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", validCue)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"editor@example.com": "editor"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("editor@example.com", nil)
		req := connect.NewRequest(&consolev1.DeleteDeploymentTemplateRequest{Project: "my-project", Name: "web-app"})
		_, err := handler.DeleteDeploymentTemplate(ctx, req)
		if err == nil {
			t.Fatal("expected error for editor deleting template")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})

	t.Run("viewer cannot delete template", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", validCue)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"viewer@example.com": "viewer"}}
		handler := NewHandler(k8s, pr)

		ctx := authedCtx("viewer@example.com", nil)
		req := connect.NewRequest(&consolev1.DeleteDeploymentTemplateRequest{Project: "my-project", Name: "web-app"})
		_, err := handler.DeleteDeploymentTemplate(ctx, req)
		if err == nil {
			t.Fatal("expected error for viewer deleting template")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})
}
