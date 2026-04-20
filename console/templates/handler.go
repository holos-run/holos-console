// Package templates provides the unified TemplateService handler for CUE-based
// templates at every hierarchy level (organization, folder, project). This
// package replaces the separate DeploymentTemplateService (console/templates/)
// and OrgTemplateService (console/org_templates/) from v1alpha1 (ADR 021).
package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"connectrpc.com/connect"
	"cuelang.org/go/cue/parser"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "template"

// dnsLabelRe validates template names as DNS labels.
var dnsLabelRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// scopeKind is a local discriminator for RBAC routing. Every handler
// classifies an incoming namespace into one of these three values via
// the resolver; the namespace remains authoritative for storage.
type scopeKind int

const (
	scopeKindUnspecified scopeKind = iota
	scopeKindOrganization
	scopeKindFolder
	scopeKindProject
)

// String returns a short lowercase label for audit logs and error messages.
func (s scopeKind) String() string {
	switch s {
	case scopeKindOrganization:
		return v1alpha2.ResourceTypeOrganization
	case scopeKindFolder:
		return v1alpha2.ResourceTypeFolder
	case scopeKindProject:
		return v1alpha2.ResourceTypeProject
	default:
		return "unspecified"
	}
}

// scopeNamespace returns the Kubernetes namespace owning a (kind, name)
// pair. Unspecified inputs produce an empty string so the caller can treat
// the result as "no namespace."
func scopeNamespace(r *resolver.Resolver, kind scopeKind, name string) string {
	if r == nil {
		return ""
	}
	switch kind {
	case scopeKindOrganization:
		return r.OrgNamespace(name)
	case scopeKindFolder:
		return r.FolderNamespace(name)
	case scopeKindProject:
		return r.ProjectNamespace(name)
	default:
		return ""
	}
}

// classifyNamespace returns the scopeKind and logical name (org/folder/project
// slug) for a Kubernetes namespace via the resolver's prefix scheme.
func classifyNamespace(r *resolver.Resolver, ns string) (scopeKind, string) {
	if r == nil {
		return scopeKindUnspecified, ""
	}
	kind, name, err := r.ResourceTypeFromNamespace(ns)
	if err != nil {
		return scopeKindUnspecified, ""
	}
	switch kind {
	case v1alpha2.ResourceTypeOrganization:
		return scopeKindOrganization, name
	case v1alpha2.ResourceTypeFolder:
		return scopeKindFolder, name
	case v1alpha2.ResourceTypeProject:
		return scopeKindProject, name
	}
	return scopeKindUnspecified, ""
}

// OrgGrantResolver resolves organization-level grants.
type OrgGrantResolver interface {
	GetOrgGrants(ctx context.Context, org string) (users, roles map[string]string, err error)
}

// FolderGrantResolver resolves folder-level grants.
type FolderGrantResolver interface {
	GetFolderGrants(ctx context.Context, folder string) (users, roles map[string]string, err error)
}

// ProjectGrantResolver resolves project namespace grants for access checks.
type ProjectGrantResolver interface {
	GetProjectGrants(ctx context.Context, project string) (shareUsers, shareRoles map[string]string, err error)
}

// AncestorWalker walks the namespace hierarchy to collect ancestor namespaces.
type AncestorWalker interface {
	WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error)
}

// RenderResource is a single rendered resource with its YAML representation
// and its raw object data for JSON serialization.
type RenderResource struct {
	YAML   string
	Object map[string]any
}

// GroupedRenderResources holds rendered resources partitioned by origin: platform
// (organization/folder-level templates) and project (project-level templates).
type GroupedRenderResources struct {
	Platform []RenderResource
	Project  []RenderResource
	// Structured CUE evaluation outputs as JSON, propagated from
	// deployments.GroupedResources. Nil means the section was absent.
	DefaultsJSON                *string
	PlatformInputJSON           *string
	ProjectInputJSON            *string
	PlatformResourcesStructJSON *string
	ProjectResourcesStructJSON  *string
}

// Renderer evaluates a CUE template unified with platform and user CUE input
// strings and returns a list of rendered Kubernetes manifests.
type Renderer interface {
	Render(ctx context.Context, cueTemplate string, cuePlatformInput string, cueInput string) ([]RenderResource, error)
	// RenderWithTemplateSources evaluates the template unified with zero or more
	// ancestor template CUE sources, then with the CUE input.
	RenderWithTemplateSources(ctx context.Context, cueTemplate string, templateSources []string, cuePlatformInput string, cueInput string) ([]RenderResource, error)
	// RenderGrouped evaluates the template and returns resources grouped by origin.
	RenderGrouped(ctx context.Context, cueTemplate string, cuePlatformInput string, cueInput string) (*GroupedRenderResources, error)
	// RenderGroupedWithTemplateSources evaluates the template unified with ancestor
	// sources and returns resources grouped by origin.
	RenderGroupedWithTemplateSources(ctx context.Context, cueTemplate string, templateSources []string, cuePlatformInput string, cueInput string) (*GroupedRenderResources, error)
}

// ProjectTemplateDriftChecker exposes the minimal surface needed to serve
// TemplateService.GetProjectTemplatePolicyState and to record the applied
// render set on successful CreateTemplate/UpdateTemplate at project scope.
// Defined as a local interface so tests can stub it without importing the
// policyresolver package through this handler.
type ProjectTemplateDriftChecker interface {
	// PolicyState returns the full TemplatePolicy drift snapshot for a
	// project-scope template. `project` is the owning project slug;
	// `templateName` is the template's DNS label within that project;
	// `explicitRefs` carries the owner-linked template list read from the
	// target template's LinkedTemplates annotation.
	PolicyState(ctx context.Context, project, templateName string, explicitRefs []*consolev1.LinkedTemplateRef) (*consolev1.PolicyState, error)
	// RecordApplied persists the effective render set for the
	// project-scope template on successful Create/Update. Idempotent.
	RecordApplied(ctx context.Context, project, templateName string, refs []*consolev1.LinkedTemplateRef) error
}

// Handler implements the unified TemplateService (ADR 021).
type Handler struct {
	consolev1connect.UnimplementedTemplateServiceHandler
	k8s                  *K8sClient
	orgGrantResolver     OrgGrantResolver
	folderGrantResolver  FolderGrantResolver
	projectGrantResolver ProjectGrantResolver
	walker               AncestorWalker
	resolver             *resolver.Resolver
	renderer             Renderer
	// policyResolver is the TemplatePolicy resolution seam threaded through
	// every render path in this handler (HOL-566 Phase 4). Phase 5 (HOL-567)
	// swaps the no-op implementation wired at server startup for a real
	// REQUIRE/EXCLUDE resolver without touching this handler.
	policyResolver policyresolver.PolicyResolver
	// projectTemplateDriftChecker serves GetProjectTemplatePolicyState and
	// the Create/Update write-through to folder-namespace storage. Optional
	// — a nil value disables drift reporting and applied-state recording
	// for project-scope templates (HOL-567).
	projectTemplateDriftChecker ProjectTemplateDriftChecker
}

// NewHandler creates a TemplateService handler. policyResolver is the
// TemplatePolicy resolution seam — Phase 4 callers should pass
// policyresolver.NewNoopResolver(); Phase 5 swaps in a real implementation.
func NewHandler(k8s *K8sClient, r *resolver.Resolver, renderer Renderer, policyResolver policyresolver.PolicyResolver) *Handler {
	return &Handler{k8s: k8s, resolver: r, renderer: renderer, policyResolver: policyResolver}
}

// WithOrgGrantResolver configures the handler with an OrgGrantResolver.
func (h *Handler) WithOrgGrantResolver(ogr OrgGrantResolver) *Handler {
	h.orgGrantResolver = ogr
	return h
}

// WithFolderGrantResolver configures the handler with a FolderGrantResolver.
func (h *Handler) WithFolderGrantResolver(fgr FolderGrantResolver) *Handler {
	h.folderGrantResolver = fgr
	return h
}

// WithProjectGrantResolver configures the handler with a ProjectGrantResolver.
func (h *Handler) WithProjectGrantResolver(pgr ProjectGrantResolver) *Handler {
	h.projectGrantResolver = pgr
	return h
}

// WithAncestorWalker configures the handler with an AncestorWalker for
// hierarchy-aware permission checks and ancestor template collection.
func (h *Handler) WithAncestorWalker(w AncestorWalker) *Handler {
	h.walker = w
	return h
}

// WithProjectTemplateDriftChecker wires the TemplatePolicy drift checker used
// by GetProjectTemplatePolicyState and by CreateTemplate/UpdateTemplate
// (project scope) to persist the resolved render set. A nil checker disables
// the behavior so local/dev wiring without a cluster policy resolver works.
func (h *Handler) WithProjectTemplateDriftChecker(c ProjectTemplateDriftChecker) *Handler {
	h.projectTemplateDriftChecker = c
	return h
}

// ListTemplates returns all templates in the given scope.
func (h *Handler) ListTemplates(
	ctx context.Context,
	req *connect.Request[consolev1.ListTemplatesRequest],
) (*connect.Response[consolev1.ListTemplatesResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesList); err != nil {
		return nil, err
	}

	ns := scopeNamespace(h.k8s.Resolver, scope, scopeName)
	crds, err := h.k8s.ListTemplates(ctx, ns)
	if err != nil {
		return nil, mapK8sError(err)
	}

	templates := make([]*consolev1.Template, 0, len(crds))
	for i := range crds {
		templates = append(templates, templateCRDToProto(&crds[i], scope == scopeKindProject))
	}

	slog.InfoContext(ctx, "templates listed",
		slog.String("action", "templates_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(templates)),
	)

	return connect.NewResponse(&consolev1.ListTemplatesResponse{
		Templates: templates,
	}), nil
}

// SearchTemplates returns templates visible to the caller across organization,
// folder, and project namespaces in a single flat response (HOL-602). The
// request supports four optional, intersecting filters:
//
//   - namespace: when non-empty, only templates owned by the given namespace
//     are returned. The namespace must classify through the resolver as one
//     of organization/folder/project for RBAC to apply; an unclassifiable
//     namespace yields an empty result rather than an error so this RPC
//     behaves consistently with the "templates visible to the caller"
//     contract.
//   - name: exact DNS-label match against Template.name.
//   - display_name_contains: case-insensitive substring match against
//     Template.display_name.
//   - organization: restricts results to namespaces rooted at the given
//     organization. Combines with namespace when both are set.
//
// RBAC is enforced per owning namespace using the same TemplateCascadePerms
// table that ListTemplates consults — a caller without PermissionTemplatesList
// at a given scope simply doesn't see that scope's templates. Per-namespace
// grant lookups are memoized for the duration of one request so a folder with
// 100 templates pays one folder-grant lookup, not 100.
func (h *Handler) SearchTemplates(
	ctx context.Context,
	req *connect.Request[consolev1.SearchTemplatesRequest],
) (*connect.Response[consolev1.SearchTemplatesResponse], error) {
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}
	if h.k8s == nil || h.k8s.Resolver == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("namespace resolver not wired"))
	}

	nameFilter := req.Msg.GetName()
	displayNameNeedle := strings.ToLower(req.Msg.GetDisplayNameContains())
	orgFilter := req.Msg.GetOrganization()
	nsFilter := req.Msg.GetNamespace()

	var crds []templatesv1alpha1.Template
	if nsFilter != "" {
		// Single-namespace path — equivalent to ListTemplates but without
		// short-circuiting on access denial; if the caller cannot see the
		// scope, the result is an empty list (visible-to-caller contract).
		ns := nsFilter
		scope, scopeName := classifyNamespace(h.k8s.Resolver, ns)
		if scope == scopeKindUnspecified {
			// Unclassifiable namespace — return empty rather than error so
			// the search RPC stays consistent with cross-scope behavior.
			return connect.NewResponse(&consolev1.SearchTemplatesResponse{}), nil
		}
		// Apply organization filter when the namespace is folder- or
		// project-scoped (org scope's name == filter target).
		if orgFilter != "" && !h.namespaceBelongsToOrg(ctx, scope, scopeName, ns, orgFilter) {
			return connect.NewResponse(&consolev1.SearchTemplatesResponse{}), nil
		}
		listed, err := h.k8s.ListTemplates(ctx, ns)
		if err != nil {
			return nil, mapK8sError(err)
		}
		// Skip the access check when the caller has no permission at this
		// scope — return an empty list instead of erroring.
		if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesList); err != nil {
			return connect.NewResponse(&consolev1.SearchTemplatesResponse{}), nil
		}
		crds = listed
	} else {
		// Cross-scope path — list every Template the controller-runtime
		// client can see and filter per template.
		listed, err := h.k8s.ListAllTemplates(ctx)
		if err != nil {
			return nil, mapK8sError(err)
		}
		crds = listed
	}

	// Per-namespace access cache keyed by (ns) so we evaluate RBAC at most
	// once per scope namespace within this request, even when the namespace
	// owns N templates.
	nsAccess := make(map[string]bool, 16)
	allows := func(ns string) bool {
		if cached, ok := nsAccess[ns]; ok {
			return cached
		}
		scope, scopeName := classifyNamespace(h.k8s.Resolver, ns)
		if scope == scopeKindUnspecified {
			nsAccess[ns] = false
			return false
		}
		// Org filter: skip namespaces that don't roll up to the requested
		// organization. For org-scope namespaces the scopeName is the org
		// name itself; for folder and project namespaces consult the
		// LabelOrganization stamped at create time (HOL-602).
		if orgFilter != "" {
			if !h.namespaceBelongsToOrg(ctx, scope, scopeName, ns, orgFilter) {
				nsAccess[ns] = false
				return false
			}
		}
		err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesList)
		ok := err == nil
		nsAccess[ns] = ok
		return ok
	}

	templates := make([]*consolev1.Template, 0, len(crds))
	for i := range crds {
		tmpl := &crds[i]
		if nameFilter != "" && tmpl.Name != nameFilter {
			continue
		}
		if displayNameNeedle != "" {
			haystack := strings.ToLower(tmpl.Spec.DisplayName)
			if !strings.Contains(haystack, displayNameNeedle) {
				continue
			}
		}
		if !allows(tmpl.Namespace) {
			continue
		}
		scope, _ := classifyNamespace(h.k8s.Resolver, tmpl.Namespace)
		templates = append(templates, templateCRDToProto(tmpl, scope == scopeKindProject))
	}

	slog.InfoContext(ctx, "templates searched",
		slog.String("action", "templates_search"),
		slog.String("resource_type", auditResourceType),
		slog.String("namespace_filter", nsFilter),
		slog.String("name_filter", nameFilter),
		slog.String("display_name_contains", req.Msg.GetDisplayNameContains()),
		slog.String("organization_filter", orgFilter),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(templates)),
	)

	return connect.NewResponse(&consolev1.SearchTemplatesResponse{
		Templates: templates,
	}), nil
}

// namespaceBelongsToOrg reports whether the given namespace rolls up to the
// given organization. For org-scope namespaces the check is direct against
// the resolver-derived scope name; for folder and project namespaces it
// reads the v1alpha2.LabelOrganization stamped on the namespace at create
// time. The label is present on every managed namespace and is updated on
// reparent, so a single Get-namespace lookup is enough to attribute the
// namespace to its root organization without walking the parent chain
// per template (HOL-602).
//
// Lookups are intentionally per-call here — SearchTemplates wraps the
// allows() check in a per-namespace cache so each unique namespace sees at
// most one Get-namespace round-trip per request.
func (h *Handler) namespaceBelongsToOrg(ctx context.Context, scope scopeKind, scopeName, ns, org string) bool {
	if scope == scopeKindOrganization {
		return scopeName == org
	}
	got, err := h.k8s.GetNamespaceOrg(ctx, ns)
	if err != nil {
		// Treat lookup failures as "not in this org" so a misconfigured
		// fixture or a transient apiserver glitch never widens the search
		// beyond what the caller asked for.
		slog.WarnContext(ctx, "failed to resolve namespace organization for SearchTemplates filter",
			slog.String("namespace", ns),
			slog.Any("error", err),
		)
		return false
	}
	return got == org
}
func (h *Handler) GetTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.GetTemplateRequest],
) (*connect.Response[consolev1.GetTemplateResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	name := req.Msg.Name
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesRead); err != nil {
		return nil, err
	}

	ns := scopeNamespace(h.k8s.Resolver, scope, scopeName)
	tmpl, err := h.k8s.GetTemplate(ctx, ns, name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template read",
		slog.String("action", "template_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
	)

	return connect.NewResponse(&consolev1.GetTemplateResponse{
		Template: templateCRDToProto(tmpl, scope == scopeKindProject),
	}), nil
}

// GetTemplateDefaults returns the defaults block evaluated from a template's
// CUE source. It is the single authoritative source for Create Deployment form
// pre-fill (see ADR 027). Inline `*` defaults declared on `input` fields are
// NOT read — only the top-level `defaults` CUE block is considered.
//
// Behavior:
//   - For non-project scopes the response is an empty TemplateDefaults message
//     (defaults are a project-scope concept per ADR 027). Returning an empty
//     message rather than an error lets the UI call this RPC uniformly.
//   - For project-scope templates without a `defaults` block (or with all
//     non-concrete fields) the response is an empty TemplateDefaults message.
//   - Missing templates return CodeNotFound; callers lacking read permission
//     on the scope receive CodePermissionDenied (mirrors GetTemplate).
func (h *Handler) GetTemplateDefaults(
	ctx context.Context,
	req *connect.Request[consolev1.GetTemplateDefaultsRequest],
) (*connect.Response[consolev1.GetTemplateDefaultsResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	name := req.Msg.GetName()
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesRead); err != nil {
		return nil, err
	}

	// Defaults are a project-scope concept (ADR 027). For org/folder scopes,
	// return an empty TemplateDefaults so the UI can call uniformly without
	// special-casing scope.
	if scope != scopeKindProject {
		return connect.NewResponse(&consolev1.GetTemplateDefaultsResponse{
			Defaults: &consolev1.TemplateDefaults{},
		}), nil
	}

	ns := scopeNamespace(h.k8s.Resolver, scope, scopeName)
	tmpl, err := h.k8s.GetTemplate(ctx, ns, name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	cueSource := tmpl.Spec.CueTemplate
	extracted, err := ExtractDefaults(cueSource)
	if err != nil {
		slog.WarnContext(ctx, "failed to extract template defaults",
			slog.String("scope", scope.String()),
			slog.String("scopeName", scopeName),
			slog.String("name", name),
			slog.Any("error", err),
		)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to extract template defaults: %w", err))
	}
	if extracted == nil {
		extracted = &consolev1.TemplateDefaults{}
	}

	slog.InfoContext(ctx, "template defaults read",
		slog.String("action", "template_defaults_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
	)

	return connect.NewResponse(&consolev1.GetTemplateDefaultsResponse{
		Defaults: extracted,
	}), nil
}

// CreateTemplate creates a new template at the given scope.
func (h *Handler) CreateTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.CreateTemplateRequest],
) (*connect.Response[consolev1.CreateTemplateResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	tmpl := req.Msg.GetTemplate()
	if tmpl == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("template is required"))
	}
	name := tmpl.Name
	if err := validateTemplateName(name); err != nil {
		return nil, err
	}
	if tmpl.CueTemplate != "" {
		if err := validateCueSyntax(tmpl.CueTemplate); err != nil {
			return nil, err
		}
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesWrite); err != nil {
		return nil, err
	}

	// Enforce scoped link permissions when linked templates are provided.
	if len(tmpl.LinkedTemplates) > 0 {
		if err := h.checkLinkPermissions(ctx, claims, scope, scopeName, tmpl.LinkedTemplates); err != nil {
			return nil, err
		}
	}

	// The `mandatory` annotation and its Go/proto projections were removed in
	// HOL-565. Ancestor templates that must always apply to every project now
	// come in via TemplatePolicy REQUIRE rules (HOL-567).
	ns := scopeNamespace(h.k8s.Resolver, scope, scopeName)
	_, err = h.k8s.CreateTemplate(ctx, ns, name, tmpl.DisplayName, tmpl.Description, tmpl.CueTemplate, tmpl.Defaults, tmpl.Enabled, tmpl.LinkedTemplates)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Write-through the policy-effective ref set for project-scope templates
	// so GetProjectTemplatePolicyState has a baseline to diff against
	// (HOL-569). Org/folder scopes are skipped because they are not
	// render targets — only project-scope templates participate in policy
	// resolution today. A nil drift checker (local/dev bootstrap without a
	// cluster policy resolver) is a no-op. Record failures are logged at
	// warn level and do not fail the RPC — the template was persisted
	// successfully and the set is reconstructable on the next preview.
	h.recordProjectTemplateApplied(ctx, scope, scopeName, name, tmpl.LinkedTemplates)

	slog.InfoContext(ctx, "template created",
		slog.String("action", "template_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateTemplateResponse{
		Name: name,
	}), nil
}

// UpdateTemplate updates an existing template.
func (h *Handler) UpdateTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateTemplateRequest],
) (*connect.Response[consolev1.UpdateTemplateResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	tmpl := req.Msg.GetTemplate()
	if tmpl == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("template is required"))
	}
	name := tmpl.Name
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	if tmpl.CueTemplate != "" {
		if err := validateCueSyntax(tmpl.CueTemplate); err != nil {
			return nil, err
		}
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesWrite); err != nil {
		return nil, err
	}

	displayName := tmpl.DisplayName
	description := tmpl.Description
	cueTemplate := tmpl.CueTemplate
	enabled := tmpl.Enabled

	// Determine linked template handling based on the update_linked_templates flag.
	// We also track the post-update effective explicit link set — new refs
	// when the caller asked to update links, the existing refs otherwise —
	// so the HOL-569 write-through on the success path records the right
	// baseline against which future drift checks will compare.
	//
	// skipRecordPreservedLinks flips to true when the preserve-links branch
	// could not determine the existing explicit set (read or parse failure)
	// so the write-through can be skipped rather than recording a phantom
	// empty set that would produce false drift on the next policy-state read.
	var linkedTemplates []*consolev1.LinkedTemplateRef
	var explicitRefsForRecord []*consolev1.LinkedTemplateRef
	skipRecordPreservedLinks := false
	ns := scopeNamespace(h.k8s.Resolver, scope, scopeName)
	if req.Msg.GetUpdateLinkedTemplates() {
		// Caller wants to modify links. Check permissions based on both old
		// (being removed) and new (being added) linked template scopes.
		existing, getErr := h.k8s.GetTemplate(ctx, ns, name)
		if getErr != nil {
			return nil, mapK8sError(getErr)
		}
		existingRefs := crdLinkedToProto(existing.Spec.LinkedTemplates)
		// Merge old and new refs to check all affected scopes.
		allRefs := append(existingRefs, tmpl.LinkedTemplates...)
		if len(allRefs) > 0 {
			if err := h.checkLinkPermissions(ctx, claims, scope, scopeName, allRefs); err != nil {
				return nil, err
			}
		}
		linkedTemplates = tmpl.LinkedTemplates
		// Protobuf binary encoding cannot distinguish an omitted repeated
		// field from an empty one — both arrive as nil.  When the caller
		// explicitly asked to update links, nil means "clear all links,"
		// so normalize to a non-nil empty slice.  K8sClient.UpdateTemplate
		// treats nil as "preserve existing" and empty as "delete annotation."
		if linkedTemplates == nil {
			linkedTemplates = []*consolev1.LinkedTemplateRef{}
		}
		explicitRefsForRecord = linkedTemplates
	} else {
		// When update_linked_templates is false, linkedTemplates stays nil,
		// which tells K8sClient.UpdateTemplate to preserve existing links.
		// For the HOL-569 write-through we need to know what those existing
		// links are so the resolver sees the same explicit ref set the
		// next preview will see. Only read the existing template when we
		// actually intend to record — project scope with a drift checker
		// wired — so non-project scopes don't pay an unnecessary API call.
		//
		// If the pre-update read or annotation parse fails, we set
		// skipRecordPreservedLinks so the write-through is skipped
		// entirely; recording nil in that branch would silently persist
		// an empty applied set even though the ConfigMap kept its
		// original links, producing false drift on the next policy-state
		// read (review findings P2 from codex round 1).
		if scope == scopeKindProject && h.projectTemplateDriftChecker != nil {
			existing, getErr := h.k8s.GetTemplate(ctx, ns, name)
			if getErr != nil {
				slog.WarnContext(ctx, "failed to read existing template for applied-set write-through, skipping record",
					slog.String("scope", scope.String()),
					slog.String("scopeName", scopeName),
					slog.String("name", name),
					slog.Any("error", getErr),
				)
				skipRecordPreservedLinks = true
			} else if refs := crdLinkedToProto(existing.Spec.LinkedTemplates); len(refs) > 0 {
				explicitRefsForRecord = refs
			}
			// If the template exists with no linked templates, zero
			// explicit links is the correct baseline and we do NOT skip.
		}
	}

	_, err = h.k8s.UpdateTemplate(ctx, ns, name, &displayName, &description, &cueTemplate, tmpl.Defaults, false, &enabled, linkedTemplates, false)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Write-through the policy-effective ref set for project-scope templates
	// after a successful persist so GetProjectTemplatePolicyState reflects
	// the new linking state (HOL-569). Non-project scopes are skipped as on
	// the create path. Record failures are logged but do not fail the RPC.
	// Skip entirely when the preserve-links branch could not read the
	// existing explicit set — recording nil in that case would silently
	// persist a mismatched applied set (review round 1 P2 finding).
	if !skipRecordPreservedLinks {
		h.recordProjectTemplateApplied(ctx, scope, scopeName, name, explicitRefsForRecord)
	}

	slog.InfoContext(ctx, "template updated",
		slog.String("action", "template_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateTemplateResponse{}), nil
}

// recordProjectTemplateApplied writes the policy-effective ref set for a
// project-scope template to the applied-render-set store. This is the
// write-through path invoked from the CreateTemplate/UpdateTemplate happy
// paths so GetProjectTemplatePolicyState and the project-template drift
// badge have a baseline to diff against (HOL-569).
//
// This is a no-op for non-project scopes because only project-scope
// templates participate in TemplatePolicy resolution today (org/folder
// templates are not render targets on their own — they are only unified
// into a project render via ancestor walks). It is also a no-op when
// no ProjectTemplateDriftChecker is wired (local/dev bootstrap without a
// cluster policy resolver).
//
// The resolver is invoked directly (not via ListEffectiveTemplateSources)
// because the template handler does not render during Create/Update — the
// sources are irrelevant here, only the resolved ref set is. A failure
// is logged at warn level and swallowed so the RPC's success contract is
// not broken: the template was persisted, and the set can be reconstructed
// on the next preview.
func (h *Handler) recordProjectTemplateApplied(
	ctx context.Context,
	scope scopeKind,
	scopeName, name string,
	explicitRefs []*consolev1.LinkedTemplateRef,
) {
	if scope != scopeKindProject {
		return
	}
	if h.projectTemplateDriftChecker == nil {
		return
	}
	effectiveRefs := explicitRefs
	if h.policyResolver != nil {
		projectNs := h.resolver.ProjectNamespace(scopeName)
		resolved, resolveErr := h.policyResolver.Resolve(ctx, projectNs, policyresolver.TargetKindProjectTemplate, name, explicitRefs)
		if resolveErr != nil {
			slog.WarnContext(ctx, "failed to resolve policy for project template applied-set write-through",
				slog.String("project", scopeName),
				slog.String("template", name),
				slog.Any("error", resolveErr),
			)
			return
		}
		effectiveRefs = resolved
	}
	if recordErr := h.projectTemplateDriftChecker.RecordApplied(ctx, scopeName, name, effectiveRefs); recordErr != nil {
		slog.WarnContext(ctx, "failed to record applied render set for project template",
			slog.String("project", scopeName),
			slog.String("template", name),
			slog.Any("error", recordErr),
		)
	}
}

// DeleteTemplate deletes a template.
func (h *Handler) DeleteTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteTemplateRequest],
) (*connect.Response[consolev1.DeleteTemplateResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	name := req.Msg.Name
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesDelete); err != nil {
		return nil, err
	}

	ns := scopeNamespace(h.k8s.Resolver, scope, scopeName)
	if err := h.k8s.DeleteTemplate(ctx, ns, name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template deleted",
		slog.String("action", "template_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteTemplateResponse{}), nil
}

// CloneTemplate copies an existing template to a new name within the same scope.
func (h *Handler) CloneTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.CloneTemplateRequest],
) (*connect.Response[consolev1.CloneTemplateResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	sourceName := req.Msg.SourceName
	newName := req.Msg.Name
	if sourceName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("source_name is required"))
	}
	if err := validateTemplateName(newName); err != nil {
		return nil, err
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesWrite); err != nil {
		return nil, err
	}

	ns := scopeNamespace(h.k8s.Resolver, scope, scopeName)
	_, err = h.k8s.CloneTemplate(ctx, ns, sourceName, newName, req.Msg.DisplayName)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template cloned",
		slog.String("action", "template_clone"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("source_name", sourceName),
		slog.String("name", newName),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CloneTemplateResponse{
		Name: newName,
	}), nil
}

// RenderTemplate evaluates a CUE template and returns rendered Kubernetes manifests.
func (h *Handler) RenderTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.RenderTemplateRequest],
) (*connect.Response[consolev1.RenderTemplateResponse], error) {
	if req.Msg.CueTemplate == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("cue_template is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if h.renderer == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("renderer not configured"))
	}

	grouped, err := h.renderTemplateGrouped(ctx, req.Msg)
	if err != nil {
		return nil, err
	}

	// Serialize per-collection resources.
	platformYAML, platformJSON, err := serializeResources(grouped.Platform)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to serialize platform resources: %w", err))
	}
	projectYAML, projectJSON, err := serializeResources(grouped.Project)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to serialize project resources: %w", err))
	}

	// Produce the unified rendered output by combining both collections.
	allResources := append(grouped.Platform, grouped.Project...)
	unifiedYAML, unifiedJSON, err := serializeResources(allResources)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to serialize unified resources: %w", err))
	}

	return connect.NewResponse(&consolev1.RenderTemplateResponse{
		RenderedYaml:                    unifiedYAML,
		RenderedJson:                    unifiedJSON,
		PlatformResourcesYaml:           platformYAML,
		PlatformResourcesJson:           platformJSON,
		ProjectResourcesYaml:            projectYAML,
		ProjectResourcesJson:            projectJSON,
		DefaultsJson:                    grouped.DefaultsJSON,
		PlatformInputJson:               grouped.PlatformInputJSON,
		ProjectInputJson:                grouped.ProjectInputJSON,
		PlatformResourcesStructuredJson: grouped.PlatformResourcesStructJSON,
		ProjectResourcesStructuredJson:  grouped.ProjectResourcesStructJSON,
	}), nil
}

// renderTemplateGrouped resolves the effective ancestor-template source list
// for the preview target and delegates to the renderer. Both paths — this
// preview and the deployments apply path — now route through the same
// K8sClient.ListEffectiveTemplateSources helper, so preview-vs-apply
// divergence of the ancestor-source slice is structurally impossible
// (HOL-562 Phase 2, HOL-564).
//
// When the handler has no Kubernetes client (in-process test / no-k8s mode),
// the render runs without any ancestor sources. The previous three-branch
// fallback ladder (walker → org-only → plain) was deleted: production always
// has a walker, and tests that want no walker simply pass nil like the
// deployments handler does.
func (h *Handler) renderTemplateGrouped(ctx context.Context, msg *consolev1.RenderTemplateRequest) (*GroupedRenderResources, error) {
	var templateSources []string
	// HOL-619/HOL-723 made the request's namespace the authoritative
	// identifier. Classify it for the preview-target-kind discriminator
	// only; storage works directly against the namespace.
	if h.k8s != nil && h.walker != nil && msg.GetNamespace() != "" {
		startNs := msg.GetNamespace()
		msgKind, msgName := classifyNamespace(h.k8s.Resolver, startNs)
		if msgKind == scopeKindUnspecified {
			slog.WarnContext(ctx, "failed to classify namespace for render, falling back to plain render",
				slog.String("namespace", startNs),
			)
		} else {
			// ListEffectiveTemplateSources currently swallows walker errors
			// internally and returns (nil, nil) on walker failure (see
			// k8s.go: "failed to walk ancestor chain for render, returning
			// empty sources"). The walkErr branch below is therefore
			// unreachable today but kept as belt-and-suspenders so a future
			// edit that starts propagating walker errors out of the helper
			// still degrades to the plain-render fallback here.
			sources, _, walkErr := h.k8s.ListEffectiveTemplateSources(
				ctx,
				startNs,
				previewTargetKindForScope(msgKind),
				msgName,
				msg.LinkedTemplates,
				h.walker,
				h.policyResolver,
			)
			if walkErr != nil {
				slog.WarnContext(ctx, "ancestor template resolution failed, falling back to plain render",
					slog.Any("error", walkErr),
				)
			} else {
				templateSources = sources
			}
		}
	}

	if len(templateSources) == 0 {
		grouped, err := h.renderer.RenderGrouped(ctx, msg.CueTemplate, msg.CuePlatformInput, msg.CueProjectInput)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("template render failed: %w", err))
		}
		return grouped, nil
	}
	grouped, err := h.renderer.RenderGroupedWithTemplateSources(ctx, msg.CueTemplate, templateSources, msg.CuePlatformInput, msg.CueProjectInput)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("template render failed: %w", err))
	}
	return grouped, nil
}

// previewTargetKindForScope maps the preview's request scope to the
// TargetKind the unified helper expects. Project scope means a project-scope
// template is being previewed (TargetKindProjectTemplate). Org/folder/unknown
// scopes also use TargetKindProjectTemplate today — no call site actually
// differentiates yet. The discriminator is plumbed end-to-end by HOL-566
// Phase 4 so Phase 5 (HOL-567) can key real REQUIRE/EXCLUDE evaluation off
// it without touching call sites.
//
// IMPORTANT: This helper is for the PREVIEW render path only. The deployments
// apply path does NOT go through this function: it calls
// K8sClient.ListEffectiveTemplateSources directly with TargetKindDeployment
// from AncestorTemplateResolver. When Phase 5 wires real policy evaluation,
// do not add a branch here that returns TargetKindDeployment — that would be
// wrong because this function never runs on the apply path. The scope input
// is intentionally ignored today; the parameter exists so Phase 5 can add
// preview-path-specific TargetKind discrimination (e.g., different kinds for
// project-vs-folder preview) without changing the call site signature.
func previewTargetKindForScope(kind scopeKind) TargetKind {
	_ = kind
	return TargetKindProjectTemplate
}

// ListLinkableTemplates returns all enabled templates in ancestor scopes that
// the given scope may link against (ADR 021 Decision 7). When
// include_self_scope is true the response also contains enabled templates at
// the request's own scope — needed by the TemplatePolicy editor so org-scope
// policies (which have no ancestors) and folder-scope policies can pick
// same-scope templates. See HOL-561.
func (h *Handler) ListLinkableTemplates(
	ctx context.Context,
	req *connect.Request[consolev1.ListLinkableTemplatesRequest],
) (*connect.Response[consolev1.ListLinkableTemplatesResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	includeSelfScope := req.Msg.GetIncludeSelfScope()

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesList); err != nil {
		return nil, err
	}

	if h.walker == nil {
		return connect.NewResponse(&consolev1.ListLinkableTemplatesResponse{}), nil
	}

	// Walk ancestors of the given scope's namespace.
	startNs := scopeNamespace(h.k8s.Resolver, scope, scopeName)

	ancestors, walkErr := h.walker.WalkAncestors(ctx, startNs)
	if walkErr != nil {
		slog.WarnContext(ctx, "failed to walk ancestors for linkable templates",
			slog.String("scope", scope.String()),
			slog.String("scopeName", scopeName),
			slog.Any("error", walkErr),
		)
		return connect.NewResponse(&consolev1.ListLinkableTemplatesResponse{}), nil
	}

	// Collect linkable (enabled) templates from each ancestor. When
	// include_self_scope is false we skip the first namespace (the scope
	// itself) and only return ancestor templates — this preserves existing
	// project-template linking semantics. When include_self_scope is true we
	// include the scope's own templates as well so the policy editor can pick
	// same-scope templates. See HOL-561.
	var result []*consolev1.LinkableTemplate
	for i, ns := range ancestors {
		if i == 0 && !includeSelfScope {
			continue // skip the scope itself unless explicitly requested
		}
		entryKind, _ := classifyNamespace(h.resolver, ns.Name)
		if entryKind == scopeKindUnspecified {
			continue
		}
		infos, err := h.k8s.ListLinkableTemplateInfos(ctx, ns.Name)
		if err != nil {
			slog.WarnContext(ctx, "failed to list linkable templates from namespace",
				slog.String("namespace", ns.Name),
				slog.Any("error", err),
			)
			continue
		}
		// Fetch releases for each linkable template and populate the Releases
		// field, stripping cue_template and defaults to keep the payload small.
		for _, lt := range infos {
			rels, relErr := h.k8s.ListReleases(ctx, ns.Name, lt.Name)
			if relErr != nil {
				slog.WarnContext(ctx, "failed to list releases for linkable template",
					slog.String("template", lt.Name),
					slog.Any("error", relErr),
				)
				continue
			}
			releases := make([]*consolev1.Release, 0, len(rels))
			for i := range rels {
				r := releaseCRDToProto(&rels[i])
				// Strip heavy fields the linking UI does not need.
				r.CueTemplate = ""
				r.Defaults = nil
				releases = append(releases, r)
			}
			lt.Releases = releases
		}
		result = append(result, infos...)
	}

	slog.InfoContext(ctx, "linkable templates listed",
		slog.String("action", "linkable_templates_list"),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.Bool("include_self_scope", includeSelfScope),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(result)),
	)

	return connect.NewResponse(&consolev1.ListLinkableTemplatesResponse{
		Templates: result,
	}), nil
}

// ListAncestorTemplates returns templates from all ancestor scopes that
// participate in the effective render set for the given scope. This is used by
// the renderer to compute the effective template set (ADR 021 Decision 6).
func (h *Handler) ListAncestorTemplates(
	ctx context.Context,
	req *connect.Request[consolev1.ListAncestorTemplatesRequest],
) (*connect.Response[consolev1.ListAncestorTemplatesResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesRead); err != nil {
		return nil, err
	}

	templates, err := h.collectAncestorTemplates(ctx, scope, scopeName, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&consolev1.ListAncestorTemplatesResponse{
		Templates: templates,
	}), nil
}

// collectAncestorTemplates walks the hierarchy and collects templates from all
// ancestor scopes plus the current scope itself. The render-set formula is:
//
//	enabled AND ref IN linkedRefs
//
// Results are returned in org→folders→project order for correct CUE
// unification. If linkedRefs is empty, no ancestor templates are returned.
// The "mandatory" annotation branch of the effective set was removed in
// HOL-565; TemplatePolicy REQUIRE rules (wired in HOL-567) reintroduce
// unconditional ancestor inclusion at render time via the policy resolver
// that sits in front of ListEffectiveTemplateSources — not via this
// helper, which only surfaces the caller's explicit linkedRefs.
//
// Storage-isolation note (HOL-554): the traversal only visits ancestor
// namespaces — organization and folder — and never reads templates from a
// project namespace even when the project itself is the starting scope.
// TemplatePolicy ConfigMaps and applied-render-set state live exclusively
// in folder/organization namespaces precisely because project owners can
// write to their project namespace and would otherwise be able to tamper
// with the constraints the platform is enforcing.
func (h *Handler) collectAncestorTemplates(ctx context.Context, scope scopeKind, scopeName string, linkedRefs []*consolev1.LinkedTemplateRef) ([]*consolev1.Template, error) {
	if h.walker == nil {
		return nil, nil
	}

	startNs := scopeNamespace(h.k8s.Resolver, scope, scopeName)

	ancestors, err := h.walker.WalkAncestors(ctx, startNs)
	if err != nil {
		return nil, fmt.Errorf("walking ancestors for %s/%s: %w", scope, scopeName, err)
	}

	// Build a set of linked refs for O(1) lookup.
	linkedSet := make(map[linkedRef]bool, len(linkedRefs))
	for _, ref := range linkedRefs {
		if ref != nil {
			linkedSet[linkedRefFromProto(ref)] = true
		}
	}

	// Collect templates from each ancestor, in reverse (org first, child last).
	// ancestors is child→parent order; reverse to get org→child.
	var result []*consolev1.Template
	for i := len(ancestors) - 1; i >= 0; i-- {
		ns := ancestors[i]
		ancestorKind, _ := classifyNamespace(h.resolver, ns.Name)
		if ancestorKind == scopeKindUnspecified {
			continue
		}

		crds, err := h.k8s.ListTemplates(ctx, ns.Name)
		if err != nil {
			slog.WarnContext(ctx, "failed to list templates from ancestor, skipping",
				slog.String("namespace", ns.Name),
				slog.Any("error", err),
			)
			continue
		}

		for i := range crds {
			crd := &crds[i]
			if !crd.Spec.Enabled {
				continue
			}
			ref := linkedRef{
				namespace: ns.Name,
				name:      crd.Name,
			}
			if !linkedSet[ref] {
				continue
			}
			result = append(result, templateCRDToProto(crd, ancestorKind == scopeKindProject))
		}
	}

	return result, nil
}

// checkLinkPermissions verifies the caller has the scoped link permissions
// required by the provided linked template references. If any ref targets an
// org-scope template, PermissionTemplatesLinkOrgWrite is checked. If any ref
// targets a folder-scope template, PermissionTemplatesLinkFolderWrite is checked.
// Both checks are performed at the template's owning scope.
func (h *Handler) checkLinkPermissions(ctx context.Context, claims *rpc.Claims, scope scopeKind, scopeName string, linkedTemplates []*consolev1.LinkedTemplateRef) error {
	hasOrg := false
	hasFolder := false
	for _, ref := range linkedTemplates {
		if ref == nil {
			continue
		}
		refKind, _ := classifyNamespace(h.resolver, ref.GetNamespace())
		switch refKind {
		case scopeKindOrganization:
			hasOrg = true
		case scopeKindFolder:
			hasFolder = true
		}
	}
	if hasOrg {
		if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesLinkOrgWrite); err != nil {
			return err
		}
	}
	if hasFolder {
		if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesLinkFolderWrite); err != nil {
			return err
		}
	}
	return nil
}

// checkAccess verifies the caller has the given permission for the requested scope.
// All scope levels (org, folder, project) use the unified TemplateCascadePerms
// table per ADR 021 Decision 2.
func (h *Handler) checkAccess(ctx context.Context, claims *rpc.Claims, scope scopeKind, scopeName string, perm rbac.Permission) error {
	switch scope {
	case scopeKindOrganization:
		return h.checkOrgAccess(ctx, claims, scopeName, perm)
	case scopeKindFolder:
		return h.checkFolderAccess(ctx, claims, scopeName, perm)
	case scopeKindProject:
		return h.checkProjectAccess(ctx, claims, scopeName, perm)
	default:
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown scope %v", scope))
	}
}

func (h *Handler) checkOrgAccess(ctx context.Context, claims *rpc.Claims, org string, perm rbac.Permission) error {
	if h.orgGrantResolver == nil {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	users, roles, err := h.orgGrantResolver.GetOrgGrants(ctx, org)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve org grants", slog.String("org", org), slog.Any("error", err))
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	return rbac.CheckCascadeAccess(claims.Email, claims.Roles, users, roles, perm, rbac.TemplateCascadePerms)
}

func (h *Handler) checkFolderAccess(ctx context.Context, claims *rpc.Claims, folder string, perm rbac.Permission) error {
	if h.folderGrantResolver == nil {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	users, roles, err := h.folderGrantResolver.GetFolderGrants(ctx, folder)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve folder grants", slog.String("folder", folder), slog.Any("error", err))
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	return rbac.CheckCascadeAccess(claims.Email, claims.Roles, users, roles, perm, rbac.TemplateCascadePerms)
}

func (h *Handler) checkProjectAccess(ctx context.Context, claims *rpc.Claims, project string, perm rbac.Permission) error {
	if h.projectGrantResolver == nil {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	users, roles, err := h.projectGrantResolver.GetProjectGrants(ctx, project)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve project grants", slog.String("project", project), slog.Any("error", err))
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	return rbac.CheckCascadeAccess(claims.Email, claims.Roles, users, roles, perm, rbac.TemplateCascadePerms)
}

// CreateRelease publishes a new immutable release of a template.
func (h *Handler) CreateRelease(
	ctx context.Context,
	req *connect.Request[consolev1.CreateReleaseRequest],
) (*connect.Response[consolev1.CreateReleaseResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	release := req.Msg.GetRelease()
	if release == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("release is required"))
	}
	templateName := release.TemplateName
	if templateName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("release.template_name is required"))
	}
	if release.Version == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("release.version is required"))
	}

	version, err := ParseVersion(release.Version)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesWrite); err != nil {
		return nil, err
	}

	ns, nsErr := h.mustNamespaceFor(scope, scopeName)
	if nsErr != nil {
		return nil, nsErr
	}

	// Create the TemplateRelease CRD. The apiserver returns AlreadyExists if
	// the version is duplicated (object name is deterministic from
	// template+version).
	rel, err := h.k8s.CreateRelease(ctx, ns, templateName, version, release.CueTemplate, release.Defaults, release.Changelog, release.UpgradeAdvice)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "release created",
		slog.String("action", "release_create"),
		slog.String("resource_type", "template-release"),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("templateName", templateName),
		slog.String("version", version.String()),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateReleaseResponse{
		Release: releaseCRDToProto(rel),
	}), nil
}

// ListReleases returns all releases for a template, sorted by version descending.
func (h *Handler) ListReleases(
	ctx context.Context,
	req *connect.Request[consolev1.ListReleasesRequest],
) (*connect.Response[consolev1.ListReleasesResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	templateName := req.Msg.TemplateName
	if templateName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("template_name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesRead); err != nil {
		return nil, err
	}

	ns, nsErr := h.mustNamespaceFor(scope, scopeName)
	if nsErr != nil {
		return nil, nsErr
	}

	rels, err := h.k8s.ListReleases(ctx, ns, templateName)
	if err != nil {
		return nil, mapK8sError(err)
	}

	releases := make([]*consolev1.Release, 0, len(rels))
	for i := range rels {
		releases = append(releases, releaseCRDToProto(&rels[i]))
	}

	slog.InfoContext(ctx, "releases listed",
		slog.String("action", "releases_list"),
		slog.String("resource_type", "template-release"),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("templateName", templateName),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(releases)),
	)

	return connect.NewResponse(&consolev1.ListReleasesResponse{
		Releases: releases,
	}), nil
}

// GetRelease retrieves a single release by template name, scope, and version.
func (h *Handler) GetRelease(
	ctx context.Context,
	req *connect.Request[consolev1.GetReleaseRequest],
) (*connect.Response[consolev1.GetReleaseResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	templateName := req.Msg.TemplateName
	if templateName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("template_name is required"))
	}
	if req.Msg.Version == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("version is required"))
	}

	version, err := ParseVersion(req.Msg.Version)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesRead); err != nil {
		return nil, err
	}

	ns, nsErr := h.mustNamespaceFor(scope, scopeName)
	if nsErr != nil {
		return nil, nsErr
	}

	rel, err := h.k8s.GetRelease(ctx, ns, templateName, version)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "release read",
		slog.String("action", "release_read"),
		slog.String("resource_type", "template-release"),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("templateName", templateName),
		slog.String("version", version.String()),
		slog.String("sub", claims.Sub),
	)

	return connect.NewResponse(&consolev1.GetReleaseResponse{
		Release: releaseCRDToProto(rel),
	}), nil
}

// CheckUpdates computes available version updates for all linked templates
// of a template (or all templates in a scope).
func (h *Handler) CheckUpdates(
	ctx context.Context,
	req *connect.Request[consolev1.CheckUpdatesRequest],
) (*connect.Response[consolev1.CheckUpdatesResponse], error) {
	scope, scopeName, err := h.extractScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatesRead); err != nil {
		return nil, err
	}

	// Collect templates to check. If template_name is specified, check only
	// that template's linked refs. Otherwise check all templates in scope.
	ns := scopeNamespace(h.k8s.Resolver, scope, scopeName)
	var templates []templatesv1alpha1.Template
	if req.Msg.TemplateName != "" {
		tmpl, getErr := h.k8s.GetTemplate(ctx, ns, req.Msg.TemplateName)
		if getErr != nil {
			return nil, mapK8sError(getErr)
		}
		templates = []templatesv1alpha1.Template{*tmpl}
	} else {
		list, listErr := h.k8s.ListTemplates(ctx, ns)
		if listErr != nil {
			return nil, mapK8sError(listErr)
		}
		templates = list
	}

	var updates []*consolev1.TemplateUpdate
	for i := range templates {
		refs := crdLinkedToProto(templates[i].Spec.LinkedTemplates)
		if len(refs) == 0 {
			continue
		}
		for _, ref := range refs {
			update, err := h.checkLinkedUpdate(ctx, ref, req.Msg.GetIncludeCurrent())
			if err != nil {
				slog.WarnContext(ctx, "failed to check update for linked template",
					slog.String("name", ref.Name),
					slog.Any("error", err),
				)
				continue
			}
			if update != nil {
				updates = append(updates, update)
			}
		}
	}

	slog.InfoContext(ctx, "updates checked",
		slog.String("action", "check_updates"),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(updates)),
	)

	return connect.NewResponse(&consolev1.CheckUpdatesResponse{
		Updates: updates,
	}), nil
}

// checkLinkedUpdate computes the update status for a single linked template
// reference. When includeCurrent is false (default), returns nil if no updates
// are available. When includeCurrent is true, always returns a TemplateUpdate
// with resolved version information even if the template is already at the
// latest compatible version.
func (h *Handler) checkLinkedUpdate(ctx context.Context, ref *consolev1.LinkedTemplateRef, includeCurrent bool) (*consolev1.TemplateUpdate, error) {
	refNamespace := ref.GetNamespace()
	refName := ref.Name
	constraintStr := ref.VersionConstraint

	// List all release versions for the linked template.
	versions, err := h.k8s.ListReleaseVersions(ctx, refNamespace, refName)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, nil // no releases means no updates
	}

	// Parse the constraint from the linked ref.
	constraint, err := ParseConstraint(constraintStr)
	if err != nil {
		return nil, err
	}

	// Approximate the current pinned version as the latest matching release.
	// The resolver picks the highest release satisfying the constraint, so
	// LatestMatchingVersion is a closer proxy than OldestMatchingVersion.
	// A truly accurate value would require tracking the resolved version per
	// deployment; this approximation is sufficient until that is implemented.
	currentVersion := LatestMatchingVersion(versions, constraint)
	var currentStr string
	if currentVersion != nil {
		currentStr = currentVersion.String()
	}

	// Find the absolute latest version (no constraint).
	latestVersion := LatestMatchingVersion(versions, nil)
	var latestStr string
	if latestVersion != nil {
		latestStr = latestVersion.String()
	}

	// Find the latest compatible version (with constraint).
	latestCompatible := LatestMatchingVersion(versions, constraint)
	var latestCompatibleStr string
	if latestCompatible != nil {
		latestCompatibleStr = latestCompatible.String()
	}

	// Determine if a breaking update exists: there is a newer version outside
	// the constraint range.
	breakingAvailable := false
	if latestVersion != nil && constraint != nil {
		if !MatchesConstraint(latestVersion, constraint) {
			breakingAvailable = true
		}
	}

	// Only report an update if there is something new, unless the caller
	// requested all entries (include_current).
	hasCompatibleUpdate := latestCompatibleStr != "" && latestCompatibleStr != currentStr
	if !hasCompatibleUpdate && !breakingAvailable && !includeCurrent {
		return nil, nil
	}

	update := &consolev1.TemplateUpdate{
		Ref:                     ref,
		CurrentVersion:          currentStr,
		LatestCompatibleVersion: latestCompatibleStr,
		LatestVersion:           latestStr,
		BreakingUpdateAvailable: breakingAvailable,
	}
	return update, nil
}

// mustNamespaceFor returns the Kubernetes namespace owning the given
// (scope, scopeName) pair. Returns connect.CodeInternal when the resolver is
// unwired or the kind is unspecified. Callers that already validated the
// scope via extractScope use this to reduce boilerplate when they need to
// route writes back to the canonical namespace.
func (h *Handler) mustNamespaceFor(scope scopeKind, scopeName string) (string, error) {
	if h.k8s == nil || h.k8s.Resolver == nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("namespace resolver not wired"))
	}
	ns := scopeNamespace(h.k8s.Resolver, scope, scopeName)
	if ns == "" {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("cannot derive namespace for scope %s/%s", scope, scopeName))
	}
	return ns, nil
}

// extractScope classifies an incoming namespace into a scopeKind and logical
// scope name. Returns InvalidArgument when the namespace is empty or cannot
// be classified. The namespace itself remains authoritative for storage —
// the derived (kind, name) pair is only used for RBAC cascade routing and
// slog attributes.
func (h *Handler) extractScope(namespace string) (scopeKind, string, error) {
	if namespace == "" {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace is required"))
	}
	if h.k8s == nil || h.k8s.Resolver == nil {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInternal, fmt.Errorf("namespace resolver not wired"))
	}
	kind, name := classifyNamespace(h.k8s.Resolver, namespace)
	if kind == scopeKindUnspecified {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("namespace %q does not match any known prefix", namespace))
	}
	return kind, name, nil
}

// validateTemplateName checks that the name is a valid DNS label.
func validateTemplateName(name string) error {
	if name == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	if len(name) > 63 {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name must be at most 63 characters"))
	}
	if !dnsLabelRe.MatchString(name) {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name must be a valid DNS label (lowercase alphanumeric and hyphens, starting with a letter)"))
	}
	return nil
}

// validateCueSyntax parses the CUE source to verify it is syntactically valid.
func validateCueSyntax(source string) error {
	_, err := parser.ParseFile("template.cue", source)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid CUE syntax: %w", err))
	}
	return nil
}

// serializeResources converts a slice of RenderResource into a multi-document
// YAML string (separated by "---\n") and a JSON array string. Returns an empty
// YAML string and "[]" for an empty or nil slice so that JSON fields are always
// valid parseable JSON arrays.
func serializeResources(resources []RenderResource) (yamlStr, jsonStr string, err error) {
	if len(resources) == 0 {
		return "", "[]", nil
	}

	var buf strings.Builder
	objects := make([]map[string]any, 0, len(resources))
	for i, r := range resources {
		if i > 0 {
			buf.WriteString("---\n")
		}
		buf.WriteString(r.YAML)
		if r.Object != nil {
			objects = append(objects, r.Object)
		}
	}

	jsonBytes, err := json.MarshalIndent(objects, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal rendered resources to JSON: %w", err)
	}
	return buf.String(), string(jsonBytes), nil
}

// mapK8sError converts Kubernetes API errors to ConnectRPC errors.
func mapK8sError(err error) error {
	if errors.IsNotFound(err) {
		return connect.NewError(connect.CodeNotFound, err)
	}
	if errors.IsAlreadyExists(err) {
		return connect.NewError(connect.CodeAlreadyExists, err)
	}
	if errors.IsForbidden(err) {
		return connect.NewError(connect.CodePermissionDenied, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

// GetProjectTemplatePolicyState returns the full TemplatePolicy drift
// snapshot for a project-scope template (HOL-567). Mirrors
// DeploymentService.GetDeploymentPolicyState but keyed by (project slug,
// template name). Non-project scopes are rejected with InvalidArgument — a
// folder-scope or org-scope template has no render surface today.
//
// When no ProjectTemplateDriftChecker is wired (dev/local bootstrap without
// a cluster policy resolver), the response carries an empty PolicyState
// with has_applied_state=false and drift=false so clients can round-trip
// the RPC without special-casing missing wiring.
func (h *Handler) GetProjectTemplatePolicyState(
	ctx context.Context,
	req *connect.Request[consolev1.GetProjectTemplatePolicyStateRequest],
) (*connect.Response[consolev1.GetProjectTemplatePolicyStateResponse], error) {
	namespace := req.Msg.GetNamespace()
	name := req.Msg.GetName()
	if namespace == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	// HOL-619 replaced the request's TemplateScopeRef with a Kubernetes
	// namespace that MUST classify as a project namespace. Rejecting a
	// non-project namespace preserves the pre-HOL-619 contract (which
	// rejected scope != TEMPLATE_SCOPE_PROJECT here).
	scope, project, err := h.extractScope(namespace)
	if err != nil {
		return nil, err
	}
	if scope != scopeKindProject {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("namespace must classify as a project namespace for project-template policy state"))
	}
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project slug is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scopeKindProject, project, rbac.PermissionTemplatesRead); err != nil {
		return nil, err
	}

	// Read the template so we can pass its owner-linked refs to the
	// resolver. The caller must have read access to the owning project for
	// this to succeed.
	projectNs := scopeNamespace(h.k8s.Resolver, scopeKindProject, project)
	tmpl, err := h.k8s.GetTemplate(ctx, projectNs, name)
	if err != nil {
		return nil, mapK8sError(err)
	}
	explicitRefs := crdLinkedToProto(tmpl.Spec.LinkedTemplates)

	if h.projectTemplateDriftChecker == nil {
		return connect.NewResponse(&consolev1.GetProjectTemplatePolicyStateResponse{
			State: &consolev1.PolicyState{},
		}), nil
	}
	state, err := h.projectTemplateDriftChecker.PolicyState(ctx, project, name, explicitRefs)
	if err != nil {
		slog.WarnContext(ctx, "project-template policy state computation failed",
			slog.String("project", project),
			slog.String("template", name),
			slog.Any("error", err),
		)
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&consolev1.GetProjectTemplatePolicyStateResponse{State: state}), nil
}
