package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/secrets"
	"github.com/holos-run/holos-console/console/templatepolicybindings"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// migrateBindingNameSuffix is appended to the policy name to derive the
// TemplatePolicyBinding name produced by the migration. Keeping the suffix
// deterministic is what makes the migration idempotent: re-running the
// command finds the existing binding by name and leaves it alone.
const migrateBindingNameSuffix = "-migrated"

// maxK8sNameLength is the Kubernetes DNS-label limit for ConfigMap
// metadata.name. A policy whose name, once the "-migrated" suffix is
// appended, would exceed this limit cannot be turned into a binding by
// the deterministic naming scheme. The migrator surfaces such policies
// during planning as a conflict so dry-run output makes the problem
// visible before --apply fails mid-run.
const maxK8sNameLength = 63

// newMigrateCommand returns the `holos-console migrate` parent command
// grouping every migration subcommand in the tree.
func newMigrateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run in-place migrations against TemplatePolicy and related resources",
		Long: "migrate groups one-shot migration commands that translate older " +
			"TemplatePolicy storage shapes into the shapes expected by the " +
			"current console. Each subcommand is idempotent and defaults to " +
			"--dry-run; pass --apply to mutate cluster state.",
	}
	cmd.AddCommand(newMigrateTemplatePolicyTargetsCommand())
	return cmd
}

// newMigrateTemplatePolicyTargetsCommand returns the `migrate
// template-policy-targets` subcommand. The subcommand translates populated
// TemplatePolicyRule.Target globs into TemplatePolicyBinding objects as a
// prerequisite for HOL-600 (removal of the legacy glob evaluation path).
func newMigrateTemplatePolicyTargetsCommand() *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:   "template-policy-targets",
		Short: "Translate TemplatePolicyRule.Target globs into TemplatePolicyBindings",
		Long: "template-policy-targets iterates every TemplatePolicy in the " +
			"cluster, enumerates the concrete render targets that each rule's " +
			"globs currently match (projects, project-scope templates, and " +
			"deployments), and creates a TemplatePolicyBinding per policy " +
			"with the union of those targets. After a binding lands, the " +
			"policy's rule-level Target globs are cleared on the next Update " +
			"so HOL-600 can remove the legacy evaluation path without " +
			"surprise.\n\n" +
			"The command is idempotent: re-running it produces no new bindings " +
			"when the target sets have not changed, and a policy whose Target " +
			"globs are already empty is skipped entirely.\n\n" +
			"--dry-run is the default. Pass --apply to write bindings and " +
			"clear the Target fields on the originating policies.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := secrets.NewClientset()
			if err != nil {
				return fmt.Errorf("building kubernetes client: %w", err)
			}
			if client == nil {
				return fmt.Errorf("no kubernetes config available; set KUBECONFIG or run inside the cluster")
			}
			r := newResolverFromFlags()
			opts := MigrateTemplatePolicyTargetsOptions{
				Client:   client,
				Resolver: r,
				Apply:    apply,
				Out:      cmd.OutOrStdout(),
			}
			res, err := MigrateTemplatePolicyTargets(cmd.Context(), opts)
			if err != nil {
				return err
			}
			printMigrationSummary(cmd.OutOrStdout(), res, apply)
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "Apply changes (default dry-run prints the plan without mutating state)")
	return cmd
}

// newResolverFromFlags builds a namespace resolver from the root command's
// global prefix flags. The migration command runs inside the same process
// the server uses at runtime, so the flags are already parsed into the
// package-level variables by the time this helper is invoked.
func newResolverFromFlags() *resolver.Resolver {
	return &resolver.Resolver{
		NamespacePrefix:    namespacePrefix,
		OrganizationPrefix: organizationPrefix,
		FolderPrefix:       folderPrefix,
		ProjectPrefix:      projectPrefix,
	}
}

// MigrateTemplatePolicyTargetsOptions wires the migrator to its Kubernetes
// client and output sink. The struct is exported so the table-driven tests in
// this package can construct the same contract without duplicating cobra flag
// plumbing.
type MigrateTemplatePolicyTargetsOptions struct {
	// Client is the Kubernetes client used to list namespaces, ConfigMaps,
	// and to write bindings / updated policies. Required.
	Client kubernetes.Interface
	// Resolver translates between user-facing names and Kubernetes
	// namespace names. Required.
	Resolver *resolver.Resolver
	// Apply controls whether the migrator mutates cluster state. When
	// false (the default), the migrator computes the plan and prints it
	// but leaves the cluster untouched.
	Apply bool
	// Out is the sink for the plan/summary text. Required.
	Out io.Writer
}

// PolicyMigrationPlan describes what the migrator intends to do for a single
// TemplatePolicy. The migrator produces one plan per policy with non-empty
// Target globs; policies whose Target globs are already empty are skipped
// before a plan is generated.
type PolicyMigrationPlan struct {
	// PolicyNamespace is the folder or organization namespace that owns
	// the policy ConfigMap.
	PolicyNamespace string
	// PolicyName is the policy ConfigMap's metadata.name.
	PolicyName string
	// BindingName is the deterministic name the migrator will use when
	// creating (or verifying) the TemplatePolicyBinding that replaces
	// this policy's Target globs. Re-running the migrator finds the
	// same binding by this name — that is how idempotency is enforced.
	BindingName string
	// Scope is the TemplateScope derived from PolicyNamespace. Used to
	// write the policy_ref on the binding and to route subsequent
	// UpdatePolicy / CreateBinding calls through the correct K8s
	// namespace.
	Scope scopeshim.Scope
	// ScopeName is the folder or organization slug derived from
	// PolicyNamespace.
	ScopeName string
	// TargetRefs is the de-duplicated union of every render target
	// selected by any rule's Target globs on this policy. The migrator
	// creates a binding with this exact list.
	TargetRefs []*consolev1.TemplatePolicyBindingTargetRef
	// BindingExists records whether a TemplatePolicyBinding by BindingName
	// already exists when the plan is built. Idempotency branches off
	// this flag: an existing binding with the same targets is left alone,
	// an existing binding with different targets is reported as a
	// conflict (and the migrator refuses to touch it).
	BindingExists bool
	// BindingTargetsMatch is set when BindingExists is true and the
	// existing binding's target_refs (set semantics) equal TargetRefs.
	// When true the migrator considers the binding side already migrated
	// and only proceeds with clearing the policy's Target fields.
	BindingTargetsMatch bool
	// ClearPolicyTargets is true when the policy still carries a non-
	// empty Target on at least one rule. When false the policy is
	// already cleared and the migrator skips the UpdatePolicy call.
	ClearPolicyTargets bool
	// EmptyTargets is true when the rule globs currently match no
	// render targets in the cluster. The migrator skips binding
	// creation in that case (the binding handler rejects an empty
	// target_refs list, so writing one would produce an
	// uneditable artifact) but still clears the policy's Target
	// globs so HOL-600 can remove the evaluation path safely — the
	// globs matched nothing under the legacy path either, so clearing
	// them is semantics-preserving.
	EmptyTargets bool
	// Notes accumulates per-plan warnings or informational messages
	// (e.g. conflict notices) so the printed summary can surface them
	// without forcing every caller to re-derive them.
	Notes []string
}

// MigrationResult aggregates the per-policy outcomes of a migration run.
// Exported fields are zero-valued on fresh clusters (no policies to
// migrate), which is the canonical "empty" shape the idempotency tests
// assert against.
type MigrationResult struct {
	// Plans lists one entry per policy with non-empty Target globs. The
	// order reflects the namespace list / per-namespace policy list order
	// at the time of execution and is therefore not guaranteed stable
	// across runs.
	Plans []*PolicyMigrationPlan
	// BindingsCreated counts the bindings the migrator wrote during this
	// run. Always 0 for a dry-run.
	BindingsCreated int
	// PoliciesUpdated counts the policies whose Target globs the
	// migrator cleared during this run. Always 0 for a dry-run.
	PoliciesUpdated int
	// Conflicts counts plans that were skipped because an existing
	// binding by the same name carried different target_refs than the
	// migrator would have written. A non-zero value indicates operator
	// intervention is required before the migration can complete.
	Conflicts int
	// Skipped counts policies whose Target globs are already empty and
	// therefore do not require migration. Always reported so an operator
	// can see the migrator ran to completion and found no work.
	Skipped int
}

// MigrateTemplatePolicyTargets is the package-level entry point tests and the
// cobra subcommand share. It discovers every policy-bearing namespace in the
// cluster, builds a PolicyMigrationPlan per policy with non-empty Target
// globs, and (when Apply is true) writes the bindings and clears the Target
// fields.
func MigrateTemplatePolicyTargets(ctx context.Context, opts MigrateTemplatePolicyTargetsOptions) (*MigrationResult, error) {
	if opts.Client == nil {
		return nil, fmt.Errorf("kubernetes client is required")
	}
	if opts.Resolver == nil {
		return nil, fmt.Errorf("namespace resolver is required")
	}
	if opts.Out == nil {
		return nil, fmt.Errorf("output writer is required")
	}

	// Index every managed namespace up front so we can classify and walk
	// without making per-policy round-trips to the K8s API. The selector
	// keeps the list narrowly scoped to namespaces the console owns;
	// unmanaged namespaces never appear in the traversal.
	namespaces, err := listManagedNamespaces(ctx, opts.Client)
	if err != nil {
		return nil, err
	}

	classified := classifyNamespaces(opts.Resolver, namespaces)

	// Walk the owning namespace for each policy; the helper handles the
	// fail-open contract on a single per-namespace list error (log and
	// continue) that matches the K8sClient.ListPoliciesInNamespace shape
	// used at runtime.
	//
	// Every count that the summary line reports — BindingsCreated,
	// PoliciesUpdated, Conflicts, Skipped — is tallied here during
	// planning, BEFORE any mutation runs. That way the dry-run preview
	// matches what --apply would produce, which is what an operator
	// looks for when deciding whether to re-run with --apply. The
	// apply loop below only mutates the cluster; it never touches the
	// counters.
	result := &MigrationResult{}
	for _, ns := range classified.policyBearing {
		cms, err := listPoliciesInNamespace(ctx, opts.Client, ns.Name)
		if err != nil {
			// A transient list failure must not mask bindings we
			// already wrote in prior iterations; surface it to the
			// caller so the operator can retry.
			return result, fmt.Errorf("listing policies in %q: %w", ns.Name, err)
		}
		for i := range cms {
			cm := &cms[i]
			plan, planErr := buildPolicyPlan(ctx, opts.Client, opts.Resolver, classified, ns, cm)
			if planErr != nil {
				return result, planErr
			}
			if plan == nil {
				result.Skipped++
				continue
			}
			result.Plans = append(result.Plans, plan)
			switch {
			case plan.BindingExists && !plan.BindingTargetsMatch:
				// An existing binding with a different target
				// set — or a non-binding ConfigMap that
				// collided on the synthesized name — is a
				// conflict that only an operator can resolve.
				// The apply loop leaves both the policy and
				// the offending object untouched.
				result.Conflicts++
			default:
				if !plan.BindingExists && !plan.EmptyTargets {
					result.BindingsCreated++
				}
				if plan.ClearPolicyTargets {
					result.PoliciesUpdated++
				}
			}
		}
	}

	// Print the plan before any mutation so operators see exactly what
	// the migrator intends to do. A dry-run stops here.
	printMigrationPlan(opts.Out, result.Plans)

	if !opts.Apply {
		return result, nil
	}

	// Apply phase: each plan is executed atomically per policy — binding
	// first, then clear. A binding-create failure leaves the policy
	// untouched so the next run retries cleanly; a policy-update failure
	// after a successful binding-create is retryable too because the
	// existing binding is detected and re-used next time.
	for _, plan := range result.Plans {
		if plan.BindingExists && !plan.BindingTargetsMatch {
			continue
		}
		if !plan.BindingExists && !plan.EmptyTargets {
			if err := createBindingFromPlan(ctx, opts.Client, plan); err != nil {
				return result, fmt.Errorf("creating binding %q in %q: %w",
					plan.BindingName, plan.PolicyNamespace, err)
			}
		}
		if plan.ClearPolicyTargets {
			if err := clearPolicyTargets(ctx, opts.Client, plan); err != nil {
				return result, fmt.Errorf("clearing targets on policy %q in %q: %w",
					plan.PolicyName, plan.PolicyNamespace, err)
			}
		}
	}

	return result, nil
}

// listManagedNamespaces fetches every namespace carrying the
// managed-by=console.holos.run label. The migrator never reads unmanaged
// namespaces — an operator with a hand-authored ConfigMap in an unrelated
// namespace should not have their data surfaced by a migration command.
func listManagedNamespaces(ctx context.Context, client kubernetes.Interface) ([]corev1.Namespace, error) {
	labelSelector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue
	list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("listing managed namespaces: %w", err)
	}
	return list.Items, nil
}

// classifiedNamespaces partitions managed namespaces by their resource-type
// label so the migrator can cheaply answer "which projects descend from this
// folder?" without re-listing the cluster per policy.
type classifiedNamespaces struct {
	byName       map[string]*corev1.Namespace
	organization map[string]*corev1.Namespace // scopeName -> namespace
	folder       map[string]*corev1.Namespace // scopeName -> namespace
	project      map[string]*corev1.Namespace // project slug -> namespace
	// policyBearing is the ordered union of organization + folder
	// namespaces — the two kinds that can own a TemplatePolicy. Order
	// is deterministic (organizations first, then folders, each in
	// alphabetical namespace-name order) so the plan is reproducible
	// enough for operators to diff between dry-run and apply.
	policyBearing []*corev1.Namespace
}

// classifyNamespaces splits a managed-namespace listing by resource-type
// label. Namespaces whose prefix does not resolve to one of the three
// expected kinds are ignored — they may be render-state ConfigMaps'
// companion namespaces or other managed resources that do not participate
// in template-policy migration.
func classifyNamespaces(r *resolver.Resolver, namespaces []corev1.Namespace) *classifiedNamespaces {
	c := &classifiedNamespaces{
		byName:       make(map[string]*corev1.Namespace, len(namespaces)),
		organization: make(map[string]*corev1.Namespace),
		folder:       make(map[string]*corev1.Namespace),
		project:      make(map[string]*corev1.Namespace),
	}
	for i := range namespaces {
		ns := &namespaces[i]
		c.byName[ns.Name] = ns
		kind, name, err := r.ResourceTypeFromNamespace(ns.Name)
		if err != nil {
			continue
		}
		switch kind {
		case v1alpha2.ResourceTypeOrganization:
			c.organization[name] = ns
		case v1alpha2.ResourceTypeFolder:
			c.folder[name] = ns
		case v1alpha2.ResourceTypeProject:
			c.project[name] = ns
		}
	}
	// Deterministic order: organizations sorted by namespace name,
	// folders next sorted by namespace name. The actual per-policy
	// order within each namespace is driven by the K8s list response
	// and is therefore not under our control, but cross-namespace order
	// is.
	orgNames := make([]string, 0, len(c.organization))
	for name := range c.organization {
		orgNames = append(orgNames, name)
	}
	sort.Strings(orgNames)
	folderNames := make([]string, 0, len(c.folder))
	for name := range c.folder {
		folderNames = append(folderNames, name)
	}
	sort.Strings(folderNames)
	for _, name := range orgNames {
		c.policyBearing = append(c.policyBearing, c.organization[name])
	}
	for _, name := range folderNames {
		c.policyBearing = append(c.policyBearing, c.folder[name])
	}
	return c
}

// listPoliciesInNamespace lists TemplatePolicy ConfigMaps in the given
// namespace. The label selector mirrors
// templatepolicies.K8sClient.listPoliciesInNamespace so the migrator reads
// the same objects the runtime handler would.
func listPoliciesInNamespace(ctx context.Context, client kubernetes.Interface, ns string) ([]corev1.ConfigMap, error) {
	labelSelector := v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeTemplatePolicy
	list, err := client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// buildPolicyPlan produces a PolicyMigrationPlan for a single policy, or
// returns (nil, nil) when the policy has no non-empty Target globs and
// therefore does not need migration. Any K8s error encountered while
// enumerating candidate render targets propagates — a partial plan is
// worse than no plan because the apply phase would under-bind.
func buildPolicyPlan(
	ctx context.Context,
	client kubernetes.Interface,
	r *resolver.Resolver,
	nsIndex *classifiedNamespaces,
	scopeNs *corev1.Namespace,
	policy *corev1.ConfigMap,
) (*PolicyMigrationPlan, error) {
	rawRules := policy.Annotations[v1alpha2.AnnotationTemplatePolicyRules]
	// HOL-600 removed the `target` field from the proto rule, so the
	// proto-level unmarshaler (templatepolicies.UnmarshalRules) can no
	// longer surface the legacy glob values the migrator needs to
	// translate. Parse the annotation directly into the local
	// ruleWire / targetWire shape so the migrator keeps working
	// against pre-migration ConfigMaps regardless of the runtime
	// proto's field set.
	legacyRules, err := parseLegacyRules(rawRules)
	if err != nil {
		return nil, fmt.Errorf("parsing rules on policy %q in %q: %w",
			policy.Name, policy.Namespace, err)
	}
	if !policyHasNonEmptyTargets(legacyRules) {
		return nil, nil
	}

	scope, scopeName, err := templateScopeFromNamespace(r, scopeNs.Name)
	if err != nil {
		return nil, fmt.Errorf("classifying scope for %q: %w", scopeNs.Name, err)
	}

	// Enumerate the candidate projects reachable from this policy's
	// owning scope. For an organization scope every managed project
	// labeled with the org qualifies; for a folder scope only the
	// projects whose ancestor chain passes through this folder
	// qualify. The helper resolves ancestry via the parent label —
	// the same seam the runtime walker uses at render time.
	projects, err := descendantProjects(r, nsIndex, scopeNs)
	if err != nil {
		return nil, fmt.Errorf("enumerating descendant projects for %q: %w", scopeNs.Name, err)
	}

	// Collect the render targets per rule. A rule with an empty
	// project_pattern matches every descendant project (the pre-HOL-600
	// resolver treated "" as "*"), and a rule with an empty
	// deployment_pattern selects BOTH the project-scope templates and
	// deployments within each matching project.
	targetSet := newTargetRefSet()
	for _, rule := range legacyRules {
		projectPattern := rule.Target.ProjectPattern
		deploymentPattern := rule.Target.DeploymentPattern
		if projectPattern == "" && deploymentPattern == "" {
			continue
		}
		for _, projNs := range projects {
			projectSlug, projErr := r.ProjectFromNamespace(projNs.Name)
			if projErr != nil {
				// Non-project namespace in the project index is
				// a classification bug; skip defensively rather
				// than crash the whole migration.
				continue
			}
			if !patternMatches(projectPattern, projectSlug) {
				continue
			}
			if deploymentPattern == "" {
				// Empty deployment pattern selects both the
				// project-scope templates and the deployments
				// in the project — the renderer's unified
				// semantic.
				tmpls, tErr := listProjectTemplates(ctx, client, projNs.Name)
				if tErr != nil {
					return nil, fmt.Errorf("listing project templates in %q: %w", projNs.Name, tErr)
				}
				for _, tmpl := range tmpls {
					targetSet.Add(&consolev1.TemplatePolicyBindingTargetRef{
						Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE,
						Name:        tmpl.Name,
						ProjectName: projectSlug,
					})
				}
				deployments, dErr := listProjectDeployments(ctx, client, projNs.Name)
				if dErr != nil {
					return nil, fmt.Errorf("listing deployments in %q: %w", projNs.Name, dErr)
				}
				for _, dep := range deployments {
					targetSet.Add(&consolev1.TemplatePolicyBindingTargetRef{
						Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
						Name:        dep.Name,
						ProjectName: projectSlug,
					})
				}
				continue
			}
			deployments, dErr := listProjectDeployments(ctx, client, projNs.Name)
			if dErr != nil {
				return nil, fmt.Errorf("listing deployments in %q: %w", projNs.Name, dErr)
			}
			for _, dep := range deployments {
				if !patternMatches(deploymentPattern, dep.Name) {
					continue
				}
				targetSet.Add(&consolev1.TemplatePolicyBindingTargetRef{
					Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
					Name:        dep.Name,
					ProjectName: projectSlug,
				})
			}
		}
	}

	bindingName := policy.Name + migrateBindingNameSuffix
	resolvedTargets := targetSet.Sorted()
	plan := &PolicyMigrationPlan{
		PolicyNamespace:    policy.Namespace,
		PolicyName:         policy.Name,
		BindingName:        bindingName,
		Scope:              scope,
		ScopeName:          scopeName,
		TargetRefs:         resolvedTargets,
		ClearPolicyTargets: true,
		EmptyTargets:       len(resolvedTargets) == 0,
	}
	if plan.EmptyTargets {
		plan.Notes = append(plan.Notes,
			"glob patterns matched no live project templates or deployments; skipping binding creation (an empty target_refs list is rejected by the binding handler) while still clearing the policy Target globs")
	}
	// Preflight the synthesized binding name against the K8s DNS-label
	// limit. A policy name long enough to push the "-migrated" suffix
	// past 63 characters cannot be expressed as a ConfigMap, and the
	// error would only surface mid-apply — the operator sees a clean
	// dry-run then a broken run. Surface it during planning so the
	// dry-run output matches what --apply will actually do.
	if len(bindingName) > maxK8sNameLength {
		plan.Notes = append(plan.Notes,
			fmt.Sprintf("synthesized binding name %q is %d characters, exceeds Kubernetes DNS-label limit %d; rename the policy before running the migration or create the binding manually",
				bindingName, len(bindingName), maxK8sNameLength))
		// Route through the conflict branch so neither the binding
		// nor the policy mutation happens. BindingExists=true with
		// BindingTargetsMatch=false is the plan's "skip everything,
		// operator must act" shape.
		plan.BindingExists = true
		return plan, nil
	}

	// Inspect any pre-existing binding by BindingName. A matching set
	// means the binding side is already migrated and only the
	// policy-side clear still needs to run; a differing set is a
	// conflict an operator must resolve.
	//
	// Organization and folder namespaces also host Template and
	// TemplatePolicy ConfigMaps, so we must verify the resource-type
	// label before treating the matched object as a binding. Without
	// that check a Template named "<policy>-migrated" (or any other
	// ConfigMap that happened to share the synthesized binding name)
	// would make the migrator report a permanent conflict that no
	// operator action on the binding side could resolve.
	existing, err := client.CoreV1().ConfigMaps(policy.Namespace).Get(ctx, bindingName, metav1.GetOptions{})
	if err == nil {
		if existing.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeTemplatePolicyBinding {
			plan.Notes = append(plan.Notes,
				fmt.Sprintf("ConfigMap %q exists in %q but is not a TemplatePolicyBinding (resource-type=%q); binding cannot be written without colliding — resolve by hand before re-running",
					bindingName, policy.Namespace, existing.Labels[v1alpha2.LabelResourceType]))
			// Record the blocker as a conflict-shaped plan so the
			// apply loop skips both the binding create and the
			// policy clear. BindingExists=true combined with
			// BindingTargetsMatch=false routes through the
			// conflict branch, leaving the policy untouched until
			// an operator removes the offending ConfigMap.
			plan.BindingExists = true
		} else {
			plan.BindingExists = true
			existingRefs, parseErr := templatepolicybindings.UnmarshalTargetRefs(existing.Annotations[v1alpha2.AnnotationTemplatePolicyBindingTargetRefs])
			if parseErr != nil {
				plan.Notes = append(plan.Notes,
					fmt.Sprintf("existing binding %q has unparseable target_refs: %v", bindingName, parseErr))
			} else if targetSetsEqual(existingRefs, plan.TargetRefs) {
				plan.BindingTargetsMatch = true
			} else {
				plan.Notes = append(plan.Notes,
					fmt.Sprintf("existing binding %q has different target_refs; leaving untouched", bindingName))
			}
		}
	} else if !k8serrors.IsNotFound(err) {
		return nil, fmt.Errorf("checking for existing binding %q in %q: %w",
			bindingName, policy.Namespace, err)
	}

	return plan, nil
}

// policyHasNonEmptyTargets reports whether any rule on the policy still
// carries a Target glob that the migrator needs to translate. A policy
// whose rules are all cleared (both project_pattern and
// deployment_pattern are the empty string) is already migrated and the
// migrator skips it entirely — the canonical idempotency short-circuit.
//
// A rule with project_pattern="*" (a legitimate legacy pre-migration
// glob) is NOT treated as cleared: the `"*"` token is what an operator
// would write to target every project, and the migrator must translate
// it into an explicit binding rather than mistaking it for the post-
// migration shape.
func policyHasNonEmptyTargets(rules []ruleWire) bool {
	for _, rule := range rules {
		if rule.Target.ProjectPattern != "" || rule.Target.DeploymentPattern != "" {
			return true
		}
	}
	return false
}

// templateScopeFromNamespace derives the TemplateScope + scope slug from a
// policy-owning namespace. Only organization and folder namespaces reach
// here; the classification helper already filtered project and unknown
// kinds out of the policy-bearing slice.
func templateScopeFromNamespace(r *resolver.Resolver, ns string) (scopeshim.Scope, string, error) {
	kind, name, err := r.ResourceTypeFromNamespace(ns)
	if err != nil {
		return scopeshim.ScopeUnspecified, "", err
	}
	switch kind {
	case v1alpha2.ResourceTypeOrganization:
		return scopeshim.ScopeOrganization, name, nil
	case v1alpha2.ResourceTypeFolder:
		return scopeshim.ScopeFolder, name, nil
	default:
		return scopeshim.ScopeUnspecified, "",
			fmt.Errorf("namespace %q is not a policy-bearing scope (got %q)", ns, kind)
	}
}

// descendantProjects returns every managed project namespace whose ancestor
// chain passes through scopeNs. The walk uses the parent label index built
// in classifyNamespaces so the helper never touches the K8s API — all the
// metadata it needs is already in memory.
//
// The candidate set is narrowed by the policy scope's owning organization
// label (via `orgForScopeNamespace`) so a malformed project chain in an
// unrelated organization cannot fail the migration for a policy that
// could never match that project anyway. This mirrors
// `K8sResourceTopology.ListProjectsUnderScope` (HOL-570), which also
// pre-filters by the org label before walking ancestors — keeping the
// blast radius of ancestry errors narrow is the whole point of that
// filter, and the migration preserves the same contract.
//
// Projects carrying a non-nil DeletionTimestamp are skipped to mirror
// the same topology helper: the runtime treats a terminating namespace as
// already unreachable, so the migration must not bind targets in
// namespaces that will never activate under the legacy glob path the
// binding is replacing.
//
// Ancestry walk failures (missing parent label, missing parent namespace,
// cycle, depth exceeded) encountered on an in-org candidate project
// propagate to the caller. Silently dropping an unreachable project would
// be a data-loss path: the migrator would skip targets that legitimately
// match the policy's globs, clear the policy's Target fields, and leave
// the project permanently uncovered once HOL-600 removes the legacy
// evaluation. Failing loudly forces the operator to fix the ancestry
// before the migration runs.
func descendantProjects(r *resolver.Resolver, nsIndex *classifiedNamespaces, scopeNs *corev1.Namespace) ([]*corev1.Namespace, error) {
	wantNs := scopeNs.Name
	orgLabel := orgForScopeNamespace(r, scopeNs)
	out := make([]*corev1.Namespace, 0)
	for _, projNs := range nsIndex.project {
		if projNs.DeletionTimestamp != nil {
			continue
		}
		// Pre-filter by organization label so a malformed parent
		// chain in a different org cannot fail this walk. When the
		// scope namespace itself carries no org label (legacy
		// fixtures, tests that set up a bare fixture without the
		// org label) fall through to the ancestry check without a
		// pre-filter — correctness first, speed second.
		if orgLabel != "" && projNs.Labels[v1alpha2.LabelOrganization] != orgLabel {
			continue
		}
		contained, err := ancestorChainContains(nsIndex, projNs, wantNs)
		if err != nil {
			return nil, fmt.Errorf("walking ancestors of %q: %w", projNs.Name, err)
		}
		if contained {
			out = append(out, projNs)
		}
	}
	// Stable ordering keeps the printed plan reproducible across runs.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// orgForScopeNamespace returns the organization slug that owns the
// policy's scope namespace, used as a pre-filter on the descendant
// project walk.
//
//   - Organization scope: the slug derives directly from the namespace
//     prefix, classified via the resolver. The organization namespace
//     carries no LabelOrganization by convention, so a label lookup
//     would miss it.
//   - Folder scope: read LabelOrganization from the folder namespace; if
//     the label is missing, return "" and fall through to the
//     unfiltered walk (matches the runtime topology fallback).
//   - Any other case (unclassified namespace): return "" so the caller
//     defaults to the unfiltered walk.
func orgForScopeNamespace(r *resolver.Resolver, scopeNs *corev1.Namespace) string {
	if scopeNs == nil {
		return ""
	}
	kind, name, err := r.ResourceTypeFromNamespace(scopeNs.Name)
	if err != nil {
		return ""
	}
	switch kind {
	case v1alpha2.ResourceTypeOrganization:
		return name
	case v1alpha2.ResourceTypeFolder:
		return scopeNs.Labels[v1alpha2.LabelOrganization]
	default:
		return ""
	}
}

// ancestorChainContains walks the parent label from startNs up the
// hierarchy, returning true when wantNs appears on the chain. The walk is
// bounded by the number of known namespaces so a mis-labeled parent chain
// cannot cause an infinite loop at migration time.
//
// An organization namespace is a legitimate terminal: reaching it without
// finding wantNs means wantNs is not on the chain, and the walker returns
// (false, nil). Every other "cannot continue" situation — a non-
// organization namespace with no parent label, a parent label that points
// at a namespace not in the managed index, or a cycle — is an ancestry
// error propagated to the caller so the migration fails loudly rather
// than silently dropping a project.
func ancestorChainContains(nsIndex *classifiedNamespaces, startNs *corev1.Namespace, wantNs string) (bool, error) {
	if startNs == nil {
		return false, fmt.Errorf("nil start namespace")
	}
	current := startNs
	visited := make(map[string]struct{}, len(nsIndex.byName))
	for depth := 0; depth < len(nsIndex.byName)+1; depth++ {
		if current.Name == wantNs {
			return true, nil
		}
		if _, seen := visited[current.Name]; seen {
			return false, fmt.Errorf("cycle detected at %q (starting from %q)", current.Name, startNs.Name)
		}
		visited[current.Name] = struct{}{}
		// Reaching an organization namespace means the chain is
		// complete; wantNs is definitively not on it.
		if current.Labels[v1alpha2.LabelResourceType] == v1alpha2.ResourceTypeOrganization {
			return false, nil
		}
		parent := current.Labels[v1alpha2.AnnotationParent]
		if parent == "" {
			return false, fmt.Errorf("namespace %q is missing required parent annotation %q", current.Name, v1alpha2.AnnotationParent)
		}
		next, ok := nsIndex.byName[parent]
		if !ok {
			return false, fmt.Errorf("parent namespace %q of %q is not in the managed index", parent, current.Name)
		}
		current = next
	}
	return false, fmt.Errorf("namespace hierarchy depth exceeded limit starting from %q", startNs.Name)
}

// listProjectTemplates returns every managed project-scope Template ConfigMap
// in the project namespace. Selector semantics mirror
// K8sResourceTopology.ListProjectTemplates so the migrator reads the same
// objects the HOL-570 guardrail uses.
func listProjectTemplates(ctx context.Context, client kubernetes.Interface, projectNs string) ([]corev1.ConfigMap, error) {
	selector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue + "," +
		v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeTemplate + "," +
		v1alpha2.LabelTemplateScope + "=" + v1alpha2.TemplateScopeProject
	list, err := client.CoreV1().ConfigMaps(projectNs).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// listProjectDeployments returns every managed Deployment ConfigMap in the
// project namespace. Selector semantics mirror
// K8sResourceTopology.ListProjectDeployments so the migrator reads the same
// objects the HOL-570 guardrail uses.
func listProjectDeployments(ctx context.Context, client kubernetes.Interface, projectNs string) ([]corev1.ConfigMap, error) {
	selector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue + "," +
		v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeDeployment
	list, err := client.CoreV1().ConfigMaps(projectNs).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// patternMatches wraps path.Match with the same "empty pattern means *"
// treatment folderResolver uses at render time. Matching semantics are kept
// in lock-step so the migration enumerates exactly the set the runtime
// currently activates — an operator who dry-runs today sees the same
// targets the resolver selected yesterday.
func patternMatches(pattern, subject string) bool {
	if pattern == "" {
		pattern = "*"
	}
	ok, err := path.Match(pattern, subject)
	if err != nil {
		return false
	}
	return ok
}

// targetRefSet is a deterministic, de-duplicating collector for
// (kind, projectName, name) triples. The sorted output is what the
// migrator writes to the binding's annotation and compares against the
// existing binding for idempotency.
type targetRefSet struct {
	byKey map[string]*consolev1.TemplatePolicyBindingTargetRef
}

func newTargetRefSet() *targetRefSet {
	return &targetRefSet{byKey: map[string]*consolev1.TemplatePolicyBindingTargetRef{}}
}

// Add inserts a target ref into the set if it is not already present. The
// key is the "kind|projectName|name" triple — exactly the shape the binding
// handler rejects duplicates on, so the migration never produces a binding
// the handler would reject.
func (s *targetRefSet) Add(ref *consolev1.TemplatePolicyBindingTargetRef) {
	if ref == nil {
		return
	}
	k := targetRefKey(ref)
	if _, ok := s.byKey[k]; ok {
		return
	}
	s.byKey[k] = ref
}

// Sorted returns the set's contents sorted by the composite key. The
// deterministic order is what lets the idempotency check in
// targetSetsEqual use slice equality instead of hashing.
func (s *targetRefSet) Sorted() []*consolev1.TemplatePolicyBindingTargetRef {
	out := make([]*consolev1.TemplatePolicyBindingTargetRef, 0, len(s.byKey))
	keys := make([]string, 0, len(s.byKey))
	for k := range s.byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, s.byKey[k])
	}
	return out
}

// targetRefKey produces the composite key used by the set and by
// targetSetsEqual. Keeping a single key function eliminates the risk of
// an idempotency-check/Add-key divergence that would resurrect a binding
// the migrator thought it had already written.
func targetRefKey(ref *consolev1.TemplatePolicyBindingTargetRef) string {
	return bindingTargetKindString(ref.GetKind()) + "|" + ref.GetProjectName() + "|" + ref.GetName()
}

// targetSetsEqual reports whether two target-ref lists are equal under set
// semantics. The migrator uses this to decide whether an existing binding
// matches the plan it would write; a mismatch is a conflict an operator
// must resolve.
func targetSetsEqual(a, b []*consolev1.TemplatePolicyBindingTargetRef) bool {
	if len(a) != len(b) {
		return false
	}
	want := make(map[string]struct{}, len(a))
	for _, ref := range a {
		want[targetRefKey(ref)] = struct{}{}
	}
	for _, ref := range b {
		if _, ok := want[targetRefKey(ref)]; !ok {
			return false
		}
	}
	return true
}

// bindingTargetKindString mirrors templatepolicybindings.targetKindToString.
// The migration command cannot import the unexported helper, but the
// annotation contract it protects is identical — both encode PROJECT_TEMPLATE
// as "project-template" and DEPLOYMENT as "deployment".
func bindingTargetKindString(k consolev1.TemplatePolicyBindingTargetKind) string {
	switch k {
	case consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE:
		return "project-template"
	case consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT:
		return "deployment"
	default:
		return ""
	}
}

// templateScopeAnnotationValue is the scope label the binding's annotation
// carries. Matches templatepolicybindings.scopeLabelValue one-for-one so
// the runtime resolver decodes what the migrator writes.
func templateScopeAnnotationValue(scope scopeshim.Scope) string {
	switch scope {
	case scopeshim.ScopeOrganization:
		return v1alpha2.TemplateScopeOrganization
	case scopeshim.ScopeFolder:
		return v1alpha2.TemplateScopeFolder
	default:
		return ""
	}
}

// createBindingFromPlan writes the TemplatePolicyBinding ConfigMap the plan
// describes. The wire shape duplicates templatepolicybindings.CreateBinding's
// annotation layout so the runtime handler reads exactly what the migration
// wrote — there is no separate binding-handler code path for migrated
// bindings.
func createBindingFromPlan(ctx context.Context, client kubernetes.Interface, plan *PolicyMigrationPlan) error {
	// HOL-619 replaced LinkedTemplatePolicyRef.scope_ref with a flat
	// namespace field; build the proto through the shim so the owning
	// Kubernetes namespace is filled in from the default resolver.
	policyRef := scopeshim.NewLinkedTemplatePolicyRef(plan.Scope, plan.ScopeName, plan.PolicyName)
	policyJSON, err := marshalBindingPolicyRef(policyRef)
	if err != nil {
		return err
	}
	targetsJSON, err := marshalBindingTargetRefs(plan.TargetRefs)
	if err != nil {
		return err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      plan.BindingName,
			Namespace: plan.PolicyNamespace,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplatePolicyBinding,
				v1alpha2.LabelTemplateScope: templateScopeAnnotationValue(plan.Scope),
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:                     plan.PolicyName + " (migrated targets)",
				v1alpha2.AnnotationDescription:                     "Auto-generated by holos-console migrate template-policy-targets (HOL-599). Supersedes the legacy TemplatePolicyRule.Target globs on " + plan.PolicyName + ".",
				v1alpha2.AnnotationCreatorEmail:                    "holos-console-migrate",
				v1alpha2.AnnotationTemplatePolicyBindingPolicyRef:  string(policyJSON),
				v1alpha2.AnnotationTemplatePolicyBindingTargetRefs: string(targetsJSON),
			},
		},
	}
	_, err = client.CoreV1().ConfigMaps(plan.PolicyNamespace).Create(ctx, cm, metav1.CreateOptions{})
	return err
}

// storedPolicyRefWire mirrors templatepolicybindings.storedPolicyRef so the
// migration package can round-trip the same JSON the runtime handler
// expects without introducing a cross-package dependency cycle.
type storedPolicyRefWire struct {
	Scope     string `json:"scope"`
	ScopeName string `json:"scopeName"`
	Name      string `json:"name"`
}

// storedTargetRefWire mirrors templatepolicybindings.storedTargetRef for the
// same reason storedPolicyRefWire mirrors its counterpart.
type storedTargetRefWire struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	ProjectName string `json:"projectName"`
}

func marshalBindingPolicyRef(ref *consolev1.LinkedTemplatePolicyRef) ([]byte, error) {
	sr := storedPolicyRefWire{}
	if ref != nil {
		// HOL-619 collapsed the scope_ref discriminator into a flat
		// namespace on LinkedTemplatePolicyRef; classify it through the
		// shim so the persisted annotation JSON keeps its existing
		// (scope, scopeName) key until HOL-621 rewrites this layer.
		sr.Scope = templateScopeAnnotationValue(scopeshim.PolicyRefScope(ref))
		sr.ScopeName = scopeshim.PolicyRefScopeName(ref)
		sr.Name = ref.GetName()
	}
	b, err := json.Marshal(sr)
	if err != nil {
		return nil, fmt.Errorf("serializing binding policy_ref: %w", err)
	}
	return b, nil
}

func marshalBindingTargetRefs(refs []*consolev1.TemplatePolicyBindingTargetRef) ([]byte, error) {
	out := make([]storedTargetRefWire, 0, len(refs))
	for _, r := range refs {
		if r == nil {
			continue
		}
		out = append(out, storedTargetRefWire{
			Kind:        bindingTargetKindString(r.GetKind()),
			Name:        r.GetName(),
			ProjectName: r.GetProjectName(),
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("serializing binding target_refs: %w", err)
	}
	return b, nil
}

// clearPolicyTargets updates the policy's rule annotation so every rule
// carries empty project_pattern and deployment_pattern strings. The rest of
// the rule — Kind, Template, VersionConstraint — is preserved verbatim.
// Clearing happens in-place so a follow-up runtime release (HOL-600) can
// delete the legacy evaluation path without leaving stale glob values on
// disk.
//
// The function parses the stored JSON directly rather than routing
// through the proto-level templatepolicies.UnmarshalRules, because the
// proto Target field was removed in HOL-600 and is no longer available
// on the decoded rule.
func clearPolicyTargets(ctx context.Context, client kubernetes.Interface, plan *PolicyMigrationPlan) error {
	cm, err := client.CoreV1().ConfigMaps(plan.PolicyNamespace).Get(ctx, plan.PolicyName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting policy for update: %w", err)
	}
	if cm.Annotations == nil {
		cm.Annotations = make(map[string]string)
	}
	raw := cm.Annotations[v1alpha2.AnnotationTemplatePolicyRules]
	rules, err := parseLegacyRules(raw)
	if err != nil {
		return fmt.Errorf("parsing rules for update: %w", err)
	}
	for i := range rules {
		rules[i].Target = targetWire{}
	}
	clearedJSON, err := marshalClearedRuleWires(rules)
	if err != nil {
		return err
	}
	cm.Annotations[v1alpha2.AnnotationTemplatePolicyRules] = string(clearedJSON)
	_, err = client.CoreV1().ConfigMaps(plan.PolicyNamespace).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

// parseLegacyRules decodes the JSON rules annotation into the migrator's
// own ruleWire shape so the migration logic sees the pre-HOL-600 target
// glob values the runtime proto no longer exposes. An empty or missing
// annotation decodes to a nil slice (matching the pre-refactor behavior).
func parseLegacyRules(raw string) ([]ruleWire, error) {
	if raw == "" {
		return nil, nil
	}
	var rules []ruleWire
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, fmt.Errorf("decoding template policy rules: %w", err)
	}
	return rules, nil
}

// ruleWire mirrors the on-disk JSON shape stored by
// templatepolicies.marshalRules. The migrator must write back the exact
// same fields the runtime handler reads — re-using the unexported helper
// would introduce an import cycle, so the wire struct is duplicated here
// with the same `json:` tags.
type ruleWire struct {
	Kind     string          `json:"kind"`
	Template templateRefWire `json:"template"`
	Target   targetWire      `json:"target"`
}

type templateRefWire struct {
	Scope             string `json:"scope"`
	ScopeName         string `json:"scope_name"`
	Name              string `json:"name"`
	VersionConstraint string `json:"version_constraint,omitempty"`
}

type targetWire struct {
	ProjectPattern    string `json:"project_pattern"`
	DeploymentPattern string `json:"deployment_pattern,omitempty"`
}

// marshalClearedRules serializes the in-memory rules with BOTH Target
// globs set to the empty string — the shape the AC requires. The function
// intentionally does NOT call out into templatepolicies.marshalRules —
// that helper is unexported, and duplicating the wire struct keeps the
// migrator independent from templatepolicies' internal refactors.
//
// A `project_pattern: ""` placeholder is chosen over `"*"` for two
// reasons. First, a literal `"*"` would still be evaluated by the pre-
// HOL-600 resolver for render targets the binding does NOT cover (new
// projects, new deployments), broadening the policy's effective set
// during the transition window — the HOL-596 resolver only bypasses the
// glob for the specific render targets named by a matching binding, not
// for every target that shares the policy. Second, `"*"` is a legitimate
// pre-migration glob value, so a placeholder that picks the same token
// would make the idempotency classifier unable to tell a legacy wildcard
// rule apart from a migrator-cleared rule, silently skipping migration
// of policies that actually still need one.
//
// HOL-600 removed the validator that rejected empty project_pattern
// values, so a migrated (cleared-Target) policy can be updated through
// the UI/API again. The empty-string form is preferred over `"*"` for
// the idempotency reasons above: it lets re-runs of the migrator
// distinguish a cleared rule from a legacy wildcard one.
func marshalClearedRuleWires(rules []ruleWire) ([]byte, error) {
	// rules already carry their cleared Target. Re-use the wire shape
	// so the marshaler emits the same JSON the runtime reads and the
	// on-disk payload matches what the migrator originally parsed.
	b, err := json.Marshal(rules)
	if err != nil {
		return nil, fmt.Errorf("serializing cleared template policy rules: %w", err)
	}
	return b, nil
}

// printMigrationPlan writes a human-readable plan to w. Operators diff the
// dry-run output against the --apply output to confirm nothing changed
// between the two runs — the migrator renders the same text in both modes.
// The write errors are intentionally discarded — the migrator is a CLI
// subcommand and its output sink is stdout / a test bytes.Buffer; a write
// failure on either is unrecoverable and not worth bubbling up over the
// mutation-result return.
func printMigrationPlan(w io.Writer, plans []*PolicyMigrationPlan) {
	if len(plans) == 0 {
		_, _ = fmt.Fprintln(w, "No TemplatePolicy objects carry non-empty Target globs; nothing to migrate.")
		return
	}
	_, _ = fmt.Fprintf(w, "Planning migration for %d policy/policies:\n", len(plans))
	for _, plan := range plans {
		_, _ = fmt.Fprintf(w, "\npolicy %s/%s -> binding %s/%s (scope=%s, scope_name=%s)\n",
			plan.PolicyNamespace, plan.PolicyName,
			plan.PolicyNamespace, plan.BindingName,
			scopeDisplay(plan.Scope), plan.ScopeName,
		)
		if len(plan.TargetRefs) == 0 {
			_, _ = fmt.Fprintln(w, "  targets: <none> (globs matched no render targets)")
		} else {
			_, _ = fmt.Fprintln(w, "  targets:")
			for _, ref := range plan.TargetRefs {
				_, _ = fmt.Fprintf(w, "    - kind=%s project=%s name=%s\n",
					bindingTargetKindString(ref.GetKind()), ref.GetProjectName(), ref.GetName())
			}
		}
		switch {
		case plan.BindingExists && plan.BindingTargetsMatch:
			_, _ = fmt.Fprintln(w, "  status: binding already exists with matching targets; will clear policy Target globs only")
		case plan.BindingExists && !plan.BindingTargetsMatch:
			_, _ = fmt.Fprintln(w, "  status: CONFLICT — existing binding has different targets; no changes will be made")
		case plan.EmptyTargets:
			_, _ = fmt.Fprintln(w, "  status: globs matched no live render targets; will clear policy Target globs only (no binding is written because the binding handler rejects an empty target_refs list)")
		default:
			_, _ = fmt.Fprintln(w, "  status: will create new binding and clear policy Target globs")
		}
		for _, note := range plan.Notes {
			_, _ = fmt.Fprintf(w, "  note: %s\n", note)
		}
	}
}

// printMigrationSummary prints a final tally so operators can confirm the
// run finished. The wording distinguishes dry-run from apply so a skimming
// reader cannot confuse a preview with a completed mutation. Write errors
// are discarded for the same reason as in printMigrationPlan.
func printMigrationSummary(w io.Writer, res *MigrationResult, apply bool) {
	if res == nil {
		return
	}
	verb := "would create"
	policyVerb := "would clear targets on"
	if apply {
		verb = "created"
		policyVerb = "cleared targets on"
	}
	_, _ = fmt.Fprintf(w, "\nSummary: %d %s bindings; %s %d policies; %d skipped (already migrated); %d conflicts.\n",
		res.BindingsCreated, verb, policyVerb, res.PoliciesUpdated, res.Skipped, res.Conflicts)
	if !apply {
		_, _ = fmt.Fprintln(w, "Re-run with --apply to mutate the cluster.")
	}
}

// scopeDisplay returns a short, operator-friendly label for a TemplateScope.
// Used only in the printed plan.
func scopeDisplay(scope scopeshim.Scope) string {
	switch scope {
	case scopeshim.ScopeOrganization:
		return "organization"
	case scopeshim.ScopeFolder:
		return "folder"
	default:
		return strings.ToLower(scope.String())
	}
}
