// Package scopeshim carries a Go-only compatibility shim for the
// TemplateScope / TemplateScopeRef enum that was removed from proto in
// HOL-619 (parent: HOL-615). Without this patch the Go tree could not
// compile once proto was rewritten to key Template / TemplatePolicy /
// TemplatePolicyBinding resources by `(namespace, name)` alone, because
// the resolver layer (console/policyresolver), the deployments handler,
// the templates handlers, and CLI migration tools all rely on a scope
// discriminator to route reads and writes to organization, folder, and
// project namespaces.
//
// This package is temporary. Phase 5 (HOL-624) removes every call site
// and deletes this package when the storage layer has been fully
// rewritten to operate on namespaces directly (HOL-621 / HOL-622).
//
// Root-cause note: the pre-HOL-619 proto carried two sources of truth
// for "which namespace owns this template" — the resolver prefix map
// and the TemplateScope enum. The parent ticket decided the namespace
// is authoritative. This shim is the minimal Go translation that keeps
// the tree compiling during the intermediate phases where proto is
// namespace-only but storage, resolver, drift-state serialization, and
// migration tooling still think in `(scope, scopeName)` pairs.
package scopeshim

import (
	"fmt"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// NamespaceResolver is the minimal subset of console/resolver.Resolver the
// shim needs to classify a Kubernetes namespace into (Scope, scopeName).
// Defined as an interface so callers can pass the concrete resolver or a
// test double without an import cycle.
type NamespaceResolver interface {
	OrgNamespace(org string) string
	FolderNamespace(folder string) string
	ProjectNamespace(project string) string
	ResourceTypeFromNamespace(ns string) (kind, name string, err error)
}

// defaultResolver is the process-wide NamespaceResolver used by the
// compatibility helpers below. Configured at server startup via
// SetDefaultResolver; tests configure it via SetDefaultResolverForTest.
// Package-level state is acceptable for this temporary shim because
// there is exactly one resolver per server process and every call site
// shares it. The package-level global goes away with the shim in phase
// 5 (HOL-624).
var defaultResolver NamespaceResolver

// SetDefaultResolver registers the process-wide namespace resolver used
// by RefScope / RefScopeName and friends. Call once during server
// bootstrap, before any handler is wired.
func SetDefaultResolver(r NamespaceResolver) {
	defaultResolver = r
}

// DefaultResolver returns the currently registered resolver. Returns
// nil when SetDefaultResolver has not been called — callers SHOULD
// guard against that to avoid panicking in test harnesses that don't
// wire a resolver.
func DefaultResolver() NamespaceResolver {
	return defaultResolver
}

// Scope is the legacy TemplateScope discriminator, kept only so Go code
// can continue to switch on hierarchy kind until the namespace-only
// refactor in HOL-621 / HOL-622 lands. Numeric values mirror the removed
// consolev1.TemplateScope enum so stored label strings round-trip without
// behavior change.
type Scope int

const (
	// ScopeUnspecified mirrors TEMPLATE_SCOPE_UNSPECIFIED.
	ScopeUnspecified Scope = 0
	// ScopeOrganization mirrors TEMPLATE_SCOPE_ORGANIZATION.
	ScopeOrganization Scope = 1
	// ScopeFolder mirrors TEMPLATE_SCOPE_FOLDER.
	ScopeFolder Scope = 2
	// ScopeProject mirrors TEMPLATE_SCOPE_PROJECT.
	ScopeProject Scope = 3
)

// String returns the canonical TemplateScope enum name for diagnostic
// logging and error messages.
func (s Scope) String() string {
	switch s {
	case ScopeOrganization:
		return "TEMPLATE_SCOPE_ORGANIZATION"
	case ScopeFolder:
		return "TEMPLATE_SCOPE_FOLDER"
	case ScopeProject:
		return "TEMPLATE_SCOPE_PROJECT"
	default:
		return "TEMPLATE_SCOPE_UNSPECIFIED"
	}
}

// NamespaceFor returns the Kubernetes namespace for `(scope, scopeName)`.
func NamespaceFor(r NamespaceResolver, scope Scope, scopeName string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("namespace resolver is required")
	}
	switch scope {
	case ScopeOrganization:
		return r.OrgNamespace(scopeName), nil
	case ScopeFolder:
		return r.FolderNamespace(scopeName), nil
	case ScopeProject:
		return r.ProjectNamespace(scopeName), nil
	default:
		return "", fmt.Errorf("unknown template scope %v", scope)
	}
}

// FromNamespace classifies a Kubernetes namespace into (Scope, scopeName)
// using the resolver prefix map. Returns ScopeUnspecified with a non-nil
// error when the namespace does not match any known prefix.
func FromNamespace(r NamespaceResolver, ns string) (Scope, string, error) {
	if r == nil {
		return ScopeUnspecified, "", fmt.Errorf("namespace resolver is required")
	}
	if ns == "" {
		return ScopeUnspecified, "", fmt.Errorf("namespace is required")
	}
	kind, name, err := r.ResourceTypeFromNamespace(ns)
	if err != nil {
		return ScopeUnspecified, "", err
	}
	switch kind {
	case v1alpha2.ResourceTypeOrganization:
		return ScopeOrganization, name, nil
	case v1alpha2.ResourceTypeFolder:
		return ScopeFolder, name, nil
	case v1alpha2.ResourceTypeProject:
		return ScopeProject, name, nil
	default:
		return ScopeUnspecified, "", fmt.Errorf("namespace %q classified as unknown resource type %q", ns, kind)
	}
}

// LabelValue returns the v1alpha2 label string for the scope. Empty for
// ScopeUnspecified so callers can treat an absent label uniformly.
func LabelValue(scope Scope) string {
	switch scope {
	case ScopeOrganization:
		return v1alpha2.TemplateScopeOrganization
	case ScopeFolder:
		return v1alpha2.TemplateScopeFolder
	case ScopeProject:
		return v1alpha2.TemplateScopeProject
	default:
		return ""
	}
}

// ScopeFromLabel reverses LabelValue so code reading persisted
// ConfigMap labels can reconstruct the scope enum.
func ScopeFromLabel(label string) Scope {
	switch label {
	case v1alpha2.TemplateScopeOrganization:
		return ScopeOrganization
	case v1alpha2.TemplateScopeFolder:
		return ScopeFolder
	case v1alpha2.TemplateScopeProject:
		return ScopeProject
	default:
		return ScopeUnspecified
	}
}

// LinkedRef mirrors the pre-HOL-619 LinkedTemplateRef shape with its
// scope discriminator. Go code that still needs to reason about scope
// carries this struct alongside the proto LinkedTemplateRef so render
// paths, drift evaluation, and migration helpers keep compiling. The
// proto message itself now carries only `(namespace, name,
// version_constraint)`; this struct is the one-stop translator.
type LinkedRef struct {
	Scope             Scope
	ScopeName         string
	Name              string
	VersionConstraint string
}

// LinkedRefFromProto converts a proto LinkedTemplateRef to the Go shim
// form, classifying the carried namespace into (Scope, scopeName).
// Returns an error when the namespace does not match any known prefix.
func LinkedRefFromProto(r NamespaceResolver, ref *consolev1.LinkedTemplateRef) (*LinkedRef, error) {
	if ref == nil {
		return nil, nil
	}
	scope, scopeName, err := FromNamespace(r, ref.GetNamespace())
	if err != nil {
		return nil, err
	}
	return &LinkedRef{
		Scope:             scope,
		ScopeName:         scopeName,
		Name:              ref.GetName(),
		VersionConstraint: ref.GetVersionConstraint(),
	}, nil
}

// LinkedRefToProto rebuilds a proto LinkedTemplateRef from a shim
// LinkedRef, resolving the scope back to a Kubernetes namespace.
func LinkedRefToProto(r NamespaceResolver, ref *LinkedRef) (*consolev1.LinkedTemplateRef, error) {
	if ref == nil {
		return nil, nil
	}
	ns, err := NamespaceFor(r, ref.Scope, ref.ScopeName)
	if err != nil {
		return nil, err
	}
	return &consolev1.LinkedTemplateRef{
		Namespace:         ns,
		Name:              ref.Name,
		VersionConstraint: ref.VersionConstraint,
	}, nil
}

// RefScope classifies a proto LinkedTemplateRef via the
// default resolver and returns the legacy Scope enum. Returns
// ScopeUnspecified when the ref is nil, the resolver is unwired, or
// the carried namespace does not match any known prefix. Callers that
// need to distinguish those three cases should use FromNamespace
// directly with an explicit resolver.
func RefScope(ref *consolev1.LinkedTemplateRef) Scope {
	if ref == nil || defaultResolver == nil {
		return ScopeUnspecified
	}
	scope, _, err := FromNamespace(defaultResolver, ref.GetNamespace())
	if err != nil {
		return ScopeUnspecified
	}
	return scope
}

// RefScopeName returns the legacy scope_name for a proto
// LinkedTemplateRef via the default resolver. Returns "" when
// classification fails. Pair with RefScope when you need both values.
func RefScopeName(ref *consolev1.LinkedTemplateRef) string {
	if ref == nil || defaultResolver == nil {
		return ""
	}
	_, name, err := FromNamespace(defaultResolver, ref.GetNamespace())
	if err != nil {
		return ""
	}
	return name
}

// PolicyRefScope classifies a LinkedTemplatePolicyRef's namespace via
// the default resolver and returns the legacy Scope enum.
func PolicyRefScope(ref *consolev1.LinkedTemplatePolicyRef) Scope {
	if ref == nil || defaultResolver == nil {
		return ScopeUnspecified
	}
	scope, _, err := FromNamespace(defaultResolver, ref.GetNamespace())
	if err != nil {
		return ScopeUnspecified
	}
	return scope
}

// PolicyRefScopeName returns the legacy scope_name for a
// LinkedTemplatePolicyRef via the default resolver.
func PolicyRefScopeName(ref *consolev1.LinkedTemplatePolicyRef) string {
	if ref == nil || defaultResolver == nil {
		return ""
	}
	_, name, err := FromNamespace(defaultResolver, ref.GetNamespace())
	if err != nil {
		return ""
	}
	return name
}

// NewLinkedTemplateRef builds a proto LinkedTemplateRef from the
// legacy `(scope, scopeName, name, versionConstraint)` quartet using
// the default resolver. Emits a ref with an empty namespace when the
// scope cannot be resolved — callers that need to surface that error
// should use LinkedRefToProto with an explicit resolver.
func NewLinkedTemplateRef(scope Scope, scopeName, name, versionConstraint string) *consolev1.LinkedTemplateRef {
	ns := ""
	if defaultResolver != nil {
		if resolved, err := NamespaceFor(defaultResolver, scope, scopeName); err == nil {
			ns = resolved
		}
	}
	return &consolev1.LinkedTemplateRef{
		Namespace:         ns,
		Name:              name,
		VersionConstraint: versionConstraint,
	}
}

// NewLinkedTemplatePolicyRef builds a proto LinkedTemplatePolicyRef
// from `(scope, scopeName, name)` using the default resolver.
func NewLinkedTemplatePolicyRef(scope Scope, scopeName, name string) *consolev1.LinkedTemplatePolicyRef {
	ns := ""
	if defaultResolver != nil {
		if resolved, err := NamespaceFor(defaultResolver, scope, scopeName); err == nil {
			ns = resolved
		}
	}
	return &consolev1.LinkedTemplatePolicyRef{
		Namespace: ns,
		Name:      name,
	}
}
