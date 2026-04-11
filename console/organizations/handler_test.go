package organizations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// contextWithClaims creates a context with OIDC claims.
func contextWithClaims(email string, groups ...string) context.Context {
	claims := &rpc.Claims{
		Sub:           "sub-" + email,
		Email:         email,
		EmailVerified: true,
		Name:          email,
		Roles:         groups,
	}
	return rpc.ContextWithClaims(context.Background(), claims)
}

// orgNS creates an organization namespace with share-users annotation.
// Uses the default "holos-" namespace prefix matching testResolver().
func orgNS(name string, shareUsersJSON string) *corev1.Namespace {
	annotations := map[string]string{}
	if shareUsersJSON != "" {
		annotations[v1alpha2.AnnotationShareUsers] = shareUsersJSON
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-" + name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
				resolver.OrganizationLabel: name,
			},
			Annotations: annotations,
		},
	}
}

type testHandlerOpts struct {
	disableOrgCreation bool
	creatorUsers       []string
	creatorRoles       []string
	projectLister      ProjectLister
	folderCreator      FolderCreator
	folderLister       FolderLister
}

func newTestHandler(namespaces ...*corev1.Namespace) *Handler {
	return newTestHandlerWithOpts(testHandlerOpts{}, namespaces...)
}

func newTestHandlerWithOpts(opts testHandlerOpts, namespaces ...*corev1.Namespace) *Handler {
	objs := make([]runtime.Object, len(namespaces))
	for i, ns := range namespaces {
		objs[i] = ns
	}
	fakeClient := fake.NewClientset(objs...)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, opts.projectLister, opts.folderCreator, opts.folderLister, opts.disableOrgCreation, opts.creatorUsers, opts.creatorRoles)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return handler
}

// ---- ListOrganizations tests ----

func TestListOrganizations_ReturnsFilteredByAccess(t *testing.T) {
	ns1 := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	ns2 := orgNS("beta", `[{"principal":"alice@example.com","role":"viewer"}]`)
	ns3 := orgNS("gamma", `[{"principal":"bob@example.com","role":"owner"}]`)

	handler := newTestHandler(ns1, ns2, ns3)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.ListOrganizations(ctx, connect.NewRequest(&consolev1.ListOrganizationsRequest{}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Organizations) != 2 {
		t.Fatalf("expected 2 organizations, got %d", len(resp.Msg.Organizations))
	}
}

func TestListOrganizations_Unauthenticated(t *testing.T) {
	handler := newTestHandler()
	_, err := handler.ListOrganizations(context.Background(), connect.NewRequest(&consolev1.ListOrganizationsRequest{}))
	assertUnauthenticated(t, err)
}

func TestListOrganizations_ReturnsOrgNameNotNamespace(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.ListOrganizations(ctx, connect.NewRequest(&consolev1.ListOrganizationsRequest{}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Organizations) != 1 {
		t.Fatalf("expected 1 org, got %d", len(resp.Msg.Organizations))
	}
	if resp.Msg.Organizations[0].Name != "acme" {
		t.Errorf("expected name 'acme', got %q", resp.Msg.Organizations[0].Name)
	}
}

// ---- GetOrganization tests ----

func TestGetOrganization_Authorized(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	ns.Annotations[v1alpha2.AnnotationDisplayName] = "ACME Corp"
	ns.Annotations[v1alpha2.AnnotationDescription] = "Test org"

	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.GetOrganization(ctx, connect.NewRequest(&consolev1.GetOrganizationRequest{Name: "acme"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	org := resp.Msg.Organization
	if org.Name != "acme" {
		t.Errorf("expected name 'acme', got %q", org.Name)
	}
	if org.DisplayName != "ACME Corp" {
		t.Errorf("expected display_name 'ACME Corp', got %q", org.DisplayName)
	}
	if org.UserRole != consolev1.Role_ROLE_VIEWER {
		t.Errorf("expected ROLE_VIEWER, got %v", org.UserRole)
	}
}

func TestGetOrganization_Denied(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"bob@example.com","role":"owner"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("nobody@example.com")

	_, err := handler.GetOrganization(ctx, connect.NewRequest(&consolev1.GetOrganizationRequest{Name: "acme"}))
	assertPermissionDenied(t, err)
}

func TestGetOrganization_InvalidArgument(t *testing.T) {
	handler := newTestHandler()
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.GetOrganization(ctx, connect.NewRequest(&consolev1.GetOrganizationRequest{Name: ""}))
	assertInvalidArgument(t, err)
}

// ---- CreateOrganization tests ----

func TestCreateOrganization_AuthorizedByCreatorUsers(t *testing.T) {
	handler := newTestHandlerWithOpts(testHandlerOpts{
		creatorUsers: []string{"alice@example.com"},
	})
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name:        "new-org",
		DisplayName: "New Org",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "new-org" {
		t.Errorf("expected name 'new-org', got %q", resp.Msg.Name)
	}
}

func TestCreateOrganization_AuthorizedByCreatorGroups(t *testing.T) {
	handler := newTestHandlerWithOpts(testHandlerOpts{
		creatorRoles: []string{"platform-admins"},
	})
	ctx := contextWithClaims("bob@example.com", "platform-admins")

	resp, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "new-org",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "new-org" {
		t.Errorf("expected name 'new-org', got %q", resp.Msg.Name)
	}
}

func TestCreateOrganization_DeniedNotInCreatorLists(t *testing.T) {
	handler := newTestHandlerWithOpts(testHandlerOpts{
		disableOrgCreation: true,
		creatorUsers:       []string{"admin@example.com"},
		creatorRoles:       []string{"platform-admins"},
	})
	ctx := contextWithClaims("alice@example.com", "developers")

	_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "new-org",
	}))
	assertPermissionDenied(t, err)
}

func TestCreateOrganization_ImplicitGrantAllAuthenticated(t *testing.T) {
	// With disableOrgCreation=false (default) and empty creator lists,
	// all authenticated users get an implicit grant to create orgs.
	handler := newTestHandlerWithOpts(testHandlerOpts{})
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "new-org",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "new-org" {
		t.Errorf("expected name 'new-org', got %q", resp.Msg.Name)
	}
}

func TestCreateOrganization_OwnershipNoLongerGrantsCreate(t *testing.T) {
	// Being owner on an existing org should NOT grant create permission
	// when --disable-org-creation is set and user is not in creator lists.
	existing := orgNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandlerWithOpts(testHandlerOpts{disableOrgCreation: true}, existing)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "new-org",
	}))
	assertPermissionDenied(t, err)
}

func TestCreateOrganization_DisabledHonorsCreatorUsers(t *testing.T) {
	// With disableOrgCreation=true, explicit --org-creator-users grants are still honored.
	handler := newTestHandlerWithOpts(testHandlerOpts{
		disableOrgCreation: true,
		creatorUsers:       []string{"alice@example.com"},
	})
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "new-org",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "new-org" {
		t.Errorf("expected name 'new-org', got %q", resp.Msg.Name)
	}
}

func TestCreateOrganization_DisabledHonorsCreatorRoles(t *testing.T) {
	// With disableOrgCreation=true, explicit --org-creator-roles grants are still honored.
	handler := newTestHandlerWithOpts(testHandlerOpts{
		disableOrgCreation: true,
		creatorRoles:       []string{"platform-admins"},
	})
	ctx := contextWithClaims("bob@example.com", "platform-admins")

	resp, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "new-org",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "new-org" {
		t.Errorf("expected name 'new-org', got %q", resp.Msg.Name)
	}
}

func TestCreateOrganization_DisabledDeniesWithoutExplicitGrant(t *testing.T) {
	// With disableOrgCreation=true and user NOT in any creator list, creation is denied.
	handler := newTestHandlerWithOpts(testHandlerOpts{
		disableOrgCreation: true,
	})
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "new-org",
	}))
	assertPermissionDenied(t, err)
}

func TestCreateOrganization_AutoOwner(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, nil, nil, nil, false, []string{"alice@example.com"}, nil)

	ctx := contextWithClaims("alice@example.com")
	_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "new-org",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ns, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-org-new-org", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected namespace to exist, got %v", err)
	}
	users, err := GetShareUsers(ns)
	if err != nil {
		t.Fatalf("failed to parse share-users: %v", err)
	}
	found := false
	for _, u := range users {
		if u.Principal == "alice@example.com" && u.Role == "owner" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected creator as owner in share-users, got %v", users)
	}
}

// ---- UpdateOrganization tests ----

func TestUpdateOrganization_EditorAllows(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	displayName := "Updated"
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:        "acme",
		DisplayName: &displayName,
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestUpdateOrganization_ViewerDenies(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	displayName := "Updated"
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:        "acme",
		DisplayName: &displayName,
	}))
	assertPermissionDenied(t, err)
}

// ---- DeleteOrganization tests ----

func TestDeleteOrganization_OwnerAllows(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteOrganization(ctx, connect.NewRequest(&consolev1.DeleteOrganizationRequest{Name: "acme"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDeleteOrganization_EditorDenies(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteOrganization(ctx, connect.NewRequest(&consolev1.DeleteOrganizationRequest{Name: "acme"}))
	assertPermissionDenied(t, err)
}

func TestDeleteOrganization_FailsWithLinkedProjects(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandlerWithOpts(testHandlerOpts{
		projectLister: &mockProjectLister{
			projects: []*corev1.Namespace{{ObjectMeta: metav1.ObjectMeta{Name: "prj-myproject"}}},
		},
	}, ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteOrganization(ctx, connect.NewRequest(&consolev1.DeleteOrganizationRequest{Name: "acme"}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeFailedPrecondition {
		t.Errorf("expected CodeFailedPrecondition, got %v", connectErr.Code())
	}
}

func TestDeleteOrganization_SucceedsWithNoLinkedProjects(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandlerWithOpts(testHandlerOpts{
		projectLister: &mockProjectLister{projects: nil},
	}, ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteOrganization(ctx, connect.NewRequest(&consolev1.DeleteOrganizationRequest{Name: "acme"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// mockProjectLister implements ProjectLister for testing.
type mockProjectLister struct {
	projects []*corev1.Namespace
	err      error
}

func (m *mockProjectLister) ListProjects(_ context.Context, _, _ string) ([]*corev1.Namespace, error) {
	return m.projects, m.err
}

// ---- UpdateOrganizationSharing tests ----

func TestUpdateOrgSharing_OwnerAllows(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.UpdateOrganizationSharing(ctx, connect.NewRequest(&consolev1.UpdateOrganizationSharingRequest{
		Name: "acme",
		UserGrants: []*consolev1.ShareGrant{
			{Principal: "alice@example.com", Role: consolev1.Role_ROLE_OWNER},
			{Principal: "bob@example.com", Role: consolev1.Role_ROLE_EDITOR},
		},
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Organization.UserGrants) != 2 {
		t.Errorf("expected 2 user grants, got %d", len(resp.Msg.Organization.UserGrants))
	}
}

func TestUpdateOrgSharing_WithRoleGrants(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.UpdateOrganizationSharing(ctx, connect.NewRequest(&consolev1.UpdateOrganizationSharingRequest{
		Name: "acme",
		UserGrants: []*consolev1.ShareGrant{
			{Principal: "alice@example.com", Role: consolev1.Role_ROLE_OWNER},
		},
		RoleGrants: []*consolev1.ShareGrant{
			{Principal: "dev-team", Role: consolev1.Role_ROLE_EDITOR},
			{Principal: "platform-admins", Role: consolev1.Role_ROLE_OWNER},
		},
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Organization.UserGrants) != 1 {
		t.Errorf("expected 1 user grant, got %d", len(resp.Msg.Organization.UserGrants))
	}
	if len(resp.Msg.Organization.RoleGrants) != 2 {
		t.Errorf("expected 2 role grants, got %d", len(resp.Msg.Organization.RoleGrants))
	}

	// Verify role annotations are persisted to K8s
	k8sNS, err := handler.k8s.client.CoreV1().Namespaces().Get(context.Background(), "holos-org-acme", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected namespace to exist, got %v", err)
	}
	rolesJSON := k8sNS.Annotations[v1alpha2.AnnotationShareRoles]
	if rolesJSON == "" {
		t.Fatal("expected share-roles annotation to be set")
	}
	var roles []secrets.AnnotationGrant
	if err := json.Unmarshal([]byte(rolesJSON), &roles); err != nil {
		t.Fatalf("failed to parse share-roles: %v", err)
	}
	if len(roles) != 2 {
		t.Errorf("expected 2 roles in annotation, got %d", len(roles))
	}
}

func TestUpdateOrgSharing_RoleGrantsOnly(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.UpdateOrganizationSharing(ctx, connect.NewRequest(&consolev1.UpdateOrganizationSharingRequest{
		Name: "acme",
		RoleGrants: []*consolev1.ShareGrant{
			{Principal: "dev-team", Role: consolev1.Role_ROLE_VIEWER},
		},
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Organization.UserGrants) != 0 {
		t.Errorf("expected 0 user grants, got %d", len(resp.Msg.Organization.UserGrants))
	}
	if len(resp.Msg.Organization.RoleGrants) != 1 {
		t.Errorf("expected 1 role grant, got %d", len(resp.Msg.Organization.RoleGrants))
	}
}

func TestUpdateOrgSharing_NonOwnerDenies(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.UpdateOrganizationSharing(ctx, connect.NewRequest(&consolev1.UpdateOrganizationSharingRequest{
		Name: "acme",
		UserGrants: []*consolev1.ShareGrant{
			{Principal: "alice@example.com", Role: consolev1.Role_ROLE_OWNER},
		},
	}))
	assertPermissionDenied(t, err)
}

// ---- UpdateOrganizationDefaultSharing tests ----

func TestUpdateOrgDefaultSharing_OwnerAllows(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.UpdateOrganizationDefaultSharing(ctx, connect.NewRequest(&consolev1.UpdateOrganizationDefaultSharingRequest{
		Name: "acme",
		DefaultUserGrants: []*consolev1.ShareGrant{
			{Principal: "bob@example.com", Role: consolev1.Role_ROLE_EDITOR},
		},
		DefaultRoleGrants: []*consolev1.ShareGrant{
			{Principal: "engineering", Role: consolev1.Role_ROLE_VIEWER},
		},
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	org := resp.Msg.Organization
	if len(org.DefaultUserGrants) != 1 {
		t.Errorf("expected 1 default user grant, got %d", len(org.DefaultUserGrants))
	}
	if len(org.DefaultRoleGrants) != 1 {
		t.Errorf("expected 1 default role grant, got %d", len(org.DefaultRoleGrants))
	}
	if org.DefaultUserGrants[0].Principal != "bob@example.com" {
		t.Errorf("expected principal bob@example.com, got %q", org.DefaultUserGrants[0].Principal)
	}
}

func TestUpdateOrgDefaultSharing_NonOwnerDenies(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.UpdateOrganizationDefaultSharing(ctx, connect.NewRequest(&consolev1.UpdateOrganizationDefaultSharingRequest{
		Name: "acme",
		DefaultUserGrants: []*consolev1.ShareGrant{
			{Principal: "bob@example.com", Role: consolev1.Role_ROLE_EDITOR},
		},
	}))
	assertPermissionDenied(t, err)
}

func TestUpdateOrgDefaultSharing_EmptyNameRejects(t *testing.T) {
	handler := newTestHandler()
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.UpdateOrganizationDefaultSharing(ctx, connect.NewRequest(&consolev1.UpdateOrganizationDefaultSharingRequest{
		Name: "",
	}))
	assertInvalidArgument(t, err)
}

func TestUpdateOrgDefaultSharing_Unauthenticated(t *testing.T) {
	handler := newTestHandler()
	_, err := handler.UpdateOrganizationDefaultSharing(context.Background(), connect.NewRequest(&consolev1.UpdateOrganizationDefaultSharingRequest{
		Name: "acme",
	}))
	assertUnauthenticated(t, err)
}

func TestBuildOrganization_IncludesDefaultSharing(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	ns.Annotations[v1alpha2.AnnotationDefaultShareUsers] = `[{"principal":"bob@example.com","role":"editor"}]`
	ns.Annotations[v1alpha2.AnnotationDefaultShareRoles] = `[{"principal":"engineering","role":"viewer"}]`

	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.GetOrganization(ctx, connect.NewRequest(&consolev1.GetOrganizationRequest{Name: "acme"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	org := resp.Msg.Organization
	if len(org.DefaultUserGrants) != 1 {
		t.Fatalf("expected 1 default user grant, got %d", len(org.DefaultUserGrants))
	}
	if org.DefaultUserGrants[0].Principal != "bob@example.com" {
		t.Errorf("expected bob@example.com, got %q", org.DefaultUserGrants[0].Principal)
	}
	if len(org.DefaultRoleGrants) != 1 {
		t.Fatalf("expected 1 default role grant, got %d", len(org.DefaultRoleGrants))
	}
	if org.DefaultRoleGrants[0].Principal != "engineering" {
		t.Errorf("expected engineering, got %q", org.DefaultRoleGrants[0].Principal)
	}
}

// ---- GetOrganizationRaw tests ----

func TestGetOrganizationRaw_ReturnsNamespaceJSON(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	ns.Annotations[v1alpha2.AnnotationDisplayName] = "ACME Corp"
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.GetOrganizationRaw(ctx, connect.NewRequest(&consolev1.GetOrganizationRawRequest{Name: "acme"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(resp.Msg.Raw), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got parse error: %v", err)
	}
	if parsed["apiVersion"] != "v1" {
		t.Errorf("expected apiVersion 'v1', got %v", parsed["apiVersion"])
	}
	if parsed["kind"] != "Namespace" {
		t.Errorf("expected kind 'Namespace', got %v", parsed["kind"])
	}
	metadata := parsed["metadata"].(map[string]interface{})
	if metadata["name"] != "holos-org-acme" {
		t.Errorf("expected metadata.name 'holos-org-acme', got %v", metadata["name"])
	}
	labels := metadata["labels"].(map[string]interface{})
	if labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
		t.Errorf("expected managed-by label, got %v", labels[v1alpha2.LabelManagedBy])
	}
	if labels[resolver.ResourceTypeLabel] != resolver.ResourceTypeOrganization {
		t.Errorf("expected resource-type label, got %v", labels[resolver.ResourceTypeLabel])
	}
}

func TestGetOrganizationRaw_DeniesUnauthorized(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"bob@example.com","role":"owner"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("nobody@example.com")

	_, err := handler.GetOrganizationRaw(ctx, connect.NewRequest(&consolev1.GetOrganizationRawRequest{Name: "acme"}))
	assertPermissionDenied(t, err)
}

// ---- Label-based name extraction tests ----

func TestBuildOrganization_UsesLabelNotNamespaceParsing(t *testing.T) {
	// When namespace-prefix + org-prefix overlap with the org name,
	// namespace parsing produces wrong results. The label is authoritative.
	r := &resolver.Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "o-", ProjectPrefix: "p-"}
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, r)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-o-holos", // namespace-prefix "holos-" + org-prefix "o-" + name "holos"
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
				resolver.OrganizationLabel: "holos",
			},
		},
	}

	org := buildOrganization(k8s, ns, nil, nil, 0)
	if org.Name != "holos" {
		t.Errorf("expected org name 'holos', got %q", org.Name)
	}
}

func TestBuildOrganization_LabelTakesPrecedenceOverParsing(t *testing.T) {
	// Label value differs from what OrgFromNamespace would produce.
	r := &resolver.Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, r)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "org-legacy-name",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
				resolver.OrganizationLabel: "correct-name",
			},
		},
	}

	org := buildOrganization(k8s, ns, nil, nil, 0)
	if org.Name != "correct-name" {
		t.Errorf("expected org name 'correct-name', got %q", org.Name)
	}
}

func TestListOrganizations_UsesLabelWithNamespacePrefix(t *testing.T) {
	// Integration test: full flow with namespace prefix that would break parsing
	r := &resolver.Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "o-", ProjectPrefix: "p-"}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-o-holos",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
				resolver.OrganizationLabel: "holos",
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"viewer"}]`,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, r)
	handler := NewHandler(k8s, nil, nil, nil, false, nil, nil)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := contextWithClaims("alice@example.com")
	resp, err := handler.ListOrganizations(ctx, connect.NewRequest(&consolev1.ListOrganizationsRequest{}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Organizations) != 1 {
		t.Fatalf("expected 1 org, got %d", len(resp.Msg.Organizations))
	}
	if resp.Msg.Organizations[0].Name != "holos" {
		t.Errorf("expected name 'holos', got %q", resp.Msg.Organizations[0].Name)
	}
}

// ---- Namespace prefix tests ----

func TestCreateOrganization_NamespacePrefixIncluded(t *testing.T) {
	r := &resolver.Resolver{NamespacePrefix: "prod-", OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, r)
	handler := NewHandler(k8s, nil, nil, nil, false, []string{"alice@example.com"}, nil)

	ctx := contextWithClaims("alice@example.com")
	_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify the namespace name includes the namespace prefix
	ns, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "prod-org-acme", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected namespace prod-org-acme to exist, got %v", err)
	}
	if ns.Name != "prod-org-acme" {
		t.Errorf("expected namespace name 'prod-org-acme', got %q", ns.Name)
	}
}

// ---- Default folder tests ----

// mockFolderCreator implements FolderCreator for testing.
type mockFolderCreator struct {
	createdFolders []*corev1.Namespace
	createErr      error
	getFolders     map[string]*corev1.Namespace
	getErr         error
}

func (m *mockFolderCreator) CreateFolder(_ context.Context, name, displayName, description, org, parentNs, creatorEmail string, shareUsers, shareRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-fld-" + name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelOrganization: org,
				v1alpha2.LabelFolder:       name,
				v1alpha2.AnnotationParent:  parentNs,
			},
		},
	}
	m.createdFolders = append(m.createdFolders, ns)
	return ns, nil
}

func (m *mockFolderCreator) GetFolder(_ context.Context, name string) (*corev1.Namespace, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.getFolders != nil {
		ns, ok := m.getFolders[name]
		if !ok {
			return nil, fmt.Errorf("folder %q not found", name)
		}
		return ns, nil
	}
	return nil, fmt.Errorf("folder %q not found", name)
}

// mockFolderLister implements FolderLister for testing.
type mockFolderLister struct {
	children []*corev1.Namespace
	err      error
}

func (m *mockFolderLister) ListChildFolders(_ context.Context, _ string) ([]*corev1.Namespace, error) {
	return m.children, m.err
}

func TestCreateOrganization_CreatesDefaultFolder(t *testing.T) {
	fc := &mockFolderCreator{}
	handler := newTestHandlerWithOpts(testHandlerOpts{
		folderCreator: fc,
	})
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "acme" {
		t.Errorf("expected name 'acme', got %q", resp.Msg.Name)
	}
	// Verify a folder was created
	if len(fc.createdFolders) != 1 {
		t.Fatalf("expected 1 folder created, got %d", len(fc.createdFolders))
	}
	folder := fc.createdFolders[0]
	if folder.Labels[v1alpha2.LabelFolder] != "acme-default" {
		t.Errorf("expected folder name 'acme-default', got %q", folder.Labels[v1alpha2.LabelFolder])
	}
	if folder.Labels[v1alpha2.LabelOrganization] != "acme" {
		t.Errorf("expected folder org 'acme', got %q", folder.Labels[v1alpha2.LabelOrganization])
	}
	if folder.Labels[v1alpha2.AnnotationParent] != "holos-org-acme" {
		t.Errorf("expected folder parent 'holos-org-acme', got %q", folder.Labels[v1alpha2.AnnotationParent])
	}

	// Verify default folder annotation was set on the org namespace
	orgNs, err := handler.k8s.client.CoreV1().Namespaces().Get(context.Background(), "holos-org-acme", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected org namespace to exist, got %v", err)
	}
	if orgNs.Annotations[v1alpha2.AnnotationDefaultFolder] != "acme-default" {
		t.Errorf("expected default-folder annotation 'acme-default', got %q", orgNs.Annotations[v1alpha2.AnnotationDefaultFolder])
	}
}

func TestCreateOrganization_CustomDefaultFolderName(t *testing.T) {
	fc := &mockFolderCreator{}
	handler := newTestHandlerWithOpts(testHandlerOpts{
		folderCreator: fc,
	})
	ctx := contextWithClaims("alice@example.com")

	customName := "production"
	_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name:          "acme",
		DefaultFolder: &customName,
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(fc.createdFolders) != 1 {
		t.Fatalf("expected 1 folder created, got %d", len(fc.createdFolders))
	}
	if fc.createdFolders[0].Labels[v1alpha2.LabelFolder] != "production" {
		t.Errorf("expected folder name 'production', got %q", fc.createdFolders[0].Labels[v1alpha2.LabelFolder])
	}

	// Verify annotation reflects custom name
	orgNs, err := handler.k8s.client.CoreV1().Namespaces().Get(context.Background(), "holos-org-acme", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected org namespace to exist, got %v", err)
	}
	if orgNs.Annotations[v1alpha2.AnnotationDefaultFolder] != "production" {
		t.Errorf("expected default-folder annotation 'production', got %q", orgNs.Annotations[v1alpha2.AnnotationDefaultFolder])
	}
}

func TestCreateOrganization_FolderFailureRollsBack(t *testing.T) {
	fc := &mockFolderCreator{createErr: fmt.Errorf("folder creation failed")}
	handler := newTestHandlerWithOpts(testHandlerOpts{
		folderCreator: fc,
	})
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "acme",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", connectErr.Code())
	}

	// Verify the org namespace was rolled back (deleted)
	_, getErr := handler.k8s.client.CoreV1().Namespaces().Get(context.Background(), "holos-org-acme", metav1.GetOptions{})
	if getErr == nil {
		t.Error("expected org namespace to be deleted after rollback, but it still exists")
	}
}

func TestUpdateOrganization_UpdateDefaultFolder(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	ns.Annotations[v1alpha2.AnnotationDefaultFolder] = "default"

	// Set up a folder that is a child of the org
	childFolder := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-fld-staging",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelOrganization: "acme",
				v1alpha2.LabelFolder:       "staging",
				v1alpha2.AnnotationParent:  "holos-org-acme",
			},
		},
	}

	fc := &mockFolderCreator{
		getFolders: map[string]*corev1.Namespace{
			"staging": childFolder,
		},
	}
	fl := &mockFolderLister{
		children: []*corev1.Namespace{childFolder},
	}

	handler := newTestHandlerWithOpts(testHandlerOpts{
		folderCreator: fc,
		folderLister:  fl,
	}, ns)
	ctx := contextWithClaims("alice@example.com")

	newFolder := "staging"
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:          "acme",
		DefaultFolder: &newFolder,
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify the annotation was updated
	orgNs, err := handler.k8s.client.CoreV1().Namespaces().Get(context.Background(), "holos-org-acme", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected org namespace to exist, got %v", err)
	}
	if orgNs.Annotations[v1alpha2.AnnotationDefaultFolder] != "staging" {
		t.Errorf("expected default-folder annotation 'staging', got %q", orgNs.Annotations[v1alpha2.AnnotationDefaultFolder])
	}
}

func TestUpdateOrganization_DefaultFolderNotFound(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)

	fc := &mockFolderCreator{
		getFolders: map[string]*corev1.Namespace{}, // empty — folder doesn't exist
	}

	handler := newTestHandlerWithOpts(testHandlerOpts{
		folderCreator: fc,
	}, ns)
	ctx := contextWithClaims("alice@example.com")

	newFolder := "nonexistent"
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:          "acme",
		DefaultFolder: &newFolder,
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
	}
}

func TestUpdateOrganization_DefaultFolderNotChildOfOrg(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)

	// Folder exists but is not a child of this org
	otherFolder := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-fld-other",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelOrganization: "other-org",
				v1alpha2.LabelFolder:       "other",
				v1alpha2.AnnotationParent:  "holos-org-other-org",
			},
		},
	}

	fc := &mockFolderCreator{
		getFolders: map[string]*corev1.Namespace{
			"other": otherFolder,
		},
	}
	fl := &mockFolderLister{
		children: []*corev1.Namespace{}, // no children of acme
	}

	handler := newTestHandlerWithOpts(testHandlerOpts{
		folderCreator: fc,
		folderLister:  fl,
	}, ns)
	ctx := contextWithClaims("alice@example.com")

	newFolder := "other"
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:          "acme",
		DefaultFolder: &newFolder,
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
	}
}

func TestUpdateOrganization_DefaultFolderRequiresAdmin(t *testing.T) {
	// An editor should be denied when trying to change the default folder
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)

	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	newFolder := "staging"
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:          "acme",
		DefaultFolder: &newFolder,
	}))
	assertPermissionDenied(t, err)
}

func TestBuildOrganization_PopulatesDefaultFolder(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	ns.Annotations[v1alpha2.AnnotationDefaultFolder] = "my-folder"

	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.GetOrganization(ctx, connect.NewRequest(&consolev1.GetOrganizationRequest{Name: "acme"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Organization.DefaultFolder != "my-folder" {
		t.Errorf("expected default_folder 'my-folder', got %q", resp.Msg.Organization.DefaultFolder)
	}
}

func TestListOrganizations_PopulatesDefaultFolder(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	ns.Annotations[v1alpha2.AnnotationDefaultFolder] = "default"

	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.ListOrganizations(ctx, connect.NewRequest(&consolev1.ListOrganizationsRequest{}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Organizations) != 1 {
		t.Fatalf("expected 1 org, got %d", len(resp.Msg.Organizations))
	}
	if resp.Msg.Organizations[0].DefaultFolder != "default" {
		t.Errorf("expected default_folder 'default', got %q", resp.Msg.Organizations[0].DefaultFolder)
	}
}

// ---- Helpers ----

func assertUnauthenticated(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeUnauthenticated {
		t.Errorf("expected CodeUnauthenticated, got %v", connectErr.Code())
	}
}

func assertPermissionDenied(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodePermissionDenied {
		t.Errorf("expected CodePermissionDenied, got %v", connectErr.Code())
	}
}

func assertInvalidArgument(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
	}
}
