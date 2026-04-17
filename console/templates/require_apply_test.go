package templates

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/deployments"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// stubRequireRuleResolver returns a fixed list of matches for every call. Used
// by TestRequiredTemplateApplier to drive the apply path without a real
// TemplatePolicy resolver.
type stubRequireRuleResolver struct {
	matches []RequireRuleMatch
	err     error
}

func (s *stubRequireRuleResolver) ResolveRequiredTemplates(_ context.Context, _, _ string) ([]RequireRuleMatch, error) {
	return s.matches, s.err
}

// stubHierarchyWalkerFromNamespaces adapts a flat list of namespaces to the
// RenderHierarchyWalker interface so tests can drive ancestor resolution.
type stubHierarchyWalkerFromNamespaces struct {
	ancestors []*corev1.Namespace
}

func (s *stubHierarchyWalkerFromNamespaces) WalkAncestors(_ context.Context, _ string) ([]*corev1.Namespace, error) {
	return s.ancestors, nil
}

// recordingApplier captures Apply calls so the test can assert deployment name
// and resource list without running against a real cluster.
type recordingApplier struct {
	calls []recordedApply
	err   error
}

type recordedApply struct {
	project        string
	deploymentName string
	resources      []unstructured.Unstructured
}

func (r *recordingApplier) Apply(_ context.Context, project, deploymentName string, resources []unstructured.Unstructured) error {
	r.calls = append(r.calls, recordedApply{project: project, deploymentName: deploymentName, resources: resources})
	return r.err
}

func TestRequiredTemplateApplier(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Minimal valid template: one ConfigMap in the project namespace. We
	// hardcode the namespace string because the test harness does not have
	// the CUE evaluator wire the PlatformInput into a top-level `platform`
	// identifier (FillPath fills the path, it does not declare an
	// identifier). Production templates reach the same namespace via
	// `(platform.namespace)` but for the purposes of this test the literal
	// string keeps the fixture small.
	validTemplateCUE := `projectResources: namespacedResources: "prj-new-prj": ConfigMap: "sentinel": {
	apiVersion: "v1"
	kind:       "ConfigMap"
	metadata: {
		name:      "sentinel"
		namespace: "prj-new-prj"
		labels: "app.kubernetes.io/managed-by": "console.holos.run"
	}
}
`

	orgNsObj := orgNS("acme")
	projectNsObj := projectNS("new-prj")
	tmpl := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "audit-policy",
			Namespace: "org-acme",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: "Audit Policy",
				v1alpha2.AnnotationEnabled:     "true",
			},
		},
		Data: map[string]string{
			CueTemplateKey: validTemplateCUE,
		},
	}

	tests := []struct {
		name            string
		matches         []RequireRuleMatch
		resolveErr      error
		applyErr        error
		wantApplyCalls  int
		wantErrContains string
	}{
		{
			name:           "no matches is a no-op",
			matches:        nil,
			wantApplyCalls: 0,
		},
		{
			name: "single match applies the template",
			matches: []RequireRuleMatch{
				{
					Scope:        consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
					ScopeName:    "acme",
					TemplateName: "audit-policy",
				},
			},
			wantApplyCalls: 1,
		},
		{
			name:            "resolver error propagates",
			resolveErr:      fmt.Errorf("resolver boom"),
			wantErrContains: "resolver boom",
		},
		{
			name: "apply error propagates with template identity",
			matches: []RequireRuleMatch{
				{
					Scope:        consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
					ScopeName:    "acme",
					TemplateName: "audit-policy",
				},
			},
			applyErr:        fmt.Errorf("apply boom"),
			wantErrContains: "audit-policy",
			wantApplyCalls:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientset(orgNsObj, projectNsObj, tmpl)
			r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
			k8s := NewK8sClient(fakeClient, r)
			walker := &stubHierarchyWalkerFromNamespaces{
				ancestors: []*corev1.Namespace{projectNsObj, orgNsObj},
			}
			applier := &recordingApplier{err: tc.applyErr}
			resolver := &stubRequireRuleResolver{matches: tc.matches, err: tc.resolveErr}

			rta := NewRequiredTemplateApplier(k8s, walker, &deployments.CueRenderer{}, applier, resolver)

			claims := &rpc.Claims{Sub: "alice", Email: "alice@example.com"}
			err := rta.ApplyRequiredTemplates(context.Background(), "acme", "new-prj", "prj-new-prj", claims)

			if tc.wantErrContains == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrContains)
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Errorf("expected error to contain %q, got %q", tc.wantErrContains, err.Error())
				}
			}

			if len(applier.calls) != tc.wantApplyCalls {
				t.Errorf("expected %d Apply calls, got %d", tc.wantApplyCalls, len(applier.calls))
			}
			for _, call := range applier.calls {
				if call.project != "new-prj" {
					t.Errorf("expected apply project %q, got %q", "new-prj", call.project)
				}
				if call.deploymentName != "audit-policy" {
					t.Errorf("expected apply deploymentName %q, got %q", "audit-policy", call.deploymentName)
				}
				if len(call.resources) == 0 {
					t.Errorf("expected at least one rendered resource, got 0")
				}
			}
		})
	}
}

func TestRequiredTemplateApplier_NilResolverIsNoOp(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
	k8s := NewK8sClient(fake.NewClientset(), r)
	applier := &recordingApplier{}
	rta := NewRequiredTemplateApplier(k8s, nil, &deployments.CueRenderer{}, applier, nil)

	if err := rta.ApplyRequiredTemplates(context.Background(), "acme", "new-prj", "prj-new-prj", nil); err != nil {
		t.Fatalf("expected no error with nil resolver, got %v", err)
	}
	if len(applier.calls) != 0 {
		t.Errorf("expected 0 Apply calls with nil resolver, got %d", len(applier.calls))
	}
}

func TestEmptyRequireRuleResolver(t *testing.T) {
	resolver := NewEmptyRequireRuleResolver()
	matches, err := resolver.ResolveRequiredTemplates(context.Background(), "acme", "proj")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}
