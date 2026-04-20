package templates

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// stubFolderGrantResolver implements FolderGrantResolver for tests.
type stubFolderGrantResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubFolderGrantResolver) GetFolderGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	return s.users, s.roles, s.err
}

// folderScopeRef returns the Kubernetes namespace string for the named
// folder scope. HOL-619 collapsed the TemplateScopeRef enum; the namespace
// is now the sole scope discriminator on request / proto messages.
func folderScopeRef(folder string) string {
	return scopeshim.DefaultResolver().FolderNamespace(folder)
}

// enabledTemplateCMForScope creates an enabled template ConfigMap in the given
// namespace with a scope-appropriate LabelTemplateScope value. Used by tests
// that need same-scope (non-org) templates such as folder-owned templates.
func enabledTemplateCMForScope(ns, name, displayName, description string, scope scopeshim.Scope) *corev1.ConfigMap {
	var scopeLabel string
	switch scope {
	case scopeshim.ScopeOrganization:
		scopeLabel = v1alpha2.TemplateScopeOrganization
	case scopeshim.ScopeFolder:
		scopeLabel = v1alpha2.TemplateScopeFolder
	case scopeshim.ScopeProject:
		scopeLabel = v1alpha2.TemplateScopeProject
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: scopeLabel,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: displayName,
				v1alpha2.AnnotationDescription: description,
				v1alpha2.AnnotationEnabled:     "true",
			},
		},
		Data: map[string]string{CueTemplateKey: validCue},
	}
}

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
// ListLinkableTemplateInfos. Prior to HOL-565 this helper also toggled the
// now-deleted `console.holos.run/mandatory` annotation; the boolean is
// retained as a no-op parameter so call sites that previously asserted the
// mandatory-vs-linked distinction continue to compile during the transition.
func enabledTemplateCM(ns, name, displayName, description string, _ bool) *corev1.ConfigMap {
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
				v1alpha2.AnnotationEnabled:     "true",
			},
		},
		Data: map[string]string{CueTemplateKey: validCue},
	}
}

// newLinkableTestHandler builds a Handler with an AncestorWalker wired up for
// testing ListLinkableTemplates. Uses org-level grant resolver since linkable
// templates come from ancestor (org/folder) scopes. Release fixtures are
// TemplateRelease CRDs (HOL-693) passed through the variadic extra argument
// — the fake Clientset no longer stores release ConfigMaps.
func newLinkableTestHandler(t *testing.T, fakeClient *fake.Clientset, shareUsers map[string]string, walker AncestorWalker, extra ...ctrlclient.Object) *Handler {
	t.Helper()
	r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
	k8s := newTestK8sClient(t, fakeClient, r, extra...)
	handler := NewHandler(k8s, r, &stubRenderer{}, policyresolver.NewNoopResolver())
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
		r1 := makeReleaseCRDWithData("org-"+org, templateName, "1.0.0", validCue, `{"image":"nginx:1.0"}`)
		r2 := makeReleaseCRDWithData("org-"+org, templateName, "2.0.0", validCue, `{"image":"nginx:2.0"}`)

		fakeClient := fake.NewClientset(orgNsObj, projectNsObj, tmpl)
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{projectNsObj, orgNsObj},
		}
		handler := newLinkableTestHandler(t, fakeClient, shareUsers, walker, r1, r2)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListLinkableTemplatesRequest{
			Namespace: projectScopeRef(project),
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
		r1 := makeReleaseCRDWithData("org-"+org, templateName, "1.0.0", validCue, "")
		r2 := makeReleaseCRDWithData("org-"+org, templateName, "1.5.0", validCue, "")
		r3 := makeReleaseCRDWithData("org-"+org, templateName, "2.0.0", validCue, "")

		fakeClient := fake.NewClientset(orgNsObj, projectNsObj, tmpl)
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{projectNsObj, orgNsObj},
		}
		handler := newLinkableTestHandler(t, fakeClient, shareUsers, walker, r1, r2, r3)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListLinkableTemplatesRequest{
			Namespace: projectScopeRef(project),
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
		r1 := makeReleaseCRDWithData("org-"+org, templateName, "1.0.0", validCue, `{"image":"nginx:1.0"}`)

		fakeClient := fake.NewClientset(orgNsObj, projectNsObj, tmpl)
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{projectNsObj, orgNsObj},
		}
		handler := newLinkableTestHandler(t, fakeClient, shareUsers, walker, r1)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListLinkableTemplatesRequest{
			Namespace: projectScopeRef(project),
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
		handler := newLinkableTestHandler(t, fakeClient, shareUsers, walker)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListLinkableTemplatesRequest{
			Namespace: projectScopeRef(project),
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

	// HOL-565 removed the `console.holos.run/mandatory` annotation reader.
	// `Forced` is always false in the ListLinkableTemplates response because
	// this path does not evaluate TemplatePolicy REQUIRE rules per candidate
	// (render-time resolution is the authoritative source). The previous
	// `forced=true when template has mandatory annotation` assertion is
	// therefore intentionally gone — the dual of it below is kept so
	// regressions that re-populate `forced` from anything but REQUIRE rules
	// show up as a test failure.
	t.Run("forced=false after mandatory annotation reader removal", func(t *testing.T) {
		orgNsObj := orgNS(org)
		projectNsObj := projectNS(project)
		// Pass true for the (now-ignored) mandatory parameter to lock in the
		// behavior: even if the caller would have requested a "mandatory"
		// template, `forced` stays false because the annotation is gone.
		tmpl := enabledTemplateCM("org-"+org, templateName, "HTTPRoute", "Expose via gateway", true)

		fakeClient := fake.NewClientset(orgNsObj, projectNsObj, tmpl)
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{projectNsObj, orgNsObj},
		}
		handler := newLinkableTestHandler(t, fakeClient, shareUsers, walker)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListLinkableTemplatesRequest{
			Namespace: projectScopeRef(project),
		})

		resp, err := handler.ListLinkableTemplates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(resp.Msg.Templates) != 1 {
			t.Fatalf("expected 1 linkable template, got %d", len(resp.Msg.Templates))
		}
		lt := resp.Msg.Templates[0]
		if lt.Forced {
			t.Errorf("expected forced=false after HOL-565, got true (did a REQUIRE resolver accidentally wire through?)")
		}
	})

	t.Run("forced=false when template has no mandatory annotation", func(t *testing.T) {
		orgNsObj := orgNS(org)
		projectNsObj := projectNS(project)
		tmpl := enabledTemplateCM("org-"+org, templateName, "HTTPRoute", "Expose via gateway", false)

		fakeClient := fake.NewClientset(orgNsObj, projectNsObj, tmpl)
		walker := &stubAncestorWalker{
			ancestors: []*corev1.Namespace{projectNsObj, orgNsObj},
		}
		handler := newLinkableTestHandler(t, fakeClient, shareUsers, walker)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListLinkableTemplatesRequest{
			Namespace: projectScopeRef(project),
		})

		resp, err := handler.ListLinkableTemplates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(resp.Msg.Templates) != 1 {
			t.Fatalf("expected 1 linkable template, got %d", len(resp.Msg.Templates))
		}
		lt := resp.Msg.Templates[0]
		if lt.Forced {
			t.Errorf("expected forced=false for non-mandatory template, got true")
		}
	})
}

// TestListLinkableTemplatesIncludeSelfScope exercises HOL-561: the
// `include_self_scope` request flag toggles whether templates at the request's
// own scope are returned alongside ancestor-scope templates. The TemplatePolicy
// editor sets it to true so org-scope policies (no ancestors) and folder-scope
// policies can pick same-scope templates. All existing call sites leave it at
// the default false, which preserves the ancestor-only semantics required by
// the project-template linking UI.
func TestListLinkableTemplatesIncludeSelfScope(t *testing.T) {
	const org = "acme"
	const folder = "platform"
	const ownerEmail = "platform@localhost"

	orgUsers := map[string]string{ownerEmail: "owner"}
	folderUsers := map[string]string{ownerEmail: "owner"}

	// Build a hierarchy with one org-scope template and one folder-scope
	// template. A folder is a child of the org.
	orgNsObj := orgNS(org)
	folderNsObj := folderNS(folder)

	orgTemplate := enabledTemplateCMForScope("org-"+org, "org-httproute", "OrgHTTPRoute", "org-owned", scopeshim.ScopeOrganization)
	folderTemplate := enabledTemplateCMForScope("fld-"+folder, "folder-gateway", "FolderGateway", "folder-owned", scopeshim.ScopeFolder)

	// Build a fresh handler per subtest so fakeClient state is isolated.
	makeHandler := func(scope scopeshim.Scope, ancestors []*corev1.Namespace) *Handler {
		fakeClient := fake.NewClientset(orgNsObj, folderNsObj, orgTemplate, folderTemplate)
		r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
		k8s := newTestK8sClient(t, fakeClient, r)
		handler := NewHandler(k8s, r, &stubRenderer{}, policyresolver.NewNoopResolver())
		handler.WithAncestorWalker(&stubAncestorWalker{ancestors: ancestors})
		// Wire whichever grant resolver matches the request scope so
		// checkAccess passes.
		switch scope {
		case scopeshim.ScopeOrganization:
			handler.WithOrgGrantResolver(&stubOrgGrantResolver{users: orgUsers})
		case scopeshim.ScopeFolder:
			handler.WithFolderGrantResolver(&stubFolderGrantResolver{users: folderUsers})
		}
		return handler
	}

	type want struct {
		// names is the set of expected template names (order-insensitive since
		// the handler concatenates results across namespaces).
		names map[string]bool
	}

	tests := []struct {
		description      string
		scope            string
		requestScope     scopeshim.Scope
		ancestors        []*corev1.Namespace
		includeSelfScope bool
		want             want
	}{
		{
			description:      "org scope with include_self_scope=false returns empty (no ancestors)",
			scope:            orgScopeRef(org),
			requestScope:     scopeshim.ScopeOrganization,
			ancestors:        []*corev1.Namespace{orgNsObj},
			includeSelfScope: false,
			want: want{
				names: map[string]bool{},
			},
		},
		{
			description:      "org scope with include_self_scope=true returns org templates",
			scope:            orgScopeRef(org),
			requestScope:     scopeshim.ScopeOrganization,
			ancestors:        []*corev1.Namespace{orgNsObj},
			includeSelfScope: true,
			want: want{
				names: map[string]bool{"org-httproute": true},
			},
		},
		{
			description:      "folder scope with include_self_scope=false returns only ancestor (org) templates",
			scope:            folderScopeRef(folder),
			requestScope:     scopeshim.ScopeFolder,
			ancestors:        []*corev1.Namespace{folderNsObj, orgNsObj},
			includeSelfScope: false,
			want: want{
				names: map[string]bool{"org-httproute": true},
			},
		},
		{
			description:      "folder scope with include_self_scope=true returns both folder and org templates",
			scope:            folderScopeRef(folder),
			requestScope:     scopeshim.ScopeFolder,
			ancestors:        []*corev1.Namespace{folderNsObj, orgNsObj},
			includeSelfScope: true,
			want: want{
				names: map[string]bool{"folder-gateway": true, "org-httproute": true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			handler := makeHandler(tt.requestScope, tt.ancestors)

			ctx := authedCtx(ownerEmail, nil)
			req := connect.NewRequest(&consolev1.ListLinkableTemplatesRequest{
				Namespace:            tt.scope,
				IncludeSelfScope: tt.includeSelfScope,
			})

			resp, err := handler.ListLinkableTemplates(ctx, req)
			if err != nil {
				t.Fatalf("ListLinkableTemplates returned unexpected error: %v", err)
			}

			got := make(map[string]bool, len(resp.Msg.Templates))
			for _, lt := range resp.Msg.Templates {
				got[lt.Name] = true
			}
			if len(got) != len(tt.want.names) {
				t.Fatalf("template count mismatch: want %d (%v), got %d (%v)",
					len(tt.want.names), tt.want.names, len(got), got)
			}
			for name := range tt.want.names {
				if !got[name] {
					t.Errorf("expected template %q in response, got names %v", name, got)
				}
			}
		})
	}
}
