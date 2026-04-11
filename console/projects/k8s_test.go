package projects

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/secrets"
)

func TestListProjects_ReturnsOnlyProjectNamespaces(t *testing.T) {
	managed1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-project-a",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "project-a",
				v1alpha2.LabelOrganization: "acme",
			},
		},
	}
	managed2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-project-b",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "project-b",
				v1alpha2.LabelOrganization: "acme",
			},
		},
	}
	orgNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
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

	projects, err := k8s.ListProjects(context.Background(), "", "")
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

	projects, err := k8s.ListProjects(context.Background(), "", "")
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
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "active",
			},
		},
	}
	terminating := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-terminating",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "terminating",
			},
			DeletionTimestamp: &now,
		},
	}
	fakeClient := fake.NewClientset(active, terminating)
	k8s := NewK8sClient(fakeClient, testResolver())

	projects, err := k8s.ListProjects(context.Background(), "", "")
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
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "foo",
				v1alpha2.LabelOrganization: "acme",
			},
		},
	}
	prj2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-bar",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "bar",
				v1alpha2.LabelOrganization: "acme",
			},
		},
	}
	prj3 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-baz",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "baz",
				v1alpha2.LabelOrganization: "beta",
			},
		},
	}
	fakeClient := fake.NewClientset(prj1, prj2, prj3)
	k8s := NewK8sClient(fakeClient, testResolver())

	projects, err := k8s.ListProjects(context.Background(), "acme", "")
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
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "my-project",
				v1alpha2.LabelOrganization: "acme",
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: "My Project",
				v1alpha2.AnnotationDescription: "Test project",
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
	if result.Annotations[v1alpha2.AnnotationDisplayName] != "My Project" {
		t.Errorf("expected display-name 'My Project', got %q", result.Annotations[v1alpha2.AnnotationDisplayName])
	}
}

func TestGetProject_ReturnsOrganization(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-my-project",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "my-project",
				v1alpha2.LabelOrganization: "acme",
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

	result, err := k8s.CreateProject(context.Background(), "new-project", "New Project", "A test project", "acme", "", "", shareUsers, shareRoles, nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Name != "holos-prj-new-project" {
		t.Errorf("expected namespace 'holos-prj-new-project', got %q", result.Name)
	}
	if result.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
		t.Errorf("expected managed-by label, got %v", result.Labels)
	}
	if result.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeProject {
		t.Error("expected resource-type=project label")
	}
	if result.Labels[v1alpha2.LabelProject] != "new-project" {
		t.Errorf("expected project label 'new-project', got %q", result.Labels[v1alpha2.LabelProject])
	}
	if result.Annotations[v1alpha2.AnnotationDisplayName] != "New Project" {
		t.Errorf("expected display-name 'New Project', got %q", result.Annotations[v1alpha2.AnnotationDisplayName])
	}
}

func TestCreateProject_SetsOrgLabelWhenProvided(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.CreateProject(context.Background(), "foo", "", "", "acme", "", "", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Labels[v1alpha2.LabelOrganization] != "acme" {
		t.Errorf("expected organization label 'acme', got %q", result.Labels[v1alpha2.LabelOrganization])
	}
}

func TestCreateProject_OmitsOrgLabelWhenEmpty(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.CreateProject(context.Background(), "foo", "", "", "", "", "", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, ok := result.Labels[v1alpha2.LabelOrganization]; ok {
		t.Error("expected no organization label when org is empty")
	}
}

func TestCreateProject_ReturnsAlreadyExistsForDuplicateName(t *testing.T) {
	existing := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-existing",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "existing",
			},
		},
	}
	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, testResolver())

	_, err := k8s.CreateProject(context.Background(), "existing", "", "", "", "", "", nil, nil, nil, nil)
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
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "my-project",
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"owner"}]`,
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
	if result.Annotations[v1alpha2.AnnotationDescription] != "Updated desc" {
		t.Errorf("expected description 'Updated desc', got %q", result.Annotations[v1alpha2.AnnotationDescription])
	}
	if result.Annotations[v1alpha2.AnnotationDisplayName] != "Updated Name" {
		t.Errorf("expected display-name 'Updated Name', got %q", result.Annotations[v1alpha2.AnnotationDisplayName])
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
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "my-project",
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
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "project-a",
			},
		},
	}
	mismatched := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "other-prj-project-b",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "project-b",
			},
		},
	}
	fakeClient := fake.NewClientset(matching, mismatched)
	k8s := NewK8sClient(fakeClient, testResolver())

	projects, err := k8s.ListProjects(context.Background(), "", "")
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

func TestGetDefaultShareUsers_ParsesAnnotation(t *testing.T) {
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
			},
		},
	}
	grants, err := GetDefaultShareUsers(ns)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	if grants[0].Principal != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %q", grants[0].Principal)
	}
}

func TestGetDefaultShareUsers_ReturnsNilWhenAbsent(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-my-project",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "my-project",
			},
		},
	}
	grants, err := GetDefaultShareUsers(ns)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if grants != nil {
		t.Errorf("expected nil, got %v", grants)
	}
}

func TestGetDefaultShareRoles_ParsesAnnotation(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-my-project",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "my-project",
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDefaultShareRoles: `[{"principal":"engineering","role":"editor"}]`,
			},
		},
	}
	grants, err := GetDefaultShareRoles(ns)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	if grants[0].Principal != "engineering" {
		t.Errorf("expected engineering, got %q", grants[0].Principal)
	}
}

func TestUpdateProjectDefaultSharing_PersistsAnnotations(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-my-project",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "my-project",
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	defaultUsers := []secrets.AnnotationGrant{{Principal: "alice@example.com", Role: "viewer"}}
	defaultRoles := []secrets.AnnotationGrant{{Principal: "engineering", Role: "editor"}}

	result, err := k8s.UpdateProjectDefaultSharing(context.Background(), "my-project", defaultUsers, defaultRoles)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	gotUsers, err := GetDefaultShareUsers(result)
	if err != nil {
		t.Fatalf("failed to parse default share-users: %v", err)
	}
	if len(gotUsers) != 1 || gotUsers[0].Principal != "alice@example.com" {
		t.Errorf("expected alice@example.com in default share-users, got %v", gotUsers)
	}
	gotRoles, err := GetDefaultShareRoles(result)
	if err != nil {
		t.Fatalf("failed to parse default share-roles: %v", err)
	}
	if len(gotRoles) != 1 || gotRoles[0].Principal != "engineering" {
		t.Errorf("expected engineering in default share-roles, got %v", gotRoles)
	}
}

func TestUpdateProjectDefaultSharing_RejectsUnmanagedNamespace(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	_, err := k8s.UpdateProjectDefaultSharing(context.Background(), "nonexistent", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestUpdateProjectSharing_UpdatesShareAnnotations(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-my-project",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "my-project",
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationShareUsers: `[{"principal":"old@example.com","role":"viewer"}]`,
				v1alpha2.AnnotationShareRoles: `[]`,
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

func TestCreateProject_StoresCreatorEmailAnnotation(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.CreateProject(context.Background(), "my-project", "", "", "acme", "", "creator@example.com", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Annotations[v1alpha2.AnnotationCreatorEmail] != "creator@example.com" {
		t.Errorf("expected creator-email annotation %q, got %q", "creator@example.com", result.Annotations[v1alpha2.AnnotationCreatorEmail])
	}
}

func TestCreateProject_EmptyCreatorEmail_NoAnnotation(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.CreateProject(context.Background(), "my-project", "", "", "acme", "", "", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, ok := result.Annotations[v1alpha2.AnnotationCreatorEmail]; ok {
		t.Error("expected no creator-email annotation when email is empty")
	}
}

func TestBuildProject_PopulatesCreatorEmailAndCreatedAt(t *testing.T) {
	now := metav1.Now()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "holos-prj-my-project",
			CreationTimestamp: now,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "my-project",
				v1alpha2.LabelOrganization: "acme",
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationCreatorEmail: "creator@example.com",
				v1alpha2.AnnotationShareUsers:   `[{"principal":"creator@example.com","role":"owner"}]`,
				v1alpha2.AnnotationShareRoles:   `[]`,
			},
		},
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	k8s := NewK8sClient(fake.NewClientset(ns), testResolver())
	h := &Handler{k8s: k8s}
	project := h.buildProject(ns, shareUsers, shareRoles, 0)

	if project.CreatorEmail != "creator@example.com" {
		t.Errorf("expected CreatorEmail %q, got %q", "creator@example.com", project.CreatorEmail)
	}
	if project.CreatedAt == "" {
		t.Error("expected CreatedAt to be non-empty")
	}
	expectedTime := now.UTC().Format("2006-01-02T15:04:05Z07:00")
	if project.CreatedAt != expectedTime {
		t.Errorf("expected CreatedAt %q, got %q", expectedTime, project.CreatedAt)
	}
}

func TestBuildProject_NoAnnotation_EmptyCreatorEmail(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-my-project",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "my-project",
			},
		},
	}

	k8s := NewK8sClient(fake.NewClientset(ns), testResolver())
	h := &Handler{k8s: k8s}
	project := h.buildProject(ns, nil, nil, 0)

	if project.CreatorEmail != "" {
		t.Errorf("expected empty CreatorEmail for namespace without annotation, got %q", project.CreatorEmail)
	}
}

func TestNamespaceExists_ReturnsTrueForExisting(t *testing.T) {
	ns := managedNS("frontend", "")
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	exists, err := k8s.NamespaceExists(context.Background(), "holos-prj-frontend")
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

	exists, err := k8s.NamespaceExists(context.Background(), "holos-prj-missing")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if exists {
		t.Error("expected exists=false for missing namespace")
	}
}
