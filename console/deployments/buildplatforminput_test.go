// Tests for buildPlatformInput's gateway-namespace resolution path
// (HOL-526 phase 3 / HOL-644). The legacy implementation hard-coded
// PlatformInput.GatewayNamespace = DefaultGatewayNamespace ("istio-ingress")
// which broke any user template that explicitly set the same field to a
// site-specific value (e.g. "ci-private-apps-gateway"): CUE's unification
// rules treat string : "istio-ingress" & string : "ci-private-apps-gateway"
// as a conflict and refuse to evaluate. These tests pin the new behavior:
//
//   - With no OrganizationGatewayResolver wired, the handler still emits
//     DefaultGatewayNamespace (legacy behavior, preserves existing test
//     wiring that does not bother to construct an org NS).
//   - With a resolver that returns "" (annotation absent), the handler
//     emits DefaultGatewayNamespace.
//   - With a resolver that returns a non-empty value, the handler emits
//     that value verbatim — and unification with a template setting the
//     same value succeeds (the bug-reproduction scenario).
//   - With a resolver that errors, the handler logs and falls back to
//     DefaultGatewayNamespace (a transient lookup failure must never break
//     a render).

package deployments

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/organizations"
	"github.com/holos-run/holos-console/console/projects"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
)

// stubGatewayResolver is a hand-rolled OrganizationGatewayResolver for the
// table-driven tests. The fake K8s client + real resolver path is exercised
// in TestBuildPlatformInput_GatewayNamespace_K8sResolverIntegration so this
// stub keeps the unit tests focused on Handler semantics.
type stubGatewayResolver struct {
	value     string
	err       error
	called    int
	calledFor string
}

func (s *stubGatewayResolver) GetGatewayNamespace(_ context.Context, project string) (string, error) {
	s.called++
	s.calledFor = project
	return s.value, s.err
}

// newHandlerWithGateway wires a Handler with the minimum stubs needed to
// exercise buildPlatformInput's gateway resolution path. The renderer,
// applier, and other collaborators are not invoked by buildPlatformInput
// itself, so trivial stubs are sufficient.
func newHandlerWithGateway(gw OrganizationGatewayResolver) *Handler {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())
	h := NewHandler(k8s, &stubProjectResolver{}, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, &stubRenderer{}, &stubApplier{})
	if gw != nil {
		h = h.WithOrganizationGatewayResolver(gw)
	}
	return h
}

func TestBuildPlatformInput_GatewayNamespace(t *testing.T) {
	cases := []struct {
		name     string
		resolver OrganizationGatewayResolver
		want     string
		// wantCallFor asserts that the resolver was invoked with the
		// expected project name. Empty means "do not check".
		wantCallFor string
	}{
		{
			// Legacy test wiring: no resolver configured. Behavior must
			// be unchanged from the pre-HOL-644 hard-coded default so
			// the dozens of existing handler tests keep passing without
			// being touched.
			name:     "nil resolver falls back to DefaultGatewayNamespace",
			resolver: nil,
			want:     DefaultGatewayNamespace,
		},
		{
			// Org has no annotation set: resolver returns "". The
			// handler must apply DefaultGatewayNamespace — a legitimate
			// "use the default" signal, not an error.
			name:     "empty resolver value falls back to DefaultGatewayNamespace",
			resolver: &stubGatewayResolver{value: ""},
			want:     DefaultGatewayNamespace,
		},
		{
			// HOL-526 bug-fix path: org's gateway-namespace
			// annotation is set to a site-specific value. The handler
			// must inject that value so a template that pins the same
			// value unifies cleanly instead of conflicting with the
			// historical "istio-ingress" injection.
			name:        "configured resolver value is propagated verbatim",
			resolver:    &stubGatewayResolver{value: "ci-private-apps-gateway"},
			want:        "ci-private-apps-gateway",
			wantCallFor: "my-project",
		},
		{
			// Transient resolver failure (e.g. org NS not yet
			// created, transient K8s API error): the handler must NOT
			// fail the build — it logs at WARN and falls back to the
			// historical default so callers get a render error from
			// CUE only when the template itself conflicts, not from
			// the gateway lookup.
			name:        "resolver error falls back to DefaultGatewayNamespace",
			resolver:    &stubGatewayResolver{err: errors.New("k8s api unavailable")},
			want:        DefaultGatewayNamespace,
			wantCallFor: "my-project",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHandlerWithGateway(tc.resolver)
			pi := h.buildPlatformInput(context.Background(), "my-project", "prj-my-project", &rpc.Claims{Sub: "u1", Email: "test@example.com"})
			if pi.GatewayNamespace != tc.want {
				t.Errorf("GatewayNamespace = %q, want %q", pi.GatewayNamespace, tc.want)
			}
			if tc.wantCallFor != "" {
				stub, ok := tc.resolver.(*stubGatewayResolver)
				if !ok {
					t.Fatalf("wantCallFor set but resolver is not a *stubGatewayResolver")
				}
				if stub.called != 1 {
					t.Errorf("resolver call count = %d, want 1", stub.called)
				}
				if stub.calledFor != tc.wantCallFor {
					t.Errorf("resolver called for %q, want %q", stub.calledFor, tc.wantCallFor)
				}
			}
		})
	}
}

// TestBuildPlatformInput_GatewayNamespace_PropagatesToRenderer is the
// integration boundary check: the resolved value must actually land in the
// PlatformInput passed to the Renderer, not just be set on a local
// variable. This guards against future refactors that compute the value
// correctly but forget to thread it through to inputs.Platform.
func TestBuildPlatformInput_GatewayNamespace_PropagatesToRenderer(t *testing.T) {
	stub := &stubGatewayResolver{value: "edge-gateway"}
	h := newHandlerWithGateway(stub)
	pi := h.buildPlatformInput(context.Background(), "my-project", "prj-my-project", &rpc.Claims{Sub: "u1"})
	if pi.GatewayNamespace != "edge-gateway" {
		t.Fatalf("PlatformInput.GatewayNamespace = %q, want %q", pi.GatewayNamespace, "edge-gateway")
	}
	// Sanity: the other fields the renderer relies on must also be set —
	// a regression here would be confusing because the gateway field
	// would still pass while the broader contract was broken.
	if pi.Project != "my-project" {
		t.Errorf("Project = %q, want %q", pi.Project, "my-project")
	}
	if pi.Namespace != "prj-my-project" {
		t.Errorf("Namespace = %q, want %q", pi.Namespace, "prj-my-project")
	}
}

// TestBuildPlatformInput_GatewayNamespace_K8sResolverIntegration drives the
// real organizations.GatewayNamespaceResolver against a fake K8s client to
// catch wiring drift between the deployments interface and the
// organizations adapter implementation. A pure-stub test would let a future
// rename of the projects.ProjectGrantResolver method (GetProjectOrganization)
// silently break production wiring without failing CI.
func TestBuildPlatformInput_GatewayNamespace_K8sResolverIntegration(t *testing.T) {
	const orgName = "acme"
	const projectName = "my-project"

	// orgNs builds an organization namespace for the integration tests.
	// The label set mirrors what organizations.K8sClient.CreateOrganization
	// produces in production so GetOrganization's label assertions pass.
	orgNs := func(annotations map[string]string) *corev1.Namespace {
		return &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "holos-org-" + orgName,
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
					v1alpha2.LabelOrganization: orgName,
				},
				Annotations: annotations,
			},
		}
	}
	// projectNs mirrors the production project namespace shape: the
	// LabelOrganization value lets projects.GetProjectOrg derive the org
	// name without walking ancestry.
	projectNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-" + projectName,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      projectName,
				v1alpha2.LabelOrganization: orgName,
			},
		},
	}

	cases := []struct {
		name        string
		annotations map[string]string
		want        string
	}{
		{
			name:        "annotation absent yields DefaultGatewayNamespace",
			annotations: nil,
			want:        DefaultGatewayNamespace,
		},
		{
			name:        "annotation empty yields DefaultGatewayNamespace",
			annotations: map[string]string{v1alpha2.AnnotationGatewayNamespace: ""},
			want:        DefaultGatewayNamespace,
		},
		{
			name:        "annotation set to ci-private-apps-gateway is propagated (HOL-526 fix)",
			annotations: map[string]string{v1alpha2.AnnotationGatewayNamespace: "ci-private-apps-gateway"},
			want:        "ci-private-apps-gateway",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientset(orgNs(tc.annotations), projectNs)
			// Use the same prefix layout as testResolver() in k8s_test.go
			// (NamespacePrefix="holos-", OrganizationPrefix="org-",
			// ProjectPrefix="prj-") so OrgNamespace + ProjectNamespace
			// match the holos-org-<name> / holos-prj-<name> objects above.
			r := &resolver.Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
			orgsK8s := organizations.NewK8sClient(fakeClient, r)
			projectsK8s := projects.NewK8sClient(fakeClient, r)
			projectGrants := projects.NewProjectGrantResolver(projectsK8s)
			gw := organizations.NewGatewayNamespaceResolver(orgsK8s, projectGrants)

			h := NewHandler(NewK8sClient(fakeClient, r), &stubProjectResolver{}, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, &stubRenderer{}, &stubApplier{}).
				WithOrganizationGatewayResolver(gw)

			pi := h.buildPlatformInput(context.Background(), projectName, "holos-prj-"+projectName, &rpc.Claims{Sub: "u1"})
			if pi.GatewayNamespace != tc.want {
				t.Errorf("GatewayNamespace = %q, want %q", pi.GatewayNamespace, tc.want)
			}
		})
	}
}

// TestBuildPlatformInput_GatewayNamespace_TemplateUnifies pins the actual
// HOL-526 bug fix: with the org annotation set to "ci-private-apps-gateway"
// and the deployment template setting platform.gatewayNamespace to the same
// value, the render must succeed (no CUE unification conflict) and the
// resulting resource must carry that value. Pre-HOL-644, the handler
// injected the hard-coded "istio-ingress" while the template injected
// "ci-private-apps-gateway", causing CUE to fail with a string conflict
// and the deployment to be rejected.
func TestBuildPlatformInput_GatewayNamespace_TemplateUnifies(t *testing.T) {
	const orgName = "acme"
	const projectName = "my-project"
	const gw = "ci-private-apps-gateway"

	orgNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-" + orgName,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
				v1alpha2.LabelOrganization: orgName,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationGatewayNamespace: gw,
			},
		},
	}
	projectNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-" + projectName,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      projectName,
				v1alpha2.LabelOrganization: orgName,
			},
		},
	}

	fakeClient := fake.NewClientset(orgNamespace, projectNamespace)
	r := &resolver.Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
	orgsK8s := organizations.NewK8sClient(fakeClient, r)
	projectsK8s := projects.NewK8sClient(fakeClient, r)
	gwResolver := organizations.NewGatewayNamespaceResolver(orgsK8s, projects.NewProjectGrantResolver(projectsK8s))

	h := NewHandler(NewK8sClient(fakeClient, r), &stubProjectResolver{}, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, &stubRenderer{}, &stubApplier{}).
		WithOrganizationGatewayResolver(gwResolver)

	pi := h.buildPlatformInput(context.Background(), projectName, "holos-prj-"+projectName, &rpc.Claims{
		Sub:           "u1",
		Email:         "test@example.com",
		EmailVerified: true,
	})
	if pi.GatewayNamespace != gw {
		t.Fatalf("GatewayNamespace = %q, want %q", pi.GatewayNamespace, gw)
	}

	// Drive the real CueRenderer with a template that pins the same
	// gateway namespace value the org has configured. Pre-HOL-644 this
	// would fail with a CUE unification conflict because the handler
	// injected "istio-ingress" while the template injected
	// "ci-private-apps-gateway". With the fix in place, both inputs agree
	// and the render succeeds.
	renderer := &CueRenderer{}
	resources, err := renderFlat(renderer, context.Background(), gatewayNamespacePinningTemplate(gw), pi, v1alpha2.ProjectInput{
		Name:  "web-app",
		Image: "nginx",
		Tag:   "1.25",
		Port:  8080,
	})
	if err != nil {
		t.Fatalf("expected render to succeed when template and org agree on gatewayNamespace=%q, got error: %v", gw, err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	got := resources[0].GetAnnotations()["test.holos.run/gateway-namespace"]
	if got != gw {
		t.Errorf("rendered gateway-namespace annotation = %q, want %q", got, gw)
	}
}

// gatewayNamespacePinningTemplate returns a CUE template that pins
// platform.gatewayNamespace to the supplied value. This is the shape of
// template that triggered the HOL-526 conflict before the fix: it asserts
// a concrete value at the same path the backend was injecting, and CUE
// rejects the unification when the values disagree.
func gatewayNamespacePinningTemplate(want string) string {
	return `
input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:          string
	namespace:        string
	gatewayNamespace: "` + want + `"
	organization:     string
	claims: {
		iss:            string
		sub:            string
		exp:            int
		iat:            int
		email:          string
		email_verified: bool
	}
}

projectResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: (input.name): {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       input.name
				}
				annotations: {
					"test.holos.run/gateway-namespace": platform.gatewayNamespace
				}
			}
		}
	}
	clusterResources: {}
}
`
}
