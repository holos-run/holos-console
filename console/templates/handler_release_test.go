package templates

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"k8s.io/client-go/kubernetes/fake"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// newOrgTestHandler builds a Handler wired to a fake K8s client with org grant
// resolver for release tests. The grant resolver maps emails to roles. Extra
// CRD seed objects (typically TemplateRelease fixtures after HOL-693) flow into
// the fake controller-runtime client that backs the release CRUD path.
func newOrgTestHandler(t *testing.T, fakeClient *fake.Clientset, shareUsers map[string]string, extra ...ctrlclient.Object) *Handler {
	t.Helper()
	r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
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
// organization scope. HOL-619 collapsed the TemplateScopeRef enum so the
// helper now emits a namespace string the handler will classify via the
// scopeshim resolver.
func orgScopeRef(org string) string {
	return scopeshim.DefaultResolver().OrgNamespace(org)
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
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Namespace: orgScopeRef(org),
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
	})

	t.Run("rejects prerelease version", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Namespace: orgScopeRef(org),
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
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Namespace: orgScopeRef(org),
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
		fakeClient := fake.NewClientset(ns)
		// Pre-seed a TemplateRelease CRD; CreateRelease should surface
		// AlreadyExists from the apiserver when the deterministic object
		// name collides.
		existing := makeReleaseCRD("org-"+org, templateName, "1.0.0")
		handler := newOrgTestHandler(t, fakeClient, shareUsers, existing)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Namespace: orgScopeRef(org),
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
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Namespace: orgScopeRef(org),
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
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Namespace: orgScopeRef(org),
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
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Namespace: orgScopeRef(org),
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
		handler := newOrgTestHandler(t, fakeClient, viewerShareUsers)

		ctx := authedCtx(viewerEmail, nil)
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Namespace: orgScopeRef(org),
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
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		defaults := &consolev1.TemplateDefaults{
			Image: "ghcr.io/example/app",
			Tag:   "1.0.0",
		}
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Namespace: orgScopeRef(org),
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

	t.Run("preserves env secret_key_ref and config_map_key_ref through round-trip", func(t *testing.T) {
		// Regression guard for HOL-693: the retired release ConfigMap
		// stored the proto TemplateDefaults as JSON verbatim. The CRD
		// replacement must preserve the same fidelity; a literal-only
		// round-trip would silently drop env entries that select a
		// SecretKeyRef or ConfigMapKeyRef.
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		defaults := &consolev1.TemplateDefaults{
			Image: "ghcr.io/example/app",
			Env: []*consolev1.EnvVar{
				{
					Name:   "PLAIN",
					Source: &consolev1.EnvVar_Value{Value: "literal"},
				},
				{
					Name: "DB_PASSWORD",
					Source: &consolev1.EnvVar_SecretKeyRef{
						SecretKeyRef: &consolev1.SecretKeyRef{
							Name: "db-credentials",
							Key:  "password",
						},
					},
				},
				{
					Name: "FEATURE_FLAGS",
					Source: &consolev1.EnvVar_ConfigMapKeyRef{
						ConfigMapKeyRef: &consolev1.ConfigMapKeyRef{
							Name: "feature-flags",
							Key:  "json",
						},
					},
				},
			},
		}

		createReq := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Namespace: orgScopeRef(org),
			Release: &consolev1.Release{
				TemplateName: templateName,
				Version:      "1.2.3",
				CueTemplate:  validCue,
				Defaults:     defaults,
			},
		})
		createResp, err := handler.CreateRelease(ctx, createReq)
		if err != nil {
			t.Fatalf("CreateRelease returned unexpected error: %v", err)
		}
		assertEnvRoundTripped(t, createResp.Msg.Release.GetDefaults())

		getResp, err := handler.GetRelease(ctx, connect.NewRequest(&consolev1.GetReleaseRequest{
			Namespace:    orgScopeRef(org),
			TemplateName: templateName,
			Version:      "1.2.3",
		}))
		if err != nil {
			t.Fatalf("GetRelease returned unexpected error: %v", err)
		}
		assertEnvRoundTripped(t, getResp.Msg.Release.GetDefaults())
	})

	t.Run("unauthenticated request rejected", func(t *testing.T) {
		ns := orgNS(org)
		fakeClient := fake.NewClientset(ns)
		handler := newOrgTestHandler(t, fakeClient, shareUsers)

		ctx := context.Background() // no claims
		req := connect.NewRequest(&consolev1.CreateReleaseRequest{
			Namespace: orgScopeRef(org),
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

// assertEnvRoundTripped verifies the TemplateDefaults produced by
// CreateRelease / GetRelease preserved the three env-var variants used by
// the HOL-693 regression guard: a literal value, a SecretKeyRef, and a
// ConfigMapKeyRef.
func assertEnvRoundTripped(t *testing.T, got *consolev1.TemplateDefaults) {
	t.Helper()
	if got == nil {
		t.Fatal("expected non-nil defaults in response")
	}
	if len(got.GetEnv()) != 3 {
		t.Fatalf("expected 3 env entries, got %d", len(got.GetEnv()))
	}
	if v := got.GetEnv()[0].GetValue(); v != "literal" {
		t.Errorf("PLAIN: expected literal %q, got %q", "literal", v)
	}
	secretRef := got.GetEnv()[1].GetSecretKeyRef()
	if secretRef == nil {
		t.Fatal("DB_PASSWORD: expected SecretKeyRef source, got nil (regression: env ref dropped)")
	}
	if secretRef.GetName() != "db-credentials" || secretRef.GetKey() != "password" {
		t.Errorf("DB_PASSWORD: unexpected SecretKeyRef: %+v", secretRef)
	}
	cmRef := got.GetEnv()[2].GetConfigMapKeyRef()
	if cmRef == nil {
		t.Fatal("FEATURE_FLAGS: expected ConfigMapKeyRef source, got nil (regression: env ref dropped)")
	}
	if cmRef.GetName() != "feature-flags" || cmRef.GetKey() != "json" {
		t.Errorf("FEATURE_FLAGS: unexpected ConfigMapKeyRef: %+v", cmRef)
	}
}
