package templates

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

// stubRenderer implements Renderer for tests.
type stubRenderer struct {
	resources []RenderResource
	err       error
}

func (r *stubRenderer) Render(_ context.Context, _ string, _ string, _ string) ([]RenderResource, error) {
	return r.resources, r.err
}

func (r *stubRenderer) RenderWithOrgTemplateSources(_ context.Context, _ string, _ []string, _ string, _ string) ([]RenderResource, error) {
	return r.resources, r.err
}

func authedCtx(email string, roles []string) context.Context {
	return rpc.ContextWithClaims(context.Background(), &rpc.Claims{
		Sub:   "user-123",
		Email: email,
		Roles: roles,
	})
}

const validCue = `
#Input: {
	name: string
}
`

// TestConfigMapToTemplate verifies that configMapToTemplate correctly converts
// a ConfigMap to the new v1alpha2 Template proto message.
func TestConfigMapToTemplate(t *testing.T) {
	t.Run("basic fields populated", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-app",
				Namespace: "prj-my-project",
				Annotations: map[string]string{
					v1alpha1.AnnotationDisplayName: "Web App",
					v1alpha1.AnnotationDescription: "A web app",
				},
			},
			Data: map[string]string{
				CueTemplateKey: validCue,
			},
		}
		tmpl := configMapToTemplate(cm, "my-project")
		if tmpl.Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", tmpl.Name)
		}
		if tmpl.DisplayName != "Web App" {
			t.Errorf("expected display name 'Web App', got %q", tmpl.DisplayName)
		}
		if tmpl.Description != "A web app" {
			t.Errorf("expected description 'A web app', got %q", tmpl.Description)
		}
		if tmpl.CueTemplate != validCue {
			t.Errorf("expected cue template, got %q", tmpl.CueTemplate)
		}
		if tmpl.ScopeRef == nil {
			t.Fatal("expected non-nil ScopeRef")
		}
		if tmpl.ScopeRef.ScopeName != "my-project" {
			t.Errorf("expected scope_name 'my-project', got %q", tmpl.ScopeRef.ScopeName)
		}
		if tmpl.ScopeRef.Scope != consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT {
			t.Errorf("expected project scope, got %v", tmpl.ScopeRef.Scope)
		}
	})

	t.Run("linked templates from annotation", func(t *testing.T) {
		linkedJSON, _ := json.Marshal([]string{"httproute", "security-policy"})
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-app",
				Namespace: "prj-my-project",
				Annotations: map[string]string{
					v1alpha1.AnnotationLinkedOrgTemplates: string(linkedJSON),
				},
			},
			Data: map[string]string{},
		}
		tmpl := configMapToTemplate(cm, "my-project")
		if len(tmpl.LinkedTemplates) != 2 {
			t.Fatalf("expected 2 linked templates, got %d", len(tmpl.LinkedTemplates))
		}
		if tmpl.LinkedTemplates[0].Name != "httproute" {
			t.Errorf("expected 'httproute', got %q", tmpl.LinkedTemplates[0].Name)
		}
		if tmpl.LinkedTemplates[1].Name != "security-policy" {
			t.Errorf("expected 'security-policy', got %q", tmpl.LinkedTemplates[1].Name)
		}
		for _, ref := range tmpl.LinkedTemplates {
			if ref.Scope != consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION {
				t.Errorf("expected org scope for linked template %q, got %v", ref.Name, ref.Scope)
			}
		}
	})

	t.Run("defaults from annotation fallback", func(t *testing.T) {
		defaultsJSON := `{"image":"ghcr.io/example/app","tag":"v1.0"}`
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-app",
				Namespace: "prj-my-project",
			},
			Data: map[string]string{
				DefaultsKey: defaultsJSON,
			},
		}
		tmpl := configMapToTemplate(cm, "my-project")
		if tmpl.Defaults == nil {
			t.Fatal("expected non-nil defaults from annotation fallback")
		}
		if tmpl.Defaults.Image != "ghcr.io/example/app" {
			t.Errorf("expected image 'ghcr.io/example/app', got %q", tmpl.Defaults.Image)
		}
		if tmpl.Defaults.Tag != "v1.0" {
			t.Errorf("expected tag 'v1.0', got %q", tmpl.Defaults.Tag)
		}
	})

	t.Run("no defaults when both sources absent", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-app",
				Namespace: "prj-my-project",
			},
			Data: map[string]string{
				CueTemplateKey: validCue,
			},
		}
		tmpl := configMapToTemplate(cm, "my-project")
		if tmpl.Defaults != nil {
			t.Errorf("expected nil defaults when neither CUE defaults nor annotation present, got %+v", tmpl.Defaults)
		}
	})
}

// TestValidateTemplateName verifies the DNS label validation rules.
func TestValidateTemplateName(t *testing.T) {
	valid := []string{"web-app", "my-service", "abc", "a1b2c3"}
	for _, name := range valid {
		if err := validateTemplateName(name); err != nil {
			t.Errorf("expected valid name %q to pass, got %v", name, err)
		}
	}
	invalid := []string{"", "Invalid-Name!", "1bad", "-bad", "bad-", "this-name-is-longer-than-sixty-three-characters-and-should-fail!"}
	for _, name := range invalid {
		if err := validateTemplateName(name); err == nil {
			t.Errorf("expected invalid name %q to fail, but it passed", name)
		}
	}
}

// TestValidateCueSyntax verifies CUE syntax detection.
func TestValidateCueSyntax(t *testing.T) {
	if err := validateCueSyntax(validCue); err != nil {
		t.Errorf("expected valid CUE to pass, got %v", err)
	}
	if err := validateCueSyntax("this is not valid {{ cue"); err == nil {
		t.Error("expected invalid CUE to fail, but it passed")
	}
}
