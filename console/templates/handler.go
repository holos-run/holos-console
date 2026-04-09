// Package templates provides the TemplateService handler for project-scoped
// CUE deployment templates. This package implements the unified v1alpha2
// TemplateService for TEMPLATE_SCOPE_PROJECT templates. Phase 9 will extend
// this to handle org- and folder-scoped templates in a single handler.
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

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "template"

// dnsLabelRe validates template names as DNS labels.
var dnsLabelRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// ProjectResolver resolves project namespace grants for access checks.
type ProjectResolver interface {
	GetProjectGrants(ctx context.Context, project string) (shareUsers, shareRoles map[string]string, err error)
}

// OrgResolver resolves the organization for a project.
type OrgResolver interface {
	GetProjectOrganization(ctx context.Context, project string) (string, error)
}

// OrgTemplateLister lists linkable platform templates for an organization.
// Satisfied structurally by org_templates.K8sClient.
type OrgTemplateLister interface {
	ListLinkableOrgTemplateInfos(ctx context.Context, org string) ([]*consolev1.LinkableTemplate, error)
}

// RenderResource is a single rendered resource with its YAML representation
// and its raw object data for JSON serialization.
type RenderResource struct {
	YAML   string
	Object map[string]any
}

// Renderer evaluates a CUE template unified with platform and user CUE input strings
// and returns a list of rendered Kubernetes manifests with both YAML and structured
// object data.  cuePlatformInput carries trusted backend values (project, namespace,
// claims); cueInput carries user-provided deployment parameters.
type Renderer interface {
	Render(ctx context.Context, cueTemplate string, cuePlatformInput string, cueInput string) ([]RenderResource, error)
	// RenderWithOrgTemplateSources evaluates the deployment template unified with
	// zero or more platform template CUE sources, then with the CUE input.
	// Used by the preview RPC when linked_org_templates is supplied.
	RenderWithOrgTemplateSources(ctx context.Context, cueTemplate string, orgTemplateSources []string, cuePlatformInput string, cueInput string) ([]RenderResource, error)
}

// Handler implements the TemplateService (stub — phase 9 will fill in the
// full implementation against the unified v1alpha2 TemplateService).
type Handler struct {
	consolev1connect.UnimplementedTemplateServiceHandler
	k8s              *K8sClient
	projectResolver  ProjectResolver
	renderer         Renderer
	orgResolver      OrgResolver
	orgTemplateLister OrgTemplateLister
}

// NewHandler creates a TemplateService handler stub.
func NewHandler(k8s *K8sClient, projectResolver ProjectResolver, renderer Renderer) *Handler {
	return &Handler{k8s: k8s, projectResolver: projectResolver, renderer: renderer}
}

// WithOrgResolver configures the handler with an OrgResolver for resolving
// the project's organization.
func (h *Handler) WithOrgResolver(or OrgResolver) *Handler {
	h.orgResolver = or
	return h
}

// WithOrgTemplateLister configures the handler with an OrgTemplateLister for
// listing linkable platform templates.
func (h *Handler) WithOrgTemplateLister(l OrgTemplateLister) *Handler {
	h.orgTemplateLister = l
	return h
}

// ListTemplates returns all templates in the given scope (project only in this phase).
func (h *Handler) ListTemplates(
	ctx context.Context,
	req *connect.Request[consolev1.ListTemplatesRequest],
) (*connect.Response[consolev1.ListTemplatesResponse], error) {
	scope := req.Msg.GetScope()
	if scope == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope is required"))
	}
	project := scope.ScopeName
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope.scope_name (project) is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionTemplatesList); err != nil {
		return nil, err
	}

	cms, err := h.k8s.ListTemplates(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}

	templates := make([]*consolev1.Template, 0, len(cms))
	for _, cm := range cms {
		templates = append(templates, configMapToTemplate(&cm, project))
	}

	slog.InfoContext(ctx, "templates listed",
		slog.String("action", "templates_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
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
	scope := req.Msg.GetScope()
	if scope == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope is required"))
	}
	project := scope.ScopeName
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope.scope_name (project) is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionTemplatesRead); err != nil {
		return nil, err
	}

	cm, err := h.k8s.GetTemplate(ctx, project, name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template read",
		slog.String("action", "template_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
	)

	return connect.NewResponse(&consolev1.GetTemplateResponse{
		Template: configMapToTemplate(cm, project),
	}), nil
}

// CreateTemplate creates a new template.
func (h *Handler) CreateTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.CreateTemplateRequest],
) (*connect.Response[consolev1.CreateTemplateResponse], error) {
	scope := req.Msg.GetScope()
	if scope == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope is required"))
	}
	project := scope.ScopeName
	tmpl := req.Msg.GetTemplate()
	if tmpl == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("template is required"))
	}
	name := tmpl.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope.scope_name (project) is required"))
	}
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

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionTemplatesWrite); err != nil {
		return nil, err
	}

	// Extract linked org template names from LinkedTemplates.
	var linkedOrgTemplates []string
	for _, ref := range tmpl.LinkedTemplates {
		if ref.Scope == consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION && ref.Name != "" {
			linkedOrgTemplates = append(linkedOrgTemplates, ref.Name)
		}
	}

	_, err := h.k8s.CreateTemplate(ctx, project, name, tmpl.DisplayName, tmpl.Description, tmpl.CueTemplate, tmpl.Defaults, linkedOrgTemplates)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template created",
		slog.String("action", "template_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
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
	scope := req.Msg.GetScope()
	if scope == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope is required"))
	}
	project := scope.ScopeName
	tmpl := req.Msg.GetTemplate()
	if tmpl == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("template is required"))
	}
	name := tmpl.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope.scope_name (project) is required"))
	}
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

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionTemplatesWrite); err != nil {
		return nil, err
	}

	// Extract linked org template names from LinkedTemplates.
	var linkedOrgTemplates []string
	for _, ref := range tmpl.LinkedTemplates {
		if ref.Scope == consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION && ref.Name != "" {
			linkedOrgTemplates = append(linkedOrgTemplates, ref.Name)
		}
	}

	displayName := tmpl.DisplayName
	description := tmpl.Description
	cueTemplate := tmpl.CueTemplate
	_, err := h.k8s.UpdateTemplate(ctx, project, name, &displayName, &description, &cueTemplate, tmpl.Defaults, false, linkedOrgTemplates)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template updated",
		slog.String("action", "template_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
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
	scope := req.Msg.GetScope()
	if scope == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope is required"))
	}
	project := scope.ScopeName
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope.scope_name (project) is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionTemplatesDelete); err != nil {
		return nil, err
	}

	if err := h.k8s.DeleteTemplate(ctx, project, name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template deleted",
		slog.String("action", "template_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
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
	scope := req.Msg.GetScope()
	if scope == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope is required"))
	}
	project := scope.ScopeName
	sourceName := req.Msg.SourceName
	newName := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope.scope_name (project) is required"))
	}
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

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionTemplatesWrite); err != nil {
		return nil, err
	}

	_, err := h.k8s.CloneTemplate(ctx, project, sourceName, newName, req.Msg.DisplayName)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template cloned",
		slog.String("action", "template_clone"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
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

// ListLinkableTemplates returns the set of enabled org templates for the
// project's organization that a deployment template may link against (ADR 019).
func (h *Handler) ListLinkableTemplates(
	ctx context.Context,
	req *connect.Request[consolev1.ListLinkableTemplatesRequest],
) (*connect.Response[consolev1.ListLinkableTemplatesResponse], error) {
	scope := req.Msg.GetScope()
	if scope == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope is required"))
	}
	project := scope.ScopeName
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope.scope_name (project) is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionTemplatesList); err != nil {
		return nil, err
	}

	if h.orgResolver == nil || h.orgTemplateLister == nil {
		return connect.NewResponse(&consolev1.ListLinkableTemplatesResponse{}), nil
	}

	org, err := h.orgResolver.GetProjectOrganization(ctx, project)
	if err != nil || org == "" {
		return connect.NewResponse(&consolev1.ListLinkableTemplatesResponse{}), nil
	}

	templates, err := h.orgTemplateLister.ListLinkableOrgTemplateInfos(ctx, org)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("listing linkable platform templates: %w", err))
	}

	slog.InfoContext(ctx, "linkable platform templates listed",
		slog.String("action", "linkable_templates_list"),
		slog.String("project", project),
		slog.String("org", org),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(templates)),
	)

	return connect.NewResponse(&consolev1.ListLinkableTemplatesResponse{
		Templates: templates,
	}), nil
}

// checkProjectAccess verifies that the user has the given permission via project cascade grants.
func (h *Handler) checkProjectAccess(ctx context.Context, claims *rpc.Claims, project string, permission rbac.Permission) error {
	if h.projectResolver == nil {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	users, roles, err := h.projectResolver.GetProjectGrants(ctx, project)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve project grants",
			slog.String("project", project),
			slog.Any("error", err),
		)
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	return rbac.CheckCascadeAccess(claims.Email, claims.Roles, users, roles, permission, rbac.ProjectCascadeTemplatePerms)
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

// configMapToTemplate converts a Kubernetes ConfigMap to a Template protobuf message.
//
// Defaults are populated in priority order:
//  1. CUE extraction — reads the `defaults` field from the template CUE source.
//     This is the canonical approach for templates authored using the ADR 018 pattern.
//  2. Annotation fallback — reads DefaultsKey from ConfigMap data. Used for templates
//     that predate ADR 018 and store defaults as JSON in a ConfigMap annotation.
//
// If CUE extraction succeeds and returns non-nil defaults, the annotation fallback
// is skipped. If CUE extraction fails or the template has no `defaults` block, the
// annotation fallback is attempted. If both are absent, Defaults is left nil.
func configMapToTemplate(cm *corev1.ConfigMap, project string) *consolev1.Template {
	cueSource := cm.Data[CueTemplateKey]
	tmpl := &consolev1.Template{
		Name:        cm.Name,
		DisplayName: cm.Annotations[v1alpha1.AnnotationDisplayName],
		Description: cm.Annotations[v1alpha1.AnnotationDescription],
		CueTemplate: cueSource,
		ScopeRef: &consolev1.TemplateScopeRef{
			Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
			ScopeName: project,
		},
	}

	// Populate linked templates from annotation (ADR 019).
	if raw, ok := cm.Annotations[v1alpha1.AnnotationLinkedOrgTemplates]; ok && raw != "" {
		var linked []string
		if err := json.Unmarshal([]byte(raw), &linked); err == nil {
			refs := make([]*consolev1.LinkedTemplateRef, 0, len(linked))
			for _, name := range linked {
				refs = append(refs, &consolev1.LinkedTemplateRef{
					Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
					Name:  name,
				})
			}
			tmpl.LinkedTemplates = refs
		} else {
			slog.Warn("failed to parse linked-org-templates annotation",
				slog.String("name", cm.Name),
				slog.String("namespace", cm.Namespace),
				slog.Any("error", err),
			)
		}
	}

	// Priority 1: CUE extraction from the template source.
	if cueSource != "" {
		extracted, err := ExtractDefaults(cueSource)
		if err != nil {
			slog.Warn("failed to extract defaults from CUE template; falling back to annotation",
				slog.String("name", cm.Name),
				slog.String("namespace", cm.Namespace),
				slog.Any("error", err),
			)
		} else if extracted != nil {
			tmpl.Defaults = extracted
			return tmpl
		}
	}

	// Priority 2: Annotation fallback for pre-ADR 018 templates.
	if rawJSON, ok := cm.Data[DefaultsKey]; ok && rawJSON != "" {
		var defaults consolev1.TemplateDefaults
		if err := json.Unmarshal([]byte(rawJSON), &defaults); err == nil {
			tmpl.Defaults = &defaults
		} else {
			slog.Warn("failed to deserialize template defaults from ConfigMap",
				slog.String("name", cm.Name),
				slog.String("namespace", cm.Namespace),
				slog.Any("error", err),
			)
		}
	}
	return tmpl
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
