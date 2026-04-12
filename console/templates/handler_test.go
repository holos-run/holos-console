package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
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

func (r *stubRenderer) RenderGrouped(_ context.Context, _ string, _ string, _ string) (*GroupedRenderResources, error) {
	return &GroupedRenderResources{Project: r.resources}, r.err
}

func (r *stubRenderer) RenderGroupedWithTemplateSources(_ context.Context, _ string, _ []string, _ string, _ string) (*GroupedRenderResources, error) {
	return &GroupedRenderResources{Project: r.resources}, r.err
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

// newTestHandler builds a Handler wired to a fake K8s client and stub grant
// resolver for link permission tests. The grant resolver maps emails to roles
// via shareUsers so tests can control which role the caller has.
func newTestHandler(fakeClient *fake.Clientset, shareUsers map[string]string) *Handler {
	r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
	k8s := NewK8sClient(fakeClient, r)
	handler := NewHandler(k8s, r, &stubRenderer{})
	handler.WithProjectGrantResolver(&stubProjectGrantResolver{users: shareUsers})
	return handler
}

// orgLinkedRef returns a LinkedTemplateRef pointing at an org-scope template.
func orgLinkedRef(org, name string) *consolev1.LinkedTemplateRef {
	return &consolev1.LinkedTemplateRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
		ScopeName: org,
		Name:      name,
	}
}

// folderLinkedRef returns a LinkedTemplateRef pointing at a folder-scope template.
func folderLinkedRef(folder, name string) *consolev1.LinkedTemplateRef {
	return &consolev1.LinkedTemplateRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER,
		ScopeName: folder,
		Name:      name,
	}
}

// projectScopeRef returns a project-scoped TemplateScopeRef.
func projectScopeRef(project string) *consolev1.TemplateScopeRef {
	return &consolev1.TemplateScopeRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		ScopeName: project,
	}
}

// TestCreateTemplateLinkPermissions verifies that CreateTemplate enforces scoped
// link permissions: OWNER can create with org and folder links, EDITOR cannot.
func TestCreateTemplateLinkPermissions(t *testing.T) {
	const project = "my-project"
	const ownerEmail = "platform@localhost"
	const editorEmail = "product@localhost"

	tests := []struct {
		name            string
		email           string
		linkedTemplates []*consolev1.LinkedTemplateRef
		wantErr         bool
		wantCode        connect.Code
	}{
		{
			name:            "OWNER creates template with org-linked templates succeeds",
			email:           ownerEmail,
			linkedTemplates: []*consolev1.LinkedTemplateRef{orgLinkedRef("acme", "httproute")},
			wantErr:         false,
		},
		{
			name:            "OWNER creates template with folder-linked templates succeeds",
			email:           ownerEmail,
			linkedTemplates: []*consolev1.LinkedTemplateRef{folderLinkedRef("payments", "payments-policy")},
			wantErr:         false,
		},
		{
			name:  "OWNER creates template with both org and folder links succeeds",
			email: ownerEmail,
			linkedTemplates: []*consolev1.LinkedTemplateRef{
				orgLinkedRef("acme", "httproute"),
				folderLinkedRef("payments", "payments-policy"),
			},
			wantErr: false,
		},
		{
			name:            "EDITOR creates template with org-linked templates fails",
			email:           editorEmail,
			linkedTemplates: []*consolev1.LinkedTemplateRef{orgLinkedRef("acme", "httproute")},
			wantErr:         true,
			wantCode:        connect.CodePermissionDenied,
		},
		{
			name:            "EDITOR creates template with folder-linked templates fails",
			email:           editorEmail,
			linkedTemplates: []*consolev1.LinkedTemplateRef{folderLinkedRef("payments", "payments-policy")},
			wantErr:         true,
			wantCode:        connect.CodePermissionDenied,
		},
		{
			name:            "EDITOR creates template with no linked templates succeeds",
			email:           editorEmail,
			linkedTemplates: nil,
			wantErr:         false,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "prj-" + project,
				},
			}
			fakeClient := fake.NewClientset(ns)
			shareUsers := map[string]string{
				ownerEmail:  "owner",
				editorEmail: "editor",
			}
			handler := newTestHandler(fakeClient, shareUsers)

			// Use a unique template name per test to avoid AlreadyExists.
			templateName := fmt.Sprintf("tmpl-%d", i)
			ctx := authedCtx(tt.email, nil)
			req := connect.NewRequest(&consolev1.CreateTemplateRequest{
				Scope: projectScopeRef(project),
				Template: &consolev1.Template{
					Name:            templateName,
					CueTemplate:     validCue,
					LinkedTemplates: tt.linkedTemplates,
				},
			})

			_, err := handler.CreateTemplate(ctx, req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if connectErr := new(connect.Error); connect.CodeOf(err) != tt.wantCode {
					t.Errorf("expected code %v, got %v (%v)", tt.wantCode, connect.CodeOf(err), connectErr)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			}
		})
	}
}

// TestUpdateTemplateLinkPermissions verifies that UpdateTemplate honors the
// update_linked_templates flag and enforces scoped link permissions.
func TestUpdateTemplateLinkPermissions(t *testing.T) {
	const project = "my-project"
	const ownerEmail = "platform@localhost"
	const editorEmail = "product@localhost"

	// Pre-seed a template with org-linked templates for update tests.
	existingLinkedJSON := `[{"scope":"organization","scope_name":"acme","name":"httproute"}]`

	makeExistingTemplate := func() *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-app",
				Namespace: "prj-" + project,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationDisplayName:     "Web App",
					v1alpha2.AnnotationDescription:     "A web app",
					v1alpha2.AnnotationMandatory:       "false",
					v1alpha2.AnnotationEnabled:         "false",
					v1alpha2.AnnotationLinkedTemplates: existingLinkedJSON,
				},
			},
			Data: map[string]string{
				CueTemplateKey: validCue,
			},
		}
	}

	tests := []struct {
		name                 string
		email                string
		updateLinkedTmpl     bool
		linkedTemplates      []*consolev1.LinkedTemplateRef
		wantErr              bool
		wantCode             connect.Code
		wantLinksPreserved   bool // When true, verify existing links are still present after update.
		wantLinksCleared     bool // When true, verify linked-templates annotation is removed after update.
	}{
		{
			name:             "OWNER updates linked templates with update_linked_templates=true succeeds",
			email:            ownerEmail,
			updateLinkedTmpl: true,
			linkedTemplates: []*consolev1.LinkedTemplateRef{
				orgLinkedRef("acme", "httproute"),
				folderLinkedRef("payments", "payments-policy"),
			},
			wantErr: false,
		},
		{
			name:             "EDITOR updates linked templates with update_linked_templates=true fails",
			email:            editorEmail,
			updateLinkedTmpl: true,
			linkedTemplates: []*consolev1.LinkedTemplateRef{
				orgLinkedRef("acme", "new-route"),
			},
			wantErr:  true,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:             "EDITOR updates folder-linked templates with update_linked_templates=true fails",
			email:            editorEmail,
			updateLinkedTmpl: true,
			linkedTemplates: []*consolev1.LinkedTemplateRef{
				folderLinkedRef("payments", "payments-policy"),
			},
			wantErr:  true,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:               "EDITOR updates CUE only with update_linked_templates=false succeeds and preserves links",
			email:              editorEmail,
			updateLinkedTmpl:   false,
			linkedTemplates:    nil,
			wantErr:            false,
			wantLinksPreserved: true,
		},
		{
			name:             "OWNER clears all linked templates with update_linked_templates=true empty list succeeds",
			email:            ownerEmail,
			updateLinkedTmpl: true,
			linkedTemplates:  []*consolev1.LinkedTemplateRef{}, // empty list clears links
			wantErr:          false,
			wantLinksCleared: true,
		},
		{
			name:             "OWNER clears linked templates with update_linked_templates=true nil list succeeds",
			email:            ownerEmail,
			updateLinkedTmpl: true,
			linkedTemplates:  nil, // protobuf binary encoding delivers nil for empty repeated fields
			wantErr:          false,
			wantLinksCleared: true,
		},
		{
			name:             "EDITOR clears linked templates with update_linked_templates=true empty list fails",
			email:            editorEmail,
			updateLinkedTmpl: true,
			linkedTemplates:  []*consolev1.LinkedTemplateRef{}, // clearing requires permission on existing scopes
			wantErr:          true,
			wantCode:         connect.CodePermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "prj-" + project,
				},
			}
			cm := makeExistingTemplate()
			fakeClient := fake.NewClientset(ns, cm)
			shareUsers := map[string]string{
				ownerEmail:  "owner",
				editorEmail: "editor",
			}
			handler := newTestHandler(fakeClient, shareUsers)

			ctx := authedCtx(tt.email, nil)
			req := connect.NewRequest(&consolev1.UpdateTemplateRequest{
				Scope: projectScopeRef(project),
				Template: &consolev1.Template{
					Name:            "web-app",
					CueTemplate:     validCue,
					LinkedTemplates: tt.linkedTemplates,
				},
				UpdateLinkedTemplates: tt.updateLinkedTmpl,
			})

			_, err := handler.UpdateTemplate(ctx, req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if connect.CodeOf(err) != tt.wantCode {
					t.Errorf("expected code %v, got %v", tt.wantCode, connect.CodeOf(err))
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			// Verify link preservation when update_linked_templates=false.
			if tt.wantLinksPreserved {
				updated, getErr := fakeClient.CoreV1().ConfigMaps("prj-"+project).Get(context.Background(), "web-app", metav1.GetOptions{})
				if getErr != nil {
					t.Fatalf("failed to get updated ConfigMap: %v", getErr)
				}
				raw, ok := updated.Annotations[v1alpha2.AnnotationLinkedTemplates]
				if !ok {
					t.Fatal("expected linked-templates annotation to be preserved")
				}
				if raw != existingLinkedJSON {
					t.Errorf("expected links preserved as %q, got %q", existingLinkedJSON, raw)
				}
			}

			// Verify link clearing when update_linked_templates=true with empty or nil list.
			if tt.wantLinksCleared {
				updated, getErr := fakeClient.CoreV1().ConfigMaps("prj-"+project).Get(context.Background(), "web-app", metav1.GetOptions{})
				if getErr != nil {
					t.Fatalf("failed to get updated ConfigMap: %v", getErr)
				}
				if raw, ok := updated.Annotations[v1alpha2.AnnotationLinkedTemplates]; ok {
					t.Errorf("expected linked-templates annotation to be removed, but found %q", raw)
				}
			}
		})
	}
}

// TestUpdateTemplateMalformedLinkedAnnotation verifies that UpdateTemplate returns
// an error when the stored linked-templates annotation is malformed JSON, rather
// than silently discarding the parse error (which would leave existingRefs empty
// and allow an EDITOR to bypass link permission checks).
func TestUpdateTemplateMalformedLinkedAnnotation(t *testing.T) {
	const project = "my-project"
	const ownerEmail = "platform@localhost"

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prj-" + project,
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-app",
			Namespace: "prj-" + project,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:     "Web App",
				v1alpha2.AnnotationDescription:     "A web app",
				v1alpha2.AnnotationMandatory:       "false",
				v1alpha2.AnnotationEnabled:         "false",
				v1alpha2.AnnotationLinkedTemplates: `{not valid json`,
			},
		},
		Data: map[string]string{
			CueTemplateKey: validCue,
		},
	}

	fakeClient := fake.NewClientset(ns, cm)
	shareUsers := map[string]string{
		ownerEmail: "owner",
	}
	handler := newTestHandler(fakeClient, shareUsers)

	ctx := authedCtx(ownerEmail, nil)
	req := connect.NewRequest(&consolev1.UpdateTemplateRequest{
		Scope: projectScopeRef(project),
		Template: &consolev1.Template{
			Name:        "web-app",
			CueTemplate: validCue,
			LinkedTemplates: []*consolev1.LinkedTemplateRef{
				orgLinkedRef("acme", "httproute"),
			},
		},
		UpdateLinkedTemplates: true,
	})

	_, err := handler.UpdateTemplate(ctx, req)
	if err == nil {
		t.Fatal("expected error for malformed linked-templates annotation, got nil")
	}
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Errorf("expected code %v, got %v: %v", connect.CodeInternal, connect.CodeOf(err), err)
	}
}

// trackingRenderer records which renderer method was called so tests can
// verify that renderTemplateGrouped dispatches correctly.
type trackingRenderer struct {
	stubRenderer
	calledGrouped            bool
	calledGroupedWithSources bool
	lastTemplateSources      []string
}

func (r *trackingRenderer) RenderGrouped(_ context.Context, _ string, _ string, _ string) (*GroupedRenderResources, error) {
	r.calledGrouped = true
	return &GroupedRenderResources{Project: r.resources}, r.err
}

func (r *trackingRenderer) RenderGroupedWithTemplateSources(_ context.Context, _ string, sources []string, _ string, _ string) (*GroupedRenderResources, error) {
	r.calledGroupedWithSources = true
	r.lastTemplateSources = sources
	return &GroupedRenderResources{Project: r.resources}, r.err
}

// TestRenderTemplateGroupedFolderScoped verifies that renderTemplateGrouped
// resolves folder-scoped linked template refs when a walker is configured.
func TestRenderTemplateGroupedFolderScoped(t *testing.T) {
	const org = "acme"
	const folder = "payments"
	const project = "my-project"

	t.Run("folder-only linked refs uses ancestor walking", func(t *testing.T) {
		orgNsObj := orgNS(org)
		fldNsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "fld-" + folder,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
					v1alpha2.LabelFolder:       folder,
				},
			},
		}
		prjNsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "prj-" + project,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
					v1alpha2.LabelProject:      project,
				},
			},
		}

		folderCue := "// folder payments policy"
		fldCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "payments-policy",
				Namespace: "fld-" + folder,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationDisplayName: "Payments Policy",
					v1alpha2.AnnotationMandatory:   "false",
					v1alpha2.AnnotationEnabled:     "true",
				},
			},
			Data: map[string]string{CueTemplateKey: folderCue},
		}

		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM)
		r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
		k8s := NewK8sClient(fakeClient, r)
		renderer := &trackingRenderer{}
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj},
		}

		handler := NewHandler(k8s, r, renderer)
		handler.WithAncestorWalker(walker)

		msg := &consolev1.RenderTemplateRequest{
			Scope:       projectScopeRef(project),
			CueTemplate: validCue,
			LinkedTemplates: []*consolev1.LinkedTemplateRef{
				folderLinkedRef(folder, "payments-policy"),
			},
		}

		_, err := handler.renderTemplateGrouped(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !renderer.calledGroupedWithSources {
			t.Error("expected RenderGroupedWithTemplateSources to be called")
		}
		if len(renderer.lastTemplateSources) != 1 {
			t.Fatalf("expected 1 template source, got %d", len(renderer.lastTemplateSources))
		}
		if renderer.lastTemplateSources[0] != folderCue {
			t.Errorf("expected source %q, got %q", folderCue, renderer.lastTemplateSources[0])
		}
	})

	t.Run("mixed org+folder linked refs resolves from all scopes", func(t *testing.T) {
		orgNsObj := orgNS(org)
		fldNsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "fld-" + folder,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
					v1alpha2.LabelFolder:       folder,
				},
			},
		}
		prjNsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "prj-" + project,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
					v1alpha2.LabelProject:      project,
				},
			},
		}

		orgCue := "// org httproute"
		orgCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "httproute",
				Namespace: "org-" + org,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationMandatory: "false",
					v1alpha2.AnnotationEnabled:   "true",
				},
			},
			Data: map[string]string{CueTemplateKey: orgCue},
		}

		folderCue := "// folder payments policy"
		fldCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "payments-policy",
				Namespace: "fld-" + folder,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationMandatory: "false",
					v1alpha2.AnnotationEnabled:   "true",
				},
			},
			Data: map[string]string{CueTemplateKey: folderCue},
		}

		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, orgCM, fldCM)
		r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
		k8s := NewK8sClient(fakeClient, r)
		renderer := &trackingRenderer{}
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj},
		}

		handler := NewHandler(k8s, r, renderer)
		handler.WithAncestorWalker(walker)

		msg := &consolev1.RenderTemplateRequest{
			Scope:       projectScopeRef(project),
			CueTemplate: validCue,
			LinkedTemplates: []*consolev1.LinkedTemplateRef{
				orgLinkedRef(org, "httproute"),
				folderLinkedRef(folder, "payments-policy"),
			},
		}

		_, err := handler.renderTemplateGrouped(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !renderer.calledGroupedWithSources {
			t.Error("expected RenderGroupedWithTemplateSources to be called")
		}
		if len(renderer.lastTemplateSources) != 2 {
			t.Fatalf("expected 2 template sources, got %d", len(renderer.lastTemplateSources))
		}
	})

	t.Run("falls back to org-only when no walker configured", func(t *testing.T) {
		orgNsObj := orgNS(org)
		prjNsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "prj-" + project,
			},
		}

		orgCue := "// org httproute"
		orgCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "httproute",
				Namespace: "org-" + org,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationMandatory: "false",
					v1alpha2.AnnotationEnabled:   "true",
				},
			},
			Data: map[string]string{CueTemplateKey: orgCue},
		}

		fakeClient := fake.NewClientset(orgNsObj, prjNsObj, orgCM)
		r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
		k8s := NewK8sClient(fakeClient, r)
		renderer := &trackingRenderer{}
		// No walker configured.
		handler := NewHandler(k8s, r, renderer)

		msg := &consolev1.RenderTemplateRequest{
			CueTemplate: validCue,
			LinkedTemplates: []*consolev1.LinkedTemplateRef{
				orgLinkedRef(org, "httproute"),
			},
		}

		_, err := handler.renderTemplateGrouped(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should use org-only fallback.
		if !renderer.calledGroupedWithSources {
			t.Error("expected RenderGroupedWithTemplateSources to be called via org fallback")
		}
	})
}
