package resources

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/folders"
	"github.com/holos-run/holos-console/console/projects"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "resources"

// Handler implements the ResourceService introduced in HOL-602.
//
// Per HOL-602's acceptance criteria:
//   - the RPC returns folders and projects in a single flat list;
//   - each entry carries its ancestor path in root→leaf order, with the
//     organization first and the entry's immediate parent last;
//   - the response respects the caller's RBAC on each entry — list
//     permission is checked per entry against the entry's own share-users
//     and share-roles grants;
//   - the optional organization filter restricts results to a single org;
//   - the optional types filter restricts results to folders, projects, or
//     both. RESOURCE_TYPE_UNSPECIFIED in the types list is silently
//     ignored.
type Handler struct {
	consolev1connect.UnimplementedResourceServiceHandler
	k8s      *K8sClient
	resolver *resolver.Resolver
	walker   *resolver.Walker
}

// NewHandler builds a ResourceService handler from a composite K8sClient,
// a namespace resolver, and a walker. The walker is used to compute each
// entry's ancestor chain — wired in production from the controller-runtime
// cache-backed getter so the per-hop namespace lookups don't pay an
// apiserver round-trip.
func NewHandler(k8s *K8sClient, r *resolver.Resolver, walker *resolver.Walker) *Handler {
	return &Handler{k8s: k8s, resolver: r, walker: walker}
}

// ListResources returns the flat cross-kind list per the ResourceService
// contract in resources.proto. Implementation notes:
//
//   - Authentication is enforced first; unauthenticated callers receive
//     CodeUnauthenticated.
//   - The optional types filter is normalised: an empty list (or a list
//     containing only RESOURCE_TYPE_UNSPECIFIED) means "both kinds".
//   - For each candidate namespace the handler walks ancestors via the
//     shared Walker, classifies each ancestor with the resolver, and
//     emits a PathElement per hop in root→leaf order. The leaf entry is
//     not repeated in the path — Resource.name / Resource.display_name
//     carry that information.
//   - RBAC is per-entry: a folder is included when the caller has
//     PERMISSION_FOLDERS_LIST against the folder's own grants; a project
//     is included when the caller has PERMISSION_PROJECTS_LIST against
//     the project's own grants. Mirrors the existing per-kind handlers
//     (no cross-kind cascade in this phase).
//   - A per-request walker cache (CachedWalker) memoises ancestor chains
//     so a deeply-nested project shared with siblings doesn't pay the
//     walk cost twice.
func (h *Handler) ListResources(
	ctx context.Context,
	req *connect.Request[consolev1.ListResourcesRequest],
) (*connect.Response[consolev1.ListResourcesResponse], error) {
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	includeFolders, includeProjects := normalizeTypes(req.Msg.GetTypes())

	cachedWalker := h.walker.CachedWalker()
	orgDisplayCache := make(map[string]string)
	now := time.Now()

	var resources []*consolev1.Resource

	if includeFolders {
		folderNamespaces, err := h.k8s.ListFolders(ctx, req.Msg.GetOrganization())
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("listing folders: %w", err))
		}
		for _, ns := range folderNamespaces {
			if !h.callerCanListFolder(claims, ns, now) {
				continue
			}
			res := h.buildResource(ctx, ns, consolev1.ResourceType_RESOURCE_TYPE_FOLDER, cachedWalker, orgDisplayCache)
			resources = append(resources, res)
		}
	}

	if includeProjects {
		projectNamespaces, err := h.k8s.ListProjects(ctx, req.Msg.GetOrganization())
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("listing projects: %w", err))
		}
		for _, ns := range projectNamespaces {
			if !h.callerCanListProject(claims, ns, now) {
				continue
			}
			res := h.buildResource(ctx, ns, consolev1.ResourceType_RESOURCE_TYPE_PROJECT, cachedWalker, orgDisplayCache)
			resources = append(resources, res)
		}
	}

	slog.InfoContext(ctx, "resources listed",
		slog.String("action", "resources_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.GetOrganization()),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("total", len(resources)),
	)

	return connect.NewResponse(&consolev1.ListResourcesResponse{
		Resources: resources,
	}), nil
}

// normalizeTypes converts the optional types filter into two booleans.
// Per the proto contract, RESOURCE_TYPE_UNSPECIFIED is silently ignored;
// an empty (or all-UNSPECIFIED) list means "both kinds" so the navigation
// UI can issue a single unfiltered request.
func normalizeTypes(types []consolev1.ResourceType) (includeFolders, includeProjects bool) {
	if len(types) == 0 {
		return true, true
	}
	any := false
	for _, t := range types {
		switch t {
		case consolev1.ResourceType_RESOURCE_TYPE_FOLDER:
			includeFolders = true
			any = true
		case consolev1.ResourceType_RESOURCE_TYPE_PROJECT:
			includeProjects = true
			any = true
		}
	}
	if !any {
		return true, true
	}
	return includeFolders, includeProjects
}

// callerCanListFolder reports whether the caller has list permission on
// the folder's own grants. Mirrors folders.CheckFolderListAccess so the
// cross-kind RPC and the per-kind ListFolders return the same set.
func (h *Handler) callerCanListFolder(claims *rpc.Claims, ns *corev1.Namespace, now time.Time) bool {
	shareUsers, _ := folders.GetShareUsers(ns)
	shareRoles, _ := folders.GetShareRoles(ns)
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)
	return rbac.CheckAccessGrants(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionFoldersList) == nil
}

// callerCanListProject reports whether the caller has list permission on
// the project's own grants. Project list intentionally does NOT cascade
// from the parent organization (ADR 007).
func (h *Handler) callerCanListProject(claims *rpc.Claims, ns *corev1.Namespace, now time.Time) bool {
	shareUsers, _ := projects.GetShareUsers(ns)
	shareRoles, _ := projects.GetShareRoles(ns)
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)
	return rbac.CheckAccessGrants(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsList) == nil
}

// buildResource assembles a single Resource entry. The ancestor path is
// produced by walking the namespace hierarchy via the cached walker,
// dropping the leaf (which is the entry itself), and reversing the
// child→parent walk into the root→leaf order required by the proto.
//
// Resilience: a folder/project the caller is allowed to see MUST appear in
// the response even when its ancestor chain is partially unresolvable
// (transient apiserver hiccup, an ancestor namespace that was just deleted
// or reparented, an ancestor of an unexpected kind). In those cases the
// path is truncated at the deepest resolvable hop and the failure is
// recorded as a structured warning so an operator can investigate without
// the missing entry vanishing from navigation. This matches the behavior
// of ListFolders / ListProjects — they too return the entry itself even
// when its parent chain cannot be fully classified.
func (h *Handler) buildResource(
	ctx context.Context,
	ns *corev1.Namespace,
	entryType consolev1.ResourceType,
	cachedWalker *resolver.CachedWalker,
	orgDisplayCache map[string]string,
) *consolev1.Resource {
	res := &consolev1.Resource{
		Type:        entryType,
		DisplayName: displayName(ns),
		Name:        leafName(h.resolver, ns),
	}

	chain, err := cachedWalker.WalkAncestors(ctx, ns.Name)
	if err != nil {
		slog.WarnContext(ctx, "ancestor walk failed; returning resource with empty path",
			slog.String("namespace", ns.Name),
			slog.Any("error", err),
		)
		return res
	}
	if len(chain) == 0 {
		slog.WarnContext(ctx, "ancestor walk returned empty chain; returning resource with empty path",
			slog.String("namespace", ns.Name),
		)
		return res
	}

	// The walker returns child→parent (entry first, org last). Drop the
	// leaf (the entry itself) and reverse the rest to get root→leaf.
	ancestors := chain[1:]
	path := make([]*consolev1.PathElement, 0, len(ancestors))
	for i := len(ancestors) - 1; i >= 0; i-- {
		ancestor := ancestors[i]
		element, err := h.pathElementFromNamespace(ctx, ancestor, orgDisplayCache)
		if err != nil {
			slog.WarnContext(ctx, "skipping unclassifiable ancestor; truncating path",
				slog.String("namespace", ns.Name),
				slog.String("ancestor", ancestor.Name),
				slog.Any("error", err),
			)
			break
		}
		path = append(path, element)
	}
	res.Path = path

	return res
}

// pathElementFromNamespace produces a single PathElement from an ancestor
// namespace. The element's name field is the user-facing slug (org name,
// folder slug) — never the Kubernetes namespace prefix-stamped name.
func (h *Handler) pathElementFromNamespace(
	ctx context.Context,
	ns *corev1.Namespace,
	orgDisplayCache map[string]string,
) (*consolev1.PathElement, error) {
	kind, name, err := h.resolver.ResourceTypeFromNamespace(ns.Name)
	if err != nil {
		return nil, err
	}
	switch kind {
	case v1alpha2.ResourceTypeOrganization:
		// The proto contract says path[0] for an org root is
		// RESOURCE_TYPE_UNSPECIFIED — orgs are not a ResourceType (only
		// folders and projects are returned as Resource entries).
		return &consolev1.PathElement{
			Name:        name,
			DisplayName: h.orgDisplayName(ctx, name, ns, orgDisplayCache),
			Type:        consolev1.ResourceType_RESOURCE_TYPE_UNSPECIFIED,
		}, nil
	case v1alpha2.ResourceTypeFolder:
		return &consolev1.PathElement{
			Name:        name,
			DisplayName: displayName(ns),
			Type:        consolev1.ResourceType_RESOURCE_TYPE_FOLDER,
		}, nil
	case v1alpha2.ResourceTypeProject:
		// Projects shouldn't appear as ancestors (only orgs and folders
		// can parent another resource), but emit a defensive entry so a
		// misconfigured cluster doesn't drop the entire chain.
		return &consolev1.PathElement{
			Name:        name,
			DisplayName: displayName(ns),
			Type:        consolev1.ResourceType_RESOURCE_TYPE_PROJECT,
		}, nil
	default:
		return nil, fmt.Errorf("unexpected resource type %q on namespace %q", kind, ns.Name)
	}
}

// orgDisplayName returns the org's human-readable display name. The
// ancestor walk already returned the org's namespace object, so the
// display annotation is read directly from it. The cache exists so that
// repeated walks across siblings of the same org don't repeat the lookup.
func (h *Handler) orgDisplayName(_ context.Context, name string, orgNs *corev1.Namespace, cache map[string]string) string {
	if cached, ok := cache[name]; ok {
		return cached
	}
	display := displayName(orgNs)
	cache[name] = display
	return display
}

// displayName reads the human-readable display name annotation, returning
// the empty string if unset. Callers (and the proto contract) say clients
// MUST fall back to `name` when this is empty — keeping the empty value
// here preserves that contract end-to-end.
func displayName(ns *corev1.Namespace) string {
	if ns.Annotations == nil {
		return ""
	}
	return ns.Annotations[v1alpha2.AnnotationDisplayName]
}

// leafName returns the user-facing slug for the entry namespace. Reading
// from labels is the cheap path; falling back to the resolver handles
// label-stripped fixtures.
func leafName(r *resolver.Resolver, ns *corev1.Namespace) string {
	if ns.Labels != nil {
		if folder := ns.Labels[v1alpha2.LabelFolder]; folder != "" {
			return folder
		}
		if project := ns.Labels[v1alpha2.LabelProject]; project != "" {
			return project
		}
	}
	if _, name, err := r.ResourceTypeFromNamespace(ns.Name); err == nil {
		return name
	}
	return ns.Name
}
