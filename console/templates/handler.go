// Package templates provides the TemplateService handler for project-scoped
// CUE deployment templates. This package is being migrated to v1alpha2 as part
// of phase 9 (unified TemplateService). Until that migration is complete, the
// handler stubs all RPCs as Unimplemented — the K8s backend (k8s.go) and
// defaults extraction (defaults.go) remain functional for use by the
// deployments package.
package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"

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

// OrgTemplateLister lists platform templates for an organization.
// Satisfied structurally by org_templates.K8sClient.
type OrgTemplateLister interface {
	ListLinkableOrgTemplateInfos(ctx context.Context, org string) ([]*consolev1.Template, error)
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
