// Package resolver translates user-facing resource names (organizations, projects)
// to Kubernetes namespace names using independently configurable prefixes for each
// resource type.
package resolver

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
)

// Re-export constants from v1alpha1 so existing importers continue to compile.
// New code should import v1alpha1 directly.
var (
	ResourceTypeLabel        = v1alpha1.LabelResourceType
	ResourceTypeOrganization = v1alpha1.ResourceTypeOrganization
	ResourceTypeProject      = v1alpha1.ResourceTypeProject
	OrganizationLabel        = v1alpha1.LabelOrganization
	ProjectLabel             = v1alpha1.LabelProject
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
