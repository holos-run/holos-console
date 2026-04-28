package resolver

import (
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

func TestOrgNamespace(t *testing.T) {
	r := &Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	got := r.OrgNamespace("acme")
	if got != "org-acme" {
		t.Errorf("expected %q, got %q", "org-acme", got)
	}
}

func TestOrgNamespace_CustomPrefix(t *testing.T) {
	r := &Resolver{OrganizationPrefix: "myco-org-", ProjectPrefix: "myco-prj-"}
	got := r.OrgNamespace("acme")
	if got != "myco-org-acme" {
		t.Errorf("expected %q, got %q", "myco-org-acme", got)
	}
}

func TestOrgFromNamespace(t *testing.T) {
	r := &Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	got, err := r.OrgFromNamespace("org-acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "acme" {
		t.Errorf("expected %q, got %q", "acme", got)
	}
}

func TestProjectNamespace(t *testing.T) {
	r := &Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	got := r.ProjectNamespace("api")
	if got != "prj-api" {
		t.Errorf("expected %q, got %q", "prj-api", got)
	}
}

func TestProjectNamespace_CustomPrefix(t *testing.T) {
	r := &Resolver{OrganizationPrefix: "myco-org-", ProjectPrefix: "myco-prj-"}
	got := r.ProjectNamespace("api")
	if got != "myco-prj-api" {
		t.Errorf("expected %q, got %q", "myco-prj-api", got)
	}
}

func TestProjectFromNamespace(t *testing.T) {
	r := &Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	got, err := r.ProjectFromNamespace("prj-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "api" {
		t.Errorf("expected %q, got %q", "api", got)
	}
}

func TestOrgAndProjectSameNameDifferentNamespaces(t *testing.T) {
	r := &Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	orgNS := r.OrgNamespace("acme")
	projNS := r.ProjectNamespace("acme")
	if orgNS == projNS {
		t.Errorf("org and project with same name should have different namespaces, both got %q", orgNS)
	}
	if orgNS != "org-acme" {
		t.Errorf("expected org namespace %q, got %q", "org-acme", orgNS)
	}
	if projNS != "prj-acme" {
		t.Errorf("expected project namespace %q, got %q", "prj-acme", projNS)
	}
}

func TestOrgRoundTrip(t *testing.T) {
	r := &Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	name := "acme"
	got, err := r.OrgFromNamespace(r.OrgNamespace(name))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != name {
		t.Errorf("round-trip failed: expected %q, got %q", name, got)
	}
}

func TestProjectRoundTrip(t *testing.T) {
	r := &Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	name := "api"
	got, err := r.ProjectFromNamespace(r.ProjectNamespace(name))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != name {
		t.Errorf("round-trip failed: expected %q, got %q", name, got)
	}
}

func TestOrgNamespace_WithNamespacePrefix(t *testing.T) {
	r := &Resolver{NamespacePrefix: "prod-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	got := r.OrgNamespace("acme")
	if got != "prod-org-acme" {
		t.Errorf("expected %q, got %q", "prod-org-acme", got)
	}
}

func TestOrgFromNamespace_WithNamespacePrefix(t *testing.T) {
	r := &Resolver{NamespacePrefix: "prod-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	got, err := r.OrgFromNamespace("prod-org-acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "acme" {
		t.Errorf("expected %q, got %q", "acme", got)
	}
}

func TestProjectNamespace_WithNamespacePrefix(t *testing.T) {
	r := &Resolver{NamespacePrefix: "prod-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	got := r.ProjectNamespace("api")
	if got != "prod-prj-api" {
		t.Errorf("expected %q, got %q", "prod-prj-api", got)
	}
}

func TestProjectFromNamespace_WithNamespacePrefix(t *testing.T) {
	r := &Resolver{NamespacePrefix: "prod-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	got, err := r.ProjectFromNamespace("prod-prj-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "api" {
		t.Errorf("expected %q, got %q", "api", got)
	}
}

func TestOrgRoundTrip_WithNamespacePrefix(t *testing.T) {
	r := &Resolver{NamespacePrefix: "ci-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	name := "acme"
	got, err := r.OrgFromNamespace(r.OrgNamespace(name))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != name {
		t.Errorf("round-trip failed: expected %q, got %q", name, got)
	}
}

func TestProjectRoundTrip_WithNamespacePrefix(t *testing.T) {
	r := &Resolver{NamespacePrefix: "ci-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	name := "api"
	got, err := r.ProjectFromNamespace(r.ProjectNamespace(name))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != name {
		t.Errorf("round-trip failed: expected %q, got %q", name, got)
	}
}

func TestNamespacePrefix_EmptyIsNoOp(t *testing.T) {
	r := &Resolver{NamespacePrefix: "", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	if got := r.OrgNamespace("acme"); got != "org-acme" {
		t.Errorf("expected %q, got %q", "org-acme", got)
	}
	if got := r.ProjectNamespace("api"); got != "prj-api" {
		t.Errorf("expected %q, got %q", "prj-api", got)
	}
}

// ---- PrefixMismatchError tests ----

func TestOrgFromNamespace_PrefixMismatch(t *testing.T) {
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	_, err := r.OrgFromNamespace("other-org-acme")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pme *PrefixMismatchError
	if !errors.As(err, &pme) {
		t.Fatalf("expected *PrefixMismatchError, got %T: %v", err, err)
	}
	if pme.Namespace != "other-org-acme" {
		t.Errorf("expected Namespace %q, got %q", "other-org-acme", pme.Namespace)
	}
	if pme.Prefix != "holos-org-" {
		t.Errorf("expected Prefix %q, got %q", "holos-org-", pme.Prefix)
	}
}

func TestProjectFromNamespace_PrefixMismatch(t *testing.T) {
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	_, err := r.ProjectFromNamespace("other-prj-api")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pme *PrefixMismatchError
	if !errors.As(err, &pme) {
		t.Fatalf("expected *PrefixMismatchError, got %T: %v", err, err)
	}
	if pme.Namespace != "other-prj-api" {
		t.Errorf("expected Namespace %q, got %q", "other-prj-api", pme.Namespace)
	}
	if pme.Prefix != "holos-prj-" {
		t.Errorf("expected Prefix %q, got %q", "holos-prj-", pme.Prefix)
	}
}

func TestOrgFromNamespace_ProjectNamespaceIsMismatch(t *testing.T) {
	// A project namespace should not be parseable as an org namespace
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	_, err := r.OrgFromNamespace("holos-prj-api")
	if err == nil {
		t.Fatal("expected error when parsing project namespace as org, got nil")
	}
	var pme *PrefixMismatchError
	if !errors.As(err, &pme) {
		t.Fatalf("expected *PrefixMismatchError, got %T: %v", err, err)
	}
}

func TestProjectFromNamespace_OrgNamespaceIsMismatch(t *testing.T) {
	// An org namespace should not be parseable as a project namespace
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	_, err := r.ProjectFromNamespace("holos-org-acme")
	if err == nil {
		t.Fatal("expected error when parsing org namespace as project, got nil")
	}
	var pme *PrefixMismatchError
	if !errors.As(err, &pme) {
		t.Fatalf("expected *PrefixMismatchError, got %T: %v", err, err)
	}
}

func TestOrgFromNamespace_EmptyNamespace(t *testing.T) {
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	_, err := r.OrgFromNamespace("")
	if err == nil {
		t.Fatal("expected error for empty namespace, got nil")
	}
	var pme *PrefixMismatchError
	if !errors.As(err, &pme) {
		t.Fatalf("expected *PrefixMismatchError, got %T: %v", err, err)
	}
}

// ---- Folder resolution tests ----

func TestFolderNamespace(t *testing.T) {
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-", FolderPrefix: "fld-"}
	got := r.FolderNamespace("payments")
	if got != "holos-fld-payments" {
		t.Errorf("expected %q, got %q", "holos-fld-payments", got)
	}
}

func TestFolderNamespace_CustomPrefix(t *testing.T) {
	r := &Resolver{NamespacePrefix: "prod-", OrganizationPrefix: "org-", ProjectPrefix: "prj-", FolderPrefix: "fld-"}
	got := r.FolderNamespace("payments")
	if got != "prod-fld-payments" {
		t.Errorf("expected %q, got %q", "prod-fld-payments", got)
	}
}

func TestFolderFromNamespace(t *testing.T) {
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-", FolderPrefix: "fld-"}
	got, err := r.FolderFromNamespace("holos-fld-payments")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "payments" {
		t.Errorf("expected %q, got %q", "payments", got)
	}
}

func TestFolderFromNamespace_PrefixMismatch(t *testing.T) {
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-", FolderPrefix: "fld-"}
	_, err := r.FolderFromNamespace("holos-prj-api")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pme *PrefixMismatchError
	if !errors.As(err, &pme) {
		t.Fatalf("expected *PrefixMismatchError, got %T: %v", err, err)
	}
}

func TestFolderRoundTrip(t *testing.T) {
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-", FolderPrefix: "fld-"}
	name := "payments"
	got, err := r.FolderFromNamespace(r.FolderNamespace(name))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != name {
		t.Errorf("round-trip failed: expected %q, got %q", name, got)
	}
}

// ---- ResourceTypeFromNamespace tests ----

func TestResourceTypeFromNamespace_Org(t *testing.T) {
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-", FolderPrefix: "fld-"}
	kind, name, err := r.ResourceTypeFromNamespace("holos-org-acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != "organization" {
		t.Errorf("expected kind %q, got %q", "organization", kind)
	}
	if name != "acme" {
		t.Errorf("expected name %q, got %q", "acme", name)
	}
}

func TestResourceTypeFromNamespace_Folder(t *testing.T) {
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-", FolderPrefix: "fld-"}
	kind, name, err := r.ResourceTypeFromNamespace("holos-fld-payments")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != "folder" {
		t.Errorf("expected kind %q, got %q", "folder", kind)
	}
	if name != "payments" {
		t.Errorf("expected name %q, got %q", "payments", name)
	}
}

func TestResourceTypeFromNamespace_Project(t *testing.T) {
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-", FolderPrefix: "fld-"}
	kind, name, err := r.ResourceTypeFromNamespace("holos-prj-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != "project" {
		t.Errorf("expected kind %q, got %q", "project", kind)
	}
	if name != "api" {
		t.Errorf("expected name %q, got %q", "api", name)
	}
}

func TestResourceTypeFromNamespace_Error(t *testing.T) {
	r := &Resolver{NamespacePrefix: "holos-", OrganizationPrefix: "org-", ProjectPrefix: "prj-", FolderPrefix: "fld-"}
	_, _, err := r.ResourceTypeFromNamespace("kube-system")
	if err == nil {
		t.Fatal("expected error for unrecognized namespace, got nil")
	}
}

// ---- Walker tests ----

func makeNS(name string, labels map[string]string) corev1.Namespace {
	return corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func defaultResolver() *Resolver {
	return &Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		ProjectPrefix:      "prj-",
		FolderPrefix:       "fld-",
	}
}

func TestWalkAncestors_OrgProject(t *testing.T) {
	// Org → Project (direct child, 2 levels)
	orgNS := makeNS("holos-org-acme", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
	})
	projNS := makeNS("holos-prj-api", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
		v1alpha2.AnnotationParent:  "holos-org-acme",
	})
	client := fake.NewClientset(&orgNS, &projNS)
	w := &Walker{Client: client, Resolver: defaultResolver()}

	ancestors, err := w.WalkAncestors(t.Context(), "holos-prj-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ancestors) != 2 {
		t.Fatalf("expected 2 ancestors (project + org), got %d", len(ancestors))
	}
	if ancestors[0].Name != "holos-prj-api" {
		t.Errorf("expected first ancestor %q, got %q", "holos-prj-api", ancestors[0].Name)
	}
	if ancestors[1].Name != "holos-org-acme" {
		t.Errorf("expected second ancestor %q, got %q", "holos-org-acme", ancestors[1].Name)
	}
}

func TestWalkAncestors_OrgFolderProject(t *testing.T) {
	// Org → Folder → Project
	orgNS := makeNS("holos-org-acme", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
	})
	folderNS := makeNS("holos-fld-payments", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
		v1alpha2.AnnotationParent:  "holos-org-acme",
	})
	projNS := makeNS("holos-prj-api", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
		v1alpha2.AnnotationParent:  "holos-fld-payments",
	})
	client := fake.NewClientset(&orgNS, &folderNS, &projNS)
	w := &Walker{Client: client, Resolver: defaultResolver()}

	ancestors, err := w.WalkAncestors(t.Context(), "holos-prj-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ancestors) != 3 {
		t.Fatalf("expected 3 ancestors, got %d", len(ancestors))
	}
	if ancestors[0].Name != "holos-prj-api" {
		t.Errorf("expected first %q, got %q", "holos-prj-api", ancestors[0].Name)
	}
	if ancestors[1].Name != "holos-fld-payments" {
		t.Errorf("expected second %q, got %q", "holos-fld-payments", ancestors[1].Name)
	}
	if ancestors[2].Name != "holos-org-acme" {
		t.Errorf("expected third %q, got %q", "holos-org-acme", ancestors[2].Name)
	}
}

func TestWalkAncestors_MaxDepth(t *testing.T) {
	// Org → Folder → Folder → Folder → Project (4 levels = 5 namespaces, at cap)
	orgNS := makeNS("holos-org-acme", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
	})
	f1 := makeNS("holos-fld-f1", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
		v1alpha2.AnnotationParent:  "holos-org-acme",
	})
	f2 := makeNS("holos-fld-f2", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
		v1alpha2.AnnotationParent:  "holos-fld-f1",
	})
	f3 := makeNS("holos-fld-f3", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
		v1alpha2.AnnotationParent:  "holos-fld-f2",
	})
	proj := makeNS("holos-prj-api", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
		v1alpha2.AnnotationParent:  "holos-fld-f3",
	})
	client := fake.NewClientset(&orgNS, &f1, &f2, &f3, &proj)
	w := &Walker{Client: client, Resolver: defaultResolver()}

	ancestors, err := w.WalkAncestors(t.Context(), "holos-prj-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ancestors) != 5 {
		t.Fatalf("expected 5 ancestors, got %d", len(ancestors))
	}
}

func TestWalkAncestors_DepthExceeded(t *testing.T) {
	// 6 levels deep — should return error
	orgNS := makeNS("holos-org-acme", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
	})
	nss := []corev1.Namespace{orgNS}
	prev := "holos-org-acme"
	for i := 1; i <= 5; i++ {
		name := "holos-fld-f" + string(rune('0'+i))
		ns := makeNS(name, map[string]string{
			v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
			v1alpha2.AnnotationParent:  prev,
		})
		nss = append(nss, ns)
		prev = name
	}
	proj := makeNS("holos-prj-api", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
		v1alpha2.AnnotationParent:  prev,
	})
	nss = append(nss, proj)

	client := fake.NewClientset(nsSliceToObjects(nss)...)
	w := &Walker{Client: client, Resolver: defaultResolver()}

	_, err := w.WalkAncestors(t.Context(), "holos-prj-api")
	if err == nil {
		t.Fatal("expected depth exceeded error, got nil")
	}
}

func TestWalkAncestors_CycleDetected(t *testing.T) {
	// Cycle: f1 → f2 → f1
	f1 := makeNS("holos-fld-f1", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
		v1alpha2.AnnotationParent:  "holos-fld-f2",
	})
	f2 := makeNS("holos-fld-f2", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
		v1alpha2.AnnotationParent:  "holos-fld-f1",
	})
	proj := makeNS("holos-prj-api", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
		v1alpha2.AnnotationParent:  "holos-fld-f1",
	})
	client := fake.NewClientset(&f1, &f2, &proj)
	w := &Walker{Client: client, Resolver: defaultResolver()}

	_, err := w.WalkAncestors(t.Context(), "holos-prj-api")
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}

func TestWalkAncestors_MissingParentLabel(t *testing.T) {
	// Folder without parent label — should return error
	folder := makeNS("holos-fld-f1", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
		// no AnnotationParent
	})
	proj := makeNS("holos-prj-api", map[string]string{
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
		v1alpha2.AnnotationParent:  "holos-fld-f1",
	})
	client := fake.NewClientset(&folder, &proj)
	w := &Walker{Client: client, Resolver: defaultResolver()}

	_, err := w.WalkAncestors(t.Context(), "holos-prj-api")
	if err == nil {
		t.Fatal("expected missing parent label error, got nil")
	}
}

// nsSliceToObjects converts []corev1.Namespace to []runtime.Object for fake client.
func nsSliceToObjects(nss []corev1.Namespace) []runtime.Object {
	result := make([]runtime.Object, len(nss))
	for i := range nss {
		result[i] = &nss[i]
	}
	return result
}

func TestPrefixMismatchError_ErrorMessage(t *testing.T) {
	err := &PrefixMismatchError{Namespace: "kube-system", Prefix: "holos-org-"}
	want := `namespace "kube-system" does not match expected prefix "holos-org-"`
	if got := err.Error(); got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}
