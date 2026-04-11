package organizations

import (
	"context"
	"strings"
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
	return &resolver.Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
}

func TestListOrganizations_ReturnsOnlyOrgNamespaces(t *testing.T) {
	orgNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
			},
		},
	}
	projectNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-foo",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      "foo",
				resolver.OrganizationLabel: "acme",
			},
		},
	}
	unmanagedNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
		},
	}
	fakeClient := fake.NewClientset(orgNS, projectNS, unmanagedNS)
	k8s := NewK8sClient(fakeClient, testResolver())

	orgs, err := k8s.ListOrganizations(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(orgs) != 1 {
		t.Fatalf("expected 1 org, got %d", len(orgs))
	}
	if orgs[0].Name != "holos-org-acme" {
		t.Errorf("expected org-acme, got %s", orgs[0].Name)
	}
}

func TestListOrganizations_ExcludesTerminatingNamespaces(t *testing.T) {
	now := metav1.Now()
	active := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-active",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
				resolver.OrganizationLabel: "active",
			},
		},
	}
	terminating := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-terminating",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
				resolver.OrganizationLabel: "terminating",
			},
			DeletionTimestamp: &now,
		},
	}
	fakeClient := fake.NewClientset(active, terminating)
	k8s := NewK8sClient(fakeClient, testResolver())

	orgs, err := k8s.ListOrganizations(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(orgs) != 1 {
		t.Fatalf("expected 1 org (excluding terminating), got %d", len(orgs))
	}
	if orgs[0].Name != "holos-org-active" {
		t.Errorf("expected org-active, got %s", orgs[0].Name)
	}
}

func TestListOrganizations_EmptyList(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	orgs, err := k8s.ListOrganizations(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(orgs) != 0 {
		t.Fatalf("expected 0 orgs, got %d", len(orgs))
	}
}

func TestGetOrganization_ReturnsOrgByName(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: "ACME Corp",
				v1alpha2.AnnotationDescription: "Test org",
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.GetOrganization(context.Background(), "acme")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Name != "holos-org-acme" {
		t.Errorf("expected namespace org-acme, got %s", result.Name)
	}
	if result.Annotations[v1alpha2.AnnotationDisplayName] != "ACME Corp" {
		t.Errorf("expected display name ACME Corp, got %s", result.Annotations[v1alpha2.AnnotationDisplayName])
	}
}

func TestGetOrganization_ReturnsNotFoundForMissing(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	_, err := k8s.GetOrganization(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestGetOrganization_RejectsNonOrg(t *testing.T) {
	// Namespace exists but has project resource-type label
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-fake",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	_, err := k8s.GetOrganization(context.Background(), "fake")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not an organization") {
		t.Errorf("expected 'not an organization' error, got %v", err)
	}
}

func TestCreateOrganization_CreatesNamespaceWithPrefixAndLabels(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	shareUsers := []secrets.AnnotationGrant{{Principal: "alice@example.com", Role: "owner"}}
	result, err := k8s.CreateOrganization(context.Background(), "acme", "ACME Corp", "Test org", "", shareUsers, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Name != "holos-org-acme" {
		t.Errorf("expected org-acme, got %s", result.Name)
	}
	if result.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
		t.Error("expected managed-by label")
	}
	if result.Labels[resolver.ResourceTypeLabel] != resolver.ResourceTypeOrganization {
		t.Error("expected resource-type=organization label")
	}
	if result.Annotations[v1alpha2.AnnotationDisplayName] != "ACME Corp" {
		t.Errorf("expected display name ACME Corp, got %s", result.Annotations[v1alpha2.AnnotationDisplayName])
	}
	users, err := GetShareUsers(result)
	if err != nil {
		t.Fatalf("failed to parse share-users: %v", err)
	}
	if len(users) != 1 || users[0].Principal != "alice@example.com" || users[0].Role != "owner" {
		t.Errorf("expected [{alice@example.com owner}], got %v", users)
	}
}

func TestCreateOrganization_SetsOrganizationLabel(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.CreateOrganization(context.Background(), "acme", "", "", "", nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Labels[resolver.OrganizationLabel] != "acme" {
		t.Errorf("expected organization label 'acme', got %q", result.Labels[resolver.OrganizationLabel])
	}
}

func TestCreateOrganization_ReturnsAlreadyExists(t *testing.T) {
	existing := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
			},
		},
	}
	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, testResolver())

	_, err := k8s.CreateOrganization(context.Background(), "acme", "", "", "", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsAlreadyExists(err) {
		t.Errorf("expected AlreadyExists, got %v", err)
	}
}

func TestUpdateOrganization_UpdatesAnnotations(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"owner"}]`,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	displayName := "Updated Name"
	desc := "Updated desc"
	result, err := k8s.UpdateOrganization(context.Background(), "acme", &displayName, &desc)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Annotations[v1alpha2.AnnotationDisplayName] != "Updated Name" {
		t.Errorf("expected 'Updated Name', got %q", result.Annotations[v1alpha2.AnnotationDisplayName])
	}
	if result.Annotations[v1alpha2.AnnotationDescription] != "Updated desc" {
		t.Errorf("expected 'Updated desc', got %q", result.Annotations[v1alpha2.AnnotationDescription])
	}
	// Verify share-users preserved
	if result.Annotations[v1alpha2.AnnotationShareUsers] != `[{"principal":"alice@example.com","role":"owner"}]` {
		t.Errorf("expected share-users preserved")
	}
}

func TestUpdateOrganization_RejectsUnmanaged(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-fake",
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	desc := "test"
	_, err := k8s.UpdateOrganization(context.Background(), "fake", nil, &desc)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDeleteOrganization_DeletesOrgNamespace(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	err := k8s.DeleteOrganization(context.Background(), "acme")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	_, err = fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-org-acme", metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound after delete, got %v", err)
	}
}

func TestDeleteOrganization_RejectsNonOrg(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-fake",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	err := k8s.DeleteOrganization(context.Background(), "fake")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpdateOrgSharing_UpdatesAnnotations(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
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
	result, err := k8s.UpdateOrganizationSharing(context.Background(), "acme", newUsers, newGroups)
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
	groups, err := GetShareRoles(result)
	if err != nil {
		t.Fatalf("failed to parse share-roles: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 role grant, got %d", len(groups))
	}
}

func TestListOrganizations_FiltersPrefixMismatchNamespaces(t *testing.T) {
	// A namespace with correct labels but wrong prefix (from another console instance)
	// should be filtered out of results.
	matching := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
				resolver.OrganizationLabel: "acme",
			},
		},
	}
	mismatched := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "other-org-beta",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
				resolver.OrganizationLabel: "beta",
			},
		},
	}
	fakeClient := fake.NewClientset(matching, mismatched)
	k8s := NewK8sClient(fakeClient, testResolver())

	orgs, err := k8s.ListOrganizations(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(orgs) != 1 {
		t.Fatalf("expected 1 org (prefix mismatch filtered), got %d", len(orgs))
	}
	if orgs[0].Name != "holos-org-acme" {
		t.Errorf("expected holos-org-acme, got %s", orgs[0].Name)
	}
}

func TestGetDefaultShareUsers_ParsesAnnotation(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Annotations: map[string]string{
				v1alpha2.AnnotationDefaultShareUsers: `[{"principal":"alice@example.com","role":"editor"}]`,
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
	if grants[0].Principal != "alice@example.com" || grants[0].Role != "editor" {
		t.Errorf("unexpected grant: %+v", grants[0])
	}
}

func TestGetDefaultShareRoles_ParsesAnnotation(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Annotations: map[string]string{
				v1alpha2.AnnotationDefaultShareRoles: `[{"principal":"engineering","role":"viewer"}]`,
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
	if grants[0].Principal != "engineering" || grants[0].Role != "viewer" {
		t.Errorf("unexpected grant: %+v", grants[0])
	}
}

func TestGetDefaultShareUsers_ReturnsNilWhenAbsent(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
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

func TestUpdateOrgDefaultSharing_UpdatesAnnotations(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"owner"}]`,
			},
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	defaultUsers := []secrets.AnnotationGrant{
		{Principal: "bob@example.com", Role: "editor"},
	}
	defaultRoles := []secrets.AnnotationGrant{
		{Principal: "engineering", Role: "viewer"},
	}
	result, err := k8s.UpdateOrganizationDefaultSharing(context.Background(), "acme", defaultUsers, defaultRoles)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	users, err := GetDefaultShareUsers(result)
	if err != nil {
		t.Fatalf("failed to parse default-share-users: %v", err)
	}
	if len(users) != 1 || users[0].Principal != "bob@example.com" {
		t.Errorf("expected [{bob@example.com editor}], got %v", users)
	}
	roles, err := GetDefaultShareRoles(result)
	if err != nil {
		t.Fatalf("failed to parse default-share-roles: %v", err)
	}
	if len(roles) != 1 || roles[0].Principal != "engineering" {
		t.Errorf("expected [{engineering viewer}], got %v", roles)
	}
	// Verify existing share-users preserved
	if result.Annotations[v1alpha2.AnnotationShareUsers] != `[{"principal":"alice@example.com","role":"owner"}]` {
		t.Error("expected share-users annotation preserved")
	}
}

func TestUpdateOrgDefaultSharing_RejectsNonOrg(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-fake",
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	_, err := k8s.UpdateOrganizationDefaultSharing(context.Background(), "fake", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpdateOrgSharing_RejectsNonOrg(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-fake",
		},
	}
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())

	_, err := k8s.UpdateOrganizationSharing(context.Background(), "fake", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateOrganization_StoresCreatorEmailAnnotation(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.CreateOrganization(context.Background(), "acme", "", "", "creator@example.com", nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Annotations[v1alpha2.AnnotationCreatorEmail] != "creator@example.com" {
		t.Errorf("expected creator-email annotation %q, got %q", "creator@example.com", result.Annotations[v1alpha2.AnnotationCreatorEmail])
	}
}

func TestCreateOrganization_EmptyCreatorEmail_NoAnnotation(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())

	result, err := k8s.CreateOrganization(context.Background(), "acme", "", "", "", nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, ok := result.Annotations[v1alpha2.AnnotationCreatorEmail]; ok {
		t.Error("expected no creator-email annotation when email is empty")
	}
}

func TestBuildOrganization_PopulatesCreatorEmailAndCreatedAt(t *testing.T) {
	now := metav1.Now()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "holos-org-acme",
			CreationTimestamp: now,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
				resolver.OrganizationLabel: "acme",
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
	org := buildOrganization(k8s, ns, shareUsers, shareRoles, 0)

	if org.CreatorEmail != "creator@example.com" {
		t.Errorf("expected CreatorEmail %q, got %q", "creator@example.com", org.CreatorEmail)
	}
	if org.CreatedAt == "" {
		t.Error("expected CreatedAt to be non-empty")
	}
	expectedTime := now.UTC().Format("2006-01-02T15:04:05Z07:00")
	if org.CreatedAt != expectedTime {
		t.Errorf("expected CreatedAt %q, got %q", expectedTime, org.CreatedAt)
	}
}

func TestBuildOrganization_NoAnnotation_EmptyCreatorEmail(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-acme",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
				resolver.OrganizationLabel: "acme",
			},
		},
	}

	k8s := NewK8sClient(fake.NewClientset(ns), testResolver())
	org := buildOrganization(k8s, ns, nil, nil, 0)

	if org.CreatorEmail != "" {
		t.Errorf("expected empty CreatorEmail for namespace without annotation, got %q", org.CreatorEmail)
	}
}
