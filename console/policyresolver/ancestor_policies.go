package policyresolver

import (
	"context"
	"log/slog"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// AncestorPolicyLister walks the ancestor chain of a starting namespace and
// collects every TemplatePolicy rule stored in the folder and organization
// namespaces on that chain. Project namespaces are skipped — storing a
// TemplatePolicy in a project namespace is a HOL-554 storage-isolation
// violation, and the loader must never consume such a record even if it
// exists (an attacker could otherwise craft a policy in their own project
// namespace that overrides the platform's constraints).
//
// This helper is shared by the render-time `folderResolver` (which classifies
// rules as REQUIRE vs EXCLUDE and evaluates them against a render target)
// and the project-creation-time `policyRequireRuleResolver` (which evaluates
// REQUIRE rules against the new project's name before any deployment
// exists). Keeping both callers on a single traversal means the
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
