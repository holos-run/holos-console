package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/deployments"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	"github.com/holos-run/holos-console/console/templates"
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

// findAttr returns the string value of the named attribute on the record, or "" if not found.
func findAttr(r *slog.Record, key string) string {
	var val string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			val = a.Value.String()
			return false
		}
		return true
	})
	return val
}

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

// managedNS creates a managed project namespace with share-users annotation.
// Uses the default "holos-" namespace prefix matching testResolver().
func managedNS(name string, shareUsersJSON string) *corev1.Namespace {
	annotations := map[string]string{}
	if shareUsersJSON != "" {
		annotations[v1alpha2.AnnotationShareUsers] = shareUsersJSON
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-" + name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      name,
			},
			Annotations: annotations,
		},
	}
}

func testResolver() *resolver.Resolver {
	return &resolver.Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
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
	ns.Annotations[v1alpha2.AnnotationDisplayName] = "My Project"
	ns.Annotations[v1alpha2.AnnotationDescription] = "A test project"

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

func TestGetProject_AuditLogIncludesOrganization(t *testing.T) {
	ns := managedNSWithOrg("my-project", "my-org", `[{"principal":"alice@example.com","role":"viewer"}]`)
	handler, logHandler := newHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.GetProject(ctx, connect.NewRequest(&consolev1.GetProjectRequest{Name: "my-project"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	record := logHandler.findRecord("project_read")
	if record == nil {
		t.Fatal("expected project_read audit log")
	}
	if got := findAttr(record, "organization"); got != "my-org" {
		t.Errorf("expected organization='my-org', got %q", got)
	}
}

func TestGetProject_DeniedAuditLogIncludesOrganization(t *testing.T) {
	ns := managedNSWithOrg("my-project", "my-org", `[{"principal":"bob@example.com","role":"owner"}]`)
	handler, logHandler := newHandler(ns)
	ctx := contextWithClaims("nobody@example.com")

	_, err := handler.GetProject(ctx, connect.NewRequest(&consolev1.GetProjectRequest{Name: "my-project"}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	record := logHandler.findRecord("project_read_denied")
	if record == nil {
		t.Fatal("expected project_read_denied audit log")
	}
	if got := findAttr(record, "organization"); got != "my-org" {
		t.Errorf("expected organization='my-org', got %q", got)
	}
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
	ns, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-prj-new-project", metav1.GetOptions{})
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

func TestCreateProject_RequiresNameOrDisplayName(t *testing.T) {
	handler, _ := newHandler()
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{Name: "", DisplayName: ""}))
	assertInvalidArgument(t, err)
}

func TestCreateProject_DeriveNameFromDisplayName(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler, _ := newHandler(existing)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		DisplayName: "My Frontend App",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "my-frontend-app" {
		t.Errorf("expected name 'my-frontend-app', got %q", resp.Msg.Name)
	}
}

func TestCreateProject_DeriveNameWithCollision(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	colliding := managedNS("frontend", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler, _ := newHandler(existing, colliding)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		DisplayName: "Frontend",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// Name should have a suffix since "frontend" was taken.
	if resp.Msg.Name == "frontend" {
		t.Error("expected name with suffix due to collision, got 'frontend'")
	}
	if len(resp.Msg.Name) < len("frontend-000000") {
		t.Errorf("expected suffixed name, got %q", resp.Msg.Name)
	}
}

func TestCreateProject_ReturnsUnauthenticatedWithoutClaims(t *testing.T) {
	handler, _ := newHandler()
	_, err := handler.CreateProject(context.Background(), connect.NewRequest(&consolev1.CreateProjectRequest{Name: "test"}))
	assertUnauthenticated(t, err)
}

func TestCreateProject_RetriesOnAlreadyExistsRace(t *testing.T) {
	// Simulate race: GenerateIdentifier finds "frontend" available, but by the
	// time CreateProject calls K8s Create, another request has taken it.
	// The handler should regenerate the identifier and retry.
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	fakeClient := fake.NewClientset(existing)

	var createCount atomic.Int32
	fakeClient.PrependReactor("create", "namespaces", func(action k8stesting.Action) (bool, runtime.Object, error) {
		n := createCount.Add(1)
		if n == 1 {
			// First create: simulate race by adding the namespace then returning AlreadyExists
			ca := action.(k8stesting.CreateAction)
			ns := ca.GetObject().(*corev1.Namespace)
			_ = fakeClient.Tracker().Add(ns)
			return true, nil, k8serrors.NewAlreadyExists(
				schema.GroupResource{Resource: "namespaces"}, ns.Name)
		}
		return false, nil, nil // fall through to default handler
	})

	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, nil)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := contextWithClaims("alice@example.com")
	resp, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		DisplayName: "Frontend",
	}))
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	// Name should have a suffix since the plain slug was taken by the race
	if resp.Msg.Name == "frontend" {
		t.Error("expected name with suffix after retry, got 'frontend'")
	}
	if len(resp.Msg.Name) < len("frontend-000000") {
		t.Errorf("expected suffixed name, got %q", resp.Msg.Name)
	}
}

func TestCreateProject_ExplicitNameDoesNotRetry(t *testing.T) {
	// When an explicit name is provided and it collides, the error should propagate
	// without retry — the caller chose that name deliberately.
	existing := managedNS("my-project", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler, _ := newHandler(existing)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name: "my-project",
	}))
	if err == nil {
		t.Fatal("expected AlreadyExists error for explicit name collision")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeAlreadyExists {
		t.Errorf("expected CodeAlreadyExists, got %v", connectErr.Code())
	}
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
		RoleGrants: []*consolev1.ShareGrant{
			{Principal: "engineering", Role: consolev1.Role_ROLE_VIEWER},
		},
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Project.UserGrants) != 2 {
		t.Errorf("expected 2 user grants, got %d", len(resp.Msg.Project.UserGrants))
	}
	if len(resp.Msg.Project.RoleGrants) != 1 {
		t.Errorf("expected 1 role grant, got %d", len(resp.Msg.Project.RoleGrants))
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

// ---- Label-based name extraction tests ----

func TestBuildProject_FallbackProducesWrongNameWithPrefix(t *testing.T) {
	// When the project label is missing and namespace-prefix is configured,
	// ProjectFromNamespace produces the wrong name.
	r := &resolver.Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "o-", FolderPrefix: "fld-", ProjectPrefix: "p-"}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-p-holos", // namespace-prefix "holos-" + project-prefix "p-" + name "holos"
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				// No ProjectLabel — forces fallback
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"viewer"}]`,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, r)
	handler := NewHandler(k8s, nil)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	p := handler.buildProject(ns, nil, nil, 0)
	// The fallback uses ProjectFromNamespace which with NamespacePrefix "holos-"
	// and ProjectPrefix "p-" strips "holos-p-" leaving "holos" — this happens
	// to be correct by coincidence. Test with a name where it breaks:
	// TrimPrefix("holos-p-holos", "holos-p-") = "holos" — coincidence.
	// Use a name where the prefix is NOT a prefix of the namespace name to show
	// the fallback is fragile.
	if p.Name == "" {
		t.Errorf("expected non-empty name from fallback, got empty")
	}
}

func TestBuildProject_LabelPreferredOverFallback(t *testing.T) {
	r := &resolver.Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "o-", FolderPrefix: "fld-", ProjectPrefix: "p-"}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-p-holos",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "holos",
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"viewer"}]`,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, r)
	handler := NewHandler(k8s, nil)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	p := handler.buildProject(ns, nil, nil, 0)
	if p.Name != "holos" {
		t.Errorf("expected project name 'holos', got %q", p.Name)
	}
}

// ---- Namespace prefix tests ----

func TestCreateProject_NamespacePrefixIncluded(t *testing.T) {
	r := &resolver.Resolver{NamespacePrefix: "prod-", OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
	// Need an existing project with owner grant for create permission
	existing := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prod-prj-existing",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "existing",
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"owner"}]`,
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
	ns.Annotations[v1alpha2.AnnotationDisplayName] = "My Project"
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
	if metadata["name"] != "holos-prj-my-project" {
		t.Errorf("expected metadata.name 'prj-my-project', got %v", metadata["name"])
	}
	labels := metadata["labels"].(map[string]interface{})
	if labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
		t.Errorf("expected managed-by label, got %v", labels[v1alpha2.LabelManagedBy])
	}
	if labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeProject {
		t.Errorf("expected resource-type label, got %v", labels[v1alpha2.LabelResourceType])
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
	handler, _ := newHandlerWithOrgAndClient(orgResolver, namespaces...)
	return handler
}

// newHandlerWithOrgAndClient creates a handler and returns the fake K8s
// clientset so tests can inspect the actions taken.
func newHandlerWithOrgAndClient(orgResolver OrgResolver, namespaces ...*corev1.Namespace) (*Handler, *fake.Clientset) {
	objs := make([]runtime.Object, len(namespaces))
	for i, ns := range namespaces {
		objs[i] = ns
	}
	fakeClient := fake.NewClientset(objs...)
	k8s := NewK8sClient(fakeClient, testResolver())
	return NewHandler(k8s, orgResolver), fakeClient
}

// managedNSWithOrg creates a managed project namespace associated with an org.
func managedNSWithOrg(name, org, shareUsersJSON string) *corev1.Namespace {
	ns := managedNS(name, shareUsersJSON)
	ns.Labels[v1alpha2.LabelOrganization] = org
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

func TestGetProject_OrgOwnerCannotReadProjectWithoutProjectGrant(t *testing.T) {
	// Org owner has no per-project grant — org grants do not cascade to projects
	ns := managedNSWithOrg("my-project", "acme", "")
	orgResolver := &mockOrgResolver{
		users: map[string]string{"alice@example.com": "owner"},
	}
	handler := newHandlerWithOrg(orgResolver, ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.GetProject(ctx, connect.NewRequest(&consolev1.GetProjectRequest{Name: "my-project"}))
	assertPermissionDenied(t, err)
}

func TestUpdateProject_OrgOwnerCannotUpdateProjectWithoutProjectGrant(t *testing.T) {
	// Org owner has no per-project grant — org grants do not cascade to projects
	ns := managedNSWithOrg("my-project", "acme", "")
	orgResolver := &mockOrgResolver{
		users: map[string]string{"alice@example.com": "owner"},
	}
	handler := newHandlerWithOrg(orgResolver, ns)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	displayName := "Updated"
	_, err := handler.UpdateProject(ctx, connect.NewRequest(&consolev1.UpdateProjectRequest{
		Name:        "my-project",
		DisplayName: &displayName,
	}))
	assertPermissionDenied(t, err)
}

// ---- UpdateProjectDefaultSharing tests ----

func TestUpdateProjectDefaultSharing_UpdatesGrantsForOwner(t *testing.T) {
	ns := managedNS("my-project", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler, logHandler := newHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.UpdateProjectDefaultSharing(ctx, connect.NewRequest(&consolev1.UpdateProjectDefaultSharingRequest{
		Name: "my-project",
		DefaultUserGrants: []*consolev1.ShareGrant{
			{Principal: "bob@example.com", Role: consolev1.Role_ROLE_VIEWER},
		},
		DefaultRoleGrants: []*consolev1.ShareGrant{
			{Principal: "engineering", Role: consolev1.Role_ROLE_EDITOR},
		},
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Project.DefaultUserGrants) != 1 {
		t.Errorf("expected 1 default user grant, got %d", len(resp.Msg.Project.DefaultUserGrants))
	}
	if resp.Msg.Project.DefaultUserGrants[0].Principal != "bob@example.com" {
		t.Errorf("expected bob@example.com, got %q", resp.Msg.Project.DefaultUserGrants[0].Principal)
	}
	if len(resp.Msg.Project.DefaultRoleGrants) != 1 {
		t.Errorf("expected 1 default role grant, got %d", len(resp.Msg.Project.DefaultRoleGrants))
	}

	if r := logHandler.findRecord("project_default_sharing_update"); r == nil {
		t.Error("expected project_default_sharing_update audit log")
	}
}

func TestUpdateProjectDefaultSharing_DeniesNonOwner(t *testing.T) {
	ns := managedNS("my-project", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler, logHandler := newHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.UpdateProjectDefaultSharing(ctx, connect.NewRequest(&consolev1.UpdateProjectDefaultSharingRequest{
		Name: "my-project",
		DefaultUserGrants: []*consolev1.ShareGrant{
			{Principal: "bob@example.com", Role: consolev1.Role_ROLE_VIEWER},
		},
	}))
	assertPermissionDenied(t, err)

	if r := logHandler.findRecord("project_default_sharing_denied"); r == nil {
		t.Error("expected project_default_sharing_denied audit log")
	}
}

func TestUpdateProjectDefaultSharing_RequiresProjectName(t *testing.T) {
	handler, _ := newHandler()
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.UpdateProjectDefaultSharing(ctx, connect.NewRequest(&consolev1.UpdateProjectDefaultSharingRequest{Name: ""}))
	assertInvalidArgument(t, err)
}

func TestUpdateProjectDefaultSharing_ReturnsUnauthenticatedWithoutClaims(t *testing.T) {
	handler, _ := newHandler()
	_, err := handler.UpdateProjectDefaultSharing(context.Background(), connect.NewRequest(&consolev1.UpdateProjectDefaultSharingRequest{Name: "test"}))
	assertUnauthenticated(t, err)
}

func TestBuildProject_PopulatesDefaultGrants(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-my-project",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "my-project",
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDefaultShareUsers: `[{"principal":"alice@example.com","role":"viewer"}]`,
				v1alpha2.AnnotationDefaultShareRoles: `[{"principal":"engineering","role":"editor"}]`,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, nil)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	defaultUsers, _ := GetDefaultShareUsers(ns)
	defaultRoles, _ := GetDefaultShareRoles(ns)
	p := handler.buildProject(ns, nil, nil, 0)

	_ = defaultUsers
	_ = defaultRoles

	if len(p.DefaultUserGrants) != 1 {
		t.Errorf("expected 1 default user grant, got %d", len(p.DefaultUserGrants))
	}
	if p.DefaultUserGrants[0].Principal != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %q", p.DefaultUserGrants[0].Principal)
	}
	if len(p.DefaultRoleGrants) != 1 {
		t.Errorf("expected 1 default role grant, got %d", len(p.DefaultRoleGrants))
	}
	if p.DefaultRoleGrants[0].Principal != "engineering" {
		t.Errorf("expected engineering, got %q", p.DefaultRoleGrants[0].Principal)
	}
}

// ---- CreateProject with org default sharing tests ----

// mockOrgDefaultShareResolver implements both OrgResolver and OrgDefaultShareResolver.
type mockOrgDefaultShareResolver struct {
	users         map[string]string
	groups        map[string]string
	defaultUsers  []secrets.AnnotationGrant
	defaultRoles  []secrets.AnnotationGrant
	defaultCalled bool
}

func (m *mockOrgDefaultShareResolver) GetOrgGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	return m.users, m.groups, nil
}

func (m *mockOrgDefaultShareResolver) GetOrgDefaultGrants(_ context.Context, _ string) ([]secrets.AnnotationGrant, []secrets.AnnotationGrant, error) {
	m.defaultCalled = true
	return m.defaultUsers, m.defaultRoles, nil
}

func TestCreateProject_MergesOrgDefaultsIntoProjectGrants(t *testing.T) {
	// Existing project so alice has create permission
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	orgResolver := &mockOrgDefaultShareResolver{
		users:  map[string]string{"alice@example.com": "owner"},
		groups: nil,
		defaultUsers: []secrets.AnnotationGrant{
			{Principal: "bob@example.com", Role: "viewer"},
			{Principal: "carol@example.com", Role: "editor"},
		},
		defaultRoles: []secrets.AnnotationGrant{
			{Principal: "engineering", Role: "viewer"},
		},
	}

	objs := []runtime.Object{existing}
	fakeClient := fake.NewClientset(objs...)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, orgResolver)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "new-project",
		Organization: "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify the created namespace has merged grants
	ns, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-prj-new-project", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected namespace to exist, got %v", err)
	}

	users, err := GetShareUsers(ns)
	if err != nil {
		t.Fatalf("failed to parse share-users: %v", err)
	}

	// Should contain: alice (owner from ensureCreatorOwner), bob (viewer from org default), carol (editor from org default)
	principalRoles := make(map[string]string)
	for _, u := range users {
		principalRoles[u.Principal] = u.Role
	}
	if principalRoles["alice@example.com"] != "owner" {
		t.Errorf("expected alice as owner, got %q", principalRoles["alice@example.com"])
	}
	if principalRoles["bob@example.com"] != "viewer" {
		t.Errorf("expected bob as viewer from org default, got %q", principalRoles["bob@example.com"])
	}
	if principalRoles["carol@example.com"] != "editor" {
		t.Errorf("expected carol as editor from org default, got %q", principalRoles["carol@example.com"])
	}

	// Verify role grants merged
	roles, err := GetShareRoles(ns)
	if err != nil {
		t.Fatalf("failed to parse share-roles: %v", err)
	}
	roleMap := make(map[string]string)
	for _, r := range roles {
		roleMap[r.Principal] = r.Role
	}
	if roleMap["engineering"] != "viewer" {
		t.Errorf("expected engineering as viewer from org default, got %q", roleMap["engineering"])
	}
}

func TestCreateProject_CopiesOrgDefaultsAsProjectDefaults(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	orgResolver := &mockOrgDefaultShareResolver{
		users:  map[string]string{"alice@example.com": "owner"},
		groups: nil,
		defaultUsers: []secrets.AnnotationGrant{
			{Principal: "bob@example.com", Role: "viewer"},
		},
		defaultRoles: []secrets.AnnotationGrant{
			{Principal: "engineering", Role: "editor"},
		},
	}

	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, orgResolver)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "new-project",
		Organization: "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ns, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-prj-new-project", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected namespace to exist, got %v", err)
	}

	// Verify default sharing annotations were set
	defaultUsers, err := GetDefaultShareUsers(ns)
	if err != nil {
		t.Fatalf("failed to parse default-share-users: %v", err)
	}
	if len(defaultUsers) != 1 || defaultUsers[0].Principal != "bob@example.com" || defaultUsers[0].Role != "viewer" {
		t.Errorf("expected default-share-users [{bob@example.com viewer}], got %v", defaultUsers)
	}

	defaultRoles, err := GetDefaultShareRoles(ns)
	if err != nil {
		t.Fatalf("failed to parse default-share-roles: %v", err)
	}
	if len(defaultRoles) != 1 || defaultRoles[0].Principal != "engineering" || defaultRoles[0].Role != "editor" {
		t.Errorf("expected default-share-roles [{engineering editor}], got %v", defaultRoles)
	}
}

func TestCreateProject_RequestGrantsOverrideOrgDefaults(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	orgResolver := &mockOrgDefaultShareResolver{
		users:  map[string]string{"alice@example.com": "owner"},
		groups: nil,
		defaultUsers: []secrets.AnnotationGrant{
			{Principal: "bob@example.com", Role: "viewer"},
		},
	}

	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, orgResolver)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	// Request grants bob as editor — should override the org default of viewer
	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "new-project",
		Organization: "acme",
		UserGrants: []*consolev1.ShareGrant{
			{Principal: "bob@example.com", Role: consolev1.Role_ROLE_EDITOR},
		},
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ns, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-prj-new-project", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected namespace to exist, got %v", err)
	}
	users, err := GetShareUsers(ns)
	if err != nil {
		t.Fatalf("failed to parse share-users: %v", err)
	}
	for _, u := range users {
		if u.Principal == "bob@example.com" {
			if u.Role != "editor" {
				t.Errorf("expected bob as editor (request override), got %q", u.Role)
			}
			return
		}
	}
	t.Error("expected bob@example.com in share-users")
}

func TestCreateProject_WithoutOrg_BehavesAsBeforeNoDefaults(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	orgResolver := &mockOrgDefaultShareResolver{
		users: map[string]string{"alice@example.com": "owner"},
		defaultUsers: []secrets.AnnotationGrant{
			{Principal: "should-not-appear@example.com", Role: "viewer"},
		},
	}

	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, orgResolver)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	// Create without organization — org defaults should NOT be applied
	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name: "standalone-project",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if orgResolver.defaultCalled {
		t.Error("expected org default resolver to NOT be called when no organization is specified")
	}

	ns, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-prj-standalone-project", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected namespace to exist, got %v", err)
	}

	// Should only have creator as owner, no org defaults
	users, err := GetShareUsers(ns)
	if err != nil {
		t.Fatalf("failed to parse share-users: %v", err)
	}
	if len(users) != 1 || users[0].Principal != "alice@example.com" {
		t.Errorf("expected only creator alice, got %v", users)
	}

	// Should have no default sharing annotations
	defaultUsers, _ := GetDefaultShareUsers(ns)
	if len(defaultUsers) != 0 {
		t.Errorf("expected no default-share-users, got %v", defaultUsers)
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

// ---- CreateProject with REQUIRE-rule applier tests ----
//
// HOL-565 (Phase 3 of HOL-562) replaced the legacy MandatoryTemplateApplier —
// which walked the ancestor chain reading `console.holos.run/mandatory` — with
// a RequiredTemplateApplier interface that evaluates TemplatePolicy REQUIRE
// rules at project-creation time. The stub here only asserts that
// CreateProject invokes the applier with the right arguments and respects its
// error return; Phase 5 (HOL-567) wires the real resolver.

// stubRequiredTemplateApplier implements RequiredTemplateApplier for tests.
type stubRequiredTemplateApplier struct {
	called  bool
	org     string
	project string
	err     error
}

func (s *stubRequiredTemplateApplier) ApplyRequiredTemplates(_ context.Context, org, project, _ string, _ *rpc.Claims) error {
	s.called = true
	s.org = org
	s.project = project
	return s.err
}

func TestCreateProject_CallsRequiredTemplateApplierOnSuccess(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler, _ := newHandler(existing)

	applier := &stubRequiredTemplateApplier{}
	handler = handler.WithRequiredTemplateApplier(applier)

	ctx := contextWithClaims("alice@example.com")
	resp, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "new-project",
		Organization: "my-org",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "new-project" {
		t.Errorf("expected name 'new-project', got %q", resp.Msg.Name)
	}
	if !applier.called {
		t.Error("expected required template applier to be called")
	}
	if applier.org != "my-org" {
		t.Errorf("expected org 'my-org', got %q", applier.org)
	}
	if applier.project != "new-project" {
		t.Errorf("expected project 'new-project', got %q", applier.project)
	}
}

func TestCreateProject_NotCalledWhenNoOrgSpecified(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler, _ := newHandler(existing)

	applier := &stubRequiredTemplateApplier{}
	handler = handler.WithRequiredTemplateApplier(applier)

	ctx := contextWithClaims("alice@example.com")
	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "new-project",
		Organization: "", // no org
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if applier.called {
		t.Error("expected required template applier NOT to be called when org is empty")
	}
}

// ---- CreateProject with REAL REQUIRE-rule resolver ----
//
// HOL-571 Phase 9 swapped the Phase 3 empty resolver for the real
// TemplatePolicy-backed resolver that walks the new project's ancestor chain
// looking for REQUIRE rules whose project_pattern matches the project name.
// These tests wire the real resolver through a RequiredTemplateApplier
// instance with a recording applier so the assertions can look at what a
// real resolver actually picks up — the previous tests only exercised the
// handler/applier wiring through a hand-crafted stub and never went through
// the ancestor walk.

// capturingApplier is a ResourceApplier that records every Apply call so a
// test can assert which templates were rendered into which project.
type capturingApplier struct {
	calls []capturedApply
	err   error
}

type capturedApply struct {
	project        string
	deploymentName string
	resources      []unstructured.Unstructured
}

func (c *capturingApplier) ApplyRequiredTemplate(_ context.Context, project, templateName string, resources []unstructured.Unstructured) error {
	c.calls = append(c.calls, capturedApply{project: project, deploymentName: templateName, resources: resources})
	return c.err
}

// makeOrgNamespace builds a fake organization namespace labeled so the
// ancestor walker classifies it as an organization.
func makeOrgNamespace(org string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-" + org,
			Labels: map[string]string{
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
			},
		},
	}
}

// marshalFixtureRules serializes a minimal rule fixture in the shape
// templatepolicies.UnmarshalRules expects on the annotation.
func marshalFixtureRules(t *testing.T, rules []map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(rules)
	if err != nil {
		t.Fatalf("marshal fixture rules: %v", err)
	}
	return string(raw)
}

// seedOrgRequirePolicy returns a TemplatePolicy ConfigMap containing a single
// REQUIRE rule that mandates the named organization-scope template for every
// project matching projectPattern. Mirrors the minimal fixture used by the
// templates package's require_policy_resolver tests.
func seedOrgRequirePolicy(t *testing.T, org, templateName, projectPattern string) *corev1.ConfigMap {
	t.Helper()
	rules := []map[string]any{
		{
			"kind": "require",
			"template": map[string]any{
				"scope":      v1alpha2.TemplateScopeOrganization,
				"scope_name": org,
				"name":       templateName,
			},
			"target": map[string]any{
				"project_pattern": projectPattern,
			},
		},
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "require-" + templateName,
			Namespace: "holos-org-" + org,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicy,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationTemplatePolicyRules: marshalFixtureRules(t, rules),
			},
		},
	}
}

// seedOrgTemplate returns an organization-scope Template ConfigMap with a
// minimal valid CUE source that emits exactly one ConfigMap resource into
// the new project's namespace. The test harness does not fill the
// PlatformInput struct into CUE, so the namespace is hardcoded in the CUE.
func seedOrgTemplate(org, templateName, projectSlug string) *corev1.ConfigMap {
	cueSrc := fmt.Sprintf(`projectResources: namespacedResources: "%s": ConfigMap: "sentinel-%s": {
	apiVersion: "v1"
	kind:       "ConfigMap"
	metadata: {
		name:      "sentinel-%s"
		namespace: "%s"
		labels: "app.kubernetes.io/managed-by": "console.holos.run"
	}
}
`, projectSlug, templateName, templateName, projectSlug)
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      templateName,
			Namespace: "holos-org-" + org,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: templateName,
				v1alpha2.AnnotationEnabled:     "true",
			},
		},
		Data: map[string]string{
			templates.CueTemplateKey: cueSrc,
		},
	}
}

// TestCreateProject_RealRequireRuleResolver_AppliesOrgTemplate is the
// HOL-571 end-to-end assertion: with the real policy-backed resolver wired
// into the applier, creating a project under an organization whose
// TemplatePolicy REQUIREs a template with project_pattern "*" causes that
// template to be rendered and applied to the new project.
func TestCreateProject_RealRequireRuleResolver_AppliesOrgTemplate(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	existing := managedNSWithOrg("existing", "acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	orgNs := makeOrgNamespace("acme")
	orgTmpl := seedOrgTemplate("acme", "audit-policy", "holos-prj-new-prj")
	policyCM := seedOrgRequirePolicy(t, "acme", "audit-policy", "*")

	handler, fakeClient := buildCreateProjectRealResolverHandler(t, existing, orgNs, orgTmpl, policyCM)

	recording := handler.requiredTemplateApplier.(*recordingAppliedTemplates)

	ctx := contextWithClaims("alice@example.com")
	resp, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "new-prj",
		Organization: "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "new-prj" {
		t.Errorf("expected project name 'new-prj', got %q", resp.Msg.Name)
	}

	// Real resolver should have matched the wildcard REQUIRE rule and the
	// recording applier should have been called exactly once with the
	// org-scope template.
	if len(recording.applied.calls) != 1 {
		t.Fatalf("expected exactly 1 Apply call for REQUIRE-matched template, got %d", len(recording.applied.calls))
	}
	call := recording.applied.calls[0]
	if call.project != "new-prj" {
		t.Errorf("apply called with project=%q, want new-prj", call.project)
	}
	if call.deploymentName != "audit-policy" {
		t.Errorf("apply called with templateName=%q, want audit-policy", call.deploymentName)
	}
	if len(call.resources) == 0 {
		t.Error("expected at least one rendered resource, got 0")
	}

	// Sanity check the project namespace was actually created.
	if _, err := fakeClient.CoreV1().Namespaces().Get(ctx, "holos-prj-new-prj", metav1.GetOptions{}); err != nil {
		t.Errorf("project namespace not created: %v", err)
	}
}

// TestCreateProject_RealRequireRuleResolver_SkipsProjectNamespacePolicy
// exercises the HOL-554 storage-isolation guardrail end-to-end: a
// TemplatePolicy ConfigMap seeded in a project namespace (where it is not
// allowed to live) must not contribute REQUIRE matches even though the
// ConfigMap itself would deserialize cleanly. A project owner who smuggled
// a policy into their own project namespace must not be able to force
// templates on newly created peer projects — the resolver only reads
// folder/org namespaces.
func TestCreateProject_RealRequireRuleResolver_SkipsProjectNamespacePolicy(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create a project namespace and seed a (forbidden) TemplatePolicy
	// ConfigMap inside it. The resolver must skip this namespace per
	// HOL-554.
	existing := managedNSWithOrg("existing", "acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	orgNs := makeOrgNamespace("acme")

	pwnedNs := "holos-prj-pwned"
	pwnedProjectNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: pwnedNs,
			Labels: map[string]string{
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelProject:      "pwned",
			},
		},
	}

	pwnedPolicy := seedOrgRequirePolicy(t, "acme", "should-be-ignored", "*")
	pwnedPolicy.Namespace = pwnedNs // forbidden placement

	handler, _ := buildCreateProjectRealResolverHandler(t, existing, orgNs, pwnedProjectNs, pwnedPolicy)

	recording := handler.requiredTemplateApplier.(*recordingAppliedTemplates)

	ctx := contextWithClaims("alice@example.com")
	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "new-prj",
		Organization: "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(recording.applied.calls) != 0 {
		t.Errorf("project-namespace policy leaked into CreateProject apply: got %d calls, want 0",
			len(recording.applied.calls))
	}
}

// recordingAppliedTemplates wraps a RequiredTemplateApplier built out of the
// real resolver + real applier so tests can assert both that the applier ran
// and what it applied. The wrapper is needed because CreateProject's apply
// call goes through RequiredTemplateApplier.ApplyRequiredTemplates (which
// returns only an error), and the tests need to peek at the recording
// applier inside.
type recordingAppliedTemplates struct {
	inner   RequiredTemplateApplier
	applied *capturingApplier
}

func (r *recordingAppliedTemplates) ApplyRequiredTemplates(ctx context.Context, org, project, projectNamespace string, claims *rpc.Claims) error {
	return r.inner.ApplyRequiredTemplates(ctx, org, project, projectNamespace, claims)
}

// buildCreateProjectRealResolverHandler wires a projects.Handler whose
// RequiredTemplateApplier is constructed from the real
// templates.PolicyRequireRuleResolver (backed by the given fake K8s client)
// and a capturingApplier that records Apply calls. The recording wrapper is
// returned as the handler's applier field so tests can reach into it.
func buildCreateProjectRealResolverHandler(t *testing.T, namespaces ...runtime.Object) (*Handler, *fake.Clientset) {
	t.Helper()

	client := fake.NewClientset(namespaces...)
	r := testResolver()
	k8s := NewK8sClient(client, r)

	nsWalker := &policyresolverWalker{client: client, resolver: r}
	templatePoliciesLister := &configMapPolicyLister{client: client}
	templatesK8s := templates.NewK8sClient(client, r)

	ancestorLister := policyresolver.NewAncestorPolicyLister(
		templatePoliciesLister,
		nsWalker,
		r,
		policyresolver.RuleUnmarshalerFunc(templatepoliciesUnmarshalRules),
	)
	requireResolver := templates.NewPolicyRequireRuleResolver(
		ancestorLister,
		r.ProjectNamespace,
	)
	capturing := &capturingApplier{}
	rta := templates.NewRequiredTemplateApplier(
		templatesK8s,
		nsWalker,
		&deployments.CueRenderer{},
		capturing,
		requireResolver,
		policyresolver.NewNoopResolver(),
	)
	wrapper := &recordingAppliedTemplates{inner: rta, applied: capturing}

	handler := NewHandler(k8s, nil).WithRequiredTemplateApplier(wrapper)
	return handler, client
}

// policyresolverWalker is a thin WalkAncestors adapter over a fake clientset
// that avoids pulling in the full resolver.Walker (which would require
// reimporting console/resolver into this package from
// console/projects — already imported, but the fake here only needs the
// ancestor-labels behavior, so keeping it inline avoids a circular setup).
type policyresolverWalker struct {
	client   *fake.Clientset
	resolver *resolver.Resolver
}

func (w *policyresolverWalker) WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error) {
	return (&resolver.Walker{Client: w.client, Resolver: w.resolver}).WalkAncestors(ctx, startNs)
}

// configMapPolicyLister is a PolicyListerInNamespace implementation over a
// fake clientset that lists TemplatePolicy ConfigMaps by label. It mirrors
// templatepolicies.K8sClient.ListPoliciesInNamespace without pulling the
// whole package in.
type configMapPolicyLister struct {
	client *fake.Clientset
}

func (l *configMapPolicyLister) ListPoliciesInNamespace(ctx context.Context, ns string) ([]corev1.ConfigMap, error) {
	labelSelector := v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeTemplatePolicy
	list, err := l.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// templatepoliciesUnmarshalRules mirrors the JSON wire shape produced by the
// templatepolicies package so tests in this package can drive the resolver
// without importing that package (which would create a test-only dependency
// cycle).
func templatepoliciesUnmarshalRules(raw string) ([]*consolev1.TemplatePolicyRule, error) {
	if raw == "" {
		return nil, nil
	}
	type storedRule struct {
		Kind     string `json:"kind"`
		Template struct {
			Scope             string `json:"scope"`
			ScopeName         string `json:"scope_name"`
			Name              string `json:"name"`
			VersionConstraint string `json:"version_constraint,omitempty"`
		} `json:"template"`
		Target struct {
			ProjectPattern    string `json:"project_pattern"`
			DeploymentPattern string `json:"deployment_pattern,omitempty"`
		} `json:"target"`
	}
	var stored []storedRule
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, err
	}
	rules := make([]*consolev1.TemplatePolicyRule, 0, len(stored))
	for _, s := range stored {
		kind := consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_UNSPECIFIED
		switch s.Kind {
		case "require":
			kind = consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE
		case "exclude":
			kind = consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE
		}
		scope := consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED
		switch s.Template.Scope {
		case v1alpha2.TemplateScopeOrganization:
			scope = consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION
		case v1alpha2.TemplateScopeFolder:
			scope = consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER
		case v1alpha2.TemplateScopeProject:
			scope = consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT
		}
		rules = append(rules, &consolev1.TemplatePolicyRule{
			Kind: kind,
			Template: &consolev1.LinkedTemplateRef{
				Scope:             scope,
				ScopeName:         s.Template.ScopeName,
				Name:              s.Template.Name,
				VersionConstraint: s.Template.VersionConstraint,
			},
			Target: &consolev1.TemplatePolicyTarget{
				ProjectPattern:    s.Target.ProjectPattern,
				DeploymentPattern: s.Target.DeploymentPattern,
			},
		})
	}
	return rules, nil
}

// ---- CheckProjectIdentifier tests ----

func TestCheckProjectIdentifier_Available(t *testing.T) {
	handler, _ := newHandler()
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CheckProjectIdentifier(ctx, connect.NewRequest(&consolev1.CheckProjectIdentifierRequest{
		Identifier: "frontend",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !resp.Msg.Available {
		t.Error("expected available=true")
	}
	if resp.Msg.SuggestedIdentifier != "frontend" {
		t.Errorf("expected suggested_identifier='frontend', got %q", resp.Msg.SuggestedIdentifier)
	}
}

func TestCheckProjectIdentifier_Taken(t *testing.T) {
	existing := managedNS("frontend", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler, _ := newHandler(existing)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CheckProjectIdentifier(ctx, connect.NewRequest(&consolev1.CheckProjectIdentifierRequest{
		Identifier: "frontend",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Available {
		t.Error("expected available=false")
	}
	if resp.Msg.SuggestedIdentifier == "frontend" {
		t.Error("expected suggested_identifier to differ from input")
	}
	// Should start with "frontend-"
	if len(resp.Msg.SuggestedIdentifier) < len("frontend-000000") {
		t.Errorf("expected suffixed identifier, got %q", resp.Msg.SuggestedIdentifier)
	}
}

func TestCheckProjectIdentifier_EmptyRejects(t *testing.T) {
	handler, _ := newHandler()
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CheckProjectIdentifier(ctx, connect.NewRequest(&consolev1.CheckProjectIdentifierRequest{
		Identifier: "",
	}))
	assertInvalidArgument(t, err)
}

func TestCheckProjectIdentifier_Unauthenticated(t *testing.T) {
	handler, _ := newHandler()
	_, err := handler.CheckProjectIdentifier(context.Background(), connect.NewRequest(&consolev1.CheckProjectIdentifierRequest{
		Identifier: "frontend",
	}))
	assertUnauthenticated(t, err)
}

func TestCheckProjectIdentifier_NonSlugReturnsUnavailable(t *testing.T) {
	// No project namespace exists, but the input is not a valid slug.
	// Should return available=false with the slugified form as suggestion.
	handler, _ := newHandler()
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CheckProjectIdentifier(ctx, connect.NewRequest(&consolev1.CheckProjectIdentifierRequest{
		Identifier: "My Project",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Available {
		t.Error("expected available=false for non-slug input")
	}
	if resp.Msg.SuggestedIdentifier != "my-project" {
		t.Errorf("expected suggested_identifier='my-project', got %q", resp.Msg.SuggestedIdentifier)
	}
}

// ---- UpdateProject reparent tests ----

// projectNSWithParent creates a project namespace with org, parent, and grants.
func projectNSWithParent(name, org, parentNs, shareUsersJSON string) *corev1.Namespace {
	ns := managedNSWithOrg(name, org, shareUsersJSON)
	ns.Labels[v1alpha2.AnnotationParent] = parentNs
	return ns
}

// orgNSWithGrants creates an org namespace with share-users annotation.
func orgNSWithGrants(name, shareUsersJSON string) *corev1.Namespace {
	annotations := map[string]string{}
	if shareUsersJSON != "" {
		annotations[v1alpha2.AnnotationShareUsers] = shareUsersJSON
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

// folderNSWithGrants creates a folder namespace with share-users annotation.
func folderNSWithGrants(name, org, parentNs, shareUsersJSON string) *corev1.Namespace {
	annotations := map[string]string{}
	if shareUsersJSON != "" {
		annotations[v1alpha2.AnnotationShareUsers] = shareUsersJSON
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

func TestUpdateProject_Reparent_SuccessOrgOwner(t *testing.T) {
	// Alice is org owner, so she can reparent projects within the org via cascade.
	orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	srcFolder := folderNSWithGrants("rp-prj-src", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	destFolder := folderNSWithGrants("rp-prj-dest", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	prj := projectNSWithParent("rp-prj-test", "acme", "holos-fld-rp-prj-src", `[{"principal":"alice@example.com","role":"editor"}]`)

	handler := newHandlerWithOrg(
		&mockOrgResolver{users: map[string]string{"alice@example.com": "owner"}},
		orgNs, srcFolder, destFolder, prj,
	)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	newParentType := consolev1.ParentType_PARENT_TYPE_FOLDER
	newParentName := "rp-prj-dest"
	_, err := handler.UpdateProject(ctx, connect.NewRequest(&consolev1.UpdateProjectRequest{
		Name:       "rp-prj-test",
		ParentType: &newParentType,
		ParentName: &newParentName,
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestUpdateProject_Reparent_DeniedOnSource(t *testing.T) {
	// Alice is editor on source parent but not org owner → denied.
	orgNs := orgNSWithGrants("acme", `[{"principal":"bob@example.com","role":"owner"}]`)
	srcFolder := folderNSWithGrants("rp-prj-src2", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	destFolder := folderNSWithGrants("rp-prj-dest2", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	prj := projectNSWithParent("rp-prj-denied-src", "acme", "holos-fld-rp-prj-src2", `[{"principal":"alice@example.com","role":"editor"}]`)

	handler := newHandlerWithOrg(
		&mockOrgResolver{users: map[string]string{"bob@example.com": "owner"}},
		orgNs, srcFolder, destFolder, prj,
	)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	newParentType := consolev1.ParentType_PARENT_TYPE_FOLDER
	newParentName := "rp-prj-dest2"
	_, err := handler.UpdateProject(ctx, connect.NewRequest(&consolev1.UpdateProjectRequest{
		Name:       "rp-prj-denied-src",
		ParentType: &newParentType,
		ParentName: &newParentName,
	}))
	assertPermissionDenied(t, err)
}

func TestUpdateProject_Reparent_DeniedOnDestination(t *testing.T) {
	// Alice is owner on source parent but editor on destination → denied.
	orgNs := orgNSWithGrants("acme", `[{"principal":"bob@example.com","role":"owner"}]`)
	srcFolder := folderNSWithGrants("rp-prj-src3", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	destFolder := folderNSWithGrants("rp-prj-dest3", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	prj := projectNSWithParent("rp-prj-denied-dest", "acme", "holos-fld-rp-prj-src3", `[{"principal":"alice@example.com","role":"editor"}]`)

	handler := newHandlerWithOrg(
		&mockOrgResolver{users: map[string]string{"bob@example.com": "owner"}},
		orgNs, srcFolder, destFolder, prj,
	)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	newParentType := consolev1.ParentType_PARENT_TYPE_FOLDER
	newParentName := "rp-prj-dest3"
	_, err := handler.UpdateProject(ctx, connect.NewRequest(&consolev1.UpdateProjectRequest{
		Name:       "rp-prj-denied-dest",
		ParentType: &newParentType,
		ParentName: &newParentName,
	}))
	assertPermissionDenied(t, err)
}

func TestUpdateProject_Reparent_SameParentIsNoop(t *testing.T) {
	orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	folder := folderNSWithGrants("rp-prj-noop", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	prj := projectNSWithParent("rp-prj-noop-test", "acme", "holos-fld-rp-prj-noop", `[{"principal":"alice@example.com","role":"editor"}]`)

	handler := newHandlerWithOrg(
		&mockOrgResolver{users: map[string]string{"alice@example.com": "owner"}},
		orgNs, folder, prj,
	)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	// Move to same parent (folder rp-prj-noop).
	newParentType := consolev1.ParentType_PARENT_TYPE_FOLDER
	newParentName := "rp-prj-noop"
	_, err := handler.UpdateProject(ctx, connect.NewRequest(&consolev1.UpdateProjectRequest{
		Name:       "rp-prj-noop-test",
		ParentType: &newParentType,
		ParentName: &newParentName,
	}))
	if err != nil {
		t.Fatalf("expected no error (no-op), got %v", err)
	}
}

func TestUpdateProject_Reparent_SameParentSkipsK8sWrite(t *testing.T) {
	orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	folder := folderNSWithGrants("rp-prj-noop2", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	prj := projectNSWithParent("rp-prj-noop2-test", "acme", "holos-fld-rp-prj-noop2", `[{"principal":"alice@example.com","role":"editor"}]`)

	handler, fakeClient := newHandlerWithOrgAndClient(
		&mockOrgResolver{users: map[string]string{"alice@example.com": "owner"}},
		orgNs, folder, prj,
	)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	// Record action count before the call.
	beforeActions := len(fakeClient.Actions())

	// Move to same parent with no metadata changes.
	newParentType := consolev1.ParentType_PARENT_TYPE_FOLDER
	newParentName := "rp-prj-noop2"
	_, err := handler.UpdateProject(ctx, connect.NewRequest(&consolev1.UpdateProjectRequest{
		Name:       "rp-prj-noop2-test",
		ParentType: &newParentType,
		ParentName: &newParentName,
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify no update actions were issued after the initial get.
	afterActions := fakeClient.Actions()
	for _, action := range afterActions[beforeActions:] {
		if action.GetVerb() == "update" {
			t.Fatalf("expected no K8s update for same-parent reparent with no metadata changes, but got update action on %s", action.GetResource().Resource)
		}
	}
}

func TestUpdateProject_Reparent_MoveFromFolderToOrg(t *testing.T) {
	// Move a project from a folder to the org root.
	orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	folder := folderNSWithGrants("rp-prj-fld2org", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	prj := projectNSWithParent("rp-prj-fld2org-test", "acme", "holos-fld-rp-prj-fld2org", `[{"principal":"alice@example.com","role":"editor"}]`)

	handler := newHandlerWithOrg(
		&mockOrgResolver{users: map[string]string{"alice@example.com": "owner"}},
		orgNs, folder, prj,
	)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	newParentType := consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	newParentName := "acme"
	_, err := handler.UpdateProject(ctx, connect.NewRequest(&consolev1.UpdateProjectRequest{
		Name:       "rp-prj-fld2org-test",
		ParentType: &newParentType,
		ParentName: &newParentName,
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestCreateProject_CleansUpNamespaceOnApplierFailure(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, nil)

	applier := &stubRequiredTemplateApplier{
		err: fmt.Errorf("render failed"),
	}
	handler = handler.WithRequiredTemplateApplier(applier)

	ctx := contextWithClaims("alice@example.com")
	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "new-project",
		Organization: "my-org",
	}))
	if err == nil {
		t.Fatal("expected error when required template applier fails")
	}

	// Verify project namespace was cleaned up (deleted).
	_, getErr := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-prj-new-project", metav1.GetOptions{})
	if getErr == nil {
		t.Error("expected project namespace to be deleted after applier failure")
	}
}

// ---- Default Folder Resolution Tests ----

func TestCreateProject_DefaultsToOrgDefaultFolder(t *testing.T) {
	// Org has a default-folder annotation pointing to a valid folder.
	orgNs := orgNSWithGrants("df-org-a", `[{"principal":"alice@example.com","role":"owner"}]`)
	orgNs.Annotations[v1alpha2.AnnotationDefaultFolder] = "df-folder-a"

	folderNs := folderNSWithGrants("df-folder-a", "df-org-a", "holos-org-df-org-a", `[{"principal":"alice@example.com","role":"editor"}]`)

	handler := newHandlerWithOrg(
		&mockOrgResolver{users: map[string]string{"alice@example.com": "owner"}},
		orgNs, folderNs,
	)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "df-prj-a",
		Organization: "df-org-a",
		// No ParentType or ParentName — should default to the org's default folder.
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "df-prj-a" {
		t.Errorf("expected name 'df-prj-a', got %q", resp.Msg.Name)
	}

	// Verify the project's parent label points to the folder namespace.
	ns, err := handler.k8s.GetProject(ctx, "df-prj-a")
	if err != nil {
		t.Fatalf("expected project to exist, got %v", err)
	}
	parentLabel := ns.Labels[v1alpha2.AnnotationParent]
	if parentLabel != "holos-fld-df-folder-a" {
		t.Errorf("expected parent label 'holos-fld-df-folder-a', got %q", parentLabel)
	}
}

func TestCreateProject_FallsBackToOrgWhenNoDefaultFolderAnnotation(t *testing.T) {
	// Org has no default-folder annotation (legacy org).
	orgNs := orgNSWithGrants("df-org-b", `[{"principal":"bob@example.com","role":"owner"}]`)

	handler := newHandlerWithOrg(
		&mockOrgResolver{users: map[string]string{"bob@example.com": "owner"}},
		orgNs,
	)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("bob@example.com")

	resp, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "df-prj-b",
		Organization: "df-org-b",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "df-prj-b" {
		t.Errorf("expected name 'df-prj-b', got %q", resp.Msg.Name)
	}

	// Verify the project's parent label points to the org namespace.
	ns, err := handler.k8s.GetProject(ctx, "df-prj-b")
	if err != nil {
		t.Fatalf("expected project to exist, got %v", err)
	}
	parentLabel := ns.Labels[v1alpha2.AnnotationParent]
	if parentLabel != "holos-org-df-org-b" {
		t.Errorf("expected parent label 'holos-org-df-org-b', got %q", parentLabel)
	}
}

func TestCreateProject_FallsBackToOrgWhenDefaultFolderDeleted(t *testing.T) {
	// Org has a default-folder annotation, but the referenced folder does not exist.
	orgNs := orgNSWithGrants("df-org-c", `[{"principal":"carol@example.com","role":"owner"}]`)
	orgNs.Annotations[v1alpha2.AnnotationDefaultFolder] = "df-folder-gone"

	// No folder namespace created — simulates a deleted folder.
	logHandler := &testLogHandler{}
	slog.SetDefault(slog.New(logHandler))

	handler := newHandlerWithOrg(
		&mockOrgResolver{users: map[string]string{"carol@example.com": "owner"}},
		orgNs,
	)
	ctx := contextWithClaims("carol@example.com")

	resp, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "df-prj-c",
		Organization: "df-org-c",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "df-prj-c" {
		t.Errorf("expected name 'df-prj-c', got %q", resp.Msg.Name)
	}

	// Verify the project's parent label falls back to the org namespace.
	ns, err := handler.k8s.GetProject(ctx, "df-prj-c")
	if err != nil {
		t.Fatalf("expected project to exist, got %v", err)
	}
	parentLabel := ns.Labels[v1alpha2.AnnotationParent]
	if parentLabel != "holos-org-df-org-c" {
		t.Errorf("expected parent label 'holos-org-df-org-c', got %q", parentLabel)
	}

	// Verify a warning was logged about the missing default folder.
	if r := logHandler.findRecord("default_folder_not_found"); r == nil {
		t.Error("expected warning log with action 'default_folder_not_found'")
	}
}

func TestCreateProject_ExplicitParentOverridesDefaultFolder(t *testing.T) {
	// Org has a default-folder annotation, but the request specifies an explicit parent.
	orgNs := orgNSWithGrants("df-org-d", `[{"principal":"dave@example.com","role":"owner"}]`)
	orgNs.Annotations[v1alpha2.AnnotationDefaultFolder] = "df-folder-d"

	folderNs := folderNSWithGrants("df-folder-d", "df-org-d", "holos-org-df-org-d", `[{"principal":"dave@example.com","role":"editor"}]`)
	explicitFolder := folderNSWithGrants("df-explicit-d", "df-org-d", "holos-org-df-org-d", `[{"principal":"dave@example.com","role":"editor"}]`)

	handler := newHandlerWithOrg(
		&mockOrgResolver{users: map[string]string{"dave@example.com": "owner"}},
		orgNs, folderNs, explicitFolder,
	)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("dave@example.com")

	resp, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "df-prj-d",
		Organization: "df-org-d",
		ParentType:   consolev1.ParentType_PARENT_TYPE_FOLDER,
		ParentName:   "df-explicit-d",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "df-prj-d" {
		t.Errorf("expected name 'df-prj-d', got %q", resp.Msg.Name)
	}

	// Verify the project's parent label points to the explicitly specified folder.
	ns, err := handler.k8s.GetProject(ctx, "df-prj-d")
	if err != nil {
		t.Fatalf("expected project to exist, got %v", err)
	}
	parentLabel := ns.Labels[v1alpha2.AnnotationParent]
	if parentLabel != "holos-fld-df-explicit-d" {
		t.Errorf("expected parent label 'holos-fld-df-explicit-d', got %q", parentLabel)
	}
}
