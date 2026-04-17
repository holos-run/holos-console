package policyresolver

import (
	"context"
	"log/slog"
	"path"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
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
// Wildcard matching uses the same `path.Match` semantics as
// validatePolicyRules so the resolver honors the glob forms already accepted
// at policy-create time.
type folderResolver struct {
	policyLister PolicyListerInNamespace
	walker       WalkerInterface
	resolver     *resolver.Resolver
	unmarshaler  RuleUnmarshaler
}

// NewFolderResolver returns a folderResolver wired with the given dependencies.
// All four arguments are required; passing nil for any of them yields a
// resolver that falls back to returning the caller's explicit refs unchanged
// (equivalent to noopResolver) so tests and test-only wire-ups continue to
// work without crashing.
func NewFolderResolver(
	policyLister PolicyListerInNamespace,
	walker WalkerInterface,
	r *resolver.Resolver,
	unmarshaler RuleUnmarshaler,
) PolicyResolver {
	return &folderResolver{
		policyLister: policyLister,
		walker:       walker,
		resolver:     r,
		unmarshaler:  unmarshaler,
	}
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

	ancestors, walkErr := r.walker.WalkAncestors(ctx, projectNs)
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

	// Collect REQUIRE/EXCLUDE rules from folder and organization
	// namespaces only. ancestors[0] is the starting namespace; skip any
	// namespace whose resource type is project (including the start when
	// it is itself a project namespace).
	var requireRules []*consolev1.TemplatePolicyRule
	var excludeRules []*consolev1.TemplatePolicyRule
	for _, ns := range ancestors {
		if ns == nil {
			continue
		}
		kind, _, kErr := r.resolver.ResourceTypeFromNamespace(ns.Name)
		if kErr != nil {
			continue
		}
		if kind == v1alpha2.ResourceTypeProject {
			continue
		}
		cms, listErr := r.policyLister.ListPoliciesInNamespace(ctx, ns.Name)
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
			rules, parseErr := r.unmarshaler.UnmarshalRules(raw)
			if parseErr != nil {
				slog.WarnContext(ctx, "failed to parse template policy rules; skipping policy",
					slog.String("namespace", ns.Name),
					slog.String("policy", cm.Name),
					slog.Any("error", parseErr),
				)
				continue
			}
			for _, rule := range rules {
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
	}

	// Start the effective set with the caller's explicit refs, deduped
	// on `(scope, scope_name, name)`. Any explicit ref that a REQUIRE
	// rule also matches stays in the set; we only add new entries.
	effective, effectiveSet, explicitKeys := dedupRefs(explicitRefs)

	// Inject REQUIRE matches. Each rule that matches `(project,
	// targetKind, targetName)` contributes its template ref. The
	// injected ref carries the rule-author-declared version constraint
	// (if any) so a REQUIRE rule can pin the platform-forced template
	// to a specific semver band.
	for _, rule := range requireRules {
		if !ruleAppliesTo(rule, project, targetKind, targetName) {
			continue
		}
		tmpl := rule.GetTemplate()
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
		for _, rule := range excludeRules {
			if !ruleAppliesTo(rule, project, targetKind, targetName) {
				continue
			}
			tmpl := rule.GetTemplate()
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

// globMatch wraps path.Match and filepath.Match into a single function that
// is tolerant of malformed patterns: a pattern that fails to compile is
// treated as non-matching rather than propagated, because the policy
// validator already rejects invalid patterns at write time, and a resolve-
// time error would surface as a cryptic render failure for what is really
// a stale-data problem.
func globMatch(pattern, subject string) bool {
	if pattern == "" {
		return false
	}
	ok, err := path.Match(pattern, subject)
	if err == nil {
		return ok
	}
	// Fall back to filepath.Match. Both wrap the same matcher underneath
	// but surfacing two attempts guards against platform-specific errors
	// on exotic pattern syntax.
	ok, err = filepath.Match(pattern, subject)
	if err != nil {
		return false
	}
	return ok
}

