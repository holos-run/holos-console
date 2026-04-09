// Package resolver translates user-facing resource names (organizations, folders,
// projects) to Kubernetes namespace names using independently configurable prefixes
// for each resource type.
package resolver

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
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
// Folder namespaces:       {NamespacePrefix}{FolderPrefix}{name}
// Project namespaces:      {NamespacePrefix}{ProjectPrefix}{name}
type Resolver struct {
	NamespacePrefix    string // default "holos-"
	OrganizationPrefix string // default "org-"
	FolderPrefix       string // default "fld-"
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

// FolderNamespace returns the Kubernetes namespace name for a folder.
func (r *Resolver) FolderNamespace(folder string) string {
	return r.NamespacePrefix + r.FolderPrefix + folder
}

// FolderFromNamespace extracts the folder name from a Kubernetes namespace name.
// Returns a *PrefixMismatchError when ns does not start with the expected prefix.
func (r *Resolver) FolderFromNamespace(ns string) (string, error) {
	prefix := r.NamespacePrefix + r.FolderPrefix
	if !strings.HasPrefix(ns, prefix) {
		return "", &PrefixMismatchError{Namespace: ns, Prefix: prefix}
	}
	return strings.TrimPrefix(ns, prefix), nil
}

// ResourceTypeFromNamespace returns the resource kind ("organization", "folder",
// "project") and logical name for the given Kubernetes namespace, based on prefix
// matching. Returns an error when the namespace does not match any known prefix.
// This is used by the hierarchy walker to classify ancestors without an extra
// K8s API call.
func (r *Resolver) ResourceTypeFromNamespace(ns string) (kind, name string, err error) {
	if orgName, e := r.OrgFromNamespace(ns); e == nil {
		return v1alpha2.ResourceTypeOrganization, orgName, nil
	}
	if folderName, e := r.FolderFromNamespace(ns); e == nil {
		return v1alpha2.ResourceTypeFolder, folderName, nil
	}
	if projName, e := r.ProjectFromNamespace(ns); e == nil {
		return v1alpha2.ResourceTypeProject, projName, nil
	}
	return "", "", fmt.Errorf("namespace %q does not match any known prefix (org=%q, folder=%q, project=%q)",
		ns,
		r.NamespacePrefix+r.OrganizationPrefix,
		r.NamespacePrefix+r.FolderPrefix,
		r.NamespacePrefix+r.ProjectPrefix,
	)
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
