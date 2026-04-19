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

// GetOrgDefaultGrants returns the default sharing grants for an organization.
// These are applied to new projects created within the organization.
// Implements projects.OrgDefaultShareResolver.
func (r *OrgGrantResolver) GetOrgDefaultGrants(ctx context.Context, org string) ([]secrets.AnnotationGrant, []secrets.AnnotationGrant, error) {
	ns, err := r.k8s.GetOrganization(ctx, org)
	if err != nil {
		return nil, nil, err
	}
	defaultUsers, _ := GetDefaultShareUsers(ns)
	defaultRoles, _ := GetDefaultShareRoles(ns)
	return defaultUsers, defaultRoles, nil
}

// ProjectOrgResolver maps a user-facing project name to the user-facing
// organization name that owns it. It deliberately mirrors
// settings.ProjectOrgResolver so the existing project→org mapping can be
// reused without dragging the projects package into a cyclic import here.
type ProjectOrgResolver interface {
	GetProjectOrganization(ctx context.Context, project string) (string, error)
}

// GatewayNamespaceResolver looks up the configured ingress-gateway
// namespace for the organization that owns a given project. It implements
// the deployments.OrganizationGatewayResolver interface so the deployments
// handler can inject the platform engineer's configured value (set via the
// Organization service in HOL-643) into PlatformInput.gatewayNamespace —
// removing the historical hard-coded "istio-ingress" injection that
// conflicted with template authors who explicitly pinned a different value
// (HOL-526).
//
// Resolution path: project → org name (via ProjectOrgResolver) → org
// namespace (via K8sClient.GetOrganization) → gateway-namespace annotation
// (via GetGatewayNamespace). Returns an empty string when the annotation is
// absent so the caller can apply its own fallback (deployments.Handler
// falls back to DefaultGatewayNamespace).
type GatewayNamespaceResolver struct {
	k8s         *K8sClient
	projectOrgs ProjectOrgResolver
}

// NewGatewayNamespaceResolver constructs a GatewayNamespaceResolver. The
// projectOrgs resolver is required: without it, we cannot map a project to
// its owning organization and no annotation lookup is possible.
func NewGatewayNamespaceResolver(k8s *K8sClient, projectOrgs ProjectOrgResolver) *GatewayNamespaceResolver {
	return &GatewayNamespaceResolver{k8s: k8s, projectOrgs: projectOrgs}
}

// GetGatewayNamespace returns the value of the gateway-namespace annotation
// on the organization namespace that owns the given project, or "" when the
// annotation is unset. Returns an error only when the project→org lookup or
// the org-namespace fetch fails; the deployments handler treats such errors
// as soft failures and falls back to DefaultGatewayNamespace.
func (r *GatewayNamespaceResolver) GetGatewayNamespace(ctx context.Context, project string) (string, error) {
	if r.projectOrgs == nil {
		return "", nil
	}
	org, err := r.projectOrgs.GetProjectOrganization(ctx, project)
	if err != nil {
		return "", err
	}
	if org == "" {
		return "", nil
	}
	ns, err := r.k8s.GetOrganization(ctx, org)
	if err != nil {
		return "", err
	}
	return GetGatewayNamespace(ns), nil
}
