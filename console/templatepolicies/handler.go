// Package templatepolicies implements the TemplatePolicyService handler
// (HOL-556). Policies declaratively attach templates to projects via
// REQUIRE/EXCLUDE rules and replace the removed `mandatory` flag on Template
// and LinkableTemplate (HOL-554/HOL-555). The handler:
//
//   - Rejects project-scope storage outright so a project owner cannot tamper
//     with the very policy the platform means to constrain them with
//     (HOL-554 storage-isolation design note).
//   - Stores policies as ConfigMaps in the folder or organization namespace
//     with the console.holos.run/resource-type=template-policy label and a
//     JSON-serialized rules annotation.
//   - Enforces RBAC through the new TemplatePolicyCascadePerms table using the
//     PERMISSION_TEMPLATE_POLICIES_* permissions.
//
// Render-time integration (treating REQUIRE rules as the only source of forced
// templates) lands in HOL-557.
package templatepolicies

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"google.golang.org/protobuf/types/known/timestamppb"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "template-policy"

// dnsLabelRe validates policy names as DNS labels (mirrors console/templates).
var dnsLabelRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// TemplateExistsResolver reports whether a template exists at a given scope.
// The handler calls this as a best-effort check when a policy references a
// template so obviously broken policies (typos, wrong scope) fail fast; the
// check is advisory, and transient Kubernetes errors are logged but do not
// block the write.
//
// This interface lets the handler decouple from console/templates to avoid an
// import cycle (console/templates will depend on this package in HOL-557).
type TemplateExistsResolver interface {
	TemplateExists(ctx context.Context, scope consolev1.TemplateScope, scopeName, name string) (bool, error)
}

// Handler implements the TemplatePolicyService.
type Handler struct {
	consolev1connect.UnimplementedTemplatePolicyServiceHandler
	k8s                 *K8sClient
	resolver            *resolver.Resolver
	orgGrantResolver    OrgGrantResolver
	folderGrantResolver FolderGrantResolver
	templateResolver    TemplateExistsResolver
}

// NewHandler creates a TemplatePolicyService handler.
func NewHandler(k8s *K8sClient, r *resolver.Resolver) *Handler {
	return &Handler{k8s: k8s, resolver: r}
}

// WithOrgGrantResolver configures organization grant resolution.
func (h *Handler) WithOrgGrantResolver(ogr OrgGrantResolver) *Handler {
	h.orgGrantResolver = ogr
	return h
}

// WithFolderGrantResolver configures folder grant resolution.
func (h *Handler) WithFolderGrantResolver(fgr FolderGrantResolver) *Handler {
	h.folderGrantResolver = fgr
	return h
}

// WithTemplateExistsResolver configures best-effort template-existence checks
// used when validating a policy's rules.
func (h *Handler) WithTemplateExistsResolver(ter TemplateExistsResolver) *Handler {
	h.templateResolver = ter
	return h
}

// ListTemplatePolicies returns all policies visible in the given scope.
func (h *Handler) ListTemplatePolicies(
	ctx context.Context,
	req *connect.Request[consolev1.ListTemplatePoliciesRequest],
) (*connect.Response[consolev1.ListTemplatePoliciesResponse], error) {
	scope, scopeName, err := h.extractPolicyScope(req.Msg.GetScope())
	if err != nil {
		return nil, err
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatePoliciesList); err != nil {
		return nil, err
	}

	cms, err := h.k8s.ListPolicies(ctx, scope, scopeName)
	if err != nil {
		return nil, mapK8sError(err)
	}

	policies := make([]*consolev1.TemplatePolicy, 0, len(cms))
	for i := range cms {
		policies = append(policies, configMapToPolicy(&cms[i], scope, scopeName))
	}

	slog.InfoContext(ctx, "template policies listed",
		slog.String("action", "template_policy_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(policies)),
	)

	return connect.NewResponse(&consolev1.ListTemplatePoliciesResponse{Policies: policies}), nil
}

// GetTemplatePolicy returns a single policy by name.
func (h *Handler) GetTemplatePolicy(
	ctx context.Context,
	req *connect.Request[consolev1.GetTemplatePolicyRequest],
) (*connect.Response[consolev1.GetTemplatePolicyResponse], error) {
	scope, scopeName, err := h.extractPolicyScope(req.Msg.GetScope())
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

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatePoliciesRead); err != nil {
		return nil, err
	}

	cm, err := h.k8s.GetPolicy(ctx, scope, scopeName, name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template policy read",
		slog.String("action", "template_policy_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
	)

	return connect.NewResponse(&consolev1.GetTemplatePolicyResponse{
		Policy: configMapToPolicy(cm, scope, scopeName),
	}), nil
}

// CreateTemplatePolicy creates a new policy.
func (h *Handler) CreateTemplatePolicy(
	ctx context.Context,
	req *connect.Request[consolev1.CreateTemplatePolicyRequest],
) (*connect.Response[consolev1.CreateTemplatePolicyResponse], error) {
	scope, scopeName, err := h.extractPolicyScope(req.Msg.GetScope())
	if err != nil {
		return nil, err
	}
	policy := req.Msg.GetPolicy()
	if policy == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("policy is required"))
	}
	if err := validatePolicyName(policy.GetName()); err != nil {
		return nil, err
	}
	if err := validatePolicyRules(policy.GetRules()); err != nil {
		return nil, err
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatePoliciesWrite); err != nil {
		return nil, err
	}

	// Best-effort template-existence probe. Log but do not block on transient
	// errors so the control plane can still author a policy when the template
	// service is momentarily unavailable.
	h.probeReferencedTemplates(ctx, policy.GetRules())

	_, err = h.k8s.CreatePolicy(ctx, scope, scopeName, policy.GetName(), policy.GetDisplayName(), policy.GetDescription(), claims.Email, policy.GetRules())
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template policy created",
		slog.String("action", "template_policy_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", policy.GetName()),
		slog.Int("rules", len(policy.GetRules())),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateTemplatePolicyResponse{
		Name: policy.GetName(),
	}), nil
}

// UpdateTemplatePolicy updates an existing policy. Fields not set on the
// inbound policy are preserved — the handler distinguishes "clear display
// name" (non-nil empty string) from "no change" (the same field on the stored
// ConfigMap). Proto3 does not give us presence semantics on scalar fields, so
// display_name and description are *always* replaced with whatever the
// request carries; callers that want to preserve them must send them back
// verbatim. Rules are always replaced.
func (h *Handler) UpdateTemplatePolicy(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateTemplatePolicyRequest],
) (*connect.Response[consolev1.UpdateTemplatePolicyResponse], error) {
	scope, scopeName, err := h.extractPolicyScope(req.Msg.GetScope())
	if err != nil {
		return nil, err
	}
	policy := req.Msg.GetPolicy()
	if policy == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("policy is required"))
	}
	name := policy.GetName()
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	if err := validatePolicyRules(policy.GetRules()); err != nil {
		return nil, err
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatePoliciesWrite); err != nil {
		return nil, err
	}

	// Fetch the existing policy so we can distinguish "unset" from "set to
	// empty" for the top-level metadata fields. The previous read is also
	// required to surface NotFound before we attempt the Update (the K8s API
	// would otherwise return a less-informative error).
	existing, err := h.k8s.GetPolicy(ctx, scope, scopeName, name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Preserve unspecified fields. Proto3 scalars default to "" which we
	// intentionally treat as "no change" here so UI clients can send a
	// rules-only update without clobbering the display name and description.
	// A future API revision may introduce field masks for explicit clears.
	var displayName, description *string
	if policy.GetDisplayName() != "" {
		dn := policy.GetDisplayName()
		displayName = &dn
	} else if _, ok := existing.Annotations[v1alpha2.AnnotationDisplayName]; !ok {
		empty := ""
		displayName = &empty
	}
	if policy.GetDescription() != "" {
		d := policy.GetDescription()
		description = &d
	} else if _, ok := existing.Annotations[v1alpha2.AnnotationDescription]; !ok {
		empty := ""
		description = &empty
	}

	h.probeReferencedTemplates(ctx, policy.GetRules())

	_, err = h.k8s.UpdatePolicy(ctx, scope, scopeName, name, displayName, description, policy.GetRules(), true)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template policy updated",
		slog.String("action", "template_policy_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.Int("rules", len(policy.GetRules())),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateTemplatePolicyResponse{}), nil
}

// DeleteTemplatePolicy deletes a policy.
func (h *Handler) DeleteTemplatePolicy(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteTemplatePolicyRequest],
) (*connect.Response[consolev1.DeleteTemplatePolicyResponse], error) {
	scope, scopeName, err := h.extractPolicyScope(req.Msg.GetScope())
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

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatePoliciesDelete); err != nil {
		return nil, err
	}

	if err := h.k8s.DeletePolicy(ctx, scope, scopeName, name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template policy deleted",
		slog.String("action", "template_policy_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteTemplatePolicyResponse{}), nil
}

// extractPolicyScope validates a TemplateScopeRef for the TemplatePolicyService.
// Beyond the basic checks performed by the templates handler, this function
// also rejects TEMPLATE_SCOPE_PROJECT directly and reports the would-be
// project namespace in the error message so operators can debug misrouted
// clients. The same rejection applies on read and write so probing a project
// namespace cannot leak data.
func (h *Handler) extractPolicyScope(ref *consolev1.TemplateScopeRef) (consolev1.TemplateScope, string, error) {
	if ref == nil {
		return 0, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope is required"))
	}
	switch ref.GetScope() {
	case consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED:
		return 0, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope must be specified"))
	case consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT:
		// Derive the project namespace name for the error message from the
		// raw resolver prefixes. The k8s layer performs the authoritative
		// ResourceTypeFromNamespace check and will re-reject there; this is
		// the fast path so we can emit a precise diagnostic without a K8s
		// round trip.
		projectNs := h.resolver.NamespacePrefix + h.resolver.ProjectPrefix + ref.GetScopeName()
		return 0, "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("template policies cannot be stored in project namespace %q; use an organization or folder scope", projectNs))
	}
	if ref.GetScopeName() == "" {
		return 0, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope.scope_name is required"))
	}
	return ref.GetScope(), ref.GetScopeName(), nil
}

// validatePolicyName enforces DNS-label rules and the 63-character limit so
// the generated ConfigMap name is always valid Kubernetes.
func validatePolicyName(name string) error {
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

// validatePolicyRules enforces kind, template reference, and glob syntax
// invariants. Rules with invalid glob patterns are rejected early so an
// operator never commits a policy that would silently fail to match anything
// at render time.
func validatePolicyRules(rules []*consolev1.TemplatePolicyRule) error {
	if len(rules) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("policy must have at least one rule"))
	}
	for i, rule := range rules {
		if rule == nil {
			return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("rule %d: rule is required", i))
		}
		switch rule.GetKind() {
		case consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE,
			consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE:
		default:
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("rule %d: kind must be REQUIRE or EXCLUDE, got %v", i, rule.GetKind()))
		}
		tmpl := rule.GetTemplate()
		if tmpl == nil || tmpl.GetName() == "" {
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("rule %d: template.name is required", i))
		}
		if tmpl.GetScope() == consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED {
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("rule %d: template.scope is required", i))
		}
		if tmpl.GetScopeName() == "" {
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("rule %d: template.scope_name is required", i))
		}
		target := rule.GetTarget()
		if target == nil {
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("rule %d: target is required", i))
		}
		if target.GetProjectPattern() == "" {
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("rule %d: target.project_pattern is required (use \"*\" for all projects)", i))
		}
		if err := validateGlob(target.GetProjectPattern()); err != nil {
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("rule %d: invalid project_pattern: %w", i, err))
		}
		if target.GetDeploymentPattern() != "" {
			if err := validateGlob(target.GetDeploymentPattern()); err != nil {
				return connect.NewError(connect.CodeInvalidArgument,
					fmt.Errorf("rule %d: invalid deployment_pattern: %w", i, err))
			}
		}
	}
	return nil
}

// validateGlob confirms a glob pattern parses cleanly. filepath.Match returns
// ErrBadPattern for malformed globs; we perform a dry run against an empty
// string to surface that error without requiring a real subject.
func validateGlob(pattern string) error {
	if _, err := filepath.Match(pattern, ""); err != nil {
		return fmt.Errorf("%w: %s", err, pattern)
	}
	return nil
}

// probeReferencedTemplates performs a best-effort existence check for every
// template referenced by the policy. Per the acceptance criteria a transient
// failure is logged and ignored so the policy can still be written; only
// definitive "does not exist" signals are logged as warnings. The function
// intentionally does not return an error — enforcement happens at render time
// (HOL-557).
func (h *Handler) probeReferencedTemplates(ctx context.Context, rules []*consolev1.TemplatePolicyRule) {
	if h.templateResolver == nil {
		return
	}
	for i, rule := range rules {
		tmpl := rule.GetTemplate()
		if tmpl == nil {
			continue
		}
		exists, err := h.templateResolver.TemplateExists(ctx, tmpl.GetScope(), tmpl.GetScopeName(), tmpl.GetName())
		if err != nil {
			slog.WarnContext(ctx, "template existence probe failed; continuing",
				slog.Int("rule_index", i),
				slog.String("template_scope", tmpl.GetScope().String()),
				slog.String("template_scope_name", tmpl.GetScopeName()),
				slog.String("template_name", tmpl.GetName()),
				slog.Any("error", err),
			)
			continue
		}
		if !exists {
			slog.WarnContext(ctx, "policy references template that does not currently exist",
				slog.Int("rule_index", i),
				slog.String("template_scope", tmpl.GetScope().String()),
				slog.String("template_scope_name", tmpl.GetScopeName()),
				slog.String("template_name", tmpl.GetName()),
			)
		}
	}
}

// configMapToPolicy converts a stored ConfigMap into a TemplatePolicy proto.
// The caller supplies the authoritative scope/scopeName pair because the
// ConfigMap namespace alone is not enough to reconstruct the scope name in
// all cases (cluster installers may configure non-default prefixes).
func configMapToPolicy(cm *corev1.ConfigMap, scope consolev1.TemplateScope, scopeName string) *consolev1.TemplatePolicy {
	policy := &consolev1.TemplatePolicy{
		Name:         cm.Name,
		DisplayName:  cm.Annotations[v1alpha2.AnnotationDisplayName],
		Description:  cm.Annotations[v1alpha2.AnnotationDescription],
		CreatorEmail: cm.Annotations[v1alpha2.AnnotationCreatorEmail],
		ScopeRef: &consolev1.TemplateScopeRef{
			Scope:     scope,
			ScopeName: scopeName,
		},
		CreatedAt: timestamppb.New(cm.CreationTimestamp.Time),
	}
	if raw := cm.Annotations[v1alpha2.AnnotationTemplatePolicyRules]; raw != "" {
		rules, err := unmarshalRules(raw)
		if err != nil {
			slog.Warn("failed to parse template policy rules annotation",
				slog.String("name", cm.Name),
				slog.String("namespace", cm.Namespace),
				slog.Any("error", err),
			)
		} else {
			policy.Rules = rules
		}
	}
	return policy
}

// mapK8sError converts Kubernetes API errors to ConnectRPC errors. The
// function also recognises the package-level ProjectNamespaceError and maps
// it to CodeInvalidArgument so the handler does not have to duplicate the
// type switch at every call site.
func mapK8sError(err error) error {
	var pne *ProjectNamespaceError
	if errors.As(err, &pne) {
		return connect.NewError(connect.CodeInvalidArgument, pne)
	}
	if k8serrors.IsNotFound(err) {
		return connect.NewError(connect.CodeNotFound, err)
	}
	if k8serrors.IsAlreadyExists(err) {
		return connect.NewError(connect.CodeAlreadyExists, err)
	}
	if k8serrors.IsForbidden(err) {
		return connect.NewError(connect.CodePermissionDenied, err)
	}
	if k8serrors.IsUnauthorized(err) {
		return connect.NewError(connect.CodeUnauthenticated, err)
	}
	if k8serrors.IsBadRequest(err) {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
