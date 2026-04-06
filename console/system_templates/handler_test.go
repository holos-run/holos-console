package system_templates

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// stubOrgResolver implements OrgResolver for tests.
type stubOrgResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubOrgResolver) GetOrgGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
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

func authedCtx(email string, roles []string) context.Context {
	return rpc.ContextWithClaims(context.Background(), &rpc.Claims{
		Sub:   "user-123",
		Email: email,
		Roles: roles,
	})
}

// ownerGrants returns grants giving the email OWNER access to the org.
func ownerGrants(email string) *stubOrgResolver {
	return &stubOrgResolver{
		users: map[string]string{email: "owner"},
	}
}

// viewerGrants returns grants giving the email VIEWER access to the org.
func viewerGrants(email string) *stubOrgResolver {
	return &stubOrgResolver{
		users: map[string]string{email: "viewer"},
	}
}


const validCue = `package deployment

#Input: {}
`

func TestListSystemTemplatesHandler(t *testing.T) {
	t.Run("returns templates for org OWNER", func(t *testing.T) {
		email := "owner@example.com"
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "package deployment\n", true, false)
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		resp, err := h.ListSystemTemplates(ctx, connect.NewRequest(&consolev1.ListSystemTemplatesRequest{Org: "my-org"}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Msg.Templates) != 1 {
			t.Errorf("expected 1 template, got %d", len(resp.Msg.Templates))
		}
	})

	t.Run("seeds default templates when org has none", func(t *testing.T) {
		email := "owner@example.com"
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		resp, err := h.ListSystemTemplates(ctx, connect.NewRequest(&consolev1.ListSystemTemplatesRequest{Org: "my-org"}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Should have seeded the default reference-grant template.
		if len(resp.Msg.Templates) != 1 {
			t.Errorf("expected 1 seeded template, got %d", len(resp.Msg.Templates))
		}
		if resp.Msg.Templates[0].Name != DefaultReferenceGrantName {
			t.Errorf("expected seeded template name %q, got %q", DefaultReferenceGrantName, resp.Msg.Templates[0].Name)
		}
		// The seeded HTTPRoute template is not mandatory — it is opt-in per deployment.
		if resp.Msg.Templates[0].Mandatory {
			t.Error("expected seeded HTTPRoute template to not be mandatory (opt-in per deployment)")
		}
		// The seeded template starts disabled so org owners can configure it first.
		if resp.Msg.Templates[0].Enabled {
			t.Error("expected seeded HTTPRoute template to be disabled by default")
		}
	})

	t.Run("returns error when org is missing", func(t *testing.T) {
		email := "owner@example.com"
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		_, err := h.ListSystemTemplates(ctx, connect.NewRequest(&consolev1.ListSystemTemplatesRequest{}))
		if err == nil {
			t.Fatal("expected error when org is empty")
		}
	})

	t.Run("returns error when unauthenticated", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants("owner@example.com"), &stubRenderer{})

		_, err := h.ListSystemTemplates(context.Background(), connect.NewRequest(&consolev1.ListSystemTemplatesRequest{Org: "my-org"}))
		if err == nil {
			t.Fatal("expected error for unauthenticated request")
		}
	})
}

func TestGetSystemTemplateHandler(t *testing.T) {
	t.Run("returns template for org VIEWER", func(t *testing.T) {
		email := "viewer@example.com"
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "package deployment\n", true, false)
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, viewerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		resp, err := h.GetSystemTemplate(ctx, connect.NewRequest(&consolev1.GetSystemTemplateRequest{Org: "my-org", Name: "ref-grant"}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Template.Name != "ref-grant" {
			t.Errorf("expected name 'ref-grant', got %q", resp.Msg.Template.Name)
		}
		if !resp.Msg.Template.Mandatory {
			t.Error("expected mandatory=true")
		}
	})
}

func TestCreateSystemTemplateHandler(t *testing.T) {
	t.Run("allows org OWNER to create", func(t *testing.T) {
		email := "owner@example.com"
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		resp, err := h.CreateSystemTemplate(ctx, connect.NewRequest(&consolev1.CreateSystemTemplateRequest{
			Name:        "ref-grant",
			Org:         "my-org",
			DisplayName: "ReferenceGrant",
			Description: "desc",
			CueTemplate: validCue,
			Mandatory:   true,
			Enabled:     false,
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Name != "ref-grant" {
			t.Errorf("expected name 'ref-grant', got %q", resp.Msg.Name)
		}
	})

	t.Run("denies org VIEWER", func(t *testing.T) {
		email := "viewer@example.com"
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, viewerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		_, err := h.CreateSystemTemplate(ctx, connect.NewRequest(&consolev1.CreateSystemTemplateRequest{
			Name:        "ref-grant",
			Org:         "my-org",
			CueTemplate: validCue,
		}))
		if err == nil {
			t.Fatal("expected permission denied error for VIEWER")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", err)
		}
	})

	t.Run("rejects invalid CUE syntax", func(t *testing.T) {
		email := "owner@example.com"
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		_, err := h.CreateSystemTemplate(ctx, connect.NewRequest(&consolev1.CreateSystemTemplateRequest{
			Name:        "ref-grant",
			Org:         "my-org",
			CueTemplate: "THIS IS NOT VALID CUE {{{{",
		}))
		if err == nil {
			t.Fatal("expected error for invalid CUE syntax")
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("expected CodeInvalidArgument, got %v", err)
		}
	})
}

func TestUpdateSystemTemplateHandler(t *testing.T) {
	t.Run("allows org OWNER to update", func(t *testing.T) {
		email := "owner@example.com"
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", validCue, true, false)
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		enabled := true
		ctx := authedCtx(email, nil)
		_, err := h.UpdateSystemTemplate(ctx, connect.NewRequest(&consolev1.UpdateSystemTemplateRequest{
			Name:    "ref-grant",
			Org:     "my-org",
			Enabled: &enabled,
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("denies org VIEWER", func(t *testing.T) {
		email := "viewer@example.com"
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", validCue, true, false)
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, viewerGrants(email), &stubRenderer{})

		enabled := true
		ctx := authedCtx(email, nil)
		_, err := h.UpdateSystemTemplate(ctx, connect.NewRequest(&consolev1.UpdateSystemTemplateRequest{
			Name:    "ref-grant",
			Org:     "my-org",
			Enabled: &enabled,
		}))
		if err == nil {
			t.Fatal("expected permission denied error for VIEWER")
		}
	})
}

func TestDeleteSystemTemplateHandler(t *testing.T) {
	t.Run("allows org OWNER to delete", func(t *testing.T) {
		email := "owner@example.com"
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", validCue, true, false)
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		_, err := h.DeleteSystemTemplate(ctx, connect.NewRequest(&consolev1.DeleteSystemTemplateRequest{
			Name: "ref-grant",
			Org:  "my-org",
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("denies org VIEWER", func(t *testing.T) {
		email := "viewer@example.com"
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", validCue, true, false)
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, viewerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		_, err := h.DeleteSystemTemplate(ctx, connect.NewRequest(&consolev1.DeleteSystemTemplateRequest{
			Name: "ref-grant",
			Org:  "my-org",
		}))
		if err == nil {
			t.Fatal("expected permission denied error for VIEWER")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", err)
		}
	})
}

func TestRenderSystemTemplateHandler(t *testing.T) {
	t.Run("renders template for authenticated user", func(t *testing.T) {
		email := "owner@example.com"
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		renderer := &stubRenderer{
			resources: []RenderResource{
				{YAML: "kind: ReferenceGrant\n", Object: map[string]any{"kind": "ReferenceGrant"}},
			},
		}
		h := NewHandler(k8s, ownerGrants(email), renderer)

		ctx := authedCtx(email, nil)
		resp, err := h.RenderSystemTemplate(ctx, connect.NewRequest(&consolev1.RenderSystemTemplateRequest{
			CueTemplate: validCue,
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.RenderedYaml == "" {
			t.Error("expected rendered YAML to be non-empty")
		}
		if resp.Msg.RenderedJson == "" {
			t.Error("expected rendered JSON to be non-empty")
		}
	})

	t.Run("returns error when cue_template is empty", func(t *testing.T) {
		email := "owner@example.com"
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		_, err := h.RenderSystemTemplate(ctx, connect.NewRequest(&consolev1.RenderSystemTemplateRequest{}))
		if err == nil {
			t.Fatal("expected error when cue_template is empty")
		}
	})

	t.Run("renders default HTTPRoute system template with mock input", func(t *testing.T) {
		email := "owner@example.com"
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), NewCueRendererAdapter())

		ctx := authedCtx(email, nil)
		systemInput := `system: {
	project:          "my-project"
	namespace:        "prj-my-project"
	gatewayNamespace: "istio-ingress"
	claims: {
		iss:            "https://example.com"
		sub:            "user-123"
		exp:            9999999999
		iat:            1000000000
		email:          "owner@example.com"
		email_verified: true
	}
}`
		// Mock input values allow standalone preview of the HTTPRoute system template.
		// At actual deploy time the template is unified with the real deployment template.
		userInput := `input: {
	name:  "my-app"
	image: "nginx"
	tag:   "latest"
}`
		resp, err := h.RenderSystemTemplate(ctx, connect.NewRequest(&consolev1.RenderSystemTemplateRequest{
			CueTemplate:    DefaultReferenceGrantTemplate,
			CueSystemInput: systemInput,
			CueInput:       userInput,
		}))
		if err != nil {
			t.Fatalf("expected no error rendering default HTTPRoute template, got %v", err)
		}
		if resp.Msg.RenderedYaml == "" {
			t.Error("expected non-empty YAML for default HTTPRoute system template")
		}
		// Verify HTTPRoute is in the output.
		if !contains(resp.Msg.RenderedYaml, "HTTPRoute") {
			t.Errorf("expected 'HTTPRoute' in rendered YAML, got: %s", resp.Msg.RenderedYaml)
		}
		// The mock input.name "my-app" should appear in the rendered YAML.
		if !contains(resp.Msg.RenderedYaml, "my-app") {
			t.Errorf("expected 'my-app' in rendered YAML, got: %s", resp.Msg.RenderedYaml)
		}
		// The gatewayNamespace "istio-ingress" should appear in the rendered YAML.
		if !contains(resp.Msg.RenderedYaml, "istio-ingress") {
			t.Errorf("expected 'istio-ingress' in rendered YAML, got: %s", resp.Msg.RenderedYaml)
		}
	})
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) >= len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}

func TestCloneSystemTemplateHandler(t *testing.T) {
	t.Run("allows org OWNER to clone", func(t *testing.T) {
		email := "owner@example.com"
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", validCue, true, true)
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		resp, err := h.CloneSystemTemplate(ctx, connect.NewRequest(&consolev1.CloneSystemTemplateRequest{
			SourceName:  "ref-grant",
			Org:         "my-org",
			Name:        "ref-grant-copy",
			DisplayName: "ReferenceGrant Copy",
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Name != "ref-grant-copy" {
			t.Errorf("expected name 'ref-grant-copy', got %q", resp.Msg.Name)
		}
	})

	t.Run("clone starts with enabled=false", func(t *testing.T) {
		email := "owner@example.com"
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", validCue, true, true)
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		_, err := h.CloneSystemTemplate(ctx, connect.NewRequest(&consolev1.CloneSystemTemplateRequest{
			SourceName:  "ref-grant",
			Org:         "my-org",
			Name:        "ref-grant-copy",
			DisplayName: "ReferenceGrant Copy",
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		clonedCM, err := fakeClient.CoreV1().ConfigMaps("org-my-org").Get(context.Background(), "ref-grant-copy", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected cloned ConfigMap, got %v", err)
		}
		if clonedCM.Annotations[EnabledAnnotation] != "false" {
			t.Errorf("expected cloned template to start with enabled=false, got %q", clonedCM.Annotations[EnabledAnnotation])
		}
	})

	t.Run("denies org VIEWER", func(t *testing.T) {
		email := "viewer@example.com"
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", validCue, true, false)
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, viewerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		_, err := h.CloneSystemTemplate(ctx, connect.NewRequest(&consolev1.CloneSystemTemplateRequest{
			SourceName:  "ref-grant",
			Org:         "my-org",
			Name:        "ref-grant-copy",
			DisplayName: "ReferenceGrant Copy",
		}))
		if err == nil {
			t.Fatal("expected permission denied error for VIEWER")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", err)
		}
	})

	t.Run("returns error when source does not exist", func(t *testing.T) {
		email := "owner@example.com"
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		_, err := h.CloneSystemTemplate(ctx, connect.NewRequest(&consolev1.CloneSystemTemplateRequest{
			SourceName:  "nonexistent",
			Org:         "my-org",
			Name:        "copy",
			DisplayName: "Copy",
		}))
		if err == nil {
			t.Fatal("expected error when source does not exist")
		}
		if connect.CodeOf(err) != connect.CodeNotFound {
			t.Errorf("expected CodeNotFound, got %v", err)
		}
	})

	t.Run("returns error when target name already exists", func(t *testing.T) {
		email := "owner@example.com"
		source := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", validCue, true, false)
		target := sysTemplateConfigMap("my-org", "ref-grant-copy", "ReferenceGrant Copy", "desc", validCue, false, false)
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns, source, target)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		ctx := authedCtx(email, nil)
		_, err := h.CloneSystemTemplate(ctx, connect.NewRequest(&consolev1.CloneSystemTemplateRequest{
			SourceName:  "ref-grant",
			Org:         "my-org",
			Name:        "ref-grant-copy",
			DisplayName: "ReferenceGrant Copy",
		}))
		if err == nil {
			t.Fatal("expected error when target name already exists")
		}
		if connect.CodeOf(err) != connect.CodeAlreadyExists {
			t.Errorf("expected CodeAlreadyExists, got %v", err)
		}
	})
}

func TestSeedDefaultTemplates(t *testing.T) {
	t.Run("seeds HTTPRoute example template with mandatory=false and enabled=false", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.SeedDefaultTemplates(context.Background(), "my-org")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		cm, err := fakeClient.CoreV1().ConfigMaps("org-my-org").Get(context.Background(), DefaultReferenceGrantName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected seeded ConfigMap, got %v", err)
		}
		// The seeded HTTPRoute template is opt-in (not mandatory) and starts disabled.
		if cm.Annotations[MandatoryAnnotation] != "false" {
			t.Errorf("expected mandatory annotation 'false' for seeded HTTPRoute template, got %q", cm.Annotations[MandatoryAnnotation])
		}
		if cm.Annotations[EnabledAnnotation] != "false" {
			t.Errorf("expected enabled annotation 'false' for seeded template, got %q", cm.Annotations[EnabledAnnotation])
		}
		if cm.Data[CueTemplateKey] == "" {
			t.Error("expected non-empty CUE template")
		}
	})
}
