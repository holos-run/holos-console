// Package templatepolicies implements the TemplatePolicyService handler
// (HOL-556). Policies declaratively attach templates to projects via
// REQUIRE/EXCLUDE rules and replace the removed `mandatory` flag on Template
// and LinkableTemplate (HOL-554/HOL-555). The handler:
//
//   - Rejects project-scope storage outright so a project owner cannot tamper
//     with the very policy the platform means to constrain them with. See
//     storage-isolation guardrail below.
//   - Stores policies as ConfigMaps in the folder or organization namespace
//     with the console.holos.run/resource-type=template-policy label and a
//     JSON-serialized rules annotation.
//   - Enforces RBAC through the TemplatePolicyCascadePerms table using the
//     PERMISSION_TEMPLATE_POLICIES_* permissions, so WRITE/DELETE/ADMIN can
//     only be granted at organization or folder scope.
//
// # Storage-isolation guardrail (HOL-554)
//
// TemplatePolicy ConfigMaps and any applied-render-set state live exclusively
// in folder or organization namespaces. They must NEVER be stored in a
// project namespace because project owners have namespace-scoped write access
// to their project namespace and could otherwise tamper with the very policy
// the platform means to constrain them with. Storage-isolation is enforced
// at three layers:
//
//  1. Proto contract: CreateTemplatePolicyRequest and UpdateTemplatePolicyRequest
//     both carry a TemplateScopeRef; validatePolicyScopeRef rejects
//     TEMPLATE_SCOPE_PROJECT directly.
//  2. Handler validation: extractPolicyScope rejects project scope with a
//     specific error message naming the project namespace.
//  3. K8s client: namespaceForScope in k8s.go re-derives the namespace via
//     the resolver and asserts it does not classify as a project namespace,
//     catching any bug that routed a project scope through validation.
//
// Render-time integration (treating REQUIRE rules as the only source of
// forced templates) is tracked by HOL-557. Until that lands, the existing
// annotation-driven `mandatory` flag on Template ConfigMaps continues to
// drive auto-inclusion at render time; see console/templates/k8s.go.
package templatepolicies

import (
	"context"
	"encoding/json"
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

// ResourceTopologyResolver enumerates the render-target resources (project
// namespaces plus their ProjectTemplate and Deployment ConfigMaps) that live
// under a TemplatePolicy's owning scope. The handler uses it to enforce the
// HOL-570 "EXCLUDE cannot contradict an explicit link" guardrail at policy
// authoring time: every EXCLUDE rule is checked against the
// `console.holos.run/linked-templates` annotation of every candidate target,
// so the operator learns immediately that the rule they just wrote cannot
// do what they expect.
//
// Methods are intentionally narrow so the handler never reaches into the
// cluster directly — that keeps both the import graph and the test seam
// simple. A nil ResourceTopologyResolver disables the guardrail (so unit
// tests that never exercise EXCLUDE rules do not need to stub it).
//
// ListProjectsUnderScope returns project namespaces whose ancestor chain
// passes through the namespace that owns the policy. Implementations MUST
// filter by DeletionTimestamp and MUST NOT surface a folder or organization
// namespace. When the scope is an organization, every project in that
// organization is reachable regardless of which folder contains it.
//
// ListProjectTemplates returns Template ConfigMaps stored in the given
// project namespace; only project-scope Template resources are considered
// candidate EXCLUDE targets (org and folder-scope templates are injected by
// REQUIRE rules, never owned by a project).
//
// ListProjectDeployments returns Deployment ConfigMaps stored in the given
// project namespace. Both lists are filtered by the standard
// `console.holos.run/resource-type` label selectors so unmanaged ConfigMaps
// do not appear as candidate targets.
type ResourceTopologyResolver interface {
	ListProjectsUnderScope(ctx context.Context, scope consolev1.TemplateScope, scopeName string) ([]*corev1.Namespace, error)
	ListProjectTemplates(ctx context.Context, projectNs string) ([]corev1.ConfigMap, error)
	ListProjectDeployments(ctx context.Context, projectNs string) ([]corev1.ConfigMap, error)
}

// Handler implements the TemplatePolicyService.
type Handler struct {
	consolev1connect.UnimplementedTemplatePolicyServiceHandler
	k8s                 *K8sClient
	resolver            *resolver.Resolver
	orgGrantResolver    OrgGrantResolver
	folderGrantResolver FolderGrantResolver
	templateResolver    TemplateExistsResolver
	topologyResolver    ResourceTopologyResolver
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

// WithResourceTopologyResolver configures the enumerator used by the HOL-570
// EXCLUDE-vs-explicit-link guardrail. Leaving it unset keeps the guardrail
// off (every EXCLUDE is accepted), which is the safe default for unit tests
// that never author EXCLUDE rules but would otherwise need to stub the
// resolver just to exercise the Create/Update paths.
func (h *Handler) WithResourceTopologyResolver(r ResourceTopologyResolver) *Handler {
	h.topologyResolver = r
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

	// HOL-560: audit shape harmonized with console/folders/handler.go. List and
	// read paths include the caller email alongside sub so security review can
	// pivot on either identifier.
	slog.InfoContext(ctx, "template policies listed",
		slog.String("action", "template_policy_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
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
		slog.String("email", claims.Email),
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
	if err := validatePolicyScopeRef(policy.GetScopeRef(), scope, scopeName); err != nil {
		return nil, err
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

	// HOL-570: EXCLUDE rules are rejected when they would contradict an
	// explicit `console.holos.run/linked-templates` annotation already set on
	// a matching ProjectTemplate or Deployment. Runs AFTER checkAccess so an
	// unauthorized caller cannot use the guardrail's "conflict with
	// lilies/web" error as a probe oracle that reveals which project
	// explicitly links which template (information disclosure to someone
	// without policy-write access). Runs BEFORE CreatePolicy so the
	// rejection short-circuits the Kubernetes write.
	if err := h.validateExcludeRulesAgainstExplicitLinks(ctx, scope, scopeName, policy.GetRules()); err != nil {
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
	if err := validatePolicyScopeRef(policy.GetScopeRef(), scope, scopeName); err != nil {
		return nil, err
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

	// HOL-570: reject EXCLUDE rules that would contradict explicit links.
	// Runs AFTER checkAccess to avoid leaking "template X is linked on
	// project Y" to an unauthorized caller (see the matching comment in
	// CreateTemplatePolicy) and BEFORE GetPolicy + UpdatePolicy so a
	// rejected call never touches the stored ConfigMap.
	if err := h.validateExcludeRulesAgainstExplicitLinks(ctx, scope, scopeName, policy.GetRules()); err != nil {
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

// validatePolicyScopeRef enforces the proto contract that every
// TemplatePolicy carries a scope_ref and that it exactly matches the outer
// request scope. The proto comment on CreateTemplatePolicyRequest.policy
// states scope_ref is required; silently accepting a nil or mismatched
// scope_ref would let a client believe a policy was stored at one scope when
// it was actually stored at another. Project scope is rejected here too so
// the storage-isolation guardrail (HOL-554) holds at the proto boundary and
// not only via the downstream namespace check.
func validatePolicyScopeRef(ref *consolev1.TemplateScopeRef, reqScope consolev1.TemplateScope, reqScopeName string) error {
	if ref == nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("policy.scope_ref is required"))
	}
	if ref.GetScope() == consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED || ref.GetScopeName() == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("policy.scope_ref.scope and policy.scope_ref.scope_name are required"))
	}
	if ref.GetScope() == consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("policy.scope_ref cannot be TEMPLATE_SCOPE_PROJECT; template policies must live at folder or organization scope"))
	}
	if ref.GetScope() != reqScope || ref.GetScopeName() != reqScopeName {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("policy.scope_ref (%s/%s) must match request scope (%s/%s)",
				ref.GetScope(), ref.GetScopeName(), reqScope, reqScopeName))
	}
	return nil
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

// validateExcludeRulesAgainstExplicitLinks enforces the HOL-570 guardrail:
// an EXCLUDE rule cannot reference a template that any existing ProjectTemplate
// or Deployment *explicitly* linked via the
// `console.holos.run/linked-templates` annotation. At render time, the
// policy resolver already ignores EXCLUDE against owner-linked refs
// (console/policyresolver/folder_resolver.go), so the subtraction would
// silently be a no-op. Failing fast at policy-authoring time tells the
// platform engineer that the rule they just wrote cannot do what they
// expect; they can either narrow the rule's patterns or ask the resource
// owner to unlink the template.
//
// Scope of the check: the rule's (project_pattern, deployment_pattern) is
// evaluated against every project namespace that lives under the policy's
// owning scope (ancestor chain passes through the folder or organization
// namespace). Within each matching project, ProjectTemplates and Deployments
// that match the rule's deployment_pattern are inspected for the EXCLUDE
// template in their LinkedTemplates annotation. The first offense per rule
// short-circuits validation with the rule index, so a policy with multiple
// EXCLUDEs surfaces one conflict at a time.
//
// An empty scope (no existing candidate resources) accepts any EXCLUDE rule.
// A resource owner who later links the excluded template is an operator
// problem, not a policy-author problem — the guardrail fires only against
// the state visible to the author at the moment they submit the rule. When
// the handler is constructed without a topologyResolver, the guardrail is
// disabled (unit tests that never exercise EXCLUDE rules can skip the
// wiring). REQUIRE rules are unaffected.
func (h *Handler) validateExcludeRulesAgainstExplicitLinks(
	ctx context.Context,
	scope consolev1.TemplateScope,
	scopeName string,
	rules []*consolev1.TemplatePolicyRule,
) error {
	if h.topologyResolver == nil {
		return nil
	}
	// Fast path: no EXCLUDE rules means nothing to validate. Avoids a
	// potentially expensive namespace enumeration for the common REQUIRE-only
	// policy.
	hasExclude := false
	for _, rule := range rules {
		if rule != nil && rule.GetKind() == consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE {
			hasExclude = true
			break
		}
	}
	if !hasExclude {
		return nil
	}

	projects, err := h.topologyResolver.ListProjectsUnderScope(ctx, scope, scopeName)
	if err != nil {
		return connect.NewError(connect.CodeInternal,
			fmt.Errorf("enumerating projects under scope %s/%s: %w", scope, scopeName, err))
	}
	if len(projects) == 0 {
		return nil
	}

	for i, rule := range rules {
		if rule == nil || rule.GetKind() != consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE {
			continue
		}
		tmpl := rule.GetTemplate()
		target := rule.GetTarget()
		if tmpl == nil || target == nil {
			continue // validatePolicyRules already rejected this shape
		}
		projectPattern := target.GetProjectPattern()
		deploymentPattern := target.GetDeploymentPattern()

		for _, projectNs := range projects {
			projectSlug, err := h.resolver.ProjectFromNamespace(projectNs.Name)
			if err != nil {
				// Fall back to the raw namespace name if the resolver can't
				// strip the prefix — matching still works on the
				// fully-qualified name in that degenerate case.
				projectSlug = projectNs.Name
			}
			matched, err := filepath.Match(projectPattern, projectSlug)
			if err != nil || !matched {
				continue
			}

			offender, err := h.findExplicitLinkOffender(ctx, projectNs.Name, projectSlug, deploymentPattern, tmpl)
			if err != nil {
				return connect.NewError(connect.CodeInternal,
					fmt.Errorf("rule %d: listing candidate targets in %q: %w", i, projectNs.Name, err))
			}
			if offender != "" {
				return connect.NewError(connect.CodeFailedPrecondition,
					fmt.Errorf("rule %d: EXCLUDE of template %s/%s/%s conflicts with explicit link on %s; unlink the template or narrow the rule's patterns",
						i,
						templateScopeLabel(tmpl.GetScope()),
						tmpl.GetScopeName(),
						tmpl.GetName(),
						offender))
			}
		}
	}
	return nil
}

// findExplicitLinkOffender returns a human-readable resource identifier of
// the first candidate (ProjectTemplate or Deployment) in projectNs whose
// linked-templates annotation contains `tmpl` AND whose target kind +
// deployment_pattern pair would select it at render time. Empty string
// means "no conflict in this project". ProjectTemplates are checked before
// Deployments so the reported identifier prefers the template-bearing
// resource when both would conflict.
//
// The render-selection contract (see console/policyresolver/
// folder_resolver.go:ruleAppliesTo): an EXCLUDE rule with a non-empty
// `deployment_pattern` applies ONLY to Deployments, never to project-scope
// Templates. Matching the same pattern against project-template names here
// would incorrectly reject deployment-only EXCLUDE rules that happen to
// carry a name overlapping an existing project-template. An empty
// `deployment_pattern` applies to both resource kinds, matching the
// resolver's behavior for project-template previews.
func (h *Handler) findExplicitLinkOffender(
	ctx context.Context,
	projectNs, projectSlug, deploymentPattern string,
	tmpl *consolev1.LinkedTemplateRef,
) (string, error) {
	if deploymentPattern == "" {
		templates, err := h.topologyResolver.ListProjectTemplates(ctx, projectNs)
		if err != nil {
			return "", fmt.Errorf("listing project templates: %w", err)
		}
		for i := range templates {
			if annotationLinksTemplate(&templates[i], tmpl) {
				return fmt.Sprintf("project-template %s/%s", projectSlug, templates[i].Name), nil
			}
		}
	}
	deployments, err := h.topologyResolver.ListProjectDeployments(ctx, projectNs)
	if err != nil {
		return "", fmt.Errorf("listing deployments: %w", err)
	}
	for i := range deployments {
		if matchesDeploymentPattern(deploymentPattern, deployments[i].Name) && annotationLinksTemplate(&deployments[i], tmpl) {
			return fmt.Sprintf("deployment %s/%s", projectSlug, deployments[i].Name), nil
		}
	}
	return "", nil
}

// matchesDeploymentPattern applies the rule's deployment_pattern glob
// against a Deployment name. An empty pattern matches everything —
// mirroring the resolver's behavior that treats an empty deployment_pattern
// as "apply to every deployment in the project".
func matchesDeploymentPattern(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	ok, err := filepath.Match(pattern, name)
	if err != nil {
		// validatePolicyRules already rejected bad patterns, so an error
		// here means the rule was mutated in-flight — treat as non-match so
		// the caller cannot use a malformed pattern to bypass the check in
		// one direction while it correctly fires in another.
		return false
	}
	return ok
}

// annotationLinksTemplate returns true when the `linked-templates` annotation
// on cm contains the exact (scope, scope_name, name) triple of ref. The JSON
// wire shape matches the one on Template + Deployment ConfigMaps (see
// console/templates/k8s.go:marshalLinkedTemplates). A malformed annotation
// is treated as "no explicit link" — the guardrail refuses to give the
// operator a false positive when the annotation itself is broken; any
// real-world malformed data would have already surfaced a warning from the
// owning handler when the resource was created.
func annotationLinksTemplate(cm *corev1.ConfigMap, ref *consolev1.LinkedTemplateRef) bool {
	if cm == nil || cm.Annotations == nil || ref == nil {
		return false
	}
	raw, ok := cm.Annotations[v1alpha2.AnnotationLinkedTemplates]
	if !ok || raw == "" {
		return false
	}
	type storedRef struct {
		Scope     string `json:"scope"`
		ScopeName string `json:"scope_name"`
		Name      string `json:"name"`
	}
	var stored []storedRef
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return false
	}
	wantScope := templateScopeLabel(ref.GetScope())
	wantScopeName := ref.GetScopeName()
	wantName := ref.GetName()
	for _, s := range stored {
		if s.Scope == wantScope && s.ScopeName == wantScopeName && s.Name == wantName {
			return true
		}
	}
	return false
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
