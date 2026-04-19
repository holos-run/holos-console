package policyresolver

import (
	"context"
	"log/slog"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// BindingListerInNamespace reports the TemplatePolicyBinding CRD objects
// stored in a specific Kubernetes namespace. The folderResolver uses this
// to fetch bindings from each folder or organization namespace in the
// ancestor chain without importing console/templatepolicybindings directly
// (which would create an import cycle once that package depends on
// console/policyresolver).
//
// Implementations MUST only read from folder and organization namespaces.
// The folderResolver guarantees it never passes a project namespace to this
// method because the ancestor walk skips project-kind namespaces before
// calling the lister, but implementations should still treat a project
// namespace as a programming error and return an empty list.
//
// HOL-662 migrated the return type from corev1.ConfigMap to the CRD; the
// CEL ValidatingAdmissionPolicy (HOL-618) is now the authoritative
// enforcement point for the HOL-554 storage-isolation guardrail.
//
// HOL-622 converted the return shape from a value slice to a pointer slice so
// the ancestor walker and the folderResolver share the same addressable
// binding value through the cache boundary. This keeps the resolver-side
// loops index-free and mirrors the PolicyListerInNamespace shape.
type BindingListerInNamespace interface {
	ListBindingsInNamespace(ctx context.Context, ns string) ([]*templatesv1alpha1.TemplatePolicyBinding, error)
}

// ResolvedBinding is the decoded form of a TemplatePolicyBinding CRD, keyed
// with the owning namespace so downstream evaluation can locate the bound
// policy and record which binding contributed a ref. The folder resolver
// consumes a slice of these from AncestorBindingLister.ListBindings.
type ResolvedBinding struct {
	// Name is the binding's DNS-label slug. Stable across updates.
	Name string
	// Namespace is the folder or organization namespace that owns the
	// binding CRD. Used by the resolver when it logs a warning for a
	// binding whose policy_ref does not resolve.
	Namespace string
	// PolicyRef identifies the TemplatePolicy the binding attaches. May
	// be nil if the binding's spec.policy_ref is empty — the caller
	// treats that as "no-op" (a warning is logged by the lister).
	PolicyRef *consolev1.LinkedTemplatePolicyRef
	// TargetRefs enumerates the explicit render targets this binding
	// applies its policy to. May be empty; an empty list means the
	// binding does not cover any render target and contributes no refs.
	TargetRefs []*consolev1.TemplatePolicyBindingTargetRef
}

// AncestorBindingLister walks the ancestor chain of a starting namespace and
// collects every TemplatePolicyBinding CRD stored in the folder and
// organization namespaces on that chain. Project namespaces are skipped to
// mirror the HOL-554 storage-isolation guardrail already enforced for
// TemplatePolicy — a binding in a project namespace is a misconfiguration
// that must never be consumed at render time.
//
// This helper is used by the render-time `folderResolver` (HOL-596) to
// evaluate binding-driven REQUIRE/EXCLUDE semantics. Centralizing the
// ancestor walk here — and the slog-based error-logging contract that goes
// with it — means the storage-isolation guardrail lives in exactly one
// place for binding reads, matching the shape used by AncestorPolicyLister.
type AncestorBindingLister struct {
	bindingLister BindingListerInNamespace
	walker        WalkerInterface
	resolver      *resolver.Resolver
}

// NewAncestorBindingLister returns a lister wired with the given dependencies.
// Any nil dependency yields a lister whose ListBindings method returns an
// empty slice without error (fail-open behavior — misconfigured bootstraps
// must not block project creation or render).
func NewAncestorBindingLister(
	bindingLister BindingListerInNamespace,
	walker WalkerInterface,
	r *resolver.Resolver,
) *AncestorBindingLister {
	return &AncestorBindingLister{
		bindingLister: bindingLister,
		walker:        walker,
		resolver:      r,
	}
}

// ListBindings returns every TemplatePolicyBinding declared in a folder or
// organization namespace on the ancestor chain starting from startNs. The
// returned bindings preserve the walker's order (closest ancestor first)
// within each namespace and the lister's order within each namespace;
// callers that need a deterministic evaluation order should dedupe or sort
// after.
//
// A misconfigured lister (any nil dependency) returns (nil, nil) — the
// fail-open contract mirrors folderResolver.Resolve so a bootstrap
// misconfiguration degrades to "no bindings" rather than "render errors on
// every call".
//
// A walker failure returns (nil, err) so render-time callers can decide
// how to surface the failure (same behavior as AncestorPolicyLister).
//
// Individual per-namespace lister errors do not abort traversal; they are
// logged and the namespace is skipped. A single corrupted
// TemplatePolicyBinding must not prevent legitimate bindings in peer
// namespaces from being honored.
func (a *AncestorBindingLister) ListBindings(ctx context.Context, startNs string) ([]*ResolvedBinding, error) {
	if a == nil || a.bindingLister == nil || a.walker == nil || a.resolver == nil {
		slog.WarnContext(ctx, "ancestor binding lister is misconfigured; returning no bindings",
			slog.String("startNs", startNs),
			slog.Bool("bindingListerNil", a == nil || a.bindingLister == nil),
			slog.Bool("walkerNil", a == nil || a.walker == nil),
			slog.Bool("resolverNil", a == nil || a.resolver == nil),
		)
		return nil, nil
	}

	ancestors, err := a.walker.WalkAncestors(ctx, startNs)
	if err != nil {
		return nil, err
	}

	var out []*ResolvedBinding
	for _, ns := range ancestors {
		if ns == nil {
			continue
		}
		kind, _, kErr := a.resolver.ResourceTypeFromNamespace(ns.Name)
		if kErr != nil {
			continue
		}
		if kind == v1alpha2.ResourceTypeProject {
			continue
		}
		items, listErr := a.bindingLister.ListBindingsInNamespace(ctx, ns.Name)
		if listErr != nil {
			slog.WarnContext(ctx, "failed to list template policy bindings in ancestor namespace",
				slog.String("namespace", ns.Name),
				slog.Any("error", listErr),
			)
			continue
		}
		for _, b := range items {
			if b == nil {
				continue
			}
			out = append(out, &ResolvedBinding{
				Name:       b.Name,
				Namespace:  ns.Name,
				PolicyRef:  crdPolicyRefToProto(b.Spec.PolicyRef),
				TargetRefs: crdTargetRefsToProto(b.Spec.TargetRefs),
			})
		}
	}
	return out, nil
}

// crdPolicyRefToProto converts the CRD's policy_ref spec field into a proto
// LinkedTemplatePolicyRef. Mirrors templatepolicybindings.CRDPolicyRefToProto;
// duplicated here to avoid an import cycle with console/templatepolicybindings.
func crdPolicyRefToProto(ref templatesv1alpha1.LinkedTemplatePolicyRef) *consolev1.LinkedTemplatePolicyRef {
	if ref.Name == "" && ref.ScopeName == "" && ref.Scope == "" {
		return nil
	}
	return scopeshim.NewLinkedTemplatePolicyRef(
		scopeFromPolicyRefLabel(ref.Scope),
		ref.ScopeName,
		ref.Name,
	)
}

// crdTargetRefsToProto converts the CRD's target_refs spec field into proto
// TemplatePolicyBindingTargetRef values. Mirrors
// templatepolicybindings.CRDTargetRefsToProto.
func crdTargetRefsToProto(refs []templatesv1alpha1.TemplatePolicyBindingTargetRef) []*consolev1.TemplatePolicyBindingTargetRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]*consolev1.TemplatePolicyBindingTargetRef, 0, len(refs))
	for i := range refs {
		r := &refs[i]
		out = append(out, &consolev1.TemplatePolicyBindingTargetRef{
			Kind:        targetKindCRDToProto(r.Kind),
			Name:        r.Name,
			ProjectName: r.ProjectName,
		})
	}
	return out
}

func scopeFromPolicyRefLabel(label string) scopeshim.Scope {
	switch label {
	case "organization":
		return scopeshim.ScopeOrganization
	case "folder":
		return scopeshim.ScopeFolder
	default:
		return scopeshim.ScopeUnspecified
	}
}

func targetKindCRDToProto(k templatesv1alpha1.TemplatePolicyBindingTargetKind) consolev1.TemplatePolicyBindingTargetKind {
	switch k {
	case templatesv1alpha1.TemplatePolicyBindingTargetKindProjectTemplate:
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE
	case templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment:
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT
	default:
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_UNSPECIFIED
	}
}
