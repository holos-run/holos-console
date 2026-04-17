package policyresolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// FolderNamespaceForProject resolves the Kubernetes namespace where the
// drift-state ConfigMap for a given project should live. The walker climbs
// from the project namespace; the first ancestor we hit whose resolver kind
// is either "folder" or "organization" wins.
//
// This is the single authoritative helper for "where does drift state go?"
// Handlers MUST NOT recompute a namespace themselves. Storing the applied
// render set in the project namespace would defeat the whole point of
// policy: the project owner would be able to clear the drift marker.
//
// The helper intentionally does NOT fall back to the project namespace on
// failure — if no ancestor above the project can be resolved we return an
// error so the caller can decide whether to block the write (safer) or log
// and skip drift tracking (lossier). Callers in HOL-557 log and skip so a
// transient walker outage never blocks a legitimate deployment.
//
// When the project's immediate parent is the organization namespace (no
// folders in between), that organization namespace is the drift-state
// home — the function walks the chain, it does not require a folder.
func FolderNamespaceForProject(
	ctx context.Context,
	walker AncestorWalker,
	r *resolver.Resolver,
	project string,
) (string, error) {
	if walker == nil {
		return "", fmt.Errorf("policyresolver.FolderNamespaceForProject: walker is nil")
	}
	if r == nil {
		return "", fmt.Errorf("policyresolver.FolderNamespaceForProject: resolver is nil")
	}
	projectNs := r.ProjectNamespace(project)
	ancestors, err := walker.WalkAncestors(ctx, projectNs)
	if err != nil {
		return "", fmt.Errorf("walking ancestors from %q: %w", projectNs, err)
	}
	// ancestors is ordered child→parent. Skip the project itself (ancestors[0])
	// and return the first folder or organization namespace. We prefer a
	// folder when present because it keeps drift state close to the
	// organizational unit the project sits in, which makes RBAC grants on the
	// folder namespace naturally cover the drift state too.
	for i := 1; i < len(ancestors); i++ {
		ns := ancestors[i]
		kind, _, classifyErr := r.ResourceTypeFromNamespace(ns.Name)
		if classifyErr != nil {
			slog.WarnContext(ctx, "policyresolver: skipping unclassified namespace while resolving drift-state location",
				slog.String("namespace", ns.Name),
				slog.Any("error", classifyErr),
			)
			continue
		}
		if kind == v1alpha2.ResourceTypeFolder || kind == v1alpha2.ResourceTypeOrganization {
			return ns.Name, nil
		}
	}
	return "", fmt.Errorf("no folder or organization ancestor for project %q", project)
}

// RecordAppliedRenderSet writes the effective render set for a target to a
// ConfigMap in the folder or organization namespace of the target's project.
// Called by Create/Update on both Deployment and ProjectTemplate success
// paths so a later drift check can compare the current resolver output
// against the last-applied set.
//
// The ConfigMap name is a SHA-256 hash of (project, target-kind, target-name)
// truncated to the Kubernetes DNS-label limit. The name is not human-friendly
// on purpose — the stable target identity lives in annotations so the UI can
// reverse-map a ConfigMap back to its target without fragile name parsing.
//
// Failures propagate to the caller. Upstream callers MUST treat a storage
// failure as a warning that drift detection is disabled for the target, not
// as a reason to fail the overall Create/Update RPC — the render itself
// already succeeded at that point.
func RecordAppliedRenderSet(
	ctx context.Context,
	client kubernetes.Interface,
	walker AncestorWalker,
	r *resolver.Resolver,
	project string,
	targetKind TargetKind,
	targetName string,
	refs []*consolev1.LinkedTemplateRef,
) error {
	if client == nil {
		return fmt.Errorf("policyresolver.RecordAppliedRenderSet: client is nil")
	}
	folderNs, err := FolderNamespaceForProject(ctx, walker, r, project)
	if err != nil {
		return fmt.Errorf("resolving folder namespace for project %q: %w", project, err)
	}
	cmName := appliedRenderSetConfigMapName(project, targetKind, targetName)

	payload, err := marshalRefs(refs)
	if err != nil {
		return fmt.Errorf("marshaling applied render set: %w", err)
	}

	annotations := map[string]string{
		v1alpha2.AnnotationRenderStateTarget:     targetKind.String(),
		v1alpha2.AnnotationRenderStateProject:    project,
		v1alpha2.AnnotationRenderStateTargetName: targetName,
	}
	labels := map[string]string{
		v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeRenderState,
		v1alpha2.LabelProject:      project,
	}
	data := map[string]string{
		v1alpha2.RenderStateAppliedSetKey: string(payload),
	}

	// First try to update in place so a rotation does not leak ConfigMaps.
	existing, getErr := client.CoreV1().ConfigMaps(folderNs).Get(ctx, cmName, metav1.GetOptions{})
	if getErr == nil {
		if existing.Annotations == nil {
			existing.Annotations = make(map[string]string, len(annotations))
		}
		for k, v := range annotations {
			existing.Annotations[k] = v
		}
		if existing.Labels == nil {
			existing.Labels = make(map[string]string, len(labels))
		}
		for k, v := range labels {
			existing.Labels[k] = v
		}
		existing.Data = data
		_, updateErr := client.CoreV1().ConfigMaps(folderNs).Update(ctx, existing, metav1.UpdateOptions{})
		if updateErr != nil {
			return fmt.Errorf("updating applied render-set ConfigMap %q in %q: %w", cmName, folderNs, updateErr)
		}
		return nil
	}
	if !k8serrors.IsNotFound(getErr) {
		return fmt.Errorf("looking up applied render-set ConfigMap %q in %q: %w", cmName, folderNs, getErr)
	}
	// Not found: create a fresh one.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cmName,
			Namespace:   folderNs,
			Labels:      labels,
			Annotations: annotations,
		},
		Data: data,
	}
	if _, createErr := client.CoreV1().ConfigMaps(folderNs).Create(ctx, cm, metav1.CreateOptions{}); createErr != nil {
		return fmt.Errorf("creating applied render-set ConfigMap %q in %q: %w", cmName, folderNs, createErr)
	}
	return nil
}

// ReadAppliedRenderSet returns the last-applied render set for a target. It
// consults only the folder-namespace drift store; any stale annotation left
// behind in a project namespace is intentionally ignored so a misbehaving
// project owner cannot influence drift detection.
//
// Returns (nil, nil) when no applied set has been recorded yet — callers
// treat this as "applied == empty" so a brand-new target with no
// REQUIRE-injected templates correctly reports no drift.
func ReadAppliedRenderSet(
	ctx context.Context,
	client kubernetes.Interface,
	walker AncestorWalker,
	r *resolver.Resolver,
	project string,
	targetKind TargetKind,
	targetName string,
) ([]*consolev1.LinkedTemplateRef, error) {
	if client == nil {
		return nil, fmt.Errorf("policyresolver.ReadAppliedRenderSet: client is nil")
	}
	folderNs, err := FolderNamespaceForProject(ctx, walker, r, project)
	if err != nil {
		return nil, fmt.Errorf("resolving folder namespace for project %q: %w", project, err)
	}
	cmName := appliedRenderSetConfigMapName(project, targetKind, targetName)
	cm, err := client.CoreV1().ConfigMaps(folderNs).Get(ctx, cmName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading applied render-set ConfigMap %q in %q: %w", cmName, folderNs, err)
	}
	raw, ok := cm.Data[v1alpha2.RenderStateAppliedSetKey]
	if !ok || raw == "" {
		return nil, nil
	}
	refs, err := unmarshalRefs([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("parsing applied render-set payload: %w", err)
	}
	return refs, nil
}

// appliedRenderSetConfigMapName returns the deterministic ConfigMap name for
// the (project, target-kind, target-name) tuple. A short prefix preserves
// some human affordance in `kubectl get configmaps`, and the SHA-256 suffix
// guarantees two different tuples never collide within the same folder
// namespace (a folder can hold drift state for every project nested under
// it).
//
// Names are DNS labels, so they must fit in 63 characters. We use a 16-char
// hex digest which, combined with the `render-state-` prefix (13 chars),
// leaves room for growth without risking overflow.
func appliedRenderSetConfigMapName(project string, kind TargetKind, name string) string {
	h := sha256.Sum256([]byte(project + "/" + kind.String() + "/" + name))
	return "render-state-" + hex.EncodeToString(h[:])[:16]
}

// marshalRefs serializes a LinkedTemplateRef slice to a stable JSON shape.
// The wire format intentionally matches AnnotationLinkedTemplates so external
// tooling can read both without a shape change.
func marshalRefs(refs []*consolev1.LinkedTemplateRef) ([]byte, error) {
	type storedRef struct {
		Scope             string `json:"scope"`
		ScopeName         string `json:"scope_name"`
		Name              string `json:"name"`
		VersionConstraint string `json:"version_constraint,omitempty"`
	}
	stored := make([]storedRef, 0, len(refs))
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		stored = append(stored, storedRef{
			Scope:             scopeLabelValue(ref.GetScope()),
			ScopeName:         ref.GetScopeName(),
			Name:              ref.GetName(),
			VersionConstraint: ref.GetVersionConstraint(),
		})
	}
	return json.Marshal(stored)
}

// unmarshalRefs parses the JSON wire shape emitted by marshalRefs.
func unmarshalRefs(raw []byte) ([]*consolev1.LinkedTemplateRef, error) {
	type storedRef struct {
		Scope             string `json:"scope"`
		ScopeName         string `json:"scope_name"`
		Name              string `json:"name"`
		VersionConstraint string `json:"version_constraint,omitempty"`
	}
	var stored []storedRef
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, err
	}
	refs := make([]*consolev1.LinkedTemplateRef, 0, len(stored))
	for _, s := range stored {
		refs = append(refs, &consolev1.LinkedTemplateRef{
			Scope:             scopeFromTemplateLabel(s.Scope),
			ScopeName:         s.ScopeName,
			Name:              s.Name,
			VersionConstraint: s.VersionConstraint,
		})
	}
	return refs, nil
}

// scopeLabelValue is a local helper that mirrors the value used by
// console/templates.scopeLabelValue. Duplicated here to avoid importing
// console/templates from this package (which would create a cycle once
// console/templates imports this package for render-time resolution).
func scopeLabelValue(scope consolev1.TemplateScope) string {
	switch scope {
	case consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION:
		return v1alpha2.TemplateScopeOrganization
	case consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER:
		return v1alpha2.TemplateScopeFolder
	case consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT:
		return v1alpha2.TemplateScopeProject
	default:
		return ""
	}
}
