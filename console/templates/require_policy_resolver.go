package templates

import (
	"context"
	"fmt"
	"log/slog"
	"path"

	"github.com/holos-run/holos-console/console/policyresolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// policyRequireRuleResolver is the real, TemplatePolicy-backed
// RequireRuleResolver wired into CreateProject. It walks the ancestor chain
// of the newly created project's namespace (project → folder(s) → org),
// reads TemplatePolicy ConfigMaps from every folder and organization
// namespace on the chain (never from the project namespace itself — HOL-554
// storage-isolation), and emits a RequireRuleMatch for each REQUIRE rule
// whose project_pattern matches the new project's name via path.Match.
//
// deployment_pattern is deliberately ignored at project-creation time: a new
// project has zero Deployments and zero ProjectTemplates, so a rule gated by
// a deployment_pattern has no candidate name to match. The same rule will
// still fire at deployment render time via folderResolver (which does
// evaluate deployment_pattern), so org-wide "REQUIRE this template on every
// deployment" rules continue to work — they just do not pre-populate the
// project namespace at creation time unless they also declare an empty
// deployment_pattern (the "this applies to every target kind" form used by
// project-level required templates).
//
// Wildcard semantics mirror folderResolver.globMatch so policy authors have
// one mental model across both evaluators.
type policyRequireRuleResolver struct {
	lister *policyresolver.AncestorPolicyLister
	// projectNamespaceFor maps a project slug to the Kubernetes
	// namespace name from which the ancestor walk should start. The
	// walker classifies a project namespace as a project and skips it
	// for policy reads; the walker still needs the project namespace as
	// the starting node so it can find the folder/org parents labeled
	// above it.
	projectNamespaceFor func(project string) string
}

// NewPolicyRequireRuleResolver returns a RequireRuleResolver that evaluates
// TemplatePolicy REQUIRE rules against a new project's name, pulling rules
// from ancestor folder and organization namespaces only.
//
// Any nil dependency yields a resolver that returns (nil, nil) — the
// fail-open contract for misconfigured bootstraps, matching
// folderResolver's behavior. At project-creation time, failing open means
// "no REQUIRE rules injected"; the project is still created with whatever
// templates the owner explicitly links. A follow-up render when the first
// deployment is created will re-evaluate policies via folderResolver, so a
// transient bootstrap gap here does not permanently lose the policy.
func NewPolicyRequireRuleResolver(
	lister *policyresolver.AncestorPolicyLister,
	projectNamespaceFor func(project string) string,
) RequireRuleResolver {
	return &policyRequireRuleResolver{
		lister:              lister,
		projectNamespaceFor: projectNamespaceFor,
	}
}

// ResolveRequiredTemplates returns one RequireRuleMatch per REQUIRE rule
// whose project_pattern matches the given project name. Matches are deduped
// by `(scope, scopeName, templateName)` so a template required from both
// the folder and the org above it appears once, not twice.
func (r *policyRequireRuleResolver) ResolveRequiredTemplates(ctx context.Context, org, project string) ([]RequireRuleMatch, error) {
	if r == nil || r.lister == nil || r.projectNamespaceFor == nil {
		slog.WarnContext(ctx, "policy require-rule resolver is misconfigured; returning no matches",
			slog.String("organization", org),
			slog.String("project", project),
			slog.Bool("resolverNil", r == nil),
			slog.Bool("listerNil", r == nil || r.lister == nil),
			slog.Bool("projectNamespaceForNil", r == nil || r.projectNamespaceFor == nil),
		)
		return nil, nil
	}
	if project == "" {
		return nil, nil
	}

	startNs := r.projectNamespaceFor(project)
	rules, err := r.lister.ListRules(ctx, startNs)
	if err != nil {
		return nil, fmt.Errorf("listing ancestor template policies for project %q: %w", project, err)
	}

	type dedupKey struct {
		scope     consolev1.TemplateScope
		scopeName string
		name      string
	}
	seen := make(map[dedupKey]struct{}, len(rules))
	matches := make([]RequireRuleMatch, 0, len(rules))

	for _, rule := range rules {
		if rule == nil {
			continue
		}
		if rule.GetKind() != consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE {
			continue
		}
		target := rule.GetTarget()
		if target == nil {
			continue
		}
		projectPattern := target.GetProjectPattern()
		if projectPattern == "" {
			// An empty project_pattern behaves as "*" everywhere else
			// in the policy surface (validatePolicyRules normalizes
			// it, folderResolver.ruleAppliesTo treats it as "*").
			// Stay consistent so a hand-authored ConfigMap with an
			// omitted field still fires.
			projectPattern = "*"
		}
		if !globMatchesProject(projectPattern, project) {
			continue
		}
		tmpl := rule.GetTemplate()
		if tmpl == nil || tmpl.GetName() == "" {
			continue
		}
		key := dedupKey{scope: tmpl.GetScope(), scopeName: tmpl.GetScopeName(), name: tmpl.GetName()}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		matches = append(matches, RequireRuleMatch{
			Scope:        tmpl.GetScope(),
			ScopeName:    tmpl.GetScopeName(),
			TemplateName: tmpl.GetName(),
		})
	}
	return matches, nil
}

// globMatchesProject mirrors folderResolver.globMatch: pattern failures are
// treated as non-matching rather than propagated, because the policy
// validator rejects invalid patterns at write time and a resolve-time error
// would surface as a cryptic CreateProject failure for what is really a
// stale-data problem.
func globMatchesProject(pattern, subject string) bool {
	if pattern == "" {
		return false
	}
	ok, err := path.Match(pattern, subject)
	if err != nil {
		return false
	}
	return ok
}
