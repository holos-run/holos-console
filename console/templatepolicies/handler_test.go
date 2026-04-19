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
	"github.com/holos-run/holos-console/console/scopeshim"
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
//
// The resolver supports two modes. When perScope is non-nil it is consulted
// first and keyed by the org scope_name — this lets a single test install
// different grants for different scopes (e.g. seed an "owner" grant only on
// the project namespace while keeping folder / org namespaces empty). The
// fall-through fields users and roles preserve the simpler "one grant set for
// all scopes" behaviour the rest of the tests rely on.
type stubOrgGrantResolver struct {
	users    map[string]string
	roles    map[string]string
	perScope map[string]stubGrant
	err      error
}

// stubGrant is a (users, roles) pair keyed by scope name in the per-scope
// variants of the grant-resolver stubs.
type stubGrant struct {
	users map[string]string
	roles map[string]string
}

func (s *stubOrgGrantResolver) GetOrgGrants(_ context.Context, scopeName string) (map[string]string, map[string]string, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	if s.perScope != nil {
		if g, ok := s.perScope[scopeName]; ok {
			return g.users, g.roles, nil
		}
		return map[string]string{}, map[string]string{}, nil
	}
	return s.users, s.roles, nil
}

type stubFolderGrantResolver struct {
	users    map[string]string
	roles    map[string]string
	perScope map[string]stubGrant
	err      error
}

func (s *stubFolderGrantResolver) GetFolderGrants(_ context.Context, scopeName string) (map[string]string, map[string]string, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	if s.perScope != nil {
		if g, ok := s.perScope[scopeName]; ok {
			return g.users, g.roles, nil
		}
		return map[string]string{}, map[string]string{}, nil
	}
	return s.users, s.roles, nil
}

// fakeTemplateResolver implements TemplateExistsResolver. The err field is
// checked *first*, before exists, so a test can assert the handler does not
// block on transient errors.
type fakeTemplateResolver struct {
	exists bool
	err    error
	calls  int
}

func (f *fakeTemplateResolver) TemplateExists(_ context.Context, _ scopeshim.Scope, _, _ string) (bool, error) {
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

// newFolderScope and newOrgScope are short constructors for the namespace
// strings used in every table-driven case below. HOL-619 removed
// TemplateScopeRef from proto, so the helpers now return the Kubernetes
// namespace the scope resolves to. The request-time namespace field is
// the canonical scope discriminator; tests still classify it back to
// (scope, scopeName) inside the handler via the scopeshim resolver.
func newFolderScope(name string) string {
	return scopeshim.DefaultResolver().FolderNamespace(name)
}

func newOrgScope(name string) string {
	return scopeshim.DefaultResolver().OrgNamespace(name)
}

func newProjectScope(name string) string {
	return scopeshim.DefaultResolver().ProjectNamespace(name)
}

// basicPolicy builds a policy whose namespace matches the supplied request
// namespace. The outer request namespace and the embedded policy namespace
// must line up for the handler to accept the request (see
// validatePolicyNamespace); this helper keeps the invariant in one place
// instead of duplicating namespace construction at every call site.
func basicPolicy(namespace string) *consolev1.TemplatePolicy {
	return &consolev1.TemplatePolicy{
		Name:        "require-httproute",
		DisplayName: "Require HTTPRoute",
		Description: "Force HTTPRoute for every project",
		Namespace:   namespace,
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
		Namespace:  newProjectScope("billing-web"),
		Policy: basicPolicy(newProjectScope("billing-web")),
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
					Namespace: newProjectScope("billing-web"),
				}))
				return err
			},
		},
		{
			name: "get",
			run: func() error {
				_, err := h.GetTemplatePolicy(ctx, connect.NewRequest(&consolev1.GetTemplatePolicyRequest{
					Namespace: newProjectScope("billing-web"),
					Name:  "any",
				}))
				return err
			},
		},
		{
			name: "update",
			run: func() error {
				_, err := h.UpdateTemplatePolicy(ctx, connect.NewRequest(&consolev1.UpdateTemplatePolicyRequest{
					Namespace:  newProjectScope("billing-web"),
					Policy: basicPolicy(newProjectScope("billing-web")),
				}))
				return err
			},
		},
		{
			name: "delete",
			run: func() error {
				_, err := h.DeleteTemplatePolicy(ctx, connect.NewRequest(&consolev1.DeleteTemplatePolicyRequest{
					Namespace: newProjectScope("billing-web"),
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
						Template: scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, "acme", "t", ""),
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
						Template: scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, "acme", "", ""),
					},
				},
			},
			wantMsg: "template.name",
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
			// Ensure every fixture in this table carries the matching
			// namespace so the test isolates the field-level validation
			// under test (rules, names) from the namespace check covered
			// by TestCreatePolicyNamespaceValidation.
			if tt.policy != nil && tt.policy.Namespace == "" {
				tt.policy.Namespace = newFolderScope("payments")
			}
			_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
				Namespace:  newFolderScope("payments"),
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

// TestCreatePolicyNamespaceValidation covers the proto-contract check on
// policy.namespace. HOL-619 replaced the per-message TemplateScopeRef with
// a top-level namespace field that must match the request namespace.
// The reviewer called out that the handler previously accepted nil,
// mismatched, or project-scope scope_ref values and silently stored the
// policy under the outer request scope; the equivalent failure modes post
// HOL-619 are an empty or mismatched namespace.
func TestCreatePolicyNamespaceValidation(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ctx := authedCtx("owner@example.com", nil)

	tests := []struct {
		name     string
		reqNs    string
		polNs    string
		wantMsg  string
	}{
		{
			name:    "missing policy namespace",
			reqNs:   newFolderScope("payments"),
			polNs:   "",
			wantMsg: "policy.namespace",
		},
		{
			name:    "mismatched folder vs org",
			reqNs:   newFolderScope("payments"),
			polNs:   newOrgScope("payments"),
			wantMsg: "must match request namespace",
		},
		{
			name:    "mismatched folder names",
			reqNs:   newFolderScope("payments"),
			polNs:   newFolderScope("identity"),
			wantMsg: "must match request namespace",
		},
		{
			name:    "mismatched org names",
			reqNs:   newOrgScope("acme"),
			polNs:   newOrgScope("other"),
			wantMsg: "must match request namespace",
		},
		{
			name:    "project-scope policy at folder request",
			reqNs:   newFolderScope("payments"),
			polNs:   newProjectScope("billing-web"),
			wantMsg: "must match request namespace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
				Namespace: tt.reqNs,
				Policy:    basicPolicy(tt.polNs),
			}))
			if err == nil {
				t.Fatal("expected namespace validation error")
			}
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Errorf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
			}
			if !containsString(err.Error(), tt.wantMsg) {
				t.Errorf("expected error to contain %q, got %v", tt.wantMsg, err)
			}
		})
	}

	t.Run("valid matching folder namespace is accepted", func(t *testing.T) {
		_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
			Namespace: newFolderScope("payments"),
			Policy:    basicPolicy(newFolderScope("payments")),
		}))
		if err != nil {
			t.Fatalf("matching folder namespace must be accepted: %v", err)
		}
	})
}

// TestUpdatePolicyNamespaceValidation mirrors the create-path coverage for
// UpdateTemplatePolicy. The update path is independently vulnerable because
// callers commonly round-trip a proto Policy fetched from Get and could edit
// policy.namespace to escape the outer namespace guard.
func TestUpdatePolicyNamespaceValidation(t *testing.T) {
	// Seed a policy we can legitimately update in the happy-path subtest.
	h, fakeClient := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ctx := authedCtx("owner@example.com", nil)
	if _, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Namespace: newFolderScope("payments"),
		Policy:    basicPolicy(newFolderScope("payments")),
	})); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	_ = fakeClient

	update := func(reqNs, polNs string) error {
		_, err := h.UpdateTemplatePolicy(ctx, connect.NewRequest(&consolev1.UpdateTemplatePolicyRequest{
			Namespace: reqNs,
			Policy: &consolev1.TemplatePolicy{
				Name:      "require-httproute",
				Namespace: polNs,
				Rules:     []*consolev1.TemplatePolicyRule{sampleRule()},
			},
		}))
		return err
	}

	tests := []struct {
		name    string
		reqNs   string
		polNs   string
		wantMsg string
	}{
		{
			name:    "missing policy namespace",
			reqNs:   newFolderScope("payments"),
			polNs:   "",
			wantMsg: "policy.namespace",
		},
		{
			name:    "mismatched scope kind",
			reqNs:   newFolderScope("payments"),
			polNs:   newOrgScope("payments"),
			wantMsg: "must match request namespace",
		},
		{
			name:    "mismatched folder name",
			reqNs:   newFolderScope("payments"),
			polNs:   newFolderScope("identity"),
			wantMsg: "must match request namespace",
		},
		{
			name:    "project-scope policy",
			reqNs:   newFolderScope("payments"),
			polNs:   newProjectScope("billing-web"),
			wantMsg: "must match request namespace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := update(tt.reqNs, tt.polNs)
			if err == nil {
				t.Fatal("expected namespace validation error")
			}
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Errorf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
			}
			if !containsString(err.Error(), tt.wantMsg) {
				t.Errorf("expected error to contain %q, got %v", tt.wantMsg, err)
			}
		})
	}

	t.Run("valid matching folder scope is accepted", func(t *testing.T) {
		if err := update(newFolderScope("payments"), newFolderScope("payments")); err != nil {
			t.Fatalf("matching folder scope_ref must be accepted on update: %v", err)
		}
	})
}

// TestCreatePolicyHappyPath walks the end-to-end create flow and verifies the
// audit trail and storage invariants.
func TestCreatePolicyHappyPath(t *testing.T) {
	h, fakeClient := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ctx := authedCtx("owner@example.com", nil)

	_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
		Namespace:  newFolderScope("payments"),
		Policy: basicPolicy(newFolderScope("payments")),
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
		Namespace:  newFolderScope("payments"),
		Policy: basicPolicy(newFolderScope("payments")),
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
		Namespace:  newOrgScope("acme"),
		Policy: basicPolicy(newOrgScope("acme")),
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
		Namespace: newFolderScope("payments"),
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
		Namespace: newFolderScope("payments"),
		Policy: &consolev1.TemplatePolicy{
			Name:     "policy",
			Namespace: newFolderScope("payments"),
			Rules:    []*consolev1.TemplatePolicyRule{sampleRule()},
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
		Namespace:  newFolderScope("payments"),
		Policy: basicPolicy(newFolderScope("payments")),
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
		Namespace:  newFolderScope("payments"),
		Policy: basicPolicy(newFolderScope("payments")),
	})); err != nil {
		t.Fatalf("CreateTemplatePolicy: %v", err)
	}

	resp, err := h.ListTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListTemplatePoliciesRequest{
		Namespace: newFolderScope("payments"),
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
	if gotScope, _, err := scopeshim.FromNamespace(scopeshim.DefaultResolver(), got.GetNamespace()); err != nil || gotScope != scopeshim.ScopeFolder {
		t.Errorf("expected folder scope from namespace %q, got scope=%v err=%v", got.GetNamespace(), gotScope, err)
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
// read the removed Template.Mandatory proto field. The annotation key may
// still linger on older ConfigMaps in the wild, but any proto field access
// would indicate the `mandatory` shim came back.
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

// TestProjectOwnerCannotMutatePolicy is the storage-isolation negative test
// called out by HOL-554 AC "A negative test in HOL-560 verifies a project-owner
// role cannot mutate any policy ConfigMap or policy-enforcement annotation."
//
// The scenario: a user holds an owner grant *only* on a project (typical PaaS
// customer-persona grant) and has NO grant at any ancestor folder or
// organization. The TemplatePolicyService handler must refuse every mutation
// path at every reachable scope, so there is no way for such a user to create,
// update, or delete a policy — regardless of whether they aim the request at
// their own project namespace (rejected as InvalidArgument by the proto /
// scope guard) or at the owning folder / organization namespace (rejected as
// PermissionDenied because the folder / org grant resolver carries no grant
// for this user).
//
// To make the test a genuine regression guard rather than a trivially-passing
// "unauthenticated user is denied" check, the grant resolvers are wired to
// return an actual "owner" grant for the user — but only under the project
// scope name. If a future refactor accidentally routes folder / org RBAC
// checks through the project-scope grant table (or otherwise elevates
// project-scoped ownership to folder/org writes on TemplatePolicy), the folder
// / org cases below would flip from PermissionDenied to success and this test
// would fail. Keeping the folder and org perScope maps empty for the queried
// scope names ("payments", "acme") proves the existing handler does NOT
// consult project grants when authorizing folder/org policy mutations.
//
// The user also carries a project-scoped role claim ("project-owner") in
// their JWT-like Claims.Roles. No folder / org shareRoles map contains that
// claim, so BestRoleFromGrants must not elevate the user via the role claim.
// A regression that made folder / org shareRoles contain project-level role
// mappings would again fail this test.
//
// This closes the loop on the HOL-554 storage-isolation guardrail: storage is
// restricted to folder / org namespaces by construction (project scope is
// rejected up front), and write access to those namespaces is gated by the
// TemplatePolicyCascadePerms table which never awards WRITE / DELETE to a
// project-scoped grant. The test asserts both halves.
func TestProjectOwnerCannotMutatePolicy(t *testing.T) {
	const projectOwnerEmail = "project-owner@example.com"
	const projectScopeName = "billing-web"
	const folderScopeName = "payments"
	const orgScopeName = "acme"
	const projectOwnerRoleClaim = "project-owner"

	// Simulate a genuine project-owner grant. The org and folder resolvers
	// are per-scope-keyed: for the folder and org scope names queried by
	// the handler below (payments / acme) they return empty grants, so the
	// TemplatePolicyCascadePerms lookup yields RoleUnspecified and denies.
	// For the project scope name (billing-web) the resolvers *do* carry a
	// real owner grant — the handler must never reach this entry because
	// TemplatePolicyService does not authorize writes against the project
	// scope. If a regression ever wired checkFolderAccess or checkOrgAccess
	// to the project scope (or extended the project scope grant into folder
	// / org cascade tables), the test cases below would begin to succeed
	// and this test would fail, catching the regression.
	orgResolver := &stubOrgGrantResolver{
		perScope: map[string]stubGrant{
			orgScopeName:     {users: map[string]string{}, roles: map[string]string{}},
			projectScopeName: {users: map[string]string{projectOwnerEmail: "owner"}},
		},
	}
	folderResolver := &stubFolderGrantResolver{
		perScope: map[string]stubGrant{
			folderScopeName:  {users: map[string]string{}, roles: map[string]string{}},
			projectScopeName: {users: map[string]string{projectOwnerEmail: "owner"}},
		},
	}

	fakeClient := fake.NewClientset()
	r := newTestResolver()
	k := NewK8sClient(fakeClient, r)
	h := NewHandler(k, r).
		WithOrgGrantResolver(orgResolver).
		WithFolderGrantResolver(folderResolver)

	// Seed a legitimate folder-scope policy so the Delete / Update cases have
	// something to attempt to mutate. Seed directly via the k8s client so the
	// creation is not itself gated by RBAC.
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "require-httproute",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplatePolicy,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:         "Require HTTPRoute",
				v1alpha2.AnnotationDescription:         "Force HTTPRoute for every project",
				v1alpha2.AnnotationCreatorEmail:        "platform@example.com",
				v1alpha2.AnnotationTemplatePolicyRules: `[]`,
			},
		},
	}
	if _, err := fakeClient.CoreV1().ConfigMaps("holos-fld-payments").Create(context.Background(), existing, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seeding folder-scope policy: %v", err)
	}

	// The user carries a project-owner role claim in their JWT-style
	// Claims.Roles. Folder / org shareRoles maps are empty, so the role
	// claim must not grant access at those scopes.
	ctx := authedCtx(projectOwnerEmail, []string{projectOwnerRoleClaim})

	type mutation struct {
		name    string
		run     func() error
		wantErr connect.Code
	}
	cases := []mutation{
		{
			name: "Create at folder scope (no folder grant)",
			run: func() error {
				_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
					Namespace:  newFolderScope("payments"),
					Policy: basicPolicy(newFolderScope("payments")),
				}))
				return err
			},
			wantErr: connect.CodePermissionDenied,
		},
		{
			name: "Create at organization scope (no org grant)",
			run: func() error {
				_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
					Namespace:  newOrgScope("acme"),
					Policy: basicPolicy(newOrgScope("acme")),
				}))
				return err
			},
			wantErr: connect.CodePermissionDenied,
		},
		{
			name: "Create targeting project namespace (storage-isolation rejection)",
			run: func() error {
				_, err := h.CreateTemplatePolicy(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyRequest{
					Namespace:  newProjectScope("billing-web"),
					Policy: basicPolicy(newProjectScope("billing-web")),
				}))
				return err
			},
			wantErr: connect.CodeInvalidArgument,
		},
		{
			name: "Update folder-scope policy (no folder grant)",
			run: func() error {
				_, err := h.UpdateTemplatePolicy(ctx, connect.NewRequest(&consolev1.UpdateTemplatePolicyRequest{
					Namespace: newFolderScope("payments"),
					Policy: &consolev1.TemplatePolicy{
						Name:     "require-httproute",
						Namespace: newFolderScope("payments"),
						Rules:    []*consolev1.TemplatePolicyRule{sampleRule()},
					},
				}))
				return err
			},
			wantErr: connect.CodePermissionDenied,
		},
		{
			name: "Delete folder-scope policy (no folder grant)",
			run: func() error {
				_, err := h.DeleteTemplatePolicy(ctx, connect.NewRequest(&consolev1.DeleteTemplatePolicyRequest{
					Namespace: newFolderScope("payments"),
					Name:  "require-httproute",
				}))
				return err
			},
			wantErr: connect.CodePermissionDenied,
		},
		{
			name: "Delete targeting project namespace (storage-isolation rejection)",
			run: func() error {
				_, err := h.DeleteTemplatePolicy(ctx, connect.NewRequest(&consolev1.DeleteTemplatePolicyRequest{
					Namespace: newProjectScope("billing-web"),
					Name:  "any",
				}))
				return err
			},
			wantErr: connect.CodeInvalidArgument,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil {
				t.Fatalf("expected error, got nil (project-owner must not be able to mutate policies)")
			}
			if got := connect.CodeOf(err); got != tc.wantErr {
				t.Errorf("expected %v, got %v: %v", tc.wantErr, got, err)
			}
		})
	}

	// Belt-and-suspenders: after all the mutation attempts, the seeded
	// ConfigMap in the folder namespace must still be unchanged — no rules
	// annotation rewrite, no deletion. This catches any future regression in
	// which a handler path accidentally writes before checkAccess.
	after, err := fakeClient.CoreV1().ConfigMaps("holos-fld-payments").Get(context.Background(), "require-httproute", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("folder-scope policy unexpectedly missing after denied mutations: %v", err)
	}
	if got, want := after.Annotations[v1alpha2.AnnotationTemplatePolicyRules], `[]`; got != want {
		t.Errorf("folder-scope policy rules annotation mutated by denied caller: got %q want %q", got, want)
	}
	if got, want := after.Annotations[v1alpha2.AnnotationCreatorEmail], "platform@example.com"; got != want {
		t.Errorf("folder-scope policy creator annotation mutated by denied caller: got %q want %q", got, want)
	}

	// And no ConfigMap should have landed in any project namespace, even
	// though one of the Create cases targeted TEMPLATE_SCOPE_PROJECT.
	projectCms, _ := fakeClient.CoreV1().ConfigMaps("holos-prj-billing-web").List(context.Background(), metav1.ListOptions{})
	if len(projectCms.Items) != 0 {
		t.Errorf("expected zero ConfigMaps in project namespace, got %d", len(projectCms.Items))
	}
}
