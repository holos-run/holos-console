package policyresolver

import (
	"context"
	"log/slog"

	corev1 "k8s.io/api/core/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// BindingListerInNamespace reports the TemplatePolicyBinding ConfigMaps stored
// in a specific Kubernetes namespace. The folderResolver uses this to fetch
// bindings from each folder or organization namespace in the ancestor chain
// without importing console/templatepolicybindings directly (which would
// create an import cycle once that package depends on console/policyresolver).
//
// Implementations MUST only read from folder and organization namespaces.
// The folderResolver guarantees it never passes a project namespace to this
// method because the ancestor walk skips project-kind namespaces before
// calling the lister, but implementations should still treat a project
// namespace as a programming error and return an empty list.
type BindingListerInNamespace interface {
	ListBindingsInNamespace(ctx context.Context, ns string) ([]corev1.ConfigMap, error)
}

// BindingUnmarshaler decodes the JSON-serialized policy_ref and target_refs
// annotations on a TemplatePolicyBinding ConfigMap into proto values. The
// folderResolver delegates decoding to console/templatepolicybindings so the
// resolver never hard-codes the wire shape.
type BindingUnmarshaler interface {
	UnmarshalPolicyRef(raw string) (*consolev1.LinkedTemplatePolicyRef, error)
	UnmarshalTargetRefs(raw string) ([]*consolev1.TemplatePolicyBindingTargetRef, error)
}

// BindingUnmarshalerAdapter adapts a pair of free-standing functions into a
// BindingUnmarshaler. Use it at wire-up time to pass
// templatepolicybindings.UnmarshalPolicyRef and .UnmarshalTargetRefs without
// defining a one-shot type at the call site.
type BindingUnmarshalerAdapter struct {
	PolicyRefFunc  func(raw string) (*consolev1.LinkedTemplatePolicyRef, error)
	TargetRefsFunc func(raw string) ([]*consolev1.TemplatePolicyBindingTargetRef, error)
}

// UnmarshalPolicyRef satisfies BindingUnmarshaler.
func (a BindingUnmarshalerAdapter) UnmarshalPolicyRef(raw string) (*consolev1.LinkedTemplatePolicyRef, error) {
	if a.PolicyRefFunc == nil {
		return nil, nil
	}
	return a.PolicyRefFunc(raw)
}

// UnmarshalTargetRefs satisfies BindingUnmarshaler.
func (a BindingUnmarshalerAdapter) UnmarshalTargetRefs(raw string) ([]*consolev1.TemplatePolicyBindingTargetRef, error) {
	if a.TargetRefsFunc == nil {
		return nil, nil
	}
	return a.TargetRefsFunc(raw)
}

// ResolvedBinding is the decoded form of a TemplatePolicyBinding ConfigMap,
// keyed with the owning namespace so downstream evaluation can locate the
// bound policy and record which binding contributed a ref. The folder
// resolver consumes a slice of these from AncestorBindingLister.ListBindings.
type ResolvedBinding struct {
	// Name is the binding's DNS-label slug. Stable across updates.
	Name string
	// Namespace is the folder or organization namespace that owns the
	// binding ConfigMap. Used by the resolver when it logs a warning for
	// a binding whose policy_ref does not resolve.
	Namespace string
	// PolicyRef identifies the TemplatePolicy the binding attaches. May
	// be nil if the annotation is missing or malformed — the caller
	// treats that as "no-op" (a warning is logged by the lister).
	PolicyRef *consolev1.LinkedTemplatePolicyRef
	// TargetRefs enumerates the explicit render targets this binding
	// applies its policy to. May be empty; an empty list means the
	// binding does not cover any render target and contributes no refs.
	TargetRefs []*consolev1.TemplatePolicyBindingTargetRef
}

// AncestorBindingLister walks the ancestor chain of a starting namespace and
// collects every TemplatePolicyBinding ConfigMap stored in the folder and
// organization namespaces on that chain. Project namespaces are skipped to
// mirror the HOL-554 storage-isolation guardrail already enforced for
// TemplatePolicy — a binding in a project namespace is a misconfiguration
// that must never be consumed at render time.
//
// This helper is used by the render-time `folderResolver` (HOL-596) to
// evaluate binding-driven REQUIRE/EXCLUDE semantics alongside the legacy
// glob-based TemplatePolicyRule.Target fallback. Centralizing the ancestor
// walk here — and the slog-based error-logging contract that goes with it —
// means the storage-isolation guardrail lives in exactly one place for
// binding reads, matching the shape used by AncestorPolicyLister.
type AncestorBindingLister struct {
	bindingLister BindingListerInNamespace
	walker        WalkerInterface
	resolver      *resolver.Resolver
	unmarshaler   BindingUnmarshaler
}

// NewAncestorBindingLister returns a lister wired with the given dependencies.
// Any nil dependency yields a lister whose ListBindings method returns an
// empty slice without error (fail-open behavior — misconfigured bootstraps
// must not block project creation or render).
func NewAncestorBindingLister(
	bindingLister BindingListerInNamespace,
	walker WalkerInterface,
	r *resolver.Resolver,
	unmarshaler BindingUnmarshaler,
) *AncestorBindingLister {
	return &AncestorBindingLister{
		bindingLister: bindingLister,
		walker:        walker,
		resolver:      r,
		unmarshaler:   unmarshaler,
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
// whether to fall back to the legacy glob path (same behavior as
// AncestorPolicyLister) or surface the failure.
//
// Individual per-namespace lister or parse errors do not abort traversal;
// they are logged and the namespace (or individual binding) is skipped. A
// single corrupted TemplatePolicyBinding ConfigMap must not prevent
// legitimate bindings in peer namespaces from being honored.
func (a *AncestorBindingLister) ListBindings(ctx context.Context, startNs string) ([]*ResolvedBinding, error) {
	if a == nil || a.bindingLister == nil || a.walker == nil || a.resolver == nil || a.unmarshaler == nil {
		slog.WarnContext(ctx, "ancestor binding lister is misconfigured; returning no bindings",
			slog.String("startNs", startNs),
			slog.Bool("bindingListerNil", a == nil || a.bindingLister == nil),
			slog.Bool("walkerNil", a == nil || a.walker == nil),
			slog.Bool("resolverNil", a == nil || a.resolver == nil),
			slog.Bool("unmarshalerNil", a == nil || a.unmarshaler == nil),
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
		cms, listErr := a.bindingLister.ListBindingsInNamespace(ctx, ns.Name)
		if listErr != nil {
			slog.WarnContext(ctx, "failed to list template policy bindings in ancestor namespace",
				slog.String("namespace", ns.Name),
				slog.Any("error", listErr),
			)
			continue
		}
		for i := range cms {
			cm := &cms[i]
			policyRaw := cm.Annotations[v1alpha2.AnnotationTemplatePolicyBindingPolicyRef]
			targetsRaw := cm.Annotations[v1alpha2.AnnotationTemplatePolicyBindingTargetRefs]

			policyRef, policyErr := a.unmarshaler.UnmarshalPolicyRef(policyRaw)
			if policyErr != nil {
				slog.WarnContext(ctx, "failed to parse template policy binding policy_ref; skipping binding",
					slog.String("namespace", ns.Name),
					slog.String("binding", cm.Name),
					slog.Any("error", policyErr),
				)
				continue
			}
			targetRefs, targetsErr := a.unmarshaler.UnmarshalTargetRefs(targetsRaw)
			if targetsErr != nil {
				slog.WarnContext(ctx, "failed to parse template policy binding target_refs; skipping binding",
					slog.String("namespace", ns.Name),
					slog.String("binding", cm.Name),
					slog.Any("error", targetsErr),
				)
				continue
			}

			out = append(out, &ResolvedBinding{
				Name:       cm.Name,
				Namespace:  ns.Name,
				PolicyRef:  policyRef,
				TargetRefs: targetRefs,
			})
		}
	}
	return out, nil
}
