package folders

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rpc"
	secrpkg "github.com/holos-run/holos-console/console/secrets"
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

// newTestHandler creates a handler with a fake K8s client pre-populated with namespaces.
func newTestHandler(namespaces ...*corev1.Namespace) *Handler {
	objs := make([]runtime.Object, len(namespaces))
	for i, ns := range namespaces {
		objs[i] = ns
	}
	fakeClient := fake.NewClientset(objs...)
	k8s := NewK8sClient(fakeClient, testResolver())
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return NewHandler(k8s)
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

// ---- ListFolders tests ----

func TestListFolders_Unauthenticated(t *testing.T) {
	handler := newTestHandler()
	_, err := handler.ListFolders(context.Background(), connect.NewRequest(&consolev1.ListFoldersRequest{}))
	assertUnauthenticated(t, err)
}

func TestListFolders_FiltersByAccess(t *testing.T) {
	f1 := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	f2 := folderNSWithGrants("ops", "acme", "holos-org-acme", `[{"principal":"bob@example.com","role":"owner"}]`)
	handler := newTestHandler(f1, f2)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.ListFolders(ctx, connect.NewRequest(&consolev1.ListFoldersRequest{Organization: "acme"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Folders) != 1 {
		t.Errorf("expected 1 folder, got %d", len(resp.Msg.Folders))
	}
	if resp.Msg.Folders[0].Name != "eng" {
		t.Errorf("expected 'eng', got %q", resp.Msg.Folders[0].Name)
	}
}

// ---- GetFolder tests ----

func TestGetFolder_Authorized(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	f.Annotations[v1alpha2.AnnotationDisplayName] = "Engineering"
	handler := newTestHandler(f)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.GetFolder(ctx, connect.NewRequest(&consolev1.GetFolderRequest{Name: "eng"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	folder := resp.Msg.Folder
	if folder.Name != "eng" {
		t.Errorf("expected name 'eng', got %q", folder.Name)
	}
	if folder.DisplayName != "Engineering" {
		t.Errorf("expected display_name 'Engineering', got %q", folder.DisplayName)
	}
	if folder.UserRole != consolev1.Role_ROLE_VIEWER {
		t.Errorf("expected ROLE_VIEWER, got %v", folder.UserRole)
	}
}

func TestGetFolder_Denied(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"bob@example.com","role":"owner"}]`)
	handler := newTestHandler(f)
	ctx := contextWithClaims("nobody@example.com")

	_, err := handler.GetFolder(ctx, connect.NewRequest(&consolev1.GetFolderRequest{Name: "eng"}))
	assertPermissionDenied(t, err)
}

func TestGetFolder_EmptyNameRejects(t *testing.T) {
	handler := newTestHandler()
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.GetFolder(ctx, connect.NewRequest(&consolev1.GetFolderRequest{Name: ""}))
	assertInvalidArgument(t, err)
}

func TestGetFolder_Unauthenticated(t *testing.T) {
	handler := newTestHandler()
	_, err := handler.GetFolder(context.Background(), connect.NewRequest(&consolev1.GetFolderRequest{Name: "eng"}))
	assertUnauthenticated(t, err)
}

// ---- CreateFolder tests ----

func TestCreateFolder_UnderOrg_Depth1(t *testing.T) {
	// org exists in fake K8s; alice has owner access on it
	orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(orgNs)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateFolder(ctx, connect.NewRequest(&consolev1.CreateFolderRequest{
		Name:         "eng",
		Organization: "acme",
		ParentType:   consolev1.ParentType_PARENT_TYPE_ORGANIZATION,
		ParentName:   "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "eng" {
		t.Errorf("expected name 'eng', got %q", resp.Msg.Name)
	}
}

func TestCreateFolder_UnderFolder_Depth2(t *testing.T) {
	// Folder "eng" exists under org "acme" (depth 1)
	orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	engFolder := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(orgNs, engFolder)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateFolder(ctx, connect.NewRequest(&consolev1.CreateFolderRequest{
		Name:         "backend",
		Organization: "acme",
		ParentType:   consolev1.ParentType_PARENT_TYPE_FOLDER,
		ParentName:   "eng",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "backend" {
		t.Errorf("expected name 'backend', got %q", resp.Msg.Name)
	}
}

func TestCreateFolder_Depth3Allowed(t *testing.T) {
	// Create a depth-3 folder: org -> f1 -> f2 -> f3
	orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	f1 := folderNSWithGrants("f1", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	f2 := folderNSWithGrants("f2", "acme", "holos-fld-f1", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(orgNs, f1, f2)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateFolder(ctx, connect.NewRequest(&consolev1.CreateFolderRequest{
		Name:         "f3",
		Organization: "acme",
		ParentType:   consolev1.ParentType_PARENT_TYPE_FOLDER,
		ParentName:   "f2",
	}))
	if err != nil {
		t.Fatalf("expected no error at depth 3, got %v", err)
	}
	if resp.Msg.Name != "f3" {
		t.Errorf("expected name 'f3', got %q", resp.Msg.Name)
	}
}

func TestCreateFolder_Depth4Rejected(t *testing.T) {
	// Attempt to create a depth-4 folder: org -> f1 -> f2 -> f3 -> f4 (rejected)
	orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	f1 := folderNSWithGrants("f1", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	f2 := folderNSWithGrants("f2", "acme", "holos-fld-f1", `[{"principal":"alice@example.com","role":"owner"}]`)
	f3 := folderNSWithGrants("f3", "acme", "holos-fld-f2", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(orgNs, f1, f2, f3)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateFolder(ctx, connect.NewRequest(&consolev1.CreateFolderRequest{
		Name:         "f4",
		Organization: "acme",
		ParentType:   consolev1.ParentType_PARENT_TYPE_FOLDER,
		ParentName:   "f3",
	}))
	if err == nil {
		t.Fatal("expected error for depth > 3, got nil")
	}
	assertInvalidArgument(t, err)
}

func TestCreateFolder_DeriveNameFromDisplayName(t *testing.T) {
	orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(orgNs)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateFolder(ctx, connect.NewRequest(&consolev1.CreateFolderRequest{
		DisplayName:  "Engineering Team",
		Organization: "acme",
		ParentType:   consolev1.ParentType_PARENT_TYPE_ORGANIZATION,
		ParentName:   "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "engineering-team" {
		t.Errorf("expected name 'engineering-team', got %q", resp.Msg.Name)
	}
}

func TestCreateFolder_DeriveNameWithCollision(t *testing.T) {
	orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	// Pre-create the folder that will collide with the slug
	existing := folderNSWithGrants("engineering", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(orgNs, existing)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateFolder(ctx, connect.NewRequest(&consolev1.CreateFolderRequest{
		DisplayName:  "Engineering",
		Organization: "acme",
		ParentType:   consolev1.ParentType_PARENT_TYPE_ORGANIZATION,
		ParentName:   "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// Name should have a suffix since "engineering" was taken.
	if resp.Msg.Name == "engineering" {
		t.Error("expected name with suffix due to collision, got 'engineering'")
	}
	if len(resp.Msg.Name) < len("engineering-000000") {
		t.Errorf("expected suffixed name, got %q", resp.Msg.Name)
	}
}

func TestCreateFolder_MissingNameAndDisplayName(t *testing.T) {
	handler := newTestHandler()
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateFolder(ctx, connect.NewRequest(&consolev1.CreateFolderRequest{
		Organization: "acme",
		ParentType:   consolev1.ParentType_PARENT_TYPE_ORGANIZATION,
		ParentName:   "acme",
	}))
	assertInvalidArgument(t, err)
}

func TestCreateFolder_Unauthenticated(t *testing.T) {
	handler := newTestHandler()
	_, err := handler.CreateFolder(context.Background(), connect.NewRequest(&consolev1.CreateFolderRequest{
		Name:         "eng",
		Organization: "acme",
		ParentType:   consolev1.ParentType_PARENT_TYPE_ORGANIZATION,
		ParentName:   "acme",
	}))
	assertUnauthenticated(t, err)
}

func TestCreateFolder_CreatorIsAutoOwner(t *testing.T) {
	orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	fakeClient := fake.NewClientset(orgNs)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateFolder(ctx, connect.NewRequest(&consolev1.CreateFolderRequest{
		Name:         "eng",
		Organization: "acme",
		ParentType:   consolev1.ParentType_PARENT_TYPE_ORGANIZATION,
		ParentName:   "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify the created namespace has creator as owner.
	ns, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-fld-eng", metav1.GetOptions{})
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

func TestCreateFolder_DefaultShareCascadeFromOrg(t *testing.T) {
	// Org has default-share-users; folder should inherit them.
	orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	orgNs.Annotations = map[string]string{
		v1alpha2.AnnotationShareUsers:        `[{"principal":"alice@example.com","role":"owner"}]`,
		v1alpha2.AnnotationDefaultShareUsers: `[{"principal":"bob@example.com","role":"editor"}]`,
	}
	fakeClient := fake.NewClientset(orgNs)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateFolder(ctx, connect.NewRequest(&consolev1.CreateFolderRequest{
		Name:         "eng",
		Organization: "acme",
		ParentType:   consolev1.ParentType_PARENT_TYPE_ORGANIZATION,
		ParentName:   "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ns, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-fld-eng", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected namespace to exist, got %v", err)
	}
	users, err := GetShareUsers(ns)
	if err != nil {
		t.Fatalf("failed to parse share-users: %v", err)
	}
	// Should include bob from default-share-users cascade
	found := false
	for _, u := range users {
		if u.Principal == "bob@example.com" && u.Role == "editor" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected bob@example.com editor from default-share cascade, got %v", users)
	}
}

// ---- UpdateFolder tests ----

func TestUpdateFolder_EditorAllows(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler := newTestHandler(f)
	ctx := contextWithClaims("alice@example.com")

	displayName := "Updated Engineering"
	_, err := handler.UpdateFolder(ctx, connect.NewRequest(&consolev1.UpdateFolderRequest{
		Name:        "eng",
		DisplayName: &displayName,
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestUpdateFolder_ViewerDenies(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	handler := newTestHandler(f)
	ctx := contextWithClaims("alice@example.com")

	displayName := "Updated"
	_, err := handler.UpdateFolder(ctx, connect.NewRequest(&consolev1.UpdateFolderRequest{
		Name:        "eng",
		DisplayName: &displayName,
	}))
	assertPermissionDenied(t, err)
}

// ---- DeleteFolder tests ----

func TestDeleteFolder_OwnerAllows(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(f)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteFolder(ctx, connect.NewRequest(&consolev1.DeleteFolderRequest{Name: "eng"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDeleteFolder_EditorDenies(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler := newTestHandler(f)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteFolder(ctx, connect.NewRequest(&consolev1.DeleteFolderRequest{Name: "eng"}))
	assertPermissionDenied(t, err)
}

func TestDeleteFolder_FailsWithChildFolders(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	child := folderNSWithGrants("sub", "acme", "holos-fld-eng", `[]`)
	handler := newTestHandler(f, child)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteFolder(ctx, connect.NewRequest(&consolev1.DeleteFolderRequest{Name: "eng"}))
	assertFailedPrecondition(t, err)
}

func TestDeleteFolder_FailsWithChildProjects(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	prj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-api",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.AnnotationParent:  "holos-fld-eng",
			},
		},
	}
	handler := newTestHandler(f, prj)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteFolder(ctx, connect.NewRequest(&consolev1.DeleteFolderRequest{Name: "eng"}))
	assertFailedPrecondition(t, err)
}

func TestDeleteFolder_SucceedsWithNoChildren(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(f)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteFolder(ctx, connect.NewRequest(&consolev1.DeleteFolderRequest{Name: "eng"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// ---- UpdateFolderSharing tests ----

func TestUpdateFolderSharing_OwnerAllows(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(f)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.UpdateFolderSharing(ctx, connect.NewRequest(&consolev1.UpdateFolderSharingRequest{
		Name: "eng",
		UserGrants: []*consolev1.ShareGrant{
			{Principal: "alice@example.com", Role: consolev1.Role_ROLE_OWNER},
			{Principal: "bob@example.com", Role: consolev1.Role_ROLE_EDITOR},
		},
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Folder.UserGrants) != 2 {
		t.Errorf("expected 2 user grants, got %d", len(resp.Msg.Folder.UserGrants))
	}
}

func TestUpdateFolderSharing_NonOwnerDenies(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler := newTestHandler(f)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.UpdateFolderSharing(ctx, connect.NewRequest(&consolev1.UpdateFolderSharingRequest{
		Name: "eng",
	}))
	assertPermissionDenied(t, err)
}

// ---- UpdateFolderDefaultSharing tests ----

func TestUpdateFolderDefaultSharing_OwnerAllows(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(f)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.UpdateFolderDefaultSharing(ctx, connect.NewRequest(&consolev1.UpdateFolderDefaultSharingRequest{
		Name: "eng",
		DefaultUserGrants: []*consolev1.ShareGrant{
			{Principal: "bob@example.com", Role: consolev1.Role_ROLE_EDITOR},
		},
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Folder.DefaultUserGrants) != 1 {
		t.Errorf("expected 1 default user grant, got %d", len(resp.Msg.Folder.DefaultUserGrants))
	}
}

// ---- GetFolderRaw tests ----

func TestGetFolderRaw_ReturnsNamespaceJSON(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	handler := newTestHandler(f)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.GetFolderRaw(ctx, connect.NewRequest(&consolev1.GetFolderRawRequest{Name: "eng"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Raw == "" {
		t.Error("expected non-empty raw JSON")
	}
}

func TestGetFolderRaw_DeniesUnauthorized(t *testing.T) {
	f := folderNSWithGrants("eng", "acme", "holos-org-acme", `[{"principal":"bob@example.com","role":"owner"}]`)
	handler := newTestHandler(f)
	ctx := contextWithClaims("nobody@example.com")

	_, err := handler.GetFolderRaw(ctx, connect.NewRequest(&consolev1.GetFolderRawRequest{Name: "eng"}))
	assertPermissionDenied(t, err)
}

// ---- buildFolder tests ----

func TestBuildFolder_PopulatesParentInfo(t *testing.T) {
	r := testResolver()
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, r)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-fld-eng",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelOrganization: "acme",
				v1alpha2.LabelFolder:       "eng",
				v1alpha2.AnnotationParent:  "holos-org-acme",
			},
		},
	}

	folder := buildFolder(k8s, ns, nil, nil, 0)
	if folder.Name != "eng" {
		t.Errorf("expected name 'eng', got %q", folder.Name)
	}
	if folder.Organization != "acme" {
		t.Errorf("expected organization 'acme', got %q", folder.Organization)
	}
	if folder.ParentType != consolev1.ParentType_PARENT_TYPE_ORGANIZATION {
		t.Errorf("expected PARENT_TYPE_ORGANIZATION, got %v", folder.ParentType)
	}
	if folder.ParentName != "acme" {
		t.Errorf("expected parent_name 'acme', got %q", folder.ParentName)
	}
}

func TestBuildFolder_FolderParentType(t *testing.T) {
	r := testResolver()
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, r)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-fld-sub",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelOrganization: "acme",
				v1alpha2.LabelFolder:       "sub",
				v1alpha2.AnnotationParent:  "holos-fld-eng",
			},
		},
	}

	folder := buildFolder(k8s, ns, nil, nil, 0)
	if folder.ParentType != consolev1.ParentType_PARENT_TYPE_FOLDER {
		t.Errorf("expected PARENT_TYPE_FOLDER, got %v", folder.ParentType)
	}
	if folder.ParentName != "eng" {
		t.Errorf("expected parent_name 'eng', got %q", folder.ParentName)
	}
}

// ---- mergeGrants tests ----

func TestMergeGrants_HigherRoleWins(t *testing.T) {
	base := []annotationGrant{
		{Principal: "alice@example.com", Role: "viewer"},
	}
	override := []annotationGrant{
		{Principal: "alice@example.com", Role: "editor"},
	}
	result := mergeGrants(base, override)
	if len(result) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(result))
	}
	if result[0].Role != "editor" {
		t.Errorf("expected editor (override wins), got %q", result[0].Role)
	}
}

func TestMergeGrants_PreservesBaseWhenOverrideIsLower(t *testing.T) {
	base := []annotationGrant{
		{Principal: "alice@example.com", Role: "owner"},
	}
	override := []annotationGrant{
		{Principal: "alice@example.com", Role: "viewer"},
	}
	result := mergeGrants(base, override)
	if len(result) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(result))
	}
	if result[0].Role != "owner" {
		t.Errorf("expected owner (base wins when higher), got %q", result[0].Role)
	}
}

// ---- CheckFolderIdentifier tests ----

func TestCheckFolderIdentifier_Available(t *testing.T) {
	// No folder namespace exists, so identifier should be available.
	handler := newTestHandler()
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CheckFolderIdentifier(ctx, connect.NewRequest(&consolev1.CheckFolderIdentifierRequest{
		Identifier: "engineering",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !resp.Msg.Available {
		t.Error("expected available=true")
	}
	if resp.Msg.SuggestedIdentifier != "engineering" {
		t.Errorf("expected suggested_identifier='engineering', got %q", resp.Msg.SuggestedIdentifier)
	}
}

func TestCheckFolderIdentifier_Taken(t *testing.T) {
	// Create an existing folder namespace that will collide.
	f := folderNSWithGrants("engineering", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(f)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CheckFolderIdentifier(ctx, connect.NewRequest(&consolev1.CheckFolderIdentifierRequest{
		Identifier: "engineering",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Available {
		t.Error("expected available=false")
	}
	if resp.Msg.SuggestedIdentifier == "engineering" {
		t.Error("expected suggested_identifier to differ from input")
	}
	// Should start with "engineering-"
	if len(resp.Msg.SuggestedIdentifier) < len("engineering-000000") {
		t.Errorf("expected suffixed identifier, got %q", resp.Msg.SuggestedIdentifier)
	}
}

func TestCheckFolderIdentifier_EmptyRejects(t *testing.T) {
	handler := newTestHandler()
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CheckFolderIdentifier(ctx, connect.NewRequest(&consolev1.CheckFolderIdentifierRequest{
		Identifier: "",
	}))
	assertInvalidArgument(t, err)
}

func TestCheckFolderIdentifier_Unauthenticated(t *testing.T) {
	handler := newTestHandler()
	_, err := handler.CheckFolderIdentifier(context.Background(), connect.NewRequest(&consolev1.CheckFolderIdentifierRequest{
		Identifier: "eng",
	}))
	assertUnauthenticated(t, err)
}

// ---- helpers ----

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

func assertFailedPrecondition(t *testing.T, err error) {
	t.Helper()
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

// annotationGrant is a local alias for merge tests, avoiding import naming conflicts.
type annotationGrant = secrpkg.AnnotationGrant
