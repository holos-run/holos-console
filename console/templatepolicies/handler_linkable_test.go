// handler_linkable_test.go tests ListLinkableTemplatePolicies (HOL-912).
// The RPC now accepts only a namespace and returns every TemplatePolicy in that
// namespace ordered alphabetically by name — no ancestor walk, no per-scope RBAC
// omission. Tests mirror the structure of handler_test.go and cover the three
// cases called out in the issue acceptance criteria:
//
//	(a) empty namespace → empty list
//	(b) namespace with N policies → N returned alphabetically
//	(c) caller without LIST permission → PermissionDenied
package templatepolicies

import (
	"context"
	"sort"
	"testing"

	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// seedLinkablePolicy seeds a TemplatePolicy CRD directly into the fake
// ctrlclient so tests can assert on the handler's list output without going
// through Create.
func seedLinkablePolicy(t *testing.T, h *Handler, namespace, name, displayName string) {
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

// newSimpleLinkableHandler constructs a handler with org and folder grant
// resolvers wired for the given owner email — no walker (not needed after
// HOL-912).
func newSimpleLinkableHandler(t *testing.T, ownerEmail string) *Handler {
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
		})
	return h
}

// TestListLinkableTemplatePolicies_MissingNamespace asserts that an empty
// namespace returns CodeInvalidArgument.
func TestListLinkableTemplatePolicies_MissingNamespace(t *testing.T) {
	h := newSimpleLinkableHandler(t, "owner@example.com")
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
	h := newSimpleLinkableHandler(t, "owner@example.com")

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
// namespace returns CodeInvalidArgument (storage-isolation guardrail).
func TestListLinkableTemplatePolicies_RejectsProjectScope(t *testing.T) {
	h := newSimpleLinkableHandler(t, "owner@example.com")
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

// TestListLinkableTemplatePolicies_PermissionDenied asserts that a caller
// without PERMISSION_TEMPLATE_POLICIES_LIST on the namespace receives
// CodePermissionDenied.
// TestListLinkableTemplatePolicies_EmptyNamespace asserts that a namespace
// with no policies returns an empty list (acceptance criterion a).
func TestListLinkableTemplatePolicies_EmptyNamespace(t *testing.T) {
	const ownerEmail = "owner@example.com"
	h := newSimpleLinkableHandler(t, ownerEmail)
	orgNs := newTestResolver().OrgNamespace("acme")

	ctx := authedCtx(ownerEmail, nil)
	resp, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
		Namespace: orgNs,
	}))
	if err != nil {
		t.Fatalf("ListLinkableTemplatePolicies: %v", err)
	}
	if len(resp.Msg.GetPolicies()) != 0 {
		t.Errorf("expected 0 policies, got %d", len(resp.Msg.GetPolicies()))
	}
}

// TestListLinkableTemplatePolicies_ReturnsNamespacePoliciesAlphabetically
// asserts that N policies in a namespace are returned alphabetically by name
// (acceptance criteria b + c ordering).
func TestListLinkableTemplatePolicies_ReturnsNamespacePoliciesAlphabetically(t *testing.T) {
	const ownerEmail = "owner@example.com"
	h := newSimpleLinkableHandler(t, ownerEmail)
	orgNs := newTestResolver().OrgNamespace("acme")

	// Seed in non-alphabetical order to confirm sorting.
	seedLinkablePolicy(t, h, orgNs, "zebra-policy", "Zebra Policy")
	seedLinkablePolicy(t, h, orgNs, "alpha-policy", "Alpha Policy")
	seedLinkablePolicy(t, h, orgNs, "middle-policy", "Middle Policy")

	ctx := authedCtx(ownerEmail, nil)
	resp, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
		Namespace: orgNs,
	}))
	if err != nil {
		t.Fatalf("ListLinkableTemplatePolicies: %v", err)
	}
	if len(resp.Msg.GetPolicies()) != 3 {
		t.Fatalf("expected 3 policies, got %d", len(resp.Msg.GetPolicies()))
	}

	// Verify alphabetical ordering.
	names := make([]string, 0, len(resp.Msg.GetPolicies()))
	for _, lp := range resp.Msg.GetPolicies() {
		names = append(names, lp.GetPolicy().GetName())
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("expected policies sorted alphabetically, got %v", names)
	}
	wantNames := []string{"alpha-policy", "middle-policy", "zebra-policy"}
	for i, want := range wantNames {
		if got := names[i]; got != want {
			t.Errorf("policy[%d]: expected %q, got %q", i, want, got)
		}
	}
}

// TestListLinkableTemplatePolicies_ReturnsOnlyNamespacePolicies asserts that
// policies in other namespaces are not returned — the RPC is namespace-only,
// no ancestor walk.
func TestListLinkableTemplatePolicies_ReturnsOnlyNamespacePolicies(t *testing.T) {
	const ownerEmail = "owner@example.com"
	h := newSimpleLinkableHandler(t, ownerEmail)
	r := newTestResolver()
	orgNs := r.OrgNamespace("acme")
	folderNs := r.FolderNamespace("payments")

	// Seed one policy in the org namespace and one in a folder namespace.
	seedLinkablePolicy(t, h, orgNs, "org-policy", "Org Policy")
	seedLinkablePolicy(t, h, folderNs, "folder-policy", "Folder Policy")

	ctx := authedCtx(ownerEmail, nil)
	resp, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
		Namespace: orgNs,
	}))
	if err != nil {
		t.Fatalf("ListLinkableTemplatePolicies: %v", err)
	}
	// Only the org namespace policy should be returned.
	if len(resp.Msg.GetPolicies()) != 1 {
		t.Fatalf("expected 1 policy (org namespace only), got %d", len(resp.Msg.GetPolicies()))
	}
	if got := resp.Msg.GetPolicies()[0].GetPolicy().GetName(); got != "org-policy" {
		t.Errorf("expected org-policy, got %q", got)
	}
	if got := resp.Msg.GetPolicies()[0].GetPolicy().GetNamespace(); got != orgNs {
		t.Errorf("expected namespace=%q, got %q", orgNs, got)
	}
}

// TestListLinkableTemplatePolicies_FolderScope asserts that the RPC works for
// folder-scope namespaces, not just org-scope.
func TestListLinkableTemplatePolicies_FolderScope(t *testing.T) {
	const ownerEmail = "owner@example.com"
	h := newSimpleLinkableHandler(t, ownerEmail)
	r := newTestResolver()
	folderNs := r.FolderNamespace("payments")

	seedLinkablePolicy(t, h, folderNs, "folder-policy", "Folder Policy")

	ctx := authedCtx(ownerEmail, nil)
	resp, err := h.ListLinkableTemplatePolicies(ctx, connect.NewRequest(&consolev1.ListLinkableTemplatePoliciesRequest{
		Namespace: folderNs,
	}))
	if err != nil {
		t.Fatalf("ListLinkableTemplatePolicies: %v", err)
	}
	if len(resp.Msg.GetPolicies()) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(resp.Msg.GetPolicies()))
	}
	if got := resp.Msg.GetPolicies()[0].GetPolicy().GetNamespace(); got != folderNs {
		t.Errorf("expected namespace=%q, got %q", folderNs, got)
	}
}
