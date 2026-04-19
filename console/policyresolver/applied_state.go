package policyresolver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// AppliedRenderStateClient persists and reads the effective set of
// LinkedTemplateRef values last applied to a render target (deployment or
// project-scope template). Storage is a ConfigMap in the *folder* namespace
// that owns the project — NEVER the project namespace itself (HOL-554
// storage-isolation). When the project's immediate parent is an organization
// (i.e. no intervening folder), the organization namespace is used as the
// storage location.
//
// A render-state ConfigMap is keyed by `(targetKind, targetName)` within a
// namespace, so multiple deployments or templates under the same project each
// get their own record. The record carries the project slug on a label so
// callers can list by project without re-walking the ancestor chain.
//
// This package owns render-state storage instead of pushing it into
// console/templates because the seam lives here: the resolver computes the
// effective set, and the same package stores what was applied. Keeping the
// two helpers co-located avoids a cross-package dependency cycle between
// templates and policyresolver.
//
// HOL-622 scope decision: render-state remains on ConfigMap storage. The
// HOL-615 plan scopes the CRD migration to `Template`, `TemplatePolicy`, and
// `TemplatePolicyBinding`; the applied-render-set is an implementation
// detail of the drift surface — not a user-declared policy artifact — so it
// continues to live as a managed ConfigMap in the folder/organization
// namespace. A future phase may migrate this to a dedicated CRD, at which
// point this client will route through the controller-runtime cache like
// the policy readers above; until then the cache hit the resolver's List
// paths enjoy is the sole HOL-622 optimization.
type AppliedRenderStateClient struct {
	client   kubernetes.Interface
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
// applied-render-set records to the folder namespace that owns a project.
func NewAppliedRenderStateClient(client kubernetes.Interface, r *resolver.Resolver, w WalkerInterface) *AppliedRenderStateClient {
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

// storedLinkedRef is the JSON wire shape for applied-render-set entries. The
// `scope` field uses the same string values as the LabelTemplateScope label
// (organization/folder/project) so records can be inspected with `kubectl`
// without a proto decoder.
type storedLinkedRef struct {
	Scope             string `json:"scope"`
	ScopeName         string `json:"scope_name"`
	Name              string `json:"name"`
	VersionConstraint string `json:"version_constraint,omitempty"`
}

// MarshalAppliedRenderSet serializes a slice of LinkedTemplateRef values to
// the canonical JSON wire shape used for the AnnotationAppliedRenderSet
// annotation. Nil entries are skipped so a caller passing an unfiltered
// resolver output gets a stable document.
func MarshalAppliedRenderSet(refs []*consolev1.LinkedTemplateRef) ([]byte, error) {
	stored := make([]storedLinkedRef, 0, len(refs))
	for _, r := range refs {
		if r == nil {
			continue
		}
		stored = append(stored, storedLinkedRef{
			Scope:             scopeLabelValue(scopeshim.RefScope(r)),
			ScopeName:         scopeshim.RefScopeName(r),
			Name:              r.GetName(),
			VersionConstraint: r.GetVersionConstraint(),
		})
	}
	return json.Marshal(stored)
}

// UnmarshalAppliedRenderSet parses the canonical JSON wire shape back into
// LinkedTemplateRef values. Returns an empty slice (not nil) when raw is the
// empty string so callers can compare len() without a nil check.
func UnmarshalAppliedRenderSet(raw string) ([]*consolev1.LinkedTemplateRef, error) {
	if raw == "" {
		return []*consolev1.LinkedTemplateRef{}, nil
	}
	var stored []storedLinkedRef
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("parsing applied render set: %w", err)
	}
	refs := make([]*consolev1.LinkedTemplateRef, 0, len(stored))
	for _, s := range stored {
		refs = append(refs, scopeshim.NewLinkedTemplateRef(
			scopeFromLabel(s.Scope),
			s.ScopeName,
			s.Name,
			s.VersionConstraint,
		))
	}
	return refs, nil
}

// RecordAppliedRenderSet writes the resolved render set to the folder or
// organization namespace that owns the project. The record is a ConfigMap
// named `<targetKind>-<project>-<targetName>`; collisions are impossible
// within the same folder namespace because (targetKind, project, targetName)
// is unique per deployment or project-scope template. Callers invoke this
// helper from CreateDeployment, UpdateDeployment, CreateTemplate (project),
// and UpdateTemplate (project) on the success path only.
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

	payload, err := MarshalAppliedRenderSet(refs)
	if err != nil {
		return fmt.Errorf("serializing applied render set: %w", err)
	}

	kindLabel := renderTargetKindLabel(targetKind)
	cmName := renderStateConfigMapName(targetKind, project, targetName)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: folderNs,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:        v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:     v1alpha2.ResourceTypeRenderState,
				v1alpha2.LabelRenderTargetKind: kindLabel,
				v1alpha2.LabelRenderTargetName: targetName,
				v1alpha2.LabelProject:          project,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationAppliedRenderSet: string(payload),
			},
		},
	}

	slog.DebugContext(ctx, "recording applied render set",
		slog.String("folderNamespace", folderNs),
		slog.String("projectNamespace", projectNs),
		slog.String("project", project),
		slog.String("targetKind", kindLabel),
		slog.String("targetName", targetName),
		slog.String("configMap", cmName),
		slog.Int("refs", len(refs)),
	)

	// Try to create first. On AlreadyExists, fall through to Update so
	// re-applying an unchanged render set is idempotent and an edit that
	// shrinks the applied set still overwrites the stored value.
	_, createErr := c.client.CoreV1().ConfigMaps(folderNs).Create(ctx, cm, metav1.CreateOptions{})
	if createErr == nil {
		return nil
	}
	if !k8serrors.IsAlreadyExists(createErr) {
		return fmt.Errorf("creating render state ConfigMap %q in %q: %w", cmName, folderNs, createErr)
	}

	existing, getErr := c.client.CoreV1().ConfigMaps(folderNs).Get(ctx, cmName, metav1.GetOptions{})
	if getErr != nil {
		return fmt.Errorf("getting render state ConfigMap %q in %q for update: %w", cmName, folderNs, getErr)
	}
	if existing.Annotations == nil {
		existing.Annotations = make(map[string]string)
	}
	existing.Annotations[v1alpha2.AnnotationAppliedRenderSet] = string(payload)
	// Refresh labels in case prior writers used stale values (e.g. a project
	// rename). The managed-by/resource-type labels never change; the
	// target-kind/project/name labels are invariants for this CM name so
	// overwriting is safe.
	if existing.Labels == nil {
		existing.Labels = make(map[string]string)
	}
	existing.Labels[v1alpha2.LabelManagedBy] = v1alpha2.ManagedByValue
	existing.Labels[v1alpha2.LabelResourceType] = v1alpha2.ResourceTypeRenderState
	existing.Labels[v1alpha2.LabelRenderTargetKind] = kindLabel
	existing.Labels[v1alpha2.LabelRenderTargetName] = targetName
	existing.Labels[v1alpha2.LabelProject] = project
	// LabelRenderTargetProject was set by earlier writers before HOL-567
	// standardized on LabelProject; scrub it so older values cannot linger
	// on update after the canonical label is authoritative.
	delete(existing.Labels, v1alpha2.LabelRenderTargetProject)
	if _, updateErr := c.client.CoreV1().ConfigMaps(folderNs).Update(ctx, existing, metav1.UpdateOptions{}); updateErr != nil {
		return fmt.Errorf("updating render state ConfigMap %q in %q: %w", cmName, folderNs, updateErr)
	}
	return nil
}

// ReadAppliedRenderSet returns the applied render set last recorded for the
// target. Returns an empty slice (and ok=false) when no record exists; this
// lets callers treat "never applied" and "empty applied set" as distinct
// states (the latter round-trips the empty JSON array and returns ok=true).
//
// The read consults ONLY folder/organization namespace storage; any
// render-set artifact sitting in a project namespace is ignored. Operators
// migrating from a stale fixture can observe this by seeing
// GetDeploymentPolicyState report no drift even while a project-namespace
// annotation carries a different value.
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

	cmName := renderStateConfigMapName(targetKind, project, targetName)
	cm, getErr := c.client.CoreV1().ConfigMaps(folderNs).Get(ctx, cmName, metav1.GetOptions{})
	if getErr != nil {
		if k8serrors.IsNotFound(getErr) {
			return []*consolev1.LinkedTemplateRef{}, false, nil
		}
		return nil, false, fmt.Errorf("getting render state ConfigMap %q in %q: %w", cmName, folderNs, getErr)
	}
	raw := cm.Annotations[v1alpha2.AnnotationAppliedRenderSet]
	parsed, parseErr := UnmarshalAppliedRenderSet(raw)
	if parseErr != nil {
		return nil, false, fmt.Errorf("parsing applied render set on %q in %q: %w", cmName, folderNs, parseErr)
	}
	return parsed, true, nil
}

// renderTargetKindLabel maps a TargetKind to the string label value written
// to the LabelRenderTargetKind label.
func renderTargetKindLabel(kind TargetKind) string {
	switch kind {
	case TargetKindDeployment:
		return v1alpha2.RenderTargetKindDeployment
	case TargetKindProjectTemplate:
		return v1alpha2.RenderTargetKindProjectTemplate
	default:
		return ""
	}
}

// renderStateConfigMapName builds the deterministic ConfigMap name that
// stores the applied render set for a given target. The name encodes
// (kind, project, target) so multiple projects and multiple render targets
// can coexist in the same folder namespace.
//
// The name is bounded by the Kubernetes ConfigMap name limit (253 chars).
// Because project and target names are themselves DNS labels (max 63 each)
// and the kind prefix is a literal, the concatenation cannot overflow.
func renderStateConfigMapName(kind TargetKind, project, target string) string {
	return fmt.Sprintf("render-state-%s-%s-%s", renderTargetKindLabel(kind), project, target)
}

// scopeLabelValue maps a TemplateScope enum to the canonical scope label
// value. Kept package-private to avoid conflicting with templates.scopeLabelValue.
func scopeLabelValue(scope scopeshim.Scope) string {
	switch scope {
	case scopeshim.ScopeOrganization:
		return v1alpha2.TemplateScopeOrganization
	case scopeshim.ScopeFolder:
		return v1alpha2.TemplateScopeFolder
	case scopeshim.ScopeProject:
		return v1alpha2.TemplateScopeProject
	default:
		return ""
	}
}

// scopeFromLabel is the inverse of scopeLabelValue.
func scopeFromLabel(label string) scopeshim.Scope {
	switch label {
	case v1alpha2.TemplateScopeOrganization:
		return scopeshim.ScopeOrganization
	case v1alpha2.TemplateScopeFolder:
		return scopeshim.ScopeFolder
	case v1alpha2.TemplateScopeProject:
		return scopeshim.ScopeProject
	default:
		return scopeshim.ScopeUnspecified
	}
}

// DiffRenderSets classifies refs as (added, removed, drifted) given a prior
// applied set and a newly resolved set. Ordering is normalized: added holds
// refs present in current but not applied; removed holds refs present in
// applied but not current. `drifted` is true iff added or removed is
// non-empty.
//
// The comparison key is `(scope, scope_name, name, version_constraint)` —
// two refs that differ only by version constraint are treated as distinct
// because tightening a version constraint is itself drift worth surfacing.
//
// Key asymmetry with the resolver's dedup: the resolver deduplicates on the
// `(scope, scope_name, name)` triple only (see RefKey in folder_resolver.go),
// so when an explicit (owner-linked) ref and a REQUIRE rule name the same
// template with different version constraints, the explicit ref wins and the
// REQUIRE rule's constraint is dropped. Consequently, a REQUIRE-only change
// to a version constraint (same template name) will not surface as drift if
// the template is also explicitly linked on the target. This is intentional
// per TestFolderResolver_DedupRespectsExplicit — the owner's choice is
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
	scope             scopeshim.Scope
	scopeName         string
	name              string
	versionConstraint string
}

func keyForRef(r *consolev1.LinkedTemplateRef) refKey {
	return refKey{
		scope:             scopeshim.RefScope(r),
		scopeName:         scopeshim.RefScopeName(r),
		name:              r.GetName(),
		versionConstraint: r.GetVersionConstraint(),
	}
}
