package templatepolicies

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// WalkerInterface is the subset of the ancestor walker used by the topology
// resolver. It is satisfied by *resolver.Walker and by the cached variant
// returned from Walker.CachedWalker(). Declaring the local interface keeps
// the topology resolver independent of concrete walker construction and
// also eases testing — tests can inject a stub that enumerates ancestors
// without touching the K8s API.
type WalkerInterface interface {
	WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error)
}

// K8sResourceTopology implements ResourceTopologyResolver against a live
// kubernetes.Interface. It is intentionally a thin shim: the handler layer
// owns all business logic (pattern matching, annotation parsing, error
// shaping), so this type only answers three narrow listing questions.
//
// ListProjectsUnderScope walks the managed project-namespace list and keeps
// every namespace whose ancestor chain contains the policy's owning scope
// namespace. That is stricter than a one-hop child match (which would miss
// projects inside a nested folder under a folder-scope policy) and strictly
// cheaper than calling ListChildProjects per-folder-node, because the
// ancestor walk is needed anyway to distinguish "descendant of this folder"
// from "descendant of a sibling folder with the same display name" when
// non-default resolver prefixes are in use.
type K8sResourceTopology struct {
	Client   kubernetes.Interface
	Resolver *resolver.Resolver
	Walker   WalkerInterface
}

// NewK8sResourceTopology constructs a topology resolver wired to the given
// kubernetes client, namespace-prefix resolver, and ancestor walker. The
// walker must be non-nil at call time so ancestor checks can run; all three
// arguments are required.
func NewK8sResourceTopology(client kubernetes.Interface, r *resolver.Resolver, w WalkerInterface) *K8sResourceTopology {
	return &K8sResourceTopology{Client: client, Resolver: r, Walker: w}
}

// scopeNamespace returns the Kubernetes namespace name the policy owns. We
// intentionally do not call through to K8sClient.namespaceForScope here
// because this helper must accept the organization and folder scopes only
// (project scope is rejected at handler entry) without failing on the
// ResourceTypeFromNamespace classification check — the walker enumerates
// project namespaces in a separate step.
func (t *K8sResourceTopology) scopeNamespace(scope consolev1.TemplateScope, scopeName string) (string, error) {
	switch scope {
	case consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION:
		return t.Resolver.OrgNamespace(scopeName), nil
	case consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER:
		return t.Resolver.FolderNamespace(scopeName), nil
	default:
		return "", fmt.Errorf("unsupported scope %v for topology traversal", scope)
	}
}

// ListProjectsUnderScope enumerates every managed project namespace whose
// ancestor chain passes through the policy's owning scope namespace. The
// cluster-wide namespace list is filtered by the
// `console.holos.run/managed-by` and `resource-type=project` labels so
// unmanaged namespaces never appear.
func (t *K8sResourceTopology) ListProjectsUnderScope(
	ctx context.Context,
	scope consolev1.TemplateScope,
	scopeName string,
) ([]*corev1.Namespace, error) {
	scopeNs, err := t.scopeNamespace(scope, scopeName)
	if err != nil {
		return nil, err
	}
	labelSelector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue + "," +
		v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeProject
	list, err := t.Client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("listing project namespaces: %w", err)
	}
	result := make([]*corev1.Namespace, 0, len(list.Items))
	for i := range list.Items {
		ns := &list.Items[i]
		if ns.DeletionTimestamp != nil {
			continue
		}
		if t.ancestorChainContains(ctx, ns.Name, scopeNs) {
			result = append(result, ns)
		}
	}
	return result, nil
}

// ancestorChainContains reports whether `wantNs` appears in the ancestor
// chain of `startNs`. Walker errors fall back to "not a descendant" — a
// project namespace that cannot be walked (missing parent label, cycle) is
// already dysfunctional and cannot reliably host an EXCLUDE conflict; we
// refuse to erroneously cite it as a conflict source.
func (t *K8sResourceTopology) ancestorChainContains(ctx context.Context, startNs, wantNs string) bool {
	chain, err := t.Walker.WalkAncestors(ctx, startNs)
	if err != nil {
		return false
	}
	for _, ancestor := range chain {
		if ancestor.Name == wantNs {
			return true
		}
	}
	return false
}

// ListProjectTemplates returns all Template ConfigMaps in the project
// namespace. Listing by label keeps unmanaged ConfigMaps (for example,
// anything dropped into the namespace by a customer script) from being
// treated as a candidate EXCLUDE target.
func (t *K8sResourceTopology) ListProjectTemplates(ctx context.Context, projectNs string) ([]corev1.ConfigMap, error) {
	labelSelector := v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeTemplate
	list, err := t.Client.CoreV1().ConfigMaps(projectNs).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("listing project templates in %q: %w", projectNs, err)
	}
	return list.Items, nil
}

// ListProjectDeployments returns all Deployment ConfigMaps in the project
// namespace. Same rationale as ListProjectTemplates for the label filter.
func (t *K8sResourceTopology) ListProjectDeployments(ctx context.Context, projectNs string) ([]corev1.ConfigMap, error) {
	labelSelector := v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeDeployment
	list, err := t.Client.CoreV1().ConfigMaps(projectNs).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("listing deployments in %q: %w", projectNs, err)
	}
	return list.Items, nil
}
