// Package org_templates provides the TemplateService handler for org-scoped
// (platform) CUE templates, plus K8s backend operations and the
// MandatoryTemplateApplier used during project creation. This package is being
// migrated to v1alpha2 as part of phase 9 (unified TemplateService). Until
// that migration is complete the handler stubs all RPCs as Unimplemented — the
// K8s backend (k8s.go) and template applier (apply.go) remain functional.
package org_templates

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	"connectrpc.com/connect"
	"cuelang.org/go/cue/parser"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "org-template"

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

// Renderer evaluates a CUE template unified with platform and project CUE
// input strings and returns a list of rendered Kubernetes manifests.
type Renderer interface {
	Render(ctx context.Context, cueTemplate string, cuePlatformInput string, cueInput string) ([]RenderResource, error)
}

// Handler implements the TemplateService (stub — phase 9 will fill in the
// full implementation against the unified v1alpha2 TemplateService).
type Handler struct {
	consolev1connect.UnimplementedTemplateServiceHandler
	k8s         *K8sClient
	orgResolver OrgResolver
	renderer    Renderer
}

// NewHandler creates a TemplateService handler stub for org-scoped templates.
func NewHandler(k8s *K8sClient, orgResolver OrgResolver, renderer Renderer) *Handler {
	return &Handler{k8s: k8s, orgResolver: orgResolver, renderer: renderer}
}

// checkOrgReadAccess verifies the user has read access at the org level.
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

// checkOrgEditAccess verifies the user has PERMISSION_TEMPLATES_WRITE
// at the org level via the OrgCascadeTemplatePerms cascade table.
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
	return rbac.CheckCascadeAccess(claims.Email, claims.Roles, users, roles, rbac.PermissionTemplatesWrite, rbac.OrgCascadeTemplatePerms)
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
