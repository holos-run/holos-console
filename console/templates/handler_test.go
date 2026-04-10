package templates

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// stubProjectGrantResolver implements ProjectGrantResolver for tests.
type stubProjectGrantResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubProjectGrantResolver) GetProjectGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
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

func (r *stubRenderer) RenderWithTemplateSources(_ context.Context, _ string, _ []string, _ string, _ string) ([]RenderResource, error) {
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
// a ConfigMap to the Template proto message.
func TestConfigMapToTemplate(t *testing.T) {
	t.Run("basic project scope fields populated", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-app",
				Namespace: "prj-my-project",
				Labels: map[string]string{
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationDisplayName: "Web App",
					v1alpha2.AnnotationDescription: "A web app",
				},
			},
			Data: map[string]string{
				CueTemplateKey: validCue,
			},
		}
		tmpl := configMapToTemplate(cm, consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT, "my-project")
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

	t.Run("org scope fields populated with mandatory and enabled", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ref-grant",
				Namespace: "org-acme",
				Annotations: map[string]string{
					v1alpha2.AnnotationDisplayName: "ReferenceGrant",
					v1alpha2.AnnotationMandatory:   "true",
					v1alpha2.AnnotationEnabled:     "true",
				},
			},
			Data: map[string]string{},
		}
		tmpl := configMapToTemplate(cm, consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, "acme")
		if !tmpl.Mandatory {
			t.Error("expected mandatory=true")
		}
		if !tmpl.Enabled {
			t.Error("expected enabled=true")
		}
		if tmpl.ScopeRef.Scope != consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION {
			t.Errorf("expected org scope, got %v", tmpl.ScopeRef.Scope)
		}
	})

	t.Run("linked templates from v1alpha2 annotation", func(t *testing.T) {
		type storedRef struct {
			Scope     string `json:"scope"`
			ScopeName string `json:"scope_name"`
			Name      string `json:"name"`
		}
		refs := []storedRef{
			{Scope: "organization", ScopeName: "acme", Name: "httproute"},
			{Scope: "folder", ScopeName: "payments", Name: "payments-policy"},
		}
		linkedJSON, _ := json.Marshal(refs)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-app",
				Namespace: "prj-my-project",
				Annotations: map[string]string{
					v1alpha2.AnnotationLinkedTemplates: string(linkedJSON),
				},
			},
			Data: map[string]string{},
		}
		tmpl := configMapToTemplate(cm, consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT, "my-project")
		if len(tmpl.LinkedTemplates) != 2 {
			t.Fatalf("expected 2 linked templates, got %d", len(tmpl.LinkedTemplates))
		}
		if tmpl.LinkedTemplates[0].Name != "httproute" {
			t.Errorf("expected 'httproute', got %q", tmpl.LinkedTemplates[0].Name)
		}
		if tmpl.LinkedTemplates[0].Scope != consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION {
			t.Errorf("expected org scope, got %v", tmpl.LinkedTemplates[0].Scope)
		}
		if tmpl.LinkedTemplates[1].Scope != consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER {
			t.Errorf("expected folder scope, got %v", tmpl.LinkedTemplates[1].Scope)
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
		tmpl := configMapToTemplate(cm, consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT, "my-project")
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

	t.Run("no defaults for org-scope templates even when CUE present", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ref-grant",
				Namespace: "org-acme",
			},
			Data: map[string]string{
				CueTemplateKey: validCue,
			},
		}
		tmpl := configMapToTemplate(cm, consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, "acme")
		if tmpl.Defaults != nil {
			t.Errorf("expected nil defaults for org-scope template, got %+v", tmpl.Defaults)
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
		tmpl := configMapToTemplate(cm, consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT, "my-project")
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
