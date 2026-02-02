// Package resolver translates user-facing resource names (organizations, projects)
// to Kubernetes namespace names using independently configurable prefixes for each
// resource type.
package resolver

import (
	"fmt"
	"strings"
)

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
	NamespacePrefix    string // default "holos-"
	OrganizationPrefix string // default "org-"
	ProjectPrefix      string // default "prj-"
}

// OrgNamespace returns the Kubernetes namespace name for an organization.
func (r *Resolver) OrgNamespace(org string) string {
	return r.NamespacePrefix + r.OrganizationPrefix + org
}

// OrgFromNamespace extracts the organization name from a Kubernetes namespace name.
// Returns a *PrefixMismatchError when ns does not start with the expected prefix.
// Prefer the OrganizationLabel on the namespace when available.
func (r *Resolver) OrgFromNamespace(ns string) (string, error) {
	prefix := r.NamespacePrefix + r.OrganizationPrefix
	if !strings.HasPrefix(ns, prefix) {
		return "", &PrefixMismatchError{Namespace: ns, Prefix: prefix}
	}
	return strings.TrimPrefix(ns, prefix), nil
}

// ProjectNamespace returns the Kubernetes namespace name for a project.
func (r *Resolver) ProjectNamespace(project string) string {
	return r.NamespacePrefix + r.ProjectPrefix + project
}

// ProjectFromNamespace extracts the project name from a Kubernetes namespace name.
// Returns a *PrefixMismatchError when ns does not start with the expected prefix.
// Prefer the ProjectLabel on the namespace when available.
func (r *Resolver) ProjectFromNamespace(ns string) (string, error) {
	prefix := r.NamespacePrefix + r.ProjectPrefix
	if !strings.HasPrefix(ns, prefix) {
		return "", &PrefixMismatchError{Namespace: ns, Prefix: prefix}
	}
	return strings.TrimPrefix(ns, prefix), nil
}

// PrefixMismatchError is returned when a namespace name does not begin with
// the expected prefix for the resource type being resolved.
type PrefixMismatchError struct {
	Namespace string // the namespace name that was checked
	Prefix    string // the expected prefix that was not found
}

func (e *PrefixMismatchError) Error() string {
	return fmt.Sprintf("namespace %q does not match expected prefix %q", e.Namespace, e.Prefix)
}
