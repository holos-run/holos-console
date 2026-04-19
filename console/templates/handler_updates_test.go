package templates

import (
	"encoding/json"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// makeReleaseCMInNS creates a release ConfigMap in the given namespace.
func makeReleaseCMInNS(ns, templateName, version string) *corev1.ConfigMap {
	v, _ := ParseVersion(version)
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
		Data: map[string]string{
			CueTemplateKey: validCue,
		},
	}
}

// makeTemplateWithLinks creates a template ConfigMap with linked template refs.
func makeTemplateWithLinks(ns, name string, links []*consolev1.LinkedTemplateRef) *corev1.ConfigMap {
	type storedRef struct {
		Scope             string `json:"scope"`
		ScopeName         string `json:"scope_name"`
		Name              string `json:"name"`
		VersionConstraint string `json:"version_constraint,omitempty"`
	}
	stored := make([]storedRef, 0, len(links))
	for _, ref := range links {
		// HOL-619: LinkedTemplateRef.Scope / ScopeName were removed; the
		// shim classifies the ref's namespace back to the legacy pair
		// via the package-level default resolver installed in TestMain.
		stored = append(stored, storedRef{
			Scope:             scopeLabelValue(scopeshim.RefScope(ref)),
			ScopeName:         scopeshim.RefScopeName(ref),
			Name:              ref.GetName(),
			VersionConstraint: ref.GetVersionConstraint(),
		})
	}
	linkedJSON, _ := json.Marshal(stored)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:     name,
				v1alpha2.AnnotationDescription:     "test template",
				v1alpha2.AnnotationEnabled:         "true",
				v1alpha2.AnnotationLinkedTemplates: string(linkedJSON),
			},
		},
		Data: map[string]string{
			CueTemplateKey: validCue,
		},
	}
}

func TestCheckUpdates(t *testing.T) {
	const org = "acme"
	const project = "my-project"
	const ownerEmail = "platform@localhost"
	const linkedTemplateName = "httproute"

	shareUsers := map[string]string{ownerEmail: "owner"}

	t.Run("no updates when no releases exist", func(t *testing.T) {
		projectNs := projectNS(project)
		tmpl := makeTemplateWithLinks("prj-"+project, "web-app", []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, org, linkedTemplateName, ""),
		})
		fakeClient := fake.NewClientset(projectNs, orgNS(org), tmpl)
		handler := newTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CheckUpdatesRequest{
			Namespace:        projectScopeRef(project),
			TemplateName: "web-app",
		})

		resp, err := handler.CheckUpdates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.Updates) != 0 {
			t.Errorf("expected 0 updates, got %d", len(resp.Msg.Updates))
		}
	})

	t.Run("no compatible update when already on latest matching", func(t *testing.T) {
		projectNs := projectNS(project)
		tmpl := makeTemplateWithLinks("prj-"+project, "web-app", []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, org, linkedTemplateName, ">=1.0.0 <2.0.0"),
		})
		// Create releases: 1.0.0 and 1.1.0
		r1 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.0.0")
		r2 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.1.0")
		fakeClient := fake.NewClientset(projectNs, orgNS(org), tmpl, r1, r2)
		handler := newTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CheckUpdatesRequest{
			Namespace:        projectScopeRef(project),
			TemplateName: "web-app",
		})

		resp, err := handler.CheckUpdates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// With constraint >=1.0.0 <2.0.0 and releases 1.0.0 + 1.1.0,
		// the current version (latest matching) is 1.1.0, which equals the
		// latest compatible version. No update should be reported because
		// the resolver already picks the highest matching release.
		if len(resp.Msg.Updates) != 0 {
			t.Fatalf("expected 0 updates (already on latest compatible), got %d", len(resp.Msg.Updates))
		}
	})

	t.Run("breaking update available", func(t *testing.T) {
		projectNs := projectNS(project)
		tmpl := makeTemplateWithLinks("prj-"+project, "web-app", []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, org, linkedTemplateName, ">=1.0.0 <2.0.0"),
		})
		// Create releases: 1.0.0, 1.5.0, 2.0.0
		r1 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.0.0")
		r2 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.5.0")
		r3 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "2.0.0")
		fakeClient := fake.NewClientset(projectNs, orgNS(org), tmpl, r1, r2, r3)
		handler := newTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CheckUpdatesRequest{
			Namespace:        projectScopeRef(project),
			TemplateName: "web-app",
		})

		resp, err := handler.CheckUpdates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.Updates) != 1 {
			t.Fatalf("expected 1 update, got %d", len(resp.Msg.Updates))
		}
		update := resp.Msg.Updates[0]
		if !update.BreakingUpdateAvailable {
			t.Error("expected breaking_update_available=true")
		}
		if update.LatestVersion != "2.0.0" {
			t.Errorf("expected latest_version 2.0.0, got %q", update.LatestVersion)
		}
		if update.LatestCompatibleVersion != "1.5.0" {
			t.Errorf("expected latest_compatible_version 1.5.0, got %q", update.LatestCompatibleVersion)
		}
		if update.CurrentVersion != "1.5.0" {
			t.Errorf("expected current_version 1.5.0, got %q", update.CurrentVersion)
		}
	})

	t.Run("no constraint no update when already on latest", func(t *testing.T) {
		projectNs := projectNS(project)
		tmpl := makeTemplateWithLinks("prj-"+project, "web-app", []*consolev1.LinkedTemplateRef{
			// No version constraint
			scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, org, linkedTemplateName, ""),
		})
		// Create releases: 1.0.0, 2.0.0
		r1 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.0.0")
		r2 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "2.0.0")
		fakeClient := fake.NewClientset(projectNs, orgNS(org), tmpl, r1, r2)
		handler := newTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CheckUpdatesRequest{
			Namespace:        projectScopeRef(project),
			TemplateName: "web-app",
		})

		resp, err := handler.CheckUpdates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// No constraint: current (latest matching) = 2.0.0, latest compatible
		// = 2.0.0. No update is reported because the resolver already picks
		// the highest matching release.
		if len(resp.Msg.Updates) != 0 {
			t.Fatalf("expected 0 updates (already on latest), got %d", len(resp.Msg.Updates))
		}
	})

	t.Run("no constraint single release means no update", func(t *testing.T) {
		projectNs := projectNS(project)
		tmpl := makeTemplateWithLinks("prj-"+project, "web-app", []*consolev1.LinkedTemplateRef{
			// No version constraint
			scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, org, linkedTemplateName, ""),
		})
		// Single release: current == latest, no update.
		r1 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.0.0")
		fakeClient := fake.NewClientset(projectNs, orgNS(org), tmpl, r1)
		handler := newTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CheckUpdatesRequest{
			Namespace:        projectScopeRef(project),
			TemplateName: "web-app",
		})

		resp, err := handler.CheckUpdates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.Updates) != 0 {
			t.Errorf("expected 0 updates when single release, got %d", len(resp.Msg.Updates))
		}
	})

	t.Run("no linked templates means no updates", func(t *testing.T) {
		projectNs := projectNS(project)
		// Template with no linked templates.
		tmpl := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "standalone",
				Namespace: "prj-" + project,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationDisplayName: "standalone",
					v1alpha2.AnnotationDescription: "no links",
					v1alpha2.AnnotationEnabled:     "true",
				},
			},
			Data: map[string]string{CueTemplateKey: validCue},
		}
		fakeClient := fake.NewClientset(projectNs, tmpl)
		handler := newTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CheckUpdatesRequest{
			Namespace:        projectScopeRef(project),
			TemplateName: "standalone",
		})

		resp, err := handler.CheckUpdates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.Updates) != 0 {
			t.Errorf("expected 0 updates, got %d", len(resp.Msg.Updates))
		}
	})

	t.Run("checks all templates when template_name omitted", func(t *testing.T) {
		projectNs := projectNS(project)
		tmpl1 := makeTemplateWithLinks("prj-"+project, "web-app", []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, org, linkedTemplateName, ">=1.0.0 <2.0.0"),
		})
		tmpl2 := makeTemplateWithLinks("prj-"+project, "api-svc", []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, org, "gateway", ">=1.0.0 <2.0.0"),
		})
		// httproute: has breaking update (2.0.0)
		r1 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.0.0")
		r2 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "2.0.0")
		// gateway: has no updates
		r3 := makeReleaseCMInNS("org-"+org, "gateway", "1.0.0")
		fakeClient := fake.NewClientset(projectNs, orgNS(org), tmpl1, tmpl2, r1, r2, r3)
		handler := newTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CheckUpdatesRequest{
			Namespace: projectScopeRef(project),
			// template_name omitted -- check all
		})

		resp, err := handler.CheckUpdates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Only httproute should have an update (breaking).
		if len(resp.Msg.Updates) != 1 {
			t.Fatalf("expected 1 update, got %d", len(resp.Msg.Updates))
		}
		if resp.Msg.Updates[0].Ref.Name != linkedTemplateName {
			t.Errorf("expected update for %q, got %q", linkedTemplateName, resp.Msg.Updates[0].Ref.Name)
		}
	})

	t.Run("include_current returns entries for up-to-date linked templates", func(t *testing.T) {
		projectNs := projectNS(project)
		tmpl := makeTemplateWithLinks("prj-"+project, "web-app", []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, org, linkedTemplateName, ">=1.0.0 <2.0.0"),
		})
		// Releases: 1.0.0 and 1.1.0 — already on latest compatible.
		r1 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.0.0")
		r2 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.1.0")
		fakeClient := fake.NewClientset(projectNs, orgNS(org), tmpl, r1, r2)
		handler := newTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CheckUpdatesRequest{
			Namespace:          projectScopeRef(project),
			TemplateName:   "web-app",
			IncludeCurrent: true,
		})

		resp, err := handler.CheckUpdates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// include_current=true should return 1 entry even though already up-to-date.
		if len(resp.Msg.Updates) != 1 {
			t.Fatalf("expected 1 update (include_current=true), got %d", len(resp.Msg.Updates))
		}
		update := resp.Msg.Updates[0]
		if update.CurrentVersion != "1.1.0" {
			t.Errorf("expected current_version 1.1.0, got %q", update.CurrentVersion)
		}
		if update.LatestCompatibleVersion != "1.1.0" {
			t.Errorf("expected latest_compatible_version 1.1.0, got %q", update.LatestCompatibleVersion)
		}
		if update.BreakingUpdateAvailable {
			t.Error("expected breaking_update_available=false")
		}
	})

	t.Run("include_current false preserves existing behavior", func(t *testing.T) {
		projectNs := projectNS(project)
		tmpl := makeTemplateWithLinks("prj-"+project, "web-app", []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, org, linkedTemplateName, ">=1.0.0 <2.0.0"),
		})
		// Releases: 1.0.0 and 1.1.0 — already on latest compatible.
		r1 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.0.0")
		r2 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.1.0")
		fakeClient := fake.NewClientset(projectNs, orgNS(org), tmpl, r1, r2)
		handler := newTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CheckUpdatesRequest{
			Namespace:          projectScopeRef(project),
			TemplateName:   "web-app",
			IncludeCurrent: false,
		})

		resp, err := handler.CheckUpdates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// include_current=false (default): no updates when already on latest.
		if len(resp.Msg.Updates) != 0 {
			t.Fatalf("expected 0 updates (include_current=false), got %d", len(resp.Msg.Updates))
		}
	})

	t.Run("include_current with no releases returns no entries", func(t *testing.T) {
		projectNs := projectNS(project)
		tmpl := makeTemplateWithLinks("prj-"+project, "web-app", []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, org, linkedTemplateName, ""),
		})
		fakeClient := fake.NewClientset(projectNs, orgNS(org), tmpl)
		handler := newTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CheckUpdatesRequest{
			Namespace:          projectScopeRef(project),
			TemplateName:   "web-app",
			IncludeCurrent: true,
		})

		resp, err := handler.CheckUpdates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// No releases exist, so even include_current should produce nothing.
		if len(resp.Msg.Updates) != 0 {
			t.Errorf("expected 0 updates, got %d", len(resp.Msg.Updates))
		}
	})

	t.Run("include_current with breaking update still reports breaking", func(t *testing.T) {
		projectNs := projectNS(project)
		tmpl := makeTemplateWithLinks("prj-"+project, "web-app", []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, org, linkedTemplateName, ">=1.0.0 <2.0.0"),
		})
		// Releases: 1.0.0, 1.5.0, 2.0.0 — breaking update exists.
		r1 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.0.0")
		r2 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "1.5.0")
		r3 := makeReleaseCMInNS("org-"+org, linkedTemplateName, "2.0.0")
		fakeClient := fake.NewClientset(projectNs, orgNS(org), tmpl, r1, r2, r3)
		handler := newTestHandler(t, fakeClient, shareUsers)

		ctx := authedCtx(ownerEmail, nil)
		req := connect.NewRequest(&consolev1.CheckUpdatesRequest{
			Namespace:          projectScopeRef(project),
			TemplateName:   "web-app",
			IncludeCurrent: true,
		})

		resp, err := handler.CheckUpdates(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.Updates) != 1 {
			t.Fatalf("expected 1 update, got %d", len(resp.Msg.Updates))
		}
		update := resp.Msg.Updates[0]
		if !update.BreakingUpdateAvailable {
			t.Error("expected breaking_update_available=true")
		}
		if update.LatestVersion != "2.0.0" {
			t.Errorf("expected latest_version 2.0.0, got %q", update.LatestVersion)
		}
	})
}
