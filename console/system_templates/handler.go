// Package system_templates implements the SystemTemplateService RPC handler.
// Platform templates (code: SystemTemplate) are org-scoped CUE templates stored
// in org namespace ConfigMaps. They differ from deployment templates in that they
// can be marked mandatory, causing them to be automatically applied to project
// namespaces at creation time.
package system_templates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"connectrpc.com/connect"
	"cuelang.org/go/cue/parser"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "system-template"

// dnsLabelRe validates template names as DNS labels.
var dnsLabelRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// OrgResolver resolves organization-level grants for access checks.
type OrgResolver interface {
	GetOrgGrants(ctx context.Context, org string) (users, roles map[string]string, err error)
}

// RenderResource is a single rendered resource with its YAML representation
// and its raw object data for JSON serialization.
type RenderResource struct {
	YAML   string
	Object map[string]any
}

// Renderer evaluates a CUE template unified with system and user CUE input strings
// and returns a list of rendered Kubernetes manifests.
type Renderer interface {
	Render(ctx context.Context, cueTemplate string, cueSystemInput string, cueInput string) ([]RenderResource, error)
}

// Handler implements the SystemTemplateService.
type Handler struct {
	consolev1connect.UnimplementedOrgTemplateServiceHandler
	k8s         *K8sClient
	orgResolver OrgResolver
	renderer    Renderer
}

// NewHandler creates a SystemTemplateService handler.
func NewHandler(k8s *K8sClient, orgResolver OrgResolver, renderer Renderer) *Handler {
	return &Handler{k8s: k8s, orgResolver: orgResolver, renderer: renderer}
}

// ListSystemTemplates returns all platform templates in an org, seeding defaults on first access.
func (h *Handler) ListOrgTemplates(
	ctx context.Context,
	req *connect.Request[consolev1.ListOrgTemplatesRequest],
) (*connect.Response[consolev1.ListOrgTemplatesResponse], error) {
	org := req.Msg.Org
	if org == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("org is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkOrgReadAccess(ctx, claims, org); err != nil {
		return nil, err
	}

	cms, err := h.k8s.ListSystemTemplates(ctx, org)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Seed built-in templates on first access (no existing templates found).
	if len(cms) == 0 {
		if seedErr := h.k8s.SeedDefaultTemplates(ctx, org); seedErr != nil {
			// Log but don't fail: seeding is best-effort; the org namespace may not exist yet.
			slog.WarnContext(ctx, "failed to seed default platform templates",
				slog.String("org", org),
				slog.Any("error", seedErr),
			)
		} else {
			// Re-list after seeding.
			cms, err = h.k8s.ListSystemTemplates(ctx, org)
			if err != nil {
				return nil, mapK8sError(err)
			}
		}
	}

	templates := make([]*consolev1.OrgTemplate, 0, len(cms))
	for _, cm := range cms {
		templates = append(templates, configMapToSystemTemplate(&cm, org))
	}

	slog.InfoContext(ctx, "platform templates listed",
		slog.String("action", "system_templates_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("org", org),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(templates)),
	)

	return connect.NewResponse(&consolev1.ListOrgTemplatesResponse{
		Templates: templates,
	}), nil
}

// GetSystemTemplate returns a single platform template by name.
func (h *Handler) GetOrgTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.GetOrgTemplateRequest],
) (*connect.Response[consolev1.GetOrgTemplateResponse], error) {
	org := req.Msg.Org
	name := req.Msg.Name
	if org == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("org is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkOrgReadAccess(ctx, claims, org); err != nil {
		return nil, err
	}

	cm, err := h.k8s.GetSystemTemplate(ctx, org, name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "platform template read",
		slog.String("action", "system_template_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("org", org),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
	)

	return connect.NewResponse(&consolev1.GetOrgTemplateResponse{
		Template: configMapToSystemTemplate(cm, org),
	}), nil
}

// CreateSystemTemplate creates a new platform template.
func (h *Handler) CreateOrgTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.CreateOrgTemplateRequest],
) (*connect.Response[consolev1.CreateOrgTemplateResponse], error) {
	org := req.Msg.Org
	name := req.Msg.Name
	if org == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("org is required"))
	}
	if err := validateTemplateName(name); err != nil {
		return nil, err
	}
	if err := validateCueSyntax(req.Msg.CueTemplate); err != nil {
		return nil, err
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkOrgEditAccess(ctx, claims, org); err != nil {
		return nil, err
	}

	_, err := h.k8s.CreateSystemTemplate(ctx, org, name, req.Msg.DisplayName, req.Msg.Description, req.Msg.CueTemplate, req.Msg.Mandatory, req.Msg.Enabled)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "platform template created",
		slog.String("action", "system_template_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("org", org),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateOrgTemplateResponse{
		Name: name,
	}), nil
}

// UpdateSystemTemplate updates an existing platform template.
func (h *Handler) UpdateOrgTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateOrgTemplateRequest],
) (*connect.Response[consolev1.UpdateOrgTemplateResponse], error) {
	org := req.Msg.Org
	name := req.Msg.Name
	if org == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("org is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	// Validate CUE syntax if a new template is provided.
	if req.Msg.CueTemplate != nil {
		if err := validateCueSyntax(*req.Msg.CueTemplate); err != nil {
			return nil, err
		}
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkOrgEditAccess(ctx, claims, org); err != nil {
		return nil, err
	}

	_, err := h.k8s.UpdateSystemTemplate(ctx, org, name, req.Msg.DisplayName, req.Msg.Description, req.Msg.CueTemplate, req.Msg.Mandatory, req.Msg.Enabled)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "platform template updated",
		slog.String("action", "system_template_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("org", org),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateOrgTemplateResponse{}), nil
}

// DeleteSystemTemplate deletes a platform template.
func (h *Handler) DeleteOrgTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteOrgTemplateRequest],
) (*connect.Response[consolev1.DeleteOrgTemplateResponse], error) {
	org := req.Msg.Org
	name := req.Msg.Name
	if org == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("org is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkOrgEditAccess(ctx, claims, org); err != nil {
		return nil, err
	}

	if err := h.k8s.DeleteSystemTemplate(ctx, org, name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "platform template deleted",
		slog.String("action", "system_template_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("org", org),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteOrgTemplateResponse{}), nil
}

// CloneSystemTemplate copies an existing platform template to a new name.
func (h *Handler) CloneOrgTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.CloneOrgTemplateRequest],
) (*connect.Response[consolev1.CloneOrgTemplateResponse], error) {
	org := req.Msg.Org
	sourceName := req.Msg.SourceName
	newName := req.Msg.Name
	if org == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("org is required"))
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

	if err := h.checkOrgEditAccess(ctx, claims, org); err != nil {
		return nil, err
	}

	_, err := h.k8s.CloneSystemTemplate(ctx, org, sourceName, newName, req.Msg.DisplayName)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "platform template cloned",
		slog.String("action", "system_template_clone"),
		slog.String("resource_type", auditResourceType),
		slog.String("org", org),
		slog.String("source_name", sourceName),
		slog.String("name", newName),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CloneOrgTemplateResponse{
		Name: newName,
	}), nil
}

// RenderSystemTemplate evaluates a CUE platform template and returns rendered manifests.
func (h *Handler) RenderOrgTemplate(
	ctx context.Context,
	req *connect.Request[consolev1.RenderOrgTemplateRequest],
) (*connect.Response[consolev1.RenderOrgTemplateResponse], error) {
	if req.Msg.CueTemplate == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("cue_template is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	resources, err := h.renderer.Render(ctx, req.Msg.CueTemplate, req.Msg.CuePlatformInput, req.Msg.CueInput)
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

	return connect.NewResponse(&consolev1.RenderOrgTemplateResponse{
		RenderedYaml: buf.String(),
		RenderedJson: string(jsonBytes),
	}), nil
}

// checkOrgReadAccess verifies the user can read platform templates in the org.
// Requires org-level grants (VIEWER or above).
func (h *Handler) checkOrgReadAccess(ctx context.Context, claims *rpc.Claims, org string) error {
	if h.orgResolver == nil {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	users, roles, err := h.orgResolver.GetOrgGrants(ctx, org)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve org grants",
			slog.String("org", org),
			slog.Any("error", err),
		)
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	return rbac.CheckAccessGrants(claims.Email, claims.Roles, users, roles, rbac.PermissionOrganizationsRead)
}

// checkOrgEditAccess verifies the user has PERMISSION_SYSTEM_DEPLOYMENTS_EDIT
// at the org level via the OrgCascadeSystemTemplatePerms cascade table.
func (h *Handler) checkOrgEditAccess(ctx context.Context, claims *rpc.Claims, org string) error {
	if h.orgResolver == nil {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	users, roles, err := h.orgResolver.GetOrgGrants(ctx, org)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve org grants",
			slog.String("org", org),
			slog.Any("error", err),
		)
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	return rbac.CheckCascadeAccess(claims.Email, claims.Roles, users, roles, rbac.PermissionOrgTemplatesWrite, rbac.OrgCascadeSystemTemplatePerms)
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
