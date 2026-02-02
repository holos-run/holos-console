package organizations

import (
	"context"
	"time"

	"github.com/holos-run/holos-console/console/secrets"
)

// OrgGrantResolver looks up organization-level grants for access fallback.
type OrgGrantResolver struct {
	k8s *K8sClient
}

// NewOrgGrantResolver creates a resolver that reads grants from organization namespaces.
func NewOrgGrantResolver(k8s *K8sClient) *OrgGrantResolver {
	return &OrgGrantResolver{k8s: k8s}
}

// GetOrgGrants returns the active user and role grant maps for an organization.
func (r *OrgGrantResolver) GetOrgGrants(ctx context.Context, org string) (map[string]string, map[string]string, error) {
	ns, err := r.k8s.GetOrganization(ctx, org)
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
