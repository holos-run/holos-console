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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
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
	withFolderCreator  bool
	withDefaultsSeeder bool
	templateSeeder     TemplateSeeder
	projectCreator     ProjectCreator
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
	r := testResolver()
	k8s := NewK8sClient(fakeClient, r)
	handler := NewHandler(k8s, opts.projectLister, opts.disableOrgCreation, opts.creatorUsers, opts.creatorRoles)

	if opts.withFolderCreator {
		fc := &k8sFolderCreator{client: fakeClient, resolver: r}
		folderPrefix := r.NamespacePrefix + r.FolderPrefix
		handler.WithFolderCreator(fc, fc, folderPrefix)
	}

	if opts.withDefaultsSeeder {
		ts := opts.templateSeeder
		if ts == nil {
			ts = &mockTemplateSeeder{}
		}
		pc := opts.projectCreator
		if pc == nil {
			pc = &k8sProjectCreator{client: fakeClient, resolver: r}
		}
		projectPrefix := r.NamespacePrefix + r.ProjectPrefix
		handler.WithDefaultsSeeder(ts, pc, projectPrefix)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return handler
}

// k8sFolderCreator implements FolderCreator and FolderLister for tests.
type k8sFolderCreator struct {
	client   *fake.Clientset
	resolver *resolver.Resolver
	createFn func(ctx context.Context, name, displayName, description, org, parentNs, creatorEmail, creatorSubject string, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles []secrets.AnnotationGrant) (*corev1.Namespace, error)
}

func (f *k8sFolderCreator) CreateFolder(ctx context.Context, name, displayName, description, org, parentNs, creatorEmail, creatorSubject string, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	if f.createFn != nil {
		return f.createFn(ctx, name, displayName, description, org, parentNs, creatorEmail, creatorSubject, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles)
	}
	usersJSON, _ := json.Marshal(shareUsers)
	rolesJSON, _ := json.Marshal(shareRoles)
	annotations := map[string]string{
		v1alpha2.AnnotationShareUsers: string(usersJSON),
		v1alpha2.AnnotationShareRoles: string(rolesJSON),
	}
	if len(defaultShareUsers) > 0 {
		defaultUsersJSON, _ := json.Marshal(defaultShareUsers)
		annotations[v1alpha2.AnnotationDefaultShareUsers] = string(defaultUsersJSON)
	}
	if len(defaultShareRoles) > 0 {
		defaultRolesJSON, _ := json.Marshal(defaultShareRoles)
		annotations[v1alpha2.AnnotationDefaultShareRoles] = string(defaultRolesJSON)
	}
	if displayName != "" {
		annotations[v1alpha2.AnnotationDisplayName] = displayName
	}
	if creatorEmail != "" {
		annotations[v1alpha2.AnnotationCreatorEmail] = creatorEmail
	}
	if creatorSubject != "" {
		annotations[v1alpha2.AnnotationCreatorSubject] = creatorSubject
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: f.resolver.FolderNamespace(name),
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
	return f.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
}

func (f *k8sFolderCreator) DeleteFolder(ctx context.Context, name string) error {
	nsName := f.resolver.FolderNamespace(name)
	return f.client.CoreV1().Namespaces().Delete(ctx, nsName, metav1.DeleteOptions{})
}

func (f *k8sFolderCreator) NamespaceExists(ctx context.Context, nsName string) (bool, error) {
	_, err := f.client.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (f *k8sFolderCreator) GetFolder(ctx context.Context, name string) (*corev1.Namespace, error) {
	nsName := f.resolver.FolderNamespace(name)
	ns, err := f.client.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if ns.Labels == nil || ns.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeFolder {
		return nil, fmt.Errorf("namespace %q is not a folder", nsName)
	}
	return ns, nil
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
	handler := NewHandler(k8s, nil, false, []string{"alice@example.com"}, nil)

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
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
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
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
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
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
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
	handler := NewHandler(k8s, nil, false, nil, nil)
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
	r := &resolver.Resolver{NamespacePrefix: "prod-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, r)
	handler := NewHandler(k8s, nil, false, []string{"alice@example.com"}, nil)

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

// ---- Default folder creation tests ----

func TestCreateOrganization_CreatesDefaultFolder(t *testing.T) {
	handler := newTestHandlerWithOpts(testHandlerOpts{withFolderCreator: true})
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name:        "test-df-org",
		DisplayName: "Test Org",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "test-df-org" {
		t.Errorf("expected org name 'test-df-org', got %q", resp.Msg.Name)
	}

	// Verify the default folder namespace was created with slug "default".
	fc := handler.folderCreator.(*k8sFolderCreator)
	folderNsName := handler.k8s.resolver.NamespacePrefix + handler.k8s.resolver.FolderPrefix + "default"
	ns, err := fc.client.CoreV1().Namespaces().Get(context.Background(), folderNsName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected default folder namespace %q to exist, got %v", folderNsName, err)
	}
	if ns.Labels[v1alpha2.LabelOrganization] != "test-df-org" {
		t.Errorf("expected folder org label 'test-df-org', got %q", ns.Labels[v1alpha2.LabelOrganization])
	}
	if ns.Labels[v1alpha2.AnnotationParent] != "holos-org-test-df-org" {
		t.Errorf("expected folder parent 'holos-org-test-df-org', got %q", ns.Labels[v1alpha2.AnnotationParent])
	}
	if ns.Annotations[v1alpha2.AnnotationDisplayName] != "Default" {
		t.Errorf("expected folder display name 'Default', got %q", ns.Annotations[v1alpha2.AnnotationDisplayName])
	}

	// Verify default folder annotation on the org namespace.
	orgNs, err := fc.client.CoreV1().Namespaces().Get(context.Background(), "holos-org-test-df-org", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected org namespace to exist, got %v", err)
	}
	if orgNs.Annotations[v1alpha2.AnnotationDefaultFolder] != "default" {
		t.Errorf("expected default-folder annotation 'default', got %q", orgNs.Annotations[v1alpha2.AnnotationDefaultFolder])
	}
}

func TestCreateOrganization_CreatesDefaultFolderWithCustomName(t *testing.T) {
	handler := newTestHandlerWithOpts(testHandlerOpts{withFolderCreator: true})
	ctx := contextWithClaims("alice@example.com")

	customName := "Engineering"
	_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name:          "test-custom-org",
		DefaultFolder: &customName,
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// The slug for "Engineering" is "engineering".
	fc := handler.folderCreator.(*k8sFolderCreator)
	folderNsName := "holos-fld-engineering"
	ns, err := fc.client.CoreV1().Namespaces().Get(context.Background(), folderNsName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected folder namespace %q to exist, got %v", folderNsName, err)
	}
	if ns.Annotations[v1alpha2.AnnotationDisplayName] != "Engineering" {
		t.Errorf("expected display name 'Engineering', got %q", ns.Annotations[v1alpha2.AnnotationDisplayName])
	}
}

func TestCreateOrganization_DefaultFolderCollisionAddsSuffix(t *testing.T) {
	// Pre-create a namespace that would collide with the default folder slug.
	existingFolder := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-fld-default",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
			},
		},
	}
	handler := newTestHandlerWithOpts(testHandlerOpts{withFolderCreator: true}, existingFolder)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "test-collision-org",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "test-collision-org" {
		t.Errorf("expected org name 'test-collision-org', got %q", resp.Msg.Name)
	}

	// Verify the org has a default folder annotation with a suffixed identifier.
	fc := handler.folderCreator.(*k8sFolderCreator)
	orgNs, err := fc.client.CoreV1().Namespaces().Get(context.Background(), "holos-org-test-collision-org", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected org namespace to exist, got %v", err)
	}
	dfAnnotation := orgNs.Annotations[v1alpha2.AnnotationDefaultFolder]
	if dfAnnotation == "" {
		t.Fatal("expected default-folder annotation to be set")
	}
	if dfAnnotation == "default" {
		t.Error("expected a suffixed folder identifier due to collision, got 'default'")
	}
	// Verify the suffixed folder namespace exists.
	suffixedNsName := "holos-fld-" + dfAnnotation
	if _, err := fc.client.CoreV1().Namespaces().Get(context.Background(), suffixedNsName, metav1.GetOptions{}); err != nil {
		t.Fatalf("expected suffixed folder namespace %q to exist, got %v", suffixedNsName, err)
	}
}

func TestCreateOrganization_DefaultFolderCreatorIsOwner(t *testing.T) {
	handler := newTestHandlerWithOpts(testHandlerOpts{withFolderCreator: true})
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "test-owner-org",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	fc := handler.folderCreator.(*k8sFolderCreator)
	ns, err := fc.client.CoreV1().Namespaces().Get(context.Background(), "holos-fld-default", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected default folder to exist, got %v", err)
	}

	var grants []secrets.AnnotationGrant
	if err := json.Unmarshal([]byte(ns.Annotations[v1alpha2.AnnotationShareUsers]), &grants); err != nil {
		t.Fatalf("failed to parse share-users: %v", err)
	}
	found := false
	for _, g := range grants {
		if g.Principal == "alice@example.com" && g.Role == "owner" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected creator as owner in folder share-users, got %v", grants)
	}
}

func TestCreateOrganization_RollbackOnFolderFailure(t *testing.T) {
	objs := []runtime.Object{}
	fakeClient := fake.NewClientset(objs...)
	r := testResolver()
	k8s := NewK8sClient(fakeClient, r)
	handler := NewHandler(k8s, nil, false, nil, nil)

	// Use a folder creator that always fails.
	failFC := &k8sFolderCreator{
		client:   fakeClient,
		resolver: r,
		createFn: func(ctx context.Context, name, displayName, description, org, parentNs, creatorEmail, creatorSubject string, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
			return nil, fmt.Errorf("simulated folder creation failure")
		},
	}
	folderPrefix := r.NamespacePrefix + r.FolderPrefix
	handler.WithFolderCreator(failFC, failFC, folderPrefix)

	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "test-rollback-org",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify the org namespace was cleaned up.
	_, getErr := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-org-test-rollback-org", metav1.GetOptions{})
	if !k8serrors.IsNotFound(getErr) {
		t.Errorf("expected org namespace to be deleted after rollback, got %v", getErr)
	}
}

func TestGetOrganization_IncludesDefaultFolder(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	ns.Annotations[v1alpha2.AnnotationDefaultFolder] = "my-default"

	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.GetOrganization(ctx, connect.NewRequest(&consolev1.GetOrganizationRequest{Name: "acme"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Organization.DefaultFolder != "my-default" {
		t.Errorf("expected default_folder 'my-default', got %q", resp.Msg.Organization.DefaultFolder)
	}
}

func TestListOrganizations_IncludesDefaultFolder(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	ns.Annotations[v1alpha2.AnnotationDefaultFolder] = "projects"

	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.ListOrganizations(ctx, connect.NewRequest(&consolev1.ListOrganizationsRequest{}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Organizations) != 1 {
		t.Fatalf("expected 1 org, got %d", len(resp.Msg.Organizations))
	}
	if resp.Msg.Organizations[0].DefaultFolder != "projects" {
		t.Errorf("expected default_folder 'projects', got %q", resp.Msg.Organizations[0].DefaultFolder)
	}
}

// ---- UpdateOrganization default folder tests ----

func TestUpdateOrganization_UpdateDefaultFolder(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	ns.Annotations[v1alpha2.AnnotationDefaultFolder] = "default"

	// Create the target folder namespace.
	folderNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-fld-engineering",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelOrganization: "acme",
				v1alpha2.LabelFolder:       "engineering",
				v1alpha2.AnnotationParent:  "holos-org-acme",
			},
		},
	}

	handler := newTestHandlerWithOpts(testHandlerOpts{withFolderCreator: true}, ns, folderNs)
	ctx := contextWithClaims("alice@example.com")

	newFolder := "engineering"
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:          "acme",
		DefaultFolder: &newFolder,
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify the annotation was updated.
	fc := handler.folderCreator.(*k8sFolderCreator)
	orgNs, err := fc.client.CoreV1().Namespaces().Get(context.Background(), "holos-org-acme", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected org namespace to exist, got %v", err)
	}
	if orgNs.Annotations[v1alpha2.AnnotationDefaultFolder] != "engineering" {
		t.Errorf("expected default-folder 'engineering', got %q", orgNs.Annotations[v1alpha2.AnnotationDefaultFolder])
	}
}

func TestUpdateOrganization_UpdateDefaultFolder_NonexistentFolder(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandlerWithOpts(testHandlerOpts{withFolderCreator: true}, ns)
	ctx := contextWithClaims("alice@example.com")

	nonexistent := "nonexistent"
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:          "acme",
		DefaultFolder: &nonexistent,
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", connectErr.Code())
	}
}

func TestUpdateOrganization_UpdateDefaultFolder_WrongOrg(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)

	// Create a folder that belongs to a different org.
	folderNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-fld-other-folder",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelOrganization: "other-org",
				v1alpha2.LabelFolder:       "other-folder",
				v1alpha2.AnnotationParent:  "holos-org-other-org",
			},
		},
	}

	handler := newTestHandlerWithOpts(testHandlerOpts{withFolderCreator: true}, ns, folderNs)
	ctx := contextWithClaims("alice@example.com")

	wrongFolder := "other-folder"
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:          "acme",
		DefaultFolder: &wrongFolder,
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

func TestUpdateOrganization_UpdateDefaultFolder_EditorDenied(t *testing.T) {
	// Editors can update display_name/description but NOT default_folder.
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	newFolder := "some-folder"
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:          "acme",
		DefaultFolder: &newFolder,
	}))
	assertPermissionDenied(t, err)
}

func TestUpdateOrganization_UpdateDefaultFolder_EmptyValue(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	empty := ""
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:          "acme",
		DefaultFolder: &empty,
	}))
	assertInvalidArgument(t, err)
}

// ---- UpdateOrganization gateway_namespace tests ----

// gatewayNamespaceFromK8s reads the gateway-namespace annotation directly from
// the fake clientset bypassing the handler. Used by the UpdateOrganization
// gateway_namespace tests to assert the persisted annotation state.
func gatewayNamespaceFromK8s(t *testing.T, h *Handler, org string) (string, bool) {
	t.Helper()
	ns, err := h.k8s.client.CoreV1().Namespaces().Get(context.Background(),
		h.k8s.resolver.OrgNamespace(org), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get namespace: %v", err)
	}
	v, ok := ns.Annotations[v1alpha2.AnnotationGatewayNamespace]
	return v, ok
}

func TestUpdateOrganization_GatewayNamespace_RoundTrip(t *testing.T) {
	// Editor-permission table that walks set, preserve (nil), clear, and
	// invalid-DNS-label paths through the handler so the proto field, the
	// k8s annotation, and the buildOrganization read-back all stay in sync.
	tests := []struct {
		name             string
		initial          string  // initial annotation value, "" means not set
		input            *string // value sent in the update request
		wantValue        string  // expected annotation after update
		wantSet          bool    // whether the annotation should be present
		wantInvalidArg   bool    // whether the call should return InvalidArgument
		wantOrgFieldGet  bool    // whether to also assert via GetOrganization RPC
		wantOrgFieldWant string
	}{
		{
			name:             "set when absent",
			initial:          "",
			input:            ptr("gw-system"),
			wantValue:        "gw-system",
			wantSet:          true,
			wantOrgFieldGet:  true,
			wantOrgFieldWant: "gw-system",
		},
		{
			name:             "preserve with nil",
			initial:          "existing-gw",
			input:            nil,
			wantValue:        "existing-gw",
			wantSet:          true,
			wantOrgFieldGet:  true,
			wantOrgFieldWant: "existing-gw",
		},
		{
			name:             "clear with empty string",
			initial:          "existing-gw",
			input:            ptr(""),
			wantValue:        "",
			wantSet:          false,
			wantOrgFieldGet:  true,
			wantOrgFieldWant: "",
		},
		{
			name:           "invalid DNS-1123 label rejected",
			initial:        "existing-gw",
			input:          ptr("Invalid_Name"),
			wantValue:      "existing-gw", // unchanged
			wantSet:        true,
			wantInvalidArg: true,
		},
		{
			name:           "invalid DNS-1123 label with dots rejected",
			initial:        "",
			input:          ptr("gw.example.com"),
			wantValue:      "",
			wantSet:        false,
			wantInvalidArg: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ns := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)
			if tc.initial != "" {
				ns.Annotations[v1alpha2.AnnotationGatewayNamespace] = tc.initial
			}
			handler := newTestHandler(ns)
			ctx := contextWithClaims("alice@example.com")

			_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
				Name:             "acme",
				GatewayNamespace: tc.input,
			}))
			if tc.wantInvalidArg {
				assertInvalidArgument(t, err)
			} else if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			got, ok := gatewayNamespaceFromK8s(t, handler, "acme")
			if ok != tc.wantSet {
				t.Errorf("annotation present=%t, want %t", ok, tc.wantSet)
			}
			if got != tc.wantValue {
				t.Errorf("annotation value=%q, want %q", got, tc.wantValue)
			}

			if tc.wantOrgFieldGet {
				resp, err := handler.GetOrganization(ctx, connect.NewRequest(&consolev1.GetOrganizationRequest{Name: "acme"}))
				if err != nil {
					t.Fatalf("GetOrganization: %v", err)
				}
				if resp.Msg.Organization.GatewayNamespace != tc.wantOrgFieldWant {
					t.Errorf("Organization.GatewayNamespace=%q, want %q",
						resp.Msg.Organization.GatewayNamespace, tc.wantOrgFieldWant)
				}
			}
		})
	}
}

func TestUpdateOrganization_GatewayNamespace_PreservesOtherAnnotations(t *testing.T) {
	// Setting gateway_namespace must not perturb display_name or description.
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	ns.Annotations[v1alpha2.AnnotationDisplayName] = "ACME Corp"
	ns.Annotations[v1alpha2.AnnotationDescription] = "the test org"
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:             "acme",
		GatewayNamespace: ptr("gw-system"),
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	updated, err := handler.k8s.client.CoreV1().Namespaces().Get(context.Background(),
		"holos-org-acme", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get namespace: %v", err)
	}
	if updated.Annotations[v1alpha2.AnnotationDisplayName] != "ACME Corp" {
		t.Errorf("display name perturbed: %q", updated.Annotations[v1alpha2.AnnotationDisplayName])
	}
	if updated.Annotations[v1alpha2.AnnotationDescription] != "the test org" {
		t.Errorf("description perturbed: %q", updated.Annotations[v1alpha2.AnnotationDescription])
	}
	if updated.Annotations[v1alpha2.AnnotationGatewayNamespace] != "gw-system" {
		t.Errorf("gateway-namespace=%q, want %q",
			updated.Annotations[v1alpha2.AnnotationGatewayNamespace], "gw-system")
	}
}

func TestListOrganizations_PopulatesGatewayNamespace(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	ns.Annotations[v1alpha2.AnnotationGatewayNamespace] = "gw-system"
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.ListOrganizations(ctx, connect.NewRequest(&consolev1.ListOrganizationsRequest{}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Organizations) != 1 {
		t.Fatalf("expected 1 org, got %d", len(resp.Msg.Organizations))
	}
	if resp.Msg.Organizations[0].GatewayNamespace != "gw-system" {
		t.Errorf("Organization.GatewayNamespace=%q, want %q",
			resp.Msg.Organizations[0].GatewayNamespace, "gw-system")
	}
}

func TestUpdateOrganization_GatewayNamespace_ViewerDenies(t *testing.T) {
	// Viewers are blocked from any UpdateOrganization mutation; gateway_namespace
	// rides on the same PERMISSION_ORGANIZATIONS_WRITE check (no new permission).
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:             "acme",
		GatewayNamespace: ptr("gw-system"),
	}))
	assertPermissionDenied(t, err)
}

// ---- PopulateDefaults tests ----

// mockTemplateSeeder implements TemplateSeeder for tests.
type mockTemplateSeeder struct {
	orgTemplateSeeded     bool
	projectTemplateSeeded bool
	seedOrgErr            error
	seedProjectErr        error
	// Track the project name for verification.
	seededProjectName string
}

func (m *mockTemplateSeeder) SeedOrgTemplate(_ context.Context, _ string) error {
	if m.seedOrgErr != nil {
		return m.seedOrgErr
	}
	m.orgTemplateSeeded = true
	return nil
}

func (m *mockTemplateSeeder) SeedProjectTemplate(_ context.Context, project string) error {
	if m.seedProjectErr != nil {
		return m.seedProjectErr
	}
	m.projectTemplateSeeded = true
	m.seededProjectName = project
	return nil
}

// k8sProjectCreator implements ProjectCreator for tests.
type k8sProjectCreator struct {
	client    *fake.Clientset
	resolver  *resolver.Resolver
	createErr error
}

func (p *k8sProjectCreator) CreateProject(ctx context.Context, name, displayName, description, org, parentNs, creatorEmail, creatorSubject string, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles []secrets.AnnotationGrant) error {
	if p.createErr != nil {
		return p.createErr
	}
	usersJSON, _ := json.Marshal(shareUsers)
	rolesJSON, _ := json.Marshal(shareRoles)
	annotations := map[string]string{
		v1alpha2.AnnotationShareUsers: string(usersJSON),
		v1alpha2.AnnotationShareRoles: string(rolesJSON),
	}
	if len(defaultShareUsers) > 0 {
		defaultUsersJSON, _ := json.Marshal(defaultShareUsers)
		annotations[v1alpha2.AnnotationDefaultShareUsers] = string(defaultUsersJSON)
	}
	if len(defaultShareRoles) > 0 {
		defaultRolesJSON, _ := json.Marshal(defaultShareRoles)
		annotations[v1alpha2.AnnotationDefaultShareRoles] = string(defaultRolesJSON)
	}
	if displayName != "" {
		annotations[v1alpha2.AnnotationDisplayName] = displayName
	}
	if creatorEmail != "" {
		annotations[v1alpha2.AnnotationCreatorEmail] = creatorEmail
	}
	if creatorSubject != "" {
		annotations[v1alpha2.AnnotationCreatorSubject] = creatorSubject
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: p.resolver.ProjectNamespace(name),
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
	_, err := p.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	return err
}

func (p *k8sProjectCreator) DeleteProject(ctx context.Context, name string) error {
	nsName := p.resolver.ProjectNamespace(name)
	return p.client.CoreV1().Namespaces().Delete(ctx, nsName, metav1.DeleteOptions{})
}

func (p *k8sProjectCreator) NamespaceExists(ctx context.Context, nsName string) (bool, error) {
	_, err := p.client.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func TestCreateOrganization_PopulateDefaults(t *testing.T) {
	t.Run("true creates all expected resources", func(t *testing.T) {
		ts := &mockTemplateSeeder{}
		handler := newTestHandlerWithOpts(testHandlerOpts{
			withFolderCreator:  true,
			withDefaultsSeeder: true,
			templateSeeder:     ts,
		})
		ctx := contextWithClaims("alice@example.com")
		populateDefaults := true

		resp, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
			Name:             "seed-org",
			DisplayName:      "Seed Org",
			PopulateDefaults: &populateDefaults,
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Name != "seed-org" {
			t.Errorf("expected name 'seed-org', got %q", resp.Msg.Name)
		}

		// Verify org-level template was seeded.
		if !ts.orgTemplateSeeded {
			t.Error("expected org-level template to be seeded")
		}

		// Verify project-level template was seeded.
		if !ts.projectTemplateSeeded {
			t.Error("expected project-level template to be seeded")
		}

		// Verify the default project was created (check that at least one project namespace exists).
		pc := handler.projectCreator.(*k8sProjectCreator)
		projectNsName := pc.resolver.ProjectNamespace(ts.seededProjectName)
		projectNs, err := pc.client.CoreV1().Namespaces().Get(context.Background(), projectNsName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected default project namespace %q to exist, got %v", projectNsName, err)
		}
		if got, want := projectNs.Annotations[v1alpha2.AnnotationCreatorSubject], "sub-alice@example.com"; got != want {
			t.Fatalf("expected default project creator-sub annotation %q, got %q", want, got)
		}

		// Verify the seeded project inherited the org's default role grants
		// (Owner, Editor, Viewer) both as its active role grants and as its
		// own default role grants, mirroring projects.Handler.CreateProject.
		rolesAnnotation := projectNs.Annotations[v1alpha2.AnnotationShareRoles]
		if rolesAnnotation == "" {
			t.Fatalf("expected share-roles annotation on project namespace")
		}
		var projectRoles []secrets.AnnotationGrant
		if err := json.Unmarshal([]byte(rolesAnnotation), &projectRoles); err != nil {
			t.Fatalf("invalid share-roles annotation: %v", err)
		}
		wantRoles := map[string]bool{"owner": false, "editor": false, "viewer": false}
		for _, g := range projectRoles {
			if _, ok := wantRoles[g.Role]; ok && g.Principal == g.Role {
				wantRoles[g.Role] = true
			}
		}
		for role, seen := range wantRoles {
			if !seen {
				t.Errorf("expected seeded project to inherit org default role grant %q", role)
			}
		}

		defaultRolesAnnotation := projectNs.Annotations[v1alpha2.AnnotationDefaultShareRoles]
		if defaultRolesAnnotation == "" {
			t.Fatalf("expected default-share-roles annotation on project namespace")
		}
		var projectDefaultRoles []secrets.AnnotationGrant
		if err := json.Unmarshal([]byte(defaultRolesAnnotation), &projectDefaultRoles); err != nil {
			t.Fatalf("invalid default-share-roles annotation: %v", err)
		}
		if len(projectDefaultRoles) != 3 {
			t.Errorf("expected 3 default role grants copied from org, got %d", len(projectDefaultRoles))
		}

		// Verify the seeded default folder inherited the org's default role
		// grants on both share-roles and default-share-roles, analogous to
		// the project assertions above. This guards against regressions of
		// the bootstrap path skipping the ancestor-default-share merge.
		fc := handler.folderCreator.(*k8sFolderCreator)
		orgNsForFolder, err := handler.k8s.GetOrganization(context.Background(), "seed-org")
		if err != nil {
			t.Fatalf("failed to get org namespace: %v", err)
		}
		folderName := orgNsForFolder.Annotations[v1alpha2.AnnotationDefaultFolder]
		if folderName == "" {
			t.Fatalf("expected default folder annotation on org namespace")
		}
		folderNsName := fc.resolver.FolderNamespace(folderName)
		folderNs, err := fc.client.CoreV1().Namespaces().Get(context.Background(), folderNsName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected default folder namespace %q to exist, got %v", folderNsName, err)
		}

		folderRolesAnnotation := folderNs.Annotations[v1alpha2.AnnotationShareRoles]
		if folderRolesAnnotation == "" {
			t.Fatalf("expected share-roles annotation on folder namespace")
		}
		var folderRoles []secrets.AnnotationGrant
		if err := json.Unmarshal([]byte(folderRolesAnnotation), &folderRoles); err != nil {
			t.Fatalf("invalid share-roles annotation on folder: %v", err)
		}
		wantFolderRoles := map[string]bool{"owner": false, "editor": false, "viewer": false}
		for _, g := range folderRoles {
			if _, ok := wantFolderRoles[g.Role]; ok && g.Principal == g.Role {
				wantFolderRoles[g.Role] = true
			}
		}
		for role, seen := range wantFolderRoles {
			if !seen {
				t.Errorf("expected seeded default folder to inherit org default role grant %q on share-roles", role)
			}
		}

		// The seeded default folder must NOT carry its own default-share-*
		// annotations. Descendants pick up the current org defaults dynamically
		// via the ancestor walk, so persisting a snapshot on the folder would
		// shadow later org-level changes. See issue #933.
		if v, ok := folderNs.Annotations[v1alpha2.AnnotationDefaultShareRoles]; ok {
			t.Errorf("expected no %q annotation on seeded default folder, got %q", v1alpha2.AnnotationDefaultShareRoles, v)
		}
		if v, ok := folderNs.Annotations[v1alpha2.AnnotationDefaultShareUsers]; ok {
			t.Errorf("expected no %q annotation on seeded default folder, got %q", v1alpha2.AnnotationDefaultShareUsers, v)
		}
	})

	t.Run("false behaves as before", func(t *testing.T) {
		ts := &mockTemplateSeeder{}
		handler := newTestHandlerWithOpts(testHandlerOpts{
			withFolderCreator:  true,
			withDefaultsSeeder: true,
			templateSeeder:     ts,
		})
		ctx := contextWithClaims("alice@example.com")
		populateDefaults := false

		resp, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
			Name:             "no-seed-org",
			DisplayName:      "No Seed Org",
			PopulateDefaults: &populateDefaults,
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Name != "no-seed-org" {
			t.Errorf("expected name 'no-seed-org', got %q", resp.Msg.Name)
		}

		// Verify no seeding occurred.
		if ts.orgTemplateSeeded {
			t.Error("expected org-level template NOT to be seeded")
		}
		if ts.projectTemplateSeeded {
			t.Error("expected project-level template NOT to be seeded")
		}
	})

	t.Run("unset behaves as before", func(t *testing.T) {
		ts := &mockTemplateSeeder{}
		handler := newTestHandlerWithOpts(testHandlerOpts{
			withFolderCreator:  true,
			withDefaultsSeeder: true,
			templateSeeder:     ts,
		})
		ctx := contextWithClaims("alice@example.com")

		resp, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
			Name:        "unset-org",
			DisplayName: "Unset Org",
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Msg.Name != "unset-org" {
			t.Errorf("expected name 'unset-org', got %q", resp.Msg.Name)
		}

		// Verify no seeding occurred.
		if ts.orgTemplateSeeded {
			t.Error("expected org-level template NOT to be seeded")
		}
		if ts.projectTemplateSeeded {
			t.Error("expected project-level template NOT to be seeded")
		}
	})

	t.Run("rollback on org template seed failure", func(t *testing.T) {
		ts := &mockTemplateSeeder{seedOrgErr: fmt.Errorf("simulated org template failure")}
		handler := newTestHandlerWithOpts(testHandlerOpts{
			withFolderCreator:  true,
			withDefaultsSeeder: true,
			templateSeeder:     ts,
		})
		ctx := contextWithClaims("alice@example.com")
		populateDefaults := true

		_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
			Name:             "fail-seed-org",
			DisplayName:      "Fail Org",
			PopulateDefaults: &populateDefaults,
		}))
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		fc := handler.folderCreator.(*k8sFolderCreator)

		// Verify the org namespace was cleaned up.
		_, getErr := fc.client.CoreV1().Namespaces().Get(context.Background(), "holos-org-fail-seed-org", metav1.GetOptions{})
		if !k8serrors.IsNotFound(getErr) {
			t.Errorf("expected org namespace to be deleted after rollback, got %v", getErr)
		}

		// Verify the folder namespace was also cleaned up (namespaces are flat,
		// deleting the org does not cascade to the folder).
		nsList, _ := fc.client.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
		for _, ns := range nsList.Items {
			if ns.Labels != nil && ns.Labels[v1alpha2.LabelResourceType] == v1alpha2.ResourceTypeFolder {
				t.Errorf("expected folder namespace %q to be deleted after rollback", ns.Name)
			}
		}
	})

	t.Run("rollback on project creation failure", func(t *testing.T) {
		ts := &mockTemplateSeeder{}
		pc := &k8sProjectCreator{
			client:    nil, // will be set below
			resolver:  testResolver(),
			createErr: fmt.Errorf("simulated project creation failure"),
		}
		objs := []runtime.Object{}
		fakeClient := fake.NewClientset(objs...)
		r := testResolver()
		k8s := NewK8sClient(fakeClient, r)
		handler := NewHandler(k8s, nil, false, nil, nil)
		fc := &k8sFolderCreator{client: fakeClient, resolver: r}
		folderPrefix := r.NamespacePrefix + r.FolderPrefix
		handler.WithFolderCreator(fc, fc, folderPrefix)
		pc.client = fakeClient
		projectPrefix := r.NamespacePrefix + r.ProjectPrefix
		handler.WithDefaultsSeeder(ts, pc, projectPrefix)
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

		ctx := contextWithClaims("alice@example.com")
		populateDefaults := true

		_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
			Name:             "fail-proj-org",
			DisplayName:      "Fail Project Org",
			PopulateDefaults: &populateDefaults,
		}))
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		// Verify the org namespace was cleaned up.
		_, getErr := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-org-fail-proj-org", metav1.GetOptions{})
		if !k8serrors.IsNotFound(getErr) {
			t.Errorf("expected org namespace to be deleted after rollback, got %v", getErr)
		}

		// Verify the folder namespace was also cleaned up.
		nsList, _ := fakeClient.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
		for _, ns := range nsList.Items {
			if ns.Labels != nil && ns.Labels[v1alpha2.LabelResourceType] == v1alpha2.ResourceTypeFolder {
				t.Errorf("expected folder namespace %q to be deleted after rollback", ns.Name)
			}
		}
	})

	t.Run("rollback on project template seed failure", func(t *testing.T) {
		ts := &mockTemplateSeeder{seedProjectErr: fmt.Errorf("simulated project template failure")}
		handler := newTestHandlerWithOpts(testHandlerOpts{
			withFolderCreator:  true,
			withDefaultsSeeder: true,
			templateSeeder:     ts,
		})
		ctx := contextWithClaims("alice@example.com")
		populateDefaults := true

		_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
			Name:             "fail-ptmpl-org",
			DisplayName:      "Fail Project Template Org",
			PopulateDefaults: &populateDefaults,
		}))
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		fc := handler.folderCreator.(*k8sFolderCreator)

		// Verify the org namespace was cleaned up.
		_, getErr := fc.client.CoreV1().Namespaces().Get(context.Background(), "holos-org-fail-ptmpl-org", metav1.GetOptions{})
		if !k8serrors.IsNotFound(getErr) {
			t.Errorf("expected org namespace to be deleted after rollback, got %v", getErr)
		}

		// Verify the folder namespace was cleaned up.
		nsList, _ := fc.client.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
		for _, ns := range nsList.Items {
			if ns.Labels != nil && ns.Labels[v1alpha2.LabelResourceType] == v1alpha2.ResourceTypeFolder {
				t.Errorf("expected folder namespace %q to be deleted after rollback", ns.Name)
			}
		}

		// Verify the project namespace was cleaned up by seedDefaults'
		// incremental rollback (project was created in step 2, then step 3 failed).
		for _, ns := range nsList.Items {
			if ns.Labels != nil && ns.Labels[v1alpha2.LabelResourceType] == v1alpha2.ResourceTypeProject {
				t.Errorf("expected project namespace %q to be deleted after rollback", ns.Name)
			}
		}
	})
}

// TestCreateOrganization_SeedsDefaultRoleGrants verifies the org namespace's
// AnnotationDefaultShareRoles is populated with the three standard role
// grants (Owner, Editor, Viewer) *before* the default folder and default
// project are created when populate_defaults=true.
func TestCreateOrganization_SeedsDefaultRoleGrants(t *testing.T) {
	t.Run("populate_defaults=true writes Owner/Editor/Viewer default role grants", func(t *testing.T) {
		ts := &mockTemplateSeeder{}
		handler := newTestHandlerWithOpts(testHandlerOpts{
			withFolderCreator:  true,
			withDefaultsSeeder: true,
			templateSeeder:     ts,
		})
		ctx := contextWithClaims("alice@example.com")
		populateDefaults := true

		_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
			Name:             "seeded-org",
			DisplayName:      "Seeded Org",
			PopulateDefaults: &populateDefaults,
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Read the org namespace back and inspect the default-share-roles annotation.
		ns, err := handler.k8s.GetOrganization(context.Background(), "seeded-org")
		if err != nil {
			t.Fatalf("fetching org namespace: %v", err)
		}
		raw, ok := ns.Annotations[v1alpha2.AnnotationDefaultShareRoles]
		if !ok {
			t.Fatalf("expected annotation %q to be set", v1alpha2.AnnotationDefaultShareRoles)
		}
		var grants []secrets.AnnotationGrant
		if err := json.Unmarshal([]byte(raw), &grants); err != nil {
			t.Fatalf("parsing default-share-roles annotation: %v", err)
		}
		if len(grants) != 3 {
			t.Fatalf("expected 3 grants, got %d (%v)", len(grants), grants)
		}

		byRole := make(map[string]secrets.AnnotationGrant, 3)
		for _, g := range grants {
			byRole[g.Role] = g
		}
		for _, role := range []string{"owner", "editor", "viewer"} {
			g, ok := byRole[role]
			if !ok {
				t.Errorf("expected a grant for role %q, none found", role)
				continue
			}
			if g.Principal != role {
				t.Errorf("expected principal %q for role %q grant, got %q", role, role, g.Principal)
			}
			if g.Nbf != nil {
				t.Errorf("expected no start restriction (nbf) for role %q grant, got %v", role, *g.Nbf)
			}
			if g.Exp != nil {
				t.Errorf("expected no expiration (exp) for role %q grant, got %v", role, *g.Exp)
			}
		}

		// Also verify default-share-users was NOT set (spec: role grants only).
		if _, userDefaultsSet := ns.Annotations[v1alpha2.AnnotationDefaultShareUsers]; userDefaultsSet {
			t.Errorf("expected annotation %q to be unset, but it was present", v1alpha2.AnnotationDefaultShareUsers)
		}
	})

	t.Run("populate_defaults=false does not seed default role grants", func(t *testing.T) {
		ts := &mockTemplateSeeder{}
		handler := newTestHandlerWithOpts(testHandlerOpts{
			withFolderCreator:  true,
			withDefaultsSeeder: true,
			templateSeeder:     ts,
		})
		ctx := contextWithClaims("alice@example.com")
		populateDefaults := false

		_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
			Name:             "no-seed-defaults-org",
			DisplayName:      "No Seed Defaults Org",
			PopulateDefaults: &populateDefaults,
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		ns, err := handler.k8s.GetOrganization(context.Background(), "no-seed-defaults-org")
		if err != nil {
			t.Fatalf("fetching org namespace: %v", err)
		}
		if v, ok := ns.Annotations[v1alpha2.AnnotationDefaultShareRoles]; ok {
			t.Errorf("expected annotation %q to be unset when populate_defaults=false, got %q", v1alpha2.AnnotationDefaultShareRoles, v)
		}
	})

	t.Run("populate_defaults unset does not seed default role grants", func(t *testing.T) {
		ts := &mockTemplateSeeder{}
		handler := newTestHandlerWithOpts(testHandlerOpts{
			withFolderCreator:  true,
			withDefaultsSeeder: true,
			templateSeeder:     ts,
		})
		ctx := contextWithClaims("alice@example.com")

		_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
			Name:        "unset-defaults-org",
			DisplayName: "Unset Defaults Org",
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		ns, err := handler.k8s.GetOrganization(context.Background(), "unset-defaults-org")
		if err != nil {
			t.Fatalf("fetching org namespace: %v", err)
		}
		if v, ok := ns.Annotations[v1alpha2.AnnotationDefaultShareRoles]; ok {
			t.Errorf("expected annotation %q to be unset when populate_defaults is unset, got %q", v1alpha2.AnnotationDefaultShareRoles, v)
		}
	})

	t.Run("seeded default project inherits Owner/Editor/Viewer default role grants", func(t *testing.T) {
		// Locks in the ordering guarantee from plan #919: because the org
		// namespace's default-share-roles annotation is written *before*
		// the default project is created, the seeded default project's
		// own default-share-roles annotation must contain exactly the
		// three standard grants (Owner, Editor, Viewer) with empty start
		// and expiration, inherited via the project resolver's
		// ancestor-default merge.
		ts := &mockTemplateSeeder{}
		handler := newTestHandlerWithOpts(testHandlerOpts{
			withFolderCreator:  true,
			withDefaultsSeeder: true,
			templateSeeder:     ts,
		})
		ctx := contextWithClaims("alice@example.com")
		populateDefaults := true

		_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
			Name:             "project-inherits-org",
			DisplayName:      "Project Inherits Org",
			PopulateDefaults: &populateDefaults,
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		pc := handler.projectCreator.(*k8sProjectCreator)
		projectNsName := pc.resolver.ProjectNamespace(ts.seededProjectName)
		projectNs, err := pc.client.CoreV1().Namespaces().Get(context.Background(), projectNsName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected default project namespace %q to exist, got %v", projectNsName, err)
		}

		raw, ok := projectNs.Annotations[v1alpha2.AnnotationDefaultShareRoles]
		if !ok {
			t.Fatalf("expected annotation %q on seeded default project namespace", v1alpha2.AnnotationDefaultShareRoles)
		}
		var grants []secrets.AnnotationGrant
		if err := json.Unmarshal([]byte(raw), &grants); err != nil {
			t.Fatalf("parsing default-share-roles annotation on project: %v", err)
		}
		if len(grants) != 3 {
			t.Fatalf("expected 3 default role grants on seeded project, got %d (%v)", len(grants), grants)
		}

		byRole := make(map[string]secrets.AnnotationGrant, 3)
		for _, g := range grants {
			byRole[g.Role] = g
		}
		for _, role := range []string{"owner", "editor", "viewer"} {
			g, ok := byRole[role]
			if !ok {
				t.Errorf("expected a default grant for role %q on project namespace, none found", role)
				continue
			}
			if g.Principal != role {
				t.Errorf("expected principal %q for project default role %q grant, got %q", role, role, g.Principal)
			}
			if g.Nbf != nil {
				t.Errorf("expected no start restriction (nbf) for project default role %q grant, got %v", role, *g.Nbf)
			}
			if g.Exp != nil {
				t.Errorf("expected no expiration (exp) for project default role %q grant, got %v", role, *g.Exp)
			}
		}
	})

	t.Run("default role grants are written before default folder creation", func(t *testing.T) {
		// Use a FolderCreator that records whether the
		// default-share-roles annotation was present on the org namespace
		// at the moment CreateFolder was invoked. This verifies the
		// ordering guarantee (annotation-before-folder) described in the
		// spec: "annotations must be visible on the org namespace
		// *before* any folder/project creation call runs".
		ts := &mockTemplateSeeder{}
		handler := newTestHandlerWithOpts(testHandlerOpts{
			withFolderCreator:  true,
			withDefaultsSeeder: true,
			templateSeeder:     ts,
		})
		fc := handler.folderCreator.(*k8sFolderCreator)

		orderingProbe := &orderingFolderCreator{inner: fc, k8s: handler.k8s}
		// Swap in the probe as the FolderCreator (leaving FolderLister intact).
		folderPrefix := handler.folderPrefix
		handler.WithFolderCreator(orderingProbe, fc, folderPrefix)

		ctx := contextWithClaims("alice@example.com")
		populateDefaults := true

		_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
			Name:             "ordering-org",
			DisplayName:      "Ordering Org",
			PopulateDefaults: &populateDefaults,
		}))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if !orderingProbe.seenDefaultRolesAnnotation {
			t.Errorf("expected default-share-roles annotation to be visible on the org namespace before CreateFolder was called")
		}
	})
}

// orderingFolderCreator wraps a FolderCreator and records, at the moment
// CreateFolder is invoked, whether the org namespace already carries the
// default-share-roles annotation. Used to assert the ordering invariant
// from issue #920.
type orderingFolderCreator struct {
	inner                      FolderCreator
	k8s                        *K8sClient
	seenDefaultRolesAnnotation bool
}

func (o *orderingFolderCreator) CreateFolder(ctx context.Context, name, displayName, description, org, parentNs, creatorEmail, creatorSubject string, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	ns, err := o.k8s.GetOrganization(ctx, org)
	if err == nil && ns.Annotations != nil {
		if _, ok := ns.Annotations[v1alpha2.AnnotationDefaultShareRoles]; ok {
			o.seenDefaultRolesAnnotation = true
		}
	}
	return o.inner.CreateFolder(ctx, name, displayName, description, org, parentNs, creatorEmail, creatorSubject, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles)
}

func (o *orderingFolderCreator) DeleteFolder(ctx context.Context, name string) error {
	return o.inner.DeleteFolder(ctx, name)
}

func (o *orderingFolderCreator) NamespaceExists(ctx context.Context, nsName string) (bool, error) {
	return o.inner.NamespaceExists(ctx, nsName)
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
