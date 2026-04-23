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
// Render-time selection runs exclusively through TemplatePolicyBinding
// (HOL-590). A rule contributes to a render target only when a matching
// binding names its owning policy. See console/policyresolver and
// console/templates/k8s.go for the resolver call sites, and
// console/templatepolicybindings for the binding CRUD.
package templatepolicies

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"

	"connectrpc.com/connect"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"google.golang.org/protobuf/types/known/timestamppb"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
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

// scopeKind is a local discriminator for RBAC routing. The namespace is
// authoritative for storage; this discriminator classifies the namespace for
// access-check cascades.
type scopeKind int

const (
	scopeKindUnspecified scopeKind = iota
	scopeKindOrganization
	scopeKindFolder
	scopeKindProject
)

// String returns a short label for audit logs and error messages.
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

// classifyNamespace returns the scopeKind and logical name (org/folder/project
// slug) for a Kubernetes namespace via the resolver's prefix scheme.
func classifyNamespace(r *resolver.Resolver, ns string) (scopeKind, string) {
	if r == nil || ns == "" {
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

// TemplateExistsResolver reports whether a template exists in the given
// Kubernetes namespace. The handler calls this as a best-effort check when a
// policy references a template so obviously broken policies (typos, wrong
// namespace) fail fast; the check is advisory, and transient Kubernetes errors
// are logged but do not block the write.
//
// This interface lets the handler decouple from console/templates to avoid an
// import cycle; console/templates consumes this package transitively via
// policyresolver for the render-time resolver wired in HOL-567.
type TemplateExistsResolver interface {
	TemplateExists(ctx context.Context, namespace, name string) (bool, error)
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
	scope, scopeName, err := h.extractPolicyScope(req.Msg.GetNamespace())
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

	items, err := h.k8s.ListPolicies(ctx, req.Msg.GetNamespace())
	if err != nil {
		return nil, mapK8sError(err)
	}

	policies := make([]*consolev1.TemplatePolicy, 0, len(items))
	for i := range items {
		policies = append(policies, templatePolicyCRDToProto(&items[i]))
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
	scope, scopeName, err := h.extractPolicyScope(req.Msg.GetNamespace())
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

	p, err := h.k8s.GetPolicy(ctx, req.Msg.GetNamespace(), name)
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
		Policy: templatePolicyCRDToProto(p),
	}), nil
}

// CreateTemplatePolicy creates a new policy.
func (h *Handler) CreateTemplatePolicy(
	ctx context.Context,
	req *connect.Request[consolev1.CreateTemplatePolicyRequest],
) (*connect.Response[consolev1.CreateTemplatePolicyResponse], error) {
	scope, scopeName, err := h.extractPolicyScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	policy := req.Msg.GetPolicy()
	if policy == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("policy is required"))
	}
	if err := validatePolicyNamespace(policy.GetNamespace(), req.Msg.GetNamespace()); err != nil {
		return nil, err
	}
	if err := validatePolicyName(policy.GetName()); err != nil {
		return nil, err
	}
	if err := validatePolicyRules(h.resolver, policy.GetRules()); err != nil {
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

	_, err = h.k8s.CreatePolicy(ctx, req.Msg.GetNamespace(), policy.GetName(), policy.GetDisplayName(), policy.GetDescription(), claims.Email, policy.GetRules())
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
	scope, scopeName, err := h.extractPolicyScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	policy := req.Msg.GetPolicy()
	if policy == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("policy is required"))
	}
	if err := validatePolicyNamespace(policy.GetNamespace(), req.Msg.GetNamespace()); err != nil {
		return nil, err
	}
	name := policy.GetName()
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	if err := validatePolicyRules(h.resolver, policy.GetRules()); err != nil {
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
	existing, err := h.k8s.GetPolicy(ctx, req.Msg.GetNamespace(), name)
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
	} else if existing.Spec.DisplayName == "" {
		empty := ""
		displayName = &empty
	}
	if policy.GetDescription() != "" {
		d := policy.GetDescription()
		description = &d
	} else if existing.Spec.Description == "" {
		empty := ""
		description = &empty
	}

	h.probeReferencedTemplates(ctx, policy.GetRules())

	_, err = h.k8s.UpdatePolicy(ctx, req.Msg.GetNamespace(), name, displayName, description, policy.GetRules(), true)
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
	scope, scopeName, err := h.extractPolicyScope(req.Msg.GetNamespace())
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

	if err := h.k8s.DeletePolicy(ctx, req.Msg.GetNamespace(), name); err != nil {
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

// ListLinkableTemplatePolicies returns every TemplatePolicy in the given
// namespace, ordered alphabetically by name (HOL-912). The caller must hold
// PERMISSION_TEMPLATE_POLICIES_LIST on the namespace; callers without that
// permission receive a PermissionDenied error.
//
// This RPC is semantically equivalent to ListTemplatePolicies and is provided
// as a stable alias for the TemplatePolicyBinding picker UI. The ancestor-walk
// / per-scope RBAC logic introduced in HOL-834 has been removed; the RPC
// now accepts only a namespace and returns the TemplatePolicies stored there.
func (h *Handler) ListLinkableTemplatePolicies(
	ctx context.Context,
	req *connect.Request[consolev1.ListLinkableTemplatePoliciesRequest],
) (*connect.Response[consolev1.ListLinkableTemplatePoliciesResponse], error) {
	namespace := req.Msg.GetNamespace()
	if namespace == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	scope, scopeName, err := h.extractPolicyScope(namespace)
	if err != nil {
		return nil, err
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatePoliciesList); err != nil {
		return nil, err
	}

	items, err := h.k8s.ListPolicies(ctx, namespace)
	if err != nil {
		return nil, mapK8sError(err)
	}

	policies := make([]*consolev1.LinkableTemplatePolicy, 0, len(items))
	for i := range items {
		policies = append(policies, &consolev1.LinkableTemplatePolicy{
			Policy: templatePolicyCRDToProto(&items[i]),
		})
	}
	// Stable alphabetical ordering by name satisfies the AC and makes the
	// response deterministic regardless of Kubernetes list ordering.
	sort.Slice(policies, func(i, j int) bool {
		return policies[i].GetPolicy().GetName() < policies[j].GetPolicy().GetName()
	})

	slog.InfoContext(ctx, "linkable template policies listed",
		slog.String("action", "linkable_template_policies_list"),
		slog.String("namespace", namespace),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("count", len(policies)),
	)

	return connect.NewResponse(&consolev1.ListLinkableTemplatePoliciesResponse{
		Policies: policies,
	}), nil
}

// extractPolicyScope classifies an incoming namespace for the
// TemplatePolicyService into the legacy (scope, scopeName) pair still used
// internally by the storage and access-check layers (HOL-621 rewrites those).
// Project namespaces are rejected directly and the rejection message names
// the project namespace so operators can debug misrouted clients. The same
// rejection applies on read and write so probing a project namespace cannot
// leak data.
func (h *Handler) extractPolicyScope(namespace string) (scopeKind, string, error) {
	if namespace == "" {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace is required"))
	}
	if h.resolver == nil {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInternal, fmt.Errorf("namespace resolver not wired"))
	}
	scope, scopeName := classifyNamespace(h.resolver, namespace)
	if scope == scopeKindProject {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("template policies cannot be stored in project namespace %q; use an organization or folder scope", namespace))
	}
	if scope == scopeKindUnspecified {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace must classify as organization or folder"))
	}
	if scopeName == "" {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope name is required"))
	}
	return scope, scopeName, nil
}

// validatePolicyNamespace enforces the proto contract that every
// TemplatePolicy carries its owning namespace and that it exactly matches
// the outer request namespace. The proto comments on
// CreateTemplatePolicyRequest.policy and UpdateTemplatePolicyRequest.policy
// state namespace is required; silently accepting a blank or mismatched
// namespace would let a client believe a policy was stored at one location
// when it was actually stored at another. Project namespaces are rejected
// via the caller's extractPolicyScope (which has already classified
// reqNamespace) so this function only needs to check equality.
func validatePolicyNamespace(policyNamespace, reqNamespace string) error {
	if policyNamespace == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("policy.namespace is required"))
	}
	if policyNamespace != reqNamespace {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("policy.namespace (%q) must match request namespace (%q)", policyNamespace, reqNamespace))
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

// validatePolicyRules enforces kind and template-reference invariants. The
// legacy glob-based TemplatePolicyTarget validation (project_pattern /
// deployment_pattern) was removed in HOL-600 — TemplatePolicyBinding objects
// now own render-target selection, so a policy's rules carry only the
// (kind, template) tuple the binding will inject.
//
// When the resolver is non-nil, `template.namespace` must classify as an
// organization/folder/project namespace. HOL-619 collapsed the scope enum and
// HOL-723 retired the scopeshim; this check preserves the guardrail the old
// (scope, scopeName) enum gave us for free — render-time resolution only
// searches console-managed ancestor namespaces, so a rule naming `default`
// or any other foreign namespace would silently never apply.
func validatePolicyRules(r *resolver.Resolver, rules []*consolev1.TemplatePolicyRule) error {
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
		ns := tmpl.GetNamespace()
		if ns == "" {
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("rule %d: template.namespace is required", i))
		}
		if r != nil {
			if kind, _ := classifyNamespace(r, ns); kind == scopeKindUnspecified {
				return connect.NewError(connect.CodeInvalidArgument,
					fmt.Errorf("rule %d: template.namespace %q is not a console-managed organization, folder, or project namespace", i, ns))
			}
		}
	}
	return nil
}

// probeReferencedTemplates performs a best-effort existence check for every
// template referenced by the policy. Per the acceptance criteria a transient
// failure is logged and ignored so the policy can still be written; only
// definitive "does not exist" signals are logged as warnings. The function
// intentionally does not return an error — enforcement happens at render time
// via policyresolver.FolderResolver (HOL-567).
func (h *Handler) probeReferencedTemplates(ctx context.Context, rules []*consolev1.TemplatePolicyRule) {
	if h.templateResolver == nil {
		return
	}
	for i, rule := range rules {
		tmpl := rule.GetTemplate()
		if tmpl == nil {
			continue
		}
		exists, err := h.templateResolver.TemplateExists(ctx, tmpl.GetNamespace(), tmpl.GetName())
		if err != nil {
			slog.WarnContext(ctx, "template existence probe failed; continuing",
				slog.Int("rule_index", i),
				slog.String("template_namespace", tmpl.GetNamespace()),
				slog.String("template_name", tmpl.GetName()),
				slog.Any("error", err),
			)
			continue
		}
		if !exists {
			slog.WarnContext(ctx, "policy references template that does not currently exist",
				slog.Int("rule_index", i),
				slog.String("template_namespace", tmpl.GetNamespace()),
				slog.String("template_name", tmpl.GetName()),
			)
		}
	}
}

// templatePolicyCRDToProto converts a TemplatePolicy CRD into its proto
// representation. HOL-662 rewrote this from the legacy ConfigMap path — rules
// are now on the CRD spec directly, and display_name / description are typed
// fields instead of annotations. creator_email remains an annotation because
// the CRD does not yet have a field for it; we surface it as a best-effort
// audit pointer.
func templatePolicyCRDToProto(p *templatesv1alpha1.TemplatePolicy) *consolev1.TemplatePolicy {
	policy := &consolev1.TemplatePolicy{
		Name:         p.Name,
		Namespace:    p.Namespace,
		DisplayName:  p.Spec.DisplayName,
		Description:  p.Spec.Description,
		CreatorEmail: p.Annotations[v1alpha2.AnnotationCreatorEmail],
		CreatedAt:    timestamppb.New(p.CreationTimestamp.Time),
		Rules:        CRDRulesToProto(p.Spec.Rules),
	}
	return policy
}

// mapK8sError converts Kubernetes API errors to ConnectRPC errors. The CEL
// ValidatingAdmissionPolicy shipped in HOL-618 rejects project-namespace
// creates at admission time, so the handler only needs the generic
// k8serrors taxonomy here (plus extractPolicyScope as defense-in-depth).
func mapK8sError(err error) error {
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
	// Invalid: the apiserver rejected the object as malformed or
	// admission-denied. Examples: CRD schema validation (MinItems,
	// Required), OpenAPI type checks, and CEL ValidatingAdmissionPolicy
	// rejections (HOL-618). These are client errors from the caller's
	// perspective, not server failures, so surface them as
	// InvalidArgument so the UI can present a meaningful message
	// instead of a generic 500.
	if k8serrors.IsInvalid(err) {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
