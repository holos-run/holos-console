package policyresolver

import (
	"context"
	"log/slog"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// ResolvedPolicy is the decoded form of a TemplatePolicy ConfigMap bundled
// with the scope identity the resolver needs to match against a binding's
// policy_ref. The resolver uses this shape (rather than a flat rule slice)
// so a binding can select the exact policy it targets — two policies in the
// same ancestor chain can share rule template names, and the binding chooses
// among them by (scope, scope_name, name).
type ResolvedPolicy struct {
	// Name is the policy's DNS-label slug (the ConfigMap's metadata.name).
	Name string
	// Namespace is the folder or organization namespace that owns the
	// policy ConfigMap. Used as the authoritative source of (scope,
	// scope_name) via the resolver's prefix classification — the scope
	// label on the ConfigMap is advisory.
	Namespace string
	// Scope is the TemplateScope derived from Namespace (organization or
	// folder). Project scope is unreachable because project namespaces
	// are skipped during the ancestor walk.
	Scope scopeshim.Scope
	// ScopeName is the folder or organization name derived from
	// Namespace.
	ScopeName string
	// Rules are the parsed REQUIRE/EXCLUDE rules on this policy,
	// preserving the authored order.
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
	unmarshaler  RuleUnmarshaler
}

// NewAncestorPolicyLister returns a lister wired with the given dependencies.
// Any nil dependency yields a lister whose ListRules method returns an empty
// slice without error (fail-open behavior — misconfigured bootstraps must not
// block project creation or render).
func NewAncestorPolicyLister(
	policyLister PolicyListerInNamespace,
	walker WalkerInterface,
	r *resolver.Resolver,
	unmarshaler RuleUnmarshaler,
) *AncestorPolicyLister {
	return &AncestorPolicyLister{
		policyLister: policyLister,
		walker:       walker,
		resolver:     r,
		unmarshaler:  unmarshaler,
	}
}

// ListRules returns every TemplatePolicy rule declared in a folder or
// organization namespace on the ancestor chain starting from startNs. The
// returned rules preserve the walker's order (closest ancestor first) within
// each policy and the policy-list order within each namespace; callers that
// need a deterministic match order should dedup or sort after.
//
// startNs is typically the new project's namespace (project-creation time) or
// the render target's project namespace (render time). Passing a
// folder/organization namespace works too — any project namespace on the
// chain (including the start itself) is skipped.
//
// A misconfigured lister (any nil dependency) returns (nil, nil) — the
// fail-open contract matches `folderResolver.Resolve` so a bootstrap
// misconfiguration degrades to "no policies" rather than "render errors on
// every call".
//
// A walker failure returns (nil, err) so project-creation callers can choose
// whether to fail closed (refuse to create the project) or fail open (create
// without policy-injected templates). Today the project-creation caller
// fails closed because a silent walker failure there would let a project
// sneak in without required templates — a security-relevant outcome that
// deserves explicit handling at the call site.
//
// Individual per-namespace lister or parse errors do not abort traversal;
// they are logged and the namespace is skipped. This matches the resolver
// contract that a single corrupted policy ConfigMap should not prevent
// legitimate policies in peer namespaces from being honored.
func (a *AncestorPolicyLister) ListRules(ctx context.Context, startNs string) ([]*consolev1.TemplatePolicyRule, error) {
	if a == nil || a.policyLister == nil || a.walker == nil || a.resolver == nil || a.unmarshaler == nil {
		slog.WarnContext(ctx, "ancestor policy lister is misconfigured; returning no rules",
			slog.String("startNs", startNs),
			slog.Bool("policyListerNil", a == nil || a.policyLister == nil),
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

	var rules []*consolev1.TemplatePolicyRule
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
		cms, listErr := a.policyLister.ListPoliciesInNamespace(ctx, ns.Name)
		if listErr != nil {
			slog.WarnContext(ctx, "failed to list template policies in ancestor namespace",
				slog.String("namespace", ns.Name),
				slog.Any("error", listErr),
			)
			continue
		}
		for i := range cms {
			cm := &cms[i]
			raw := cm.Annotations[v1alpha2.AnnotationTemplatePolicyRules]
			if raw == "" {
				continue
			}
			parsed, parseErr := a.unmarshaler.UnmarshalRules(raw)
			if parseErr != nil {
				slog.WarnContext(ctx, "failed to parse template policy rules; skipping policy",
					slog.String("namespace", ns.Name),
					slog.String("policy", cm.Name),
					slog.Any("error", parseErr),
				)
				continue
			}
			for _, rule := range parsed {
				if rule == nil {
					continue
				}
				rules = append(rules, rule)
			}
		}
	}
	return rules, nil
}

// ListPolicies returns the parsed TemplatePolicy records declared in each
// folder or organization namespace on the ancestor chain starting from
// startNs. Each returned entry bundles the policy's rules with the (scope,
// scope_name, name) triple derived from its owning namespace so downstream
// consumers can match a binding's policy_ref to the policy it references.
//
// Ordering matches ListRules: closest ancestor first, list order within each
// namespace. Project namespaces are skipped (HOL-554 storage-isolation).
//
// Fail-open and per-namespace error behavior mirrors ListRules. A policy
// whose rules annotation is empty or malformed is skipped with a warning,
// but a malformed scope-prefix classification on the namespace causes the
// whole namespace to be skipped — the resolver has no way to report a
// policy whose scope it cannot identify.
func (a *AncestorPolicyLister) ListPolicies(ctx context.Context, startNs string) ([]*ResolvedPolicy, error) {
	if a == nil || a.policyLister == nil || a.walker == nil || a.resolver == nil || a.unmarshaler == nil {
		slog.WarnContext(ctx, "ancestor policy lister is misconfigured; returning no policies",
			slog.String("startNs", startNs),
			slog.Bool("policyListerNil", a == nil || a.policyLister == nil),
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
		cms, listErr := a.policyLister.ListPoliciesInNamespace(ctx, ns.Name)
		if listErr != nil {
			slog.WarnContext(ctx, "failed to list template policies in ancestor namespace",
				slog.String("namespace", ns.Name),
				slog.Any("error", listErr),
			)
			continue
		}
		for i := range cms {
			cm := &cms[i]
			raw := cm.Annotations[v1alpha2.AnnotationTemplatePolicyRules]
			if raw == "" {
				continue
			}
			parsed, parseErr := a.unmarshaler.UnmarshalRules(raw)
			if parseErr != nil {
				slog.WarnContext(ctx, "failed to parse template policy rules; skipping policy",
					slog.String("namespace", ns.Name),
					slog.String("policy", cm.Name),
					slog.Any("error", parseErr),
				)
				continue
			}
			rules := make([]*consolev1.TemplatePolicyRule, 0, len(parsed))
			for _, rule := range parsed {
				if rule == nil {
					continue
				}
				rules = append(rules, rule)
			}
			out = append(out, &ResolvedPolicy{
				Name:      cm.Name,
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
