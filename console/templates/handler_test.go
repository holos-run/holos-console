package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// stubRenderer implements Renderer for tests.
type stubRenderer struct {
	resources []RenderResource
	err       error
}

func (r *stubRenderer) Render(_ context.Context, _ string, _ string) ([]RenderResource, error) {
	return r.resources, r.err
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

// templateConfigMapWithDefaults builds a ConfigMap that includes a defaults.json key.
func templateConfigMapWithDefaults(project, name, displayName, description, cueTemplate string, defaultsJSON string) *corev1.ConfigMap {
	cm := templateConfigMap(project, name, displayName, description, cueTemplate)
	if defaultsJSON != "" {
		cm.Data[DefaultsKey] = defaultsJSON
	}
	return cm
}

func TestHandler_DefaultsRoundTrip(t *testing.T) {
	t.Run("create with defaults then get returns defaults", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"editor@example.com": "editor"}}
		handler := NewHandler(k8s, pr, nil)

		ctx := authedCtx("editor@example.com", nil)
		createReq := connect.NewRequest(&consolev1.CreateDeploymentTemplateRequest{
			Project:     "my-project",
			Name:        "web-app",
			DisplayName: "Web App",
			CueTemplate: validCue,
			Defaults: &consolev1.DeploymentDefaults{
				Image: "ghcr.io/mccutchen/go-httpbin",
				Tag:   "2.21",
			},
		})
		_, err := handler.CreateDeploymentTemplate(ctx, createReq)
		if err != nil {
			t.Fatalf("create: expected no error, got %v", err)
		}

		getReq := connect.NewRequest(&consolev1.GetDeploymentTemplateRequest{Project: "my-project", Name: "web-app"})
		getResp, err := handler.GetDeploymentTemplate(ctx, getReq)
		if err != nil {
			t.Fatalf("get: expected no error, got %v", err)
		}
		tmpl := getResp.Msg.Template
		if tmpl.Defaults == nil {
			t.Fatal("expected non-nil defaults after create with defaults")
		}
		if tmpl.Defaults.Image != "ghcr.io/mccutchen/go-httpbin" {
			t.Errorf("expected image 'ghcr.io/mccutchen/go-httpbin', got %q", tmpl.Defaults.Image)
		}
		if tmpl.Defaults.Tag != "2.21" {
			t.Errorf("expected tag '2.21', got %q", tmpl.Defaults.Tag)
		}
	})

	t.Run("create without defaults then get returns nil defaults", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"editor@example.com": "editor"}}
		handler := NewHandler(k8s, pr, nil)

		ctx := authedCtx("editor@example.com", nil)
		createReq := connect.NewRequest(&consolev1.CreateDeploymentTemplateRequest{
			Project:     "my-project",
			Name:        "web-app",
			DisplayName: "Web App",
			CueTemplate: validCue,
		})
		_, err := handler.CreateDeploymentTemplate(ctx, createReq)
		if err != nil {
			t.Fatalf("create: expected no error, got %v", err)
		}

		getReq := connect.NewRequest(&consolev1.GetDeploymentTemplateRequest{Project: "my-project", Name: "web-app"})
		getResp, err := handler.GetDeploymentTemplate(ctx, getReq)
		if err != nil {
			t.Fatalf("get: expected no error, got %v", err)
		}
		if getResp.Msg.Template.Defaults != nil {
			t.Error("expected nil defaults when template created without defaults")
		}
	})

	t.Run("update to add defaults then get returns defaults", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", validCue)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"editor@example.com": "editor"}}
		handler := NewHandler(k8s, pr, nil)

		ctx := authedCtx("editor@example.com", nil)
		updateReq := connect.NewRequest(&consolev1.UpdateDeploymentTemplateRequest{
			Project: "my-project",
			Name:    "web-app",
			Defaults: &consolev1.DeploymentDefaults{
				Image: "ghcr.io/example/app",
				Tag:   "v2.0",
			},
		})
		_, err := handler.UpdateDeploymentTemplate(ctx, updateReq)
		if err != nil {
			t.Fatalf("update: expected no error, got %v", err)
		}

		getReq := connect.NewRequest(&consolev1.GetDeploymentTemplateRequest{Project: "my-project", Name: "web-app"})
		getResp, err := handler.GetDeploymentTemplate(ctx, getReq)
		if err != nil {
			t.Fatalf("get: expected no error, got %v", err)
		}
		tmpl := getResp.Msg.Template
		if tmpl.Defaults == nil {
			t.Fatal("expected non-nil defaults after update with defaults")
		}
		if tmpl.Defaults.Image != "ghcr.io/example/app" {
			t.Errorf("expected image 'ghcr.io/example/app', got %q", tmpl.Defaults.Image)
		}
	})

	t.Run("list returns defaults on each template", func(t *testing.T) {
		ns := projectNS("my-project")
		defaultsJSON := `{"image":"ghcr.io/example/app","tag":"v1.0"}`
		cm := templateConfigMapWithDefaults("my-project", "web-app", "Web App", "A web app", validCue, defaultsJSON)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"viewer@example.com": "viewer"}}
		handler := NewHandler(k8s, pr, nil)

		ctx := authedCtx("viewer@example.com", nil)
		listReq := connect.NewRequest(&consolev1.ListDeploymentTemplatesRequest{Project: "my-project"})
		listResp, err := handler.ListDeploymentTemplates(ctx, listReq)
		if err != nil {
			t.Fatalf("list: expected no error, got %v", err)
		}
		if len(listResp.Msg.Templates) != 1 {
			t.Fatalf("expected 1 template, got %d", len(listResp.Msg.Templates))
		}
		tmpl := listResp.Msg.Templates[0]
		if tmpl.Defaults == nil {
			t.Fatal("expected non-nil defaults in list response")
		}
		if tmpl.Defaults.Image != "ghcr.io/example/app" {
			t.Errorf("expected image 'ghcr.io/example/app', got %q", tmpl.Defaults.Image)
		}
	})
}

func TestHandler_ListDeploymentTemplates(t *testing.T) {
	t.Run("viewer can list templates", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := templateConfigMap("my-project", "web-app", "Web App", "A web app", validCue)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, &stubProjectResolver{}, nil)

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
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, &stubProjectResolver{}, nil)

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
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, &stubProjectResolver{}, nil)

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
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, pr, nil)

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
		handler := NewHandler(k8s, pr, nil)

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

func TestHandler_RenderDeploymentTemplate(t *testing.T) {
	const validCueSrc = `package deployment
#Input: { name: string }
`

	t.Run("unauthenticated request returns CodeUnauthenticated", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, &stubProjectResolver{}, &stubRenderer{})

		req := connect.NewRequest(&consolev1.RenderDeploymentTemplateRequest{
			CueTemplate: validCueSrc,
		})
		_, err := handler.RenderDeploymentTemplate(context.Background(), req)
		if err == nil {
			t.Fatal("expected error for unauthenticated request")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connect.CodeOf(err))
		}
	})

	t.Run("missing cue_template returns CodeInvalidArgument", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		handler := NewHandler(k8s, nil, &stubRenderer{})

		ctx := authedCtx("viewer@example.com", nil)
		req := connect.NewRequest(&consolev1.RenderDeploymentTemplateRequest{
			CueTemplate: "",
		})
		_, err := handler.RenderDeploymentTemplate(ctx, req)
		if err == nil {
			t.Fatal("expected error for missing cue_template")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("valid request calls renderer with correct inputs and returns YAML", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		stub := &stubRenderer{
			resources: []RenderResource{
				{
					YAML:   "apiVersion: v1\nkind: ServiceAccount\n",
					Object: map[string]any{"apiVersion": "v1", "kind": "ServiceAccount"},
				},
				{
					YAML:   "apiVersion: apps/v1\nkind: Deployment\n",
					Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment"},
				},
			},
		}
		handler := NewHandler(k8s, nil, stub)

		ctx := authedCtx("viewer@example.com", nil)
		req := connect.NewRequest(&consolev1.RenderDeploymentTemplateRequest{
			CueTemplate: validCueSrc,
			CueInput: `input: {
	name:      "holos-console"
	image:     "ghcr.io/holos-run/holos-console"
	tag:       "latest"
	project:   "my-project"
	namespace: "holos-prj-my-project"
}`,
		})
		resp, err := handler.RenderDeploymentTemplate(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.RenderedYaml == "" {
			t.Error("expected non-empty rendered_yaml")
		}
		if !strings.Contains(resp.Msg.RenderedYaml, "ServiceAccount") {
			t.Error("expected YAML to contain ServiceAccount")
		}
		if !strings.Contains(resp.Msg.RenderedYaml, "---\n") {
			t.Error("expected YAML to contain document separator")
		}
	})

	t.Run("rendered_json is a pretty-printed JSON array of all resource objects", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		stub := &stubRenderer{
			resources: []RenderResource{
				{
					YAML:   "apiVersion: v1\nkind: ServiceAccount\n",
					Object: map[string]any{"apiVersion": "v1", "kind": "ServiceAccount"},
				},
				{
					YAML:   "apiVersion: apps/v1\nkind: Deployment\n",
					Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment"},
				},
			},
		}
		handler := NewHandler(k8s, nil, stub)

		ctx := authedCtx("viewer@example.com", nil)
		req := connect.NewRequest(&consolev1.RenderDeploymentTemplateRequest{
			CueTemplate: validCueSrc,
		})
		resp, err := handler.RenderDeploymentTemplate(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// rendered_json must be non-empty.
		if resp.Msg.RenderedJson == "" {
			t.Fatal("expected non-empty rendered_json")
		}

		// rendered_json must be pretty-printed (contains newlines).
		if !strings.Contains(resp.Msg.RenderedJson, "\n") {
			t.Error("expected rendered_json to be pretty-printed with newlines")
		}

		// rendered_json must be valid JSON and parse as a JSON array.
		var resources []map[string]any
		if err := json.Unmarshal([]byte(resp.Msg.RenderedJson), &resources); err != nil {
			t.Fatalf("rendered_json is not valid JSON: %v", err)
		}

		// Must contain both resources.
		if len(resources) != 2 {
			t.Fatalf("expected 2 elements in rendered_json array, got %d", len(resources))
		}

		// Verify resource kinds are present.
		kinds := make(map[string]bool)
		for _, r := range resources {
			if k, ok := r["kind"].(string); ok {
				kinds[k] = true
			}
		}
		if !kinds["ServiceAccount"] {
			t.Error("expected rendered_json to contain ServiceAccount")
		}
		if !kinds["Deployment"] {
			t.Error("expected rendered_json to contain Deployment")
		}
	})

	t.Run("rendered_json is empty array when renderer returns no objects", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		// Resources with no Object set (legacy path).
		stub := &stubRenderer{
			resources: []RenderResource{
				{YAML: "apiVersion: v1\nkind: ServiceAccount\n"},
			},
		}
		handler := NewHandler(k8s, nil, stub)

		ctx := authedCtx("viewer@example.com", nil)
		req := connect.NewRequest(&consolev1.RenderDeploymentTemplateRequest{
			CueTemplate: validCueSrc,
		})
		resp, err := handler.RenderDeploymentTemplate(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// rendered_json should be a valid JSON empty array.
		var resources []map[string]any
		if err := json.Unmarshal([]byte(resp.Msg.RenderedJson), &resources); err != nil {
			t.Fatalf("rendered_json is not valid JSON: %v", err)
		}
		if len(resources) != 0 {
			t.Errorf("expected 0 elements in rendered_json when no objects provided, got %d", len(resources))
		}
	})

	t.Run("renderer error is propagated as CodeInvalidArgument", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		k8s := NewK8sClient(fakeClient, testResolver())
		stub := &stubRenderer{err: fmt.Errorf("syntax error in CUE")}
		handler := NewHandler(k8s, nil, stub)

		ctx := authedCtx("viewer@example.com", nil)
		req := connect.NewRequest(&consolev1.RenderDeploymentTemplateRequest{
			CueTemplate: validCueSrc,
		})
		_, err := handler.RenderDeploymentTemplate(ctx, req)
		if err == nil {
			t.Fatal("expected error from renderer")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})
}
