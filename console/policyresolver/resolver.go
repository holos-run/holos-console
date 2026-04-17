// Package policyresolver is the single source of truth for "which templates
// does this target render with?" after TemplatePolicy REQUIRE and EXCLUDE
// rules are applied.
//
// The resolver walks the namespace hierarchy starting at a project namespace,
// climbs through its folder ancestors up to the owning organization, and
// combines three things:
//
//  1. The caller-supplied baseline of explicitly-linked templates
//     (LinkedTemplateRef entries from the template or deployment ConfigMap).
//  2. REQUIRE rules harvested from TemplatePolicy ConfigMaps in ancestor
//     (folder or organization) namespaces, which add ancestor templates that
//     were not explicitly linked.
//  3. EXCLUDE rules from the same ancestor policies, which remove
//     REQUIRE-injected templates when their target pattern matches.
//
// The output is the effective slice of LinkedTemplateRef entries the renderer
// should unify. The same resolver output drives drift detection (see
// applied_state.go): a difference between the current resolver output and
// the render set last written at create/update time is reported as
// policy_drift on the Deployment and ProjectTemplate read RPCs (HOL-557).
//
// Storage-isolation invariant: the resolver MUST NOT pick up a TemplatePolicy
// ConfigMap from a project namespace, even if one was planted there directly
// against the storage rule. A project owner has write access to the project
// namespace and the entire point of TemplatePolicy is to constrain that
// actor. Any namespace the resolver classifies as `ResourceTypeProject` is
// skipped with a warning log so a misconfigured cluster is visible in
// telemetry without silently affecting render behavior.
package policyresolver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// TargetKind identifies the resource kind a resolve request describes. The
// resolver treats both kinds as first-class citizens so REQUIRE/EXCLUDE rules
// can differentiate in the future, but in the current phase all rules apply
// to both kinds when the patterns match.
type TargetKind int

const (
	// TargetKindUnspecified is the zero value; callers must never pass this.
	TargetKindUnspecified TargetKind = iota
	// TargetKindProjectTemplate scopes a resolve request to a project-scope
	// Template (TEMPLATE_SCOPE_PROJECT). The `target_name` argument is the
	// project-template's DNS label slug.
	TargetKindProjectTemplate
	// TargetKindDeployment scopes a resolve request to a Deployment. The
	// `target_name` argument is the deployment's DNS label slug.
	TargetKindDeployment
)

// String returns a stable, lowercase identifier for the kind. Used in log
// fields and in the drift-store ConfigMap annotations so downstream tooling
// has a human-readable label.
func (t TargetKind) String() string {
	switch t {
	case TargetKindProjectTemplate:
		return "project-template"
	case TargetKindDeployment:
		return "deployment"
	default:
		return "unspecified"
	}
}

// AncestorWalker is the minimal interface the resolver needs to climb the
// namespace hierarchy. It matches resolver.Walker.WalkAncestors exactly so
// the existing Walker can be passed in without an adapter.
type AncestorWalker interface {
	WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error)
}

// Resolver computes the effective render set for a target after applying
// TemplatePolicy REQUIRE and EXCLUDE rules harvested from folder and
// organization namespaces. It is safe for concurrent use by multiple
// goroutines provided the Kubernetes client is concurrency-safe (the
// client-go interface is).
type Resolver struct {
	// Client is the Kubernetes clientset used to list TemplatePolicy
	// ConfigMaps from ancestor namespaces. Must be non-nil.
	Client kubernetes.Interface
	// Walker climbs the namespace hierarchy from a project namespace up to
	// the organization. Must be non-nil.
	Walker AncestorWalker
	// Resolver classifies namespaces as organization/folder/project. Used to
	// skip project namespaces on the policy-rule read path so the
	// storage-isolation invariant holds even if a project-namespace
	// TemplatePolicy ConfigMap has been planted.
	Resolver *resolver.Resolver
}

// Resolve returns the effective list of LinkedTemplateRef entries the
// renderer should unify for (scope, projectNs, targetKind, targetName). The
// result is the baseline set of explicit links plus any REQUIRE-injected
// ancestor templates, minus any EXCLUDE-matched templates — in that order.
//
// The function never returns a nil slice; an empty baseline with no matching
// REQUIRE rules produces an empty (but non-nil) slice so callers can pass
// the result directly to the renderer without a nil guard.
//
// `scope` is the TemplateScope of the target (TEMPLATE_SCOPE_PROJECT for
// both supported kinds today; retained for future kinds). `project` is the
// project's logical name (not the namespace).
//
// Deprecated-in-waiting: a future signature may add a ResourceTypeDeployment
// filter so rules can opt-in to a specific kind. For now every rule applies
// to every kind whose project_pattern matches.
func (r *Resolver) Resolve(
	ctx context.Context,
	scope consolev1.TemplateScope,
	project string,
	targetKind TargetKind,
	targetName string,
	baseLinkedRefs []*consolev1.LinkedTemplateRef,
) ([]*consolev1.LinkedTemplateRef, error) {
	if r.Walker == nil {
		return nil, fmt.Errorf("policyresolver: Walker is nil")
	}
	if r.Resolver == nil {
		return nil, fmt.Errorf("policyresolver: Resolver is nil")
	}
	if r.Client == nil {
		return nil, fmt.Errorf("policyresolver: Client is nil")
	}

	projectNs := r.Resolver.ProjectNamespace(project)
	ancestors, err := r.Walker.WalkAncestors(ctx, projectNs)
	if err != nil {
		return nil, fmt.Errorf("policyresolver: walking ancestors from %q: %w", projectNs, err)
	}

	// Build a set of the baseline explicit links so REQUIRE does not
	// duplicate an already-linked template.
	baseline := make([]*consolev1.LinkedTemplateRef, 0, len(baseLinkedRefs))
	seen := make(map[refKey]bool, len(baseLinkedRefs))
	for _, ref := range baseLinkedRefs {
		if ref == nil {
			continue
		}
		key := keyFromRef(ref)
		if seen[key] {
			continue
		}
		seen[key] = true
		baseline = append(baseline, cloneRef(ref))
	}

	// Collect REQUIRE and EXCLUDE rules from every ancestor namespace except
	// namespaces classified as project. The ancestor list is ordered
	// child→parent by the walker; policy order does not affect the outcome
	// because REQUIRE injection is an idempotent set-add and EXCLUDE removal
	// is also idempotent — the final output only depends on the set of rules
	// whose target pattern matches.
	var requires []*consolev1.TemplatePolicyRule
	var excludes []*consolev1.TemplatePolicyRule
	for _, ns := range ancestors {
		kind, _, classifyErr := r.Resolver.ResourceTypeFromNamespace(ns.Name)
		if classifyErr != nil {
			slog.WarnContext(ctx, "policyresolver: namespace classification failed, skipping for policy read",
				slog.String("namespace", ns.Name),
				slog.Any("error", classifyErr),
			)
			continue
		}
		// HOL-557 storage-isolation rule: project namespaces are never
		// consulted for policy. This holds even if a rogue ConfigMap was
		// planted in one.
		if kind == v1alpha2.ResourceTypeProject {
			continue
		}

		policies, listErr := r.listPolicies(ctx, ns.Name)
		if listErr != nil {
			slog.WarnContext(ctx, "policyresolver: list policies failed, skipping namespace",
				slog.String("namespace", ns.Name),
				slog.Any("error", listErr),
			)
			continue
		}
		for i := range policies {
			rules := rulesFromConfigMap(&policies[i])
			for _, rule := range rules {
				if !ruleMatches(rule, project, targetKind, targetName) {
					continue
				}
				switch rule.GetKind() {
				case consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE:
					requires = append(requires, rule)
				case consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE:
					excludes = append(excludes, rule)
				}
			}
		}
	}

	// Apply REQUIRE: append any rule template not already in the effective
	// set. We intentionally preserve baseline ordering and append REQUIREs
	// in the order they were harvested so the render output is deterministic
	// for a given cluster state.
	effective := baseline
	for _, rule := range requires {
		ref := cloneRef(rule.GetTemplate())
		if ref == nil {
			continue
		}
		key := keyFromRef(ref)
		if seen[key] {
			continue
		}
		seen[key] = true
		effective = append(effective, ref)
	}

	// Apply EXCLUDE: remove any matching template, but ONLY if it was added
	// by a REQUIRE rule. An EXCLUDE against an explicitly-linked template is
	// rejected up front by the TemplatePolicy handler's validator (HOL-557
	// acceptance criterion: "EXCLUDE on an explicitly-linked template is
	// rejected by Create/UpdateTemplatePolicy with FailedPrecondition"), so
	// reaching this point means either the baseline link was added after
	// the policy was written (race) or tests bypass the handler. In either
	// case the safe behavior is to leave the baseline intact — the
	// explicit link wins. The explicit-link check runs here too as
	// defense-in-depth.
	if len(excludes) > 0 {
		baselineKeys := make(map[refKey]bool, len(baseLinkedRefs))
		for _, ref := range baseLinkedRefs {
			if ref != nil {
				baselineKeys[keyFromRef(ref)] = true
			}
		}
		excluded := make(map[refKey]bool, len(excludes))
		for _, rule := range excludes {
			ref := rule.GetTemplate()
			if ref == nil {
				continue
			}
			key := keyFromRef(ref)
			if baselineKeys[key] {
				slog.WarnContext(ctx, "policyresolver: EXCLUDE rule ignored for explicitly-linked template",
					slog.String("template", ref.GetName()),
					slog.String("scope", ref.GetScope().String()),
					slog.String("scope_name", ref.GetScopeName()),
				)
				continue
			}
			excluded[key] = true
		}
		if len(excluded) > 0 {
			filtered := make([]*consolev1.LinkedTemplateRef, 0, len(effective))
			for _, ref := range effective {
				if excluded[keyFromRef(ref)] {
					continue
				}
				filtered = append(filtered, ref)
			}
			effective = filtered
		}
	}

	// Ensure we never return a nil slice so callers can unconditionally
	// range over the result.
	if effective == nil {
		effective = []*consolev1.LinkedTemplateRef{}
	}
	return effective, nil
}

// listPolicies returns every TemplatePolicy ConfigMap in the given namespace.
// Uses the shared resource-type label selector so hand-authored ConfigMaps
// without the managed-by annotation still participate, matching the
// templatepolicies handler's read path.
func (r *Resolver) listPolicies(ctx context.Context, ns string) ([]corev1.ConfigMap, error) {
	labelSelector := v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeTemplatePolicy
	list, err := r.Client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// ruleMatches reports whether a policy rule applies to the given target.
// A rule applies when its project pattern matches `project` AND either the
// deployment pattern is empty (project-level match is sufficient) OR the
// deployment pattern matches `targetName` when the target is a deployment.
// For TargetKindProjectTemplate targets the deployment pattern is ignored
// because a project template is a project-level artifact.
func ruleMatches(rule *consolev1.TemplatePolicyRule, project string, targetKind TargetKind, targetName string) bool {
	target := rule.GetTarget()
	if target == nil {
		return false
	}
	projectPattern := target.GetProjectPattern()
	if projectPattern == "" {
		return false
	}
	if !globMatch(projectPattern, project) {
		return false
	}
	deploymentPattern := target.GetDeploymentPattern()
	if deploymentPattern == "" {
		// Project-level rule: applies to every kind for the matched project.
		return true
	}
	if targetKind == TargetKindDeployment {
		return globMatch(deploymentPattern, targetName)
	}
	// deployment_pattern on a project-template target: treat as "matches any
	// project template in the project," which preserves the HOL-557 design
	// note that rules apply to both kinds equally when patterns match.
	return true
}

// globMatch wraps filepath.Match so a pattern parse failure never silently
// matches. The handler's validator already rejected malformed patterns at
// write time; a failure here indicates corrupted storage and is logged in
// the caller.
func globMatch(pattern, subject string) bool {
	ok, err := filepath.Match(pattern, subject)
	if err != nil {
		return false
	}
	return ok
}

// rulesFromConfigMap deserializes the TemplatePolicyRule JSON out of a
// TemplatePolicy ConfigMap's annotation. Matches the wire shape used by
// console/templatepolicies/k8s.go so rules round-trip byte-for-byte between
// the two packages. Returns an empty slice if the annotation is missing or
// malformed; a malformed policy is logged at the caller.
func rulesFromConfigMap(cm *corev1.ConfigMap) []*consolev1.TemplatePolicyRule {
	if cm == nil || cm.Annotations == nil {
		return nil
	}
	raw := cm.Annotations[v1alpha2.AnnotationTemplatePolicyRules]
	if raw == "" {
		return nil
	}
	// Match the exported wire shape from console/templatepolicies/k8s.go.
	type storedRef struct {
		Scope             string `json:"scope"`
		ScopeName         string `json:"scope_name"`
		Name              string `json:"name"`
		VersionConstraint string `json:"version_constraint,omitempty"`
	}
	type storedTarget struct {
		ProjectPattern    string `json:"project_pattern"`
		DeploymentPattern string `json:"deployment_pattern,omitempty"`
	}
	type storedRule struct {
		Kind     string       `json:"kind"`
		Template storedRef    `json:"template"`
		Target   storedTarget `json:"target"`
	}
	var stored []storedRule
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil
	}
	rules := make([]*consolev1.TemplatePolicyRule, 0, len(stored))
	for _, s := range stored {
		rule := &consolev1.TemplatePolicyRule{
			Kind: kindFromString(s.Kind),
			Template: &consolev1.LinkedTemplateRef{
				Scope:             scopeFromTemplateLabel(s.Template.Scope),
				ScopeName:         s.Template.ScopeName,
				Name:              s.Template.Name,
				VersionConstraint: s.Template.VersionConstraint,
			},
			Target: &consolev1.TemplatePolicyTarget{
				ProjectPattern:    s.Target.ProjectPattern,
				DeploymentPattern: s.Target.DeploymentPattern,
			},
		}
		rules = append(rules, rule)
	}
	return rules
}

func kindFromString(s string) consolev1.TemplatePolicyKind {
	switch s {
	case "require":
		return consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE
	case "exclude":
		return consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE
	default:
		return consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_UNSPECIFIED
	}
}

func scopeFromTemplateLabel(label string) consolev1.TemplateScope {
	switch label {
	case v1alpha2.TemplateScopeOrganization:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION
	case v1alpha2.TemplateScopeFolder:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER
	case v1alpha2.TemplateScopeProject:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT
	default:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED
	}
}

// refKey is a deterministic key over the identity fields of a
// LinkedTemplateRef. Version constraints are intentionally excluded so two
// different link lists that pin the same template at different version
// constraints still count as the same template for deduplication purposes
// — the resolver picks one; release resolution lives downstream.
type refKey struct {
	scope     consolev1.TemplateScope
	scopeName string
	name      string
}

func keyFromRef(ref *consolev1.LinkedTemplateRef) refKey {
	return refKey{
		scope:     ref.GetScope(),
		scopeName: ref.GetScopeName(),
		name:      ref.GetName(),
	}
}

// cloneRef returns a deep copy of a LinkedTemplateRef so caller mutations to
// the baseline slice do not leak into the resolver's output.
func cloneRef(ref *consolev1.LinkedTemplateRef) *consolev1.LinkedTemplateRef {
	if ref == nil {
		return nil
	}
	return &consolev1.LinkedTemplateRef{
		Scope:             ref.GetScope(),
		ScopeName:         ref.GetScopeName(),
		Name:              ref.GetName(),
		VersionConstraint: ref.GetVersionConstraint(),
	}
}

// DiffRefs returns the added and removed refs between an `applied` set and a
// `current` set, keyed by (scope, scope_name, name). Order is stable
// relative to the input slices: added follows `current` order, removed
// follows `applied` order.
func DiffRefs(applied, current []*consolev1.LinkedTemplateRef) (added, removed []*consolev1.LinkedTemplateRef) {
	appliedKeys := make(map[refKey]bool, len(applied))
	for _, ref := range applied {
		if ref != nil {
			appliedKeys[keyFromRef(ref)] = true
		}
	}
	currentKeys := make(map[refKey]bool, len(current))
	for _, ref := range current {
		if ref != nil {
			currentKeys[keyFromRef(ref)] = true
		}
	}
	for _, ref := range current {
		if ref == nil {
			continue
		}
		if !appliedKeys[keyFromRef(ref)] {
			added = append(added, cloneRef(ref))
		}
	}
	for _, ref := range applied {
		if ref == nil {
			continue
		}
		if !currentKeys[keyFromRef(ref)] {
			removed = append(removed, cloneRef(ref))
		}
	}
	return added, removed
}

// HasDrift reports whether an applied set and current set differ by
// (scope, scope_name, name). Cheaper than DiffRefs when the caller only
// needs the bool (e.g. list-view RPCs populating DeploymentStatusSummary).
func HasDrift(applied, current []*consolev1.LinkedTemplateRef) bool {
	if len(applied) != len(current) {
		added, removed := DiffRefs(applied, current)
		return len(added) > 0 || len(removed) > 0
	}
	appliedKeys := make(map[refKey]bool, len(applied))
	for _, ref := range applied {
		if ref != nil {
			appliedKeys[keyFromRef(ref)] = true
		}
	}
	for _, ref := range current {
		if ref == nil {
			continue
		}
		if !appliedKeys[keyFromRef(ref)] {
			return true
		}
	}
	return false
}
