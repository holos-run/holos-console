package templatepolicybindings

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// PolicyExistsGetter is the narrow interface this package asks of the
// TemplatePolicy storage layer. *templatepolicies.K8sClient satisfies it
// via its GetPolicy method; defining the interface here means console.go
// can wire the adapter without this package importing
// console/templatepolicies (which would create a cycle once the policy
// resolver also imports bindings in HOL-596).
type PolicyExistsGetter interface {
	GetPolicy(ctx context.Context, scope consolev1.TemplateScope, scopeName, name string) (*corev1.ConfigMap, error)
}

// PolicyExistsAdapter implements PolicyExistsResolver over a
// PolicyExistsGetter. A K8s NotFound is translated to (false, nil); any
// other error is returned as-is so the handler can distinguish "policy
// missing" (user-input error) from "cluster probe failed" (internal
// error).
type PolicyExistsAdapter struct {
	Getter PolicyExistsGetter
}

// NewPolicyExistsAdapter returns a PolicyExistsAdapter backed by the given
// getter.
func NewPolicyExistsAdapter(g PolicyExistsGetter) *PolicyExistsAdapter {
	return &PolicyExistsAdapter{Getter: g}
}

// PolicyExists reports whether a TemplatePolicy with the given scope and
// name exists. Returns (false, nil) for K8s NotFound so the handler can
// cleanly reject with CodeInvalidArgument.
func (a *PolicyExistsAdapter) PolicyExists(ctx context.Context, scope consolev1.TemplateScope, scopeName, name string) (bool, error) {
	if a == nil || a.Getter == nil {
		return false, fmt.Errorf("policy-exists adapter is not configured")
	}
	_, err := a.Getter.GetPolicy(ctx, scope, scopeName, name)
	if err == nil {
		return true, nil
	}
	if k8serrors.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

// WalkerInterface is the subset of the resolver walker used by the
// ancestor-chain adapter. *resolver.Walker satisfies it, as does its
// cached variant. Declaring the local interface keeps this package free
// of concrete walker construction details and keeps tests simple.
type WalkerInterface interface {
	WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error)
}

// AncestorChainAdapter implements AncestorChainResolver over a walker. The
// walker's result is a child→parent chain of namespaces; the adapter
// scans that chain for `wantNs` and returns true on the first match.
type AncestorChainAdapter struct {
	Walker WalkerInterface
}

// NewAncestorChainAdapter returns an AncestorChainAdapter backed by the
// given walker.
func NewAncestorChainAdapter(w WalkerInterface) *AncestorChainAdapter {
	return &AncestorChainAdapter{Walker: w}
}

// AncestorChainContains reports whether `wantNs` appears anywhere in the
// ancestor chain starting at `startNs`. Walker errors propagate to the
// caller — a transient cluster failure must not silently admit a binding
// whose policy_ref cannot be confirmed in-chain.
func (a *AncestorChainAdapter) AncestorChainContains(ctx context.Context, startNs, wantNs string) (bool, error) {
	if a == nil || a.Walker == nil {
		return false, fmt.Errorf("ancestor-chain adapter is not configured")
	}
	chain, err := a.Walker.WalkAncestors(ctx, startNs)
	if err != nil {
		return false, err
	}
	for _, ns := range chain {
		if ns != nil && ns.Name == wantNs {
			return true, nil
		}
	}
	return false, nil
}

// ProjectExistsAdapter implements ProjectExistsResolver against a live
// kubernetes.Interface. A project exists iff a namespace with the
// expected prefix carries the console's managed-by +
// resource-type=project labels and is not being deleted. The adapter
// does NOT enforce that the project's ancestor chain passes through the
// binding's owning scope — that belongs to HOL-596 (render-time
// evaluation); at authoring time we only confirm the project exists
// somewhere under the resolver's naming conventions so a typo fails
// loud.
type ProjectExistsAdapter struct {
	Client   kubernetes.Interface
	Resolver *resolver.Resolver
}

// NewProjectExistsAdapter returns a ProjectExistsAdapter.
func NewProjectExistsAdapter(client kubernetes.Interface, r *resolver.Resolver) *ProjectExistsAdapter {
	return &ProjectExistsAdapter{Client: client, Resolver: r}
}

// ProjectExists reports whether a managed project namespace with the
// given project slug exists. Returns (false, nil) if the namespace is
// absent or unmanaged; returns an error only for unexpected K8s
// failures.
func (a *ProjectExistsAdapter) ProjectExists(ctx context.Context, _ consolev1.TemplateScope, _, projectName string) (bool, error) {
	if a == nil || a.Client == nil || a.Resolver == nil {
		return false, fmt.Errorf("project-exists adapter is not configured")
	}
	nsName := a.Resolver.NamespacePrefix + a.Resolver.ProjectPrefix + projectName
	ns, err := a.Client.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if ns.DeletionTimestamp != nil {
		return false, nil
	}
	if ns.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
		return false, nil
	}
	if ns.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeProject {
		return false, nil
	}
	return true, nil
}
