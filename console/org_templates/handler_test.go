package org_templates

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	"github.com/holos-run/holos-console/console/rpc"
)

// stubOrgResolver implements OrgResolver for tests.
type stubOrgResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubOrgResolver) GetOrgGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	return s.users, s.roles, s.err
}

// stubRenderer implements Renderer for tests.
type stubRenderer struct {
	resources []RenderResource
	err       error
}

func (r *stubRenderer) Render(_ context.Context, _ string, _ string, _ string) ([]RenderResource, error) {
	return r.resources, r.err
}

func authedCtx(email string, roles []string) context.Context {
	return rpc.ContextWithClaims(context.Background(), &rpc.Claims{
		Sub:   "user-123",
		Email: email,
		Roles: roles,
	})
}

// ownerGrants returns grants giving the email OWNER access to the org.
func ownerGrants(email string) *stubOrgResolver {
	return &stubOrgResolver{
		users: map[string]string{email: "owner"},
	}
}

// viewerGrants returns grants giving the email VIEWER access to the org.
func viewerGrants(email string) *stubOrgResolver {
	return &stubOrgResolver{
		users: map[string]string{email: "viewer"},
	}
}

const validCue = `
#Input: {}
`

// TestValidateTemplateName tests the DNS label validation helper.
func TestValidateTemplateName(t *testing.T) {
	valid := []string{"ref-grant", "my-template", "abc", "a1b2"}
	for _, name := range valid {
		if err := validateTemplateName(name); err != nil {
			t.Errorf("expected valid name %q to pass, got %v", name, err)
		}
	}
	invalid := []string{"", "Invalid!", "1bad", "-bad", "bad-", "this-name-is-longer-than-sixty-three-characters-and-should-fail!"}
	for _, name := range invalid {
		if err := validateTemplateName(name); err == nil {
			t.Errorf("expected invalid name %q to fail, but it passed", name)
		}
	}
}

// TestValidateCueSyntax tests the CUE syntax detection helper.
func TestValidateCueSyntax(t *testing.T) {
	if err := validateCueSyntax(validCue); err != nil {
		t.Errorf("expected valid CUE to pass, got %v", err)
	}
	if err := validateCueSyntax("this is not valid {{ cue"); err == nil {
		t.Error("expected invalid CUE to fail, but it passed")
	}
}

// TestCheckOrgEditAccess verifies that only org OWNERs can write templates.
func TestCheckOrgEditAccess(t *testing.T) {
	t.Run("org OWNER can write", func(t *testing.T) {
		h := NewHandler(nil, ownerGrants("owner@example.com"), nil)
		ctx := authedCtx("owner@example.com", nil)
		claims := rpc.ClaimsFromContext(ctx)
		if err := h.checkOrgEditAccess(ctx, claims, "my-org"); err != nil {
			t.Errorf("expected owner to have write access, got %v", err)
		}
	})

	t.Run("org VIEWER cannot write", func(t *testing.T) {
		h := NewHandler(nil, viewerGrants("viewer@example.com"), nil)
		ctx := authedCtx("viewer@example.com", nil)
		claims := rpc.ClaimsFromContext(ctx)
		if err := h.checkOrgEditAccess(ctx, claims, "my-org"); err == nil {
			t.Error("expected viewer to be denied write access, got nil")
		} else if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("expected CodePermissionDenied, got %v", connect.CodeOf(err))
		}
	})
}
