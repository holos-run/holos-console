package templates

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// newOrgTestHandler builds a Handler wired to a fake K8s client with org grant
// resolver for release tests. The grant resolver maps emails to roles.
func newOrgTestHandler(fakeClient *fake.Clientset, shareUsers map[string]string) *Handler {
	r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
	k8s := NewK8sClient(fakeClient, r)
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

func orgScopeRef(org string) *consolev1.TemplateScopeRef {
	return &consolev1.TemplateScopeRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
		ScopeName: org,
	}
}

func TestCreateRelease(t *testing.T) {
	const org = "acme"
	const ownerEmail = "platform@localhost"
	const templateName = "my-template"

	shareUsers := map[string]string{
		ownerEmail: "owner",
	}

	t.Run("creates first release successfully", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Scope: orgScopeRef(org),
			Release: &consolev1.Release{
				TemplateName:  templateName,
				Version:       "1.0.0",
				CueTemplate:   validCue,
				Changelog:     "Initial release",
				UpgradeAdvice: "",
			},
		})

		resp, err := handler.CreateRelease(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Release == nil {
			t.Fatal("expected non-nil release in response")
		}
		if resp.Msg.Release.Version != "1.0.0" {
			t.Errorf("expected version 1.0.0, got %q", resp.Msg.Release.Version)
		}
		if resp.Msg.Release.TemplateName != templateName {
			t.Errorf("expected template_name %q, got %q", templateName, resp.Msg.Release.TemplateName)
		}
		if resp.Msg.Release.CueTemplate != validCue {
			t.Errorf("expected CUE template to match")
		}
		if resp.Msg.Release.Changelog != "Initial release" {
			t.Errorf("expected changelog 'Initial release', got %q", resp.Msg.Release.Changelog)
		}

		// Verify ConfigMap was persisted with correct labels.
		cm, err := fakeClient.CoreV1().ConfigMaps("org-"+org).Get(context.Background(), "my-template--v1-0-0", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected release ConfigMap to exist, got %v", err)
		}
		if cm.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeTemplateRelease {
			t.Errorf("expected resource-type %q, got %q", v1alpha2.ResourceTypeTemplateRelease, cm.Labels[v1alpha2.LabelResourceType])
		}
		if cm.Labels[v1alpha2.LabelReleaseOf] != templateName {
			t.Errorf("expected release-of %q, got %q", templateName, cm.Labels[v1alpha2.LabelReleaseOf])
		}
		if cm.Annotations[v1alpha2.AnnotationTemplateVersion] != "1.0.0" {
			t.Errorf("expected version annotation 1.0.0, got %q", cm.Annotations[v1alpha2.AnnotationTemplateVersion])
		}
		if cm.Immutable == nil || !*cm.Immutable {
			t.Error("expected ConfigMap to be immutable")
		}
	})

	t.Run("rejects prerelease version", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Scope: orgScopeRef(org),
			Release: &consolev1.Release{
				TemplateName: templateName,
				Version:      "1.0.0-beta.1",
				CueTemplate:  validCue,
			},
		})

		_, err := handler.CreateRelease(ctx, req)
		if err == nil {
			t.Fatal("expected error for prerelease version, got nil")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected code InvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects build metadata version", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Scope: orgScopeRef(org),
			Release: &consolev1.Release{
				TemplateName: templateName,
				Version:      "1.0.0+build.123",
				CueTemplate:  validCue,
			},
		})

		_, err := handler.CreateRelease(ctx, req)
		if err == nil {
			t.Fatal("expected error for build metadata version, got nil")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected code InvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects duplicate version", func(t *testing.T) {
		ns := orgNS(org)
		// Pre-seed a release ConfigMap.
		existingRelease := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-template--v1-0-0",
				Namespace: "org-" + org,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplateRelease,
					v1alpha2.LabelReleaseOf:     templateName,
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationTemplateVersion: "1.0.0",
				},
			},
			Data: map[string]string{
				CueTemplateKey: validCue,
			},
		}
		fakeClient := fake.NewClientset(ns, existingRelease)
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Scope: orgScopeRef(org),
			Release: &consolev1.Release{
				TemplateName: templateName,
				Version:      "1.0.0",
				CueTemplate:  validCue,
			},
		})

		_, err := handler.CreateRelease(ctx, req)
		if err == nil {
			t.Fatal("expected AlreadyExists error, got nil")
		}
		if connect.CodeOf(err) != connect.CodeAlreadyExists {
			t.Errorf("expected code AlreadyExists, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects invalid semver version", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Scope: orgScopeRef(org),
			Release: &consolev1.Release{
				TemplateName: templateName,
				Version:      "not-a-version",
				CueTemplate:  validCue,
			},
		})

		_, err := handler.CreateRelease(ctx, req)
		if err == nil {
			t.Fatal("expected error for invalid version, got nil")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected code InvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects missing template_name", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Scope: orgScopeRef(org),
			Release: &consolev1.Release{
				Version:     "1.0.0",
				CueTemplate: validCue,
			},
		})

		_, err := handler.CreateRelease(ctx, req)
		if err == nil {
			t.Fatal("expected error for missing template_name")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected code InvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("rejects missing version", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Scope: orgScopeRef(org),
			Release: &consolev1.Release{
				TemplateName: templateName,
				CueTemplate:  validCue,
			},
		})

		_, err := handler.CreateRelease(ctx, req)
		if err == nil {
			t.Fatal("expected error for missing version")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected code InvalidArgument, got %v", connect.CodeOf(err))
		}
	})

	t.Run("VIEWER denied by PERMISSION_TEMPLATES_WRITE", func(t *testing.T) {
		const viewerEmail = "sre@localhost"
		ns := orgNS(org)
		viewerShareUsers := map[string]string{
			ownerEmail:  "owner",
			viewerEmail: "viewer",
		}
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(fakeClient, viewerShareUsers)

		ctx := authedCtx(viewerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Scope: orgScopeRef(org),
			Release: &consolev1.Release{
				TemplateName: templateName,
				Version:      "1.0.0",
				CueTemplate:  validCue,
			},
		})

		_, err := handler.CreateRelease(ctx, req)
		if err == nil {
			t.Fatal("expected PermissionDenied error")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected code PermissionDenied, got %v", connect.CodeOf(err))
		}
	})

	t.Run("creates release with defaults", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		defaults := &consolev1.TemplateDefaults{
			Image: "ghcr.io/example/app",
			Tag:   "1.0.0",
		}
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Scope: orgScopeRef(org),
			Release: &consolev1.Release{
				TemplateName: templateName,
				Version:      "1.0.0",
				CueTemplate:  validCue,
				Defaults:     defaults,
			},
		})

		resp, err := handler.CreateRelease(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Release.Defaults == nil {
			t.Fatal("expected non-nil defaults in response")
		}
		if resp.Msg.Release.Defaults.Image != "ghcr.io/example/app" {
			t.Errorf("expected image 'ghcr.io/example/app', got %q", resp.Msg.Release.Defaults.Image)
		}
	})

	t.Run("unauthenticated request rejected", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := context.Background() // no claims
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Scope: orgScopeRef(org),
			Release: &consolev1.Release{
				TemplateName: templateName,
				Version:      "1.0.0",
				CueTemplate:  validCue,
			},
		})

		_, err := handler.CreateRelease(ctx, req)
		if err == nil {
			t.Fatal("expected Unauthenticated error")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("expected code Unauthenticated, got %v", connect.CodeOf(err))
		}
	})
}

func TestListReleases(t *testing.T) {
	const org = "acme"
	const ownerEmail = "platform@localhost"
	const templateName = "my-template"

	shareUsers := map[string]string{ownerEmail: "owner"}

	makeReleaseCM := func(version string) *corev1.ConfigMap {
		v, _ := ParseVersion(version)
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ReleaseConfigMapName(templateName, v),
				Namespace: "org-" + org,
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
			Data: map[string]string{
				CueTemplateKey: validCue,
			},
		}
	}

	t.Run("returns releases sorted by version descending", func(t *testing.T) {
		ns := orgNS(org)
		r1 := makeReleaseCM("1.0.0")
		r2 := makeReleaseCM("2.0.0")
		r3 := makeReleaseCM("1.5.0")
		fakeClient := fake.NewClientset(ns, r1, r2, r3)
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListReleasesRequest{
			Scope:        orgScopeRef(org),
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
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListReleasesRequest{
			Scope:        orgScopeRef(org),
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
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.ListReleasesRequest{
			Scope: orgScopeRef(org),
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
		release := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-template--v1-2-3",
				Namespace: "org-" + org,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplateRelease,
					v1alpha2.LabelReleaseOf:     templateName,
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationTemplateVersion: "1.2.3",
				},
			},
			Data: map[string]string{
				CueTemplateKey:            validCue,
				v1alpha2.ChangelogKey:     "Bug fixes",
				v1alpha2.UpgradeAdviceKey: "No breaking changes",
			},
		}
		fakeClient := fake.NewClientset(ns, release)
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.GetReleaseRequest{
			Scope:        orgScopeRef(org),
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
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.GetReleaseRequest{
			Scope:        orgScopeRef(org),
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
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.GetReleaseRequest{
			Scope:        orgScopeRef(org),
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
		handler := newOrgTestHandler(fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.GetReleaseRequest{
			Scope:        orgScopeRef(org),
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
