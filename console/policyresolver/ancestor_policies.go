package policyresolver

import (
	"context"
	"log/slog"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// ResolvedPolicy is the decoded form of a TemplatePolicy CRD bundled with
// the (namespace, name) identity the resolver needs to match against a
// binding's policy_ref. Two policies in the same ancestor chain can share
// names; a binding selects among them by (namespace, name).
type ResolvedPolicy struct {
	// Name is the policy's DNS-label slug (the CRD's metadata.name).
	Name string
	// Namespace is the folder or organization namespace that owns the
	// policy CRD. It is the authoritative identifier a binding's
	// policy_ref matches on.
	Namespace string
	// Rules are the parsed REQUIRE/EXCLUDE rules on this policy,
	// preserving the authored order. HOL-662 populates them from the
	// CRD spec directly.
	Rules []*consolev1.TemplatePolicyRule
}

// AncestorPolicyLister walks the ancestor chain of a starting namespace and
// collects every TemplatePolicy rule stored in the folder and organization
// namespaces on that chain. Project namespaces are skipped — storing a
// TemplatePolicy in a project namespace is a HOL-554 storage-isolation
// violation, and the loader must never consume such a record even if it
// exists (an attacker could otherwise craft a policy in their own project
// namespace that overrides the platform's constraints).
//
// This helper is used by the render-time `folderResolver` (which classifies
// rules as REQUIRE vs EXCLUDE and evaluates them against a render target).
// HOL-582 removed the project-creation-time require-rule evaluator that
// previously shared this traversal; render-time remains the sole enforcement
// path for REQUIRE rules. Centralizing the ancestor walk here means the
// storage-isolation guardrail — and the slog-based error-logging contract
// that goes with it — is implemented exactly once.
type AncestorPolicyLister struct {
	policyLister PolicyListerInNamespace
	walker       WalkerInterface
	resolver     *resolver.Resolver
}

// NewAncestorPolicyLister returns a lister wired with the given dependencies.
// Any nil dependency yields a lister whose ListPolicies method returns an
// empty slice without error (fail-open behavior — misconfigured bootstraps
// must not block project creation or render).
func NewAncestorPolicyLister(
	policyLister PolicyListerInNamespace,
	walker WalkerInterface,
	r *resolver.Resolver,
) *AncestorPolicyLister {
	return &AncestorPolicyLister{
		policyLister: policyLister,
		walker:       walker,
		resolver:     r,
	}
}

// ListPolicies returns the parsed TemplatePolicy records declared in each
// folder or organization namespace on the ancestor chain starting from
// startNs. Each returned entry bundles the policy's rules with the
// (namespace, name) pair downstream consumers use to match a binding's
// policy_ref to the policy it references.
//
// Ordering: closest ancestor first, list order within each namespace.
// Project namespaces are skipped (HOL-554 storage-isolation).
//
// A misconfigured lister (any nil dependency) returns (nil, nil) — the
// fail-open contract matches `folderResolver.Resolve` so a bootstrap
// misconfiguration degrades to "no policies" rather than "render errors on
// every call".
//
// A walker failure returns (nil, err) so callers can choose whether to fail
// closed or fail open at the call site.
//
// Individual per-namespace lister errors do not abort traversal; they are
// logged and the namespace is skipped. A namespace whose scope-prefix
// classification fails is skipped — the resolver has no way to report a
// policy whose scope it cannot identify.
//
// HOL-622 converted the policy lister to return a pointer slice; this method
// passes each pointer through unchanged so downstream ResolvedPolicy values
// observe the same addressable CRD that the informer cache returns.
func (a *AncestorPolicyLister) ListPolicies(ctx context.Context, startNs string) ([]*ResolvedPolicy, error) {
	if a == nil || a.policyLister == nil || a.walker == nil || a.resolver == nil {
		slog.WarnContext(ctx, "ancestor policy lister is misconfigured; returning no policies",
			slog.String("startNs", startNs),
			slog.Bool("policyListerNil", a == nil || a.policyLister == nil),
			slog.Bool("walkerNil", a == nil || a.walker == nil),
			slog.Bool("resolverNil", a == nil || a.resolver == nil),
		)
		return nil, nil
	}

	ancestors, err := a.walker.WalkAncestors(ctx, startNs)
	if err != nil {
		return nil, err
	}

	var out []*ResolvedPolicy
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
		if kind != v1alpha2.ResourceTypeOrganization && kind != v1alpha2.ResourceTypeFolder {
			// Non-policy-bearing resource type (e.g. the render-state or
			// template-policy-binding kind itself); skip quietly.
			continue
		}
		items, listErr := a.policyLister.ListPoliciesInNamespace(ctx, ns.Name)
		if listErr != nil {
			slog.WarnContext(ctx, "failed to list template policies in ancestor namespace",
				slog.String("namespace", ns.Name),
				slog.Any("error", listErr),
			)
			continue
		}
		for _, p := range items {
			if p == nil {
				continue
			}
			rules := crdRulesToProto(p.Spec.Rules)
			out = append(out, &ResolvedPolicy{
				Name:      p.Name,
				Namespace: ns.Name,
				Rules:     rules,
			})
		}
	}
	return out, nil
}

// crdRulesToProto converts CRD spec rules into proto TemplatePolicyRule
// values. This mirrors templatepolicies.CRDRulesToProto but is duplicated
// here to avoid an import cycle: console/templatepolicies imports
// console/policyresolver for the resolver interface. The mapping is simple
// enough that duplication is cheaper than restructuring the cycle.
func crdRulesToProto(rules []templatesv1alpha1.TemplatePolicyRule) []*consolev1.TemplatePolicyRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]*consolev1.TemplatePolicyRule, 0, len(rules))
	for i := range rules {
		r := &rules[i]
		out = append(out, &consolev1.TemplatePolicyRule{
			Kind: crdKindToProto(r.Kind),
			Template: &consolev1.LinkedTemplateRef{
				Namespace:         r.Template.Namespace,
				Name:              r.Template.Name,
				VersionConstraint: r.Template.VersionConstraint,
			},
		})
	}
	return out
}

func crdKindToProto(k templatesv1alpha1.TemplatePolicyKind) consolev1.TemplatePolicyKind {
	switch k {
	case templatesv1alpha1.TemplatePolicyKindRequire:
		return consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE
	case templatesv1alpha1.TemplatePolicyKindExclude:
		return consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE
	default:
		return consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_UNSPECIFIED
	}
}
