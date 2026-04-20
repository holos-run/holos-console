package templatepolicies

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
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

func (f *fakeTemplateResolver) TemplateExists(_ context.Context, _, _ string) (bool, error) {
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

// newTestHandler constructs a TemplatePolicyService handler wired against a
// fake controller-runtime client seeded with no TemplatePolicy CRs. The
// returned client is the shared fake, so tests can Get/List/Create CRs
// directly to assert on storage side effects without going through the
// handler. HOL-662 migrated the storage substrate from ConfigMaps to
// TemplatePolicy CRDs; the fake ctrlclient is a sufficient stand-in for
// the CRUD envtest coverage in k8s_test.go.
func newTestHandler(t *testing.T, shareUsers map[string]string) (*Handler, client.Client) {
	t.Helper()
	ctrlClient := newFakeCtrlClient(t)
	r := newTestResolver()
	k := NewK8sClient(ctrlClient, r)
	h := NewHandler(k, r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: shareUsers}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: shareUsers})
	return h, ctrlClient
}

// listPolicies collects every TemplatePolicy visible in the given namespace
// through the fake ctrlclient. Used in handler tests to assert on what the
// handler wrote (or did not write).
func listPolicies(t *testing.T, c client.Client, namespace string) []templatesv1alpha1.TemplatePolicy {
	t.Helper()
	var list templatesv1alpha1.TemplatePolicyList
	if err := c.List(context.Background(), &list, client.InNamespace(namespace)); err != nil {
		t.Fatalf("list policies in %q: %v", namespace, err)
	}
	return list.Items
}

// getPolicyCR retrieves a TemplatePolicy by namespace/name, returning nil
// if not found so tests can assert absence as easily as presence.
func getPolicyCR(t *testing.T, c client.Client, namespace, name string) *templatesv1alpha1.TemplatePolicy {
	t.Helper()
	var p templatesv1alpha1.TemplatePolicy
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, &p); err != nil {
		return nil
	}
	return &p
}

// newFolderScope and newOrgScope are short constructors for the namespace
// strings used in every table-driven case below. HOL-619 removed
// TemplateScopeRef from proto; HOL-723 retired scopeshim. The helpers now
// emit a namespace string produced by newTestResolver(); the handler
// classifies it back via resolver.ResourceTypeFromNamespace.
func newFolderScope(name string) string {
	return newTestResolver().FolderNamespace(name)
}

func newOrgScope(name string) string {
	return newTestResolver().OrgNamespace(name)
}

func newProjectScope(name string) string {
	return newTestResolver().ProjectNamespace(name)
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

	// Confirm nothing landed in the project namespace. The handler
	// validation layer short-circuits the request before any CRD write,
	// so the ctrlclient must not have a TemplatePolicy in the project
	// namespace. This is belt-and-suspenders against the CEL VAP that
	// enforces the same guarantee at admission time (HOL-618).
	if got := listPolicies(t, fakeClient, "holos-prj-billing-web"); len(got) != 0 {
		t.Errorf("expected no TemplatePolicy CRs in project namespace, got %d", len(got))
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
						Template: &consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "t"},
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
						Template: &consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: ""},
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
		{
			name: "foreign template namespace",
			policy: &consolev1.TemplatePolicy{
				Name: "foreign-ref",
				Rules: []*consolev1.TemplatePolicyRule{
					{
						Kind:     consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE,
						Template: &consolev1.LinkedTemplateRef{Namespace: "default", Name: "t"},
					},
				},
			},
			wantMsg: "not a console-managed",
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
	p := getPolicyCR(t, fakeClient, "holos-fld-payments", "require-httproute")
	if p == nil {
		t.Fatal("expected TemplatePolicy CRD to be created")
	}
	if p.Annotations[v1alpha2.AnnotationCreatorEmail] != "owner@example.com" {
		t.Errorf("expected creator annotation, got %q", p.Annotations[v1alpha2.AnnotationCreatorEmail])
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
	// HOL-662 dropped the LabelTemplateScope annotation — scope is now
	// derived from the namespace (holos-org-*) by every reader via the
	// package-level resolver. Assert on namespace instead.
	p := getPolicyCR(t, fakeClient, "holos-org-acme", "require-httproute")
	if p == nil {
		t.Fatal("expected TemplatePolicy CRD in org namespace")
	}
	if p.Namespace != "holos-org-acme" {
		t.Errorf("expected namespace=holos-org-acme, got %q", p.Namespace)
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
// semantic documented on Handler.UpdateTemplatePolicy. HOL-662 stores the
// policy as a TemplatePolicy CRD; display_name and description live on
// Spec, not annotations.
func TestUpdatePreservesUnspecifiedFields(t *testing.T) {
	// Seed a policy CRD directly so the test depends only on the handler
	// Update code path. HOL-662 persists display_name / description on
	// spec, so the fixture uses those.
	existing := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationCreatorEmail: "original@example.com",
			},
		},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			DisplayName: "Existing Name",
			Description: "Existing Desc",
			Rules:       nil,
		},
	}
	fakeClient := newFakeCtrlClient(t, existing)

	r := newTestResolver()
	k := NewK8sClient(fakeClient, r)
	h := NewHandler(k, r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: map[string]string{"owner@example.com": "owner"}}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: map[string]string{"owner@example.com": "owner"}})

	ctx := authedCtx("owner@example.com", nil)
	_, err := h.UpdateTemplatePolicy(ctx, connect.NewRequest(&consolev1.UpdateTemplatePolicyRequest{
		Namespace: newFolderScope("payments"),
		Policy: &consolev1.TemplatePolicy{
			Name:      "policy",
			Namespace: newFolderScope("payments"),
			Rules:     []*consolev1.TemplatePolicyRule{sampleRule()},
		},
	}))
	if err != nil {
		t.Fatalf("UpdateTemplatePolicy: %v", err)
	}
	after := getPolicyCR(t, fakeClient, "holos-fld-payments", "policy")
	if after == nil {
		t.Fatal("policy disappeared after update")
	}
	if after.Spec.DisplayName != "Existing Name" {
		t.Errorf("display name not preserved: %q", after.Spec.DisplayName)
	}
	if after.Spec.Description != "Existing Desc" {
		t.Errorf("description not preserved: %q", after.Spec.Description)
	}
	if len(after.Spec.Rules) != 1 {
		t.Errorf("expected rules replaced with 1 entry, got %d", len(after.Spec.Rules))
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
	if gotScope, _ := classifyNamespace(newTestResolver(), got.GetNamespace()); gotScope != scopeKindFolder {
		t.Errorf("expected folder scope from namespace %q, got scope=%v", got.GetNamespace(), gotScope)
	}

	// List directly via the fake ctrlclient to verify no project-namespace
	// artefacts leaked.
	if got := listPolicies(t, fakeClient, "holos-prj-payments-web"); len(got) != 0 {
		t.Errorf("expected 0 TemplatePolicy CRs in any project namespace, got %d", len(got))
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

	// Seed a legitimate folder-scope policy CRD so the Delete / Update
	// cases have something to attempt to mutate. Seed via the fake
	// ctrlclient so the creation is not itself gated by RBAC.
	existing := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "require-httproute",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationCreatorEmail: "platform@example.com",
			},
		},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			DisplayName: "Require HTTPRoute",
			Description: "Force HTTPRoute for every project",
			Rules:       nil,
		},
	}
	fakeClient := newFakeCtrlClient(t, existing)
	r := newTestResolver()
	k := NewK8sClient(fakeClient, r)
	h := NewHandler(k, r).
		WithOrgGrantResolver(orgResolver).
		WithFolderGrantResolver(folderResolver)

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
	// TemplatePolicy CR in the folder namespace must still be unchanged —
	// no rules rewrite, no deletion. This catches any future regression
	// in which a handler path accidentally writes before checkAccess.
	after := getPolicyCR(t, fakeClient, "holos-fld-payments", "require-httproute")
	if after == nil {
		t.Fatal("folder-scope policy unexpectedly missing after denied mutations")
	}
	if len(after.Spec.Rules) != 0 {
		t.Errorf("folder-scope policy rules mutated by denied caller: got %d rules want 0", len(after.Spec.Rules))
	}
	if got, want := after.Annotations[v1alpha2.AnnotationCreatorEmail], "platform@example.com"; got != want {
		t.Errorf("folder-scope policy creator annotation mutated by denied caller: got %q want %q", got, want)
	}

	// And no TemplatePolicy should have landed in any project namespace,
	// even though one of the Create cases targeted the project namespace
	// (handler's extractPolicyScope rejects it before any write).
	if got := listPolicies(t, fakeClient, "holos-prj-billing-web"); len(got) != 0 {
		t.Errorf("expected zero TemplatePolicy CRs in project namespace, got %d", len(got))
	}
}

// TestMapK8sError pins the taxonomy translation from the Kubernetes
// apimachinery error types to ConnectRPC codes. HOL-662 added IsInvalid
// handling so CEL ValidatingAdmissionPolicy rejections and CRD schema
// failures surface to the UI as InvalidArgument instead of the generic
// Internal fallback. The table is shared-shape with the binding
// handler's test in templatepolicybindings/handler_test.go.
func TestMapK8sError(t *testing.T) {
	gvk := schema.GroupKind{Group: "templates.holos.run", Kind: "TemplatePolicy"}
	cases := []struct {
		name string
		err  error
		want connect.Code
	}{
		{
			name: "not found maps to NotFound",
			err:  k8serrors.NewNotFound(schema.GroupResource{Group: gvk.Group, Resource: "templatepolicies"}, "missing"),
			want: connect.CodeNotFound,
		},
		{
			name: "already exists maps to AlreadyExists",
			err:  k8serrors.NewAlreadyExists(schema.GroupResource{Group: gvk.Group, Resource: "templatepolicies"}, "dupe"),
			want: connect.CodeAlreadyExists,
		},
		{
			name: "forbidden maps to PermissionDenied",
			err:  k8serrors.NewForbidden(schema.GroupResource{Group: gvk.Group, Resource: "templatepolicies"}, "nope", errors.New("rbac")),
			want: connect.CodePermissionDenied,
		},
		{
			name: "unauthorized maps to Unauthenticated",
			err:  k8serrors.NewUnauthorized("no token"),
			want: connect.CodeUnauthenticated,
		},
		{
			name: "bad request maps to InvalidArgument",
			err:  k8serrors.NewBadRequest("malformed"),
			want: connect.CodeInvalidArgument,
		},
		{
			name: "invalid (CEL VAP / CRD schema rejection) maps to InvalidArgument",
			err:  k8serrors.NewInvalid(gvk, "bad", nil),
			want: connect.CodeInvalidArgument,
		},
		{
			name: "generic error falls through to Internal",
			err:  errors.New("boom"),
			want: connect.CodeInternal,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapK8sError(tc.err)
			var cerr *connect.Error
			if !errors.As(got, &cerr) {
				t.Fatalf("expected *connect.Error, got %T: %v", got, got)
			}
			if cerr.Code() != tc.want {
				t.Errorf("code=%v want %v (err=%v)", cerr.Code(), tc.want, tc.err)
			}
		})
	}
}
