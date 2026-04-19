package templatepolicybindings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// K8sClient wraps Kubernetes client operations for TemplatePolicyBinding
// ConfigMap CRUD. TemplatePolicyBinding objects live only in organization or
// folder namespaces; any attempt to read, write, or delete a binding in a
// project namespace is rejected before the request reaches the Kubernetes
// API (HOL-554, ADR 029). The guardrail mirrors
// console/templatepolicies/k8s.go so platform-owned policy and the bindings
// that activate it share the same isolation story.
type K8sClient struct {
	client   kubernetes.Interface
	Resolver *resolver.Resolver
}

// NewK8sClient creates a K8sClient for TemplatePolicyBinding operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

// ProjectNamespaceError is returned whenever a caller attempts to read or
// write a TemplatePolicyBinding against a namespace the resolver classifies
// as a project namespace. The offending namespace is exposed on the
// Namespace field so the handler can surface it in its InvalidArgument
// response without re-deriving it from string parsing.
type ProjectNamespaceError struct {
	Namespace string
}

func (e *ProjectNamespaceError) Error() string {
	return fmt.Sprintf("template policy bindings cannot be stored in project namespace %q; use the owning folder or organization namespace", e.Namespace)
}

// namespaceForScope translates a TemplateScope into a Kubernetes namespace
// name. This method never returns a project namespace — project scope is
// rejected with InvalidArgument-equivalent semantics via
// ProjectNamespaceError.
//
// All CRUD methods on K8sClient funnel through this helper so the
// folder-only-storage invariant lives in exactly one place. The handler
// must not reach past this function into per-scope namespace derivation;
// doing so would bypass the classification check.
func (k *K8sClient) namespaceForScope(scope scopeshim.Scope, scopeName string) (string, error) {
	var ns string
	switch scope {
	case scopeshim.ScopeOrganization:
		ns = k.Resolver.OrgNamespace(scopeName)
	case scopeshim.ScopeFolder:
		ns = k.Resolver.FolderNamespace(scopeName)
	case scopeshim.ScopeProject:
		// Project scope never produces a valid namespace for binding
		// storage. We intentionally do NOT call the resolver's
		// project-namespace helper here — the regression test in
		// k8s_test.go asserts this package never references that
		// helper. The namespace name we need for the error message is
		// derived from the raw prefixes directly.
		projectNs := k.Resolver.NamespacePrefix + k.Resolver.ProjectPrefix + scopeName
		return "", &ProjectNamespaceError{Namespace: projectNs}
	default:
		return "", fmt.Errorf("unknown template scope %v", scope)
	}

	// Defense in depth: the resolver may classify a non-default prefix
	// configuration as project even when the caller asked for
	// org/folder. If that ever happens, reject rather than silently
	// storing in a project namespace.
	kind, _, err := k.Resolver.ResourceTypeFromNamespace(ns)
	if err != nil {
		// Prefix mismatch means the namespace is not managed by any
		// known resource type. Let the caller decide; K8s will return
		// the appropriate error on the subsequent request.
		return ns, nil
	}
	if kind == v1alpha2.ResourceTypeProject {
		return "", &ProjectNamespaceError{Namespace: ns}
	}
	return ns, nil
}

// ListBindings returns every TemplatePolicyBinding ConfigMap in the scope's
// namespace.
func (k *K8sClient) ListBindings(ctx context.Context, scope scopeshim.Scope, scopeName string) ([]corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	return k.listBindingsInNamespace(ctx, ns)
}

// ListBindingsInNamespace lists TemplatePolicyBinding ConfigMaps in the given
// namespace without routing through scope resolution. The caller is
// responsible for ensuring the namespace is NOT a project namespace — this
// method deliberately does NOT re-check the resource type, because it is
// invoked from ancestor walkers that already skipped project-kind
// namespaces. Re-validating here would duplicate the guard and make the
// behavior slower than necessary.
func (k *K8sClient) ListBindingsInNamespace(ctx context.Context, ns string) ([]corev1.ConfigMap, error) {
	if ns == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	return k.listBindingsInNamespace(ctx, ns)
}

func (k *K8sClient) listBindingsInNamespace(ctx context.Context, ns string) ([]corev1.ConfigMap, error) {
	labelSelector := v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeTemplatePolicyBinding
	slog.DebugContext(ctx, "listing template policy bindings from kubernetes",
		slog.String("namespace", ns),
		slog.String("labelSelector", labelSelector),
	)
	list, err := k.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing template policy bindings in %q: %w", ns, err)
	}
	return list.Items, nil
}

// GetBinding retrieves a single TemplatePolicyBinding ConfigMap by name.
func (k *K8sClient) GetBinding(ctx context.Context, scope scopeshim.Scope, scopeName, name string) (*corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "getting template policy binding from kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
}

// CreateBinding creates a new TemplatePolicyBinding ConfigMap. The policy
// reference and target list are serialized as JSON annotations — the
// ConfigMap has no data payload of its own, mirroring the annotation-only
// layout used by TemplatePolicy ConfigMaps.
func (k *K8sClient) CreateBinding(ctx context.Context, scope scopeshim.Scope, scopeName, name, displayName, description, creatorEmail string, policyRef *consolev1.LinkedTemplatePolicyRef, targetRefs []*consolev1.TemplatePolicyBindingTargetRef) (*corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}

	policyJSON, err := marshalPolicyRef(policyRef)
	if err != nil {
		return nil, err
	}
	targetsJSON, err := marshalTargetRefs(targetRefs)
	if err != nil {
		return nil, err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplatePolicyBinding,
				v1alpha2.LabelTemplateScope: scopeLabelValue(scope),
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:                     displayName,
				v1alpha2.AnnotationDescription:                     description,
				v1alpha2.AnnotationCreatorEmail:                    creatorEmail,
				v1alpha2.AnnotationTemplatePolicyBindingPolicyRef:  string(policyJSON),
				v1alpha2.AnnotationTemplatePolicyBindingTargetRefs: string(targetsJSON),
			},
		},
	}
	slog.DebugContext(ctx, "creating template policy binding in kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
}

// UpdateBinding updates an existing TemplatePolicyBinding ConfigMap in
// place. Only pointer fields that are non-nil are applied; callers can
// partial-update display/description by passing nil for fields they want
// preserved. Policy-ref and target-refs updates are gated on explicit
// booleans so an empty target list can intentionally replace the existing
// one.
func (k *K8sClient) UpdateBinding(ctx context.Context, scope scopeshim.Scope, scopeName, name string, displayName, description *string, policyRef *consolev1.LinkedTemplatePolicyRef, updatePolicyRef bool, targetRefs []*consolev1.TemplatePolicyBindingTargetRef, updateTargetRefs bool) (*corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting template policy binding for update: %w", err)
	}
	if cm.Annotations == nil {
		cm.Annotations = make(map[string]string)
	}
	if displayName != nil {
		cm.Annotations[v1alpha2.AnnotationDisplayName] = *displayName
	}
	if description != nil {
		cm.Annotations[v1alpha2.AnnotationDescription] = *description
	}
	if updatePolicyRef {
		policyJSON, err := marshalPolicyRef(policyRef)
		if err != nil {
			return nil, err
		}
		cm.Annotations[v1alpha2.AnnotationTemplatePolicyBindingPolicyRef] = string(policyJSON)
	}
	if updateTargetRefs {
		targetsJSON, err := marshalTargetRefs(targetRefs)
		if err != nil {
			return nil, err
		}
		cm.Annotations[v1alpha2.AnnotationTemplatePolicyBindingTargetRefs] = string(targetsJSON)
	}
	slog.DebugContext(ctx, "updating template policy binding in kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
}

// DeleteBinding deletes a TemplatePolicyBinding ConfigMap. Not-found errors
// propagate from the Kubernetes client so the handler can map them to
// connect.CodeNotFound.
func (k *K8sClient) DeleteBinding(ctx context.Context, scope scopeshim.Scope, scopeName, name string) error {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return err
	}
	slog.DebugContext(ctx, "deleting template policy binding from kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{})
}

// UnmarshalPolicyRef exposes the internal policy-ref parser to other
// packages (notably console/policyresolver) so downstream evaluators can
// decode stored bindings without re-implementing the JSON wire shape.
// Returns nil for empty input so callers can treat "no stored ref" as a
// validation failure rather than a parse failure.
func UnmarshalPolicyRef(raw string) (*consolev1.LinkedTemplatePolicyRef, error) {
	return unmarshalPolicyRef(raw)
}

// UnmarshalTargetRefs exposes the internal target-refs parser to other
// packages. Returns an empty slice for the empty-string input so callers
// can iterate without a nil check.
func UnmarshalTargetRefs(raw string) ([]*consolev1.TemplatePolicyBindingTargetRef, error) {
	return unmarshalTargetRefs(raw)
}

// scopeLabelValue returns the label string for a TemplateScope. Only
// organization and folder values are reachable; project is rejected
// upstream, so fall through to empty string — which would make any
// ConfigMap unusable and therefore catch any bug that routed a project
// scope through this function.
func scopeLabelValue(scope scopeshim.Scope) string {
	switch scope {
	case scopeshim.ScopeOrganization:
		return v1alpha2.TemplateScopeOrganization
	case scopeshim.ScopeFolder:
		return v1alpha2.TemplateScopeFolder
	default:
		return ""
	}
}

// storedPolicyRef is the JSON wire shape for a
// AnnotationTemplatePolicyBindingPolicyRef annotation. The nested struct
// carries its own JSON representation so a hand-authored ConfigMap without
// the latest generated types still round-trips.
type storedPolicyRef struct {
	Scope     string `json:"scope"`
	ScopeName string `json:"scopeName"`
	Name      string `json:"name"`
}

// storedTargetRef is the JSON wire shape for one entry in the
// AnnotationTemplatePolicyBindingTargetRefs annotation.
type storedTargetRef struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	ProjectName string `json:"projectName"`
}

func marshalPolicyRef(ref *consolev1.LinkedTemplatePolicyRef) ([]byte, error) {
	sr := storedPolicyRef{}
	if ref != nil {
		// HOL-619 removed the scope/scope_name discriminator in favor of the
		// owning Kubernetes namespace. Classify the namespace back into
		// (scope, scopeName) so the existing stored annotation shape is
		// preserved while the storage layer is still scope-keyed (HOL-621
		// rewrites this on-disk format).
		sr.Scope = templateScopeLabel(scopeshim.PolicyRefScope(ref))
		sr.ScopeName = scopeshim.PolicyRefScopeName(ref)
		sr.Name = ref.GetName()
	}
	b, err := json.Marshal(sr)
	if err != nil {
		return nil, fmt.Errorf("serializing template policy binding policy ref: %w", err)
	}
	return b, nil
}

func unmarshalPolicyRef(raw string) (*consolev1.LinkedTemplatePolicyRef, error) {
	if raw == "" {
		return nil, nil
	}
	var sr storedPolicyRef
	if err := json.Unmarshal([]byte(raw), &sr); err != nil {
		return nil, fmt.Errorf("parsing template policy binding policy ref: %w", err)
	}
	return scopeshim.NewLinkedTemplatePolicyRef(
		scopeFromTemplateLabel(sr.Scope),
		sr.ScopeName,
		sr.Name,
	), nil
}

func marshalTargetRefs(refs []*consolev1.TemplatePolicyBindingTargetRef) ([]byte, error) {
	stored := make([]storedTargetRef, 0, len(refs))
	for _, r := range refs {
		if r == nil {
			continue
		}
		stored = append(stored, storedTargetRef{
			Kind:        targetKindToString(r.GetKind()),
			Name:        r.GetName(),
			ProjectName: r.GetProjectName(),
		})
	}
	b, err := json.Marshal(stored)
	if err != nil {
		return nil, fmt.Errorf("serializing template policy binding target refs: %w", err)
	}
	return b, nil
}

func unmarshalTargetRefs(raw string) ([]*consolev1.TemplatePolicyBindingTargetRef, error) {
	if raw == "" {
		return nil, nil
	}
	var stored []storedTargetRef
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("parsing template policy binding target refs: %w", err)
	}
	refs := make([]*consolev1.TemplatePolicyBindingTargetRef, 0, len(stored))
	for _, s := range stored {
		refs = append(refs, &consolev1.TemplatePolicyBindingTargetRef{
			Kind:        targetKindFromString(s.Kind),
			Name:        s.Name,
			ProjectName: s.ProjectName,
		})
	}
	return refs, nil
}

func targetKindToString(k consolev1.TemplatePolicyBindingTargetKind) string {
	switch k {
	case consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE:
		return "project-template"
	case consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT:
		return "deployment"
	default:
		return ""
	}
}

func targetKindFromString(s string) consolev1.TemplatePolicyBindingTargetKind {
	switch s {
	case "project-template":
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE
	case "deployment":
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT
	default:
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_UNSPECIFIED
	}
}

// templateScopeLabel mirrors templatepolicies.templateScopeLabel but lives
// here so this package does not import console/templatepolicies (avoiding a
// dependency cycle with future handler wiring).
func templateScopeLabel(scope scopeshim.Scope) string {
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

func scopeFromTemplateLabel(label string) scopeshim.Scope {
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
