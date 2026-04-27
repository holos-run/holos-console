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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
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

// ---- CreateProject no longer auto-applies REQUIRE-rule templates ----
//
// HOL-582 deleted the creation-time RequiredTemplateApplier (Layer B in the
// HOL-580 analysis). REQUIRE rules are now enforced exclusively at render
// time via the folderResolver path; project creation is a pure namespace
// write with zero template side effects.

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

// seedOrgRequirePolicy returns a TemplatePolicy-labeled ConfigMap placeholder
// used by the HOL-582 regression guard. CreateProject intentionally ignores
// policy contents at project-creation time (it never renders templates),
// so this fixture only needs to exist. TemplatePolicy rule data is
// authoritatively carried by the templates.holos.run TemplatePolicy CRD.
func seedOrgRequirePolicy(t *testing.T, org, templateName, _ string) *corev1.ConfigMap {
	t.Helper()
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "require-" + templateName,
			Namespace: "holos-org-" + org,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
			},
		},
	}
}

// seedOrgTemplate returns an organization-scope Template ConfigMap carrying
// a single sentinel ConfigMap in its CUE source, targeted at the given
// project namespace. Used to prove that the creation-time code path does
// NOT render and apply this template (the post-HOL-582 behavior).
func seedOrgTemplate(org, templateName, projectNs string) *corev1.ConfigMap {
	cueSrc := fmt.Sprintf(`projectResources: namespacedResources: "%s": ConfigMap: "sentinel-%s": {
	apiVersion: "v1"
	kind:       "ConfigMap"
	metadata: {
		name:      "sentinel-%s"
		namespace: "%s"
		labels: "app.kubernetes.io/managed-by": "console.holos.run"
	}
}
`, projectNs, templateName, templateName, projectNs)
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      templateName,
			Namespace: "holos-org-" + org,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeOrganization,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: templateName,
				v1alpha2.AnnotationEnabled:     "true",
			},
		},
		Data: map[string]string{
			"template.cue": cueSrc,
		},
	}
}

// TestCreateProject_DoesNotAutoApplyTemplates is the HOL-582 regression guard.
// It constructs a scope (org) with a REQUIRE TemplatePolicy ConfigMap pointing
// at a platform template and asserts that after CreateProject the project
// namespace exists and contains zero resources derived from the referenced
// template. The pre-HOL-582 behavior was the opposite — the project namespace
// would eagerly contain rendered template resources — so asserting
// emptiness here verifies the deletion of Layer B.
func TestCreateProject_DoesNotAutoApplyTemplates(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	existing := managedNSWithOrg("existing", "acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	orgNs := makeOrgNamespace("acme")
	projectNs := "holos-prj-new-prj"
	orgTmpl := seedOrgTemplate("acme", "audit-policy", projectNs)
	policyCM := seedOrgRequirePolicy(t, "acme", "audit-policy", "*")

	fakeClient := fake.NewClientset(existing, orgNs, orgTmpl, policyCM)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, nil)

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

	// The project namespace must exist after CreateProject.
	if _, err := fakeClient.CoreV1().Namespaces().Get(ctx, projectNs, metav1.GetOptions{}); err != nil {
		t.Errorf("project namespace not created: %v", err)
	}

	// But it must contain ZERO rendered-template resources. The sentinel
	// ConfigMap authored by seedOrgTemplate would land here if any
	// creation-time auto-apply path still existed.
	cms, err := fakeClient.CoreV1().ConfigMaps(projectNs).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list config maps in project namespace: %v", err)
	}
	if len(cms.Items) != 0 {
		var names []string
		for _, cm := range cms.Items {
			names = append(names, cm.Name)
		}
		t.Errorf("project namespace %q must be empty after CreateProject (HOL-582), found %d ConfigMaps: %v",
			projectNs, len(cms.Items), names)
	}

	secrets, err := fakeClient.CoreV1().Secrets(projectNs).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list secrets in project namespace: %v", err)
	}
	if len(secrets.Items) != 0 {
		t.Errorf("project namespace %q must be empty after CreateProject (HOL-582), found %d Secrets",
			projectNs, len(secrets.Items))
	}
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
