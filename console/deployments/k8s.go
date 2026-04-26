package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// deploymentGVR is the GroupVersionResource for the holos-console
// Deployment CRD, used for namespace-scoped dynamic client lookups (UID
// retrieval and other CR operations that do not require ctrlclient typing).
var deploymentGVR = schema.GroupVersionResource{
	Group:    "deployments.holos.run",
	Version:  "v1alpha1",
	Resource: "deployments",
}

// ErrPartialScan is returned by ListDeploymentResources when at least one
// per-kind List call failed. The returned slice still contains every
// resource the successful kinds produced — callers may surface a partial
// view — but the error signals "do not treat this slice as authoritative
// drift evidence" so cache-rewrite paths can preserve their existing
// state instead of wiping it on a transient failure.
var ErrPartialScan = errors.New("list deployment resources: partial scan")

const (
	// Data keys in the ConfigMap.
	ImageKey    = "image"
	TagKey      = "tag"
	TemplateKey = "template"
	CommandKey  = "command"
	ArgsKey     = "args"
	EnvKey      = "env"
	PortKey     = "port"
)

// K8sClient wraps Kubernetes client operations for deployments.
type K8sClient struct {
	client kubernetes.Interface
	// dynamic, when non-nil, enables multi-kind queries used by the link
	// aggregator (HOL-574) to scan resources owned by a deployment across
	// every kind apply.go writes. A nil dynamic client makes
	// ListDeploymentResources a no-op so local/dev wiring without a cluster
	// dynamic client (and unit tests that only need typed reads) keep
	// working.
	dynamic  dynamic.Interface
	Resolver *resolver.Resolver
	// crWriter, when non-nil, mirrors every Create/Update/Delete call to the
	// deployments.holos.run/v1alpha1.Deployment CRD via server-side apply.
	// A nil crWriter disables dual-write so local/dev wiring without a
	// controller-runtime client remains unchanged.
	crWriter *CRWriter
}

// NewK8sClient creates a client for deployment operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

// WithDynamicClient configures the K8sClient with a dynamic client used by
// ListDeploymentResources to scan owned resources across every allowed kind
// (HOL-574). Returns the receiver for fluent chaining alongside the existing
// constructor so callers do not have to thread a new positional arg through
// every test that builds a K8sClient.
func (k *K8sClient) WithDynamicClient(d dynamic.Interface) *K8sClient {
	k.dynamic = d
	return k
}

// WithCRWriter configures the K8sClient to dual-write every create, update,
// and delete to the deployments.holos.run/v1alpha1 Deployment CRD via SSA
// in addition to the existing ConfigMap proto-store (HOL-957). A nil writer
// is safe — all CRWriter methods are nil-receiver no-ops.
func (k *K8sClient) WithCRWriter(w *CRWriter) *K8sClient {
	k.crWriter = w
	return k
}

// HasDynamicClient reports whether a dynamic client is configured. The link
// aggregator needs this to distinguish "scan returned zero resources"
// (legitimate empty drift — clear the cache) from "no dynamic client"
// (cannot scan at all — preserve cache as-is). Without the distinction the
// GetDeployment refresh path would either never clear stale entries
// (always preserve on empty) or wrongly wipe them on local/dev wiring
// where the dynamic client is intentionally nil.
func (k *K8sClient) HasDynamicClient() bool {
	return k.dynamic != nil
}

// ListDeployments returns all deployment ConfigMaps in the project namespace.
func (k *K8sClient) ListDeployments(ctx context.Context, project string) ([]corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	labelSelector := v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeDeployment
	slog.DebugContext(ctx, "listing deployments from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("labelSelector", labelSelector),
	)
	list, err := k.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	return list.Items, nil
}

// GetDeployment retrieves a deployment ConfigMap by name.
func (k *K8sClient) GetDeployment(ctx context.Context, project, name string) (*corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "getting deployment from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
}

// CreateDeployment creates a new deployment ConfigMap and dual-writes the CR.
// principal, when non-empty, identifies the OIDC-prefixed user the handler
// will bind as the creator-Owner via a per-Deployment RoleBinding (HOL-1033).
// principal is plumbed through unchanged here — the Role + RoleBinding
// provisioning lives in EnsureDeploymentRBAC, which the handler invokes after
// CreateDeployment returns the live CR (so the OwnerReference UID is known).
func (k *K8sClient) CreateDeployment(ctx context.Context, project, name, image, tag, tmpl, displayName, description string, command, args []string, env []v1alpha2.EnvVar, port int32) (*corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "creating deployment in kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeDeployment,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: displayName,
				v1alpha2.AnnotationDescription: description,
			},
		},
		Data: map[string]string{
			ImageKey:    image,
			TagKey:      tag,
			TemplateKey: tmpl,
		},
	}
	if len(command) > 0 {
		b, _ := json.Marshal(command)
		cm.Data[CommandKey] = string(b)
	}
	if len(args) > 0 {
		b, _ := json.Marshal(args)
		cm.Data[ArgsKey] = string(b)
	}
	if len(env) > 0 {
		b, _ := json.Marshal(env)
		cm.Data[EnvKey] = string(b)
	}
	if port > 0 {
		cm.Data[PortKey] = strconv.Itoa(int(port))
	}
	created, err := k.client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	// Dual-write: mirror the new Deployment to the CRD store. A nil crWriter
	// (local/dev wiring without a controller-runtime client) is a no-op. The
	// returned CR is intentionally discarded here — the handler invokes
	// EnsureDeploymentRBAC separately, which fetches the CR (with UID) via
	// the dynamic client to stamp ownerReferences on per-Deployment Roles
	// and RoleBindings.
	if _, crErr := k.crWriter.ApplyOnCreate(ctx, project, name, image, tag, tmpl, displayName, description, command, args, env, port); crErr != nil {
		slog.WarnContext(ctx, "deployment CR dual-write failed after proto-store create",
			slog.String("project", project),
			slog.String("namespace", ns),
			slog.String("name", name),
			slog.Any("error", crErr),
		)
		// Do not surface the error to the caller: the ConfigMap write already
		// succeeded. The CR will be lazily re-created on the next update.
	}
	return created, nil
}

// EnsureDeploymentRBAC provisions the three per-Deployment Roles (viewer,
// editor, owner) and a creator-Owner RoleBinding for the given principal.
// All Roles and RoleBindings carry an OwnerReference to the Deployment CR
// so K8s garbage collection cascades the cleanup when the Deployment is
// deleted (HOL-1033 AC #3). The Deployment CR's UID is fetched via the
// dynamic client so the function works under both real and impersonated
// clients without requiring a typed scheme.
//
// principal is the OIDC subject (with or without the "oidc:" prefix) that
// will receive the Owner RoleBinding. An empty principal skips the
// RoleBinding write — the Roles still land so subsequent UpdateSharing
// calls have something to bind. role overrides the default Owner tier
// (callers that grant a different starting tier — e.g. Viewer for a
// service-account creator — pass it explicitly).
//
// EnsureDeploymentRBAC is idempotent: re-running for the same deployment
// updates each Role's labels/rules in place and reapplies the creator's
// RoleBinding so policy churn is observable without a delete-recreate.
func (k *K8sClient) EnsureDeploymentRBAC(ctx context.Context, project, name, principal, role string) error {
	if k.dynamic == nil {
		// Without a dynamic client we cannot fetch the CR UID, so we cannot
		// stamp ownerReferences. Skip provisioning so local/dev wiring
		// without a cluster degrades gracefully — the proto-store flow
		// continues to work.
		slog.DebugContext(ctx, "skipping per-deployment RBAC provisioning: no dynamic client",
			slog.String("project", project),
			slog.String("name", name),
		)
		return nil
	}
	ns := k.Resolver.ProjectNamespace(project)
	ownerRefs, err := k.deploymentOwnerRefs(ctx, ns, name)
	if err != nil {
		// If the CR has not been created yet (dual-write disabled, fake
		// dynamic client without registered GVR, or any other CR-not-found
		// signal), degrade gracefully: per-deployment RBAC will be
		// reconciled the next time the deployment is updated. This matches
		// the lazy-creation guarantee CRWriter relies on.
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			slog.WarnContext(ctx, "skipping per-deployment RBAC provisioning: deployment CR absent",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", err),
			)
			return nil
		}
		return fmt.Errorf("resolving deployment ownerReferences: %w", err)
	}
	for _, r := range DeploymentRoles(ns, name, ownerRefs) {
		if err := k.applyRole(ctx, r); err != nil {
			return fmt.Errorf("applying deployment role %q: %w", r.Name, err)
		}
	}
	if principal == "" {
		return nil
	}
	binding := RoleBinding(ns, name, ShareTargetUser, principal, role, ownerRefs)
	if err := k.applyRoleBinding(ctx, binding); err != nil {
		return fmt.Errorf("applying creator role binding: %w", err)
	}
	return nil
}

// ReconcileDeploymentRoleBindings reconciles the user/group sharing
// RoleBindings for the named deployment against the desired set. Existing
// per-deployment RoleBindings not present in the desired set are deleted;
// missing ones are created; mismatched RoleRefs are recreated (RoleRef is
// immutable). Mirrors secrets.reconcileProjectSecretRoleBindings in shape.
func (k *K8sClient) ReconcileDeploymentRoleBindings(ctx context.Context, project, name string, shareUsers, shareRoles []DeploymentGrant) error {
	ns := k.Resolver.ProjectNamespace(project)
	ownerRefs, err := k.deploymentOwnerRefs(ctx, ns, name)
	if err != nil {
		// CR absent → no ownerReferences possible. The reconcile still
		// succeeds: any existing per-deployment RoleBindings get pruned
		// against the desired set, and new bindings are created without
		// ownerReferences. They will be retro-stamped on the next
		// update once the CR materialises.
		if !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
			return fmt.Errorf("resolving deployment ownerReferences: %w", err)
		}
		ownerRefs = nil
	}
	desired := make(map[string]*rbacv1.RoleBinding)
	for _, g := range deduplicateDeploymentGrants(shareUsers) {
		if g.Principal == "" {
			continue
		}
		rb := RoleBinding(ns, name, ShareTargetUser, g.Principal, g.Role, ownerRefs)
		desired[rb.Name] = rb
	}
	for _, g := range deduplicateDeploymentGrants(shareRoles) {
		if g.Principal == "" {
			continue
		}
		rb := RoleBinding(ns, name, ShareTargetGroup, g.Principal, g.Role, ownerRefs)
		desired[rb.Name] = rb
	}

	selector := labels.SelectorFromSet(labels.Set{
		v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
		LabelRolePurpose:        RolePurposeDeployment,
		LabelDeploymentName:     name,
	})
	current, err := k.client.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}
	for _, existing := range current.Items {
		if _, ok := desired[existing.Name]; ok {
			continue
		}
		if err := k.client.RbacV1().RoleBindings(ns).Delete(ctx, existing.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	for _, rb := range desired {
		if err := k.applyRoleBinding(ctx, rb); err != nil {
			return err
		}
	}
	return nil
}

// ListDeploymentSharing returns the user and group RoleBindings currently
// bound to the named deployment, decoded back into the wire-stable
// DeploymentGrant shape.
func (k *K8sClient) ListDeploymentSharing(ctx context.Context, project, name string) ([]DeploymentGrant, []DeploymentGrant, error) {
	ns := k.Resolver.ProjectNamespace(project)
	selector := labels.SelectorFromSet(labels.Set{
		v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
		LabelRolePurpose:        RolePurposeDeployment,
		LabelDeploymentName:     name,
	})
	list, err := k.client.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, nil, err
	}
	users := roleBindingsToGrants(list.Items, rbacv1.UserKind)
	groups := roleBindingsToGrants(list.Items, rbacv1.GroupKind)
	return users, groups, nil
}

// deploymentOwnerRefs returns an ownerReferences slice pointing at the
// Deployment CR with the given name in the project namespace. Used by
// EnsureDeploymentRBAC and ReconcileDeploymentRoleBindings to stamp
// ownerReferences on per-Deployment Roles and RoleBindings so K8s GC
// cascades cleanup.
func (k *K8sClient) deploymentOwnerRefs(ctx context.Context, namespace, name string) ([]metav1.OwnerReference, error) {
	if k.dynamic == nil {
		return nil, fmt.Errorf("dynamic client required to resolve deployment UID")
	}
	cr, err := k.dynamic.Resource(deploymentGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	controller := true
	blockOwnerDeletion := true
	return []metav1.OwnerReference{{
		APIVersion:         "deployments.holos.run/v1alpha1",
		Kind:               "Deployment",
		Name:               cr.GetName(),
		UID:                cr.GetUID(),
		Controller:         &controller,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}}, nil
}

// applyRole creates or updates a per-Deployment Role. Idempotent: an
// already-existing Role has its labels, rules, and ownerReferences
// reconciled against the desired state via Update.
func (k *K8sClient) applyRole(ctx context.Context, role *rbacv1.Role) error {
	created, err := k.client.RbacV1().Roles(role.Namespace).Create(ctx, role, metav1.CreateOptions{})
	if err == nil {
		*role = *created
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	existing, err := k.client.RbacV1().Roles(role.Namespace).Get(ctx, role.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	existing.Labels = role.Labels
	existing.Rules = role.Rules
	existing.OwnerReferences = role.OwnerReferences
	updated, err := k.client.RbacV1().Roles(role.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*role = *updated
	return nil
}

// applyRoleBinding creates or updates a per-Deployment RoleBinding. RoleRef
// is immutable in K8s, so when an existing RoleBinding's RoleRef differs
// from the desired one (e.g. role tier changed) the old binding is deleted
// and recreated.
func (k *K8sClient) applyRoleBinding(ctx context.Context, binding *rbacv1.RoleBinding) error {
	created, err := k.client.RbacV1().RoleBindings(binding.Namespace).Create(ctx, binding, metav1.CreateOptions{})
	if err == nil {
		*binding = *created
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	existing, err := k.client.RbacV1().RoleBindings(binding.Namespace).Get(ctx, binding.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if existing.RoleRef != binding.RoleRef {
		if err := k.client.RbacV1().RoleBindings(binding.Namespace).Delete(ctx, binding.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		recreated, err := k.client.RbacV1().RoleBindings(binding.Namespace).Create(ctx, binding, metav1.CreateOptions{})
		if err == nil {
			*binding = *recreated
		}
		return err
	}
	existing.Labels = binding.Labels
	existing.Annotations = binding.Annotations
	existing.Subjects = binding.Subjects
	existing.OwnerReferences = binding.OwnerReferences
	updated, err := k.client.RbacV1().RoleBindings(binding.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*binding = *updated
	return nil
}

// DeploymentGrant mirrors the secrets sharing grant shape so the
// deployments handler can reuse one wire format across the two RBAC
// migrations. Fields match secrets.AnnotationGrant intentionally (HOL-1032
// precedent) so a future shared package can subsume both.
type DeploymentGrant struct {
	Principal string
	Role      string
}

// deploymentGrantRoleRank lets deduplicateDeploymentGrants pick the
// highest-privilege grant when the same principal appears more than once.
var deploymentGrantRoleRank = map[string]int{
	RoleViewer: 1,
	RoleEditor: 2,
	RoleOwner:  3,
}

// deduplicateDeploymentGrants merges duplicate principals, keeping the
// grant with the highest role. Empty-principal entries are dropped. The
// original insertion order of first-seen principals is preserved so the
// resulting list is stable across reconcile passes.
func deduplicateDeploymentGrants(grants []DeploymentGrant) []DeploymentGrant {
	seen := make(map[string]int)
	out := make([]DeploymentGrant, 0, len(grants))
	for _, g := range grants {
		if g.Principal == "" {
			continue
		}
		if idx, ok := seen[g.Principal]; ok {
			if deploymentGrantRoleRank[NormalizeRole(g.Role)] > deploymentGrantRoleRank[NormalizeRole(out[idx].Role)] {
				out[idx] = g
			}
			continue
		}
		seen[g.Principal] = len(out)
		out = append(out, g)
	}
	return out
}

// roleBindingsToGrants converts per-Deployment RoleBindings back into the
// stable DeploymentGrant wire shape. kind selects user or group subjects
// (rbacv1.UserKind / rbacv1.GroupKind).
func roleBindingsToGrants(bindings []rbacv1.RoleBinding, kind string) []DeploymentGrant {
	var grants []DeploymentGrant
	for _, rb := range bindings {
		role := RoleFromLabels(rb.Labels)
		for _, subject := range rb.Subjects {
			if subject.Kind != kind {
				continue
			}
			grants = append(grants, DeploymentGrant{
				Principal: UnprefixedPrincipal(subject.Name),
				Role:      role,
			})
		}
	}
	return deduplicateDeploymentGrants(grants)
}

// patchTypeApply is a constant alias to keep types.ApplyPatchType visible
// in this file even though only a few helpers reference patch types.
var _ = types.ApplyPatchType

// UpdateDeployment updates an existing deployment ConfigMap.
// Only non-nil scalar fields are updated. Non-empty command/args slices replace stored values.
// A non-nil env slice (even if empty) replaces the stored env vars.
// A non-nil port pointer updates the stored port value.
func (k *K8sClient) UpdateDeployment(ctx context.Context, project, name string, image, tag, displayName, description *string, command, args []string, env []v1alpha2.EnvVar, port *int32) (*corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "updating deployment in kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting deployment for update: %w", err)
	}
	if cm.Annotations == nil {
		cm.Annotations = make(map[string]string)
	}
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	if image != nil {
		cm.Data[ImageKey] = *image
	}
	if tag != nil {
		cm.Data[TagKey] = *tag
	}
	if displayName != nil {
		cm.Annotations[v1alpha2.AnnotationDisplayName] = *displayName
	}
	if description != nil {
		cm.Annotations[v1alpha2.AnnotationDescription] = *description
	}
	if len(command) > 0 {
		b, _ := json.Marshal(command)
		cm.Data[CommandKey] = string(b)
	}
	if len(args) > 0 {
		b, _ := json.Marshal(args)
		cm.Data[ArgsKey] = string(b)
	}
	if env != nil {
		b, _ := json.Marshal(env)
		cm.Data[EnvKey] = string(b)
	}
	if port != nil {
		if *port > 0 {
			cm.Data[PortKey] = strconv.Itoa(int(*port))
		} else {
			delete(cm.Data, PortKey)
		}
	}
	updated, err := k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	// Dual-write: mirror the updated Deployment to the CRD store. Extract
	// the canonical post-update values from the updated ConfigMap so the CR
	// always reflects what the proto-store actually recorded.
	if k.crWriter != nil {
		updatedImage := updated.Data[ImageKey]
		updatedTag := updated.Data[TagKey]
		updatedTemplate := updated.Data[TemplateKey]
		updatedDisplayName := updated.Annotations[v1alpha2.AnnotationDisplayName]
		updatedDescription := updated.Annotations[v1alpha2.AnnotationDescription]
		updatedCommand := commandFromConfigMapData(updated.Data)
		updatedArgs := argsFromConfigMapData(updated.Data)
		updatedPort := portFromConfigMapData(updated.Data)
		if crErr := k.crWriter.ApplyOnUpdate(ctx, project, name, updatedImage, updatedTag, updatedTemplate, updatedDisplayName, updatedDescription, updatedCommand, updatedArgs, updatedPort); crErr != nil {
			slog.WarnContext(ctx, "deployment CR dual-write failed after proto-store update",
				slog.String("project", project),
				slog.String("namespace", ns),
				slog.String("name", name),
				slog.Any("error", crErr),
			)
			// Do not surface the error: the ConfigMap update succeeded.
		}
	}
	return updated, nil
}

// commandFromConfigMapData returns the decoded command slice from cm data,
// or nil when the key is absent or the JSON is malformed.
func commandFromConfigMapData(data map[string]string) []string {
	raw, ok := data[CommandKey]
	if !ok || raw == "" {
		return nil
	}
	var v []string
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil
	}
	return v
}

// argsFromConfigMapData returns the decoded args slice from cm data, or nil
// when the key is absent or the JSON is malformed.
func argsFromConfigMapData(data map[string]string) []string {
	raw, ok := data[ArgsKey]
	if !ok || raw == "" {
		return nil
	}
	var v []string
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil
	}
	return v
}

// portFromConfigMapData returns the port from cm data, or 0 when the key is
// absent or not parseable as an integer.
func portFromConfigMapData(data map[string]string) int32 {
	raw, ok := data[PortKey]
	if !ok || raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return int32(v)
}

// ListDeploymentResources returns every resource currently owned by the given
// deployment within the project namespace, scanned across every kind
// apply.go writes. The lookup uses the same
// `LabelProject=<project>,console.holos.run/deployment=<deployment>`
// selector applied at apply time so results are exactly the in-namespace
// subset Reconcile and Cleanup operate on. Returned objects are the live
// cluster representation — each carries its own annotations, labels,
// kind, namespace, and name — and are intended to be passed straight to
// links.ParseAnnotations by the aggregator (HOL-574).
//
// Scope: namespace-scoped (the project namespace returned by Resolver).
// This deliberately matches the existing console RBAC posture, which is
// namespaced — a cluster-wide list would silently fail in any cluster
// where the console service account does not have cluster-level list
// permissions, dropping links without a visible error and leaving the
// "RBAC unchanged" guarantee unmet. Cross-namespace owned resources
// (e.g. an HTTPRoute landing in istio-ingress) are intentionally not
// scanned here; templates that want to surface links from cross-
// namespace resources should attach `external-link.*` / `primary-url`
// annotations to a project-namespace resource instead.
//
// Determinism: kinds are iterated in lexicographic GVR order so the
// resource slice — and therefore the first-wins selection in the
// aggregator (de-duplication and `primary-url` promotion) — is stable
// across requests. Iterating the `allowedKinds` map directly would
// scramble the order on every call and cause cached values to flap
// even when the live cluster did not change (HOL-574 review round 2 P2).
//
// Partial-failure handling: if any per-kind list fails (transient API
// error, missing optional CRD, RBAC gap on a single resource type) the
// successful items are still returned but the error wraps `ErrPartialScan`
// so callers can preserve their cached state instead of treating an
// incomplete view as authoritative drift (HOL-574 review round 2 P1).
//
// When no dynamic client is configured the method returns (nil, nil) so
// the handler degrades gracefully on local/dev wiring without a cluster.
func (k *K8sClient) ListDeploymentResources(ctx context.Context, project, deployment string) ([]unstructured.Unstructured, error) {
	if k.dynamic == nil {
		return nil, nil
	}
	if project == "" || deployment == "" {
		return nil, fmt.Errorf("project and deployment are required")
	}
	ns := k.Resolver.ProjectNamespace(project)
	labelSelector := fmt.Sprintf("%s=%s,%s=%s",
		v1alpha2.LabelProject, project,
		v1alpha2.AnnotationDeployment, deployment)

	// Walk allowedKinds in a deterministic order so the aggregator's
	// first-wins de-duplication is stable across calls.
	kinds := make([]string, 0, len(allowedKinds))
	for kind := range allowedKinds {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	var out []unstructured.Unstructured
	var listErrors []error
	for _, kind := range kinds {
		gvr := allowedKinds[kind]
		list, err := k.dynamic.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			// Optional CRDs (HTTPRoute, ReferenceGrant, etc.) may
			// not be installed on every cluster: the API server
			// returns NotFound or NoKindMatchError for those GVRs.
			// Treat that as a successful empty result rather than a
			// partial-scan signal, otherwise the cache would never
			// be seeded in clusters without Gateway API installed
			// (HOL-574 review round 4). Other errors (transient
			// connectivity, RBAC) still downgrade authority via
			// ErrPartialScan so the cache is preserved.
			if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
				slog.DebugContext(ctx, "list deployment resources: optional kind absent, skipping",
					slog.String("kind", kind),
					slog.String("namespace", ns),
					slog.String("project", project),
					slog.String("deployment", deployment),
				)
				continue
			}
			slog.DebugContext(ctx, "list deployment resources: skipping kind",
				slog.String("kind", kind),
				slog.String("namespace", ns),
				slog.String("project", project),
				slog.String("deployment", deployment),
				slog.Any("error", err),
			)
			listErrors = append(listErrors, fmt.Errorf("listing %s: %w", kind, err))
			continue
		}
		out = append(out, list.Items...)
	}
	if len(listErrors) > 0 {
		return out, fmt.Errorf("%w (%d/%d kinds failed): %w",
			ErrPartialScan, len(listErrors), len(allowedKinds), listErrors[0])
	}
	return out, nil
}

// SetAggregatedLinksAnnotation sets (or clears) the aggregated-links cache
// annotation on the deployment ConfigMap. An empty payload removes the
// annotation so stale link sets from previous renders do not persist when a
// template edit drops every link source. A missing ConfigMap surfaces the
// underlying NotFound so the handler can decide whether to log or surface
// the error. Mirrors SetOutputURLAnnotation exactly so the two cached
// surfaces share one operational shape (HOL-574).
func (k *K8sClient) SetAggregatedLinksAnnotation(ctx context.Context, project, name, payload string) error {
	ns := k.Resolver.ProjectNamespace(project)
	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting deployment for aggregated-links annotation update: %w", err)
	}
	if cm.Annotations == nil {
		cm.Annotations = map[string]string{}
	}
	if payload == "" {
		if _, ok := cm.Annotations[v1alpha2.AnnotationAggregatedLinks]; !ok {
			// No-op: annotation not present and nothing to clear.
			return nil
		}
		delete(cm.Annotations, v1alpha2.AnnotationAggregatedLinks)
	} else {
		if cm.Annotations[v1alpha2.AnnotationAggregatedLinks] == payload {
			// Already up to date; avoid a needless write.
			return nil
		}
		cm.Annotations[v1alpha2.AnnotationAggregatedLinks] = payload
	}
	_, err = k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating deployment aggregated-links annotation: %w", err)
	}
	return nil
}

// SetOutputURLAnnotation sets (or clears) the output-url annotation on the
// deployment ConfigMap. An empty url removes the annotation so stale links
// from previous renders do not persist when a template edit drops the
// output block. A missing ConfigMap is returned as-is so the handler can
// decide whether to log or surface the error.
func (k *K8sClient) SetOutputURLAnnotation(ctx context.Context, project, name, url string) error {
	ns := k.Resolver.ProjectNamespace(project)
	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting deployment for annotation update: %w", err)
	}
	if cm.Annotations == nil {
		cm.Annotations = map[string]string{}
	}
	if url == "" {
		if _, ok := cm.Annotations[OutputURLAnnotation]; !ok {
			// No-op: annotation not present and nothing to clear.
			return nil
		}
		delete(cm.Annotations, OutputURLAnnotation)
	} else {
		if cm.Annotations[OutputURLAnnotation] == url {
			// Already up to date; avoid a needless write.
			return nil
		}
		cm.Annotations[OutputURLAnnotation] = url
	}
	_, err = k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating deployment output-url annotation: %w", err)
	}
	return nil
}

// DeleteDeployment deletes a deployment ConfigMap and the corresponding CR.
func (k *K8sClient) DeleteDeployment(ctx context.Context, project, name string) error {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "deleting deployment from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	if err := k.client.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return err
	}
	// Dual-write: delete the Deployment CR. NotFound is silenced (idempotent).
	if crErr := k.crWriter.DeleteCR(ctx, project, name); crErr != nil {
		slog.WarnContext(ctx, "deployment CR delete failed after proto-store delete",
			slog.String("project", project),
			slog.String("namespace", ns),
			slog.String("name", name),
			slog.Any("error", crErr),
		)
		// Do not surface the error: the ConfigMap delete succeeded.
	}
	return nil
}

// NamespaceResourceItem holds a resource name and its sorted data keys.
type NamespaceResourceItem struct {
	Name string
	Keys []string
}

// ListNamespaceSecrets lists all Secrets in the project namespace, excluding
// service-account-token type secrets which are not user data.
func (k *K8sClient) ListNamespaceSecrets(ctx context.Context, project string) ([]NamespaceResourceItem, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "listing secrets from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
	)
	list, err := k.client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing secrets: %w", err)
	}
	result := make([]NamespaceResourceItem, 0, len(list.Items))
	for _, s := range list.Items {
		if s.Type == corev1.SecretTypeServiceAccountToken {
			continue
		}
		keys := make([]string, 0, len(s.Data))
		for k := range s.Data {
			keys = append(keys, k)
		}
		result = append(result, NamespaceResourceItem{Name: s.Name, Keys: keys})
	}
	return result, nil
}

// ListNamespaceConfigMaps lists all ConfigMaps in the project namespace,
// excluding console-managed ones (those with the console.holos.run/resource-type label).
func (k *K8sClient) ListNamespaceConfigMaps(ctx context.Context, project string) ([]NamespaceResourceItem, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "listing configmaps from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
	)
	list, err := k.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing configmaps: %w", err)
	}
	result := make([]NamespaceResourceItem, 0, len(list.Items))
	for _, cm := range list.Items {
		if _, isConsoleManagedResource := cm.Labels[v1alpha2.LabelResourceType]; isConsoleManagedResource {
			continue
		}
		keys := make([]string, 0, len(cm.Data))
		for k := range cm.Data {
			keys = append(keys, k)
		}
		result = append(result, NamespaceResourceItem{Name: cm.Name, Keys: keys})
	}
	return result, nil
}
