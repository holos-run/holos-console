package rbac

import (
	"context"

	"connectrpc.com/connect"
	"fmt"
)

// Ancestor represents a single namespace in the hierarchy chain, along with
// the sharing grants stored on it. Populated by AncestorProvider implementations.
type Ancestor struct {
	// Namespace is the Kubernetes namespace name.
	Namespace string
	// ResourceType is the resource kind: "organization", "folder", or "project"
	// (matches v1alpha2.ResourceType* constants).
	ResourceType string
	// ShareUsers is the per-user grant map (email → role name) from the
	// console.holos.run/share-users annotation.
	ShareUsers map[string]string
	// ShareRoles is the per-role grant map (OIDC role → role name) from the
	// console.holos.run/share-roles annotation.
	ShareRoles map[string]string
}

// AncestorProvider retrieves the ancestor chain for a given start namespace,
// returning namespaces in child→parent order (startNs first, org last).
// The interface is satisfied by production adapters over resolver.Walker and
// by map-based fakes in tests.
type AncestorProvider interface {
	Ancestors(ctx context.Context, startNs string) ([]Ancestor, error)
}

// CheckAncestorCascade walks the ancestor chain for startNs and returns nil
// if the user is granted the requested permission at any level according to
// the per-resource-type cascade tables. The check stops at the first level
// that grants the permission; all levels are evaluated for the highest role
// before checking the permission.
//
// tableByResourceType maps resource type strings (e.g., "organization",
// "folder", "project") to the CascadeTable that governs permission inheritance
// at that level. Callers control which resource types participate: passing a
// map with only a "project" entry enforces non-cascading semantics (ADR 007).
//
// Returns a PermissionDenied error when no ancestor grants the permission.
func CheckAncestorCascade(
	ctx context.Context,
	provider AncestorProvider,
	startNs string,
	userEmail string,
	userRoles []string,
	permission Permission,
	tableByResourceType map[string]CascadeTable,
) error {
	ancestors, err := provider.Ancestors(ctx, startNs)
	if err != nil {
		return err
	}

	for _, ancestor := range ancestors {
		table, ok := tableByResourceType[ancestor.ResourceType]
		if !ok {
			// No cascade table for this resource type — skip.
			continue
		}
		role := BestRoleFromGrants(userEmail, userRoles, ancestor.ShareUsers, ancestor.ShareRoles)
		if HasCascadePermission(role, permission, table) {
			return nil
		}
	}

	return connect.NewError(
		connect.CodePermissionDenied,
		fmt.Errorf("RBAC: authorization denied"),
	)
}

// EffectiveTemplateRole returns the highest role the user holds across all
// ancestor levels using the template cascade tables. This is used by handlers
// that need to include the effective role in responses (e.g., to show whether
// the user can edit a template) without repeating the ancestor walk.
//
// Returns RoleUnspecified when the user has no grants at any ancestor level.
func EffectiveTemplateRole(
	ctx context.Context,
	provider AncestorProvider,
	startNs string,
	userEmail string,
	userRoles []string,
) (Role, error) {
	ancestors, err := provider.Ancestors(ctx, startNs)
	if err != nil {
		return RoleUnspecified, err
	}

	templateTables := map[string]CascadeTable{
		"organization": TemplateCascadePerms,
		"folder":       TemplateCascadePerms,
		"project":      TemplateCascadePerms,
	}

	bestLevel := 0
	bestRole := RoleUnspecified

	for _, ancestor := range ancestors {
		if _, ok := templateTables[ancestor.ResourceType]; !ok {
			continue
		}
		role := BestRoleFromGrants(userEmail, userRoles, ancestor.ShareUsers, ancestor.ShareRoles)
		if level := RoleLevel(role); level > bestLevel {
			bestLevel = level
			bestRole = role
		}
	}

	return bestRole, nil
}
