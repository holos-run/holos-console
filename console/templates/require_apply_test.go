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
	"github.com/holos-run/holos-console/console/policyresolver"
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
// Records every WalkAncestors call so tests can guard against regressions
// that skip the walk entirely (HOL-571 review round 2).
type stubHierarchyWalkerFromNamespaces struct {
	ancestors   []*corev1.Namespace
	callCount   int
	lastStartNs string
}

func (s *stubHierarchyWalkerFromNamespaces) WalkAncestors(_ context.Context, startNs string) ([]*corev1.Namespace, error) {
	s.callCount++
	s.lastStartNs = startNs
	return s.ancestors, nil
}

// recordingApplier captures ApplyRequiredTemplate calls so the test can
// assert template name and resource list without running against a real
// cluster. Named fields preserve the old shape for brevity; the
// deploymentName field now carries the templateName argument.
type recordingApplier struct {
	calls []recordedApply
	err   error
}

type recordedApply struct {
	project        string
	deploymentName string
	resources      []unstructured.Unstructured
}

func (r *recordingApplier) ApplyRequiredTemplate(_ context.Context, project, templateName string, resources []unstructured.Unstructured) error {
	r.calls = append(r.calls, recordedApply{project: project, deploymentName: templateName, resources: resources})
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

			rta := NewRequiredTemplateApplier(k8s, walker, &deployments.CueRenderer{}, applier, resolver, policyresolver.NewNoopResolver())

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
	rta := NewRequiredTemplateApplier(k8s, nil, &deployments.CueRenderer{}, applier, nil, policyresolver.NewNoopResolver())

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

// TestRequiredTemplateApplier_FailsClosedWhenAncestorLookupEmpty guards the
// HOL-571 round 3 P1 fix: when ListEffectiveTemplateSources returns the
// (nil, nil, nil) "degraded" signal (a nil walker or walker failure),
// applyMatch must refuse to proceed. Silently rendering an empty manifest
// would let a project be created without the policy-REQUIRE'd templates,
// defeating the enforcement boundary.
func TestRequiredTemplateApplier_FailsClosedWhenAncestorLookupEmpty(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	r := &resolver.Resolver{
		NamespacePrefix:    "",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
	k8s := NewK8sClient(fake.NewClientset(), r)
	applier := &recordingApplier{}
	res := &stubRequireRuleResolver{
		matches: []RequireRuleMatch{
			{
				Scope:        consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
				ScopeName:    "acme",
				TemplateName: "audit-policy",
			},
		},
	}
	// Passing a nil walker makes ListEffectiveTemplateSources return
	// (nil, nil, nil), the "degraded" signal this guard is written for.
	rta := NewRequiredTemplateApplier(k8s, nil, &deployments.CueRenderer{}, applier, res, policyresolver.NewNoopResolver())

	err := rta.ApplyRequiredTemplates(context.Background(), "acme", "new-prj", "prj-new-prj", nil)
	if err == nil {
		t.Fatal("expected a fail-closed error when ancestor lookup degrades, got nil")
	}
	if !strings.Contains(err.Error(), "refusing to create project") {
		t.Errorf("expected fail-closed error message, got %q", err.Error())
	}
	if len(applier.calls) != 0 {
		t.Errorf("expected no Apply calls on fail-closed path, got %d", len(applier.calls))
	}
}

// TestRequiredTemplateApplier_WalksFolderAncestryForPlatformInput guards
// the fix from HOL-571 review round 1 finding 2: the applier must consult
// the RenderHierarchyWalker when building PlatformInput so
// `platform.folders` is populated for folder/org-scope required templates.
// The stub walker records every invocation, so if the walk call is ever
// removed from ApplyRequiredTemplates the assertion will fire — covering
// the round-2 gap where checking fixture data alone was not enough.
// End-to-end Folders-content assertions live in the handler-level
// integration tests; at the unit level we guard against the call being
// skipped entirely.
func TestRequiredTemplateApplier_WalksFolderAncestryForPlatformInput(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Minimal fixture: org acme → folder eng → project new-prj.
	orgNsObj := orgNS("acme")
	folderEngNsObj := folderNS("eng")
	projectNsObj := projectNS("new-prj")
	fakeClient := fake.NewClientset(orgNsObj, folderEngNsObj, projectNsObj)

	r := &resolver.Resolver{
		NamespacePrefix:    "",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
	k8s := NewK8sClient(fakeClient, r)

	// child→parent order so applyRequired walks project, folder, org.
	walker := &stubHierarchyWalkerFromNamespaces{
		ancestors: []*corev1.Namespace{projectNsObj, folderEngNsObj, orgNsObj},
	}
	applier := &recordingApplier{}
	// Resolver matches one template so the walker path is actually
	// exercised; the apply failure on the empty template body is tolerated
	// because the test asserts on the walk, not the apply.
	res := &stubRequireRuleResolver{
		matches: []RequireRuleMatch{
			{
				Scope:        consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
				ScopeName:    "acme",
				TemplateName: "missing-template",
			},
		},
	}
	rta := NewRequiredTemplateApplier(k8s, walker, &deployments.CueRenderer{}, applier, res, policyresolver.NewNoopResolver())

	// ApplyRequiredTemplates will walk ancestors to build Folders, then
	// try to render the (nonexistent) template and fail. The error is
	// expected — the assertion below only cares that the walker was
	// consulted.
	_ = rta.ApplyRequiredTemplates(context.Background(), "acme", "new-prj", "prj-new-prj", nil)

	if walker.callCount < 1 {
		t.Errorf("expected WalkAncestors to be called at least once, got %d", walker.callCount)
	}
	// The walk starts from the new project's namespace so folder
	// ancestry is resolved relative to the right node.
	if walker.lastStartNs != "prj-new-prj" {
		t.Errorf("expected WalkAncestors start namespace %q, got %q",
			"prj-new-prj", walker.lastStartNs)
	}
}
