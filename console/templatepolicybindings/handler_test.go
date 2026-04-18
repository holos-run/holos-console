package templatepolicybindings

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// stubOrgGrantResolver echoes per-user role grants for the organization
// passed to its constructor. The two stubs below mirror the shape used in
// console/templatepolicies/handler_test.go so readers can move between the
// two tests without relearning the scaffolding.
type stubOrgGrantResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubOrgGrantResolver) GetOrgGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.users, s.roles, nil
}

type stubFolderGrantResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubFolderGrantResolver) GetFolderGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.users, s.roles, nil
}

// stubPolicyResolver implements PolicyExistsResolver. Tests that pin
// existence set `exists` directly; tests that want to simulate a transient
// probe failure set `err`.
type stubPolicyResolver struct {
	exists bool
	err    error
	calls  int
}

func (s *stubPolicyResolver) PolicyExists(_ context.Context, _ consolev1.TemplateScope, _, _ string) (bool, error) {
	s.calls++
	if s.err != nil {
		return false, s.err
	}
	return s.exists, nil
}

// stubAncestorResolver implements AncestorChainResolver. `contains` drives
// the boolean answer; `err` simulates a walker failure.
type stubAncestorResolver struct {
	contains bool
	err      error
	calls    int
}

func (s *stubAncestorResolver) AncestorChainContains(_ context.Context, _, _ string) (bool, error) {
	s.calls++
	if s.err != nil {
		return false, s.err
	}
	return s.contains, nil
}

// stubProjectResolver implements ProjectExistsResolver. `exists` is checked
// first so a test can return false without also setting err.
type stubProjectResolver struct {
	exists map[string]bool
	err    error
	calls  int
}

func (s *stubProjectResolver) ProjectExists(_ context.Context, _ consolev1.TemplateScope, _, project string) (bool, error) {
	s.calls++
	if s.err != nil {
		return false, s.err
	}
	if s.exists == nil {
		return true, nil
	}
	return s.exists[project], nil
}

func authedCtx(email string, roles []string) context.Context {
	return rpc.ContextWithClaims(context.Background(), &rpc.Claims{
		Sub:   "user-test",
		Email: email,
		Roles: roles,
	})
}

// newTestHandler builds a Handler wired to fake K8s and grant resolvers that
// return the supplied `shareUsers` map for both org and folder lookups.
// Tests that need to override individual resolvers can do so by calling the
// `With*` builders on the returned Handler.
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

// newFolderScopeRef, newOrgScopeRef, and newProjectScopeRef are short
// constructors for proto types used in every table-driven case below.
func newFolderScopeRef(name string) *consolev1.TemplateScopeRef {
	return &consolev1.TemplateScopeRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER,
		ScopeName: name,
	}
}

func newOrgScopeRef(name string) *consolev1.TemplateScopeRef {
	return &consolev1.TemplateScopeRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
		ScopeName: name,
	}
}

func newProjectScopeRef(name string) *consolev1.TemplateScopeRef {
	return &consolev1.TemplateScopeRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		ScopeName: name,
	}
}

// basicBinding builds a binding whose scope_ref matches the supplied
// request scope with a default policy_ref + single target_ref. The outer
// request scope and the embedded binding scope must line up for the
// handler to accept the request; this helper keeps the invariant in one
// place.
func basicBinding(scope *consolev1.TemplateScopeRef) *consolev1.TemplatePolicyBinding {
	return &consolev1.TemplatePolicyBinding{
		Name:        "bind-reference-grant",
		DisplayName: "Bind reference grant",
		Description: "Attach reference-grant policy to payments-web",
		ScopeRef:    scope,
		PolicyRef: &consolev1.LinkedTemplatePolicyRef{
			ScopeRef: newOrgScopeRef("acme"),
			Name:     "require-reference-grant",
		},
		TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{
			{
				Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
				Name:        "api",
				ProjectName: "payments-web",
			},
		},
	}
}

func containsString(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

// TestCreateRejectsProjectScope is the dedicated negative test called out
// by the HOL-595 acceptance criteria. The handler must refuse the request
// before any ConfigMap is written, and the InvalidArgument message must
// name the offending project namespace so operators can debug routing
// mistakes.
func TestCreateRejectsProjectScope(t *testing.T) {
	h, fakeClient := newTestHandler(t, map[string]string{"owner@example.com": "owner"})

	req := connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Scope:   newProjectScopeRef("billing-web"),
		Binding: basicBinding(newProjectScopeRef("billing-web")),
	})
	ctx := authedCtx("owner@example.com", nil)

	_, err := h.CreateTemplatePolicyBinding(ctx, req)
	if err == nil {
		t.Fatal("expected project-scope rejection")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
	}
	if want := "holos-prj-billing-web"; !containsString(err.Error(), want) {
		t.Errorf("expected error to name %q, got %v", want, err)
	}

	cms, _ := fakeClient.CoreV1().ConfigMaps("holos-prj-billing-web").List(context.Background(), metav1.ListOptions{})
	if len(cms.Items) != 0 {
		t.Errorf("expected no ConfigMaps in project namespace, got %d", len(cms.Items))
	}
}

// TestReadPathsRejectProjectScope covers the symmetric case for the read
// paths. A project-scope request on Get, List, Update, or Delete must
// fail without performing a K8s round trip so probing a project
// namespace cannot leak data.
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
				_, err := h.ListTemplatePolicyBindings(ctx, connect.NewRequest(&consolev1.ListTemplatePolicyBindingsRequest{
					Scope: newProjectScopeRef("billing-web"),
				}))
				return err
			},
		},
		{
			name: "get",
			run: func() error {
				_, err := h.GetTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.GetTemplatePolicyBindingRequest{
					Scope: newProjectScopeRef("billing-web"),
					Name:  "any",
				}))
				return err
			},
		},
		{
			name: "update",
			run: func() error {
				_, err := h.UpdateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.UpdateTemplatePolicyBindingRequest{
					Scope:   newProjectScopeRef("billing-web"),
					Binding: basicBinding(newProjectScopeRef("billing-web")),
				}))
				return err
			},
		},
		{
			name: "delete",
			run: func() error {
				_, err := h.DeleteTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.DeleteTemplatePolicyBindingRequest{
					Scope: newProjectScopeRef("billing-web"),
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

// TestCreateHappyPath exercises the full happy path: authenticated owner,
// valid folder scope, folder-local policy reference, one deployment target
// under an existing project. Verifies a ConfigMap lands in the folder
// namespace with the expected labels and annotations, and the response
// carries the binding name.
func TestCreateHappyPath(t *testing.T) {
	h, fakeClient := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	h = h.
		WithPolicyExistsResolver(&stubPolicyResolver{exists: true}).
		WithProjectExistsResolver(&stubProjectResolver{})

	ctx := authedCtx("owner@example.com", nil)
	folder := newFolderScopeRef("payments")
	binding := basicBinding(folder)
	// Override policy_ref to stay in the same folder so the reachability
	// check does not depend on the ancestor resolver.
	binding.PolicyRef = &consolev1.LinkedTemplatePolicyRef{
		ScopeRef: folder,
		Name:     "local-policy",
	}

	resp, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Scope:   folder,
		Binding: binding,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.GetName() != binding.GetName() {
		t.Errorf("expected response name %q, got %q", binding.GetName(), resp.Msg.GetName())
	}

	cm, err := fakeClient.CoreV1().ConfigMaps("holos-fld-payments").Get(context.Background(), binding.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("fetching created binding: %v", err)
	}
	if cm.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeTemplatePolicyBinding {
		t.Errorf("expected resource type label, got %q", cm.Labels[v1alpha2.LabelResourceType])
	}
	if cm.Annotations[v1alpha2.AnnotationCreatorEmail] != "owner@example.com" {
		t.Errorf("expected creator email annotation to be set from claims, got %q", cm.Annotations[v1alpha2.AnnotationCreatorEmail])
	}
}

// TestCreateRBACDenial confirms a caller with no grants sees
// PermissionDenied and never writes a ConfigMap. The stub grant resolver
// returns an empty map so the cascade evaluation yields
// RoleUnspecified -> no permissions.
func TestCreateRBACDenial(t *testing.T) {
	h, fakeClient := newTestHandler(t, map[string]string{}) // no grants
	h = h.
		WithPolicyExistsResolver(&stubPolicyResolver{exists: true}).
		WithProjectExistsResolver(&stubProjectResolver{})

	ctx := authedCtx("nobody@example.com", nil)
	folder := newFolderScopeRef("payments")
	binding := basicBinding(folder)
	binding.PolicyRef = &consolev1.LinkedTemplatePolicyRef{
		ScopeRef: folder,
		Name:     "local-policy",
	}

	_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Scope:   folder,
		Binding: binding,
	}))
	if err == nil {
		t.Fatal("expected permission denied")
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected CodePermissionDenied, got %v: %v", connect.CodeOf(err), err)
	}

	cms, _ := fakeClient.CoreV1().ConfigMaps("holos-fld-payments").List(context.Background(), metav1.ListOptions{})
	if len(cms.Items) != 0 {
		t.Errorf("expected no ConfigMaps on RBAC denial, got %d", len(cms.Items))
	}
}

// TestCreateUnauthenticated confirms a missing Claims context yields
// Unauthenticated — not a silent no-op. The handler must reject an
// unauthenticated call *after* the proto-shape validation so the error
// hierarchy surfaces the right reason.
func TestCreateUnauthenticated(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	folder := newFolderScopeRef("payments")
	binding := basicBinding(folder)
	binding.PolicyRef = &consolev1.LinkedTemplatePolicyRef{
		ScopeRef: folder,
		Name:     "local-policy",
	}

	_, err := h.CreateTemplatePolicyBinding(context.Background(), connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Scope:   folder,
		Binding: binding,
	}))
	if err == nil {
		t.Fatal("expected unauthenticated rejection")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("expected CodeUnauthenticated, got %v: %v", connect.CodeOf(err), err)
	}
}

// TestCreateValidation is the main table-driven proto-shape validation
// test. Every case here must be rejected with CodeInvalidArgument before
// any K8s write occurs — the fake clientset is ignored by these cases
// because the handler should fail up front.
func TestCreateValidation(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	h = h.
		WithPolicyExistsResolver(&stubPolicyResolver{exists: true}).
		WithProjectExistsResolver(&stubProjectResolver{})
	ctx := authedCtx("owner@example.com", nil)

	folder := newFolderScopeRef("payments")
	validPolicyRef := func() *consolev1.LinkedTemplatePolicyRef {
		return &consolev1.LinkedTemplatePolicyRef{
			ScopeRef: folder,
			Name:     "local-policy",
		}
	}
	validTarget := func() *consolev1.TemplatePolicyBindingTargetRef {
		return &consolev1.TemplatePolicyBindingTargetRef{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
			Name:        "api",
			ProjectName: "payments-web",
		}
	}

	tests := []struct {
		name    string
		binding *consolev1.TemplatePolicyBinding
		wantMsg string
	}{
		{
			name: "missing policy_ref",
			binding: &consolev1.TemplatePolicyBinding{
				Name:       "bad",
				ScopeRef:   folder,
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{validTarget()},
			},
			wantMsg: "policy_ref is required",
		},
		{
			name: "policy_ref missing scope_ref",
			binding: &consolev1.TemplatePolicyBinding{
				Name:       "bad",
				ScopeRef:   folder,
				PolicyRef:  &consolev1.LinkedTemplatePolicyRef{Name: "x"},
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{validTarget()},
			},
			wantMsg: "policy_ref.scope_ref is required",
		},
		{
			name: "policy_ref project scope",
			binding: &consolev1.TemplatePolicyBinding{
				Name:     "bad",
				ScopeRef: folder,
				PolicyRef: &consolev1.LinkedTemplatePolicyRef{
					ScopeRef: newProjectScopeRef("p"),
					Name:     "x",
				},
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{validTarget()},
			},
			wantMsg: "policy_ref.scope_ref cannot be TEMPLATE_SCOPE_PROJECT",
		},
		{
			name: "target_refs empty",
			binding: &consolev1.TemplatePolicyBinding{
				Name:       "bad",
				ScopeRef:   folder,
				PolicyRef:  validPolicyRef(),
				TargetRefs: nil,
			},
			wantMsg: "at least one target_ref",
		},
		{
			name: "target kind unspecified",
			binding: &consolev1.TemplatePolicyBinding{
				Name:      "bad",
				ScopeRef:  folder,
				PolicyRef: validPolicyRef(),
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{
					{
						Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_UNSPECIFIED,
						Name:        "api",
						ProjectName: "payments-web",
					},
				},
			},
			wantMsg: "PROJECT_TEMPLATE or DEPLOYMENT",
		},
		{
			name: "target name missing",
			binding: &consolev1.TemplatePolicyBinding{
				Name:      "bad",
				ScopeRef:  folder,
				PolicyRef: validPolicyRef(),
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{
					{
						Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
						ProjectName: "payments-web",
					},
				},
			},
			wantMsg: "name is required",
		},
		{
			name: "target project_name invalid DNS label",
			binding: &consolev1.TemplatePolicyBinding{
				Name:      "bad",
				ScopeRef:  folder,
				PolicyRef: validPolicyRef(),
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{
					{
						Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
						Name:        "api",
						ProjectName: "Bad_Project",
					},
				},
			},
			wantMsg: "project_name must be a valid DNS label",
		},
		{
			name: "target name invalid DNS label",
			binding: &consolev1.TemplatePolicyBinding{
				Name:      "bad",
				ScopeRef:  folder,
				PolicyRef: validPolicyRef(),
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{
					{
						Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
						Name:        "Bad_Name",
						ProjectName: "payments-web",
					},
				},
			},
			wantMsg: "name must be a valid DNS label",
		},
		{
			name: "target project_name missing",
			binding: &consolev1.TemplatePolicyBinding{
				Name:      "bad",
				ScopeRef:  folder,
				PolicyRef: validPolicyRef(),
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{
					{
						Kind: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
						Name: "api",
					},
				},
			},
			wantMsg: "project_name is required",
		},
		{
			name: "duplicate target triples",
			binding: &consolev1.TemplatePolicyBinding{
				Name:      "bad",
				ScopeRef:  folder,
				PolicyRef: validPolicyRef(),
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{
					validTarget(),
					validTarget(),
				},
			},
			wantMsg: "duplicate of target_refs[0]",
		},
		{
			name: "invalid name",
			binding: &consolev1.TemplatePolicyBinding{
				Name:       "Bad_Name",
				ScopeRef:   folder,
				PolicyRef:  validPolicyRef(),
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{validTarget()},
			},
			wantMsg: "valid DNS label",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
				Scope:   folder,
				Binding: tt.binding,
			}))
			if err == nil {
				t.Fatal("expected validation error")
			}
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Errorf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
			}
			if !containsString(err.Error(), tt.wantMsg) {
				t.Errorf("expected error to contain %q, got %v", tt.wantMsg, err)
			}
		})
	}
}

// TestCreateScopeRefMismatch covers the proto-contract check on
// binding.scope_ref. The reviewer called out in the sibling
// templatepolicies handler tests that the handler previously accepted
// mismatched scope_ref values; the binding handler must fail the same
// way so a stored binding cannot claim a scope different from its
// actual namespace.
func TestCreateScopeRefMismatch(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	h = h.
		WithPolicyExistsResolver(&stubPolicyResolver{exists: true}).
		WithProjectExistsResolver(&stubProjectResolver{})
	ctx := authedCtx("owner@example.com", nil)

	tests := []struct {
		name    string
		reqRef  *consolev1.TemplateScopeRef
		bindRef *consolev1.TemplateScopeRef
		wantMsg string
	}{
		{
			name:    "nil binding scope_ref",
			reqRef:  newFolderScopeRef("payments"),
			bindRef: nil,
			wantMsg: "binding.scope_ref is required",
		},
		{
			name:    "mismatched folder name",
			reqRef:  newFolderScopeRef("payments"),
			bindRef: newFolderScopeRef("identity"),
			wantMsg: "must match request scope",
		},
		{
			name:    "mismatched org vs folder",
			reqRef:  newFolderScopeRef("payments"),
			bindRef: newOrgScopeRef("acme"),
			wantMsg: "must match request scope",
		},
		{
			name:    "project scope_ref at folder request",
			reqRef:  newFolderScopeRef("payments"),
			bindRef: newProjectScopeRef("payments-web"),
			wantMsg: "cannot be TEMPLATE_SCOPE_PROJECT",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := basicBinding(tt.bindRef)
			b.PolicyRef = &consolev1.LinkedTemplatePolicyRef{
				ScopeRef: tt.reqRef,
				Name:     "p",
			}
			_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
				Scope:   tt.reqRef,
				Binding: b,
			}))
			if err == nil {
				t.Fatal("expected rejection")
			}
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Errorf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
			}
			if !containsString(err.Error(), tt.wantMsg) {
				t.Errorf("expected error to contain %q, got %v", tt.wantMsg, err)
			}
		})
	}
}

// TestCreateRejectsMissingPolicy validates that a policy_ref pointing at a
// policy the PolicyExistsResolver says does not exist is rejected with
// CodeInvalidArgument. This is the HOL-595 "missing policy_ref rejection"
// acceptance-criteria item.
func TestCreateRejectsMissingPolicy(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	missing := &stubPolicyResolver{exists: false}
	h = h.
		WithPolicyExistsResolver(missing).
		WithProjectExistsResolver(&stubProjectResolver{})
	ctx := authedCtx("owner@example.com", nil)

	folder := newFolderScopeRef("payments")
	binding := basicBinding(folder)
	binding.PolicyRef = &consolev1.LinkedTemplatePolicyRef{
		ScopeRef: folder,
		Name:     "does-not-exist",
	}

	_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Scope:   folder,
		Binding: binding,
	}))
	if err == nil {
		t.Fatal("expected missing-policy rejection")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
	}
	if !containsString(err.Error(), "unknown policy") {
		t.Errorf("expected error to mention unknown policy, got %v", err)
	}
	if missing.calls == 0 {
		t.Error("expected PolicyExists to be called")
	}
}

// TestCreateRejectsOutOfChainPolicy validates the ancestor-chain check:
// when the binding lives in folder "payments" and the policy lives in an
// unrelated org that the ancestor resolver reports as out-of-chain, the
// create is rejected with CodeFailedPrecondition.
func TestCreateRejectsOutOfChainPolicy(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ancestor := &stubAncestorResolver{contains: false}
	h = h.
		WithPolicyExistsResolver(&stubPolicyResolver{exists: true}).
		WithAncestorChainResolver(ancestor).
		WithProjectExistsResolver(&stubProjectResolver{})
	ctx := authedCtx("owner@example.com", nil)

	folder := newFolderScopeRef("payments")
	binding := basicBinding(folder)
	// Policy lives in a different organization scope than the binding's
	// folder; the ancestor walker says "not reachable".
	binding.PolicyRef = &consolev1.LinkedTemplatePolicyRef{
		ScopeRef: newOrgScopeRef("other-org"),
		Name:     "platform-policy",
	}

	_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Scope:   folder,
		Binding: binding,
	}))
	if err == nil {
		t.Fatal("expected out-of-chain rejection")
	}
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("expected CodeFailedPrecondition, got %v: %v", connect.CodeOf(err), err)
	}
	if !containsString(err.Error(), "not reachable") {
		t.Errorf("expected error to mention reachability, got %v", err)
	}
	if ancestor.calls == 0 {
		t.Error("expected ancestor resolver to be consulted")
	}
}

// TestCreateAcceptsAncestorPolicy verifies that a policy one hop up the
// ancestor chain (folder binds against a policy in its parent org) is
// accepted — the contains-in-chain resolver returning true should let the
// create succeed.
func TestCreateAcceptsAncestorPolicy(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ancestor := &stubAncestorResolver{contains: true}
	h = h.
		WithPolicyExistsResolver(&stubPolicyResolver{exists: true}).
		WithAncestorChainResolver(ancestor).
		WithProjectExistsResolver(&stubProjectResolver{})
	ctx := authedCtx("owner@example.com", nil)

	folder := newFolderScopeRef("payments")
	binding := basicBinding(folder)
	binding.PolicyRef = &consolev1.LinkedTemplatePolicyRef{
		ScopeRef: newOrgScopeRef("acme"),
		Name:     "platform-policy",
	}

	_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Scope:   folder,
		Binding: binding,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ancestor.calls == 0 {
		t.Error("expected ancestor resolver to be consulted for cross-scope policy")
	}
}

// TestCreateRejectsBadProjectName covers the target-ref project existence
// check. When the ProjectExistsResolver reports the project_name is
// unknown the create is rejected with CodeInvalidArgument and the error
// names the offending target index and project.
func TestCreateRejectsBadProjectName(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	h = h.
		WithPolicyExistsResolver(&stubPolicyResolver{exists: true}).
		WithProjectExistsResolver(&stubProjectResolver{exists: map[string]bool{"payments-web": true}})
	ctx := authedCtx("owner@example.com", nil)

	folder := newFolderScopeRef("payments")
	binding := basicBinding(folder)
	binding.PolicyRef = &consolev1.LinkedTemplatePolicyRef{
		ScopeRef: folder,
		Name:     "local-policy",
	}
	binding.TargetRefs = []*consolev1.TemplatePolicyBindingTargetRef{
		{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
			Name:        "api",
			ProjectName: "no-such-project",
		},
	}

	_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Scope:   folder,
		Binding: binding,
	}))
	if err == nil {
		t.Fatal("expected bad-project rejection")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
	}
	if !containsString(err.Error(), "no-such-project") {
		t.Errorf("expected error to name the missing project, got %v", err)
	}
}

// TestUpdatePreservesImmutableFields verifies UpdateTemplatePolicyBinding
// preserves created_at / creator_email from the stored ConfigMap: the
// handler must never rewrite those annotations even if the inbound
// binding carries different values. Also asserts policy_ref and
// target_refs are replaced when present.
func TestUpdatePreservesImmutableFields(t *testing.T) {
	existingTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bind-a",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplatePolicyBinding,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:                     "Existing Display",
				v1alpha2.AnnotationDescription:                     "Existing Description",
				v1alpha2.AnnotationCreatorEmail:                    "original@example.com",
				v1alpha2.AnnotationTemplatePolicyBindingPolicyRef:  `{"scope":"organization","scopeName":"acme","name":"old-policy"}`,
				v1alpha2.AnnotationTemplatePolicyBindingTargetRefs: `[]`,
			},
			CreationTimestamp: metav1.Time{Time: existingTime},
		},
	}
	fakeClient := fake.NewClientset(existing)
	r := newTestResolver()
	k := NewK8sClient(fakeClient, r)
	h := NewHandler(k, r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: map[string]string{"owner@example.com": "owner"}}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: map[string]string{"owner@example.com": "owner"}}).
		WithPolicyExistsResolver(&stubPolicyResolver{exists: true}).
		WithProjectExistsResolver(&stubProjectResolver{})

	ctx := authedCtx("owner@example.com", nil)
	folder := newFolderScopeRef("payments")
	inbound := &consolev1.TemplatePolicyBinding{
		Name:     "bind-a",
		ScopeRef: folder,
		// Attempt to overwrite immutable fields — the handler must
		// discard these (creator_email is not carried into the k8s
		// client's Update at all; created_at is a k8s-managed field).
		CreatorEmail: "attacker@example.com",
		DisplayName:  "Updated Display",
		// Description intentionally left empty to confirm the handler
		// preserves the stored value when the request carries "".
		PolicyRef: &consolev1.LinkedTemplatePolicyRef{
			ScopeRef: folder,
			Name:     "new-local-policy",
		},
		TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{
			{
				Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
				Name:        "api",
				ProjectName: "payments-web",
			},
		},
	}

	_, err := h.UpdateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.UpdateTemplatePolicyBindingRequest{
		Scope:   folder,
		Binding: inbound,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := fakeClient.CoreV1().ConfigMaps("holos-fld-payments").Get(context.Background(), "bind-a", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("fetching updated binding: %v", err)
	}
	if updated.Annotations[v1alpha2.AnnotationCreatorEmail] != "original@example.com" {
		t.Errorf("creator_email clobbered: %q", updated.Annotations[v1alpha2.AnnotationCreatorEmail])
	}
	if !updated.CreationTimestamp.Time.Equal(existingTime) {
		t.Errorf("creation timestamp drifted: got %v, want %v", updated.CreationTimestamp.Time, existingTime)
	}
	if updated.Annotations[v1alpha2.AnnotationDisplayName] != "Updated Display" {
		t.Errorf("display_name not applied: %q", updated.Annotations[v1alpha2.AnnotationDisplayName])
	}
	if updated.Annotations[v1alpha2.AnnotationDescription] != "Existing Description" {
		t.Errorf("description clobbered when request left it empty: %q", updated.Annotations[v1alpha2.AnnotationDescription])
	}
	parsedPolicy, err := unmarshalPolicyRef(updated.Annotations[v1alpha2.AnnotationTemplatePolicyBindingPolicyRef])
	if err != nil {
		t.Fatalf("parsing updated policy_ref: %v", err)
	}
	if parsedPolicy.GetName() != "new-local-policy" {
		t.Errorf("policy_ref not replaced: got %q", parsedPolicy.GetName())
	}
	parsedTargets, err := unmarshalTargetRefs(updated.Annotations[v1alpha2.AnnotationTemplatePolicyBindingTargetRefs])
	if err != nil {
		t.Fatalf("parsing updated target_refs: %v", err)
	}
	if len(parsedTargets) != 1 || parsedTargets[0].GetName() != "api" {
		t.Errorf("target_refs not replaced as expected: %+v", parsedTargets)
	}
}

// TestUpdateMissingReturnsNotFound confirms Update against a non-existent
// binding yields CodeNotFound so clients can rely on the
// create-or-update-if-exists pattern.
func TestUpdateMissingReturnsNotFound(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	h = h.
		WithPolicyExistsResolver(&stubPolicyResolver{exists: true}).
		WithProjectExistsResolver(&stubProjectResolver{})
	ctx := authedCtx("owner@example.com", nil)

	folder := newFolderScopeRef("payments")
	binding := basicBinding(folder)
	binding.PolicyRef = &consolev1.LinkedTemplatePolicyRef{
		ScopeRef: folder,
		Name:     "local-policy",
	}

	_, err := h.UpdateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.UpdateTemplatePolicyBindingRequest{
		Scope:   folder,
		Binding: binding,
	}))
	if err == nil {
		t.Fatal("expected NotFound")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v: %v", connect.CodeOf(err), err)
	}
}

// TestGetRoundTripsAnnotations seeds a ConfigMap with a populated policy-
// ref and targets annotation, then reads it back through the handler to
// confirm the proto-level conversion is lossless.
func TestGetRoundTripsAnnotations(t *testing.T) {
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bind-a",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplatePolicyBinding,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:                     "Existing",
				v1alpha2.AnnotationDescription:                     "Desc",
				v1alpha2.AnnotationCreatorEmail:                    "original@example.com",
				v1alpha2.AnnotationTemplatePolicyBindingPolicyRef:  `{"scope":"organization","scopeName":"acme","name":"require-reference-grant"}`,
				v1alpha2.AnnotationTemplatePolicyBindingTargetRefs: `[{"kind":"deployment","name":"api","projectName":"payments-web"}]`,
			},
		},
	}
	fakeClient := fake.NewClientset(existing)
	r := newTestResolver()
	h := NewHandler(NewK8sClient(fakeClient, r), r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: map[string]string{"viewer@example.com": "viewer"}}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: map[string]string{"viewer@example.com": "viewer"}})
	ctx := authedCtx("viewer@example.com", nil)

	resp, err := h.GetTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.GetTemplatePolicyBindingRequest{
		Scope: newFolderScopeRef("payments"),
		Name:  "bind-a",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := resp.Msg.GetBinding()
	if got.GetCreatorEmail() != "original@example.com" {
		t.Errorf("creator email lost: %q", got.GetCreatorEmail())
	}
	if got.GetPolicyRef().GetName() != "require-reference-grant" {
		t.Errorf("policy_ref name lost: %q", got.GetPolicyRef().GetName())
	}
	if len(got.GetTargetRefs()) != 1 || got.GetTargetRefs()[0].GetProjectName() != "payments-web" {
		t.Errorf("target_refs lost: %+v", got.GetTargetRefs())
	}
}

// TestListReturnsOnlyBindings is a regression test for the label selector
// on ListBindings; if the handler's List path accidentally matched other
// resource types in the same namespace, this test would fail.
func TestListReturnsOnlyBindings(t *testing.T) {
	binding := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bind-a",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicyBinding,
			},
		},
	}
	policy := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy-a",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicy,
			},
		},
	}
	fakeClient := fake.NewClientset(binding, policy)
	r := newTestResolver()
	h := NewHandler(NewK8sClient(fakeClient, r), r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: map[string]string{"viewer@example.com": "viewer"}}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: map[string]string{"viewer@example.com": "viewer"}})
	ctx := authedCtx("viewer@example.com", nil)

	resp, err := h.ListTemplatePolicyBindings(ctx, connect.NewRequest(&consolev1.ListTemplatePolicyBindingsRequest{
		Scope: newFolderScopeRef("payments"),
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Msg.GetBindings()) != 1 || resp.Msg.GetBindings()[0].GetName() != "bind-a" {
		t.Errorf("expected only the binding, got %+v", resp.Msg.GetBindings())
	}
}

// TestDeleteRemovesConfigMap covers the happy-path Delete: the ConfigMap
// must disappear from the fake clientset after the call, and the response
// must succeed.
func TestDeleteRemovesConfigMap(t *testing.T) {
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bind-a",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicyBinding,
			},
		},
	}
	fakeClient := fake.NewClientset(existing)
	r := newTestResolver()
	h := NewHandler(NewK8sClient(fakeClient, r), r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: map[string]string{"owner@example.com": "owner"}}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: map[string]string{"owner@example.com": "owner"}})
	ctx := authedCtx("owner@example.com", nil)

	_, err := h.DeleteTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.DeleteTemplatePolicyBindingRequest{
		Scope: newFolderScopeRef("payments"),
		Name:  "bind-a",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, getErr := fakeClient.CoreV1().ConfigMaps("holos-fld-payments").Get(context.Background(), "bind-a", metav1.GetOptions{})
	if getErr == nil {
		t.Error("expected binding to be deleted")
	}
}

// TestPolicyProbeErrorFailsInternal confirms the handler surfaces a
// transient PolicyExists probe failure as CodeInternal, not
// CodeInvalidArgument. Locking this in prevents a regression that would
// map a flaky K8s API to a user-input error.
func TestPolicyProbeErrorFailsInternal(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	h = h.
		WithPolicyExistsResolver(&stubPolicyResolver{err: errors.New("api down")}).
		WithProjectExistsResolver(&stubProjectResolver{})
	ctx := authedCtx("owner@example.com", nil)

	folder := newFolderScopeRef("payments")
	binding := basicBinding(folder)
	binding.PolicyRef = &consolev1.LinkedTemplatePolicyRef{
		ScopeRef: folder,
		Name:     "whatever",
	}

	_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Scope:   folder,
		Binding: binding,
	}))
	if err == nil {
		t.Fatal("expected probe failure to surface as error")
	}
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Errorf("expected CodeInternal, got %v: %v", connect.CodeOf(err), err)
	}
}
