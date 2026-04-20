package policyresolver

import (
	"context"
	"fmt"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// AppliedRenderStateClient persists and reads the effective set of
// LinkedTemplateRef values last applied to a render target (deployment or
// project-scope template). Storage is a `RenderState` CRD object in the
// *folder* namespace that owns the project — NEVER the project namespace
// itself (HOL-554 storage-isolation, enforced at admission by the
// ValidatingAdmissionPolicy `renderstate-folder-or-org-only`). When the
// project's immediate parent is an organization (i.e. no intervening
// folder), the organization namespace is used as the storage location.
//
// A RenderState object is keyed by `(targetKind, targetName)` within a
// namespace, so multiple deployments or templates under the same project
// each get their own record. The record carries the project slug both on
// `spec.project` and on a label so callers can list by project without
// re-walking the ancestor chain.
//
// HOL-694 migrated this client off ConfigMap storage onto the dedicated
// `RenderState` CRD (ADR 033). Reads and writes both go through the
// controller-runtime `client.Client`; in production that client is the
// embedded Manager's cache-backed client, primed with a RenderState
// informer so drift-check reads land in the shared informer cache
// alongside policy and binding reads.
type AppliedRenderStateClient struct {
	client   ctrlclient.Client
	resolver *resolver.Resolver
	walker   WalkerInterface
}

// WalkerInterface is the subset of resolver.Walker used for folder-namespace
// resolution. Defined here so tests can inject a lightweight fake without
// standing up a full Kubernetes fixture for the walker itself.
type WalkerInterface interface {
	WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error)
}

// NewAppliedRenderStateClient creates a client that reads and writes
// applied-render-set records as RenderState CRDs in the folder namespace
// that owns a project. The controller-runtime client backs both reads and
// writes; production wires the embedded Manager's cache-backed client so
// reads land in the shared informer cache.
func NewAppliedRenderStateClient(client ctrlclient.Client, r *resolver.Resolver, w WalkerInterface) *AppliedRenderStateClient {
	return &AppliedRenderStateClient{client: client, resolver: r, walker: w}
}

// FolderNamespaceForProject returns the namespace that owns a project's
// applied-render-set records. The walk goes up from projectNs; the first
// non-project ancestor wins. If the project's immediate parent is the
// organization namespace (no folder in between), the organization namespace
// is returned. Returns an error only when the walker fails — the project
// namespace always has at least one ancestor because of the hierarchy
// invariants enforced elsewhere (ADR 020 Decision 3).
func (c *AppliedRenderStateClient) FolderNamespaceForProject(ctx context.Context, projectNs string) (string, error) {
	if c == nil || c.walker == nil {
		return "", fmt.Errorf("applied render state client is not configured with a walker")
	}
	if c.resolver == nil {
		return "", fmt.Errorf("applied render state client is not configured with a resolver")
	}
	chain, err := c.walker.WalkAncestors(ctx, projectNs)
	if err != nil {
		return "", fmt.Errorf("walking ancestor chain for project namespace %q: %w", projectNs, err)
	}
	// chain[0] is the starting namespace (projectNs). The first ancestor
	// (chain[1]) is the owning folder or organization namespace.
	for i, ns := range chain {
		if i == 0 {
			continue
		}
		kind, _, err := c.resolver.ResourceTypeFromNamespace(ns.Name)
		if err != nil {
			// A prefix mismatch means a non-managed namespace leaked into
			// the chain. Skip it — the walker will still report the real
			// folder/org namespace further up.
			continue
		}
		if kind == v1alpha2.ResourceTypeProject {
			// Project-on-project nesting is not supported in production,
			// but guard defensively so a misconfigured fixture does not
			// pick a forbidden storage namespace.
			continue
		}
		return ns.Name, nil
	}
	return "", fmt.Errorf("no folder or organization ancestor for project namespace %q", projectNs)
}

// refsToRenderStateRefs converts the wire-side LinkedTemplateRef proto into
// the structured form stored on RenderState.spec.appliedRefs. Nil entries
// are skipped so a caller passing an unfiltered resolver output produces a
// stable document.
func refsToRenderStateRefs(refs []*consolev1.LinkedTemplateRef) []templatesv1alpha1.RenderStateLinkedTemplateRef {
	stored := make([]templatesv1alpha1.RenderStateLinkedTemplateRef, 0, len(refs))
	for _, r := range refs {
		if r == nil {
			continue
		}
		stored = append(stored, templatesv1alpha1.RenderStateLinkedTemplateRef{
			Namespace:         r.GetNamespace(),
			Name:              r.GetName(),
			VersionConstraint: r.GetVersionConstraint(),
		})
	}
	return stored
}

// renderStateRefsToProto inverts refsToRenderStateRefs.
func renderStateRefsToProto(stored []templatesv1alpha1.RenderStateLinkedTemplateRef) []*consolev1.LinkedTemplateRef {
	refs := make([]*consolev1.LinkedTemplateRef, 0, len(stored))
	for _, s := range stored {
		refs = append(refs, &consolev1.LinkedTemplateRef{
			Namespace:         s.Namespace,
			Name:              s.Name,
			VersionConstraint: s.VersionConstraint,
		})
	}
	return refs
}

// RecordAppliedRenderSet writes the resolved render set to the folder or
// organization namespace that owns the project. The record is a RenderState
// object named `<targetKind>-<project>-<targetName>`; collisions are
// impossible within the same folder namespace because (targetKind, project,
// targetName) is unique per deployment or project-scope template. Callers
// invoke this helper from CreateDeployment, UpdateDeployment, CreateTemplate
// (project), and UpdateTemplate (project) on the success path only.
//
// A nil client returns nil without error so call sites that run in
// test/dry-run modes without a Kubernetes client do not need conditional
// logic.
func (c *AppliedRenderStateClient) RecordAppliedRenderSet(
	ctx context.Context,
	projectNs string,
	targetKind TargetKind,
	targetName string,
	refs []*consolev1.LinkedTemplateRef,
) error {
	if c == nil || c.client == nil {
		return nil
	}
	if projectNs == "" {
		return fmt.Errorf("projectNs is required")
	}
	if targetName == "" {
		return fmt.Errorf("targetName is required")
	}

	folderNs, err := c.FolderNamespaceForProject(ctx, projectNs)
	if err != nil {
		return fmt.Errorf("resolving folder namespace for project %q: %w", projectNs, err)
	}

	project, projectErr := c.resolver.ProjectFromNamespace(projectNs)
	if projectErr != nil {
		return fmt.Errorf("extracting project name from namespace %q: %w", projectNs, projectErr)
	}

	rsName := renderStateObjectName(targetKind, project, targetName)
	rsTargetKind, err := renderStateTargetKindEnum(targetKind)
	if err != nil {
		return err
	}
	desiredSpec := templatesv1alpha1.RenderStateSpec{
		TargetKind:  rsTargetKind,
		TargetName:  targetName,
		Project:     project,
		AppliedRefs: refsToRenderStateRefs(refs),
	}
	desiredLabels := map[string]string{
		v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
		v1alpha2.LabelProject:   project,
	}

	slog.DebugContext(ctx, "recording applied render set",
		slog.String("folderNamespace", folderNs),
		slog.String("projectNamespace", projectNs),
		slog.String("project", project),
		slog.String("targetKind", string(rsTargetKind)),
		slog.String("targetName", targetName),
		slog.String("renderState", rsName),
		slog.Int("refs", len(refs)),
	)

	rs := &templatesv1alpha1.RenderState{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rsName,
			Namespace: folderNs,
			Labels:    desiredLabels,
		},
		Spec: desiredSpec,
	}

	// Try to create first. On AlreadyExists, fall through to Update so
	// re-applying an unchanged render set is idempotent and an edit that
	// shrinks the applied set still overwrites the stored value.
	createErr := c.client.Create(ctx, rs)
	if createErr == nil {
		return nil
	}
	if !k8serrors.IsAlreadyExists(createErr) {
		return fmt.Errorf("creating RenderState %q in %q: %w", rsName, folderNs, createErr)
	}

	existing := &templatesv1alpha1.RenderState{}
	if getErr := c.client.Get(ctx, types.NamespacedName{Namespace: folderNs, Name: rsName}, existing); getErr != nil {
		return fmt.Errorf("getting RenderState %q in %q for update: %w", rsName, folderNs, getErr)
	}
	existing.Spec = desiredSpec
	if existing.Labels == nil {
		existing.Labels = make(map[string]string, len(desiredLabels))
	}
	for k, v := range desiredLabels {
		existing.Labels[k] = v
	}
	if updateErr := c.client.Update(ctx, existing); updateErr != nil {
		return fmt.Errorf("updating RenderState %q in %q: %w", rsName, folderNs, updateErr)
	}
	return nil
}

// ReadAppliedRenderSet returns the applied render set last recorded for the
// target. Returns an empty slice (and ok=false) when no record exists; this
// lets callers treat "never applied" and "empty applied set" as distinct
// states (the latter round-trips the empty list and returns ok=true).
//
// The read consults ONLY folder/organization namespace storage; any
// RenderState artifact sitting in a project namespace is ignored. Operators
// migrating from a stale fixture can observe this by seeing
// GetDeploymentPolicyState report no drift even while a project-namespace
// snapshot carries a different value.
func (c *AppliedRenderStateClient) ReadAppliedRenderSet(
	ctx context.Context,
	projectNs string,
	targetKind TargetKind,
	targetName string,
) (refs []*consolev1.LinkedTemplateRef, ok bool, err error) {
	if c == nil || c.client == nil {
		return nil, false, nil
	}
	if projectNs == "" {
		return nil, false, fmt.Errorf("projectNs is required")
	}
	if targetName == "" {
		return nil, false, fmt.Errorf("targetName is required")
	}

	folderNs, err := c.FolderNamespaceForProject(ctx, projectNs)
	if err != nil {
		return nil, false, fmt.Errorf("resolving folder namespace for project %q: %w", projectNs, err)
	}
	project, projectErr := c.resolver.ProjectFromNamespace(projectNs)
	if projectErr != nil {
		return nil, false, fmt.Errorf("extracting project name from namespace %q: %w", projectNs, projectErr)
	}

	rsName := renderStateObjectName(targetKind, project, targetName)
	rs := &templatesv1alpha1.RenderState{}
	if getErr := c.client.Get(ctx, types.NamespacedName{Namespace: folderNs, Name: rsName}, rs); getErr != nil {
		if k8serrors.IsNotFound(getErr) {
			return []*consolev1.LinkedTemplateRef{}, false, nil
		}
		return nil, false, fmt.Errorf("getting RenderState %q in %q: %w", rsName, folderNs, getErr)
	}
	return renderStateRefsToProto(rs.Spec.AppliedRefs), true, nil
}

// renderStateTargetKindEnum maps a resolver.TargetKind to the structured
// CRD enum value written to RenderState.spec.targetKind.
func renderStateTargetKindEnum(kind TargetKind) (templatesv1alpha1.RenderTargetKind, error) {
	switch kind {
	case TargetKindDeployment:
		return templatesv1alpha1.RenderTargetKindDeployment, nil
	case TargetKindProjectTemplate:
		return templatesv1alpha1.RenderTargetKindProjectTemplate, nil
	default:
		return "", fmt.Errorf("unknown render target kind %v", kind)
	}
}

// renderStateTargetKindNameSegment returns the lowercase slug embedded in
// the RenderState object name for a given TargetKind. The segment matches
// the legacy ConfigMap naming so operators inspecting kubectl output do not
// see a sudden naming shift across the migration.
func renderStateTargetKindNameSegment(kind TargetKind) string {
	switch kind {
	case TargetKindDeployment:
		return "deployment"
	case TargetKindProjectTemplate:
		return "project-template"
	default:
		return ""
	}
}

// renderStateObjectName builds the deterministic object name that stores
// the applied render set for a given target. The name encodes
// (kind, project, target) so multiple projects and multiple render targets
// can coexist in the same folder namespace.
//
// The name is bounded by the Kubernetes 253-char object-name limit. Each
// project and target name is itself a DNS label (max 63 each), and the
// kind prefix is a literal, so the concatenation cannot overflow.
func renderStateObjectName(kind TargetKind, project, target string) string {
	return fmt.Sprintf("render-state-%s-%s-%s", renderStateTargetKindNameSegment(kind), project, target)
}

// DiffRenderSets classifies refs as (added, removed, drifted) given a prior
// applied set and a newly resolved set. Ordering is normalized: added holds
// refs present in current but not applied; removed holds refs present in
// applied but not current. `drifted` is true iff added or removed is
// non-empty.
//
// The comparison key is `(namespace, name, version_constraint)` — two refs
// that differ only by version constraint are treated as distinct because
// tightening a version constraint is itself drift worth surfacing.
//
// Key asymmetry with the resolver's dedup: the resolver deduplicates on the
// `(namespace, name)` pair only (see RefKey in folder_resolver.go), so when
// an explicit (owner-linked) ref and a REQUIRE rule name the same template
// with different version constraints, the explicit ref wins and the REQUIRE
// rule's constraint is dropped. Consequently, a REQUIRE-only change to a
// version constraint (same template name) will not surface as drift if the
// template is also explicitly linked on the target. This is intentional per
// TestFolderResolver_DedupRespectsExplicit — the owner's choice is
// authoritative. REQUIRE-only constraint changes on non-explicit refs and
// any change to an explicit ref's constraint both surface here correctly.
func DiffRenderSets(applied, current []*consolev1.LinkedTemplateRef) (added, removed []*consolev1.LinkedTemplateRef, drifted bool) {
	appliedSet := make(map[refKey]*consolev1.LinkedTemplateRef, len(applied))
	for _, r := range applied {
		if r == nil {
			continue
		}
		appliedSet[keyForRef(r)] = r
	}
	currentSet := make(map[refKey]*consolev1.LinkedTemplateRef, len(current))
	for _, r := range current {
		if r == nil {
			continue
		}
		currentSet[keyForRef(r)] = r
	}
	for k, r := range currentSet {
		if _, ok := appliedSet[k]; !ok {
			added = append(added, r)
		}
	}
	for k, r := range appliedSet {
		if _, ok := currentSet[k]; !ok {
			removed = append(removed, r)
		}
	}
	drifted = len(added) > 0 || len(removed) > 0
	return added, removed, drifted
}

// refKey normalizes a LinkedTemplateRef to its comparison tuple.
type refKey struct {
	namespace         string
	name              string
	versionConstraint string
}

func keyForRef(r *consolev1.LinkedTemplateRef) refKey {
	return refKey{
		namespace:         r.GetNamespace(),
		name:              r.GetName(),
		versionConstraint: r.GetVersionConstraint(),
	}
}
