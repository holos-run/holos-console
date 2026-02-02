package resolver

import (
	"errors"
	"testing"
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

func TestPrefixMismatchError_ErrorMessage(t *testing.T) {
	err := &PrefixMismatchError{Namespace: "kube-system", Prefix: "holos-org-"}
	want := `namespace "kube-system" does not match expected prefix "holos-org-"`
	if got := err.Error(); got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}
