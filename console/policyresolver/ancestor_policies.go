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

// ResolvedPolicy is the decoded form of a TemplatePolicy CRD bundled with
// the scope identity the resolver needs to match against a binding's
// policy_ref. The resolver uses this shape (rather than a flat rule slice)
// so a binding can select the exact policy it targets — two policies in the
// same ancestor chain can share rule template names, and the binding chooses
// among them by (scope, scope_name, name).
type ResolvedPolicy struct {
	// Name is the policy's DNS-label slug (the CRD's metadata.name).
	Name string
	// Namespace is the folder or organization namespace that owns the
	// policy CRD. Used as the authoritative source of (scope, scope_name)
	// via the resolver's prefix classification — the scope label on the
	// CRD is advisory.
	Namespace string
	// Scope is the TemplateScope derived from Namespace (organization or
	// folder). Project scope is unreachable because project namespaces
	// are skipped during the ancestor walk.
	Scope scopeshim.Scope
	// ScopeName is the folder or organization name derived from
	// Namespace.
	ScopeName string
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
// startNs. Each returned entry bundles the policy's rules with the (scope,
// scope_name, name) triple derived from its owning namespace so downstream
// consumers can match a binding's policy_ref to the policy it references.
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
		kind, scopeName, kErr := a.resolver.ResourceTypeFromNamespace(ns.Name)
		if kErr != nil {
			continue
		}
		if kind == v1alpha2.ResourceTypeProject {
			continue
		}
		scope := templateScopeForResourceType(kind)
		if scope == scopeshim.ScopeUnspecified {
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
				Scope:     scope,
				ScopeName: scopeName,
				Rules:     rules,
			})
		}
	}
	return out, nil
}

// templateScopeForResourceType maps the resolver's resource-type classification
// of a namespace onto the TemplateScope enum used by binding policy_refs.
// Only organization and folder namespaces store TemplatePolicy objects;
// every other type maps to UNSPECIFIED so callers can skip the entry.
func templateScopeForResourceType(kind string) scopeshim.Scope {
	switch kind {
	case v1alpha2.ResourceTypeOrganization:
		return scopeshim.ScopeOrganization
	case v1alpha2.ResourceTypeFolder:
		return scopeshim.ScopeFolder
	default:
		return scopeshim.ScopeUnspecified
	}
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
			Template: scopeshim.NewLinkedTemplateRef(
				scopeFromTemplateLabel(r.Template.Scope),
				r.Template.ScopeName,
				r.Template.Name,
				r.Template.VersionConstraint,
			),
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

func scopeFromTemplateLabel(label string) scopeshim.Scope {
	switch label {
	case v1alpha2.TemplateScopeOrganization:
		return scopeshim.ScopeOrganization
	case v1alpha2.TemplateScopeFolder:
		return scopeshim.ScopeFolder
	case v1alpha2.TemplateScopeProject:
		return scopeshim.ScopeProject
	default:
		return scopeshim.ScopeUnspecified
	}
}
