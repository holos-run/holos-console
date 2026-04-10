package projects

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/secrets"
)

// ProjectGrantResolver implements secrets.ProjectResolver by looking up
// namespace annotations for project-level grants.
type ProjectGrantResolver struct {
	k8s    *K8sClient
	walker *resolver.Walker // optional; enables ancestor default-share cascade
}

// NewProjectGrantResolver creates a resolver that reads grants from project namespaces.
func NewProjectGrantResolver(k8s *K8sClient) *ProjectGrantResolver {
	return &ProjectGrantResolver{k8s: k8s}
}

// WithWalker attaches a hierarchy walker to the resolver. When set,
// GetDefaultGrants walks the full ancestor chain (project → folders → org)
// and merges default-share grants from all levels (highest role wins per
// principal). Without a walker, GetDefaultGrants only reads project-level defaults.
func (r *ProjectGrantResolver) WithWalker(w *resolver.Walker) *ProjectGrantResolver {
	r.walker = w
	return r
}

// GetProjectGrants returns the active user and role grant maps for a project.
// The project parameter is the user-facing project name (not the Kubernetes namespace).
func (r *ProjectGrantResolver) GetProjectGrants(ctx context.Context, project string) (map[string]string, map[string]string, error) {
	ns, err := r.k8s.GetProject(ctx, project) // GetProject handles prefix resolution
	if err != nil {
		return nil, nil, err
	}
	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)
	return activeUsers, activeRoles, nil
}

// GetProjectOrganization returns the organization name for a project by reading
// the organization label from the project namespace.
func (r *ProjectGrantResolver) GetProjectOrganization(ctx context.Context, project string) (string, error) {
	ns, err := r.k8s.GetProject(ctx, project)
	if err != nil {
		return "", err
	}
	return GetOrganization(ns), nil
}

// GetDefaultGrants returns the default sharing grants for a project.
// These are applied to new secrets created in the project.
// When a Walker is configured (see WithWalker), it walks the full ancestor
// chain (project → folders → org) and merges default-share grants from all
// levels; highest role wins per principal. Without a walker, only project-level
// defaults are returned.
// Implements secrets.DefaultShareResolver.
func (r *ProjectGrantResolver) GetDefaultGrants(ctx context.Context, project string) ([]secrets.AnnotationGrant, []secrets.AnnotationGrant, error) {
	if r.walker == nil {
		// Fallback: project-level defaults only.
		ns, err := r.k8s.GetProject(ctx, project)
		if err != nil {
			return nil, nil, err
		}
		defaultUsers, _ := GetDefaultShareUsers(ns)
		defaultRoles, _ := GetDefaultShareRoles(ns)
		return defaultUsers, defaultRoles, nil
	}

	// Walk the full ancestor chain (project → folders → org) and merge defaults.
	projectNs := r.k8s.Resolver.ProjectNamespace(project)
	ancestors, err := r.walker.WalkAncestors(ctx, projectNs)
	if err != nil {
		slog.WarnContext(ctx, "failed to walk ancestor chain for default grants, falling back to project-level only",
			slog.String("project", project),
			slog.String("namespace", projectNs),
			slog.Any("error", err),
		)
		// Graceful degradation: fall back to project-level defaults only.
		ns, err := r.k8s.GetProject(ctx, project)
		if err != nil {
			return nil, nil, err
		}
		defaultUsers, _ := GetDefaultShareUsers(ns)
		defaultRoles, _ := GetDefaultShareRoles(ns)
		return defaultUsers, defaultRoles, nil
	}

	// Merge defaults from all ancestors. Walk order is child → parent (project
	// first, org last). We accumulate into the result, with highest-role-wins
	// per principal so that an explicit project default beats a broader org default.
	var mergedUsers, mergedRoles []secrets.AnnotationGrant
	for _, ns := range ancestors {
		nsDefaultUsers := parseNamespaceDefaultGrants(ns, v1alpha2.AnnotationDefaultShareUsers)
		nsDefaultRoles := parseNamespaceDefaultGrants(ns, v1alpha2.AnnotationDefaultShareRoles)
		mergedUsers = mergeAnnotationGrants(mergedUsers, nsDefaultUsers)
		mergedRoles = mergeAnnotationGrants(mergedRoles, nsDefaultRoles)
	}

	return mergedUsers, mergedRoles, nil
}

// parseNamespaceDefaultGrants parses a JSON annotation from a namespace into
// a slice of AnnotationGrant. Returns nil when the annotation is absent or empty.
func parseNamespaceDefaultGrants(ns *corev1.Namespace, key string) []secrets.AnnotationGrant {
	if ns.Annotations == nil {
		return nil
	}
	value, ok := ns.Annotations[key]
	if !ok || value == "" {
		return nil
	}
	var grants []secrets.AnnotationGrant
	if err := json.Unmarshal([]byte(value), &grants); err != nil {
		slog.Warn("failed to parse default grants annotation",
			slog.String("namespace", ns.Name),
			slog.String("annotation", key),
			slog.Any("error", err),
		)
		return nil
	}
	return grants
}

// mergeAnnotationGrants merges base and override grant slices. Highest role
// wins per principal; override wins when roles are equal.
func mergeAnnotationGrants(base, override []secrets.AnnotationGrant) []secrets.AnnotationGrant {
	merged := make(map[string]secrets.AnnotationGrant)
	for _, g := range base {
		merged[strings.ToLower(g.Principal)] = g
	}
	for _, g := range override {
		key := strings.ToLower(g.Principal)
		existing, ok := merged[key]
		if !ok || rbac.RoleLevel(rbac.RoleFromString(g.Role)) >= rbac.RoleLevel(rbac.RoleFromString(existing.Role)) {
			merged[key] = g
		}
	}
	result := make([]secrets.AnnotationGrant, 0, len(merged))
	for _, g := range merged {
		result = append(result, g)
	}
	return result
}
