package templatepolicybindings

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"

	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/scopeshim"
)

// OrgGrantResolver resolves organization-level grants for access checks. The
// handler accepts the same interface shape used by console/templatepolicies
// so the wiring in console.go can pass the existing organizations resolver
// without an adapter.
type OrgGrantResolver interface {
	GetOrgGrants(ctx context.Context, org string) (users, roles map[string]string, err error)
}

// FolderGrantResolver resolves folder-level grants for access checks.
type FolderGrantResolver interface {
	GetFolderGrants(ctx context.Context, folder string) (users, roles map[string]string, err error)
}

// checkAccess enforces the TemplatePolicyBindingCascadePerms RBAC table on
// the binding's owning scope. Project scope is rejected up front — the
// cascade table exists only at org/folder, so any attempt to check a
// project-scope binding here is a programming error that earlier handler
// validation should have caught.
func (h *Handler) checkAccess(ctx context.Context, claims *rpc.Claims, scope scopeshim.Scope, scopeName string, perm rbac.Permission) error {
	switch scope {
	case scopeshim.ScopeOrganization:
		return h.checkOrgAccess(ctx, claims, scopeName, perm)
	case scopeshim.ScopeFolder:
		return h.checkFolderAccess(ctx, claims, scopeName, perm)
	case scopeshim.ScopeProject:
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("template policy bindings cannot be scoped to a project; use an organization or folder scope"))
	default:
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown scope %v", scope))
	}
}

func (h *Handler) checkOrgAccess(ctx context.Context, claims *rpc.Claims, org string, perm rbac.Permission) error {
	if h.orgGrantResolver == nil {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	users, roles, err := h.orgGrantResolver.GetOrgGrants(ctx, org)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve org grants", slog.String("org", org), slog.Any("error", err))
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	return rbac.CheckCascadeAccess(claims.Email, claims.Roles, users, roles, perm, rbac.TemplatePolicyBindingCascadePerms)
}

func (h *Handler) checkFolderAccess(ctx context.Context, claims *rpc.Claims, folder string, perm rbac.Permission) error {
	if h.folderGrantResolver == nil {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	users, roles, err := h.folderGrantResolver.GetFolderGrants(ctx, folder)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve folder grants", slog.String("folder", folder), slog.Any("error", err))
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	return rbac.CheckCascadeAccess(claims.Email, claims.Roles, users, roles, perm, rbac.TemplatePolicyBindingCascadePerms)
}
