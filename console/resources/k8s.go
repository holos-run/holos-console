// Package resources implements the cross-kind ResourceService introduced in
// HOL-602 (proto in HOL-601). It returns folders and projects together in a
// single flat list — including each entry's root→leaf ancestor path — so a
// navigation tree can render the organization hierarchy without first
// fanning out to ListFolders + ListProjects and stitching the ancestry
// client-side.
//
// This package wraps the existing per-kind K8s clients (folders, projects,
// organizations) instead of talking to the apiserver directly so RBAC and
// label semantics stay defined in exactly one place. ResourceService is
// purely a read-side composition over those primitives.
package resources

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	"github.com/holos-run/holos-console/console/folders"
	"github.com/holos-run/holos-console/console/organizations"
	"github.com/holos-run/holos-console/console/projects"
)

// K8sClient composes the folders / projects / organizations K8s clients so
// the ResourceService handler can list both kinds and walk ancestor display
// names without holding a kubernetes.Interface itself. The composition lets
// label-selector behavior, prefix-mismatch filtering, and namespace
// classification stay defined in the per-kind packages.
type K8sClient struct {
	folders       *folders.K8sClient
	projects      *projects.K8sClient
	organizations *organizations.K8sClient
}

// NewK8sClient builds the composite client from the three per-kind clients.
// All three are required; ListResources walks every level of the hierarchy
// and depends on each one for label-correct namespace fetches.
func NewK8sClient(f *folders.K8sClient, p *projects.K8sClient, o *organizations.K8sClient) *K8sClient {
	return &K8sClient{folders: f, projects: p, organizations: o}
}

// ListFolders returns every folder namespace, optionally filtered to a
// single organization. parentNs is intentionally unset — ResourceService
// returns the full flat list and lets the caller stitch the tree together
// using each entry's ancestor path.
func (c *K8sClient) ListFolders(ctx context.Context, org string) ([]*corev1.Namespace, error) {
	return c.folders.ListFolders(ctx, org, "")
}

// ListProjects returns every project namespace, optionally filtered to a
// single organization. See ListFolders for why parentNs is empty.
func (c *K8sClient) ListProjects(ctx context.Context, org string) ([]*corev1.Namespace, error) {
	return c.projects.ListProjects(ctx, org, "")
}

// GetOrganization returns the org namespace by user-facing name. Used to
// resolve the root PathElement's display name when building Resource
// entries — folder/project namespaces only carry the org's slug, not its
// human-readable display name.
func (c *K8sClient) GetOrganization(ctx context.Context, name string) (*corev1.Namespace, error) {
	return c.organizations.GetOrganization(ctx, name)
}
