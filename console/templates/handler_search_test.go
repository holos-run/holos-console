// handler_search_test.go exercises the cross-scope SearchTemplates RPC
// introduced in HOL-602. The acceptance criteria are:
//
//   - filters across organization, folder, and project namespaces in a single
//     flat response;
//   - honors the existing per-scope RBAC on every namespace it visits;
//   - supports namespace, name (exact), display_name_contains (case-
//     insensitive substring), and organization filters.
//
// Tests follow the same pattern as TestRenderTemplateGroupedFolderScoped:
// fixtures are expressed as fake.Clientset namespaces plus template
// ConfigMaps, the testhelpers bridge translates the template ConfigMaps into
// Template CRDs that the rewritten K8sClient reads through the controller-
// runtime fake client.
package templates

import (
	"context"
	"sort"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

const (
	searchOrg     = "acme"
	searchFolder  = "payments"
	searchProject = "checkout"
)

// folderNSWithParent builds a folder namespace fixture with the given parent
// namespace label so the resolver/walker chain can traverse from a project
// back to its root organization through the folder. SearchTemplates does not
// itself walk ancestors, but exposing the full hierarchy in fixtures keeps
// the tests aligned with the production wiring.
func folderNSWithParent(folder, org, parentNs string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fld-" + folder,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelOrganization: org,
				v1alpha2.LabelFolder:       folder,
				v1alpha2.AnnotationParent:  parentNs,
			},
		},
	}
}

// projectNSWithParent builds a project namespace fixture with the given
// organization and parent namespace labels.
func projectNSWithParent(project, org, parentNs string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prj-" + project,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelOrganization: org,
				v1alpha2.LabelProject:      project,
				v1alpha2.AnnotationParent:  parentNs,
			},
		},
	}
}

// templateCMInNs builds a template ConfigMap fixture in the given namespace.
// Used to seed cross-scope template fixtures for SearchTemplates tests.
func templateCMInNs(ns, name, displayName string, scopeLabel string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: scopeLabel,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: displayName,
				v1alpha2.AnnotationEnabled:     "true",
			},
		},
		Data: map[string]string{CueTemplateKey: validCue},
	}
}

// newSearchTestHandler builds a handler wired against the cross-scope
// fixtures. The same shareUsers map drives org, folder, and project grant
// resolvers so callers can assert RBAC behavior at every level by toggling
// who is in the map and at what role.
func newSearchTestHandler(t *testing.T, fakeClient *fake.Clientset, shareUsers map[string]string) *Handler {
	t.Helper()
	r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
	k8s := newTestK8sClient(t, fakeClient, r)
	handler := NewHandler(k8s, r, &stubRenderer{}, policyresolver.NewNoopResolver())
	handler.WithOrgGrantResolver(&stubOrgGrantResolver{users: shareUsers})
	handler.WithFolderGrantResolver(&stubFolderGrantResolver{users: shareUsers})
	handler.WithProjectGrantResolver(&stubProjectGrantResolver{users: shareUsers})
	return handler
}

// crossScopeFixture builds a fake clientset seeded with one organization
// namespace, one folder namespace under that org, one project namespace
// under that folder, and one enabled template per scope. Returns the
// fake clientset plus the namespace strings for assertions.
func crossScopeFixture() *fake.Clientset {
	orgNsObj := orgNS(searchOrg)
	fldNsObj := folderNSWithParent(searchFolder, searchOrg, "org-"+searchOrg)
	prjNsObj := projectNSWithParent(searchProject, searchOrg, "fld-"+searchFolder)
	orgTmpl := templateCMInNs("org-"+searchOrg, "httproute", "HTTPRoute", v1alpha2.TemplateScopeOrganization)
	fldTmpl := templateCMInNs("fld-"+searchFolder, "payments-policy", "Payments Policy", v1alpha2.TemplateScopeFolder)
	prjTmpl := templateCMInNs("prj-"+searchProject, "checkout-app", "Checkout App", v1alpha2.TemplateScopeProject)
	return fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, orgTmpl, fldTmpl, prjTmpl)
}

// templateNamespaces returns the sorted set of namespaces from a slice of
// templates so test assertions don't depend on iteration order.
func templateNamespaces(tmpls []*consolev1.Template) []string {
	out := make([]string, 0, len(tmpls))
	for _, tmpl := range tmpls {
		out = append(out, tmpl.GetNamespace()+"/"+tmpl.GetName())
	}
	sort.Strings(out)
	return out
}

func TestSearchTemplates_Unauthenticated(t *testing.T) {
	handler := newSearchTestHandler(t, crossScopeFixture(), nil)
	_, err := handler.SearchTemplates(context.Background(), connect.NewRequest(&consolev1.SearchTemplatesRequest{}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got, want := connect.CodeOf(err), connect.CodeUnauthenticated; got != want {
		t.Errorf("expected code %v, got %v", want, got)
	}
}

func TestSearchTemplates_NoFiltersReturnsCrossScope(t *testing.T) {
	const owner = "platform@localhost"
	handler := newSearchTestHandler(t, crossScopeFixture(), map[string]string{owner: "owner"})
	ctx := authedCtx(owner, nil)

	resp, err := handler.SearchTemplates(ctx, connect.NewRequest(&consolev1.SearchTemplatesRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := templateNamespaces(resp.Msg.GetTemplates())
	want := []string{
		"fld-" + searchFolder + "/payments-policy",
		"org-" + searchOrg + "/httproute",
		"prj-" + searchProject + "/checkout-app",
	}
	if !equalStrings(got, want) {
		t.Errorf("templates mismatch:\ngot  %v\nwant %v", got, want)
	}
}

func TestSearchTemplates_NamespaceFilter(t *testing.T) {
	const owner = "platform@localhost"
	handler := newSearchTestHandler(t, crossScopeFixture(), map[string]string{owner: "owner"})
	ctx := authedCtx(owner, nil)

	resp, err := handler.SearchTemplates(ctx, connect.NewRequest(&consolev1.SearchTemplatesRequest{
		Namespace: "fld-" + searchFolder,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := templateNamespaces(resp.Msg.GetTemplates())
	want := []string{"fld-" + searchFolder + "/payments-policy"}
	if !equalStrings(got, want) {
		t.Errorf("templates mismatch:\ngot  %v\nwant %v", got, want)
	}
}

func TestSearchTemplates_NameExactFilter(t *testing.T) {
	const owner = "platform@localhost"
	handler := newSearchTestHandler(t, crossScopeFixture(), map[string]string{owner: "owner"})
	ctx := authedCtx(owner, nil)

	resp, err := handler.SearchTemplates(ctx, connect.NewRequest(&consolev1.SearchTemplatesRequest{
		Name: "checkout-app",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := templateNamespaces(resp.Msg.GetTemplates())
	want := []string{"prj-" + searchProject + "/checkout-app"}
	if !equalStrings(got, want) {
		t.Errorf("templates mismatch:\ngot  %v\nwant %v", got, want)
	}
}

func TestSearchTemplates_DisplayNameContainsCaseInsensitive(t *testing.T) {
	const owner = "platform@localhost"
	handler := newSearchTestHandler(t, crossScopeFixture(), map[string]string{owner: "owner"})
	ctx := authedCtx(owner, nil)

	// "Payments Policy" should match "PAYMENTS" case-insensitively.
	resp, err := handler.SearchTemplates(ctx, connect.NewRequest(&consolev1.SearchTemplatesRequest{
		DisplayNameContains: "PAYMENTS",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := templateNamespaces(resp.Msg.GetTemplates())
	want := []string{"fld-" + searchFolder + "/payments-policy"}
	if !equalStrings(got, want) {
		t.Errorf("templates mismatch:\ngot  %v\nwant %v", got, want)
	}
}

func TestSearchTemplates_OrganizationFilter(t *testing.T) {
	// Add a second organization with its own template — the filter should
	// exclude it.
	const otherOrg = "other-org"
	cs := crossScopeFixture()
	otherOrgNs := orgNS(otherOrg)
	otherTmpl := templateCMInNs("org-"+otherOrg, "other-template", "Other Template", v1alpha2.TemplateScopeOrganization)
	if _, err := cs.CoreV1().Namespaces().Create(context.Background(), otherOrgNs, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed other org: %v", err)
	}
	if _, err := cs.CoreV1().ConfigMaps(otherOrgNs.Name).Create(context.Background(), otherTmpl, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed other template: %v", err)
	}

	const owner = "platform@localhost"
	handler := newSearchTestHandler(t, cs, map[string]string{owner: "owner"})
	ctx := authedCtx(owner, nil)

	resp, err := handler.SearchTemplates(ctx, connect.NewRequest(&consolev1.SearchTemplatesRequest{
		Organization: searchOrg,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := templateNamespaces(resp.Msg.GetTemplates())
	want := []string{
		"fld-" + searchFolder + "/payments-policy",
		"org-" + searchOrg + "/httproute",
		"prj-" + searchProject + "/checkout-app",
	}
	if !equalStrings(got, want) {
		t.Errorf("templates mismatch:\ngot  %v\nwant %v", got, want)
	}
}

func TestSearchTemplates_RBACFiltersUnauthorizedNamespaces(t *testing.T) {
	// The viewer email is granted only on the project namespace by virtue of
	// being in the project's share-users; org and folder grant resolvers
	// return an empty users map for this email so the caller is filtered
	// out at those scopes. Org/folder templates must be excluded; the
	// project template must be included.
	const viewer = "viewer@localhost"
	cs := crossScopeFixture()
	// Stub resolvers all return the same shareUsers map. Distinguish by
	// installing a per-resolver stub: project grants the viewer; org and
	// folder do not.
	r := &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
	k8s := newTestK8sClient(t, cs, r)
	handler := NewHandler(k8s, r, &stubRenderer{}, policyresolver.NewNoopResolver())
	handler.WithOrgGrantResolver(&stubOrgGrantResolver{users: map[string]string{}})
	handler.WithFolderGrantResolver(&stubFolderGrantResolver{users: map[string]string{}})
	handler.WithProjectGrantResolver(&stubProjectGrantResolver{users: map[string]string{viewer: "viewer"}})

	ctx := authedCtx(viewer, nil)
	resp, err := handler.SearchTemplates(ctx, connect.NewRequest(&consolev1.SearchTemplatesRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := templateNamespaces(resp.Msg.GetTemplates())
	want := []string{"prj-" + searchProject + "/checkout-app"}
	if !equalStrings(got, want) {
		t.Errorf("templates mismatch:\ngot  %v\nwant %v", got, want)
	}
}

func TestSearchTemplates_NamespaceFilterRespectsRBAC(t *testing.T) {
	// When namespace is set explicitly to a folder the caller cannot see,
	// the response is empty (not an error — the contract is "templates
	// visible to the caller").
	const otherUser = "other@localhost"
	handler := newSearchTestHandler(t, crossScopeFixture(), nil) // no grants
	ctx := authedCtx(otherUser, nil)

	resp, err := handler.SearchTemplates(ctx, connect.NewRequest(&consolev1.SearchTemplatesRequest{
		Namespace: "fld-" + searchFolder,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(resp.Msg.GetTemplates()); got != 0 {
		t.Errorf("expected 0 templates, got %d (%v)", got, templateNamespaces(resp.Msg.GetTemplates()))
	}
}

// stubOrgGrantResolver is shared with handler_release_test.go but redefined
// here for clarity: tests in this file may need to wire a fresh stub per
// scope. The two stubs are structurally identical; the duplication is
// harmless because handler_release_test.go's stub remains the single
// definition for that file's tests.
//
// Actually: we cannot redefine stubOrgGrantResolver here without a duplicate
// declaration error. The package-level stubOrgGrantResolver from
// handler_release_test.go is reused.

// equalStrings returns true if two string slices are element-wise equal.
// Local helper to avoid pulling in a comparison library for a single use.
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

// _ = rpc.ClaimsFromContext keeps the import alive in case future tests in
// this file want to inspect claims directly. The current tests pass through
// authedCtx which already exercises the rpc package.
var _ = rpc.ClaimsFromContext
