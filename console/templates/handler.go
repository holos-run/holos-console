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
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"cuelang.org/go/cue/parser"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "template"

// dnsLabelRe validates template names as DNS labels.
var dnsLabelRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

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

// Renderer evaluates a CUE template unified with platform and user CUE input
// strings and returns a list of rendered Kubernetes manifests.
type Renderer interface {
	Render(ctx context.Context, cueTemplate string, cuePlatformInput string, cueInput string) ([]RenderResource, error)
	// RenderWithTemplateSources evaluates the template unified with zero or more
	// ancestor template CUE sources, then with the CUE input.
	RenderWithTemplateSources(ctx context.Context, cueTemplate string, templateSources []string, cuePlatformInput string, cueInput string) ([]RenderResource, error)
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
}

// NewHandler creates a TemplateService handler.
func NewHandler(k8s *K8sClient, r *resolver.Resolver, renderer Renderer) *Handler {
	return &Handler{k8s: k8s, resolver: r, renderer: renderer}
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

// ListTemplates returns all templates in the given scope.
func (h *Handler) ListTemplates(
	ctx context.Context,
	req *connect.Request[consolev1.ListTemplatesRequest],
) (*connect.Response[consolev1.ListTemplatesResponse], error) {
	scope, scopeName, err := extractScope(req.Msg.GetScope())
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

	cms, err := h.k8s.ListTemplates(ctx, scope, scopeName)
	if err != nil {
		return nil, mapK8sError(err)
	}

	templates := make([]*consolev1.Template, 0, len(cms))
	for _, cm := range cms {
		templates = append(templates, configMapToTemplate(&cm, scope, scopeName))
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

// GetTemplate returns a single template by name.
func (h *Handler) GetTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.GetTemplateRequest],
) (*connect.Response[consolev1.GetTemplateResponse], error) {
	scope, scopeName, err := extractScope(req.Msg.GetScope())
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

	cm, err := h.k8s.GetTemplate(ctx, scope, scopeName, name)
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
		Template: configMapToTemplate(cm, scope, scopeName),
	}), nil
}

// CreateTemplate creates a new template at the given scope.
func (h *Handler) CreateTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.CreateTemplateRequest],
) (*connect.Response[consolev1.CreateTemplateResponse], error) {
	scope, scopeName, err := extractScope(req.Msg.GetScope())
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

	_, err = h.k8s.CreateTemplate(ctx, scope, scopeName, name, tmpl.DisplayName, tmpl.Description, tmpl.CueTemplate, tmpl.Defaults, tmpl.Mandatory, tmpl.Enabled, tmpl.LinkedTemplates)
	if err != nil {
		return nil, mapK8sError(err)
	}

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
	scope, scopeName, err := extractScope(req.Msg.GetScope())
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
	mandatory := tmpl.Mandatory
	enabled := tmpl.Enabled

	// Determine linked template handling based on the update_linked_templates flag.
	var linkedTemplates []*consolev1.LinkedTemplateRef
	if req.Msg.GetUpdateLinkedTemplates() {
		// Caller wants to modify links. Check permissions based on both old
		// (being removed) and new (being added) linked template scopes.
		existingCM, getErr := h.k8s.GetTemplate(ctx, scope, scopeName, name)
		if getErr != nil {
			return nil, mapK8sError(getErr)
		}
		var existingRefs []*consolev1.LinkedTemplateRef
		if raw, ok := existingCM.Annotations[v1alpha2.AnnotationLinkedTemplates]; ok && raw != "" {
			var parseErr error
			existingRefs, parseErr = unmarshalLinkedTemplates(raw)
			if parseErr != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("parsing stored linked-templates annotation: %w", parseErr))
			}
		}
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
	}
	// When update_linked_templates is false, linkedTemplates stays nil,
	// which tells K8sClient.UpdateTemplate to preserve existing links.

	_, err = h.k8s.UpdateTemplate(ctx, scope, scopeName, name, &displayName, &description, &cueTemplate, tmpl.Defaults, false, &mandatory, &enabled, linkedTemplates, false)
	if err != nil {
		return nil, mapK8sError(err)
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

// DeleteTemplate deletes a template.
func (h *Handler) DeleteTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteTemplateRequest],
) (*connect.Response[consolev1.DeleteTemplateResponse], error) {
	scope, scopeName, err := extractScope(req.Msg.GetScope())
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

	if err := h.k8s.DeleteTemplate(ctx, scope, scopeName, name); err != nil {
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
	scope, scopeName, err := extractScope(req.Msg.GetScope())
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

	_, err = h.k8s.CloneTemplate(ctx, scope, scopeName, sourceName, newName, req.Msg.DisplayName)
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

	resources, err := h.renderer.Render(ctx, req.Msg.CueTemplate, req.Msg.CuePlatformInput, req.Msg.CueProjectInput)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("template render failed: %w", err))
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
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to marshal rendered resources to JSON: %w", err))
	}

	return connect.NewResponse(&consolev1.RenderTemplateResponse{
		RenderedYaml: buf.String(),
		RenderedJson: string(jsonBytes),
	}), nil
}

// ListLinkableTemplates returns all enabled templates in ancestor scopes that
// the given scope may link against (ADR 021 Decision 7).
func (h *Handler) ListLinkableTemplates(
	ctx context.Context,
	req *connect.Request[consolev1.ListLinkableTemplatesRequest],
) (*connect.Response[consolev1.ListLinkableTemplatesResponse], error) {
	scope, scopeName, err := extractScope(req.Msg.GetScope())
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

	if h.walker == nil {
		return connect.NewResponse(&consolev1.ListLinkableTemplatesResponse{}), nil
	}

	// Walk ancestors of the given scope's namespace.
	startNs, nsErr := h.k8s.namespaceForScope(scope, scopeName)
	if nsErr != nil {
		return nil, connect.NewError(connect.CodeInternal, nsErr)
	}

	ancestors, walkErr := h.walker.WalkAncestors(ctx, startNs)
	if walkErr != nil {
		slog.WarnContext(ctx, "failed to walk ancestors for linkable templates",
			slog.String("scope", scope.String()),
			slog.String("scopeName", scopeName),
			slog.Any("error", walkErr),
		)
		return connect.NewResponse(&consolev1.ListLinkableTemplatesResponse{}), nil
	}

	// Collect linkable (enabled) templates from all ancestors (skip the first
	// namespace since that's the scope itself — we only return ancestor templates).
	var result []*consolev1.LinkableTemplate
	for i, ns := range ancestors {
		if i == 0 {
			continue // skip the scope itself
		}
		ancestorScope, ancestorName := scopeAndNameFromNs(h.resolver, ns.Name)
		if ancestorScope == consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED {
			continue
		}
		infos, err := h.k8s.ListLinkableTemplateInfos(ctx, ancestorScope, ancestorName)
		if err != nil {
			slog.WarnContext(ctx, "failed to list linkable templates from ancestor",
				slog.String("namespace", ns.Name),
				slog.Any("error", err),
			)
			continue
		}
		result = append(result, infos...)
	}

	slog.InfoContext(ctx, "linkable templates listed",
		slog.String("action", "linkable_templates_list"),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
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
	scope, scopeName, err := extractScope(req.Msg.GetScope())
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
// ancestor scopes plus the current scope itself. The render set formula is:
// (mandatory AND enabled) UNION (enabled AND ref IN linkedRefs).
// Results are returned in org→folders→project order for correct CUE unification.
// If linkedRefs is nil, only mandatory+enabled templates are returned.
func (h *Handler) collectAncestorTemplates(ctx context.Context, scope consolev1.TemplateScope, scopeName string, linkedRefs []*consolev1.LinkedTemplateRef) ([]*consolev1.Template, error) {
	if h.walker == nil {
		return nil, nil
	}

	startNs, err := h.k8s.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}

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
		ancestorScope, ancestorName := scopeAndNameFromNs(h.resolver, ns.Name)
		if ancestorScope == consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED {
			continue
		}

		cms, err := h.k8s.ListTemplates(ctx, ancestorScope, ancestorName)
		if err != nil {
			slog.WarnContext(ctx, "failed to list templates from ancestor, skipping",
				slog.String("namespace", ns.Name),
				slog.Any("error", err),
			)
			continue
		}

		for _, cm := range cms {
			mandatory, _ := strconv.ParseBool(cm.Annotations[v1alpha2.AnnotationMandatory])
			enabled, _ := strconv.ParseBool(cm.Annotations[v1alpha2.AnnotationEnabled])
			if !enabled {
				continue
			}
			ref := linkedRef{
				scope:     scopeLabelValue(ancestorScope),
				scopeName: ancestorName,
				name:      cm.Name,
			}
			if !mandatory && !linkedSet[ref] {
				continue
			}
			tmplCopy := cm
			result = append(result, configMapToTemplate(&tmplCopy, ancestorScope, ancestorName))
		}
	}

	return result, nil
}

// checkLinkPermissions verifies the caller has the scoped link permissions
// required by the provided linked template references. If any ref targets an
// org-scope template, PermissionTemplatesLinkOrgWrite is checked. If any ref
// targets a folder-scope template, PermissionTemplatesLinkFolderWrite is checked.
// Both checks are performed at the template's owning scope.
func (h *Handler) checkLinkPermissions(ctx context.Context, claims *rpc.Claims, scope consolev1.TemplateScope, scopeName string, linkedTemplates []*consolev1.LinkedTemplateRef) error {
	hasOrg := false
	hasFolder := false
	for _, ref := range linkedTemplates {
		if ref == nil {
			continue
		}
		switch ref.GetScope() {
		case consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION:
			hasOrg = true
		case consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER:
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
func (h *Handler) checkAccess(ctx context.Context, claims *rpc.Claims, scope consolev1.TemplateScope, scopeName string, perm rbac.Permission) error {
	switch scope {
	case consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION:
		return h.checkOrgAccess(ctx, claims, scopeName, perm)
	case consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER:
		return h.checkFolderAccess(ctx, claims, scopeName, perm)
	case consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT:
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
	scope, scopeName, err := extractScope(req.Msg.GetScope())
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

	// Create the release ConfigMap. K8s returns AlreadyExists if the version
	// is duplicated (ConfigMap name is deterministic from template+version).
	cm, err := h.k8s.CreateRelease(ctx, scope, scopeName, templateName, version, release.CueTemplate, release.Defaults, release.Changelog, release.UpgradeAdvice)
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
		Release: configMapToRelease(cm, scope, scopeName),
	}), nil
}

// ListReleases returns all releases for a template, sorted by version descending.
func (h *Handler) ListReleases(
	ctx context.Context,
	req *connect.Request[consolev1.ListReleasesRequest],
) (*connect.Response[consolev1.ListReleasesResponse], error) {
	scope, scopeName, err := extractScope(req.Msg.GetScope())
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

	cms, err := h.k8s.ListReleases(ctx, scope, scopeName, templateName)
	if err != nil {
		return nil, mapK8sError(err)
	}

	releases := make([]*consolev1.Release, 0, len(cms))
	for _, cm := range cms {
		releases = append(releases, configMapToRelease(&cm, scope, scopeName))
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
	scope, scopeName, err := extractScope(req.Msg.GetScope())
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

	cm, err := h.k8s.GetRelease(ctx, scope, scopeName, templateName, version)
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
		Release: configMapToRelease(cm, scope, scopeName),
	}), nil
}

// CheckUpdates computes available version updates for all linked templates
// of a template (or all templates in a scope).
func (h *Handler) CheckUpdates(
	ctx context.Context,
	req *connect.Request[consolev1.CheckUpdatesRequest],
) (*connect.Response[consolev1.CheckUpdatesResponse], error) {
	scope, scopeName, err := extractScope(req.Msg.GetScope())
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
	var templates []corev1.ConfigMap
	if req.Msg.TemplateName != "" {
		cm, getErr := h.k8s.GetTemplate(ctx, scope, scopeName, req.Msg.TemplateName)
		if getErr != nil {
			return nil, mapK8sError(getErr)
		}
		templates = []corev1.ConfigMap{*cm}
	} else {
		cms, listErr := h.k8s.ListTemplates(ctx, scope, scopeName)
		if listErr != nil {
			return nil, mapK8sError(listErr)
		}
		templates = cms
	}

	var updates []*consolev1.TemplateUpdate
	for _, tmpl := range templates {
		raw, ok := tmpl.Annotations[v1alpha2.AnnotationLinkedTemplates]
		if !ok || raw == "" {
			continue
		}
		refs, err := unmarshalLinkedTemplates(raw)
		if err != nil {
			continue
		}
		for _, ref := range refs {
			update, err := h.checkLinkedUpdate(ctx, ref)
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
// reference. Returns nil if no updates are available.
func (h *Handler) checkLinkedUpdate(ctx context.Context, ref *consolev1.LinkedTemplateRef) (*consolev1.TemplateUpdate, error) {
	refScope := ref.GetScope()
	refScopeName := ref.ScopeName
	refName := ref.Name
	constraintStr := ref.VersionConstraint

	// List all release versions for the linked template.
	versions, err := h.k8s.ListReleaseVersions(ctx, refScope, refScopeName, refName)
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

	// Find the current pinned version. Without a stored resolved-version we
	// approximate with the oldest matching release so that any newer compatible
	// release is surfaced as an available update.
	// TODO(versioning): track the actually-resolved version per deployment so
	// currentVersion reflects the real pinned version, not an approximation.
	currentVersion := OldestMatchingVersion(versions, constraint)
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

	// Only report an update if there is something new.
	hasCompatibleUpdate := latestCompatibleStr != "" && latestCompatibleStr != currentStr
	if !hasCompatibleUpdate && !breakingAvailable {
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

// extractScope validates and extracts the scope and scope_name from a TemplateScopeRef.
func extractScope(ref *consolev1.TemplateScopeRef) (consolev1.TemplateScope, string, error) {
	if ref == nil {
		return 0, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope is required"))
	}
	if ref.Scope == consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED {
		return 0, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope must be specified"))
	}
	if ref.ScopeName == "" {
		return 0, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope.scope_name is required"))
	}
	return ref.Scope, ref.ScopeName, nil
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
