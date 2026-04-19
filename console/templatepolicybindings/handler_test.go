package templatepolicybindings

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/scopeshim"
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

func (s *stubPolicyResolver) PolicyExists(_ context.Context, _ scopeshim.Scope, _, _ string) (bool, error) {
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

func (s *stubProjectResolver) ProjectExists(_ context.Context, _ scopeshim.Scope, _, project string) (bool, error) {
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

// newTestHandler builds a Handler wired to a fake controller-runtime client
// and grant resolvers that return the supplied `shareUsers` map for both
// org and folder lookups. Tests that need to override individual resolvers
// can do so by calling the `With*` builders on the returned Handler.
//
// HOL-662 migrated the storage substrate from ConfigMaps to
// TemplatePolicyBinding CRDs; the fake ctrlclient is a sufficient stand-in
// for the CRUD envtest coverage in k8s_test.go.
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

// listBindings collects every TemplatePolicyBinding visible in the given
// namespace through the fake ctrlclient. Used in handler tests to assert
// on what the handler wrote (or did not write).
func listBindings(t *testing.T, c client.Client, namespace string) []templatesv1alpha1.TemplatePolicyBinding {
	t.Helper()
	var list templatesv1alpha1.TemplatePolicyBindingList
	if err := c.List(context.Background(), &list, client.InNamespace(namespace)); err != nil {
		t.Fatalf("list bindings in %q: %v", namespace, err)
	}
	return list.Items
}

// getBindingCR retrieves a TemplatePolicyBinding by namespace/name,
// returning nil if not found so tests can assert absence as easily as
// presence.
func getBindingCR(t *testing.T, c client.Client, namespace, name string) *templatesv1alpha1.TemplatePolicyBinding {
	t.Helper()
	var b templatesv1alpha1.TemplatePolicyBinding
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, &b); err != nil {
		return nil
	}
	return &b
}

// newFolderScopeRef, newOrgScopeRef, and newProjectScopeRef are short
// constructors that return the Kubernetes namespace string for the named
// scope. HOL-619 collapsed the TemplateScopeRef enum; the namespace is
// now the sole scope discriminator on request / proto messages.
func newFolderScopeRef(name string) string {
	return scopeshim.DefaultResolver().FolderNamespace(name)
}

func newOrgScopeRef(name string) string {
	return scopeshim.DefaultResolver().OrgNamespace(name)
}

func newProjectScopeRef(name string) string {
	return scopeshim.DefaultResolver().ProjectNamespace(name)
}

// basicBinding builds a binding whose namespace matches the supplied
// request namespace with a default policy_ref + single target_ref. The
// outer request namespace and the embedded binding namespace must line up
// for the handler to accept the request; this helper keeps the invariant
// in one place.
func basicBinding(namespace string) *consolev1.TemplatePolicyBinding {
	return &consolev1.TemplatePolicyBinding{
		Name:        "bind-reference-grant",
		DisplayName: "Bind reference grant",
		Description: "Attach reference-grant policy to payments-web",
		Namespace:   namespace,
		PolicyRef: scopeshim.NewLinkedTemplatePolicyRef(
			scopeshim.ScopeOrganization,
			"acme",
			"require-reference-grant",
		),
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
		Namespace: newProjectScopeRef("billing-web"),
		Binding:   basicBinding(newProjectScopeRef("billing-web")),
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

	if items := listBindings(t, fakeClient, "holos-prj-billing-web"); len(items) != 0 {
		t.Errorf("expected no TemplatePolicyBindings in project namespace, got %d", len(items))
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
					Namespace: newProjectScopeRef("billing-web"),
				}))
				return err
			},
		},
		{
			name: "get",
			run: func() error {
				_, err := h.GetTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.GetTemplatePolicyBindingRequest{
					Namespace: newProjectScopeRef("billing-web"),
					Name:  "any",
				}))
				return err
			},
		},
		{
			name: "update",
			run: func() error {
				_, err := h.UpdateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.UpdateTemplatePolicyBindingRequest{
					Namespace:   newProjectScopeRef("billing-web"),
					Binding: basicBinding(newProjectScopeRef("billing-web")),
				}))
				return err
			},
		},
		{
			name: "delete",
			run: func() error {
				_, err := h.DeleteTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.DeleteTemplatePolicyBindingRequest{
					Namespace: newProjectScopeRef("billing-web"),
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
// under an existing project. Verifies a TemplatePolicyBinding CRD lands in
// the folder namespace with the expected labels and annotations, and the
// response carries the binding name.
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
		Namespace: folder,
		Name:      "local-policy",
	}

	resp, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Namespace: folder,
		Binding:   binding,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.GetName() != binding.GetName() {
		t.Errorf("expected response name %q, got %q", binding.GetName(), resp.Msg.GetName())
	}

	b := getBindingCR(t, fakeClient, "holos-fld-payments", binding.GetName())
	if b == nil {
		t.Fatal("expected created binding in folder namespace")
	}
	if b.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeTemplatePolicyBinding {
		t.Errorf("expected resource type label, got %q", b.Labels[v1alpha2.LabelResourceType])
	}
	if b.Annotations[v1alpha2.AnnotationCreatorEmail] != "owner@example.com" {
		t.Errorf("expected creator email annotation to be set from claims, got %q", b.Annotations[v1alpha2.AnnotationCreatorEmail])
	}
}

// TestCreateRBACDenial confirms a caller with no grants sees
// PermissionDenied and never writes a binding. The stub grant resolver
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
		Namespace: folder,
		Name:      "local-policy",
	}

	_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Namespace: folder,
		Binding:   binding,
	}))
	if err == nil {
		t.Fatal("expected permission denied")
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected CodePermissionDenied, got %v: %v", connect.CodeOf(err), err)
	}

	if items := listBindings(t, fakeClient, "holos-fld-payments"); len(items) != 0 {
		t.Errorf("expected no TemplatePolicyBindings on RBAC denial, got %d", len(items))
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
		Namespace: folder,
		Name:     "local-policy",
	}

	_, err := h.CreateTemplatePolicyBinding(context.Background(), connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Namespace:   folder,
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
			Namespace: folder,
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
				Namespace:   folder,
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{validTarget()},
			},
			wantMsg: "policy_ref is required",
		},
		{
			name: "policy_ref missing scope_ref",
			binding: &consolev1.TemplatePolicyBinding{
				Name:       "bad",
				Namespace:   folder,
				PolicyRef:  &consolev1.LinkedTemplatePolicyRef{Name: "x"},
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{validTarget()},
			},
			wantMsg: "binding.policy_ref.namespace is required",
		},
		{
			name: "policy_ref project scope",
			binding: &consolev1.TemplatePolicyBinding{
				Name:     "bad",
				Namespace: folder,
				PolicyRef: &consolev1.LinkedTemplatePolicyRef{
					Namespace: newProjectScopeRef("p"),
					Name:     "x",
				},
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{validTarget()},
			},
			wantMsg: "policy_ref.namespace cannot be a project namespace",
		},
		{
			name: "target_refs empty",
			binding: &consolev1.TemplatePolicyBinding{
				Name:       "bad",
				Namespace:   folder,
				PolicyRef:  validPolicyRef(),
				TargetRefs: nil,
			},
			wantMsg: "at least one target_ref",
		},
		{
			name: "target kind unspecified",
			binding: &consolev1.TemplatePolicyBinding{
				Name:      "bad",
				Namespace:  folder,
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
				Namespace:  folder,
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
				Namespace:  folder,
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
				Namespace:  folder,
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
				Namespace:  folder,
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
				Namespace:  folder,
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
				Namespace:   folder,
				PolicyRef:  validPolicyRef(),
				TargetRefs: []*consolev1.TemplatePolicyBindingTargetRef{validTarget()},
			},
			wantMsg: "valid DNS label",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
				Namespace:   folder,
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

// TestCreateNamespaceMismatch covers the proto-contract check on
// binding.namespace. HOL-619 replaced binding.scope_ref with a top-level
// binding.namespace that must match the request namespace. The reviewer
// called out in the sibling templatepolicies handler tests that the
// handler previously accepted mismatched scope_ref values; the binding
// handler must fail the same way so a stored binding cannot claim a
// namespace different from the one it actually lives in.
func TestCreateNamespaceMismatch(t *testing.T) {
	h, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	h = h.
		WithPolicyExistsResolver(&stubPolicyResolver{exists: true}).
		WithProjectExistsResolver(&stubProjectResolver{})
	ctx := authedCtx("owner@example.com", nil)

	tests := []struct {
		name    string
		reqNs   string
		bindNs  string
		wantMsg string
	}{
		{
			name:    "empty binding namespace",
			reqNs:   newFolderScopeRef("payments"),
			bindNs:  "",
			wantMsg: "binding.namespace",
		},
		{
			name:    "mismatched folder name",
			reqNs:   newFolderScopeRef("payments"),
			bindNs:  newFolderScopeRef("identity"),
			wantMsg: "must match request namespace",
		},
		{
			name:    "mismatched org vs folder",
			reqNs:   newFolderScopeRef("payments"),
			bindNs:  newOrgScopeRef("acme"),
			wantMsg: "must match request namespace",
		},
		{
			name:    "project namespace at folder request",
			reqNs:   newFolderScopeRef("payments"),
			bindNs:  newProjectScopeRef("payments-web"),
			wantMsg: "must match request namespace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := basicBinding(tt.bindNs)
			b.PolicyRef = &consolev1.LinkedTemplatePolicyRef{
				Namespace: tt.reqNs,
				Name:      "p",
			}
			_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
				Namespace: tt.reqNs,
				Binding:   b,
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
		Namespace: folder,
		Name:     "does-not-exist",
	}

	_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Namespace:   folder,
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
		Namespace: newOrgScopeRef("other-org"),
		Name:     "platform-policy",
	}

	_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Namespace:   folder,
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
		Namespace: newOrgScopeRef("acme"),
		Name:     "platform-policy",
	}

	_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Namespace:   folder,
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
		Namespace: folder,
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
		Namespace:   folder,
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
// preserves created_at / creator_email from the stored CRD: the handler
// must never rewrite those fields even if the inbound binding carries
// different values. Also asserts policy_ref and target_refs are replaced
// when present.
func TestUpdatePreservesImmutableFields(t *testing.T) {
	existingTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	existing := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bind-a",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicyBinding,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationCreatorEmail: "original@example.com",
			},
			CreationTimestamp: metav1.Time{Time: existingTime},
		},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			DisplayName: "Existing Display",
			Description: "Existing Description",
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{
				Scope:     "organization",
				ScopeName: "acme",
				Name:      "old-policy",
			},
			TargetRefs: nil,
		},
	}
	fakeClient := newFakeCtrlClient(t, existing)
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
		Name:      "bind-a",
		Namespace: folder,
		// Attempt to overwrite immutable fields — the handler must
		// discard these (creator_email is not carried into the k8s
		// client's Update at all; created_at is a k8s-managed field).
		CreatorEmail: "attacker@example.com",
		DisplayName:  "Updated Display",
		// Description intentionally left empty to confirm the handler
		// preserves the stored value when the request carries "".
		PolicyRef: &consolev1.LinkedTemplatePolicyRef{
			Namespace: folder,
			Name:      "new-local-policy",
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
		Namespace: folder,
		Binding:   inbound,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := getBindingCR(t, fakeClient, "holos-fld-payments", "bind-a")
	if updated == nil {
		t.Fatal("expected binding to still exist after update")
	}
	if updated.Annotations[v1alpha2.AnnotationCreatorEmail] != "original@example.com" {
		t.Errorf("creator_email clobbered: %q", updated.Annotations[v1alpha2.AnnotationCreatorEmail])
	}
	if !updated.CreationTimestamp.Time.Equal(existingTime) {
		t.Errorf("creation timestamp drifted: got %v, want %v", updated.CreationTimestamp.Time, existingTime)
	}
	if updated.Spec.DisplayName != "Updated Display" {
		t.Errorf("display_name not applied: %q", updated.Spec.DisplayName)
	}
	if updated.Spec.Description != "Existing Description" {
		t.Errorf("description clobbered when request left it empty: %q", updated.Spec.Description)
	}
	if updated.Spec.PolicyRef.Name != "new-local-policy" {
		t.Errorf("policy_ref not replaced: got %q", updated.Spec.PolicyRef.Name)
	}
	if len(updated.Spec.TargetRefs) != 1 || updated.Spec.TargetRefs[0].Name != "api" {
		t.Errorf("target_refs not replaced as expected: %+v", updated.Spec.TargetRefs)
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
		Namespace: folder,
		Name:     "local-policy",
	}

	_, err := h.UpdateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.UpdateTemplatePolicyBindingRequest{
		Namespace:   folder,
		Binding: binding,
	}))
	if err == nil {
		t.Fatal("expected NotFound")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v: %v", connect.CodeOf(err), err)
	}
}

// TestGetRoundTripsAnnotations seeds a TemplatePolicyBinding CRD with a
// populated spec and reads it back through the handler to confirm the
// proto-level conversion is lossless.
func TestGetRoundTripsAnnotations(t *testing.T) {
	existing := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bind-a",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicyBinding,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationCreatorEmail: "original@example.com",
			},
		},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			DisplayName: "Existing",
			Description: "Desc",
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{
				Scope:     "organization",
				ScopeName: "acme",
				Name:      "require-reference-grant",
			},
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{
				{
					Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
					Name:        "api",
					ProjectName: "payments-web",
				},
			},
		},
	}
	fakeClient := newFakeCtrlClient(t, existing)
	r := newTestResolver()
	h := NewHandler(NewK8sClient(fakeClient, r), r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: map[string]string{"viewer@example.com": "viewer"}}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: map[string]string{"viewer@example.com": "viewer"}})
	ctx := authedCtx("viewer@example.com", nil)

	resp, err := h.GetTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.GetTemplatePolicyBindingRequest{
		Namespace: newFolderScopeRef("payments"),
		Name:      "bind-a",
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

// TestListReturnsOnlyBindings is a regression test for the List path.
// Since TemplatePolicyBinding is now a dedicated CRD (HOL-662), a List
// naturally only returns TemplatePolicyBinding objects — we no longer
// rely on label filtering the way ConfigMap storage did. This test
// confirms the happy List path returns a single seeded binding.
func TestListReturnsOnlyBindings(t *testing.T) {
	binding := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bind-a",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicyBinding,
			},
		},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			DisplayName: "A",
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{
				Scope:     "folder",
				ScopeName: "payments",
				Name:      "local-policy",
			},
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{
				{
					Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
					Name:        "api",
					ProjectName: "payments-web",
				},
			},
		},
	}
	fakeClient := newFakeCtrlClient(t, binding)
	r := newTestResolver()
	h := NewHandler(NewK8sClient(fakeClient, r), r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: map[string]string{"viewer@example.com": "viewer"}}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: map[string]string{"viewer@example.com": "viewer"}})
	ctx := authedCtx("viewer@example.com", nil)

	resp, err := h.ListTemplatePolicyBindings(ctx, connect.NewRequest(&consolev1.ListTemplatePolicyBindingsRequest{
		Namespace: newFolderScopeRef("payments"),
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Msg.GetBindings()) != 1 || resp.Msg.GetBindings()[0].GetName() != "bind-a" {
		t.Errorf("expected only the binding, got %+v", resp.Msg.GetBindings())
	}
}

// TestDeleteRemovesBinding covers the happy-path Delete: the
// TemplatePolicyBinding CRD must disappear from the fake client after
// the call, and the response must succeed.
func TestDeleteRemovesBinding(t *testing.T) {
	existing := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bind-a",
			Namespace: "holos-fld-payments",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicyBinding,
			},
		},
	}
	fakeClient := newFakeCtrlClient(t, existing)
	r := newTestResolver()
	h := NewHandler(NewK8sClient(fakeClient, r), r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: map[string]string{"owner@example.com": "owner"}}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: map[string]string{"owner@example.com": "owner"}})
	ctx := authedCtx("owner@example.com", nil)

	_, err := h.DeleteTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.DeleteTemplatePolicyBindingRequest{
		Namespace: newFolderScopeRef("payments"),
		Name:      "bind-a",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b := getBindingCR(t, fakeClient, "holos-fld-payments", "bind-a"); b != nil {
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
		Namespace: folder,
		Name:     "whatever",
	}

	_, err := h.CreateTemplatePolicyBinding(ctx, connect.NewRequest(&consolev1.CreateTemplatePolicyBindingRequest{
		Namespace:   folder,
		Binding: binding,
	}))
	if err == nil {
		t.Fatal("expected probe failure to surface as error")
	}
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Errorf("expected CodeInternal, got %v: %v", connect.CodeOf(err), err)
	}
}
