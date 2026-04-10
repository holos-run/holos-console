package folders

import (
	"context"
	"time"

	"github.com/holos-run/holos-console/console/secrets"
)

// FolderGrantResolver resolves folder-level grants for access checks.
type FolderGrantResolver struct {
	k8s *K8sClient
}

// NewFolderGrantResolver creates a resolver that reads grants from folder namespaces.
func NewFolderGrantResolver(k8s *K8sClient) *FolderGrantResolver {
	return &FolderGrantResolver{k8s: k8s}
}

// GetFolderGrants returns the active user and role grant maps for a folder.
// The folder parameter is the user-facing folder name (not the Kubernetes namespace).
func (r *FolderGrantResolver) GetFolderGrants(ctx context.Context, folder string) (map[string]string, map[string]string, error) {
	ns, err := r.k8s.GetFolder(ctx, folder)
	if err != nil {
		return nil, nil, err
	}
	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)
	return activeUsers, activeRoles, nil
}
