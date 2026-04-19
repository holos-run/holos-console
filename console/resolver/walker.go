package resolver

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

// maxWalkDepth is the maximum number of namespace hops the walker will follow
// before returning an error. Prevents infinite loops on misconfigured clusters.
const maxWalkDepth = 5

// NamespaceGetter abstracts the "fetch one namespace by name" contract the
// hierarchy walker needs. Production wires a controller-runtime cache-backed
// implementation (HOL-622) so ancestor-chain lookups are O(cache lookup) rather
// than paying an apiserver round-trip per hop; tests wire a client-go fake
// clientset (via ClientGoNamespaceGetter) because the in-memory fake already
// supports typed Namespace fixtures.
type NamespaceGetter interface {
	GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error)
}

// ClientGoNamespaceGetter adapts a client-go kubernetes.Interface onto the
// NamespaceGetter contract. This is the shape every existing test fixture
// produces — a `fake.Clientset` seeded with Namespace objects — so the getter
// abstraction stays opt-in at the call site instead of rippling the signature
// change through every policyresolver/resolver test helper.
type ClientGoNamespaceGetter struct {
	Client kubernetes.Interface
}

// GetNamespace calls CoreV1().Namespaces().Get; errors from the apiserver pass
// through unchanged so the walker's caller can inspect apierrors.IsNotFound.
func (g *ClientGoNamespaceGetter) GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	return g.Client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
}

// CtrlRuntimeNamespaceGetter adapts a controller-runtime client.Client onto the
// NamespaceGetter contract. Wiring the manager's cache-backed client here is
// the HOL-622 change that removes render-time apiserver round-trips from the
// ancestor walk: reads resolve against the shared namespace informer.
type CtrlRuntimeNamespaceGetter struct {
	Client ctrlclient.Client
}

// GetNamespace issues a typed Get against the controller-runtime client; the
// informer cache populated by the embedded Manager serves the read when the
// namespace is already known.
func (g *CtrlRuntimeNamespaceGetter) GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	if err := g.Client.Get(ctx, types.NamespacedName{Name: name}, ns); err != nil {
		return nil, err
	}
	return ns, nil
}

// Walker walks the namespace hierarchy by following the
// console.holos.run/parent annotation from a project or folder namespace up to
// the root organization namespace.
//
// HOL-622 introduced NamespaceGetter so the ancestor walk can be routed through
// the controller-runtime cache. When Getter is non-nil it takes precedence over
// Client; the Client field remains for backwards compatibility with the many
// tests that seed a `kubernetes.Interface` fake and expect to keep working
// without threading a new seam through every call site.
type Walker struct {
	Getter   NamespaceGetter
	Client   kubernetes.Interface
	Resolver *Resolver
}

// getNamespace picks between the explicit Getter and the legacy Client field.
// This internal helper keeps WalkAncestors readable at the call site — the
// selection policy lives here in one place.
func (w *Walker) getNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	if w.Getter != nil {
		return w.Getter.GetNamespace(ctx, name)
	}
	if w.Client == nil {
		return nil, fmt.Errorf("walker misconfigured: both Getter and Client are nil")
	}
	return w.Client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
}

// WalkAncestors traverses the namespace hierarchy starting from startNs,
// following the console.holos.run/parent annotation on each namespace.
// The result is returned in child→parent order (startNs first, org last).
// Returns an error if the depth exceeds maxWalkDepth, a cycle is detected,
// or a non-organization namespace is missing its parent annotation.
func (w *Walker) WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error) {
	var chain []*corev1.Namespace
	visited := make(map[string]bool)
	current := startNs

	for {
		if len(chain) >= maxWalkDepth+1 {
			return nil, fmt.Errorf("namespace hierarchy depth exceeded limit of %d starting from %q", maxWalkDepth, startNs)
		}
		if visited[current] {
			return nil, fmt.Errorf("cycle detected in namespace hierarchy at %q (starting from %q)", current, startNs)
		}
		visited[current] = true

		ns, err := w.getNamespace(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("getting namespace %q: %w", current, err)
		}
		chain = append(chain, ns)

		// Check whether this is an organization namespace (top of the tree).
		resourceType := ns.Labels[v1alpha2.LabelResourceType]
		if resourceType == v1alpha2.ResourceTypeOrganization {
			// Reached the root — done.
			break
		}

		// Non-org namespaces must have a parent annotation.
		parent := ns.Labels[v1alpha2.AnnotationParent]
		if parent == "" {
			return nil, fmt.Errorf("namespace %q is missing required parent annotation %q", current, v1alpha2.AnnotationParent)
		}
		current = parent
	}

	return chain, nil
}

// CachedWalker returns a per-request cache wrapper that memoizes WalkAncestors
// results keyed by start namespace. Use a CachedWalker when the same hierarchy
// may be walked multiple times within a single request to avoid redundant K8s
// API calls.
func (w *Walker) CachedWalker() *CachedWalker {
	return &CachedWalker{Walker: w, cache: make(map[string][]*corev1.Namespace)}
}

// CachedWalker wraps Walker with a memoization cache keyed by start namespace.
// It is not safe for concurrent use across goroutines; create one per request.
type CachedWalker struct {
	Walker *Walker
	cache  map[string][]*corev1.Namespace
}

// WalkAncestors returns the cached result for startNs when available; otherwise
// delegates to the underlying Walker and stores the result in the cache.
func (c *CachedWalker) WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error) {
	if cached, ok := c.cache[startNs]; ok {
		return cached, nil
	}
	result, err := c.Walker.WalkAncestors(ctx, startNs)
	if err != nil {
		return nil, err
	}
	c.cache[startNs] = result
	return result, nil
}
