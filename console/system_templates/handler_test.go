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


const validCue = `package system_template

#Input: {
	gatewayNamespace: string | *"istio-ingress"
}
`

func TestListSystemTemplatesHandler(t *testing.T) {
	t.Run("returns templates for org OWNER", func(t *testing.T) {
		email := "owner@example.com"
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "package system_template\n", true, "istio-ingress")
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
		if !resp.Msg.Templates[0].Mandatory {
			t.Error("expected seeded template to be mandatory")
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
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "package system_template\n", true, "istio-ingress")
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
			Name:             "ref-grant",
			Org:              "my-org",
			DisplayName:      "ReferenceGrant",
			Description:      "desc",
			CueTemplate:      validCue,
			Mandatory:        true,
			GatewayNamespace: "istio-ingress",
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
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", validCue, true, "istio-ingress")
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), &stubRenderer{})

		newGateway := "custom-gateway"
		ctx := authedCtx(email, nil)
		_, err := h.UpdateSystemTemplate(ctx, connect.NewRequest(&consolev1.UpdateSystemTemplateRequest{
			Name:             "ref-grant",
			Org:              "my-org",
			GatewayNamespace: &newGateway,
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("denies org VIEWER", func(t *testing.T) {
		email := "viewer@example.com"
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", validCue, true, "istio-ingress")
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, viewerGrants(email), &stubRenderer{})

		newGateway := "custom-gateway"
		ctx := authedCtx(email, nil)
		_, err := h.UpdateSystemTemplate(ctx, connect.NewRequest(&consolev1.UpdateSystemTemplateRequest{
			Name:             "ref-grant",
			Org:              "my-org",
			GatewayNamespace: &newGateway,
		}))
		if err == nil {
			t.Fatal("expected permission denied error for VIEWER")
		}
	})
}

func TestDeleteSystemTemplateHandler(t *testing.T) {
	t.Run("allows org OWNER to delete", func(t *testing.T) {
		email := "owner@example.com"
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", validCue, true, "istio-ingress")
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
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", validCue, true, "istio-ingress")
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

	t.Run("renders default ReferenceGrant template", func(t *testing.T) {
		email := "owner@example.com"
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		h := NewHandler(k8s, ownerGrants(email), NewCueRendererAdapter())

		ctx := authedCtx(email, nil)
		systemInput := `system: {
	project:   "my-project"
	namespace: "prj-my-project"
	claims: {
		iss:            "https://example.com"
		sub:            "user-123"
		exp:            9999999999
		iat:            1000000000
		email:          "owner@example.com"
		email_verified: true
	}
}`
		userInput := `input: gatewayNamespace: "istio-ingress"`
		resp, err := h.RenderSystemTemplate(ctx, connect.NewRequest(&consolev1.RenderSystemTemplateRequest{
			CueTemplate:    DefaultReferenceGrantTemplate,
			CueSystemInput: systemInput,
			CueInput:       userInput,
		}))
		if err != nil {
			t.Fatalf("expected no error rendering default template, got %v", err)
		}
		if resp.Msg.RenderedYaml == "" {
			t.Error("expected non-empty YAML for default ReferenceGrant template")
		}
		// Verify ReferenceGrant is in the output.
		if !contains(resp.Msg.RenderedYaml, "ReferenceGrant") {
			t.Errorf("expected 'ReferenceGrant' in rendered YAML, got: %s", resp.Msg.RenderedYaml)
		}
		if !contains(resp.Msg.RenderedYaml, "istio-ingress") {
			t.Errorf("expected 'istio-ingress' in rendered YAML, got: %s", resp.Msg.RenderedYaml)
		}
	})
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) >= len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}

func TestSeedDefaultTemplates(t *testing.T) {
	t.Run("seeds reference-grant template with mandatory=true", func(t *testing.T) {
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
		if cm.Annotations[MandatoryAnnotation] != "true" {
			t.Errorf("expected mandatory annotation 'true', got %q", cm.Annotations[MandatoryAnnotation])
		}
		if cm.Annotations[GatewayNsAnnotation] != "istio-ingress" {
			t.Errorf("expected gateway-namespace 'istio-ingress', got %q", cm.Annotations[GatewayNsAnnotation])
		}
		if cm.Data[CueTemplateKey] == "" {
			t.Error("expected non-empty CUE template")
		}
	})
}
