package folders

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/secrets"
)

func testResolver() *resolver.Resolver {
	return &resolver.Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
}

func folderNS(name, org, parentNs string) *corev1.Namespace {
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
		},
	}
}

func TestGetFolder_ReturnsFolder(t *testing.T) {
	ns := folderNS("eng", "acme", "holos-org-acme")
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.GetFolder(context.Background(), "eng")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Name != "holos-fld-eng" {
		t.Errorf("expected holos-fld-eng, got %s", result.Name)
	}
}

func TestGetFolder_ReturnsNotFoundForMissing(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	_, err := k8s.GetFolder(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestGetFolder_RejectsNonFolder(t *testing.T) {
	// Namespace with org label instead of folder label
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-fld-fake",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	_, err := k8s.GetFolder(context.Background(), "fake")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListFolders_ReturnsAllFoldersForOrg(t *testing.T) {
	f1 := folderNS("eng", "acme", "holos-org-acme")
	f2 := folderNS("ops", "acme", "holos-org-acme")
	f3 := folderNS("other", "beta", "holos-org-beta")
	fakeClient := fake.NewClientset(f1, f2, f3)
	k8s := NewK8sClient(fakeClient, testResolver())

	results, err := k8s.ListFolders(context.Background(), "acme", "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 folders for acme, got %d", len(results))
	}
}

func TestListFolders_FiltersByParent(t *testing.T) {
	f1 := folderNS("eng", "acme", "holos-org-acme")
	f2 := folderNS("sub", "acme", "holos-fld-eng") // child of eng
	fakeClient := fake.NewClientset(f1, f2)
	k8s := NewK8sClient(fakeClient, testResolver())

	results, err := k8s.ListFolders(context.Background(), "acme", "holos-fld-eng")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(results))
	}
	if results[0].Name != "holos-fld-sub" {
		t.Errorf("expected holos-fld-sub, got %s", results[0].Name)
	}
}

func TestCreateFolder_CreatesNamespaceWithLabels(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	shareUsers := []secrets.AnnotationGrant{{Principal: "alice@example.com", Role: "owner"}}
	result, err := k8s.CreateFolder(context.Background(), "eng", "Engineering", "Eng team", "acme", "holos-org-acme", "alice@example.com", "", shareUsers, nil, nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Name != "holos-fld-eng" {
		t.Errorf("expected holos-fld-eng, got %s", result.Name)
	}
	if result.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeFolder {
		t.Error("expected resource-type=folder label")
	}
	if result.Labels[v1alpha2.LabelOrganization] != "acme" {
		t.Errorf("expected organization=acme, got %s", result.Labels[v1alpha2.LabelOrganization])
	}
	if result.Labels[v1alpha2.AnnotationParent] != "holos-org-acme" {
		t.Errorf("expected parent=holos-org-acme, got %s", result.Labels[v1alpha2.AnnotationParent])
	}
	if result.Labels[v1alpha2.LabelFolder] != "eng" {
		t.Errorf("expected folder=eng, got %s", result.Labels[v1alpha2.LabelFolder])
	}
	if result.Annotations[v1alpha2.AnnotationDisplayName] != "Engineering" {
		t.Errorf("expected display name 'Engineering', got %s", result.Annotations[v1alpha2.AnnotationDisplayName])
	}
	if result.Annotations[v1alpha2.AnnotationCreatorEmail] != "alice@example.com" {
		t.Errorf("expected creator email 'alice@example.com', got %s", result.Annotations[v1alpha2.AnnotationCreatorEmail])
	}
}

func TestUpdateFolder_UpdatesAnnotations(t *testing.T) {
	ns := folderNS("eng", "acme", "holos-org-acme")
	ns.Annotations = map[string]string{
		v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"owner"}]`,
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	displayName := "Engineering Updated"
	result, err := k8s.UpdateFolder(context.Background(), "eng", &displayName, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Annotations[v1alpha2.AnnotationDisplayName] != "Engineering Updated" {
		t.Errorf("expected 'Engineering Updated', got %q", result.Annotations[v1alpha2.AnnotationDisplayName])
	}
}

func TestDeleteFolder_DeletesNamespace(t *testing.T) {
	ns := folderNS("eng", "acme", "holos-org-acme")
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	err := k8s.DeleteFolder(context.Background(), "eng")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	_, err = fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-fld-eng", metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound after delete, got %v", err)
	}
}

func TestUpdateFolderSharing_UpdatesAnnotations(t *testing.T) {
	ns := folderNS("eng", "acme", "holos-org-acme")
	ns.Annotations = map[string]string{
		v1alpha2.AnnotationShareUsers: `[{"principal":"old@example.com","role":"viewer"}]`,
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	newUsers := []secrets.AnnotationGrant{{Principal: "alice@example.com", Role: "owner"}}
	result, err := k8s.UpdateFolderSharing(context.Background(), "eng", newUsers, nil, newUsers)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	users, err := GetShareUsers(result)
	if err != nil {
		t.Fatalf("failed to parse share-users: %v", err)
	}
	if len(users) != 1 || users[0].Principal != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %v", users)
	}
}

func TestUpdateFolderDefaultSharing_UpdatesAnnotations(t *testing.T) {
	ns := folderNS("eng", "acme", "holos-org-acme")
	ns.Annotations = map[string]string{
		v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"owner"}]`,
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	defaultUsers := []secrets.AnnotationGrant{{Principal: "bob@example.com", Role: "editor"}}
	result, err := k8s.UpdateFolderDefaultSharing(context.Background(), "eng", defaultUsers, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	users, err := GetDefaultShareUsers(result)
	if err != nil {
		t.Fatalf("failed to parse default-share-users: %v", err)
	}
	if len(users) != 1 || users[0].Principal != "bob@example.com" {
		t.Errorf("expected bob@example.com, got %v", users)
	}
}

func TestListChildFolders_ReturnsChildren(t *testing.T) {
	f1 := folderNS("eng", "acme", "holos-org-acme")
	f2 := folderNS("sub", "acme", "holos-fld-eng")
	f3 := folderNS("other", "acme", "holos-org-acme")
	fakeClient := fake.NewClientset(f1, f2, f3)
	k8s := NewK8sClient(fakeClient, testResolver())

	children, err := k8s.ListChildFolders(context.Background(), "holos-fld-eng")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(children) != 1 || children[0].Name != "holos-fld-sub" {
		t.Errorf("expected [holos-fld-sub], got %v", children)
	}
}

func TestNamespaceExists_ReturnsTrueForExisting(t *testing.T) {
	ns := folderNS("eng", "acme", "holos-org-acme")
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	exists, err := k8s.NamespaceExists(context.Background(), "holos-fld-eng")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !exists {
		t.Error("expected exists=true for existing namespace")
	}
}

func TestNamespaceExists_ReturnsFalseForMissing(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	exists, err := k8s.NamespaceExists(context.Background(), "holos-fld-missing")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if exists {
		t.Error("expected exists=false for missing namespace")
	}
}

func TestUpdateParentLabel_UpdatesLabel(t *testing.T) {
	ns := folderNS("rp-upd", "acme", "holos-org-acme")
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.UpdateParentLabel(context.Background(), "rp-upd", "holos-fld-new-parent")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Labels[v1alpha2.AnnotationParent] != "holos-fld-new-parent" {
		t.Errorf("expected parent label 'holos-fld-new-parent', got %q", result.Labels[v1alpha2.AnnotationParent])
	}

	// Verify persisted in the fake client.
	fetched, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-fld-rp-upd", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if fetched.Labels[v1alpha2.AnnotationParent] != "holos-fld-new-parent" {
		t.Errorf("expected persisted parent label 'holos-fld-new-parent', got %q", fetched.Labels[v1alpha2.AnnotationParent])
	}
}

func TestListChildProjects_ReturnsChildren(t *testing.T) {
	prj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-api",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelOrganization: "acme",
				v1alpha2.AnnotationParent:  "holos-fld-eng",
			},
		},
	}
	fakeClient := fake.NewClientset(prj)
	k8s := NewK8sClient(fakeClient, testResolver())

	children, err := k8s.ListChildProjects(context.Background(), "holos-fld-eng")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(children) != 1 || children[0].Name != "holos-prj-api" {
		t.Errorf("expected [holos-prj-api], got %v", children)
	}
}
