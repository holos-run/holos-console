package templatepolicies

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// walkGoFiles calls visit for each *.go source file under root (recursively).
// Generated and test files are skipped so the audit only scans hand-authored
// production code.
func walkGoFiles(t *testing.T, root string, visit func(path, body string)) error {
	t.Helper()
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		visit(path, string(data))
		return nil
	})
}

// stubOrgGrantResolver implements OrgGrantResolver for tests. It echoes
// per-user role grants for the organization passed to its constructor; folder
// lookups use stubFolderGrantResolver instead so tests can verify the handler
// picks the right cascade source.
type stubOrgGrantResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubOrgGrantResolver) GetOrgGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	return s.users, s.roles, s.err
}

type stubFolderGrantResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubFolderGrantResolver) GetFolderGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	return s.users, s.roles, s.err
}

// fakeTemplateResolver implements TemplateExistsResolver. The err field is
// checked *first*, before exists, so a test can assert the handler does not
// block on transient errors.
type fakeTemplateResolver struct {
	exists bool
	err    error
	calls  int
}

func (f *fakeTemplateResolver) TemplateExists(_ context.Context, _ consolev1.TemplateScope, _, _ string) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.exists, nil
}

func authedCtx(email string, roles []string) context.Context {
	return rpc.ContextWithClaims(context.Background(), &rpc.Claims{
		Sub:   "user-test",
		Email: email,
		Roles: roles,
	})
}

func newTestHandler(t *testing.T, shareUsers map[string]string) (*Handler, *fake.Clientset) {
	t.Helper()
	fakeClient := fake.NewClientset()
	r := newTestResolver()
	k := NewK8sClient(fakeClient, r)
	h := NewHandler(k, r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: shareUsers}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: shareUsers})
	return h, fakeClient
}

// newFolderScope and newOrgScope are short constructors for the proto types
// used in every table-driven case below.
func newFolderScope(name string) *consolev1.TemplateScopeRef {
	return &consolev1.TemplateScopeRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER,
		ScopeName: name,
	}
}

func newOrgScope(name string) *consolev1.TemplateScopeRef {
	return &consolev1.TemplateScopeRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
		ScopeName: name,
	}
}

func newProjectScope(name string) *consolev1.TemplateScopeRef {
	return &consolev1.TemplateScopeRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		ScopeName: name,
	}
}

func basicPolicy() *consolev1.TemplatePolicy {
	return &consolev1.TemplatePolicy{
		Name:        "require-httproute",
		DisplayName: "Require HTTPRoute",
		Description: "Force HTTPRoute for every project",
		Rules:       []*consolev1.TemplatePolicyRule{sampleRule()},
	}
}

// TestCreateRejectsProjectScope is the dedicated negative test called out by
// the HOL-556 acceptance criteria. The handler must refuse the request before
// any ConfigMap is written, and the InvalidArgument message must name the
// offending project namespace so operators can debug routing mistakes.
func TestCreateRejectsProjectScope(t *testing.T) {
	h, fakeClient := newTestHandler(t, map[string]string{"owner@example.com": "owner"})

	req := connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope:  newProjectScope("billing-web"),
		Policy: basicPolicy(),
	})
	ctx := authedCtx("owner@example.com", nil)

	_, err := h.CreateTemplatePolicy(ctx, req)
	if err == nil {
		t.Fatal("expected project-scope rejection")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
	}
	if want := "holos-prj-billing-web"; !containsString(err.Error(), want) {
		t.Errorf("expected error to name %q, got %v", want, err)
	}

	// Confirm nothing landed in the project namespace.
	cms, _ := fakeClient.CoreV1().ConfigMaps("holos-prj-billing-web").List(context.Background(), metav1.ListOptions{})
	if len(cms.Items) != 0 {
		t.Errorf("expected no ConfigMaps in project namespace, got %d", len(cms.Items))
	}
}

func TestReadPathsRejectProjectScope(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"viewer@example.com": "viewer"})
	ctx := authedCtx("viewer@example.com", nil)

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "list",
			run: func() error {
				_, err := h.ListTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListTemplatePoliciesRequest{
					Scope: newProjectScope("billing-web"),
				}))
				return err
			},
		},
		{
			name: "get",
			run: func() error {
				_, err := h.GetTemplatePolicy(ctx, connect.NewRequest(&consolev1.GetTemplatePolicyRequest{
					Scope: newProjectScope("billing-web"),
					Name:  "any",
				}))
				return err
			},
		},
		{
			name: "update",
			run: func() error {
				_, err := h.UpdateTemplatePolicy(ctx, connect.NewRequest(&consolev1.UpdateTemplatePolicyRequest{
					Scope:  newProjectScope("billing-web"),
					Policy: basicPolicy(),
				}))
				return err
			},
		},
		{
			name: "delete",
			run: func() error {
				_, err := h.DeleteTemplatePolicy(ctx, connect.NewRequest(&consolev1.DeleteTemplatePolicyRequest{
					Scope: newProjectScope("billing-web"),
					Name:  "any",
				}))
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("expected rejection")
			}
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Errorf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
			}
		})
	}
}

func TestCreatePolicyValidation(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ctx := authedCtx("owner@example.com", nil)

	tests := []struct {
		name    string
		policy  *consolev1.TemplatePolicy
		wantMsg string
	}{
		{
			name: "missing rules",
			policy: &consolev1.TemplatePolicy{
				Name: "empty",
			},
			wantMsg: "at least one rule",
		},
		{
			name: "invalid kind",
			policy: &consolev1.TemplatePolicy{
				Name: "bad-kind",
				Rules: []*consolev1.TemplatePolicyRule{
					{
						Kind: consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_UNSPECIFIED,
						Template: &consolev1.LinkedTemplateRef{
							Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
							ScopeName: "acme",
							Name:      "t",
						},
						Target: &consolev1.TemplatePolicyTarget{ProjectPattern: "*"},
					},
				},
			},
			wantMsg: "REQUIRE or EXCLUDE",
		},
		{
			name: "missing template.name",
			policy: &consolev1.TemplatePolicy{
				Name: "bad-ref",
				Rules: []*consolev1.TemplatePolicyRule{
					{
						Kind: consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE,
						Template: &consolev1.LinkedTemplateRef{
							Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
							ScopeName: "acme",
						},
						Target: &consolev1.TemplatePolicyTarget{ProjectPattern: "*"},
					},
				},
			},
			wantMsg: "template.name",
		},
		{
			name: "invalid project pattern",
			policy: &consolev1.TemplatePolicy{
				Name: "bad-glob",
				Rules: []*consolev1.TemplatePolicyRule{
					{
						Kind: consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE,
						Template: &consolev1.LinkedTemplateRef{
							Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
							ScopeName: "acme",
							Name:      "t",
						},
						Target: &consolev1.TemplatePolicyTarget{ProjectPattern: "[abc"},
					},
				},
			},
			wantMsg: "invalid project_pattern",
		},
		{
			name: "empty project pattern",
			policy: &consolev1.TemplatePolicy{
				Name: "empty-pattern",
				Rules: []*consolev1.TemplatePolicyRule{
					{
						Kind: consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE,
						Template: &consolev1.LinkedTemplateRef{
							Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
							ScopeName: "acme",
							Name:      "t",
						},
						Target: &consolev1.TemplatePolicyTarget{},
					},
				},
			},
			wantMsg: "project_pattern is required",
		},
		{
			name: "invalid name",
			policy: &consolev1.TemplatePolicy{
				Name:  "Bad_Name",
				Rules: []*consolev1.TemplatePolicyRule{sampleRule()},
			},
			wantMsg: "valid DNS label",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
				Scope:  newFolderScope("payments"),
				Policy: tt.policy,
			}))
			if err == nil {
				t.Fatal("expected validation error")
			}
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
			}
			if !containsString(err.Error(), tt.wantMsg) {
				t.Errorf("expected error to contain %q, got %v", tt.wantMsg, err)
			}
		})
	}
}

// TestCreatePolicyHappyPath walks the end-to-end create flow and verifies the
// audit trail and storage invariants.
func TestCreatePolicyHappyPath(t *testing.T) {
	h, fakeClient := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ctx := authedCtx("owner@example.com", nil)

	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope:  newFolderScope("payments"),
		Policy: basicPolicy(),
	}))
	if err != nil {
		t.Fatalf("CreateTemplatePolicy: %v", err)
	}
	cm, err := fakeClient.CoreV1().ConfigMaps("holos-fld-payments").Get(context.Background(), "require-httproute", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected ConfigMap: %v", err)
	}
	if cm.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeTemplatePolicy {
		t.Errorf("expected template-policy label, got %q", cm.Labels[v1alpha2.LabelResourceType])
	}
	if cm.Annotations[v1alpha2.AnnotationCreatorEmail] != "owner@example.com" {
		t.Errorf("expected creator annotation, got %q", cm.Annotations[v1alpha2.AnnotationCreatorEmail])
	}
}

func TestCreatePolicyPermissionDeniedForViewer(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"viewer@example.com": "viewer"})
	ctx := authedCtx("viewer@example.com", nil)

	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope:  newFolderScope("payments"),
		Policy: basicPolicy(),
	}))
	if err == nil {
		t.Fatal("expected permission denied")
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", connect.CodeOf(err))
	}
}

func TestCreatePolicyAtOrgScope(t *testing.T) {
	h, fakeClient := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ctx := authedCtx("owner@example.com", nil)

	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope:  newOrgScope("acme"),
		Policy: basicPolicy(),
	}))
	if err != nil {
		t.Fatalf("CreateTemplatePolicy at org scope: %v", err)
	}
	cm, err := fakeClient.CoreV1().ConfigMaps("holos-org-acme").Get(context.Background(), "require-httproute", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected ConfigMap in org namespace: %v", err)
	}
	if cm.Labels[v1alpha2.LabelTemplateScope] != v1alpha2.TemplateScopeOrganization {
		t.Errorf("expected organization scope label, got %q", cm.Labels[v1alpha2.LabelTemplateScope])
	}
}

// TestDeleteMissingPolicyReturnsNotFound asserts the kind-to-error mapping
// callers rely on to distinguish idempotent cleanup from a real failure.
func TestDeleteMissingPolicyReturnsNotFound(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ctx := authedCtx("owner@example.com", nil)

	_, err := h.DeleteTemplatePolicy(ctx, connect.NewRequest(&consolev1.DeleteTemplatePolicyRequest{
		Scope: newFolderScope("payments"),
		Name:  "does-not-exist",
	}))
	if err == nil {
		t.Fatal("expected NotFound error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", connect.CodeOf(err))
	}
}

// TestUpdatePreservesUnspecifiedFields verifies the display-name preservation
// semantic documented on Handler.UpdateTemplatePolicy.
func TestUpdatePreservesUnspecifiedFields(t *testing.T) {
	// Seed a policy via the K8s client directly so the test depends only on
	// the handler Update code path.
	fakeClient := fake.NewClientset()
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplatePolicy,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:         "Existing Name",
				v1alpha2.AnnotationDescription:         "Existing Desc",
				v1alpha2.AnnotationCreatorEmail:        "original@example.com",
				v1alpha2.AnnotationTemplatePolicyRules: `[]`,
			},
		},
	}
	if _, err := fakeClient.CoreV1().ConfigMaps("holos-fld-payments").Create(context.Background(), existing, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	r := newTestResolver()
	k := NewK8sClient(fakeClient, r)
	h := NewHandler(k, r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: map[string]string{"owner@example.com": "owner"}}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: map[string]string{"owner@example.com": "owner"}})

	ctx := authedCtx("owner@example.com", nil)
	_, err := h.UpdateTemplatePolicy(ctx, connect.NewRequest(&consolev1.UpdateTemplatePolicyRequest{
		Scope: newFolderScope("payments"),
		Policy: &consolev1.TemplatePolicy{
			Name:  "policy",
			Rules: []*consolev1.TemplatePolicyRule{sampleRule()},
		},
	}))
	if err != nil {
		t.Fatalf("UpdateTemplatePolicy: %v", err)
	}
	after, _ := fakeClient.CoreV1().ConfigMaps("holos-fld-payments").Get(context.Background(), "policy", metav1.GetOptions{})
	if after.Annotations[v1alpha2.AnnotationDisplayName] != "Existing Name" {
		t.Errorf("display name not preserved: %q", after.Annotations[v1alpha2.AnnotationDisplayName])
	}
	if after.Annotations[v1alpha2.AnnotationDescription] != "Existing Desc" {
		t.Errorf("description not preserved: %q", after.Annotations[v1alpha2.AnnotationDescription])
	}
	rules, err := unmarshalRules(after.Annotations[v1alpha2.AnnotationTemplatePolicyRules])
	if err != nil {
		t.Fatalf("unmarshalRules: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("expected rules replaced with 1 entry, got %d", len(rules))
	}
}

// TestTemplateExistsProbeDoesNotBlockOnTransientError confirms the
// best-effort contract: a probe error is logged but never blocks the write.
func TestTemplateExistsProbeDoesNotBlockOnTransientError(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	probe := &fakeTemplateResolver{err: errors.New("backend temporarily unavailable")}
	h.WithTemplateExistsResolver(probe)

	ctx := authedCtx("owner@example.com", nil)
	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope:  newFolderScope("payments"),
		Policy: basicPolicy(),
	}))
	if err != nil {
		t.Fatalf("transient probe error must not block create: %v", err)
	}
	if probe.calls == 0 {
		t.Errorf("expected probe to be invoked")
	}
}

// TestListPoliciesReturnsStoredRules round-trips through the full
// Create->List->configMapToPolicy path.
func TestListPoliciesReturnsStoredRules(t *testing.T) {
	h, fakeClient := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ctx := authedCtx("owner@example.com", nil)

	if _, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Scope:  newFolderScope("payments"),
		Policy: basicPolicy(),
	})); err != nil {
		t.Fatalf("CreateTemplatePolicy: %v", err)
	}

	resp, err := h.ListTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListTemplatePoliciesRequest{
		Scope: newFolderScope("payments"),
	}))
	if err != nil {
		t.Fatalf("ListTemplatePolicies: %v", err)
	}
	if len(resp.Msg.GetPolicies()) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(resp.Msg.GetPolicies()))
	}
	got := resp.Msg.GetPolicies()[0]
	if got.GetName() != "require-httproute" {
		t.Errorf("expected name=require-httproute, got %q", got.GetName())
	}
	if len(got.GetRules()) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(got.GetRules()))
	}
	if got.GetScopeRef().GetScope() != consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER {
		t.Errorf("expected folder scope, got %v", got.GetScopeRef().GetScope())
	}

	// List directly via the fake client to verify no project-namespace
	// artefacts leaked.
	projectCms, _ := fakeClient.CoreV1().ConfigMaps("holos-prj-payments-web").List(context.Background(), metav1.ListOptions{})
	if len(projectCms.Items) != 0 {
		t.Errorf("expected 0 ConfigMaps in any project namespace, got %d", len(projectCms.Items))
	}
}

// TestConsoleTemplatesHasNoRemainingMandatoryReads is a regression guard for
// the audit step in HOL-556: console/templates and console/projects must not
// read the removed Template.Mandatory proto field. The annotation may still
// be present on ConfigMaps (we keep it until HOL-557 drops it), but proto
// field accesses would indicate the `mandatory` shim came back.
func TestConsoleTemplatesHasNoRemainingMandatoryReads(t *testing.T) {
	// This test is defensive rather than exhaustive — it looks for common
	// shapes (`.GetMandatory()`, `tmpl.Mandatory`) that would signal a
	// regression. The full guarantee is enforced at proto compile time
	// because the field no longer exists on the generated Go type.
	paths := []string{"../templates", "../projects"}
	const target1 = ".GetMandatory("
	const target2 = ".Mandatory" // catches struct-literal field access too
	for _, p := range paths {
		walkErr := walkGoFiles(t, p, func(path, body string) {
			if containsString(body, target1) {
				t.Errorf("%s references %q; HOL-556 audit bans proto Mandatory reads", path, target1)
			}
			// Match the literal ".Mandatory" on its own (not MandatoryTemplate)
			// by looking for ".Mandatory" followed by end-of-line, space, or
			// one of a few punctuation chars.
			for _, suffix := range []string{".Mandatory ", ".Mandatory,", ".Mandatory)", ".Mandatory\n", ".Mandatory\t"} {
				if containsString(body, suffix) {
					t.Errorf("%s references %q; HOL-556 audit bans proto Mandatory reads", path, suffix)
					break
				}
			}
			_ = target2
		})
		if walkErr != nil {
			t.Fatalf("walking %s: %v", p, walkErr)
		}
	}
}

func containsString(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && stringContains(haystack, needle))
}

// stringContains avoids a strings import solely for Contains, keeping the
// test file's import surface lean.
func stringContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
