// handler_render_test.go exercises RenderTemplate with backend-injected
// platform values (HOL-828). The tests verify that the template-preview path
// resolves and injects authoritative platform-owned values — gatewayNamespace
// at minimum — into the unified CUE value at the platform path, mirroring
// console/deployments/handler.go:buildPlatformInput.
package templates

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kfake "k8s.io/client-go/kubernetes/fake"

	"github.com/holos-run/holos-console/console/deployments"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// httpRouteTemplate is the HTTPRoute (v1) org template CUE body. It references
// platform.gatewayNamespace, which must be resolved by the backend when the
// user does not supply it explicitly.
const httpRouteTemplate = `
// input and platform are available because platform templates are unified with
// the deployment template before evaluation (ADR 016 Decision 8).
input: #ProjectInput & {
	port: >0 & <=65535 | *8080
}
platform: #PlatformInput

// platformResources holds resources the platform team manages. The renderer
// reads these only from organization/folder-level templates — project templates
// that define platformResources are silently ignored (ADR 016 Decision 8).
platformResources: {
	namespacedResources: (platform.gatewayNamespace): {
		// HTTPRoute routes traffic from the gateway to the project Service on port 80.
		HTTPRoute: (input.name): {
			apiVersion: "gateway.networking.k8s.io/v1"
			kind:       "HTTPRoute"
			metadata: {
				name:      input.name
				namespace: platform.gatewayNamespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       input.name
				}
			}
			spec: {
				parentRefs: [{
					group:     "gateway.networking.k8s.io"
					kind:      "Gateway"
					namespace: platform.gatewayNamespace
					name:      "default"
				}]
				rules: [{
					backendRefs: [{
						name:      input.name
						namespace: platform.namespace
						port:      80
					}]
				}]
			}
		}
	}
	clusterResources: {}
}
`

// stubGatewayResolver is a test double for OrganizationGatewayResolver.
type stubGatewayResolver struct {
	gatewayNs    string // returned by GetGatewayNamespace (project path)
	orgGatewayNs string // returned by GetOrgGatewayNamespace (org path)
	err          error
}

func (s *stubGatewayResolver) GetGatewayNamespace(_ context.Context, _ string) (string, error) {
	return s.gatewayNs, s.err
}

func (s *stubGatewayResolver) GetOrgGatewayNamespace(_ context.Context, _ string) (string, error) {
	return s.orgGatewayNs, s.err
}

// makeOrgNamespace returns a Namespace representing an organization scope with
// the given name, using the testResolver's org prefix.
func makeOrgNamespace(orgName string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testResolver.OrgNamespace(orgName),
		},
	}
}

// newRenderHandler builds a minimal Handler wired for RenderTemplate tests.
// It accepts a gatewayResolver and optional objects for the fake clientset.
func newRenderHandler(t *testing.T, gatewayRes OrganizationGatewayResolver, objs ...runtime.Object) *Handler {
	t.Helper()
	cs := kfake.NewClientset(objs...)
	k8sClient := newTestK8sClient(t, cs, testResolver)
	renderer := NewCueRendererAdapter()
	h := NewHandler(k8sClient, testResolver, renderer, nil)
	if gatewayRes != nil {
		h = h.WithOrganizationGatewayResolver(gatewayRes)
	}
	return h
}

// TestRenderTemplate_PlatformInjection verifies that the backend injects
// platform-owned values (gatewayNamespace) into the CUE render, so the user
// does not need to set platform.gatewayNamespace explicitly in the seed input.
func TestRenderTemplate_PlatformInjection(t *testing.T) {
	ctx := authedCtx("platform@localhost", []string{"owner"})
	orgNs := testResolver.OrgNamespace("acme")

	// seed platform input that does NOT include gatewayNamespace — the backend
	// must inject it from the resolver. The seed omits organization/project/
	// namespace so the backend-resolved values unify without conflict. A real
	// frontend seed for an org-scope template only carries what the user typed,
	// not placeholder empty strings for backend-owned fields.
	seedPlatformInput := `platform: #PlatformInput`
	// project input provides the deployment name — required by #ProjectInput.
	projectInput := `input: {
	name:  "my-service"
	image: "nginx"
	tag:   "latest"
	port:  8080
}`

	t.Run("resolver returns custom value (used)", func(t *testing.T) {
		resolver := &stubGatewayResolver{orgGatewayNs: "ci-private-apps-gateway"}
		h := newRenderHandler(t, resolver, makeOrgNamespace("acme"))

		resp, err := h.RenderTemplate(ctx, connect.NewRequest(&consolev1.RenderTemplateRequest{
			CueTemplate:      httpRouteTemplate,
			CuePlatformInput: seedPlatformInput,
			CueProjectInput:  projectInput,
			Namespace:        orgNs,
		}))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		yaml := resp.Msg.PlatformResourcesYaml
		if !strings.Contains(yaml, "kind: HTTPRoute") {
			t.Errorf("platform_resources_yaml must contain 'kind: HTTPRoute', got:\n%s", yaml)
		}
		if !strings.Contains(yaml, "ci-private-apps-gateway") {
			t.Errorf("platform_resources_yaml must contain resolved gatewayNamespace 'ci-private-apps-gateway', got:\n%s", yaml)
		}
		if resp.Msg.ProjectResourcesYaml != "" {
			t.Errorf("expected empty project_resources_yaml for platform-only template, got:\n%s", resp.Msg.ProjectResourcesYaml)
		}
	})

	t.Run("resolver returns empty (falls back to DefaultGatewayNamespace)", func(t *testing.T) {
		resolver := &stubGatewayResolver{orgGatewayNs: ""}
		h := newRenderHandler(t, resolver, makeOrgNamespace("acme"))

		resp, err := h.RenderTemplate(ctx, connect.NewRequest(&consolev1.RenderTemplateRequest{
			CueTemplate:      httpRouteTemplate,
			CuePlatformInput: seedPlatformInput,
			CueProjectInput:  projectInput,
			Namespace:        orgNs,
		}))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		yaml := resp.Msg.PlatformResourcesYaml
		if !strings.Contains(yaml, "kind: HTTPRoute") {
			t.Errorf("platform_resources_yaml must contain 'kind: HTTPRoute', got:\n%s", yaml)
		}
		if !strings.Contains(yaml, deployments.DefaultGatewayNamespace) {
			t.Errorf("platform_resources_yaml must contain default gateway namespace %q, got:\n%s", deployments.DefaultGatewayNamespace, yaml)
		}
	})

	t.Run("resolver returns error (falls back to DefaultGatewayNamespace)", func(t *testing.T) {
		resolver := &stubGatewayResolver{err: errResolverFailed}
		h := newRenderHandler(t, resolver, makeOrgNamespace("acme"))

		resp, err := h.RenderTemplate(ctx, connect.NewRequest(&consolev1.RenderTemplateRequest{
			CueTemplate:      httpRouteTemplate,
			CuePlatformInput: seedPlatformInput,
			CueProjectInput:  projectInput,
			Namespace:        orgNs,
		}))
		if err != nil {
			t.Fatalf("expected no error (resolver error is a soft failure), got: %v", err)
		}
		yaml := resp.Msg.PlatformResourcesYaml
		if !strings.Contains(yaml, deployments.DefaultGatewayNamespace) {
			t.Errorf("platform_resources_yaml must contain default gateway namespace %q on resolver error, got:\n%s", deployments.DefaultGatewayNamespace, yaml)
		}
	})

	t.Run("nil resolver (falls back to DefaultGatewayNamespace)", func(t *testing.T) {
		h := newRenderHandler(t, nil, makeOrgNamespace("acme"))

		resp, err := h.RenderTemplate(ctx, connect.NewRequest(&consolev1.RenderTemplateRequest{
			CueTemplate:      httpRouteTemplate,
			CuePlatformInput: seedPlatformInput,
			CueProjectInput:  projectInput,
			Namespace:        orgNs,
		}))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		yaml := resp.Msg.PlatformResourcesYaml
		if !strings.Contains(yaml, deployments.DefaultGatewayNamespace) {
			t.Errorf("platform_resources_yaml must contain default gateway namespace %q with nil resolver, got:\n%s", deployments.DefaultGatewayNamespace, yaml)
		}
	})

	t.Run("user supplies same value as backend resolves (unifies cleanly)", func(t *testing.T) {
		resolver := &stubGatewayResolver{orgGatewayNs: "ci-private-apps-gateway"}
		h := newRenderHandler(t, resolver, makeOrgNamespace("acme"))

		// User supplies the same gatewayNamespace the backend resolves — CUE
		// unification should succeed (string & "same" & "same" == "same").
		// The input does not set fields like organization to avoid conflict with
		// the backend-resolved "acme" value; only gatewayNamespace is pinned.
		sameValuePlatformInput := `platform: gatewayNamespace: "ci-private-apps-gateway"`
		resp, err := h.RenderTemplate(ctx, connect.NewRequest(&consolev1.RenderTemplateRequest{
			CueTemplate:      httpRouteTemplate,
			CuePlatformInput: sameValuePlatformInput,
			CueProjectInput:  projectInput,
			Namespace:        orgNs,
		}))
		if err != nil {
			t.Fatalf("user-supplied same value should unify cleanly, got: %v", err)
		}
		yaml := resp.Msg.PlatformResourcesYaml
		if !strings.Contains(yaml, "ci-private-apps-gateway") {
			t.Errorf("platform_resources_yaml must contain the expected gateway namespace, got:\n%s", yaml)
		}
	})

	t.Run("user supplies conflicting value (CUE conflict surfaces as error)", func(t *testing.T) {
		resolver := &stubGatewayResolver{orgGatewayNs: "ci-private-apps-gateway"}
		h := newRenderHandler(t, resolver, makeOrgNamespace("acme"))

		// User supplies a different gatewayNamespace from what the backend
		// resolves; CUE unification should yield a clean conflict error.
		conflictingPlatformInput := `platform: gatewayNamespace: "different-gateway"`
		_, err := h.RenderTemplate(ctx, connect.NewRequest(&consolev1.RenderTemplateRequest{
			CueTemplate:      httpRouteTemplate,
			CuePlatformInput: conflictingPlatformInput,
			CueProjectInput:  projectInput,
			Namespace:        orgNs,
		}))
		if err == nil {
			t.Fatal("expected CUE conflict error when user supplies conflicting gatewayNamespace, got nil")
		}
	})
}

// errResolverFailed is a sentinel error returned by the stub resolver in the
// "resolver errors" test case.
var errResolverFailed = errType("resolver: simulated failure")

type errType string

func (e errType) Error() string { return string(e) }
