package projects

import (
	"context"
	"time"

	"github.com/holos-run/holos-console/console/secrets"
)

// ProjectGrantResolver implements secrets.ProjectResolver by looking up
// namespace annotations for project-level grants.
type ProjectGrantResolver struct {
	k8s *K8sClient
}

// NewProjectGrantResolver creates a resolver that reads grants from project namespaces.
func NewProjectGrantResolver(k8s *K8sClient) *ProjectGrantResolver {
	return &ProjectGrantResolver{k8s: k8s}
}

// GetProjectGrants returns the active user and group grant maps for a project.
// The project parameter is the user-facing project name (not the Kubernetes namespace).
func (r *ProjectGrantResolver) GetProjectGrants(ctx context.Context, project string) (map[string]string, map[string]string, error) {
	ns, err := r.k8s.GetProject(ctx, project) // GetProject handles prefix resolution
	if err != nil {
		return nil, nil, err
	}
	shareUsers, _ := GetShareUsers(ns)
	shareGroups, _ := GetShareGroups(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeGroups := secrets.ActiveGrantsMap(shareGroups, now)
	return activeUsers, activeGroups, nil
}
