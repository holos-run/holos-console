package resolver

import "testing"

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
	got := r.OrgFromNamespace("org-acme")
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
	got := r.ProjectFromNamespace("prj-api")
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
	if got := r.OrgFromNamespace(r.OrgNamespace(name)); got != name {
		t.Errorf("round-trip failed: expected %q, got %q", name, got)
	}
}

func TestProjectRoundTrip(t *testing.T) {
	r := &Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	name := "api"
	if got := r.ProjectFromNamespace(r.ProjectNamespace(name)); got != name {
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
	got := r.OrgFromNamespace("prod-org-acme")
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
	got := r.ProjectFromNamespace("prod-prj-api")
	if got != "api" {
		t.Errorf("expected %q, got %q", "api", got)
	}
}

func TestOrgRoundTrip_WithNamespacePrefix(t *testing.T) {
	r := &Resolver{NamespacePrefix: "ci-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	name := "acme"
	if got := r.OrgFromNamespace(r.OrgNamespace(name)); got != name {
		t.Errorf("round-trip failed: expected %q, got %q", name, got)
	}
}

func TestProjectRoundTrip_WithNamespacePrefix(t *testing.T) {
	r := &Resolver{NamespacePrefix: "ci-", OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
	name := "api"
	if got := r.ProjectFromNamespace(r.ProjectNamespace(name)); got != name {
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
