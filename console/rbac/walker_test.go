package rbac

import (
	"context"
	"testing"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

// fakeAncestorProvider is a test double that returns a fixed ancestor chain.
type fakeAncestorProvider struct {
	ancestors []Ancestor
	err       error
}

func (f *fakeAncestorProvider) Ancestors(_ context.Context, _ string) ([]Ancestor, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.ancestors, nil
}

// orgAncestor creates an Ancestor for an organization namespace.
func orgAncestor(ns string, shareUsers, shareRoles map[string]string) Ancestor {
	return Ancestor{
		Namespace:    ns,
		ResourceType: v1alpha2.ResourceTypeOrganization,
		ShareUsers:   shareUsers,
		ShareRoles:   shareRoles,
	}
}

// folderAncestor creates an Ancestor for a folder namespace.
func folderAncestor(ns string, shareUsers, shareRoles map[string]string) Ancestor {
	return Ancestor{
		Namespace:    ns,
		ResourceType: v1alpha2.ResourceTypeFolder,
		ShareUsers:   shareUsers,
		ShareRoles:   shareRoles,
	}
}

// projectAncestor creates an Ancestor for a project namespace.
func projectAncestor(ns string, shareUsers, shareRoles map[string]string) Ancestor {
	return Ancestor{
		Namespace:    ns,
		ResourceType: v1alpha2.ResourceTypeProject,
		ShareUsers:   shareUsers,
		ShareRoles:   shareRoles,
	}
}

// templateCascadeTables maps resource types to the unified template cascade
// table used in tests (ADR 021 Decision 2: uniform permissions at all scopes).
var templateCascadeTables = map[string]CascadeTable{
	v1alpha2.ResourceTypeOrganization: TemplateCascadePerms,
	v1alpha2.ResourceTypeFolder:       TemplateCascadePerms,
	v1alpha2.ResourceTypeProject:      TemplateCascadePerms,
}

// secretCascadeTables maps resource types to secret cascade tables.
// Only project-level is included (ADR 007 non-cascading constraint).
var secretCascadeTables = map[string]CascadeTable{
	v1alpha2.ResourceTypeProject: ProjectCascadeSecretPerms,
}

func TestCheckAncestorCascade_OrgOwnerGrantsTemplateWrite(t *testing.T) {
	// Org Owner cascades to Project template write permission.
	provider := &fakeAncestorProvider{
		ancestors: []Ancestor{
			projectAncestor("holos-prj-api", nil, nil),
			orgAncestor("holos-org-acme", map[string]string{"alice@example.com": "owner"}, nil),
		},
	}
	err := CheckAncestorCascade(t.Context(), provider, "holos-prj-api", "alice@example.com", nil, PermissionTemplatesWrite, templateCascadeTables)
	if err != nil {
		t.Errorf("expected org OWNER to have TemplatesWrite via cascade, got: %v", err)
	}
}

func TestCheckAncestorCascade_FolderEditorGrantsTemplateWrite(t *testing.T) {
	// Folder Editor grants TemplatesWrite at project level beneath.
	provider := &fakeAncestorProvider{
		ancestors: []Ancestor{
			projectAncestor("holos-prj-api", nil, nil),
			folderAncestor("holos-fld-payments", map[string]string{"bob@example.com": "editor"}, nil),
			orgAncestor("holos-org-acme", nil, nil),
		},
	}
	err := CheckAncestorCascade(t.Context(), provider, "holos-prj-api", "bob@example.com", nil, PermissionTemplatesWrite, templateCascadeTables)
	if err != nil {
		t.Errorf("expected folder EDITOR to have TemplatesWrite via cascade, got: %v", err)
	}
}

func TestCheckAncestorCascade_ProjectViewerHasListAndRead(t *testing.T) {
	// Project Viewer has List and Read but not Write.
	provider := &fakeAncestorProvider{
		ancestors: []Ancestor{
			projectAncestor("holos-prj-api", map[string]string{"carol@example.com": "viewer"}, nil),
			orgAncestor("holos-org-acme", nil, nil),
		},
	}

	// List and Read should be granted.
	for _, perm := range []Permission{PermissionTemplatesList, PermissionTemplatesRead} {
		err := CheckAncestorCascade(t.Context(), provider, "holos-prj-api", "carol@example.com", nil, perm, templateCascadeTables)
		if err != nil {
			t.Errorf("expected project VIEWER to have %v via cascade, got: %v", perm, err)
		}
	}

	// Write should NOT be granted.
	err := CheckAncestorCascade(t.Context(), provider, "holos-prj-api", "carol@example.com", nil, PermissionTemplatesWrite, templateCascadeTables)
	if err == nil {
		t.Error("expected project VIEWER to NOT have TemplatesWrite, got nil error")
	}
}

func TestCheckAncestorCascade_SecretDoesNotWalkPastProject(t *testing.T) {
	// Org Owner grant does NOT grant SecretsRead (ADR 007: non-cascading).
	provider := &fakeAncestorProvider{
		ancestors: []Ancestor{
			projectAncestor("holos-prj-api", nil, nil),                                        // no project grant
			orgAncestor("holos-org-acme", map[string]string{"alice@example.com": "owner"}, nil), // org owner
		},
	}
	// secretCascadeTables only has project-level; org grants have no entry and
	// SecretsRead is not cascaded at the project level either.
	err := CheckAncestorCascade(t.Context(), provider, "holos-prj-api", "alice@example.com", nil, PermissionSecretsRead, secretCascadeTables)
	if err == nil {
		t.Error("expected org OWNER not to have SecretsRead via cascade (ADR 007), got nil error")
	}
}

func TestCheckAncestorCascade_SecretListGrantedAtProject(t *testing.T) {
	// Project Viewer gets SecretsList via project cascade.
	provider := &fakeAncestorProvider{
		ancestors: []Ancestor{
			projectAncestor("holos-prj-api", map[string]string{"dave@example.com": "viewer"}, nil),
			orgAncestor("holos-org-acme", nil, nil),
		},
	}
	err := CheckAncestorCascade(t.Context(), provider, "holos-prj-api", "dave@example.com", nil, PermissionSecretsList, secretCascadeTables)
	if err != nil {
		t.Errorf("expected project VIEWER to have SecretsList via project cascade, got: %v", err)
	}
}

func TestCheckAncestorCascade_MultipleGrantsHighestWins(t *testing.T) {
	// User has Viewer at project level but Owner at org level → org Owner wins → Write granted.
	provider := &fakeAncestorProvider{
		ancestors: []Ancestor{
			projectAncestor("holos-prj-api", map[string]string{"eve@example.com": "viewer"}, nil),
			orgAncestor("holos-org-acme", map[string]string{"eve@example.com": "owner"}, nil),
		},
	}
	err := CheckAncestorCascade(t.Context(), provider, "holos-prj-api", "eve@example.com", nil, PermissionTemplatesWrite, templateCascadeTables)
	if err != nil {
		t.Errorf("expected org OWNER to grant TemplatesWrite even when project VIEWER, got: %v", err)
	}
}

func TestCheckAncestorCascade_NoGrantsDenies(t *testing.T) {
	// No grants at any level → PermissionDenied.
	provider := &fakeAncestorProvider{
		ancestors: []Ancestor{
			projectAncestor("holos-prj-api", nil, nil),
			orgAncestor("holos-org-acme", nil, nil),
		},
	}
	err := CheckAncestorCascade(t.Context(), provider, "holos-prj-api", "nobody@example.com", nil, PermissionTemplatesRead, templateCascadeTables)
	if err == nil {
		t.Error("expected PermissionDenied with no grants, got nil")
	}
}

func TestEffectiveTemplateRole_OrgOwner(t *testing.T) {
	provider := &fakeAncestorProvider{
		ancestors: []Ancestor{
			projectAncestor("holos-prj-api", nil, nil),
			orgAncestor("holos-org-acme", map[string]string{"alice@example.com": "owner"}, nil),
		},
	}
	role, err := EffectiveTemplateRole(t.Context(), provider, "holos-prj-api", "alice@example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != RoleOwner {
		t.Errorf("expected RoleOwner, got %v", role)
	}
}

func TestEffectiveTemplateRole_NoGrants(t *testing.T) {
	provider := &fakeAncestorProvider{
		ancestors: []Ancestor{
			projectAncestor("holos-prj-api", nil, nil),
			orgAncestor("holos-org-acme", nil, nil),
		},
	}
	role, err := EffectiveTemplateRole(t.Context(), provider, "holos-prj-api", "nobody@example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != RoleUnspecified {
		t.Errorf("expected RoleUnspecified with no grants, got %v", role)
	}
}

func TestCheckAncestorCascade_GroupGrant(t *testing.T) {
	// Folder Editor via group grant cascades to project.
	provider := &fakeAncestorProvider{
		ancestors: []Ancestor{
			projectAncestor("holos-prj-api", nil, nil),
			folderAncestor("holos-fld-payments", nil, map[string]string{"engineering": "editor"}),
			orgAncestor("holos-org-acme", nil, nil),
		},
	}
	err := CheckAncestorCascade(t.Context(), provider, "holos-prj-api", "frank@example.com", []string{"engineering"}, PermissionTemplatesWrite, templateCascadeTables)
	if err != nil {
		t.Errorf("expected folder EDITOR via group grant to have TemplatesWrite, got: %v", err)
	}
}
