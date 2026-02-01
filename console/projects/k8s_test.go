package projects

import (
	"context"
	"strings"
	"testing"

	"github.com/holos-run/holos-console/console/secrets"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestListProjects_ReturnsOnlyManagedNamespaces(t *testing.T) {
	managed1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "project-a",
			Labels: map[string]string{secrets.ManagedByLabel: secrets.ManagedByValue},
		},
	}
	managed2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "project-b",
			Labels: map[string]string{secrets.ManagedByLabel: secrets.ManagedByValue},
		},
	}
	unmanaged := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
		},
	}
	fakeClient := fake.NewClientset(managed1, managed2, unmanaged)
	k8s := NewK8sClient(fakeClient)

	projects, err := k8s.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}

func TestListProjects_ReturnsEmptyListWhenNoManagedNamespaces(t *testing.T) {
	unmanaged := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
		},
	}
	fakeClient := fake.NewClientset(unmanaged)
	k8s := NewK8sClient(fakeClient)

	projects, err := k8s.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if projects == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(projects) != 0 {
		t.Fatalf("expected 0 projects, got %d", len(projects))
	}
}

func TestGetProject_ReturnsNamespaceByName(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-project",
			Labels: map[string]string{secrets.ManagedByLabel: secrets.ManagedByValue},
			Annotations: map[string]string{
				DisplayNameAnnotation:        "My Project",
				secrets.DescriptionAnnotation: "Test project",
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient)

	result, err := k8s.GetProject(context.Background(), "my-project")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Name != "my-project" {
		t.Errorf("expected name 'my-project', got %q", result.Name)
	}
	if result.Labels[secrets.ManagedByLabel] != secrets.ManagedByValue {
		t.Errorf("expected managed-by label, got %v", result.Labels)
	}
	if result.Annotations[DisplayNameAnnotation] != "My Project" {
		t.Errorf("expected display-name 'My Project', got %q", result.Annotations[DisplayNameAnnotation])
	}
}

func TestGetProject_ReturnsNotFoundForMissingNamespace(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient)

	_, err := k8s.GetProject(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestGetProject_ReturnsNotFoundForUnmanagedNamespace(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient)

	_, err := k8s.GetProject(context.Background(), "kube-system")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not managed by") {
		t.Errorf("expected 'not managed by' error, got %v", err)
	}
}

func TestCreateProject_CreatesNamespaceWithLabelAndAnnotations(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient)

	shareUsers := []secrets.AnnotationGrant{{Principal: "alice@example.com", Role: "owner"}}
	shareGroups := []secrets.AnnotationGrant{{Principal: "engineering", Role: "editor"}}

	result, err := k8s.CreateProject(context.Background(), "new-project", "New Project", "A test project", shareUsers, shareGroups)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Name != "new-project" {
		t.Errorf("expected name 'new-project', got %q", result.Name)
	}
	if result.Labels[secrets.ManagedByLabel] != secrets.ManagedByValue {
		t.Errorf("expected managed-by label, got %v", result.Labels)
	}
	if result.Annotations[DisplayNameAnnotation] != "New Project" {
		t.Errorf("expected display-name 'New Project', got %q", result.Annotations[DisplayNameAnnotation])
	}
	if result.Annotations[secrets.DescriptionAnnotation] != "A test project" {
		t.Errorf("expected description 'A test project', got %q", result.Annotations[secrets.DescriptionAnnotation])
	}
	users, err := GetShareUsers(result)
	if err != nil {
		t.Fatalf("failed to parse share-users: %v", err)
	}
	if len(users) != 1 || users[0].Principal != "alice@example.com" || users[0].Role != "owner" {
		t.Errorf("expected [{alice@example.com owner}], got %v", users)
	}
	groups, err := GetShareGroups(result)
	if err != nil {
		t.Fatalf("failed to parse share-groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Principal != "engineering" || groups[0].Role != "editor" {
		t.Errorf("expected [{engineering editor}], got %v", groups)
	}
}

func TestCreateProject_AutoAddsCreatorAsOwner(t *testing.T) {
	// This test verifies the K8s layer stores the grants as-is.
	// The auto-add logic is in the handler layer.
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient)

	shareUsers := []secrets.AnnotationGrant{
		{Principal: "alice@example.com", Role: "owner"},
		{Principal: "bob@example.com", Role: "editor"},
	}

	result, err := k8s.CreateProject(context.Background(), "proj", "", "", shareUsers, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	users, err := GetShareUsers(result)
	if err != nil {
		t.Fatalf("failed to parse share-users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 user grants, got %d", len(users))
	}
}

func TestCreateProject_ReturnsAlreadyExistsForDuplicateName(t *testing.T) {
	existing := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "existing",
			Labels: map[string]string{secrets.ManagedByLabel: secrets.ManagedByValue},
		},
	}
	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient)

	_, err := k8s.CreateProject(context.Background(), "existing", "", "", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsAlreadyExists(err) {
		t.Errorf("expected AlreadyExists error, got %v", err)
	}
}

func TestUpdateProject_UpdatesDescriptionAndDisplayName(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-project",
			Labels: map[string]string{secrets.ManagedByLabel: secrets.ManagedByValue},
			Annotations: map[string]string{
				secrets.ShareUsersAnnotation: `[{"principal":"alice@example.com","role":"owner"}]`,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient)

	desc := "Updated desc"
	displayName := "Updated Name"
	result, err := k8s.UpdateProject(context.Background(), "my-project", &displayName, &desc)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if GetDescription(result) != "Updated desc" {
		t.Errorf("expected description 'Updated desc', got %q", GetDescription(result))
	}
	if GetDisplayName(result) != "Updated Name" {
		t.Errorf("expected display-name 'Updated Name', got %q", GetDisplayName(result))
	}
	// Verify share-users preserved
	if result.Annotations[secrets.ShareUsersAnnotation] != `[{"principal":"alice@example.com","role":"owner"}]` {
		t.Errorf("expected share-users preserved, got %q", result.Annotations[secrets.ShareUsersAnnotation])
	}
}

func TestUpdateProject_RejectsUnmanagedNamespace(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient)

	desc := "test"
	_, err := k8s.UpdateProject(context.Background(), "kube-system", nil, &desc)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not managed by") {
		t.Errorf("expected 'not managed by' error, got %v", err)
	}
}

func TestUpdateProject_PreservesExistingAnnotationsWhenFieldNil(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-project",
			Labels: map[string]string{secrets.ManagedByLabel: secrets.ManagedByValue},
			Annotations: map[string]string{
				secrets.DescriptionAnnotation: "Original desc",
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient)

	displayName := "New Display Name"
	result, err := k8s.UpdateProject(context.Background(), "my-project", &displayName, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if GetDescription(result) != "Original desc" {
		t.Errorf("expected description 'Original desc', got %q", GetDescription(result))
	}
	if GetDisplayName(result) != "New Display Name" {
		t.Errorf("expected display-name 'New Display Name', got %q", GetDisplayName(result))
	}
}

func TestDeleteProject_DeletesManagedNamespace(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-project",
			Labels: map[string]string{secrets.ManagedByLabel: secrets.ManagedByValue},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient)

	err := k8s.DeleteProject(context.Background(), "my-project")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify namespace is gone
	_, err = fakeClient.CoreV1().Namespaces().Get(context.Background(), "my-project", metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound after delete, got %v", err)
	}
}

func TestDeleteProject_RejectsUnmanagedNamespace(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient)

	err := k8s.DeleteProject(context.Background(), "kube-system")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not managed by") {
		t.Errorf("expected 'not managed by' error, got %v", err)
	}
}

func TestUpdateProjectSharing_UpdatesShareAnnotations(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-project",
			Labels: map[string]string{secrets.ManagedByLabel: secrets.ManagedByValue},
			Annotations: map[string]string{
				secrets.ShareUsersAnnotation:  `[{"principal":"old@example.com","role":"viewer"}]`,
				secrets.ShareGroupsAnnotation: `[]`,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient)

	newUsers := []secrets.AnnotationGrant{
		{Principal: "alice@example.com", Role: "owner"},
		{Principal: "bob@example.com", Role: "editor"},
	}
	newGroups := []secrets.AnnotationGrant{
		{Principal: "engineering", Role: "viewer"},
	}

	result, err := k8s.UpdateProjectSharing(context.Background(), "my-project", newUsers, newGroups)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	users, err := GetShareUsers(result)
	if err != nil {
		t.Fatalf("failed to parse share-users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 user grants, got %d", len(users))
	}
	if users[0].Principal != "alice@example.com" || users[0].Role != "owner" {
		t.Errorf("expected alice=owner, got %s=%s", users[0].Principal, users[0].Role)
	}
	groups, err := GetShareGroups(result)
	if err != nil {
		t.Fatalf("failed to parse share-groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Principal != "engineering" {
		t.Errorf("expected [{engineering viewer}], got %v", groups)
	}
}

func TestUpdateProjectSharing_RejectsUnmanagedNamespace(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient)

	_, err := k8s.UpdateProjectSharing(context.Background(), "kube-system", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not managed by") {
		t.Errorf("expected 'not managed by' error, got %v", err)
	}
}
