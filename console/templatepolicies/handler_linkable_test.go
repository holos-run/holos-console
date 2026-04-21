// handler_linkable_test.go tests ListLinkableTemplatePolicies (HOL-834).
// Table-driven tests mirror the structure of console/templates/handler_linkable_test.go
// for ListLinkableTemplates to keep patterns consistent across the two handlers.
package templatepolicies

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// stubWalker implements AncestorWalker for linkable-policy tests. It returns a
// predetermined ordered list of namespace objects ([child, ..., org]) mirroring
// what resolver.CachedWalker returns in production.
type stubWalker struct {
	ancestors []*corev1.Namespace
	err       error
}

func (s *stubWalker) WalkAncestors(_ context.Context, _ string) ([]*corev1.Namespace, error) {
	return s.ancestors, s.err
}

// nsObj creates a minimal Namespace object from a raw namespace string.
func nsObj(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

// seedPolicy seeds a TemplatePolicy CRD directly into the fake ctrlclient so
// tests can assert on the handler's list output without going through Create.
func seedPolicy(t *testing.T, h *Handler, namespace, name, displayName string) {
	t.Helper()
	p := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationCreatorEmail: "platform@example.com",
			},
		},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			DisplayName: displayName,
			Rules:       nil,
		},
	}
	if err := h.k8s.client.Create(context.Background(), p); err != nil {
		t.Fatalf("seed policy %s/%s: %v", namespace, name, err)
	}
}

// newLinkableHandler constructs a handler with the given walker and wired
// org/folder grant resolvers. Owner grants are wired for all org and folder
// scopes using the simple (non-perScope) mode.
func newLinkableHandler(t *testing.T, ownerEmail string, walker AncestorWalker) *Handler {
	t.Helper()
	ctrlClient := newFakeCtrlClient(t)
	r := newTestResolver()
	k := NewK8sClient(ctrlClient, r)
	h := NewHandler(k, r).
		WithOrgGrantResolver(&stubOrgGrantResolver{
			users: map[string]string{ownerEmail: "owner"},
		}).
		WithFolderGrantResolver(&stubFolderGrantResolver{
			users: map[string]string{ownerEmail: "owner"},
		}).
		WithAncestorWalker(walker)
	return h
}

// TestListLinkableTemplatePolicies_MissingNamespace asserts that an empty
// namespace returns CodeInvalidArgument before touching the walker.
func TestListLinkableTemplatePolicies_MissingNamespace(t *testing.T) {
	h := newLinkableHandler(t, "owner@example.com", &stubWalker{})
	ctx := authedCtx("owner@example.com", nil)

	_, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{}))
	if err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
	}
}

// TestListLinkableTemplatePolicies_Unauthenticated asserts that a missing
// claims context returns CodeUnauthenticated.
func TestListLinkableTemplatePolicies_Unauthenticated(t *testing.T) {
	h := newLinkableHandler(t, "owner@example.com", &stubWalker{})

	_, err := h.ListLinkableTemplatePolicies(
		context.Background(), // no claims injected
		connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
			Namespace: newFolderScope("payments"),
		}),
	)
	if err == nil {
		t.Fatal("expected unauthenticated error")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("expected CodeUnauthenticated, got %v: %v", connect.CodeOf(err), err)
	}
}

// TestListLinkableTemplatePolicies_RejectsProjectScope asserts that a project
// namespace returns CodeInvalidArgument, preserving the storage-isolation
// guardrail from extractPolicyScope.
func TestListLinkableTemplatePolicies_RejectsProjectScope(t *testing.T) {
	h := newLinkableHandler(t, "owner@example.com", &stubWalker{})
	ctx := authedCtx("owner@example.com", nil)

	_, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
		Namespace: newProjectScope("billing-web"),
	}))
	if err == nil {
		t.Fatal("expected rejection for project-scope namespace")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
	}
}

// TestListLinkableTemplatePolicies_NilWalker asserts that when no walker is
// wired the handler returns an empty response rather than an error, matching
// the fallback behavior of ListLinkableTemplates.
func TestListLinkableTemplatePolicies_NilWalker(t *testing.T) {
	ctrlClient := newFakeCtrlClient(t)
	r := newTestResolver()
	k := NewK8sClient(ctrlClient, r)
	h := NewHandler(k, r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: map[string]string{"owner@example.com": "owner"}}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: map[string]string{"owner@example.com": "owner"}})
	// No walker wired — h.walker == nil

	ctx := authedCtx("owner@example.com", nil)
	resp, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
		Namespace: newOrgScope("acme"),
	}))
	if err != nil {
		t.Fatalf("expected empty response, got error: %v", err)
	}
	if len(resp.Msg.GetPolicies()) != 0 {
		t.Errorf("expected 0 policies without walker, got %d", len(resp.Msg.GetPolicies()))
	}
}

// TestListLinkableTemplatePolicies_OrgScopeReturnsOrgPolicies confirms that an
// org-scope request returns only org-namespace policies (there are no ancestors
// above the org root).
func TestListLinkableTemplatePolicies_OrgScopeReturnsOrgPolicies(t *testing.T) {
	const org = "acme"
	const ownerEmail = "owner@example.com"

	orgNs := newTestResolver().OrgNamespace(org)
	walker := &stubWalker{
		ancestors: []*corev1.Namespace{nsObj(orgNs)},
	}
	h := newLinkableHandler(t, ownerEmail, walker)
	seedPolicy(t, h, orgNs, "org-policy", "Org Policy")

	ctx := authedCtx(ownerEmail, nil)
	resp, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
		Namespace:        orgNs,
		IncludeSelfScope: true,
	}))
	if err != nil {
		t.Fatalf("ListLinkableTemplatePolicies: %v", err)
	}
	if len(resp.Msg.GetPolicies()) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(resp.Msg.GetPolicies()))
	}
	if got := resp.Msg.GetPolicies()[0].GetPolicy().GetName(); got != "org-policy" {
		t.Errorf("expected name=org-policy, got %q", got)
	}
	if got := resp.Msg.GetPolicies()[0].GetPolicy().GetNamespace(); got != orgNs {
		t.Errorf("expected namespace=%q, got %q", orgNs, got)
	}
}

// TestListLinkableTemplatePolicies_FolderScopeReturnsChain confirms that a
// folder-scope request with include_self_scope=false returns org policies but
// not folder policies (ancestors only), and with include_self_scope=true
// returns both.
func TestListLinkableTemplatePolicies_FolderScopeReturnsChain(t *testing.T) {
	const org = "acme"
	const folder = "payments"
	const ownerEmail = "owner@example.com"

	r := newTestResolver()
	orgNs := r.OrgNamespace(org)
	folderNs := r.FolderNamespace(folder)

	walker := &stubWalker{
		ancestors: []*corev1.Namespace{nsObj(folderNs), nsObj(orgNs)},
	}
	h := newLinkableHandler(t, ownerEmail, walker)
	seedPolicy(t, h, orgNs, "org-require-gateway", "Org Require Gateway")
	seedPolicy(t, h, folderNs, "folder-require-route", "Folder Require Route")

	ctx := authedCtx(ownerEmail, nil)

	t.Run("include_self_scope=false returns only org policies", func(t *testing.T) {
		resp, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
			Namespace:        folderNs,
			IncludeSelfScope: false,
		}))
		if err != nil {
			t.Fatalf("ListLinkableTemplatePolicies: %v", err)
		}
		if len(resp.Msg.GetPolicies()) != 1 {
			t.Fatalf("expected 1 policy (org only), got %d", len(resp.Msg.GetPolicies()))
		}
		if got := resp.Msg.GetPolicies()[0].GetPolicy().GetNamespace(); got != orgNs {
			t.Errorf("expected org namespace=%q, got %q", orgNs, got)
		}
	})

	t.Run("include_self_scope=true returns folder then org policies (child first)", func(t *testing.T) {
		resp, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
			Namespace:        folderNs,
			IncludeSelfScope: true,
		}))
		if err != nil {
			t.Fatalf("ListLinkableTemplatePolicies: %v", err)
		}
		if len(resp.Msg.GetPolicies()) != 2 {
			t.Fatalf("expected 2 policies, got %d", len(resp.Msg.GetPolicies()))
		}
		// Child (folder) must come first (child→parent ordering).
		if got := resp.Msg.GetPolicies()[0].GetPolicy().GetNamespace(); got != folderNs {
			t.Errorf("expected first policy from folder namespace %q, got %q", folderNs, got)
		}
		if got := resp.Msg.GetPolicies()[1].GetPolicy().GetNamespace(); got != orgNs {
			t.Errorf("expected second policy from org namespace %q, got %q", orgNs, got)
		}
	})
}

// TestListLinkableTemplatePolicies_NestedFolderReturnsFullChain asserts that a
// grand-child folder (child→parent→grandparent/org) returns the full chain
// when include_self_scope=true.
func TestListLinkableTemplatePolicies_NestedFolderReturnsFullChain(t *testing.T) {
	const org = "acme"
	const parentFolder = "platform"
	const childFolder = "payments"
	const ownerEmail = "owner@example.com"

	r := newTestResolver()
	orgNs := r.OrgNamespace(org)
	parentNs := r.FolderNamespace(parentFolder)
	childNs := r.FolderNamespace(childFolder)

	// Walker returns child→parent→org (deepest first).
	walker := &stubWalker{
		ancestors: []*corev1.Namespace{nsObj(childNs), nsObj(parentNs), nsObj(orgNs)},
	}
	h := newLinkableHandler(t, ownerEmail, walker)
	seedPolicy(t, h, orgNs, "org-policy", "Org Policy")
	seedPolicy(t, h, parentNs, "parent-policy", "Parent Policy")
	seedPolicy(t, h, childNs, "child-policy", "Child Policy")

	ctx := authedCtx(ownerEmail, nil)
	resp, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
		Namespace:        childNs,
		IncludeSelfScope: true,
	}))
	if err != nil {
		t.Fatalf("ListLinkableTemplatePolicies: %v", err)
	}
	if len(resp.Msg.GetPolicies()) != 3 {
		t.Fatalf("expected 3 policies (full chain), got %d", len(resp.Msg.GetPolicies()))
	}
	wantOrder := []string{childNs, parentNs, orgNs}
	for i, p := range resp.Msg.GetPolicies() {
		if got := p.GetPolicy().GetNamespace(); got != wantOrder[i] {
			t.Errorf("policy %d: expected namespace %q, got %q", i, wantOrder[i], got)
		}
	}
}

// TestListLinkableTemplatePolicies_WalkerErrorReturnsEmpty asserts that when
// the ancestor walker returns an error the handler returns an empty response
// rather than propagating the error, matching the ListLinkableTemplates
// behavior.
func TestListLinkableTemplatePolicies_WalkerErrorReturnsEmpty(t *testing.T) {
	const ownerEmail = "owner@example.com"

	r := newTestResolver()
	orgNs := r.OrgNamespace("acme")
	walker := &stubWalker{err: errWalkerFailed}
	h := newLinkableHandler(t, ownerEmail, walker)

	ctx := authedCtx(ownerEmail, nil)
	resp, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
		Namespace:        orgNs,
		IncludeSelfScope: true,
	}))
	if err != nil {
		t.Fatalf("expected empty response on walker error, got: %v", err)
	}
	if len(resp.Msg.GetPolicies()) != 0 {
		t.Errorf("expected 0 policies on walker error, got %d", len(resp.Msg.GetPolicies()))
	}
}

// errWalkerFailed is the sentinel error returned by the failing stub walker.
var errWalkerFailed = errWalkerFailedSentinel("walker failed")

type errWalkerFailedSentinel string

func (e errWalkerFailedSentinel) Error() string { return string(e) }

// TestListLinkableTemplatePolicies_RBACSkipsUnreachableAncestor asserts that
// when the caller lacks permission to list policies in an ancestor namespace,
// that namespace is silently skipped and the response still includes policies
// from namespaces the caller can access.
func TestListLinkableTemplatePolicies_RBACSkipsUnreachableAncestor(t *testing.T) {
	const ownerEmail = "owner@example.com"
	const org = "acme"
	const folder = "payments"

	r := newTestResolver()
	orgNs := r.OrgNamespace(org)
	folderNs := r.FolderNamespace(folder)

	walker := &stubWalker{
		ancestors: []*corev1.Namespace{nsObj(folderNs), nsObj(orgNs)},
	}

	ctrlClient := newFakeCtrlClient(t)
	k := NewK8sClient(ctrlClient, r)

	// Caller has folder-level access but NOT org-level access.
	h := NewHandler(k, r).
		WithOrgGrantResolver(&stubOrgGrantResolver{
			users: map[string]string{}, // no grants → org access denied
		}).
		WithFolderGrantResolver(&stubFolderGrantResolver{
			users: map[string]string{ownerEmail: "owner"}, // folder access granted
		}).
		WithAncestorWalker(walker)

	// Seed policies in both namespaces.
	orgPolicy := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "org-policy",
			Namespace:   orgNs,
			Labels:      map[string]string{v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue},
			Annotations: map[string]string{v1alpha2.AnnotationCreatorEmail: "platform@example.com"},
		},
		Spec: templatesv1alpha1.TemplatePolicySpec{DisplayName: "Org Policy"},
	}
	folderPolicy := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "folder-policy",
			Namespace:   folderNs,
			Labels:      map[string]string{v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue},
			Annotations: map[string]string{v1alpha2.AnnotationCreatorEmail: "platform@example.com"},
		},
		Spec: templatesv1alpha1.TemplatePolicySpec{DisplayName: "Folder Policy"},
	}
	if err := ctrlClient.Create(context.Background(), orgPolicy); err != nil {
		t.Fatalf("seed org policy: %v", err)
	}
	if err := ctrlClient.Create(context.Background(), folderPolicy); err != nil {
		t.Fatalf("seed folder policy: %v", err)
	}

	ctx := authedCtx(ownerEmail, nil)
	resp, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
		Namespace:        folderNs,
		IncludeSelfScope: true,
	}))
	if err != nil {
		t.Fatalf("ListLinkableTemplatePolicies: %v", err)
	}
	// Only the folder policy should be returned; org was silently skipped.
	if len(resp.Msg.GetPolicies()) != 1 {
		t.Fatalf("expected 1 policy (folder only), got %d", len(resp.Msg.GetPolicies()))
	}
	if got := resp.Msg.GetPolicies()[0].GetPolicy().GetNamespace(); got != folderNs {
		t.Errorf("expected folder namespace %q, got %q", folderNs, got)
	}
}

// TestListLinkableTemplatePolicies_EmptyResultWhenNoPolicies asserts that the
// RPC returns an empty slice (not nil, not an error) when no policies exist in
// any reachable namespace.
func TestListLinkableTemplatePolicies_EmptyResultWhenNoPolicies(t *testing.T) {
	const ownerEmail = "owner@example.com"

	r := newTestResolver()
	orgNs := r.OrgNamespace("acme")
	walker := &stubWalker{
		ancestors: []*corev1.Namespace{nsObj(orgNs)},
	}
	h := newLinkableHandler(t, ownerEmail, walker)

	ctx := authedCtx(ownerEmail, nil)
	resp, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
		Namespace:        orgNs,
		IncludeSelfScope: true,
	}))
	if err != nil {
		t.Fatalf("ListLinkableTemplatePolicies: %v", err)
	}
	if len(resp.Msg.GetPolicies()) != 0 {
		t.Errorf("expected 0 policies, got %d", len(resp.Msg.GetPolicies()))
	}
}
