// Command holos-console-migrate-rbac is a one-shot operator-run migration
// tool that translates the legacy Secret-sharing annotations
// (`console.holos.run/share-users`, `console.holos.run/share-roles`,
// `console.holos.run/default-share-users`,
// `console.holos.run/default-share-roles`) into native Kubernetes
// RoleBindings against the project-scoped Roles provisioned during
// HOL-1032 (Phase 4).
//
// The tool runs in dry-run mode by default; pass --apply to perform
// writes. It is idempotent: re-running it after a successful migration is
// a no-op once every annotation has been translated and stripped.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/secretrbac"
	"github.com/holos-run/holos-console/console/secrets"
	"github.com/holos-run/holos-console/console/sharing/legacy"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		// flag.ContinueOnError surfaces --help as flag.ErrHelp; the
		// flag package has already printed usage to stderr, so exit
		// cleanly rather than spamming `error: flag: help requested`.
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// options control a migration run. Exposed for testing.
type options struct {
	apply      bool
	kubeconfig string
}

func parseFlags(args []string, errOut io.Writer) (*options, error) {
	fs := flag.NewFlagSet("holos-console-migrate-rbac", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		fmt.Fprintf(errOut, "Usage: %s [flags]\n\n", fs.Name())
		fmt.Fprintln(errOut, "One-shot migration: translate legacy Secret-sharing annotations into RoleBindings.")
		fmt.Fprintln(errOut, "Runs as the console service-account (the operator-supplied kubeconfig).")
		fmt.Fprintln(errOut)
		fmt.Fprintln(errOut, "Flags:")
		fs.PrintDefaults()
	}
	opts := &options{}
	fs.BoolVar(&opts.apply, "apply", false, "Perform writes. Without --apply the tool only reports planned changes.")
	defaultKubeconfig := ""
	if home := homedir.HomeDir(); home != "" {
		defaultKubeconfig = filepath.Join(home, ".kube", "config")
	}
	fs.StringVar(&opts.kubeconfig, "kubeconfig", defaultKubeconfig, "Path to kubeconfig (defaults to in-cluster config when empty)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return opts, nil
}

func run(args []string, stdout, stderr io.Writer) error {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}
	client, err := buildClient(opts.kubeconfig)
	if err != nil {
		return fmt.Errorf("building kube client: %w", err)
	}
	ctx := context.Background()
	report, err := Migrate(ctx, client, opts.apply)
	if err != nil {
		return err
	}
	return PrintReport(stdout, report, opts.apply)
}

func buildClient(kubeconfig string) (kubernetes.Interface, error) {
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
		&clientcmd.ConfigOverrides{},
	)
	cfg, err := loader.ClientConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

// NamespaceReport captures the per-namespace migration plan and the
// outcome when --apply is set. It is the building block of Report and is
// rendered by PrintReport in stable key order.
type NamespaceReport struct {
	Namespace string
	// BindingsCreated names the RoleBindings the tool created (or would
	// create in dry-run mode) keyed by binding name.
	BindingsCreated []string
	// BindingsAlreadyPresent names the RoleBindings that already existed
	// with the desired RoleRef and Subjects, so the tool skipped them.
	BindingsAlreadyPresent []string
	// NamespaceAnnotationsStripped lists the keys removed from the
	// namespace itself.
	NamespaceAnnotationsStripped []string
	// SecretsProcessed records one entry per managed Secret in the
	// namespace.
	SecretsProcessed []SecretReport
	// Warnings collects non-fatal observations (e.g. time-bounded grant
	// dropped, malformed annotation skipped, etc.) so the operator can
	// review them before re-running.
	Warnings []string
	// Error captures a per-namespace fatal error so a single failed
	// namespace does not abort the whole sweep.
	Error string
}

// SecretReport records the result for a single Secret in a namespace.
type SecretReport struct {
	Name                string
	AnnotationsStripped []string
}

// Report is the aggregate result of a Migrate call.
type Report struct {
	Namespaces []NamespaceReport
}

// Migrate walks every namespace labelled with
// `app.kubernetes.io/managed-by=console.holos.run`, parses the legacy
// share annotations on the namespace itself and on every managed
// `v1.Secret` in that namespace, materialises the equivalent
// RoleBindings against the project-scoped Roles, and (when apply is
// true) strips the annotations after the bindings are confirmed.
//
// The returned Report contains one NamespaceReport per managed namespace
// in sorted order. A per-namespace error stops processing of that
// namespace but does not abort the sweep, so the operator gets a single
// rollup of every problem encountered.
func Migrate(ctx context.Context, client kubernetes.Interface, apply bool) (*Report, error) {
	report := &Report{}
	selector := labels.SelectorFromSet(labels.Set{
		v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
	})
	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing managed namespaces: %w", err)
	}
	sort.Slice(nsList.Items, func(i, j int) bool {
		return nsList.Items[i].Name < nsList.Items[j].Name
	})
	for i := range nsList.Items {
		nr := migrateNamespace(ctx, client, &nsList.Items[i], apply)
		report.Namespaces = append(report.Namespaces, nr)
	}
	return report, nil
}

// migrateNamespace processes a single managed namespace.
func migrateNamespace(ctx context.Context, client kubernetes.Interface, ns *corev1.Namespace, apply bool) NamespaceReport {
	nr := NamespaceReport{Namespace: ns.Name}

	// Only project namespaces hold actionable share-users / share-roles
	// grants. Org and folder namespaces only ever held the
	// default-share-* annotations, which seed projects rather than
	// represent grants directly. Those are passed through unchanged so
	// the cascade chain on new-project creation continues to work
	// during the upgrade window.
	if ns.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeProject {
		nr.Warnings = append(nr.Warnings, fmt.Sprintf("skipping non-project namespace (resource-type=%q): default-share-* annotations remain in place to preserve the cascade chain", ns.Labels[v1alpha2.LabelResourceType]))
		return nr
	}
	rolesPresent, err := projectRolesPresent(ctx, client, ns.Name)
	if err != nil {
		nr.Error = fmt.Sprintf("checking for project Roles: %v", err)
		return nr
	}
	if !rolesPresent {
		nr.Error = fmt.Sprintf("project secret Roles missing in %q — run the Phase 4 reconciler before re-running the migration", ns.Name)
		return nr
	}

	// Translate namespace-level grants. share-users and share-roles on
	// a project namespace seed the project-level Secret RoleBindings.
	nsUsers, nsUsersWarn := parseAnnotation(ns.Annotations, v1alpha2.AnnotationShareUsers, "namespace", ns.Name)
	nsRoles, nsRolesWarn := parseAnnotation(ns.Annotations, v1alpha2.AnnotationShareRoles, "namespace", ns.Name)
	nr.Warnings = append(nr.Warnings, nsUsersWarn...)
	nr.Warnings = append(nr.Warnings, nsRolesWarn...)
	if nsUsersWarn != nil || nsRolesWarn != nil {
		// Malformed JSON on the namespace itself: stop before we touch
		// any binding. The operator must hand-fix the JSON first.
		nr.Error = "malformed namespace annotation — see warnings"
		return nr
	}
	nsUsers, uTimeWarn := filterTimeBounded(nsUsers, "namespace", ns.Name)
	nsRoles, rTimeWarn := filterTimeBounded(nsRoles, "namespace", ns.Name)
	nr.Warnings = append(nr.Warnings, uTimeWarn...)
	nr.Warnings = append(nr.Warnings, rTimeWarn...)

	desired := buildDesiredBindings(ns.Name, nsUsers, nsRoles)

	// Translate per-Secret grants. Under ADR 036 all Secret access in a
	// project is namespace-scoped, so per-Secret grants collapse onto
	// the same project-level Role: every (target, principal, role)
	// triple yields the same binding regardless of which Secret it
	// came from.
	secretList, err := client.CoreV1().Secrets(ns.Name).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
		}).String(),
	})
	if err != nil {
		nr.Error = fmt.Sprintf("listing managed Secrets: %v", err)
		return nr
	}
	sort.Slice(secretList.Items, func(i, j int) bool {
		return secretList.Items[i].Name < secretList.Items[j].Name
	})
	for i := range secretList.Items {
		secret := &secretList.Items[i]
		sr := SecretReport{Name: secret.Name}
		users, sUsersWarn := parseAnnotation(secret.Annotations, v1alpha2.AnnotationShareUsers, "secret", ns.Name+"/"+secret.Name)
		roles, sRolesWarn := parseAnnotation(secret.Annotations, v1alpha2.AnnotationShareRoles, "secret", ns.Name+"/"+secret.Name)
		nr.Warnings = append(nr.Warnings, sUsersWarn...)
		nr.Warnings = append(nr.Warnings, sRolesWarn...)
		if sUsersWarn != nil || sRolesWarn != nil {
			// A malformed Secret annotation must not be silently
			// stripped. Surface it; do not attempt to merge or
			// strip this Secret on the current run.
			nr.SecretsProcessed = append(nr.SecretsProcessed, sr)
			continue
		}
		users, uWarn := filterTimeBounded(users, "secret", ns.Name+"/"+secret.Name)
		roles, rWarn := filterTimeBounded(roles, "secret", ns.Name+"/"+secret.Name)
		nr.Warnings = append(nr.Warnings, uWarn...)
		nr.Warnings = append(nr.Warnings, rWarn...)
		mergeBindings(desired, ns.Name, users, roles)

		// Strip the share annotations after the loop body has merged
		// them into the desired set. The actual write is deferred
		// until every desired binding has been applied below.
		// Recording the planned strip here keeps the per-secret
		// summary table accurate in dry-run.
		if hasAnyShareAnnotation(secret.Annotations) {
			sr.AnnotationsStripped = listShareAnnotations(secret.Annotations)
		}
		nr.SecretsProcessed = append(nr.SecretsProcessed, sr)
	}

	// Reconcile the desired RoleBindings against the existing state.
	// Idempotency comes from comparing existing bindings (matched by
	// name) against the desired set: an already-present binding whose
	// RoleRef and Subjects match is left alone.
	bindingNames := make([]string, 0, len(desired))
	for name := range desired {
		bindingNames = append(bindingNames, name)
	}
	sort.Strings(bindingNames)
	for _, name := range bindingNames {
		binding := desired[name]
		existing, err := client.RbacV1().RoleBindings(ns.Name).Get(ctx, name, metav1.GetOptions{})
		switch {
		case err == nil:
			if roleBindingsMatch(existing, binding) {
				nr.BindingsAlreadyPresent = append(nr.BindingsAlreadyPresent, name)
				continue
			}
			if apply {
				if err := client.RbacV1().RoleBindings(ns.Name).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
					nr.Error = fmt.Sprintf("deleting stale RoleBinding %q: %v", name, err)
					return nr
				}
				if _, err := client.RbacV1().RoleBindings(ns.Name).Create(ctx, binding, metav1.CreateOptions{}); err != nil {
					nr.Error = fmt.Sprintf("recreating RoleBinding %q: %v", name, err)
					return nr
				}
			}
			nr.BindingsCreated = append(nr.BindingsCreated, name)
		case apierrors.IsNotFound(err):
			if apply {
				if _, err := client.RbacV1().RoleBindings(ns.Name).Create(ctx, binding, metav1.CreateOptions{}); err != nil {
					nr.Error = fmt.Sprintf("creating RoleBinding %q: %v", name, err)
					return nr
				}
			}
			nr.BindingsCreated = append(nr.BindingsCreated, name)
		default:
			nr.Error = fmt.Sprintf("getting RoleBinding %q: %v", name, err)
			return nr
		}
	}

	// Strip annotations only after every binding has been confirmed.
	// Invariant: the migration never removes an annotation whose
	// translated binding is not yet in place. On a partial-failure
	// mid-loop the next run picks up where the previous one left off.
	if apply {
		for i := range secretList.Items {
			secret := &secretList.Items[i]
			if !hasAnyShareAnnotation(secret.Annotations) {
				continue
			}
			// Skip secrets whose previous parse failed: their
			// annotations are still present but malformed.
			if hasMalformedShareAnnotation(secret.Annotations) {
				continue
			}
			if _, err := stripSecretAnnotations(ctx, client, secret); err != nil {
				nr.Error = fmt.Sprintf("stripping annotations from Secret %q: %v", secret.Name, err)
				return nr
			}
		}
	}
	if hasAnyShareAnnotation(ns.Annotations) {
		if apply {
			stripped, err := stripNamespaceAnnotations(ctx, client, ns)
			if err != nil {
				nr.Error = fmt.Sprintf("stripping namespace annotations: %v", err)
				return nr
			}
			nr.NamespaceAnnotationsStripped = stripped
		} else {
			nr.NamespaceAnnotationsStripped = listShareAnnotations(ns.Annotations)
		}
	}
	return nr
}

// projectRolesPresent verifies all three project-secret Roles are
// present in the namespace. It looks them up by their stable names
// rather than by label so a partially relabelled cluster still gets a
// definitive "missing" answer.
func projectRolesPresent(ctx context.Context, client kubernetes.Interface, namespace string) (bool, error) {
	for _, role := range []string{
		secretrbac.RoleName(secretrbac.RoleViewer),
		secretrbac.RoleName(secretrbac.RoleEditor),
		secretrbac.RoleName(secretrbac.RoleOwner),
	} {
		_, err := client.RbacV1().Roles(namespace).Get(ctx, role, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// parseAnnotation wraps legacy.ParseGrants and converts a malformed
// annotation into an explicit warning. The caller is expected to skip
// the offending resource until the operator hand-fixes the JSON; this
// function returns nil grants in that case so the caller's "no grants"
// path runs.
func parseAnnotation(annotations map[string]string, key, kind, qualifiedName string) ([]secrets.AnnotationGrant, []string) {
	grants, err := legacy.ParseGrants(annotations, key)
	if err != nil {
		return nil, []string{fmt.Sprintf("MALFORMED %s %s annotation %s: %v — skipping this resource until the operator hand-fixes the JSON", kind, qualifiedName, key, err)}
	}
	return grants, nil
}

// filterTimeBounded drops grants that carry an `nbf` or `exp` timestamp
// and emits one warning per dropped grant. ADR 036 commits to dropping
// the time-bounded-grant feature for the MVP; the migration preserves
// that decision visibly so an operator can see exactly which grants
// would have to be re-issued in a future release.
func filterTimeBounded(grants []secrets.AnnotationGrant, kind, qualifiedName string) ([]secrets.AnnotationGrant, []string) {
	out := grants[:0:0]
	var warnings []string
	for _, g := range grants {
		if g.Nbf != nil || g.Exp != nil {
			warnings = append(warnings, fmt.Sprintf("DROPPING time-bounded grant on %s %s: principal=%q role=%q nbf=%v exp=%v — re-issue manually if still required", kind, qualifiedName, g.Principal, g.Role, derefInt64(g.Nbf), derefInt64(g.Exp)))
			continue
		}
		out = append(out, g)
	}
	return out, warnings
}

func derefInt64(p *int64) any {
	if p == nil {
		return "nil"
	}
	return *p
}

// buildDesiredBindings starts a desired-bindings map seeded with the
// namespace-level grants. The result is keyed by RoleBinding name so
// later per-Secret grants merge on the same key (deduplicating
// principals at the highest role level via secrets.DeduplicateGrants
// before encoding).
func buildDesiredBindings(namespace string, users, roles []secrets.AnnotationGrant) map[string]*rbacv1.RoleBinding {
	desired := map[string]*rbacv1.RoleBinding{}
	mergeBindings(desired, namespace, users, roles)
	return desired
}

// mergeBindings adds every (user-target) and (group-target) grant from
// the input slices into desired, keyed by binding name. When two
// callers contribute the same (target, principal) pair at different
// roles, the highest-role wins because secretrbac.RoleBindingName
// includes the role in the binding name — different roles produce
// different binding names rather than colliding.
func mergeBindings(desired map[string]*rbacv1.RoleBinding, namespace string, users, roles []secrets.AnnotationGrant) {
	for _, g := range secrets.DeduplicateGrants(users) {
		if g.Principal == "" {
			continue
		}
		b := secretrbac.RoleBinding(namespace, secretrbac.ShareTargetUser, g.Principal, g.Role, nil)
		if _, ok := desired[b.Name]; ok {
			continue
		}
		desired[b.Name] = b
	}
	for _, g := range secrets.DeduplicateGrants(roles) {
		if g.Principal == "" {
			continue
		}
		b := secretrbac.RoleBinding(namespace, secretrbac.ShareTargetGroup, g.Principal, g.Role, nil)
		if _, ok := desired[b.Name]; ok {
			continue
		}
		desired[b.Name] = b
	}
}

// roleBindingsMatch decides whether an existing binding already
// represents the desired state. The comparison covers the RoleRef and
// the Subjects slice (order-independent for the small slices the
// migration produces).
func roleBindingsMatch(existing, desired *rbacv1.RoleBinding) bool {
	if existing.RoleRef != desired.RoleRef {
		return false
	}
	if len(existing.Subjects) != len(desired.Subjects) {
		return false
	}
	seen := map[string]bool{}
	for _, s := range existing.Subjects {
		seen[subjectKey(s)] = true
	}
	for _, s := range desired.Subjects {
		if !seen[subjectKey(s)] {
			return false
		}
	}
	return true
}

func subjectKey(s rbacv1.Subject) string {
	return s.APIGroup + "|" + s.Kind + "|" + s.Name
}

// hasAnyShareAnnotation reports whether at least one of the legacy
// sharing annotation keys is present.
func hasAnyShareAnnotation(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}
	for _, key := range legacy.AnnotationKeys() {
		if _, ok := annotations[key]; ok {
			return true
		}
	}
	return false
}

// hasMalformedShareAnnotation re-runs the legacy parser to detect a
// malformed annotation. We use this on the strip pass so we never strip
// a Secret whose previous parse returned an error.
func hasMalformedShareAnnotation(annotations map[string]string) bool {
	if _, err := legacy.ShareUsers(annotations); err != nil {
		return true
	}
	if _, err := legacy.ShareRoles(annotations); err != nil {
		return true
	}
	return false
}

// listShareAnnotations returns the legacy keys present on annotations,
// in canonical order.
func listShareAnnotations(annotations map[string]string) []string {
	var out []string
	if annotations == nil {
		return out
	}
	for _, key := range legacy.AnnotationKeys() {
		if _, ok := annotations[key]; ok {
			out = append(out, key)
		}
	}
	return out
}

// stripSecretAnnotations removes every legacy share annotation from a
// Secret and returns the stripped key names. The Secret is rewritten
// via Update; the caller's next migration run re-attempts on conflict.
func stripSecretAnnotations(ctx context.Context, client kubernetes.Interface, secret *corev1.Secret) ([]string, error) {
	stripped := listShareAnnotations(secret.Annotations)
	if len(stripped) == 0 {
		return nil, nil
	}
	live, err := client.CoreV1().Secrets(secret.Namespace).Get(ctx, secret.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if live.Annotations == nil {
		return nil, nil
	}
	for _, key := range stripped {
		delete(live.Annotations, key)
	}
	if _, err := client.CoreV1().Secrets(secret.Namespace).Update(ctx, live, metav1.UpdateOptions{}); err != nil {
		return nil, err
	}
	return stripped, nil
}

// stripNamespaceAnnotations removes the legacy share-users and
// share-roles annotations from a project namespace. Default-share-*
// annotations are intentionally preserved: they seed new projects via
// the cascade chain and have no equivalent RoleBinding.
func stripNamespaceAnnotations(ctx context.Context, client kubernetes.Interface, ns *corev1.Namespace) ([]string, error) {
	target := []string{
		v1alpha2.AnnotationShareUsers,
		v1alpha2.AnnotationShareRoles,
	}
	stripped := make([]string, 0, len(target))
	for _, key := range target {
		if _, ok := ns.Annotations[key]; ok {
			stripped = append(stripped, key)
		}
	}
	if len(stripped) == 0 {
		return nil, nil
	}
	live, err := client.CoreV1().Namespaces().Get(ctx, ns.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if live.Annotations == nil {
		return nil, nil
	}
	for _, key := range stripped {
		delete(live.Annotations, key)
	}
	if _, err := client.CoreV1().Namespaces().Update(ctx, live, metav1.UpdateOptions{}); err != nil {
		return nil, err
	}
	return stripped, nil
}

// PrintReport renders a human-readable summary of a migration run.
func PrintReport(w io.Writer, report *Report, applied bool) error {
	if report == nil {
		return nil
	}
	mode := "DRY-RUN"
	if applied {
		mode = "APPLIED"
	}
	if _, err := fmt.Fprintf(w, "holos-console-migrate-rbac (%s)\n", mode); err != nil {
		return err
	}
	if len(report.Namespaces) == 0 {
		_, err := fmt.Fprintln(w, "no managed namespaces found.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tBINDINGS\tALREADY-PRESENT\tNS-ANNOTATIONS-STRIPPED\tSECRETS\tWARNINGS\tERROR")
	for _, nr := range report.Namespaces {
		errSummary := nr.Error
		if errSummary == "" {
			errSummary = "-"
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%s\n",
			nr.Namespace,
			len(nr.BindingsCreated),
			len(nr.BindingsAlreadyPresent),
			len(nr.NamespaceAnnotationsStripped),
			len(nr.SecretsProcessed),
			len(nr.Warnings),
			errSummary,
		)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	// Render warnings + errors in detail under the table so an
	// operator can copy/paste them into a follow-up runbook.
	for _, nr := range report.Namespaces {
		for _, warn := range nr.Warnings {
			if _, err := fmt.Fprintf(w, "WARN  %s: %s\n", nr.Namespace, warn); err != nil {
				return err
			}
		}
		if nr.Error != "" {
			if _, err := fmt.Fprintf(w, "ERROR %s: %s\n", nr.Namespace, nr.Error); err != nil {
				return err
			}
		}
	}
	return nil
}
