package organizations

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

// stubProjectOrgResolver is a hand-rolled ProjectOrgResolver. The real
// implementation lives in console/projects (ProjectGrantResolver) and is
// exercised end-to-end in console/deployments tests; here we focus on
// GatewayNamespaceResolver's branching logic in isolation so a regression
// in the org annotation read does not get masked by a failure in the
// project→org step.
type stubProjectOrgResolver struct {
	org string
	err error
}

func (s *stubProjectOrgResolver) GetProjectOrganization(_ context.Context, _ string) (string, error) {
	return s.org, s.err
}

func TestGatewayNamespaceResolver_GetGatewayNamespace(t *testing.T) {
	const orgName = "acme"

	makeOrgNs := func(annotations map[string]string) *corev1.Namespace {
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

	t.Run("returns annotation value when set", func(t *testing.T) {
		client := fake.NewClientset(makeOrgNs(map[string]string{
			v1alpha2.AnnotationGatewayNamespace: "ci-private-apps-gateway",
		}))
		k8s := NewK8sClient(client, testResolver())
		r := NewGatewayNamespaceResolver(k8s, &stubProjectOrgResolver{org: orgName})
		got, err := r.GetGatewayNamespace(context.Background(), "any-project")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "ci-private-apps-gateway" {
			t.Errorf("got %q, want %q", got, "ci-private-apps-gateway")
		}
	})

	t.Run("returns empty when annotation absent", func(t *testing.T) {
		client := fake.NewClientset(makeOrgNs(nil))
		k8s := NewK8sClient(client, testResolver())
		r := NewGatewayNamespaceResolver(k8s, &stubProjectOrgResolver{org: orgName})
		got, err := r.GetGatewayNamespace(context.Background(), "any-project")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("returns empty when project has no organization", func(t *testing.T) {
		// A project with no LabelOrganization yields "" from
		// GetProjectOrganization. The resolver must short-circuit
		// rather than calling GetOrganization with an empty name
		// (which would error out and trigger the soft-fail fallback,
		// hiding the real cause).
		client := fake.NewClientset(makeOrgNs(nil))
		k8s := NewK8sClient(client, testResolver())
		r := NewGatewayNamespaceResolver(k8s, &stubProjectOrgResolver{org: ""})
		got, err := r.GetGatewayNamespace(context.Background(), "orphan-project")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("returns project-org-resolver error", func(t *testing.T) {
		client := fake.NewClientset()
		k8s := NewK8sClient(client, testResolver())
		r := NewGatewayNamespaceResolver(k8s, &stubProjectOrgResolver{err: errors.New("boom")})
		_, err := r.GetGatewayNamespace(context.Background(), "any-project")
		if err == nil {
			t.Fatal("expected error from project-org resolver, got nil")
		}
	})

	t.Run("returns k8s lookup error when org namespace missing", func(t *testing.T) {
		// The fake client has no org namespace, so GetOrganization
		// surfaces a NotFound. The resolver must propagate the error
		// (not swallow it to "") so the deployments handler logs the
		// real cause when it falls back to DefaultGatewayNamespace.
		client := fake.NewClientset()
		k8s := NewK8sClient(client, testResolver())
		r := NewGatewayNamespaceResolver(k8s, &stubProjectOrgResolver{org: orgName})
		_, err := r.GetGatewayNamespace(context.Background(), "any-project")
		if err == nil {
			t.Fatal("expected error from missing org namespace, got nil")
		}
	})

	t.Run("nil project-org resolver yields empty without error", func(t *testing.T) {
		// Defensive guard: a misconfigured wire-up that forgets to
		// pass a ProjectOrgResolver must degrade gracefully rather
		// than NPE. The deployments handler treats "" as "use the
		// default" so this preserves legacy behavior in the
		// pathological case.
		client := fake.NewClientset(makeOrgNs(nil))
		k8s := NewK8sClient(client, testResolver())
		r := NewGatewayNamespaceResolver(k8s, nil)
		got, err := r.GetGatewayNamespace(context.Background(), "any-project")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})
}
