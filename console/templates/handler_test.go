package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
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
		tmpl := configMapToTemplate(cm, scopeKindProject, "my-project")
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
		// HOL-619 replaced tmpl.ScopeRef with tmpl.Namespace. The
		// expected project namespace for "my-project" under the test
		// resolver (prj- prefix, no org/namespace prefix) is
		// "prj-my-project".
		if tmpl.GetNamespace() != "prj-my-project" {
			t.Errorf("expected namespace 'prj-my-project', got %q", tmpl.GetNamespace())
		}
	})

	t.Run("org scope fields populated with enabled", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ref-grant",
				Namespace: "org-acme",
				Annotations: map[string]string{
					v1alpha2.AnnotationDisplayName: "ReferenceGrant",
					// The legacy `console.holos.run/mandatory` annotation was
					// removed in HOL-565. Templates that must always apply to
					// a project are now selected by TemplatePolicy REQUIRE
					// rules (wired in HOL-567).
					v1alpha2.AnnotationEnabled: "true",
				},
			},
			Data: map[string]string{},
		}
		tmpl := configMapToTemplate(cm, scopeKindOrganization, "acme")
		if !tmpl.Enabled {
			t.Error("expected enabled=true")
		}
		// HOL-619: expect the org namespace instead of ScopeRef.Scope.
		if tmpl.GetNamespace() != "org-acme" {
			t.Errorf("expected namespace 'org-acme', got %q", tmpl.GetNamespace())
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
		tmpl := configMapToTemplate(cm, scopeKindProject, "my-project")
		if len(tmpl.LinkedTemplates) != 2 {
			t.Fatalf("expected 2 linked templates, got %d", len(tmpl.LinkedTemplates))
		}
		if tmpl.LinkedTemplates[0].Name != "httproute" {
			t.Errorf("expected 'httproute', got %q", tmpl.LinkedTemplates[0].Name)
		}
		// Post-HOL-723 namespace is authoritative; classify via the test
		// resolver to assert the expected scope.
		if kind, _ := classifyNamespace(testResolver, tmpl.LinkedTemplates[0].GetNamespace()); kind != scopeKindOrganization {
			t.Errorf("expected org scope from namespace %q, got %v", tmpl.LinkedTemplates[0].GetNamespace(), kind)
		}
		if kind, _ := classifyNamespace(testResolver, tmpl.LinkedTemplates[1].GetNamespace()); kind != scopeKindFolder {
			t.Errorf("expected folder scope from namespace %q, got %v", tmpl.LinkedTemplates[1].GetNamespace(), kind)
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
		tmpl := configMapToTemplate(cm, scopeKindProject, "my-project")
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
		tmpl := configMapToTemplate(cm, scopeKindOrganization, "acme")
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
		tmpl := configMapToTemplate(cm, scopeKindProject, "my-project")
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
// via shareUsers so tests can control which role the caller has. Extra CRD
// seed objects (typically TemplateRelease fixtures after HOL-693) flow into
// the fake controller-runtime client that backs the Template CRUD path.
func newTestHandler(t *testing.T, fakeClient *fake.Clientset, shareUsers map[string]string, extra ...ctrlclient.Object) *Handler {
	h, _ := newTestHandlerAndK8s(t, fakeClient, shareUsers, extra...)
	return h
}

// newTestHandlerAndK8s is the variant that also returns the K8sClient the
// handler was wired with. Tests that need to assert on CRD state after a
// handler call read through the K8sClient so the HOL-661 ctrl.Client path
// is observable from within the test.
func newTestHandlerAndK8s(t *testing.T, fakeClient *fake.Clientset, shareUsers map[string]string, extra ...ctrlclient.Object) (*Handler, *K8sClient) {
	t.Helper()
	r := testResolver
	k8s := newTestK8sClient(t, fakeClient, r, extra...)
	handler := NewHandler(k8s, r, &stubRenderer{}, policyresolver.NewNoopResolver())
	handler.WithProjectGrantResolver(&stubProjectGrantResolver{users: shareUsers})
	return handler, k8s
}

// orgLinkedRef returns a LinkedTemplateRef pointing at an org-scope template.
func orgLinkedRef(org, name string) *consolev1.LinkedTemplateRef {
	return &consolev1.LinkedTemplateRef{Namespace: testResolver.OrgNamespace(org), Name: name}
}

// folderLinkedRef returns a LinkedTemplateRef pointing at a folder-scope template.
func folderLinkedRef(folder, name string) *consolev1.LinkedTemplateRef {
	return &consolev1.LinkedTemplateRef{Namespace: testResolver.FolderNamespace(folder), Name: name}
}

// projectScopeRef returns the Kubernetes namespace string for the named
// project scope. HOL-619 collapsed TemplateScopeRef; the handler now keys
// project-scope requests by namespace alone.
func projectScopeRef(project string) string {
	return testResolver.ProjectNamespace(project)
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
			handler := newTestHandler(t, fakeClient, shareUsers)

			// Use a unique template name per test to avoid AlreadyExists.
			templateName := fmt.Sprintf("tmpl-%d", i)
			ctx := authedCtx(tt.email, nil)
			req := connect.NewRequest(&consolev1.CreateTemplateRequest{
				Namespace: projectScopeRef(project),
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

// TestCreateTemplateLinkedRefsValidation covers the HOL-723 guardrail: a
// LinkedTemplateRef whose namespace does not classify as a console-managed
// org/folder/project namespace must be rejected before persistence, because
// render-time resolution only walks ancestor namespaces and would silently
// drop such a ref. HOL-619 flattened the scope enum off the wire; HOL-723
// retired the (scope, scopeName) CRD fields that previously caught this at
// decode time.
func TestCreateTemplateLinkedRefsValidation(t *testing.T) {
	const project = "my-project"
	const ownerEmail = "platform@localhost"
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prj-" + project,
		},
	}
	fakeClient := fake.NewClientset(ns)
	handler := newTestHandler(t, fakeClient, map[string]string{ownerEmail: "owner"})
	ctx := authedCtx(ownerEmail, nil)

	req := connect.NewRequest(&consolev1.CreateTemplateRequest{
		Namespace: projectScopeRef(project),
		Template: &consolev1.Template{
			Name:        "foreign-linked",
			CueTemplate: validCue,
			LinkedTemplates: []*consolev1.LinkedTemplateRef{
				{Namespace: "default", Name: "rogue"},
			},
		},
	})

	_, err := handler.CreateTemplate(ctx, req)
	if err == nil {
		t.Fatal("expected validation error for foreign linked namespace, got nil")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
	}
	if !strings.Contains(err.Error(), "not a console-managed") {
		t.Errorf("expected 'not a console-managed' in error, got: %v", err)
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
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationDisplayName:     "Web App",
					v1alpha2.AnnotationDescription:     "A web app",
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
		name               string
		email              string
		updateLinkedTmpl   bool
		linkedTemplates    []*consolev1.LinkedTemplateRef
		wantErr            bool
		wantCode           connect.Code
		wantLinksPreserved bool // When true, verify existing links are still present after update.
		wantLinksCleared   bool // When true, verify linked-templates annotation is removed after update.
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
			handler, k8s := newTestHandlerAndK8s(t, fakeClient, shareUsers)

			ctx := authedCtx(tt.email, nil)
			req := connect.NewRequest(&consolev1.UpdateTemplateRequest{
				Namespace: projectScopeRef(project),
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

			// Post-HOL-661 linked templates live on Template.Spec.LinkedTemplates
			// (typed), not on the legacy ConfigMap annotation. Read through the
			// K8sClient so the assertions observe production storage.
			updated, getErr := k8s.GetTemplate(context.Background(), "prj-"+project, "web-app")
			if getErr != nil {
				t.Fatalf("failed to get updated Template: %v", getErr)
			}

			// Verify link preservation when update_linked_templates=false.
			if tt.wantLinksPreserved {
				if len(updated.Spec.LinkedTemplates) == 0 {
					t.Fatal("expected linked templates to be preserved, but spec.linkedTemplates is empty")
				}
				got := updated.Spec.LinkedTemplates[0]
				if got.Namespace != testResolver.OrgNamespace("acme") || got.Name != "httproute" {
					t.Errorf("expected preserved org/acme/httproute link, got %+v", got)
				}
			}

			// Verify link clearing when update_linked_templates=true with empty or nil list.
			if tt.wantLinksCleared {
				if len(updated.Spec.LinkedTemplates) != 0 {
					t.Errorf("expected linked templates to be cleared, but found %d entries: %+v", len(updated.Spec.LinkedTemplates), updated.Spec.LinkedTemplates)
				}
			}
		})
	}
	_ = existingLinkedJSON
}

// NOTE: TestUpdateTemplateMalformedLinkedAnnotation was removed in HOL-661.
// Before HOL-661 linked templates were persisted as a JSON annotation on the
// Template ConfigMap, and the update-path parsed that annotation to enforce
// scoped link permissions against the pre-existing refs. The test guarded
// against a silent JSON-parse failure allowing an EDITOR to bypass those
// checks. After HOL-661 linked templates are a typed field on
// Template.Spec.LinkedTemplates — there is no JSON parse step to fail, so
// the invariant the test asserted no longer exists. Permission enforcement
// still happens via crdLinkedToProto on the typed spec.

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
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationDisplayName: "Payments Policy",
					v1alpha2.AnnotationEnabled:     "true",
				},
			},
			Data: map[string]string{CueTemplateKey: folderCue},
		}

		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM)
		r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
		k8s := newTestK8sClient(t, fakeClient, r)
		renderer := &trackingRenderer{}
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj},
		}

		handler := NewHandler(k8s, r, renderer, policyresolver.NewNoopResolver())
		handler.WithAncestorWalker(walker)

		msg := &consolev1.RenderTemplateRequest{
			Namespace:   projectScopeRef(project),
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
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationEnabled: "true",
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
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationEnabled: "true",
				},
			},
			Data: map[string]string{CueTemplateKey: folderCue},
		}

		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, orgCM, fldCM)
		r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
		k8s := newTestK8sClient(t, fakeClient, r)
		renderer := &trackingRenderer{}
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj},
		}

		handler := NewHandler(k8s, r, renderer, policyresolver.NewNoopResolver())
		handler.WithAncestorWalker(walker)

		msg := &consolev1.RenderTemplateRequest{
			Namespace:   projectScopeRef(project),
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

	// With no walker configured, the unified renderTemplateGrouped degrades
	// to a plain render (no ancestor sources). The legacy org-only fallback
	// was deleted along with ListOrgTemplateSourcesForRender in HOL-564:
	// production always has a walker, and the org-only branch's dedup-by-name
	// was inconsistent with the walker branch's dedup-by-(scope, scopeName,
	// name). The structural invariant after Phase 2 is "one helper, one dedup
	// key, one fallback" — this test guards that invariant.
	t.Run("plain render when no walker configured", func(t *testing.T) {
		orgNsObj := orgNS(org)
		prjNsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "prj-" + project,
			},
		}

		fakeClient := fake.NewClientset(orgNsObj, prjNsObj)
		r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
		k8s := newTestK8sClient(t, fakeClient, r)
		renderer := &trackingRenderer{}
		// No walker configured.
		handler := NewHandler(k8s, r, renderer, policyresolver.NewNoopResolver())

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
		if renderer.calledGroupedWithSources {
			t.Error("did not expect RenderGroupedWithTemplateSources (no walker means plain render)")
		}
		if !renderer.calledGrouped {
			t.Error("expected RenderGrouped to be called (plain render with no ancestors)")
		}
	})

	// Structural invariant HOL-564 introduces: the preview path and the apply
	// path yield the same ancestor-source slice for the same inputs because
	// both route through K8sClient.ListEffectiveTemplateSources. This test
	// sets up a project with a folder-linked template and asserts that the
	// renderer sees the same source regardless of which TargetKind would be
	// wired through the rest of the stack.
	t.Run("preview source slice matches apply source slice", func(t *testing.T) {
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

		folderCue := "// folder shared template"
		fldCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "shared",
				Namespace: "fld-" + folder,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationEnabled: "true",
				},
			},
			Data: map[string]string{CueTemplateKey: folderCue},
		}

		fakeClient := fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM)
		r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
		k8s := newTestK8sClient(t, fakeClient, r)

		// Preview path: runs via renderTemplateGrouped + ListEffectiveTemplateSources.
		renderer := &trackingRenderer{}
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj},
		}
		handler := NewHandler(k8s, r, renderer, policyresolver.NewNoopResolver())
		handler.WithAncestorWalker(walker)
		_, err := handler.renderTemplateGrouped(context.Background(), &consolev1.RenderTemplateRequest{
			Namespace:   projectScopeRef(project),
			CueTemplate: validCue,
			LinkedTemplates: []*consolev1.LinkedTemplateRef{
				folderLinkedRef(folder, "shared"),
			},
		})
		if err != nil {
			t.Fatalf("preview render failed: %v", err)
		}

		// Apply path: runs via AncestorTemplateResolver + ListEffectiveTemplateSources.
		applyResolver := NewAncestorTemplateResolver(k8s, walker, policyresolver.NewNoopResolver())
		applySources, _, err := applyResolver.ListAncestorTemplateSources(
			context.Background(),
			"prj-"+project,
			"test-deployment",
			[]*consolev1.LinkedTemplateRef{folderLinkedRef(folder, "shared")},
		)
		if err != nil {
			t.Fatalf("apply render failed: %v", err)
		}

		if len(renderer.lastTemplateSources) != len(applySources) {
			t.Fatalf("source slice length drift: preview=%d apply=%d", len(renderer.lastTemplateSources), len(applySources))
		}
		for i := range renderer.lastTemplateSources {
			if renderer.lastTemplateSources[i] != applySources[i] {
				t.Errorf("source drift at index %d: preview=%q apply=%q", i, renderer.lastTemplateSources[i], applySources[i])
			}
		}
	})
}

// structuredJSONRenderer returns GroupedRenderResources with structured JSON
// fields populated, for testing that RenderTemplate propagates them.
type structuredJSONRenderer struct {
	stubRenderer
	defaultsJSON      *string
	platformInputJSON *string
	projectInputJSON  *string
	platResStructJSON *string
	projResStructJSON *string
}

func (r *structuredJSONRenderer) RenderGrouped(_ context.Context, _ string, _ string, _ string) (*GroupedRenderResources, error) {
	return &GroupedRenderResources{
		Project:                     r.resources,
		DefaultsJSON:                r.defaultsJSON,
		PlatformInputJSON:           r.platformInputJSON,
		ProjectInputJSON:            r.projectInputJSON,
		PlatformResourcesStructJSON: r.platResStructJSON,
		ProjectResourcesStructJSON:  r.projResStructJSON,
	}, r.err
}

func (r *structuredJSONRenderer) RenderGroupedWithTemplateSources(_ context.Context, _ string, _ []string, _ string, _ string) (*GroupedRenderResources, error) {
	return r.RenderGrouped(context.Background(), "", "", "")
}

func TestRenderTemplateStructuredJSON(t *testing.T) {
	defaults := `{"name":"httpbin","image":"ghcr.io/mccutchen/go-httpbin","tag":"2.21.0"}`
	platInput := `{"project":"my-project","namespace":"prj-my-project"}`
	projInput := `{"name":"web-app","image":"nginx","tag":"1.25"}`
	platRes := `{"namespacedResources":{},"clusterResources":{}}`
	projRes := `{"namespacedResources":{"prj-my-project":{}},"clusterResources":{}}`

	renderer := &structuredJSONRenderer{
		stubRenderer: stubRenderer{
			resources: []RenderResource{{
				YAML:   "apiVersion: v1\nkind: ServiceAccount\n",
				Object: map[string]any{"apiVersion": "v1", "kind": "ServiceAccount"},
			}},
		},
		defaultsJSON:      &defaults,
		platformInputJSON: &platInput,
		projectInputJSON:  &projInput,
		platResStructJSON: &platRes,
		projResStructJSON: &projRes,
	}

	handler := &Handler{renderer: renderer}

	ctx := authedCtx("platform@localhost", nil)
	req := connect.NewRequest(&consolev1.RenderTemplateRequest{
		CueTemplate: "// dummy",
	})
	resp, err := handler.RenderTemplate(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("DefaultsJson is populated", func(t *testing.T) {
		if resp.Msg.DefaultsJson == nil {
			t.Fatal("expected DefaultsJson to be set, got nil")
		}
		if *resp.Msg.DefaultsJson != defaults {
			t.Errorf("expected %q, got %q", defaults, *resp.Msg.DefaultsJson)
		}
	})

	t.Run("PlatformInputJson is populated", func(t *testing.T) {
		if resp.Msg.PlatformInputJson == nil {
			t.Fatal("expected PlatformInputJson to be set, got nil")
		}
		if *resp.Msg.PlatformInputJson != platInput {
			t.Errorf("expected %q, got %q", platInput, *resp.Msg.PlatformInputJson)
		}
	})

	t.Run("ProjectInputJson is populated", func(t *testing.T) {
		if resp.Msg.ProjectInputJson == nil {
			t.Fatal("expected ProjectInputJson to be set, got nil")
		}
		if *resp.Msg.ProjectInputJson != projInput {
			t.Errorf("expected %q, got %q", projInput, *resp.Msg.ProjectInputJson)
		}
	})

	t.Run("PlatformResourcesStructuredJson is populated", func(t *testing.T) {
		if resp.Msg.PlatformResourcesStructuredJson == nil {
			t.Fatal("expected PlatformResourcesStructuredJson to be set, got nil")
		}
		if *resp.Msg.PlatformResourcesStructuredJson != platRes {
			t.Errorf("expected %q, got %q", platRes, *resp.Msg.PlatformResourcesStructuredJson)
		}
	})

	t.Run("ProjectResourcesStructuredJson is populated", func(t *testing.T) {
		if resp.Msg.ProjectResourcesStructuredJson == nil {
			t.Fatal("expected ProjectResourcesStructuredJson to be set, got nil")
		}
		if *resp.Msg.ProjectResourcesStructuredJson != projRes {
			t.Errorf("expected %q, got %q", projRes, *resp.Msg.ProjectResourcesStructuredJson)
		}
	})

	t.Run("all structured JSON fields are valid JSON", func(t *testing.T) {
		fields := map[string]*string{
			"DefaultsJson":                    resp.Msg.DefaultsJson,
			"PlatformInputJson":               resp.Msg.PlatformInputJson,
			"ProjectInputJson":                resp.Msg.ProjectInputJson,
			"PlatformResourcesStructuredJson": resp.Msg.PlatformResourcesStructuredJson,
			"ProjectResourcesStructuredJson":  resp.Msg.ProjectResourcesStructuredJson,
		}
		for name, val := range fields {
			if val == nil {
				continue
			}
			if !json.Valid([]byte(*val)) {
				t.Errorf("%s is not valid JSON: %s", name, *val)
			}
		}
	})

	t.Run("nil structured fields remain nil in response", func(t *testing.T) {
		// Render with a stub that returns nil structured fields.
		plainRenderer := &stubRenderer{
			resources: []RenderResource{{
				YAML:   "apiVersion: v1\nkind: ServiceAccount\n",
				Object: map[string]any{"apiVersion": "v1", "kind": "ServiceAccount"},
			}},
		}
		plainHandler := &Handler{renderer: plainRenderer}
		resp, err := plainHandler.RenderTemplate(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Msg.DefaultsJson != nil {
			t.Errorf("expected DefaultsJson to be nil, got %q", *resp.Msg.DefaultsJson)
		}
		if resp.Msg.PlatformInputJson != nil {
			t.Errorf("expected PlatformInputJson to be nil, got %q", *resp.Msg.PlatformInputJson)
		}
	})
}

// TestGetTemplateDefaults covers the handler behavior required by ADR 027 and
// the acceptance criteria on issue #928: RBAC parity with GetTemplate, empty
// responses for missing-defaults and non-project scopes, and CodeNotFound for
// missing templates.
func TestGetTemplateDefaults(t *testing.T) {
	const project = "my-project"
	const org = "acme"
	const ownerEmail = "platform@localhost"
	const strangerEmail = "stranger@localhost"

	httpbinCue := `
defaults: #ProjectInput & {
    name:        "httpbin"
    image:       "ghcr.io/mccutchen/go-httpbin"
    tag:         "2.21.0"
    port:        8080
    description: "A simple HTTP Request & Response Service"
}
input: #ProjectInput & {
    name:  *defaults.name | (string & =~"^[a-z][a-z0-9-]*$")
    image: *defaults.image | _
    tag:   *defaults.tag | _
    port:  *defaults.port | (>0 & <=65535)
}
`
	bareCue := `
input: #ProjectInput & {
    name: string
}
`

	type want struct {
		wantErr  bool
		wantCode connect.Code
		// expected concrete fields on a success response; zero values mean
		// "assert this field is zero" (empty response).
		expectName        string
		expectImage       string
		expectTag         string
		expectPort        int32
		expectDescription string
	}

	tests := []struct {
		name        string
		email       string
		scope       string
		tmplName    string
		cueTemplate string // if non-empty, seeded as a project-scope template with this name
		orgTmpl     string // if non-empty, seeded as an org-scope template with tmplName in org namespace
		want        want
	}{
		{
			name:        "project-scope template with defaults block returns populated TemplateDefaults",
			email:       ownerEmail,
			scope:       projectScopeRef(project),
			tmplName:    "httpbin",
			cueTemplate: httpbinCue,
			want: want{
				expectName:        "httpbin",
				expectImage:       "ghcr.io/mccutchen/go-httpbin",
				expectTag:         "2.21.0",
				expectPort:        8080,
				expectDescription: "A simple HTTP Request & Response Service",
			},
		},
		{
			name:        "project-scope template without defaults block returns empty TemplateDefaults",
			email:       ownerEmail,
			scope:       projectScopeRef(project),
			tmplName:    "bare",
			cueTemplate: bareCue,
			want:        want{}, // all zero values — empty TemplateDefaults, no error
		},
		{
			name:     "missing project-scope template returns CodeNotFound",
			email:    ownerEmail,
			scope:    projectScopeRef(project),
			tmplName: "does-not-exist",
			want: want{
				wantErr:  true,
				wantCode: connect.CodeNotFound,
			},
		},
		{
			name:     "org-scope template returns empty TemplateDefaults (defaults are project-scope only)",
			email:    ownerEmail,
			scope:    orgScopeRef(org),
			tmplName: "org-tmpl",
			orgTmpl:  httpbinCue, // even with a defaults block, org scope returns empty
			want:     want{},
		},
		{
			name:        "caller without permission returns CodePermissionDenied",
			email:       strangerEmail,
			scope:       projectScopeRef(project),
			tmplName:    "httpbin",
			cueTemplate: httpbinCue,
			want: want{
				wantErr:  true,
				wantCode: connect.CodePermissionDenied,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Seed namespaces for both project and org scopes so handlers can
			// resolve either hierarchy.
			objs := []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prj-" + project}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "org-" + org}},
			}
			if tt.cueTemplate != "" {
				objs = append(objs, &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tt.tmplName,
						Namespace: "prj-" + project,
						Labels: map[string]string{
							v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
						},
					},
					Data: map[string]string{
						CueTemplateKey: tt.cueTemplate,
					},
				})
			}
			if tt.orgTmpl != "" {
				objs = append(objs, &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tt.tmplName,
						Namespace: "org-" + org,
						Labels: map[string]string{
							v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
						},
					},
					Data: map[string]string{
						CueTemplateKey: tt.orgTmpl,
					},
				})
			}
			fakeClient := fake.NewClientset(objs...)

			// Owner has read/write on both scopes; stranger has no grants.
			shareUsers := map[string]string{ownerEmail: "owner"}

			r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
			k8s := newTestK8sClient(t, fakeClient, r)
			handler := NewHandler(k8s, r, &stubRenderer{}, policyresolver.NewNoopResolver())
			handler.WithProjectGrantResolver(&stubProjectGrantResolver{users: shareUsers})
			handler.WithOrgGrantResolver(&stubOrgGrantResolver{users: shareUsers})

			ctx := authedCtx(tt.email, nil)
			resp, err := handler.GetTemplateDefaults(ctx, connect.NewRequest(&consolev1.GetTemplateDefaultsRequest{
				Namespace: tt.scope,
				Name:      tt.tmplName,
			}))

			if tt.want.wantErr {
				if err == nil {
					t.Fatalf("expected error with code %v, got nil", tt.want.wantCode)
				}
				if connect.CodeOf(err) != tt.want.wantCode {
					t.Fatalf("expected code %v, got %v (%v)", tt.want.wantCode, connect.CodeOf(err), err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if resp == nil || resp.Msg == nil {
				t.Fatal("expected non-nil response")
			}
			if resp.Msg.Defaults == nil {
				t.Fatal("expected non-nil Defaults on response (empty message, never nil pointer)")
			}
			d := resp.Msg.Defaults
			if d.Name != tt.want.expectName {
				t.Errorf("Name: expected %q, got %q", tt.want.expectName, d.Name)
			}
			if d.Image != tt.want.expectImage {
				t.Errorf("Image: expected %q, got %q", tt.want.expectImage, d.Image)
			}
			if d.Tag != tt.want.expectTag {
				t.Errorf("Tag: expected %q, got %q", tt.want.expectTag, d.Tag)
			}
			if d.Port != tt.want.expectPort {
				t.Errorf("Port: expected %d, got %d", tt.want.expectPort, d.Port)
			}
			if d.Description != tt.want.expectDescription {
				t.Errorf("Description: expected %q, got %q", tt.want.expectDescription, d.Description)
			}
		})
	}
}

// TestDeleteTemplateHandler verifies that Handler.DeleteTemplate enforces authz,
// input validation, and storage behaviour for every code path in the handler.
// The K8s-layer delete is covered by TestDeleteTemplate in k8s_test.go; this
// suite focuses on the RPC handler surface: RBAC, missing-claims, empty-name
// validation, and the not-found propagation via mapK8sError.
//
// The audit-log line (slog.InfoContext "template deleted") is emitted on the
// success path and is trusted by code path rather than captured, matching the
// precedent set by every other test in this file.
func TestDeleteTemplateHandler(t *testing.T) {
	const project = "my-project"
	const ownerEmail = "platform@localhost"
	const editorEmail = "product@localhost"

	// shareUsers maps email → role; owner has PermissionTemplatesDelete,
	// editor does not (editor only has PermissionTemplatesWrite per rbac.go).
	shareUsers := map[string]string{
		ownerEmail:  "owner",
		editorEmail: "editor",
	}

	tests := []struct {
		name      string
		ctx       context.Context
		namespace string
		tmplName  string
		// seedName, when non-empty, seeds a template with this name instead of
		// tmplName. Used for the not-found case where we want a different
		// template present so the scope namespace exists but the target is absent.
		seedName string
		wantErr  bool
		wantCode connect.Code
	}{
		{
			name:      "owner deletes existing template succeeds",
			ctx:       authedCtx(ownerEmail, nil),
			namespace: projectScopeRef(project),
			tmplName:  "web-app",
			wantErr:   false,
		},
		{
			name:      "unauthenticated returns CodeUnauthenticated",
			ctx:       context.Background(), // no claims
			namespace: projectScopeRef(project),
			tmplName:  "web-app",
			wantErr:   true,
			wantCode:  connect.CodeUnauthenticated,
		},
		{
			name:      "editor without delete permission returns CodePermissionDenied",
			ctx:       authedCtx(editorEmail, nil),
			namespace: projectScopeRef(project),
			tmplName:  "web-app",
			wantErr:   true,
			wantCode:  connect.CodePermissionDenied,
		},
		{
			// The handler checks name == "" after extracting scope, so the
			// request still needs a well-formed namespace (see Pitfalls in the
			// issue description).
			name:      "empty name returns CodeInvalidArgument",
			ctx:       authedCtx(ownerEmail, nil),
			namespace: projectScopeRef(project),
			tmplName:  "", // empty
			wantErr:   true,
			wantCode:  connect.CodeInvalidArgument,
		},
		{
			// Seed a different template so the namespace exists but the
			// requested name is absent, exercising the mapK8sError(NotFound)
			// path.
			name:      "absent template returns CodeNotFound",
			ctx:       authedCtx(ownerEmail, nil),
			namespace: projectScopeRef(project),
			tmplName:  "does-not-exist",
			seedName:  "other-template", // present in the fake client
			wantErr:   true,
			wantCode:  connect.CodeNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the namespace object so extractScope succeeds.
			nsObj := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "prj-" + project},
			}

			// Choose which template name to seed (if any).
			seedName := tt.seedName
			if seedName == "" && tt.tmplName != "" {
				seedName = tt.tmplName
			}

			var objs []runtime.Object
			objs = append(objs, nsObj)
			if seedName != "" {
				objs = append(objs, &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      seedName,
						Namespace: "prj-" + project,
						Labels: map[string]string{
							v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
						},
					},
					Data: map[string]string{CueTemplateKey: validCue},
				})
			}

			fakeClient := fake.NewClientset(objs...)
			handler, k8s := newTestHandlerAndK8s(t, fakeClient, shareUsers)

			req := connect.NewRequest(&consolev1.DeleteTemplateRequest{
				Namespace: tt.namespace,
				Name:      tt.tmplName,
			})

			_, err := handler.DeleteTemplate(tt.ctx, req)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error with code %v, got nil", tt.wantCode)
				}
				if connect.CodeOf(err) != tt.wantCode {
					t.Errorf("expected code %v, got %v (%v)", tt.wantCode, connect.CodeOf(err), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			// Verify the template is gone from the fake client by asserting the
			// follow-up Get surfaces NotFound.
			_, getErr := k8s.GetTemplate(context.Background(), "prj-"+project, tt.tmplName)
			if getErr == nil {
				t.Fatal("expected GetTemplate to return NotFound after delete, got nil")
			}
		})
	}
}

// TestGetTemplateDefaultsValidation verifies request-level input validation
// that is independent of RBAC or storage state.
func TestGetTemplateDefaultsValidation(t *testing.T) {
	r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
	k8s := newTestK8sClient(t, fake.NewClientset(), r)
	handler := NewHandler(k8s, r, &stubRenderer{}, policyresolver.NewNoopResolver())
	handler.WithProjectGrantResolver(&stubProjectGrantResolver{})
	handler.WithOrgGrantResolver(&stubOrgGrantResolver{})

	t.Run("empty name returns CodeInvalidArgument", func(t *testing.T) {
		_, err := handler.GetTemplateDefaults(
			authedCtx("anyone@localhost", nil),
			connect.NewRequest(&consolev1.GetTemplateDefaultsRequest{
				Namespace: projectScopeRef("p"),
				Name:      "",
			}),
		)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("missing claims returns CodeUnauthenticated", func(t *testing.T) {
		_, err := handler.GetTemplateDefaults(
			context.Background(),
			connect.NewRequest(&consolev1.GetTemplateDefaultsRequest{
				Namespace: projectScopeRef("p"),
				Name:      "foo",
			}),
		)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", connect.CodeOf(err))
		}
	})

	t.Run("missing namespace returns CodeInvalidArgument", func(t *testing.T) {
		// HOL-619 replaced GetTemplateDefaultsRequest.Scope with
		// .Namespace. A zero value here must produce
		// CodeInvalidArgument so the handler does not silently route
		// the lookup to a mis-keyed namespace.
		_, err := handler.GetTemplateDefaults(
			authedCtx("anyone@localhost", nil),
			connect.NewRequest(&consolev1.GetTemplateDefaultsRequest{
				Namespace: "",
				Name:      "foo",
			}),
		)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
		}
	})
}

// TestHandler_ListTemplates_CreatedAt asserts that Template.CreatedAt is
// populated from the underlying Template CRD's CreationTimestamp in RFC3339 format.
func TestHandler_ListTemplates_CreatedAt(t *testing.T) {
	tests := []struct {
		name        string
		createdAt   time.Time
		wantRFC3339 string
	}{
		{
			name:        "UTC time is formatted as RFC3339",
			createdAt:   time.Date(2026, 4, 22, 19, 51, 10, 0, time.UTC),
			wantRFC3339: "2026-04-22T19:51:10Z",
		},
		{
			name:        "non-UTC time is normalised to UTC in RFC3339",
			createdAt:   time.Date(2025, 1, 15, 8, 30, 0, 0, time.FixedZone("EST", -5*60*60)),
			wantRFC3339: "2025-01-15T13:30:00Z",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const projectName = "my-project"
			ns := testResolver.ProjectNamespace(projectName)

			// Build a Template CRD with a controlled CreationTimestamp.
			tmplCRD := &templatesv1alpha1.Template{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ts-template",
					Namespace: ns,
					Labels: map[string]string{
						v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
					},
					CreationTimestamp: metav1.NewTime(tc.createdAt),
				},
				Spec: templatesv1alpha1.TemplateSpec{
					DisplayName: "TS Template",
					Description: "created_at round-trip test",
					CueTemplate: validCue,
				},
			}

			// Seed an empty Clientset (no ConfigMap templates) and inject the
			// Template CRD directly as an extra ctrl object.
			fakeClient := fake.NewClientset()
			handler := newTestHandler(t, fakeClient, map[string]string{"platform@localhost": "owner"}, tmplCRD)

			ctx := authedCtx("platform@localhost", nil)
			resp, err := handler.ListTemplates(ctx, connect.NewRequest(&consolev1.ListTemplatesRequest{
				Namespace: ns,
			}))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(resp.Msg.Templates) != 1 {
				t.Fatalf("expected 1 template, got %d", len(resp.Msg.Templates))
			}
			got := resp.Msg.Templates[0].CreatedAt
			if got != tc.wantRFC3339 {
				t.Errorf("CreatedAt: got %q, want %q", got, tc.wantRFC3339)
			}
		})
	}
}
