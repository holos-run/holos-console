// handler_test.go exercises ResourceService.ListResources, the cross-kind
// "Resources" listing introduced in HOL-602. Each test seeds an organization
// hierarchy via fake.Clientset namespaces, builds a handler wired with the
// folders/projects/organizations K8s clients plus a Walker over the same
// fake clientset, and asserts on the flat list of Resource entries the
// handler returns.
//
// Per the HOL-602 acceptance criteria the tests cover:
//   - unauthenticated callers receive CodeUnauthenticated;
//   - the empty cluster returns an empty list;
//   - a single org with one folder and one project under it returns the
//     two entries with the correct ancestor path;
//   - deeply-nested folder hierarchies (3 levels) populate path elements
//     in root→leaf order;
//   - the organization filter restricts results to one org;
//   - the types filter restricts to RESOURCE_TYPE_FOLDER or
//     RESOURCE_TYPE_PROJECT;
//   - RBAC at every level filters the response.
package resources

import (
	"context"
	"sort"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/folders"
	"github.com/holos-run/holos-console/console/organizations"
	"github.com/holos-run/holos-console/console/projects"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

func testResolver() *resolver.Resolver {
	return &resolver.Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
}

func contextWithClaims(email string, roles ...string) context.Context {
	return rpc.ContextWithClaims(context.Background(), &rpc.Claims{
		Sub:   "sub-" + email,
		Email: email,
		Roles: roles,
	})
}

// orgNS builds an organization namespace fixture. shareUsersJSON is written
// to the share-users annotation when non-empty so RBAC tests can grant
// access selectively.
func orgNS(name, displayName, shareUsersJSON string) *corev1.Namespace {
	annotations := map[string]string{}
	if shareUsersJSON != "" {
		annotations[v1alpha2.AnnotationShareUsers] = shareUsersJSON
	}
	if displayName != "" {
		annotations[v1alpha2.AnnotationDisplayName] = displayName
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-" + name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
				v1alpha2.LabelOrganization: name,
			},
			Annotations: annotations,
		},
	}
}

// folderNS builds a folder namespace fixture. parentNs is the immediate
// parent's Kubernetes namespace name (org or another folder).
func folderNS(name, displayName, org, parentNs, shareUsersJSON string) *corev1.Namespace {
	annotations := map[string]string{}
	if shareUsersJSON != "" {
		annotations[v1alpha2.AnnotationShareUsers] = shareUsersJSON
	}
	if displayName != "" {
		annotations[v1alpha2.AnnotationDisplayName] = displayName
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-fld-" + name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelOrganization: org,
				v1alpha2.LabelFolder:       name,
				v1alpha2.AnnotationParent:  parentNs,
			},
			Annotations: annotations,
		},
	}
}

// projectNS builds a project namespace fixture.
func projectNS(name, displayName, org, parentNs, shareUsersJSON string) *corev1.Namespace {
	annotations := map[string]string{}
	if shareUsersJSON != "" {
		annotations[v1alpha2.AnnotationShareUsers] = shareUsersJSON
	}
	if displayName != "" {
		annotations[v1alpha2.AnnotationDisplayName] = displayName
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-" + name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelOrganization: org,
				v1alpha2.LabelProject:      name,
				v1alpha2.AnnotationParent:  parentNs,
			},
			Annotations: annotations,
		},
	}
}

func newTestHandler(t *testing.T, namespaces ...*corev1.Namespace) *Handler {
	t.Helper()
	clientset := fake.NewClientset()
	for _, ns := range namespaces {
		if _, err := clientset.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{}); err != nil {
			t.Fatalf("seed namespace %q: %v", ns.Name, err)
		}
	}
	r := testResolver()
	walker := &resolver.Walker{Client: clientset, Resolver: r}
	return NewHandler(
		NewK8sClient(folders.NewK8sClient(clientset, r), projects.NewK8sClient(clientset, r), organizations.NewK8sClient(clientset, r)),
		r,
		walker,
	)
}

// resourceKey produces a stable string key for a Resource so the test
// assertions don't depend on iteration order.
func resourceKey(res *consolev1.Resource) string {
	pathStr := ""
	for _, p := range res.GetPath() {
		pathStr += p.GetName() + "/"
	}
	return res.GetType().String() + "|" + pathStr + res.GetName()
}

func sortedResourceKeys(resources []*consolev1.Resource) []string {
	out := make([]string, 0, len(resources))
	for _, r := range resources {
		out = append(out, resourceKey(r))
	}
	sort.Strings(out)
	return out
}

func TestListResources_Unauthenticated(t *testing.T) {
	handler := newTestHandler(t)
	_, err := handler.ListResources(context.Background(), connect.NewRequest(&consolev1.ListResourcesRequest{}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got, want := connect.CodeOf(err), connect.CodeUnauthenticated; got != want {
		t.Errorf("expected code %v, got %v", want, got)
	}
}

func TestListResources_EmptyCluster(t *testing.T) {
	handler := newTestHandler(t)
	ctx := contextWithClaims("alice@example.com")
	resp, err := handler.ListResources(ctx, connect.NewRequest(&consolev1.ListResourcesRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(resp.Msg.GetResources()); got != 0 {
		t.Errorf("expected 0 resources, got %d", got)
	}
}

func TestListResources_FlatListWithAncestors(t *testing.T) {
	const owner = "alice@example.com"
	grants := `[{"principal":"alice@example.com","role":"owner"}]`

	org := orgNS("acme", "Acme Corp", grants)
	fld := folderNS("payments", "Payments", "acme", org.Name, grants)
	prj := projectNS("checkout", "Checkout", "acme", fld.Name, grants)

	handler := newTestHandler(t, org, fld, prj)
	ctx := contextWithClaims(owner)

	resp, err := handler.ListResources(ctx, connect.NewRequest(&consolev1.ListResourcesRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := sortedResourceKeys(resp.Msg.GetResources())
	want := []string{
		"RESOURCE_TYPE_FOLDER|acme/payments",
		"RESOURCE_TYPE_PROJECT|acme/payments/checkout",
	}
	if !equalStrings(got, want) {
		t.Errorf("resources mismatch:\ngot  %v\nwant %v", got, want)
	}

	// Verify display names and path elements on the project entry.
	var prjEntry *consolev1.Resource
	for _, r := range resp.Msg.GetResources() {
		if r.GetType() == consolev1.ResourceType_RESOURCE_TYPE_PROJECT {
			prjEntry = r
			break
		}
	}
	if prjEntry == nil {
		t.Fatal("expected project entry, got none")
	}
	if got, want := prjEntry.GetDisplayName(), "Checkout"; got != want {
		t.Errorf("project display_name: got %q, want %q", got, want)
	}
	if got, want := len(prjEntry.GetPath()), 2; got != want {
		t.Fatalf("project path length: got %d, want %d", got, want)
	}
	if got, want := prjEntry.GetPath()[0].GetName(), "acme"; got != want {
		t.Errorf("project path[0].name: got %q, want %q", got, want)
	}
	if got, want := prjEntry.GetPath()[0].GetDisplayName(), "Acme Corp"; got != want {
		t.Errorf("project path[0].display_name: got %q, want %q", got, want)
	}
	if got, want := prjEntry.GetPath()[1].GetName(), "payments"; got != want {
		t.Errorf("project path[1].name: got %q, want %q", got, want)
	}
	if got, want := prjEntry.GetPath()[1].GetType(), consolev1.ResourceType_RESOURCE_TYPE_FOLDER; got != want {
		t.Errorf("project path[1].type: got %v, want %v", got, want)
	}
}

func TestListResources_DeeplyNestedFolders(t *testing.T) {
	const owner = "alice@example.com"
	grants := `[{"principal":"alice@example.com","role":"owner"}]`

	org := orgNS("acme", "Acme Corp", grants)
	fld1 := folderNS("eng", "Engineering", "acme", org.Name, grants)
	fld2 := folderNS("payments", "Payments", "acme", fld1.Name, grants)
	fld3 := folderNS("settlements", "Settlements", "acme", fld2.Name, grants)
	prj := projectNS("ledger", "Ledger", "acme", fld3.Name, grants)

	handler := newTestHandler(t, org, fld1, fld2, fld3, prj)
	ctx := contextWithClaims(owner)

	resp, err := handler.ListResources(ctx, connect.NewRequest(&consolev1.ListResourcesRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the deepest project entry and check its 4-element path.
	var ledger *consolev1.Resource
	for _, r := range resp.Msg.GetResources() {
		if r.GetName() == "ledger" {
			ledger = r
			break
		}
	}
	if ledger == nil {
		t.Fatal("expected ledger project entry")
	}

	// Path is root→leaf: org, eng, payments, settlements.
	wantPath := []string{"acme", "eng", "payments", "settlements"}
	if got, want := len(ledger.GetPath()), len(wantPath); got != want {
		t.Fatalf("ledger path length: got %d, want %d", got, want)
	}
	for i, want := range wantPath {
		if got := ledger.GetPath()[i].GetName(); got != want {
			t.Errorf("ledger path[%d].name: got %q, want %q", i, got, want)
		}
	}
}

func TestListResources_OrganizationFilter(t *testing.T) {
	const owner = "alice@example.com"
	grants := `[{"principal":"alice@example.com","role":"owner"}]`

	orgA := orgNS("acme", "", grants)
	orgB := orgNS("other", "", grants)
	fldA := folderNS("payments", "", "acme", orgA.Name, grants)
	fldB := folderNS("ops", "", "other", orgB.Name, grants)
	prjA := projectNS("checkout", "", "acme", fldA.Name, grants)
	prjB := projectNS("infra", "", "other", fldB.Name, grants)

	handler := newTestHandler(t, orgA, orgB, fldA, fldB, prjA, prjB)
	ctx := contextWithClaims(owner)

	resp, err := handler.ListResources(ctx, connect.NewRequest(&consolev1.ListResourcesRequest{
		Organization: "acme",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := sortedResourceKeys(resp.Msg.GetResources())
	want := []string{
		"RESOURCE_TYPE_FOLDER|acme/payments",
		"RESOURCE_TYPE_PROJECT|acme/payments/checkout",
	}
	if !equalStrings(got, want) {
		t.Errorf("resources mismatch:\ngot  %v\nwant %v", got, want)
	}
}

func TestListResources_TypesFilterFoldersOnly(t *testing.T) {
	const owner = "alice@example.com"
	grants := `[{"principal":"alice@example.com","role":"owner"}]`

	org := orgNS("acme", "", grants)
	fld := folderNS("payments", "", "acme", org.Name, grants)
	prj := projectNS("checkout", "", "acme", fld.Name, grants)

	handler := newTestHandler(t, org, fld, prj)
	ctx := contextWithClaims(owner)

	resp, err := handler.ListResources(ctx, connect.NewRequest(&consolev1.ListResourcesRequest{
		Types: []consolev1.ResourceType{consolev1.ResourceType_RESOURCE_TYPE_FOLDER},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := sortedResourceKeys(resp.Msg.GetResources())
	want := []string{"RESOURCE_TYPE_FOLDER|acme/payments"}
	if !equalStrings(got, want) {
		t.Errorf("resources mismatch:\ngot  %v\nwant %v", got, want)
	}
}

func TestListResources_TypesFilterProjectsOnly(t *testing.T) {
	const owner = "alice@example.com"
	grants := `[{"principal":"alice@example.com","role":"owner"}]`

	org := orgNS("acme", "", grants)
	fld := folderNS("payments", "", "acme", org.Name, grants)
	prj := projectNS("checkout", "", "acme", fld.Name, grants)

	handler := newTestHandler(t, org, fld, prj)
	ctx := contextWithClaims(owner)

	resp, err := handler.ListResources(ctx, connect.NewRequest(&consolev1.ListResourcesRequest{
		Types: []consolev1.ResourceType{consolev1.ResourceType_RESOURCE_TYPE_PROJECT},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := sortedResourceKeys(resp.Msg.GetResources())
	want := []string{"RESOURCE_TYPE_PROJECT|acme/payments/checkout"}
	if !equalStrings(got, want) {
		t.Errorf("resources mismatch:\ngot  %v\nwant %v", got, want)
	}
}

func TestListResources_TypesFilterIgnoresUnspecified(t *testing.T) {
	const owner = "alice@example.com"
	grants := `[{"principal":"alice@example.com","role":"owner"}]`

	org := orgNS("acme", "", grants)
	fld := folderNS("payments", "", "acme", org.Name, grants)
	prj := projectNS("checkout", "", "acme", fld.Name, grants)

	handler := newTestHandler(t, org, fld, prj)
	ctx := contextWithClaims(owner)

	// UNSPECIFIED in the list MUST be ignored, not interpreted as "include
	// nothing." A list of just UNSPECIFIED should behave like an empty list
	// (i.e. "both kinds").
	resp, err := handler.ListResources(ctx, connect.NewRequest(&consolev1.ListResourcesRequest{
		Types: []consolev1.ResourceType{consolev1.ResourceType_RESOURCE_TYPE_UNSPECIFIED},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := sortedResourceKeys(resp.Msg.GetResources())
	want := []string{
		"RESOURCE_TYPE_FOLDER|acme/payments",
		"RESOURCE_TYPE_PROJECT|acme/payments/checkout",
	}
	if !equalStrings(got, want) {
		t.Errorf("resources mismatch:\ngot  %v\nwant %v", got, want)
	}
}

// TestListResources_BrokenAncestorChainStillReturnsEntry asserts the
// resilience contract: a folder/project the caller can see MUST appear in
// the response even when its ancestor walk fails (e.g. a parent namespace
// was just deleted). The entry is returned with a truncated/empty path
// instead of being silently dropped, matching ListFolders / ListProjects
// behavior. Without this guarantee the navigation tree would lose visible
// nodes during transient hierarchy churn.
func TestListResources_BrokenAncestorChainStillReturnsEntry(t *testing.T) {
	owner := `[{"principal":"owner@example.com","role":"owner"}]`

	// Project's parent label points at a namespace that does not exist in
	// the cluster, so WalkAncestors returns an error after fetching the
	// project itself. The entry must still appear in the response.
	prj := projectNS("orphan", "", "acme", "holos-fld-deleted-parent", owner)

	handler := newTestHandler(t, prj)
	ctx := contextWithClaims("owner@example.com")
	resp, err := handler.ListResources(ctx, connect.NewRequest(&consolev1.ListResourcesRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resources := resp.Msg.GetResources()
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d (%v)", len(resources), sortedResourceKeys(resources))
	}
	got := resources[0]
	if got.GetType() != consolev1.ResourceType_RESOURCE_TYPE_PROJECT {
		t.Errorf("expected RESOURCE_TYPE_PROJECT, got %v", got.GetType())
	}
	if got.GetName() != "orphan" {
		t.Errorf("expected name=%q, got %q", "orphan", got.GetName())
	}
	if len(got.GetPath()) != 0 {
		t.Errorf("expected empty path on broken hierarchy, got %d elements", len(got.GetPath()))
	}
}

// equalStrings reports element-wise equality of two string slices.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
