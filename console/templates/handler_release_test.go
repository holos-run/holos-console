package templates

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"k8s.io/client-go/kubernetes/fake"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/holos-run/holos-console/console/policyresolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// newOrgTestHandler builds a Handler wired to a fake K8s client with org grant
// resolver for release tests. The grant resolver maps emails to roles. Extra
// CRD seed objects (typically TemplateRelease fixtures after HOL-693) flow into
// the fake controller-runtime client that backs the release CRUD path.
func newOrgTestHandler(t *testing.T, fakeClient *fake.Clientset, shareUsers map[string]string, extra ...ctrlclient.Object) *Handler {
	t.Helper()
	r := testResolver
	k8s := newTestK8sClient(t, fakeClient, r, extra...)
	handler := NewHandler(k8s, r, &stubRenderer{}, policyresolver.NewNoopResolver())
	handler.WithOrgGrantResolver(&stubOrgGrantResolver{users: shareUsers})
	return handler
}

// stubOrgGrantResolver implements OrgGrantResolver for tests.
type stubOrgGrantResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubOrgGrantResolver) GetOrgGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	return s.users, s.roles, s.err
}

// orgScopeRef returns the Kubernetes namespace string for the named
// organization scope. HOL-619 collapsed the TemplateScopeRef enum and
// HOL-723 retired scopeshim, so the helper now emits a namespace string
// produced by the package-level testResolver; the handler classifies it
// back via resolver.ResourceTypeFromNamespace.
func orgScopeRef(org string) string {
	return testResolver.OrgNamespace(org)
}

func TestListReleases(t *testing.T) {
	const org = "acme"
	const ownerEmail = "platform@localhost"
	const templateName = "my-template"

	shareUsers := map[string]string{ownerEmail: "owner"}

	t.Run("returns releases sorted by version descending", func(t *testing.T) {
		ns := orgNS(org)
		r1 := makeReleaseCRD("org-"+org, templateName, "1.0.0")
		r2 := makeReleaseCRD("org-"+org, templateName, "2.0.0")
		r3 := makeReleaseCRD("org-"+org, templateName, "1.5.0")
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(t, fakeClient, shareUsers, r1, r2, r3)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListReleasesRequest{
			Namespace:    orgScopeRef(org),
			TemplateName: templateName,
		})

		resp, err := handler.ListReleases(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		releases := resp.Msg.Releases
		if len(releases) != 3 {
			t.Fatalf("expected 3 releases, got %d", len(releases))
		}
		// Should be sorted descending: 2.0.0, 1.5.0, 1.0.0
		expectedVersions := []string{"2.0.0", "1.5.0", "1.0.0"}
		for i, r := range releases {
			if r.Version != expectedVersions[i] {
				t.Errorf("release %d: expected version %q, got %q", i, expectedVersions[i], r.Version)
			}
		}
	})

	t.Run("returns empty list when no releases exist", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListReleasesRequest{
			Namespace:    orgScopeRef(org),
			TemplateName: templateName,
		})

		resp, err := handler.ListReleases(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.Releases) != 0 {
			t.Errorf("expected 0 releases, got %d", len(resp.Msg.Releases))
		}
	})

	t.Run("rejects missing template_name", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListReleasesRequest{
			Namespace: orgScopeRef(org),
		})

		_, err := handler.ListReleases(ctx, req)
		if err == nil {
			t.Fatal("expected error for missing template_name")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected code InvalidArgument, got %v", connect.CodeOf(err))
		}
	})
}

func TestGetRelease(t *testing.T) {
	const org = "acme"
	const ownerEmail = "platform@localhost"
	const templateName = "my-template"

	shareUsers := map[string]string{ownerEmail: "owner"}

	t.Run("returns existing release", func(t *testing.T) {
		ns := orgNS(org)
		// Seed a TemplateRelease CRD with changelog / upgrade advice so we
		// can assert the proto conversion propagates spec.changelog and
		// spec.upgradeAdvice.
		rel := makeReleaseCRD("org-"+org, templateName, "1.2.3")
		rel.Spec.Changelog = "Bug fixes"
		rel.Spec.UpgradeAdvice = "No breaking changes"
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(t, fakeClient, shareUsers, rel)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.GetReleaseRequest{
			Namespace:    orgScopeRef(org),
			TemplateName: templateName,
			Version:      "1.2.3",
		})

		resp, err := handler.GetRelease(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		r := resp.Msg.Release
		if r.Version != "1.2.3" {
			t.Errorf("expected version 1.2.3, got %q", r.Version)
		}
		if r.TemplateName != templateName {
			t.Errorf("expected template_name %q, got %q", templateName, r.TemplateName)
		}
		if r.Changelog != "Bug fixes" {
			t.Errorf("expected changelog 'Bug fixes', got %q", r.Changelog)
		}
		if r.UpgradeAdvice != "No breaking changes" {
			t.Errorf("expected upgrade_advice 'No breaking changes', got %q", r.UpgradeAdvice)
		}
		if r.CueTemplate != validCue {
			t.Errorf("expected CUE template to match")
		}
	})

	t.Run("returns NotFound for nonexistent version", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.GetReleaseRequest{
			Namespace:    orgScopeRef(org),
			TemplateName: templateName,
			Version:      "9.9.9",
		})

		_, err := handler.GetRelease(ctx, req)
		if err == nil {
			t.Fatal("expected NotFound error")
		}
		if connect.CodeOf(err) != connect.CodeNotFound {
			t.Errorf("expected code NotFound, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects invalid version format", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.GetReleaseRequest{
			Namespace:    orgScopeRef(org),
			TemplateName: templateName,
			Version:      "bad",
		})

		_, err := handler.GetRelease(ctx, req)
		if err == nil {
			t.Fatal("expected InvalidArgument error")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected code InvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects missing version", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.GetReleaseRequest{
			Namespace:    orgScopeRef(org),
			TemplateName: templateName,
		})

		_, err := handler.GetRelease(ctx, req)
		if err == nil {
			t.Fatal("expected error for missing version")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected code InvalidArgument, got %v", connect.CodeOf(err))
		}
	})
}
