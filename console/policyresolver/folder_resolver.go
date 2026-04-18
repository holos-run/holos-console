package policyresolver

import (
	"context"
	"log/slog"
	"path"

	corev1 "k8s.io/api/core/v1"

	"github.com/holos-run/holos-console/console/resolver"
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
// HOL-596 extended the resolver to honor TemplatePolicyBinding objects
// alongside the legacy glob-based TemplatePolicyRule.Target selector. A
// binding whose target_refs match the current render target contributes
// every rule in its bound policy; the bound policy's glob Target filter is
// ignored for targets the binding covers (bindings win on conflict). Rules
// in policies that are NOT covered by any matching binding for this target
// continue to be evaluated via their glob Target, so the resolver is safely
// additive against existing fixtures.
//
// Wildcard matching (legacy glob path) uses the same `path.Match` semantics
// as validatePolicyRules so the resolver honors the glob forms already
// accepted at policy-create time.
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
	// as "no bindings exist" and falls back to the pure legacy glob
	// evaluation path. This lets the resolver stay backward-compatible
	// with wire-ups that have not yet rolled in binding support (tests,
	// pre-HOL-595 fixtures).
	ancestorBindings *AncestorBindingLister
}

// NewFolderResolver returns a folderResolver wired with the given dependencies.
// All four rule-side arguments are required; passing nil for any of them
// yields a resolver that falls back to returning the caller's explicit refs
// unchanged (equivalent to noopResolver) so tests and test-only wire-ups
// continue to work without crashing.
//
// For binding support, use NewFolderResolverWithBindings to additionally
// attach a BindingListerInNamespace and BindingUnmarshaler. A resolver
// constructed via NewFolderResolver skips the binding evaluation path
// entirely and behaves exactly as it did before HOL-596 — every rule is
// evaluated via its glob TemplatePolicyRule.Target.
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

// NewFolderResolverWithBindings wires a resolver that evaluates both
// TemplatePolicyBinding objects (HOL-596) and the legacy
// TemplatePolicyRule.Target glob fallback. The binding path takes precedence:
// any rule in a policy that a matching binding covers is evaluated as if
// the binding re-selected its own targets, and the glob Target on that rule
// is ignored for the render targets the binding names. Rules in policies
// with no matching binding for the current render target continue to fall
// back to the glob evaluation, so the two paths coexist through HOL-599 /
// HOL-600 without breaking existing fixtures.
//
// Passing a nil binding lister or unmarshaler degrades cleanly to the
// legacy-only behavior returned by NewFolderResolver. Passing a nil rule
// stack falls through to noopResolver semantics as before.
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

	// Resolve the project slug from the namespace so patterns can be
	// matched against it. If the caller passed a non-project namespace
	// (e.g. a preview of an org-level template), there is no project to
	// match against; REQUIRE rules keyed on project_pattern only match a
	// real project, so we still walk the ancestor chain but skip
	// project-pattern matching.
	project, projectErr := r.resolver.ProjectFromNamespace(projectNs)
	if projectErr != nil {
		// Non-project start (preview or org/folder render). The real
		// render code paths that invoke Resolve always pass a project
		// namespace today, but keep the branch for completeness.
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
	// ancestorBindings (no WithBindings wire-up) means we skip the
	// binding-driven path entirely and fall back to the legacy glob
	// evaluation — behavior identical to the pre-HOL-596 resolver.
	var bindings []*ResolvedBinding
	if r.ancestorBindings != nil {
		bs, bErr := r.ancestorBindings.ListBindings(ctx, projectNs)
		if bErr != nil {
			slog.WarnContext(ctx, "ancestor binding walk failed during policy resolution; falling back to legacy glob path",
				slog.String("projectNs", projectNs),
				slog.Any("error", bErr),
			)
		} else {
			bindings = bs
		}
	}

	// Classify which (policy, binding) pairs select the current render
	// target. coveredPolicies records every (policyScope, policyScopeName,
	// policyName) triple that at least one matching binding names — those
	// policies' rules are evaluated under binding semantics (glob Target
	// ignored for this render target). Every other policy's rules fall
	// back to their glob Target filter, preserving the legacy behavior
	// until HOL-599 migrates existing globs and HOL-600 removes the
	// fallback entirely.
	coveredPolicies := make(map[policyKey]struct{})
	for _, b := range bindings {
		if b == nil || b.PolicyRef == nil {
			continue
		}
		if !bindingAppliesTo(b, project, targetKind, targetName) {
			continue
		}
		scopeRef := b.PolicyRef.GetScopeRef()
		if scopeRef == nil {
			continue
		}
		coveredPolicies[policyKey{
			scope:     scopeRef.GetScope(),
			scopeName: scopeRef.GetScopeName(),
			name:      b.PolicyRef.GetName(),
		}] = struct{}{}
	}

	// Validate each covered-policy entry actually resolves to a real
	// policy in the ancestor chain. A binding that points at a
	// nonexistent policy is a misconfiguration; logging once per
	// offending binding (rather than per-render) would require tracking
	// recent warnings — the ticket's contract is that such a binding is
	// a no-op and does not fail the render, which the per-render log
	// here already satisfies. We do not remove the entry from
	// coveredPolicies because a missing policy contributes no rules
	// anyway; the loop below simply finds no matching ResolvedPolicy.
	existingPolicyKeys := make(map[policyKey]struct{}, len(policies))
	for _, p := range policies {
		if p == nil {
			continue
		}
		existingPolicyKeys[policyKey{scope: p.Scope, scopeName: p.ScopeName, name: p.Name}] = struct{}{}
	}
	for _, b := range bindings {
		if b == nil || b.PolicyRef == nil {
			continue
		}
		if !bindingAppliesTo(b, project, targetKind, targetName) {
			continue
		}
		scopeRef := b.PolicyRef.GetScopeRef()
		if scopeRef == nil {
			slog.WarnContext(ctx, "template policy binding has no policy_ref scope; treating as no-op",
				slog.String("bindingNamespace", b.Namespace),
				slog.String("binding", b.Name),
			)
			continue
		}
		key := policyKey{
			scope:     scopeRef.GetScope(),
			scopeName: scopeRef.GetScopeName(),
			name:      b.PolicyRef.GetName(),
		}
		if _, ok := existingPolicyKeys[key]; !ok {
			slog.WarnContext(ctx, "template policy binding references a policy that does not exist in the ancestor chain; treating as no-op",
				slog.String("bindingNamespace", b.Namespace),
				slog.String("binding", b.Name),
				slog.String("policyScope", key.scope.String()),
				slog.String("policyScopeName", key.scopeName),
				slog.String("policyName", key.name),
			)
		}
	}

	// Classify rules into REQUIRE and EXCLUDE lists. Rules are tagged
	// with their parent policy so the evaluation loop below can decide
	// whether to honor the glob Target filter (no covering binding) or
	// ignore it (binding covers this target for this policy).
	type scopedRule struct {
		rule             *consolev1.TemplatePolicyRule
		policyCoveredVia bool // true iff this rule's parent policy is covered by a matching binding
	}
	var requireRules []scopedRule
	var excludeRules []scopedRule
	for _, p := range policies {
		if p == nil {
			continue
		}
		_, covered := coveredPolicies[policyKey{scope: p.Scope, scopeName: p.ScopeName, name: p.Name}]
		for _, rule := range p.Rules {
			if rule == nil {
				continue
			}
			sr := scopedRule{rule: rule, policyCoveredVia: covered}
			switch rule.GetKind() {
			case consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE:
				requireRules = append(requireRules, sr)
			case consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE:
				excludeRules = append(excludeRules, sr)
			}
		}
	}

	// ruleAppliesForRender is the unified predicate: if a binding covers
	// this policy for the current render target, the rule's own glob
	// Target is bypassed and the rule applies unconditionally (the
	// binding already made the selection decision). Otherwise fall
	// back to legacy glob evaluation.
	ruleAppliesForRender := func(sr scopedRule) bool {
		if sr.policyCoveredVia {
			return true
		}
		return ruleAppliesTo(sr.rule, project, targetKind, targetName)
	}

	// Start the effective set with the caller's explicit refs, deduped
	// on `(scope, scope_name, name)`. Any explicit ref that a REQUIRE
	// rule also matches stays in the set; we only add new entries.
	effective, effectiveSet, explicitKeys := dedupRefs(explicitRefs)

	// Inject REQUIRE matches. Each rule that applies to the current
	// render target (either via a matching binding or via a matching
	// glob Target) contributes its template ref. The injected ref
	// carries the rule-author-declared version constraint (if any) so
	// a REQUIRE rule can pin the platform-forced template to a specific
	// semver band.
	for _, sr := range requireRules {
		if !ruleAppliesForRender(sr) {
			continue
		}
		tmpl := sr.rule.GetTemplate()
		if tmpl == nil || tmpl.GetName() == "" {
			continue
		}
		key := keyForTemplateRef(tmpl.GetScope(), tmpl.GetScopeName(), tmpl.GetName())
		if _, ok := effectiveSet[key]; ok {
			continue
		}
		ref := &consolev1.LinkedTemplateRef{
			Scope:             tmpl.GetScope(),
			ScopeName:         tmpl.GetScopeName(),
			Name:              tmpl.GetName(),
			VersionConstraint: tmpl.GetVersionConstraint(),
		}
		effective = append(effective, ref)
		effectiveSet[key] = ref
	}

	// Apply EXCLUDE rules. EXCLUDE only removes refs that REQUIRE
	// added — the owner-linked refs in explicitKeys are protected.
	// The resolver enforces this protection so a policy accidentally
	// authored against an org-mandated linked template does not silently
	// override the deliberate choice; CreateTemplatePolicy /
	// UpdateTemplatePolicy is the place to raise a FailedPrecondition
	// when such a collision is authored.
	if len(excludeRules) == 0 {
		return effective, nil
	}
	filtered := make([]*consolev1.LinkedTemplateRef, 0, len(effective))
	for _, ref := range effective {
		if ref == nil {
			continue
		}
		key := keyForRefProto(ref)
		// Never remove an explicit (owner-linked) ref — the policy-
		// author RPC already rejects this combination.
		if _, ownerLinked := explicitKeys[key]; ownerLinked {
			filtered = append(filtered, ref)
			continue
		}
		excluded := false
		for _, sr := range excludeRules {
			if !ruleAppliesForRender(sr) {
				continue
			}
			tmpl := sr.rule.GetTemplate()
			if tmpl == nil {
				continue
			}
			if keyForTemplateRef(tmpl.GetScope(), tmpl.GetScopeName(), tmpl.GetName()) == key {
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
	scope     consolev1.TemplateScope
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
	Scope     consolev1.TemplateScope
	ScopeName string
	Name      string
}

// keyForRefProto is the package-internal dedup key (not exported).
func keyForRefProto(r *consolev1.LinkedTemplateRef) RefKey {
	return RefKey{
		Scope:     r.GetScope(),
		ScopeName: r.GetScopeName(),
		Name:      r.GetName(),
	}
}

// keyForTemplateRef builds a RefKey from raw scope/name fields. Used when
// materializing a REQUIRE rule's template ref into an effective entry.
func keyForTemplateRef(scope consolev1.TemplateScope, scopeName, name string) RefKey {
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

// ruleAppliesTo reports whether a rule's target selects the render target at
// `(project, targetKind, targetName)`. Rules that declare a non-empty
// deployment pattern only apply when the render target is a deployment; a
// project-template preview ignores those rules by design (a "mandatory
// deployment template" has no meaning for the project-template render
// surface).
//
// A rule whose deployment pattern is the empty string applies to both
// target kinds, matching the original HOL-557 acceptance-criteria wording
// that a project-level rule (no deployment filter) also forces templates
// onto project-scope renders.
func ruleAppliesTo(rule *consolev1.TemplatePolicyRule, project string, targetKind TargetKind, targetName string) bool {
	target := rule.GetTarget()
	if target == nil {
		return false
	}

	projectPattern := target.GetProjectPattern()
	if projectPattern == "" {
		// No project filter means "every project". Treat an empty
		// pattern the same as "*" so a hand-authored ConfigMap that
		// omits the field still matches.
		projectPattern = "*"
	}
	if !globMatch(projectPattern, project) {
		return false
	}

	deploymentPattern := target.GetDeploymentPattern()
	if deploymentPattern == "" {
		// Applies to both deployments and project-template renders.
		return true
	}

	// With a deployment pattern set, the rule only matches deployment
	// render targets. Project-template previews are outside the rule's
	// selection surface.
	if targetKind != TargetKindDeployment {
		return false
	}
	return globMatch(deploymentPattern, targetName)
}

// globMatch wraps path.Match and is tolerant of malformed patterns: a pattern
// that fails to compile is treated as non-matching rather than propagated,
// because the policy validator already rejects invalid patterns at write time
// (per HOL-556), and a resolve-time error would surface as a cryptic render
// failure for what is really a stale-data problem. The subjects here are DNS
// labels (project/template/deployment names) so path.Match — which uses `/`
// as the separator — is the correct (and only) matcher.
func globMatch(pattern, subject string) bool {
	if pattern == "" {
		return false
	}
	ok, err := path.Match(pattern, subject)
	if err != nil {
		return false
	}
	return ok
}
