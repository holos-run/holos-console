package resolver

import "testing"

func TestOrgNamespace(t *testing.T) {
	r := &Resolver{OrgPrefix: "holos-org-", ProjectPrefix: "holos-prj-"}
	got := r.OrgNamespace("acme")
	if got != "holos-org-acme" {
		t.Errorf("expected %q, got %q", "holos-org-acme", got)
	}
}

func TestOrgNamespace_CustomPrefix(t *testing.T) {
	r := &Resolver{OrgPrefix: "myco-org-", ProjectPrefix: "holos-prj-"}
	got := r.OrgNamespace("acme")
	if got != "myco-org-acme" {
		t.Errorf("expected %q, got %q", "myco-org-acme", got)
	}
}

func TestOrgFromNamespace(t *testing.T) {
	r := &Resolver{OrgPrefix: "holos-org-", ProjectPrefix: "holos-prj-"}
	got := r.OrgFromNamespace("holos-org-acme")
	if got != "acme" {
		t.Errorf("expected %q, got %q", "acme", got)
	}
}

func TestProjectNamespace(t *testing.T) {
	r := &Resolver{OrgPrefix: "holos-org-", ProjectPrefix: "holos-prj-"}
	got := r.ProjectNamespace("foo")
	if got != "holos-prj-foo" {
		t.Errorf("expected %q, got %q", "holos-prj-foo", got)
	}
}

func TestProjectNamespace_CustomPrefix(t *testing.T) {
	r := &Resolver{OrgPrefix: "holos-org-", ProjectPrefix: "myco-prj-"}
	got := r.ProjectNamespace("foo")
	if got != "myco-prj-foo" {
		t.Errorf("expected %q, got %q", "myco-prj-foo", got)
	}
}

func TestProjectFromNamespace(t *testing.T) {
	r := &Resolver{OrgPrefix: "holos-org-", ProjectPrefix: "holos-prj-"}
	got := r.ProjectFromNamespace("holos-prj-foo")
	if got != "foo" {
		t.Errorf("expected %q, got %q", "foo", got)
	}
}
