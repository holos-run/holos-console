package projects

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// testLogHandler captures log records for testing.
type testLogHandler struct {
	records []slog.Record
}

func (h *testLogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *testLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *testLogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *testLogHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *testLogHandler) findRecord(action string) *slog.Record {
	for _, r := range h.records {
		var foundAction string
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "action" {
				foundAction = a.Value.String()
				return false
			}
			return true
		})
		if foundAction == action {
			return &r
		}
	}
	return nil
}

// contextWithClaims creates a context with OIDC claims.
func contextWithClaims(email string, groups ...string) context.Context {
	claims := &rpc.Claims{
		Sub:           "sub-" + email,
		Email:         email,
		EmailVerified: true,
		Name:          email,
		Groups:        groups,
	}
	return rpc.ContextWithClaims(context.Background(), claims)
}

// managedNS creates a managed project namespace with share-users annotation.
func managedNS(name string, shareUsersJSON string) *corev1.Namespace {
	annotations := map[string]string{}
	if shareUsersJSON != "" {
		annotations[secrets.ShareUsersAnnotation] = shareUsersJSON
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prj-" + name,
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      name,
			},
			Annotations: annotations,
		},
	}
}

func testResolver() *resolver.Resolver {
	return &resolver.Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
}

func newHandler(namespaces ...*corev1.Namespace) (*Handler, *testLogHandler) {
	objs := make([]runtime.Object, len(namespaces))
	for i, ns := range namespaces {
		objs[i] = ns
	}
	fakeClient := fake.NewClientset(objs...)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, nil)
	logHandler := &testLogHandler{}
	slog.SetDefault(slog.New(logHandler))
	return handler, logHandler
}

// ---- ListProjects tests ----

func TestListProjects_ReturnsProjectsFilteredByAccess(t *testing.T) {
	ns1 := managedNS("project-a", `[{"principal":"alice@example.com","role":"editor"}]`)
	ns2 := managedNS("project-b", `[{"principal":"alice@example.com","role":"viewer"}]`)
	ns3 := managedNS("project-c", `[{"principal":"bob@example.com","role":"owner"}]`)

	handler, logHandler := newHandler(ns1, ns2, ns3)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.ListProjects(ctx, connect.NewRequest(&consolev1.ListProjectsRequest{}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(resp.Msg.Projects))
	}

	if r := logHandler.findRecord("project_list"); r == nil {
		t.Error("expected project_list audit log")
	}
}

func TestListProjects_IncludesUserRolePerProject(t *testing.T) {
	ns1 := managedNS("project-a", `[{"principal":"alice@example.com","role":"editor"}]`)
	ns2 := managedNS("project-b", `[{"principal":"alice@example.com","role":"viewer"}]`)

	handler, _ := newHandler(ns1, ns2)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.ListProjects(ctx, connect.NewRequest(&consolev1.ListProjectsRequest{}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(resp.Msg.Projects))
	}

	rolesByName := make(map[string]consolev1.Role)
	for _, p := range resp.Msg.Projects {
		rolesByName[p.Name] = p.UserRole
	}
	if rolesByName["project-a"] != consolev1.Role_ROLE_EDITOR {
		t.Errorf("expected ROLE_EDITOR for project-a, got %v", rolesByName["project-a"])
	}
	if rolesByName["project-b"] != consolev1.Role_ROLE_VIEWER {
		t.Errorf("expected ROLE_VIEWER for project-b, got %v", rolesByName["project-b"])
	}
}

func TestListProjects_ReturnsEmptyListForUserWithNoAccess(t *testing.T) {
	ns := managedNS("project-a", `[{"principal":"bob@example.com","role":"owner"}]`)
	handler, _ := newHandler(ns)
	ctx := contextWithClaims("nobody@example.com")

	resp, err := handler.ListProjects(ctx, connect.NewRequest(&consolev1.ListProjectsRequest{}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(resp.Msg.Projects))
	}
}

func TestListProjects_ReturnsUnauthenticatedWithoutClaims(t *testing.T) {
	handler, _ := newHandler()
	_, err := handler.ListProjects(context.Background(), connect.NewRequest(&consolev1.ListProjectsRequest{}))
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

// ---- GetProject tests ----

func TestGetProject_ReturnsProjectForAuthorizedUser(t *testing.T) {
	ns := managedNS("my-project", `[{"principal":"alice@example.com","role":"viewer"}]`)
	ns.Annotations[DisplayNameAnnotation] = "My Project"
	ns.Annotations[secrets.DescriptionAnnotation] = "A test project"

	handler, logHandler := newHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.GetProject(ctx, connect.NewRequest(&consolev1.GetProjectRequest{Name: "my-project"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	p := resp.Msg.Project
	if p.Name != "my-project" {
		t.Errorf("expected name 'my-project', got %q", p.Name)
	}
	if p.DisplayName != "My Project" {
		t.Errorf("expected display_name 'My Project', got %q", p.DisplayName)
	}
	if p.Description != "A test project" {
		t.Errorf("expected description 'A test project', got %q", p.Description)
	}
	if p.UserRole != consolev1.Role_ROLE_VIEWER {
		t.Errorf("expected ROLE_VIEWER, got %v", p.UserRole)
	}
	if len(p.UserGrants) != 1 {
		t.Errorf("expected 1 user grant, got %d", len(p.UserGrants))
	}

	if r := logHandler.findRecord("project_read"); r == nil {
		t.Error("expected project_read audit log")
	}
}

func TestGetProject_DeniesUnauthorizedUser(t *testing.T) {
	ns := managedNS("my-project", `[{"principal":"bob@example.com","role":"owner"}]`)
	handler, logHandler := newHandler(ns)
	ctx := contextWithClaims("nobody@example.com")

	_, err := handler.GetProject(ctx, connect.NewRequest(&consolev1.GetProjectRequest{Name: "my-project"}))
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

	if r := logHandler.findRecord("project_read_denied"); r == nil {
		t.Error("expected project_read_denied audit log")
	}
}

func TestGetProject_RequiresProjectName(t *testing.T) {
	handler, _ := newHandler()
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.GetProject(ctx, connect.NewRequest(&consolev1.GetProjectRequest{Name: ""}))
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

func TestGetProject_ReturnsNotFoundForMissing(t *testing.T) {
	handler, _ := newHandler()
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.GetProject(ctx, connect.NewRequest(&consolev1.GetProjectRequest{Name: "missing"}))
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

func TestGetProject_ReturnsUnauthenticatedWithoutClaims(t *testing.T) {
	handler, _ := newHandler()
	_, err := handler.GetProject(context.Background(), connect.NewRequest(&consolev1.GetProjectRequest{Name: "test"}))
	assertUnauthenticated(t, err)
}

// ---- CreateProject tests ----

func TestCreateProject_CreatesForAuthorizedUser(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler, logHandler := newHandler(existing)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:        "new-project",
		DisplayName: "New Project",
		Description: "A new project",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "new-project" {
		t.Errorf("expected name 'new-project', got %q", resp.Msg.Name)
	}

	if r := logHandler.findRecord("project_create"); r == nil {
		t.Error("expected project_create audit log")
	}
}

func TestCreateProject_DeniesUserWithoutCreatePermission(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler, logHandler := newHandler(existing)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name: "new-project",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertPermissionDenied(t, err)

	if r := logHandler.findRecord("project_create_denied"); r == nil {
		t.Error("expected project_create_denied audit log")
	}
}

func TestCreateProject_AutoGrantsOwnerToCreator(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, nil)
	logHandler := &testLogHandler{}
	slog.SetDefault(slog.New(logHandler))

	ctx := contextWithClaims("alice@example.com")

	// Create without explicit grants
	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name: "new-project",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify the created namespace has alice as owner
	ns, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "prj-new-project", metav1.GetOptions{})
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
		t.Errorf("expected creator alice@example.com as owner in share-users, got %v", users)
	}
}

func TestCreateProject_RequiresProjectName(t *testing.T) {
	handler, _ := newHandler()
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{Name: ""}))
	assertInvalidArgument(t, err)
}

func TestCreateProject_ReturnsUnauthenticatedWithoutClaims(t *testing.T) {
	handler, _ := newHandler()
	_, err := handler.CreateProject(context.Background(), connect.NewRequest(&consolev1.CreateProjectRequest{Name: "test"}))
	assertUnauthenticated(t, err)
}

// ---- UpdateProject tests ----

func TestUpdateProject_UpdatesMetadataForEditor(t *testing.T) {
	ns := managedNS("my-project", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler, logHandler := newHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	displayName := "Updated Name"
	desc := "Updated description"
	_, err := handler.UpdateProject(ctx, connect.NewRequest(&consolev1.UpdateProjectRequest{
		Name:        "my-project",
		DisplayName: &displayName,
		Description: &desc,
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if r := logHandler.findRecord("project_update"); r == nil {
		t.Error("expected project_update audit log")
	}
}

func TestUpdateProject_DeniesViewer(t *testing.T) {
	ns := managedNS("my-project", `[{"principal":"alice@example.com","role":"viewer"}]`)
	handler, logHandler := newHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	displayName := "Updated"
	_, err := handler.UpdateProject(ctx, connect.NewRequest(&consolev1.UpdateProjectRequest{
		Name:        "my-project",
		DisplayName: &displayName,
	}))
	assertPermissionDenied(t, err)

	if r := logHandler.findRecord("project_update_denied"); r == nil {
		t.Error("expected project_update_denied audit log")
	}
}

func TestUpdateProject_ReturnsUnauthenticatedWithoutClaims(t *testing.T) {
	handler, _ := newHandler()
	_, err := handler.UpdateProject(context.Background(), connect.NewRequest(&consolev1.UpdateProjectRequest{Name: "test"}))
	assertUnauthenticated(t, err)
}

// ---- DeleteProject tests ----

func TestDeleteProject_DeletesForOwner(t *testing.T) {
	ns := managedNS("my-project", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler, logHandler := newHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteProject(ctx, connect.NewRequest(&consolev1.DeleteProjectRequest{Name: "my-project"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if r := logHandler.findRecord("project_delete"); r == nil {
		t.Error("expected project_delete audit log")
	}
}

func TestDeleteProject_DeniesEditor(t *testing.T) {
	ns := managedNS("my-project", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler, logHandler := newHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteProject(ctx, connect.NewRequest(&consolev1.DeleteProjectRequest{Name: "my-project"}))
	assertPermissionDenied(t, err)

	if r := logHandler.findRecord("project_delete_denied"); r == nil {
		t.Error("expected project_delete_denied audit log")
	}
}

func TestDeleteProject_ReturnsUnauthenticatedWithoutClaims(t *testing.T) {
	handler, _ := newHandler()
	_, err := handler.DeleteProject(context.Background(), connect.NewRequest(&consolev1.DeleteProjectRequest{Name: "test"}))
	assertUnauthenticated(t, err)
}

// ---- UpdateProjectSharing tests ----

func TestUpdateProjectSharing_UpdatesGrantsForOwner(t *testing.T) {
	ns := managedNS("my-project", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler, logHandler := newHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.UpdateProjectSharing(ctx, connect.NewRequest(&consolev1.UpdateProjectSharingRequest{
		Name: "my-project",
		UserGrants: []*consolev1.ShareGrant{
			{Principal: "alice@example.com", Role: consolev1.Role_ROLE_OWNER},
			{Principal: "bob@example.com", Role: consolev1.Role_ROLE_EDITOR},
		},
		GroupGrants: []*consolev1.ShareGrant{
			{Principal: "engineering", Role: consolev1.Role_ROLE_VIEWER},
		},
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Project.UserGrants) != 2 {
		t.Errorf("expected 2 user grants, got %d", len(resp.Msg.Project.UserGrants))
	}
	if len(resp.Msg.Project.GroupGrants) != 1 {
		t.Errorf("expected 1 group grant, got %d", len(resp.Msg.Project.GroupGrants))
	}

	if r := logHandler.findRecord("project_sharing_update"); r == nil {
		t.Error("expected project_sharing_update audit log")
	}
}

func TestUpdateProjectSharing_DeniesNonOwner(t *testing.T) {
	ns := managedNS("my-project", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler, logHandler := newHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.UpdateProjectSharing(ctx, connect.NewRequest(&consolev1.UpdateProjectSharingRequest{
		Name: "my-project",
		UserGrants: []*consolev1.ShareGrant{
			{Principal: "alice@example.com", Role: consolev1.Role_ROLE_OWNER},
		},
	}))
	assertPermissionDenied(t, err)

	if r := logHandler.findRecord("project_sharing_denied"); r == nil {
		t.Error("expected project_sharing_denied audit log")
	}
}

func TestUpdateProjectSharing_ReturnsUnauthenticatedWithoutClaims(t *testing.T) {
	handler, _ := newHandler()
	_, err := handler.UpdateProjectSharing(context.Background(), connect.NewRequest(&consolev1.UpdateProjectSharingRequest{Name: "test"}))
	assertUnauthenticated(t, err)
}

// ---- Namespace prefix tests ----

func TestCreateProject_NamespacePrefixIncluded(t *testing.T) {
	r := &resolver.Resolver{NamespacePrefix: "prod-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	// Need an existing project with owner grant for create permission
	existing := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prod-prj-existing",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "existing",
			},
			Annotations: map[string]string{
				secrets.ShareUsersAnnotation: `[{"principal":"alice@example.com","role":"owner"}]`,
			},
		},
	}
	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, r)
	handler := NewHandler(k8s, nil)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := contextWithClaims("alice@example.com")
	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name: "new-project",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify the namespace name includes the namespace prefix
	ns, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "prod-prj-new-project", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected namespace prod-prj-new-project to exist, got %v", err)
	}
	if ns.Name != "prod-prj-new-project" {
		t.Errorf("expected namespace name 'prod-prj-new-project', got %q", ns.Name)
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

// ---- GetProjectRaw tests ----

func TestGetProjectRaw_ReturnsNamespaceJSON(t *testing.T) {
	ns := managedNS("my-project", `[{"principal":"alice@example.com","role":"viewer"}]`)
	ns.Annotations[DisplayNameAnnotation] = "My Project"
	handler, _ := newHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.GetProjectRaw(ctx, connect.NewRequest(&consolev1.GetProjectRawRequest{Name: "my-project"}))
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
	if metadata["name"] != "prj-my-project" {
		t.Errorf("expected metadata.name 'prj-my-project', got %v", metadata["name"])
	}
	labels := metadata["labels"].(map[string]interface{})
	if labels[secrets.ManagedByLabel] != secrets.ManagedByValue {
		t.Errorf("expected managed-by label, got %v", labels[secrets.ManagedByLabel])
	}
	if labels[resolver.ResourceTypeLabel] != resolver.ResourceTypeProject {
		t.Errorf("expected resource-type label, got %v", labels[resolver.ResourceTypeLabel])
	}
}

func TestGetProjectRaw_DeniesUnauthorized(t *testing.T) {
	ns := managedNS("my-project", `[{"principal":"bob@example.com","role":"owner"}]`)
	handler, _ := newHandler(ns)
	ctx := contextWithClaims("nobody@example.com")

	_, err := handler.GetProjectRaw(ctx, connect.NewRequest(&consolev1.GetProjectRawRequest{Name: "my-project"}))
	assertPermissionDenied(t, err)
}

// ---- Cascade permission tests (org grant fallback) ----

// mockOrgResolver implements OrgResolver for testing.
type mockOrgResolver struct {
	users  map[string]string
	groups map[string]string
}

func (m *mockOrgResolver) GetOrgGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	return m.users, m.groups, nil
}

func newHandlerWithOrg(orgResolver OrgResolver, namespaces ...*corev1.Namespace) *Handler {
	objs := make([]runtime.Object, len(namespaces))
	for i, ns := range namespaces {
		objs[i] = ns
	}
	fakeClient := fake.NewClientset(objs...)
	k8s := NewK8sClient(fakeClient, testResolver())
	return NewHandler(k8s, orgResolver)
}

// managedNSWithOrg creates a managed project namespace associated with an org.
func managedNSWithOrg(name, org, shareUsersJSON string) *corev1.Namespace {
	ns := managedNS(name, shareUsersJSON)
	ns.Labels[resolver.OrganizationLabel] = org
	return ns
}

func TestGetProject_OrgViewerCannotReadProject(t *testing.T) {
	// Org viewer has no per-project grant — should be denied GetProject
	ns := managedNSWithOrg("my-project", "acme", "")
	orgResolver := &mockOrgResolver{
		users: map[string]string{"alice@example.com": "viewer"},
	}
	handler := newHandlerWithOrg(orgResolver, ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.GetProject(ctx, connect.NewRequest(&consolev1.GetProjectRequest{Name: "my-project"}))
	assertPermissionDenied(t, err)
}

func TestListProjects_OrgViewerCannotListProjects(t *testing.T) {
	// Org viewer has no per-project grant — should not see any projects
	ns := managedNSWithOrg("my-project", "acme", "")
	orgResolver := &mockOrgResolver{
		users: map[string]string{"alice@example.com": "viewer"},
	}
	handler := newHandlerWithOrg(orgResolver, ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.ListProjects(ctx, connect.NewRequest(&consolev1.ListProjectsRequest{}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Projects) != 0 {
		t.Errorf("expected 0 projects for org viewer, got %d", len(resp.Msg.Projects))
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
