package resolver

import "testing"

func TestOrgNamespace(t *testing.T) {
	r := &Resolver{Prefix: "holos-"}
	got := r.OrgNamespace("acme")
	if got != "holos-o-acme" {
		t.Errorf("expected %q, got %q", "holos-o-acme", got)
	}
}

func TestOrgNamespace_CustomPrefix(t *testing.T) {
	r := &Resolver{Prefix: "myco-"}
	got := r.OrgNamespace("acme")
	if got != "myco-o-acme" {
		t.Errorf("expected %q, got %q", "myco-o-acme", got)
	}
}

func TestOrgFromNamespace(t *testing.T) {
	r := &Resolver{Prefix: "holos-"}
	got := r.OrgFromNamespace("holos-o-acme")
	if got != "acme" {
		t.Errorf("expected %q, got %q", "acme", got)
	}
}

func TestProjectNamespace(t *testing.T) {
	r := &Resolver{Prefix: "holos-"}
	got := r.ProjectNamespace("api")
	if got != "holos-p-api" {
		t.Errorf("expected %q, got %q", "holos-p-api", got)
	}
}

func TestProjectNamespace_CustomPrefix(t *testing.T) {
	r := &Resolver{Prefix: "myco-"}
	got := r.ProjectNamespace("api")
	if got != "myco-p-api" {
		t.Errorf("expected %q, got %q", "myco-p-api", got)
	}
}

func TestProjectFromNamespace(t *testing.T) {
	r := &Resolver{Prefix: "holos-"}
	got := r.ProjectFromNamespace("holos-p-api")
	if got != "api" {
		t.Errorf("expected %q, got %q", "api", got)
	}
}

func TestOrgAndProjectSameNameDifferentNamespaces(t *testing.T) {
	r := &Resolver{Prefix: "holos-"}
	orgNS := r.OrgNamespace("acme")
	projNS := r.ProjectNamespace("acme")
	if orgNS == projNS {
		t.Errorf("org and project with same name should have different namespaces, both got %q", orgNS)
	}
	if orgNS != "holos-o-acme" {
		t.Errorf("expected org namespace %q, got %q", "holos-o-acme", orgNS)
	}
	if projNS != "holos-p-acme" {
		t.Errorf("expected project namespace %q, got %q", "holos-p-acme", projNS)
	}
}
