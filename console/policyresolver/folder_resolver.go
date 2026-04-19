package policyresolver

import (
	"context"
	"log/slog"

	corev1 "k8s.io/api/core/v1"

	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// PolicyListerInNamespace reports the TemplatePolicy ConfigMaps stored in a
// specific Kubernetes namespace. The folderResolver uses this to fetch
// policies from each folder or organization namespace in the ancestor chain
// without importing console/templatepolicies directly (which would create an
// import cycle once that package depends on console/policyresolver).
//
// Implementations MUST only read from folder and organization namespaces.
// The folderResolver guarantees it never passes a project namespace to this
// method because the ancestor walk skips project-kind namespaces before
// calling the lister, but implementations should still treat a project
// namespace as a programming error and return an empty list.
type PolicyListerInNamespace interface {
	ListPoliciesInNamespace(ctx context.Context, ns string) ([]corev1.ConfigMap, error)
}

// RuleUnmarshaler decodes the JSON-serialized TemplatePolicy rules annotation
// into proto rule values. The folderResolver delegates decoding to the
// templatepolicies package so the resolver never hard-codes the wire shape.
type RuleUnmarshaler interface {
	UnmarshalRules(raw string) ([]*consolev1.TemplatePolicyRule, error)
}

// RuleUnmarshalerFunc is the adapter that turns a plain function into a
// RuleUnmarshaler. Use it at wire-up time to pass
// templatepolicies.UnmarshalRules.
type RuleUnmarshalerFunc func(raw string) ([]*consolev1.TemplatePolicyRule, error)

// UnmarshalRules satisfies RuleUnmarshaler by invoking the wrapped function.
func (f RuleUnmarshalerFunc) UnmarshalRules(raw string) ([]*consolev1.TemplatePolicyRule, error) {
	return f(raw)
}

// folderResolver is the real PolicyResolver implementation introduced in
// HOL-567. It evaluates TemplatePolicy REQUIRE/EXCLUDE rules by walking from
// the project namespace up the ancestor chain and reading policies from
// folder and organization namespaces only. Project namespaces are skipped so
// a project owner cannot tamper with the policies the platform means to
// constrain them with (HOL-554 storage-isolation guardrail).
//
// HOL-600 removed the legacy glob-based TemplatePolicyRule.Target
// evaluation path. A rule contributes to the current render target only
// when a TemplatePolicyBinding names its owning policy and selects that
// target (matching on (kind, name, project_name)). Policies not covered
// by any matching binding for the render target contribute nothing; an
// annotated `target` field still present on a stale ConfigMap is ignored.
type folderResolver struct {
	policyLister       PolicyListerInNamespace
	walker             WalkerInterface
	resolver           *resolver.Resolver
	unmarshaler        RuleUnmarshaler
	bindingLister      BindingListerInNamespace
	bindingUnmarshaler BindingUnmarshaler
	// ancestorLister encapsulates the ancestor-chain traversal and
	// folder-namespace-only filter used by render-time REQUIRE/EXCLUDE
	// evaluation. HOL-582 removed the project-creation-time resolver that
	// previously shared this helper; render-time is now the sole caller.
	// When any of policyLister, walker, resolver, or unmarshaler is nil the
	// fail-open branch in Resolve short-circuits before ancestorLister is
	// consulted, so it is safe to construct from possibly-nil deps here.
	ancestorLister *AncestorPolicyLister
	// ancestorBindings encapsulates the same ancestor walk for
	// TemplatePolicyBinding ConfigMaps. Nil when bindingLister or
	// bindingUnmarshaler is nil; Resolve treats a nil ancestorBindings
	// as "no bindings exist", which post-HOL-600 means "no rules
	// contribute". This is the safe fail-open behavior: a wire-up that
	// forgot to provide a binding lister returns the caller's explicit
	// refs unchanged rather than misapplying rules.
	ancestorBindings *AncestorBindingLister
}

// NewFolderResolver returns a folderResolver wired with the policy
// (rule-side) dependencies only. Post-HOL-600 this resolver degrades to
// returning the caller's explicit refs unchanged: no binding lister is
// wired, so no rule can contribute. The constructor is retained for
// pre-binding test wire-ups that want to assert the fail-open behavior.
// Production code must use NewFolderResolverWithBindings so the binding
// evaluation path is live.
//
// Passing nil for any of the four rule-side arguments yields a resolver
// that also returns the explicit refs unchanged (equivalent to
// noopResolver), matching the fail-open contract in Resolve.
func NewFolderResolver(
	policyLister PolicyListerInNamespace,
	walker WalkerInterface,
	r *resolver.Resolver,
	unmarshaler RuleUnmarshaler,
) PolicyResolver {
	return &folderResolver{
		policyLister:   policyLister,
		walker:         walker,
		resolver:       r,
		unmarshaler:    unmarshaler,
		ancestorLister: NewAncestorPolicyLister(policyLister, walker, r, unmarshaler),
	}
}

// NewFolderResolverWithBindings wires a resolver that evaluates
// TemplatePolicyBinding objects as the sole render-target selector
// (HOL-600). A binding whose target_refs match the current render target
// dereferences its policy_ref and injects (REQUIRE) / removes (EXCLUDE)
// the bound policy's template refs. Policies that no matching binding
// names for the current target contribute nothing — the pre-HOL-600
// legacy glob Target path is gone.
//
// Passing a nil binding lister or unmarshaler reduces the resolver to
// "no rules contribute"; the caller's explicit refs pass through
// unchanged. Passing a nil rule stack falls through to noopResolver
// semantics as before.
func NewFolderResolverWithBindings(
	policyLister PolicyListerInNamespace,
	walker WalkerInterface,
	r *resolver.Resolver,
	unmarshaler RuleUnmarshaler,
	bindingLister BindingListerInNamespace,
	bindingUnmarshaler BindingUnmarshaler,
) PolicyResolver {
	fr := &folderResolver{
		policyLister:       policyLister,
		walker:             walker,
		resolver:           r,
		unmarshaler:        unmarshaler,
		bindingLister:      bindingLister,
		bindingUnmarshaler: bindingUnmarshaler,
		ancestorLister:     NewAncestorPolicyLister(policyLister, walker, r, unmarshaler),
	}
	if bindingLister != nil && bindingUnmarshaler != nil && walker != nil && r != nil {
		fr.ancestorBindings = NewAncestorBindingLister(bindingLister, walker, r, bindingUnmarshaler)
	}
	return fr
}

// Resolve returns the effective set of LinkedTemplateRef values for the
// render target at `(projectNs, targetKind, targetName)`. The computation is:
//
//	result = explicitRefs ∪ REQUIRE-injected − EXCLUDE-removed
//
// Ordering: EXCLUDE runs after REQUIRE so a policy that both REQUIREs and
// EXCLUDEs the same template (an admin typo) still removes the template.
// Ordering: EXCLUDE cannot remove a template that the owner explicitly
// linked — that rejection happens at policy-author time in
// CreateTemplatePolicy/UpdateTemplatePolicy; at resolve time EXCLUDE only
// removes templates that REQUIRE added.
//
// Dedup key for the final slice is `(scope, scope_name, name)`. Two
// explicit-ref entries that share a key are kept as the first-seen
// occurrence so the resolver never silently drops a version constraint that
// the caller set deliberately.
//
// When any dependency is nil the resolver degrades to returning explicitRefs
// unchanged and logs a warning. This mirrors the noopResolver behavior so a
// misconfigured bootstrap fails open (render proceeds) rather than failing
// closed (every render errors).
func (r *folderResolver) Resolve(
	ctx context.Context,
	projectNs string,
	targetKind TargetKind,
	targetName string,
	explicitRefs []*consolev1.LinkedTemplateRef,
) ([]*consolev1.LinkedTemplateRef, error) {
	if r == nil || r.policyLister == nil || r.walker == nil || r.resolver == nil || r.unmarshaler == nil {
		slog.WarnContext(ctx, "folder resolver is misconfigured; returning explicit refs unchanged",
			slog.String("projectNs", projectNs),
			slog.String("targetName", targetName),
			slog.Bool("policyListerNil", r == nil || r.policyLister == nil),
			slog.Bool("walkerNil", r == nil || r.walker == nil),
			slog.Bool("resolverNil", r == nil || r.resolver == nil),
			slog.Bool("unmarshalerNil", r == nil || r.unmarshaler == nil),
		)
		return explicitRefs, nil
	}
	if projectNs == "" {
		return explicitRefs, nil
	}

	// Resolve the project slug from the namespace. Bindings key targets
	// on (kind, name, project_name); a non-project start (preview of an
	// org/folder-scope template) has no project name to match, so the
	// binding path also contributes nothing. The real render code paths
	// that invoke Resolve always pass a project namespace today.
	project, projectErr := r.resolver.ProjectFromNamespace(projectNs)
	if projectErr != nil {
		project = ""
	}

	// Collect every TemplatePolicy declared in a folder or organization
	// namespace on the ancestor chain, bundled with the (scope, scope_name,
	// name) triple a binding uses to reference a specific policy. The
	// ancestor-policy lister handles the HOL-554 storage-isolation skip
	// (project namespaces are never read) and per-namespace parse/list
	// errors. The returned slice preserves closest-ancestor-first order so
	// REQUIRE injections closer to the project continue to appear later
	// in the effective set (the dedup key is stable, so ordering only
	// affects first-seen wins for explicit refs — which are set before
	// this loop).
	policies, walkErr := r.ancestorLister.ListPolicies(ctx, projectNs)
	if walkErr != nil {
		// Degrade gracefully: a walker failure at resolve time should
		// not block the render. Log and return the explicit refs so the
		// caller can still produce the minimal render.
		slog.WarnContext(ctx, "ancestor walk failed during policy resolution; returning explicit refs unchanged",
			slog.String("projectNs", projectNs),
			slog.Any("error", walkErr),
		)
		return explicitRefs, nil
	}

	// Collect the bindings from the same ancestor chain. A nil
	// ancestorBindings (no WithBindings wire-up) is the fail-open
	// degenerate case: no rule can contribute, so the caller's
	// explicit refs pass through unchanged.
	var bindings []*ResolvedBinding
	if r.ancestorBindings != nil {
		bs, bErr := r.ancestorBindings.ListBindings(ctx, projectNs)
		if bErr != nil {
			slog.WarnContext(ctx, "ancestor binding walk failed during policy resolution; returning explicit refs unchanged",
				slog.String("projectNs", projectNs),
				slog.Any("error", bErr),
			)
			return explicitRefs, nil
		}
		bindings = bs
	}

	// Classify which policies at least one matching binding names for
	// the current render target. Only these policies contribute rules
	// to the effective set — this is the sole selection mechanism
	// post-HOL-600.
	coveredPolicies := make(map[policyKey]struct{})
	for _, b := range bindings {
		if b == nil || b.PolicyRef == nil {
			continue
		}
		if !bindingAppliesTo(b, project, targetKind, targetName) {
			continue
		}
		// HOL-619 collapsed scope_ref into a flat (namespace, name) pair on
		// LinkedTemplatePolicyRef. Classify the carried namespace back into
		// (scope, scopeName) via the shim so the rest of this loop (and the
		// storage layer it feeds) keeps working until HOL-621/HOL-622
		// rewrite them.
		if b.PolicyRef.GetNamespace() == "" {
			slog.WarnContext(ctx, "template policy binding has no policy_ref namespace; treating as no-op",
				slog.String("bindingNamespace", b.Namespace),
				slog.String("binding", b.Name),
			)
			continue
		}
		coveredPolicies[policyKey{
			scope:     scopeshim.PolicyRefScope(b.PolicyRef),
			scopeName: scopeshim.PolicyRefScopeName(b.PolicyRef),
			name:      b.PolicyRef.GetName(),
		}] = struct{}{}
	}

	// Warn on dangling binding → missing policy references. A binding
	// that names a policy not in the ancestor chain is a
	// misconfiguration; contributing no rules (which the loop below
	// already does for a missing policy key) is the contracted no-op.
	existingPolicyKeys := make(map[policyKey]struct{}, len(policies))
	for _, p := range policies {
		if p == nil {
			continue
		}
		existingPolicyKeys[policyKey{scope: p.Scope, scopeName: p.ScopeName, name: p.Name}] = struct{}{}
	}
	for key := range coveredPolicies {
		if _, ok := existingPolicyKeys[key]; !ok {
			slog.WarnContext(ctx, "template policy binding references a policy that does not exist in the ancestor chain; treating as no-op",
				slog.String("policyScope", key.scope.String()),
				slog.String("policyScopeName", key.scopeName),
				slog.String("policyName", key.name),
			)
		}
	}

	// Collect REQUIRE and EXCLUDE rules only from policies a matching
	// binding covers for this render target. Every other policy in the
	// ancestor chain contributes nothing — pre-HOL-600 the legacy
	// glob Target path would have evaluated those policies' rules
	// against the rule's (project_pattern, deployment_pattern).
	var requireRules []*consolev1.TemplatePolicyRule
	var excludeRules []*consolev1.TemplatePolicyRule
	for _, p := range policies {
		if p == nil {
			continue
		}
		if _, covered := coveredPolicies[policyKey{scope: p.Scope, scopeName: p.ScopeName, name: p.Name}]; !covered {
			continue
		}
		for _, rule := range p.Rules {
			if rule == nil {
				continue
			}
			switch rule.GetKind() {
			case consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE:
				requireRules = append(requireRules, rule)
			case consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE:
				excludeRules = append(excludeRules, rule)
			}
		}
	}

	// Start the effective set with the caller's explicit refs, deduped
	// on `(scope, scope_name, name)`. Any explicit ref that a REQUIRE
	// rule also matches stays in the set; we only add new entries.
	effective, effectiveSet, explicitKeys := dedupRefs(explicitRefs)

	// Inject REQUIRE matches. A REQUIRE rule selected by a binding
	// contributes its template ref, carrying the rule-author-declared
	// version constraint so a REQUIRE rule can pin the platform-forced
	// template to a specific semver band.
	for _, rule := range requireRules {
		tmpl := rule.GetTemplate()
		if tmpl == nil || tmpl.GetName() == "" {
			continue
		}
		key := keyForTemplateRef(scopeshim.RefScope(tmpl), scopeshim.RefScopeName(tmpl), tmpl.GetName())
		if _, ok := effectiveSet[key]; ok {
			continue
		}
		ref := scopeshim.NewLinkedTemplateRef(
			scopeshim.RefScope(tmpl),
			scopeshim.RefScopeName(tmpl),
			tmpl.GetName(),
			tmpl.GetVersionConstraint(),
		)
		effective = append(effective, ref)
		effectiveSet[key] = ref
	}

	// Apply EXCLUDE rules. EXCLUDE only removes refs that REQUIRE
	// added — the owner-linked refs in explicitKeys are protected.
	// The resolver enforces this protection so a policy accidentally
	// authored against an org-mandated linked template does not
	// silently override the deliberate choice.
	if len(excludeRules) == 0 {
		return effective, nil
	}
	filtered := make([]*consolev1.LinkedTemplateRef, 0, len(effective))
	for _, ref := range effective {
		if ref == nil {
			continue
		}
		key := keyForRefProto(ref)
		// Never remove an explicit (owner-linked) ref.
		if _, ownerLinked := explicitKeys[key]; ownerLinked {
			filtered = append(filtered, ref)
			continue
		}
		excluded := false
		for _, rule := range excludeRules {
			tmpl := rule.GetTemplate()
			if tmpl == nil {
				continue
			}
			if keyForTemplateRef(scopeshim.RefScope(tmpl), scopeshim.RefScopeName(tmpl), tmpl.GetName()) == key {
				excluded = true
				break
			}
		}
		if !excluded {
			filtered = append(filtered, ref)
		}
	}
	return filtered, nil
}

// policyKey is the lookup key a binding's policy_ref resolves to: the
// (scope, scope_name, name) triple derived from the owning policy's
// namespace + metadata.name. The resolver uses it to decide which policies
// are covered by at least one matching binding for a render target.
type policyKey struct {
	scope     scopeshim.Scope
	scopeName string
	name      string
}

// bindingAppliesTo reports whether any of a binding's target_refs selects
// the render target at `(project, targetKind, targetName)`. Match semantics
// (AC bullet in HOL-596):
//
//   - kind=PROJECT_TEMPLATE: matches when the render target is a
//     project-scope template with the same name AND the binding's
//     project_name equals the target's project name. The proto contract
//     (HOL-593) requires project_name on PROJECT_TEMPLATE target refs;
//     binding handlers reject empty project_name on create/update, so
//     the match is sound.
//   - kind=DEPLOYMENT: matches when the render target is a Deployment
//     with the same name AND the binding's project_name equals the
//     target's project name.
//
// A binding with no target_refs never matches (correctly — an empty target
// list declares intent to attach zero render targets).
func bindingAppliesTo(b *ResolvedBinding, project string, targetKind TargetKind, targetName string) bool {
	if b == nil {
		return false
	}
	var wantKind consolev1.TemplatePolicyBindingTargetKind
	switch targetKind {
	case TargetKindProjectTemplate:
		wantKind = consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE
	case TargetKindDeployment:
		wantKind = consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT
	default:
		return false
	}
	for _, tr := range b.TargetRefs {
		if tr == nil {
			continue
		}
		if tr.GetKind() != wantKind {
			continue
		}
		if tr.GetName() != targetName {
			continue
		}
		if tr.GetProjectName() != project {
			continue
		}
		return true
	}
	return false
}

// RefKey is the dedup/comparison key for a LinkedTemplateRef. Exposed so
// other packages (tests, drift-detection helpers) can reason about set
// membership without re-implementing the triple.
type RefKey struct {
	Scope     scopeshim.Scope
	ScopeName string
	Name      string
}

// keyForRefProto is the package-internal dedup key (not exported).
func keyForRefProto(r *consolev1.LinkedTemplateRef) RefKey {
	return RefKey{
		Scope:     scopeshim.RefScope(r),
		ScopeName: scopeshim.RefScopeName(r),
		Name:      r.GetName(),
	}
}

// keyForTemplateRef builds a RefKey from raw scope/name fields. Used when
// materializing a REQUIRE rule's template ref into an effective entry.
func keyForTemplateRef(scope scopeshim.Scope, scopeName, name string) RefKey {
	return RefKey{Scope: scope, ScopeName: scopeName, Name: name}
}

// dedupRefs returns (deduped, deduped-set, explicit-set). deduped preserves
// first-seen order; deduped-set indexes deduped by its `(scope, scopeName,
// name)` triple. explicit-set is a snapshot of the keys in deduped before
// REQUIRE injection, so EXCLUDE can tell which refs the owner chose.
func dedupRefs(refs []*consolev1.LinkedTemplateRef) ([]*consolev1.LinkedTemplateRef, map[RefKey]*consolev1.LinkedTemplateRef, map[RefKey]struct{}) {
	out := make([]*consolev1.LinkedTemplateRef, 0, len(refs))
	set := make(map[RefKey]*consolev1.LinkedTemplateRef, len(refs))
	explicit := make(map[RefKey]struct{}, len(refs))
	for _, r := range refs {
		if r == nil {
			continue
		}
		key := keyForRefProto(r)
		if _, ok := set[key]; ok {
			continue
		}
		out = append(out, r)
		set[key] = r
		explicit[key] = struct{}{}
	}
	return out, set, explicit
}

