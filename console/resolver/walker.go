package resolver

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

// maxWalkDepth is the maximum number of namespace hops the walker will follow
// before returning an error. Prevents infinite loops on misconfigured clusters.
const maxWalkDepth = 5

// Walker walks the namespace hierarchy by following the
// console.holos.run/parent annotation from a project or folder namespace up to
// the root organization namespace.
type Walker struct {
	Client   kubernetes.Interface
	Resolver *Resolver
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

		ns, err := w.Client.CoreV1().Namespaces().Get(ctx, current, metav1.GetOptions{})
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
