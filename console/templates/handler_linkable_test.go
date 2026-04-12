package templates

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// stubAncestorWalker implements AncestorWalker for tests, returning a
// predetermined list of ancestor namespaces.
type stubAncestorWalker struct {
	ancestors []*corev1.Namespace
	err       error
}

func (s *stubAncestorWalker) WalkAncestors(_ context.Context, _ string) ([]*corev1.Namespace, error) {
	return s.ancestors, s.err
}

// enabledTemplateCM creates an enabled template ConfigMap suitable for
// ListLinkableTemplateInfos.
func enabledTemplateCM(ns, name, displayName, description string, mandatory bool) *corev1.ConfigMap {
	mandatoryStr := "false"
	if mandatory {
		mandatoryStr = "true"
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: displayName,
				v1alpha2.AnnotationDescription: description,
				v1alpha2.AnnotationMandatory:   mandatoryStr,
				v1alpha2.AnnotationEnabled:     "true",
			},
		},
		Data: map[string]string{CueTemplateKey: validCue},
	}
}

// makeReleaseCMWithData creates a release ConfigMap with CUE template and
// defaults data so we can verify stripping behavior.
func makeReleaseCMWithData(ns, templateName, version, cue, defaults string) *corev1.ConfigMap {
	v, _ := ParseVersion(version)
	data := map[string]string{
		CueTemplateKey: cue,
	}
	if defaults != "" {
		data[DefaultsKey] = defaults
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ReleaseConfigMapName(templateName, v),
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplateRelease,
				v1alpha2.LabelReleaseOf:     templateName,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationTemplateVersion: version,
			},
		},
		Data: data,
	}
}

// newLinkableTestHandler builds a Handler with an AncestorWalker wired up for
// testing ListLinkableTemplates. Uses org-level grant resolver since linkable
// templates come from ancestor (org/folder) scopes.
func newLinkableTestHandler(fakeClient *fake.Clientset, shareUsers map[string]string, walker AncestorWalker) *Handler {
	r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
	k8s := NewK8sClient(fakeClient, r)
	handler := NewHandler(k8s, r, &stubRenderer{})
	handler.WithProjectGrantResolver(&stubProjectGrantResolver{users: shareUsers})
	handler.WithAncestorWalker(walker)
	return handler
}

func TestListLinkableTemplatesReleases(t *testing.T) {
	const org = "acme"
	const project = "my-project"
	const ownerEmail = "platform@localhost"
	const templateName = "httproute"

	shareUsers := map[string]string{ownerEmail: "owner"}

	t.Run("returns releases for each linkable template", func(t *testing.T) {
		orgNsObj := orgNS(org)
		projectNsObj := projectNS(project)
		tmpl := enabledTemplateCM("org-"+org, templateName, "HTTPRoute", "Expose via gateway", false)
		r1 := makeReleaseCMWithData("org-"+org, templateName, "1.0.0", validCue, `{"image":"nginx:1.0"}`)
		r2 := makeReleaseCMWithData("org-"+org, templateName, "2.0.0", validCue, `{"image":"nginx:2.0"}`)

		fakeClient := fake.NewClientset(orgNsObj, projectNsObj, tmpl, r1, r2)
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{projectNsObj, orgNsObj},
		}
		handler := newLinkableTestHandler(fakeClient, shareUsers, walker)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListLinkableTemplatesRequest{
			Scope: projectScopeRef(project),
		})

		resp, err := handler.ListLinkableTemplates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(resp.Msg.Templates) != 1 {
			t.Fatalf("expected 1 linkable template, got %d", len(resp.Msg.Templates))
		}
		lt := resp.Msg.Templates[0]
		if lt.Name != templateName {
			t.Errorf("expected name %q, got %q", templateName, lt.Name)
		}
		if len(lt.Releases) != 2 {
			t.Fatalf("expected 2 releases, got %d", len(lt.Releases))
		}
	})

	t.Run("releases are sorted descending by version", func(t *testing.T) {
		orgNsObj := orgNS(org)
		projectNsObj := projectNS(project)
		tmpl := enabledTemplateCM("org-"+org, templateName, "HTTPRoute", "Expose via gateway", false)
		r1 := makeReleaseCMWithData("org-"+org, templateName, "1.0.0", validCue, "")
		r2 := makeReleaseCMWithData("org-"+org, templateName, "1.5.0", validCue, "")
		r3 := makeReleaseCMWithData("org-"+org, templateName, "2.0.0", validCue, "")

		fakeClient := fake.NewClientset(orgNsObj, projectNsObj, tmpl, r1, r2, r3)
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{projectNsObj, orgNsObj},
		}
		handler := newLinkableTestHandler(fakeClient, shareUsers, walker)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListLinkableTemplatesRequest{
			Scope: projectScopeRef(project),
		})

		resp, err := handler.ListLinkableTemplates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		lt := resp.Msg.Templates[0]
		if len(lt.Releases) != 3 {
			t.Fatalf("expected 3 releases, got %d", len(lt.Releases))
		}
		expectedVersions := []string{"2.0.0", "1.5.0", "1.0.0"}
		for i, r := range lt.Releases {
			if r.Version != expectedVersions[i] {
				t.Errorf("release %d: expected version %q, got %q", i, expectedVersions[i], r.Version)
			}
		}
	})

	t.Run("releases have cue_template and defaults stripped", func(t *testing.T) {
		orgNsObj := orgNS(org)
		projectNsObj := projectNS(project)
		tmpl := enabledTemplateCM("org-"+org, templateName, "HTTPRoute", "Expose via gateway", false)
		r1 := makeReleaseCMWithData("org-"+org, templateName, "1.0.0", validCue, `{"image":"nginx:1.0"}`)

		fakeClient := fake.NewClientset(orgNsObj, projectNsObj, tmpl, r1)
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{projectNsObj, orgNsObj},
		}
		handler := newLinkableTestHandler(fakeClient, shareUsers, walker)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListLinkableTemplatesRequest{
			Scope: projectScopeRef(project),
		})

		resp, err := handler.ListLinkableTemplates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		lt := resp.Msg.Templates[0]
		if len(lt.Releases) != 1 {
			t.Fatalf("expected 1 release, got %d", len(lt.Releases))
		}
		release := lt.Releases[0]
		if release.CueTemplate != "" {
			t.Errorf("expected cue_template to be stripped, got %q", release.CueTemplate)
		}
		if release.Defaults != nil {
			t.Errorf("expected defaults to be stripped, got %v", release.Defaults)
		}
		// Verify version and created_at are preserved.
		if release.Version != "1.0.0" {
			t.Errorf("expected version 1.0.0, got %q", release.Version)
		}
	})

	t.Run("no releases for template returns empty releases slice", func(t *testing.T) {
		orgNsObj := orgNS(org)
		projectNsObj := projectNS(project)
		tmpl := enabledTemplateCM("org-"+org, templateName, "HTTPRoute", "Expose via gateway", false)

		fakeClient := fake.NewClientset(orgNsObj, projectNsObj, tmpl)
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{projectNsObj, orgNsObj},
		}
		handler := newLinkableTestHandler(fakeClient, shareUsers, walker)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListLinkableTemplatesRequest{
			Scope: projectScopeRef(project),
		})

		resp, err := handler.ListLinkableTemplates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		lt := resp.Msg.Templates[0]
		if len(lt.Releases) != 0 {
			t.Errorf("expected 0 releases, got %d", len(lt.Releases))
		}
	})
}
