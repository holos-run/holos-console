// legacy_shim.go is a temporary bridge introduced in HOL-619 (parent:
// HOL-615). Before this patch the templates handler received a
// TemplateScopeRef on every request and threaded `(scope, scopeName)`
// through the K8sClient to pick the owning namespace. HOL-619 removed
// the TemplateScope enum from proto in favor of carrying the Kubernetes
// namespace directly; the storage layer (K8sClient) is rewritten in
// HOL-621 to match. This file converts the incoming namespace back to
// `(legacyScope, scopeName)` so the unchanged storage code keeps
// working during the intermediate phases. The entire file is deleted in
// phase 5 (HOL-624) once the storage rewrite lands.
//
// Root-cause note: proto and storage now disagree on their naming. The
// handler owns the conversion and MUST emit
// `connect.CodeInvalidArgument` when a caller sends a namespace the
// resolver cannot classify — that matches the pre-HOL-619 behavior of
// rejecting TEMPLATE_SCOPE_UNSPECIFIED on TemplateScopeRef.
package templates

import (
	"fmt"

	"connectrpc.com/connect"

	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// legacyScope mirrors the removed consolev1.TemplateScope enum. Aliased
// to scopeshim.Scope so the handler can continue to switch on hierarchy
// kind until HOL-621 rewrites the storage layer.
type legacyScope = scopeshim.Scope

const (
	legacyScopeUnspecified  = scopeshim.ScopeUnspecified
	legacyScopeOrganization = scopeshim.ScopeOrganization
	legacyScopeFolder       = scopeshim.ScopeFolder
	legacyScopeProject      = scopeshim.ScopeProject
)

// extractScope converts an incoming namespace to the legacy
// `(scope, scopeName)` pair the K8sClient still expects. Returns
// InvalidArgument when the namespace is empty or cannot be classified,
// matching the pre-HOL-619 contract that rejected
// TEMPLATE_SCOPE_UNSPECIFIED.
func (h *Handler) extractScope(namespace string) (legacyScope, string, error) {
	if namespace == "" {
		return legacyScopeUnspecified, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace is required"))
	}
	if h.k8s == nil || h.k8s.Resolver == nil {
		return legacyScopeUnspecified, "", connect.NewError(connect.CodeInternal, fmt.Errorf("namespace resolver not wired"))
	}
	scope, scopeName, err := scopeshim.FromNamespace(h.k8s.Resolver, namespace)
	if err != nil {
		return legacyScopeUnspecified, "", connect.NewError(connect.CodeInvalidArgument, err)
	}
	return scope, scopeName, nil
}

// namespaceFor is the inverse of extractScope. Used when the handler
// needs to emit a Template / LinkableTemplate with its owning namespace
// populated.
func (h *Handler) namespaceFor(scope legacyScope, scopeName string) string {
	if h.k8s == nil || h.k8s.Resolver == nil {
		return ""
	}
	ns, err := scopeshim.NamespaceFor(h.k8s.Resolver, scope, scopeName)
	if err != nil {
		return ""
	}
	return ns
}

// linkedRefFromProto converts an incoming consolev1.LinkedTemplateRef
// (namespace/name/version) to the legacy (scope, scopeName, name,
// version) shape used by the storage, resolver, and rendering code.
// Returns an error when the ref carries a namespace the resolver cannot
// classify — callers should treat that as InvalidArgument.
func (h *Handler) linkedRefFromProto(ref *consolev1.LinkedTemplateRef) (*scopeshim.LinkedRef, error) {
	if ref == nil {
		return nil, nil
	}
	if h.k8s == nil || h.k8s.Resolver == nil {
		return nil, fmt.Errorf("namespace resolver not wired")
	}
	return scopeshim.LinkedRefFromProto(h.k8s.Resolver, ref)
}

// linkedRefToProto rebuilds a proto LinkedTemplateRef from the legacy
// shim shape. Used on the response path when handlers produce
// LinkedTemplateRef entries from internal storage records.
func (h *Handler) linkedRefToProto(ref *scopeshim.LinkedRef) *consolev1.LinkedTemplateRef {
	if ref == nil {
		return nil
	}
	if h.k8s == nil || h.k8s.Resolver == nil {
		return nil
	}
	out, err := scopeshim.LinkedRefToProto(h.k8s.Resolver, ref)
	if err != nil {
		return nil
	}
	return out
}
