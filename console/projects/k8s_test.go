package projects

import (
	"context"
	"testing"

	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/secrets"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestListProjects_ReturnsOnlyProjectNamespaces(t *testing.T) {
	managed1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-project-a",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "project-a",
				resolver.OrganizationLabel: "acme",
			},
		},
	}
	managed2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-project-b",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "project-b",
				resolver.OrganizationLabel: "acme",
			},
		},
	}
	orgNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
			},
		},
	}
	unmanaged := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
		},
	}
	fakeClient := fake.NewClientset(managed1, managed2, orgNS, unmanaged)
	k8s := NewK8sClient(fakeClient, testResolver())

	projects, err := k8s.ListProjects(context.Background(), "")
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
	k8s := NewK8sClient(fakeClient, testResolver())

	projects, err := k8s.ListProjects(context.Background(), "")
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

func TestListProjects_ExcludesTerminatingNamespaces(t *testing.T) {
	now := metav1.Now()
	active := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-active",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "active",
			},
		},
	}
	terminating := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-terminating",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "terminating",
			},
			DeletionTimestamp: &now,
		},
	}
	fakeClient := fake.NewClientset(active, terminating)
	k8s := NewK8sClient(fakeClient, testResolver())

	projects, err := k8s.ListProjects(context.Background(), "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project (excluding terminating), got %d", len(projects))
	}
	if projects[0].Name != "holos-prj-active" {
		t.Errorf("expected active project, got %q", projects[0].Name)
	}
}

func TestListProjects_FilterByOrg(t *testing.T) {
	prj1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-foo",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "foo",
				resolver.OrganizationLabel: "acme",
			},
		},
	}
	prj2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-bar",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "bar",
				resolver.OrganizationLabel: "acme",
			},
		},
	}
	prj3 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-baz",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "baz",
				resolver.OrganizationLabel: "beta",
			},
		},
	}
	fakeClient := fake.NewClientset(prj1, prj2, prj3)
	k8s := NewK8sClient(fakeClient, testResolver())

	projects, err := k8s.ListProjects(context.Background(), "acme")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects filtered by org 'acme', got %d", len(projects))
	}
}

func TestGetProject_ReturnsByDerivedNamespace(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-my-project",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "my-project",
				resolver.OrganizationLabel: "acme",
			},
			Annotations: map[string]string{
				DisplayNameAnnotation:        "My Project",
				secrets.DescriptionAnnotation: "Test project",
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.GetProject(context.Background(), "my-project")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Name != "holos-prj-my-project" {
		t.Errorf("expected namespace 'holos-prj-my-project', got %q", result.Name)
	}
	if result.Annotations[DisplayNameAnnotation] != "My Project" {
		t.Errorf("expected display-name 'My Project', got %q", result.Annotations[DisplayNameAnnotation])
	}
}

func TestGetProject_ReturnsOrganization(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-my-project",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "my-project",
				resolver.OrganizationLabel: "acme",
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.GetProject(context.Background(), "my-project")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if GetOrganization(result) != "acme" {
		t.Errorf("expected organization 'acme', got %q", GetOrganization(result))
	}
}

func TestGetProject_ReturnsNotFoundForMissingNamespace(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	_, err := k8s.GetProject(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestGetProject_ReturnsErrorForUnmanagedNamespace(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-kube-system",
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	_, err := k8s.GetProject(context.Background(), "kube-system")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateProject_UsesPrefixNamespace(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	shareUsers := []secrets.AnnotationGrant{{Principal: "alice@example.com", Role: "owner"}}
	shareRoles := []secrets.AnnotationGrant{{Principal: "engineering", Role: "editor"}}

	result, err := k8s.CreateProject(context.Background(), "new-project", "New Project", "A test project", "acme", shareUsers, shareRoles)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Name != "holos-prj-new-project" {
		t.Errorf("expected namespace 'holos-prj-new-project', got %q", result.Name)
	}
	if result.Labels[secrets.ManagedByLabel] != secrets.ManagedByValue {
		t.Errorf("expected managed-by label, got %v", result.Labels)
	}
	if result.Labels[resolver.ResourceTypeLabel] != resolver.ResourceTypeProject {
		t.Error("expected resource-type=project label")
	}
	if result.Labels[resolver.ProjectLabel] != "new-project" {
		t.Errorf("expected project label 'new-project', got %q", result.Labels[resolver.ProjectLabel])
	}
	if result.Annotations[DisplayNameAnnotation] != "New Project" {
		t.Errorf("expected display-name 'New Project', got %q", result.Annotations[DisplayNameAnnotation])
	}
}

func TestCreateProject_SetsOrgLabelWhenProvided(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.CreateProject(context.Background(), "foo", "", "", "acme", nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Labels[resolver.OrganizationLabel] != "acme" {
		t.Errorf("expected organization label 'acme', got %q", result.Labels[resolver.OrganizationLabel])
	}
}

func TestCreateProject_OmitsOrgLabelWhenEmpty(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.CreateProject(context.Background(), "foo", "", "", "", nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, ok := result.Labels[resolver.OrganizationLabel]; ok {
		t.Error("expected no organization label when org is empty")
	}
}

func TestCreateProject_ReturnsAlreadyExistsForDuplicateName(t *testing.T) {
	existing := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-existing",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "existing",
			},
		},
	}
	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, testResolver())

	_, err := k8s.CreateProject(context.Background(), "existing", "", "", "", nil, nil)
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
			Name: "holos-prj-my-project",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "my-project",
			},
			Annotations: map[string]string{
				secrets.ShareUsersAnnotation: `[{"principal":"alice@example.com","role":"owner"}]`,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

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
}

func TestUpdateProject_RejectsUnmanagedNamespace(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	desc := "test"
	_, err := k8s.UpdateProject(context.Background(), "kube-system", nil, &desc)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestDeleteProject_DeletesManagedNamespace(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-my-project",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "my-project",
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	err := k8s.DeleteProject(context.Background(), "my-project")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	_, err = fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-prj-my-project", metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound after delete, got %v", err)
	}
}

func TestDeleteProject_RejectsUnmanagedNamespace(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	err := k8s.DeleteProject(context.Background(), "kube-system")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListProjects_FiltersPrefixMismatchNamespaces(t *testing.T) {
	// A namespace with correct labels but wrong prefix (from another console instance)
	// should be filtered out of results.
	matching := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-project-a",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "project-a",
			},
		},
	}
	mismatched := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "other-prj-project-b",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "project-b",
			},
		},
	}
	fakeClient := fake.NewClientset(matching, mismatched)
	k8s := NewK8sClient(fakeClient, testResolver())

	projects, err := k8s.ListProjects(context.Background(), "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project (prefix mismatch filtered), got %d", len(projects))
	}
	if projects[0].Name != "holos-prj-project-a" {
		t.Errorf("expected holos-prj-project-a, got %s", projects[0].Name)
	}
}

func TestUpdateProjectSharing_UpdatesShareAnnotations(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-my-project",
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "my-project",
			},
			Annotations: map[string]string{
				secrets.ShareUsersAnnotation:  `[{"principal":"old@example.com","role":"viewer"}]`,
				secrets.ShareRolesAnnotation: `[]`,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

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
}
