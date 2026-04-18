package templatepolicies

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
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// K8sClient wraps Kubernetes client operations for TemplatePolicy ConfigMap
// CRUD. TemplatePolicy objects live only in organization or folder namespaces;
// any attempt to read, write, or delete a policy in a project namespace is
// rejected before the request reaches the Kubernetes API (HOL-554).
type K8sClient struct {
	client   kubernetes.Interface
	Resolver *resolver.Resolver
}

// NewK8sClient creates a K8sClient for TemplatePolicy operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

// ProjectNamespaceError is returned whenever a caller attempts to read or write
// a TemplatePolicy against a namespace the resolver classifies as a project
// namespace. The offending namespace is exposed on the Namespace field so the
// handler can surface it in its InvalidArgument response without re-deriving
// it from string parsing.
type ProjectNamespaceError struct {
	Namespace string
}

func (e *ProjectNamespaceError) Error() string {
	return fmt.Sprintf("template policies cannot be stored in project namespace %q; use the owning folder or organization namespace", e.Namespace)
}

// namespaceForScope translates a TemplateScopeRef into a Kubernetes namespace
// name. This method never returns a project namespace — project scope is
// rejected with InvalidArgument-equivalent semantics via ProjectNamespaceError.
//
// All CRUD methods on K8sClient funnel through this helper so the
// folder-only-storage invariant lives in exactly one place. The handler must
// not reach past this function into per-scope namespace derivation; doing so
// would bypass the classification check.
func (k *K8sClient) namespaceForScope(scope consolev1.TemplateScope, scopeName string) (string, error) {
	var ns string
	switch scope {
	case consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION:
		ns = k.Resolver.OrgNamespace(scopeName)
	case consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER:
		ns = k.Resolver.FolderNamespace(scopeName)
	case consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT:
		// Project scope never produces a valid namespace for policy storage.
		// We intentionally do NOT call the resolver's project-namespace
		// helper here — the regression test in k8s_test.go asserts this
		// package never references that helper. The namespace name we need
		// for the error message is derived from the raw prefixes directly.
		projectNs := k.Resolver.NamespacePrefix + k.Resolver.ProjectPrefix + scopeName
		return "", &ProjectNamespaceError{Namespace: projectNs}
	default:
		return "", fmt.Errorf("unknown template scope %v", scope)
	}

	// Defense in depth: the resolver may classify a non-default prefix
	// configuration as project even when the caller asked for org/folder. If
	// that ever happens, reject rather than silently storing in a project
	// namespace.
	kind, _, err := k.Resolver.ResourceTypeFromNamespace(ns)
	if err != nil {
		// Prefix mismatch means the namespace is not managed by any known
		// resource type. Let the caller decide; K8s will return the
		// appropriate error on the subsequent request.
		return ns, nil
	}
	if kind == v1alpha2.ResourceTypeProject {
		return "", &ProjectNamespaceError{Namespace: ns}
	}
	return ns, nil
}

// ListPolicies returns every TemplatePolicy ConfigMap in the scope's namespace.
func (k *K8sClient) ListPolicies(ctx context.Context, scope consolev1.TemplateScope, scopeName string) ([]corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	return k.listPoliciesInNamespace(ctx, ns)
}

// ListPoliciesInNamespace lists TemplatePolicy ConfigMaps in the given
// namespace without routing through scope resolution. The folderResolver
// (HOL-567) uses this during the ancestor walk so it can feed each folder or
// organization namespace directly.
//
// The caller is responsible for ensuring the namespace is NOT a project
// namespace — this method deliberately does NOT re-check the resource type,
// because it is invoked from a walker that already skipped project-kind
// namespaces. Re-validating here would duplicate the guard and make the
// behavior slower than necessary.
func (k *K8sClient) ListPoliciesInNamespace(ctx context.Context, ns string) ([]corev1.ConfigMap, error) {
	if ns == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	return k.listPoliciesInNamespace(ctx, ns)
}

func (k *K8sClient) listPoliciesInNamespace(ctx context.Context, ns string) ([]corev1.ConfigMap, error) {
	labelSelector := v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeTemplatePolicy
	slog.DebugContext(ctx, "listing template policies from kubernetes",
		slog.String("namespace", ns),
		slog.String("labelSelector", labelSelector),
	)
	list, err := k.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing template policies in %q: %w", ns, err)
	}
	return list.Items, nil
}

// UnmarshalRules exposes the internal rule parser to other packages (notably
// console/policyresolver) so the real TemplatePolicy resolver introduced in
// HOL-567 can decode stored rules without re-implementing the JSON wire
// shape. Returns an empty slice for the empty-string input so callers can
// iterate without a nil check.
func UnmarshalRules(raw string) ([]*consolev1.TemplatePolicyRule, error) {
	return unmarshalRules(raw)
}

// GetPolicy retrieves a single TemplatePolicy ConfigMap by name.
func (k *K8sClient) GetPolicy(ctx context.Context, scope consolev1.TemplateScope, scopeName, name string) (*corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "getting template policy from kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
}

// CreatePolicy creates a new TemplatePolicy ConfigMap. Rules are serialized as
// JSON in the AnnotationTemplatePolicyRules annotation so the ConfigMap has no
// data payload of its own — all policy state lives in annotations, mirroring
// the linked-templates annotation pattern on Template ConfigMaps.
func (k *K8sClient) CreatePolicy(ctx context.Context, scope consolev1.TemplateScope, scopeName, name, displayName, description, creatorEmail string, rules []*consolev1.TemplatePolicyRule) (*corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}

	rulesJSON, err := marshalRules(rules)
	if err != nil {
		return nil, err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplatePolicy,
				v1alpha2.LabelTemplateScope: scopeLabelValue(scope),
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:         displayName,
				v1alpha2.AnnotationDescription:         description,
				v1alpha2.AnnotationCreatorEmail:        creatorEmail,
				v1alpha2.AnnotationTemplatePolicyRules: string(rulesJSON),
			},
		},
	}
	slog.DebugContext(ctx, "creating template policy in kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
}

// UpdatePolicy updates an existing TemplatePolicy ConfigMap in place. Only
// pointer fields that are non-nil are applied; callers can partial-update by
// passing nil for fields they want preserved.
func (k *K8sClient) UpdatePolicy(ctx context.Context, scope consolev1.TemplateScope, scopeName, name string, displayName, description *string, rules []*consolev1.TemplatePolicyRule, updateRules bool) (*corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting template policy for update: %w", err)
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
	if updateRules {
		rulesJSON, err := marshalRules(rules)
		if err != nil {
			return nil, err
		}
		cm.Annotations[v1alpha2.AnnotationTemplatePolicyRules] = string(rulesJSON)
	}
	slog.DebugContext(ctx, "updating template policy in kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
}

// DeletePolicy deletes a TemplatePolicy ConfigMap. Not-found errors propagate
// from the Kubernetes client so the handler can map them to connect.CodeNotFound.
func (k *K8sClient) DeletePolicy(ctx context.Context, scope consolev1.TemplateScope, scopeName, name string) error {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return err
	}
	slog.DebugContext(ctx, "deleting template policy from kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{})
}

// scopeLabelValue returns the label string for a TemplateScope. Only
// organization and folder values are reachable; project is rejected upstream,
// so fall through to empty string — which would make any ConfigMap unusable
// and therefore catch any bug that routed a project scope through this
// function.
func scopeLabelValue(scope consolev1.TemplateScope) string {
	switch scope {
	case consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION:
		return v1alpha2.TemplateScopeOrganization
	case consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER:
		return v1alpha2.TemplateScopeFolder
	default:
		return ""
	}
}

// storedRule is the JSON wire shape for a TemplatePolicyRule annotation entry.
// Nested structs carry their own JSON representation so a hand-authored
// ConfigMap without the latest generated types still round-trips.
type storedRule struct {
	Kind     string            `json:"kind"`
	Template storedTemplateRef `json:"template"`
	Target   storedTarget      `json:"target"`
}

type storedTemplateRef struct {
	Scope             string `json:"scope"`
	ScopeName         string `json:"scope_name"`
	Name              string `json:"name"`
	VersionConstraint string `json:"version_constraint,omitempty"`
}

type storedTarget struct {
	ProjectPattern    string `json:"project_pattern"`
	DeploymentPattern string `json:"deployment_pattern,omitempty"`
}

func marshalRules(rules []*consolev1.TemplatePolicyRule) ([]byte, error) {
	stored := make([]storedRule, 0, len(rules))
	for _, r := range rules {
		if r == nil {
			continue
		}
		sr := storedRule{
			Kind: kindToString(r.GetKind()),
		}
		if tmpl := r.GetTemplate(); tmpl != nil {
			sr.Template = storedTemplateRef{
				Scope:             templateScopeLabel(tmpl.GetScope()),
				ScopeName:         tmpl.GetScopeName(),
				Name:              tmpl.GetName(),
				VersionConstraint: tmpl.GetVersionConstraint(),
			}
		}
		if tgt := r.GetTarget(); tgt != nil {
			sr.Target = storedTarget{
				ProjectPattern:    tgt.GetProjectPattern(),
				DeploymentPattern: tgt.GetDeploymentPattern(),
			}
		}
		stored = append(stored, sr)
	}
	b, err := json.Marshal(stored)
	if err != nil {
		return nil, fmt.Errorf("serializing template policy rules: %w", err)
	}
	return b, nil
}

func unmarshalRules(raw string) ([]*consolev1.TemplatePolicyRule, error) {
	if raw == "" {
		return nil, nil
	}
	var stored []storedRule
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("parsing template policy rules: %w", err)
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
	return rules, nil
}

func kindToString(k consolev1.TemplatePolicyKind) string {
	switch k {
	case consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE:
		return "require"
	case consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE:
		return "exclude"
	default:
		return ""
	}
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

// templateScopeLabel mirrors templates.scopeLabelValue but lives here so this
// package does not import console/templates (avoiding a dependency cycle
// with the render-time resolver wiring in HOL-567).
func templateScopeLabel(scope consolev1.TemplateScope) string {
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
