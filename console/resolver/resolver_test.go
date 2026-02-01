package resolver

import "testing"

func TestOrgNamespace(t *testing.T) {
	r := &Resolver{Prefix: "holos-"}
	got := r.OrgNamespace("acme")
	if got != "holos-org-acme" {
		t.Errorf("expected %q, got %q", "holos-org-acme", got)
	}
}

func TestOrgNamespace_CustomPrefix(t *testing.T) {
	r := &Resolver{Prefix: "myco-"}
	got := r.OrgNamespace("acme")
	if got != "myco-org-acme" {
		t.Errorf("expected %q, got %q", "myco-org-acme", got)
	}
}

func TestOrgFromNamespace(t *testing.T) {
	r := &Resolver{Prefix: "holos-"}
	got := r.OrgFromNamespace("holos-org-acme")
	if got != "acme" {
		t.Errorf("expected %q, got %q", "acme", got)
	}
}

func TestProjectNamespace(t *testing.T) {
	r := &Resolver{Prefix: "holos-"}
	got := r.ProjectNamespace("acme", "api")
	if got != "holos-acme-api" {
		t.Errorf("expected %q, got %q", "holos-acme-api", got)
	}
}

func TestProjectNamespace_CustomPrefix(t *testing.T) {
	r := &Resolver{Prefix: "myco-"}
	got := r.ProjectNamespace("acme", "api")
	if got != "myco-acme-api" {
		t.Errorf("expected %q, got %q", "myco-acme-api", got)
	}
}

func TestProjectFromNamespace(t *testing.T) {
	r := &Resolver{Prefix: "holos-"}
	got := r.ProjectFromNamespace("holos-acme-api", "acme")
	if got != "api" {
		t.Errorf("expected %q, got %q", "api", got)
	}
}

func TestProjectNamespace_MultipleOrgs(t *testing.T) {
	r := &Resolver{Prefix: "holos-"}
	tests := []struct {
		org, project, want string
	}{
		{"acme", "api", "holos-acme-api"},
		{"acme", "frontend", "holos-acme-frontend"},
		{"beta", "api", "holos-beta-api"},
	}
	for _, tt := range tests {
		got := r.ProjectNamespace(tt.org, tt.project)
		if got != tt.want {
			t.Errorf("ProjectNamespace(%q, %q) = %q, want %q", tt.org, tt.project, got, tt.want)
		}
	}
}
