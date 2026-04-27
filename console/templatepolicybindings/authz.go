package templatepolicybindings

import (
	"context"
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
