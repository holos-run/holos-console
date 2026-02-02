// Package resolver translates user-facing resource names (organizations, projects)
// to Kubernetes namespace names using independently configurable prefixes for each
// resource type.
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
	// OrganizationLabel stores the organization name on organization and project namespaces.
	OrganizationLabel = "console.holos.run/organization"
	// ProjectLabel stores the project name on project namespaces.
	ProjectLabel = "console.holos.run/project"
)

// Resolver translates between user-facing resource names and Kubernetes namespace names.
// Organization namespaces: {NamespacePrefix}{OrganizationPrefix}{name}
// Project namespaces: {NamespacePrefix}{ProjectPrefix}{name}
type Resolver struct {
	NamespacePrefix    string // default "" (empty, no global prefix)
	OrganizationPrefix string // default "org-"
	ProjectPrefix      string // default "prj-"
}

// OrgNamespace returns the Kubernetes namespace name for an organization.
func (r *Resolver) OrgNamespace(org string) string {
	return r.NamespacePrefix + r.OrganizationPrefix + org
}

// OrgFromNamespace extracts the organization name from a Kubernetes namespace name.
func (r *Resolver) OrgFromNamespace(ns string) string {
	return strings.TrimPrefix(ns, r.NamespacePrefix+r.OrganizationPrefix)
}

// ProjectNamespace returns the Kubernetes namespace name for a project.
func (r *Resolver) ProjectNamespace(project string) string {
	return r.NamespacePrefix + r.ProjectPrefix + project
}

// ProjectFromNamespace extracts the project name from a Kubernetes namespace name.
func (r *Resolver) ProjectFromNamespace(ns string) string {
	return strings.TrimPrefix(ns, r.NamespacePrefix+r.ProjectPrefix)
}
