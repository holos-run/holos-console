package projects

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/secrets"
)

// resolverTestResolver returns a Resolver with the full prefix set.
func resolverTestResolver() *resolver.Resolver {
	return &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
}

// grantsJSON marshals a slice of grants to JSON for use in annotations.
func grantsJSON(t *testing.T, grants []secrets.AnnotationGrant) string {
	t.Helper()
	b, err := json.Marshal(grants)
	if err != nil {
		t.Fatalf("marshaling grants: %v", err)
	}
	return string(b)
}

// managedProjectNS returns a project namespace fixture with the parent label set.
func managedProjectNS(name, org, parentNs string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-" + name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      name,
				v1alpha2.LabelOrganization: org,
				v1alpha2.AnnotationParent:  parentNs,
			},
		},
	}
}

// orgNSWithDefaults returns an org namespace fixture with default share annotations.
func orgNSWithDefaults(name string, defaultUsers, defaultRoles []secrets.AnnotationGrant, t *testing.T) *corev1.Namespace {
	t.Helper()
	annotations := map[string]string{}
	if len(defaultUsers) > 0 {
		annotations[v1alpha2.AnnotationDefaultShareUsers] = grantsJSON(t, defaultUsers)
	}
	if len(defaultRoles) > 0 {
		annotations[v1alpha2.AnnotationDefaultShareRoles] = grantsJSON(t, defaultRoles)
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

// folderNSWithDefaults returns a folder namespace fixture with default share annotations.
func folderNSWithDefaults(name, org, parentNs string, defaultUsers, defaultRoles []secrets.AnnotationGrant, t *testing.T) *corev1.Namespace {
	t.Helper()
	annotations := map[string]string{}
	if len(defaultUsers) > 0 {
		annotations[v1alpha2.AnnotationDefaultShareUsers] = grantsJSON(t, defaultUsers)
	}
	if len(defaultRoles) > 0 {
		annotations[v1alpha2.AnnotationDefaultShareRoles] = grantsJSON(t, defaultRoles)
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-fld-" + name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelFolder:       name,
				v1alpha2.LabelOrganization: org,
				v1alpha2.AnnotationParent:  parentNs,
			},
			Annotations: annotations,
		},
	}
}

func TestGetDefaultGrants_NoWalker_ReturnsProjectDefaultsOnly(t *testing.T) {
	r := resolverTestResolver()
	projectNs := managedProjectNS("myproject", "acme", "holos-org-acme")
	projectNs.Annotations = map[string]string{
		v1alpha2.AnnotationDefaultShareUsers: grantsJSON(t, []secrets.AnnotationGrant{
			{Principal: "alice@example.com", Role: "viewer"},
		}),
	}
	projectNs.Labels[v1alpha2.AnnotationShareUsers] = grantsJSON(t, []secrets.AnnotationGrant{
		{Principal: "alice@example.com", Role: "owner"},
	})
	fakeClient := fake.NewClientset(projectNs)
	k8s := NewK8sClient(fakeClient, r)
	resolver := NewProjectGrantResolver(k8s) // no Walker

	users, roles, err := resolver.GetDefaultGrants(context.Background(), "myproject")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(users) != 1 || users[0].Principal != "alice@example.com" {
		t.Errorf("expected 1 user grant from project, got %v", users)
	}
	if len(roles) != 0 {
		t.Errorf("expected 0 role grants, got %v", roles)
	}
}

func TestGetDefaultGrants_WithWalker_ProjectUnderOrg_MergesOrgDefaults(t *testing.T) {
	r := resolverTestResolver()
	orgNs := orgNSWithDefaults("acme", []secrets.AnnotationGrant{
		{Principal: "bob@example.com", Role: "viewer"},
	}, nil, t)
	projectNs := managedProjectNS("myproject", "acme", "holos-org-acme")
	projectNs.Annotations = map[string]string{
		v1alpha2.AnnotationShareUsers: grantsJSON(t, []secrets.AnnotationGrant{
			{Principal: "alice@example.com", Role: "owner"},
		}),
	}
	fakeClient := fake.NewClientset(orgNs, projectNs)
	k8s := NewK8sClient(fakeClient, r)
	w := &resolver.Walker{Client: fakeClient, Resolver: r}
	pr := NewProjectGrantResolver(k8s).WithWalker(w)

	users, roles, err := pr.GetDefaultGrants(context.Background(), "myproject")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// Should include bob from org defaults
	found := false
	for _, u := range users {
		if u.Principal == "bob@example.com" && u.Role == "viewer" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected bob@example.com viewer from org defaults, got %v", users)
	}
	if len(roles) != 0 {
		t.Errorf("expected 0 role grants, got %v", roles)
	}
}

func TestGetDefaultGrants_WithWalker_ProjectUnderFolder_MergesOrgAndFolderDefaults(t *testing.T) {
	r := resolverTestResolver()
	orgNs := orgNSWithDefaults("acme", []secrets.AnnotationGrant{
		{Principal: "org-user@example.com", Role: "viewer"},
	}, nil, t)
	folderNs := folderNSWithDefaults("eng", "acme", "holos-org-acme", []secrets.AnnotationGrant{
		{Principal: "folder-user@example.com", Role: "editor"},
	}, nil, t)
	projectNs := managedProjectNS("myproject", "acme", "holos-fld-eng")
	fakeClient := fake.NewClientset(orgNs, folderNs, projectNs)
	k8s := NewK8sClient(fakeClient, r)
	w := &resolver.Walker{Client: fakeClient, Resolver: r}
	pr := NewProjectGrantResolver(k8s).WithWalker(w)

	users, _, err := pr.GetDefaultGrants(context.Background(), "myproject")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	principalRoles := make(map[string]string)
	for _, u := range users {
		principalRoles[u.Principal] = u.Role
	}
	if principalRoles["org-user@example.com"] != "viewer" {
		t.Errorf("expected org-user as viewer, got %q", principalRoles["org-user@example.com"])
	}
	if principalRoles["folder-user@example.com"] != "editor" {
		t.Errorf("expected folder-user as editor, got %q", principalRoles["folder-user@example.com"])
	}
}

func TestGetDefaultGrants_WithWalker_NestedFolders_MergesAllLevels(t *testing.T) {
	r := resolverTestResolver()
	orgNs := orgNSWithDefaults("acme", []secrets.AnnotationGrant{
		{Principal: "org-user@example.com", Role: "viewer"},
	}, nil, t)
	parentFolder := folderNSWithDefaults("eng", "acme", "holos-org-acme", []secrets.AnnotationGrant{
		{Principal: "parent-user@example.com", Role: "viewer"},
	}, nil, t)
	childFolder := folderNSWithDefaults("frontend", "acme", "holos-fld-eng", []secrets.AnnotationGrant{
		{Principal: "child-user@example.com", Role: "editor"},
	}, nil, t)
	projectNs := managedProjectNS("myproject", "acme", "holos-fld-frontend")
	fakeClient := fake.NewClientset(orgNs, parentFolder, childFolder, projectNs)
	k8s := NewK8sClient(fakeClient, r)
	w := &resolver.Walker{Client: fakeClient, Resolver: r}
	pr := NewProjectGrantResolver(k8s).WithWalker(w)

	users, _, err := pr.GetDefaultGrants(context.Background(), "myproject")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	principalRoles := make(map[string]string)
	for _, u := range users {
		principalRoles[u.Principal] = u.Role
	}
	if principalRoles["org-user@example.com"] != "viewer" {
		t.Errorf("expected org-user as viewer, got %q", principalRoles["org-user@example.com"])
	}
	if principalRoles["parent-user@example.com"] != "viewer" {
		t.Errorf("expected parent-user as viewer, got %q", principalRoles["parent-user@example.com"])
	}
	if principalRoles["child-user@example.com"] != "editor" {
		t.Errorf("expected child-user as editor, got %q", principalRoles["child-user@example.com"])
	}
}

func TestGetDefaultGrants_WithWalker_ExplicitGrantOverridesOrgDefault(t *testing.T) {
	// Project default overrides org default for the same principal.
	r := resolverTestResolver()
	orgNs := orgNSWithDefaults("acme", []secrets.AnnotationGrant{
		{Principal: "bob@example.com", Role: "viewer"},
	}, nil, t)
	projectNs := managedProjectNS("myproject", "acme", "holos-org-acme")
	// Project has bob as editor — should override org's viewer for bob.
	projectNs.Annotations = map[string]string{
		v1alpha2.AnnotationDefaultShareUsers: grantsJSON(t, []secrets.AnnotationGrant{
			{Principal: "bob@example.com", Role: "editor"},
		}),
	}
	fakeClient := fake.NewClientset(orgNs, projectNs)
	k8s := NewK8sClient(fakeClient, r)
	w := &resolver.Walker{Client: fakeClient, Resolver: r}
	pr := NewProjectGrantResolver(k8s).WithWalker(w)

	users, _, err := pr.GetDefaultGrants(context.Background(), "myproject")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var bobRole string
	for _, u := range users {
		if u.Principal == "bob@example.com" {
			bobRole = u.Role
			break
		}
	}
	if bobRole != "editor" {
		t.Errorf("expected bob as editor (project overrides org), got %q", bobRole)
	}
}
