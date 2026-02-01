// Package resolver translates user-facing resource names (organizations, projects)
// to Kubernetes namespace names using a configurable prefix.
package resolver

import "strings"

// Label and annotation constants for resource type identification.
const (
	// ResourceTypeLabel distinguishes organization and project namespaces.
	ResourceTypeLabel = "console.holos.run/resource-type"
	// ResourceTypeOrganization is the resource-type label value for organization namespaces.
	ResourceTypeOrganization = "organization"
	// ResourceTypeProject is the resource-type label value for project namespaces.
	ResourceTypeProject = "project"
	// OrganizationLabel stores the organization name on project namespaces.
	OrganizationLabel = "console.holos.run/organization"
	// ProjectLabel stores the project name on project namespaces.
	ProjectLabel = "console.holos.run/project"
)

// Resolver translates between user-facing resource names and Kubernetes namespace names.
// Organization namespaces: {Prefix}org-{name}
// Project namespaces: {Prefix}{org}-{project}
type Resolver struct {
	Prefix string // default "holos-"
}

// OrgNamespace returns the Kubernetes namespace name for an organization.
func (r *Resolver) OrgNamespace(org string) string {
	return r.Prefix + "org-" + org
}

// OrgFromNamespace extracts the organization name from a Kubernetes namespace name.
func (r *Resolver) OrgFromNamespace(ns string) string {
	return strings.TrimPrefix(ns, r.Prefix+"org-")
}

// ProjectNamespace returns the Kubernetes namespace name for a project within an organization.
func (r *Resolver) ProjectNamespace(org, project string) string {
	return r.Prefix + org + "-" + project
}

// ProjectFromNamespace extracts the project name from a Kubernetes namespace name,
// given the organization name.
func (r *Resolver) ProjectFromNamespace(ns, org string) string {
	return strings.TrimPrefix(ns, r.Prefix+org+"-")
}
