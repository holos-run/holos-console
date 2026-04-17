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
// unmanaged namespaces never appear, and by the policy's owning
// organization label so projects in other orgs never become walker
// candidates — one stale project in an unrelated org cannot fail a policy
// write scoped to a well-formed org / folder.
//
// An ancestor-walk error for a candidate project namespace propagates to
// the caller (which turns it into connect.CodeInternal in
// validateExcludeRulesAgainstExplicitLinks). Silently dropping a project
// would bypass the HOL-570 guardrail whenever a transient namespace Get
// fails, the hierarchy is temporarily inconsistent, or the walker hits its
// depth / cycle guard — any of those conditions could hide an existing
// explicit link from the validator. The organization-label prefilter keeps
// this fail-loud behavior narrowly scoped to descendants of the policy's
// own organization, which is the blast radius operators already accept for
// TemplatePolicy authoring.
func (t *K8sResourceTopology) ListProjectsUnderScope(
	ctx context.Context,
	scope consolev1.TemplateScope,
	scopeName string,
) ([]*corev1.Namespace, error) {
	scopeNs, err := t.scopeNamespace(scope, scopeName)
	if err != nil {
		return nil, err
	}
	orgLabel, err := t.organizationForScope(ctx, scope, scopeName)
	if err != nil {
		return nil, err
	}
	labelSelector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue + "," +
		v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeProject
	if orgLabel != "" {
		labelSelector += "," + v1alpha2.LabelOrganization + "=" + orgLabel
	}
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
		contained, err := t.ancestorChainContains(ctx, ns.Name, scopeNs)
		if err != nil {
			return nil, fmt.Errorf("walking ancestors of %q: %w", ns.Name, err)
		}
		if contained {
			result = append(result, ns)
		}
	}
	return result, nil
}

// organizationForScope resolves the organization slug that owns the given
// policy scope. For organization scope the slug is the scopeName directly;
// for folder scope we read the folder namespace's
// `console.holos.run/organization` label. The label is set by the folder
// creation path and is what ListProjectsUnderScope uses to narrow the
// project-namespace candidate list. Returns an empty string when the label
// is missing — in that case the caller falls back to the unfiltered search
// so the guardrail still fires for correctly-managed projects.
func (t *K8sResourceTopology) organizationForScope(
	ctx context.Context,
	scope consolev1.TemplateScope,
	scopeName string,
) (string, error) {
	switch scope {
	case consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION:
		return scopeName, nil
	case consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER:
		ns, err := t.Client.CoreV1().Namespaces().Get(ctx, t.Resolver.FolderNamespace(scopeName), metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting folder namespace for org label: %w", err)
		}
		return ns.Labels[v1alpha2.LabelOrganization], nil
	default:
		return "", nil
	}
}

// ancestorChainContains reports whether `wantNs` appears in the ancestor
// chain of `startNs`. A walker error is surfaced to the caller rather than
// swallowed — skipping the project on walker failure would silently weaken
// the HOL-570 guardrail. Callers convert the error to connect.CodeInternal
// so the RPC layer can distinguish it from validation-failure categories.
func (t *K8sResourceTopology) ancestorChainContains(ctx context.Context, startNs, wantNs string) (bool, error) {
	chain, err := t.Walker.WalkAncestors(ctx, startNs)
	if err != nil {
		return false, err
	}
	for _, ancestor := range chain {
		if ancestor.Name == wantNs {
			return true, nil
		}
	}
	return false, nil
}

// ListProjectTemplates returns project-scope Template ConfigMaps managed
// by the console in the project namespace. The selector pins
// `managed-by=console.holos.run`, `resource-type=template`, and
// `template-scope=project` so a hand-authored ConfigMap created by a
// project owner (who has namespace-scoped write access) cannot poison the
// topology scan by fabricating a linked-templates annotation the guardrail
// would then cite as a conflict. Only ConfigMaps the console itself wrote
// — i.e. the actual render targets at HOL-570 policy-authoring time — are
// considered.
func (t *K8sResourceTopology) ListProjectTemplates(ctx context.Context, projectNs string) ([]corev1.ConfigMap, error) {
	labelSelector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue + "," +
		v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeTemplate + "," +
		v1alpha2.LabelTemplateScope + "=" + v1alpha2.TemplateScopeProject
	list, err := t.Client.CoreV1().ConfigMaps(projectNs).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("listing project templates in %q: %w", projectNs, err)
	}
	return list.Items, nil
}

// ListProjectDeployments returns Deployment ConfigMaps managed by the
// console in the project namespace. Same rationale as ListProjectTemplates
// for the managed-by filter — the guardrail must ignore user-planted
// ConfigMaps so a project owner cannot forge a "conflict" that blocks an
// administrator's legitimate EXCLUDE rule.
func (t *K8sResourceTopology) ListProjectDeployments(ctx context.Context, projectNs string) ([]corev1.ConfigMap, error) {
	labelSelector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue + "," +
		v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeDeployment
	list, err := t.Client.CoreV1().ConfigMaps(projectNs).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("listing deployments in %q: %w", projectNs, err)
	}
	return list.Items, nil
}
