package templatepolicybindings

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	"connectrpc.com/connect"

	"google.golang.org/protobuf/types/known/timestamppb"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "template-policy-binding"

// dnsLabelRe validates binding names as DNS labels (mirrors
// console/templatepolicies).
var dnsLabelRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// scopeKind is a local discriminator for RBAC routing. The namespace is
// authoritative for storage; this discriminator classifies the namespace for
// access-check cascades and ancestor-chain validation.
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

// classifyNamespace returns the scopeKind and logical name for a Kubernetes
// namespace via the resolver's prefix scheme.
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

// PolicyExistsResolver reports whether a TemplatePolicy exists at a given
// scope. The handler calls this to validate a binding's policy_ref points
// at a policy that actually lives in the ancestor chain reachable from the
// binding's own scope — a binding without an existing policy is useless at
// render time, so failing at authoring time is a better developer
// experience than a silent no-op at render time.
//
// Returning (false, nil) means "policy not found" — the handler rejects
// with CodeInvalidArgument so the caller sees a precise user-input
// error. Returning (false, err) means the probe itself failed — the
// handler rejects with CodeInternal and logs the underlying error so
// operators can distinguish "policy is missing" (user bug) from "the
// cluster is unreachable" (infrastructure problem). The tests
// (TestCreateRejectsMissingPolicy, TestPolicyProbeErrorFailsInternal) pin
// both mappings.
//
// This interface lets the handler decouple from console/templatepolicies to
// avoid an import cycle; the policyresolver package consumes bindings
// transitively via HOL-596.
type PolicyExistsResolver interface {
	PolicyExists(ctx context.Context, namespace, name string) (bool, error)
}

// AncestorChainResolver reports whether a target namespace is on the
// ancestor chain starting from a given source namespace. The handler uses
// this to verify that a binding's policy_ref points at a policy stored in
// a scope the binding can reach — ancestor traversal is the only way a
// policy takes effect on the binding's render targets, so a ref outside
// the chain cannot fire at render time and should be rejected at
// authoring time.
//
// A nil resolver disables the ancestor-chain check (unit tests that only
// exercise same-scope policies may skip the wiring). A non-nil resolver
// that returns (false, nil) causes the handler to reject with
// CodeFailedPrecondition; a resolver that errors causes CodeInternal so
// operators can distinguish "policy is out of chain" from "the cluster
// couldn't tell us whether the policy is in chain".
type AncestorChainResolver interface {
	AncestorChainContains(ctx context.Context, startNs, wantNs string) (bool, error)
}

// ProjectExistsResolver reports whether a project slug exists under a given
// scope. The handler uses this to validate every target_ref carries a
// real project_name before the binding is stored; a stale project_name
// would never match at render time, so surfacing the typo at authoring
// time keeps the failure loud.
//
// A nil resolver disables the per-target project existence check (tests
// that only cover the non-target-validation paths may skip the wiring).
type ProjectExistsResolver interface {
	ProjectExists(ctx context.Context, parentNamespace, projectName string) (bool, error)
}

// Handler implements the TemplatePolicyBindingService.
type Handler struct {
	consolev1connect.UnimplementedTemplatePolicyBindingServiceHandler
	k8s                 *K8sClient
	resolver            *resolver.Resolver
	orgGrantResolver    OrgGrantResolver
	folderGrantResolver FolderGrantResolver
	policyResolver      PolicyExistsResolver
	ancestorResolver    AncestorChainResolver
	projectResolver     ProjectExistsResolver
}

// NewHandler creates a TemplatePolicyBindingService handler.
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

// WithPolicyExistsResolver configures the policy-existence check applied to
// a binding's policy_ref.
func (h *Handler) WithPolicyExistsResolver(per PolicyExistsResolver) *Handler {
	h.policyResolver = per
	return h
}

// WithAncestorChainResolver configures the ancestor-chain check applied to a
// binding's policy_ref.
func (h *Handler) WithAncestorChainResolver(acr AncestorChainResolver) *Handler {
	h.ancestorResolver = acr
	return h
}

// WithProjectExistsResolver configures the project-existence check applied
// to each target_ref's project_name.
func (h *Handler) WithProjectExistsResolver(per ProjectExistsResolver) *Handler {
	h.projectResolver = per
	return h
}

// ListTemplatePolicyBindings returns all bindings visible in the given
// scope.
func (h *Handler) ListTemplatePolicyBindings(
	ctx context.Context,
	req *connect.Request[consolev1.ListTemplatePolicyBindingsRequest],
) (*connect.Response[consolev1.ListTemplatePolicyBindingsResponse], error) {
	scope, scopeName, err := h.extractBindingScope(req.Msg.GetNamespace())
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

	items, err := h.k8s.ListBindings(ctx, req.Msg.GetNamespace())
	if err != nil {
		return nil, mapK8sError(err)
	}

	bindings := make([]*consolev1.TemplatePolicyBinding, 0, len(items))
	for i := range items {
		bindings = append(bindings, templatePolicyBindingCRDToProto(&items[i]))
	}

	slog.InfoContext(ctx, "template policy bindings listed",
		slog.String("action", "template_policy_binding_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("count", len(bindings)),
	)

	return connect.NewResponse(&consolev1.ListTemplatePolicyBindingsResponse{Bindings: bindings}), nil
}

// GetTemplatePolicyBinding returns a single binding by name.
func (h *Handler) GetTemplatePolicyBinding(
	ctx context.Context,
	req *connect.Request[consolev1.GetTemplatePolicyBindingRequest],
) (*connect.Response[consolev1.GetTemplatePolicyBindingResponse], error) {
	scope, scopeName, err := h.extractBindingScope(req.Msg.GetNamespace())
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

	b, err := h.k8s.GetBinding(ctx, req.Msg.GetNamespace(), name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template policy binding read",
		slog.String("action", "template_policy_binding_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.GetTemplatePolicyBindingResponse{
		Binding: templatePolicyBindingCRDToProto(b),
	}), nil
}

// CreateTemplatePolicyBinding creates a new binding.
func (h *Handler) CreateTemplatePolicyBinding(
	ctx context.Context,
	req *connect.Request[consolev1.CreateTemplatePolicyBindingRequest],
) (*connect.Response[consolev1.CreateTemplatePolicyBindingResponse], error) {
	scope, scopeName, err := h.extractBindingScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	binding := req.Msg.GetBinding()
	if binding == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("binding is required"))
	}
	if err := validateBindingNamespace(binding.GetNamespace(), req.Msg.GetNamespace()); err != nil {
		return nil, err
	}
	if err := validateBindingName(binding.GetName()); err != nil {
		return nil, err
	}
	if err := h.validatePolicyRef(binding.GetPolicyRef()); err != nil {
		return nil, err
	}
	if err := validateTargetRefs(binding.GetTargetRefs()); err != nil {
		return nil, err
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatePoliciesWrite); err != nil {
		return nil, err
	}

	// HOL-595: a binding's policy_ref MUST point at a policy that exists
	// in the binding's own scope or an ancestor scope. Rejecting an
	// out-of-chain ref at authoring time surfaces the mistake immediately
	// rather than silently no-op'ing at render time. The check runs AFTER
	// checkAccess so an unauthorized caller can't use the error to probe
	// which policies exist in which scope.
	if err := h.validatePolicyRefReachable(ctx, scope, scopeName, req.Msg.GetNamespace(), binding.GetPolicyRef()); err != nil {
		return nil, err
	}

	// HOL-595: every target_ref's project_name must reference a real
	// project under the binding's owning scope. Runs AFTER checkAccess
	// for the same probe-oracle reason as the policy-ref check.
	if err := h.validateTargetProjects(ctx, scope, scopeName, req.Msg.GetNamespace(), binding.GetTargetRefs()); err != nil {
		return nil, err
	}

	_, err = h.k8s.CreateBinding(
		ctx,
		req.Msg.GetNamespace(),
		binding.GetName(),
		binding.GetDisplayName(),
		binding.GetDescription(),
		claims.Email,
		binding.GetPolicyRef(),
		binding.GetTargetRefs(),
	)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template policy binding created",
		slog.String("action", "template_policy_binding_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", binding.GetName()),
		slog.Int("targets", len(binding.GetTargetRefs())),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateTemplatePolicyBindingResponse{
		Name: binding.GetName(),
	}), nil
}

// UpdateTemplatePolicyBinding updates an existing binding. Immutable fields
// (name, scope_ref, created_at, creator_email) are preserved from the
// stored ConfigMap; display_name, description, policy_ref, and target_refs
// are replaced from the request. Proto3 does not give us presence
// semantics on scalars, so display_name and description are always
// replaced — callers that want to preserve them must send them back
// verbatim. Policy_ref and target_refs are always re-validated; an update
// that introduces an out-of-chain policy_ref or a bad target_ref is
// rejected before the write.
func (h *Handler) UpdateTemplatePolicyBinding(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateTemplatePolicyBindingRequest],
) (*connect.Response[consolev1.UpdateTemplatePolicyBindingResponse], error) {
	scope, scopeName, err := h.extractBindingScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	binding := req.Msg.GetBinding()
	if binding == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("binding is required"))
	}
	if err := validateBindingNamespace(binding.GetNamespace(), req.Msg.GetNamespace()); err != nil {
		return nil, err
	}
	name := binding.GetName()
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	if err := h.validatePolicyRef(binding.GetPolicyRef()); err != nil {
		return nil, err
	}
	if err := validateTargetRefs(binding.GetTargetRefs()); err != nil {
		return nil, err
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkAccess(ctx, claims, scope, scopeName, rbac.PermissionTemplatePoliciesWrite); err != nil {
		return nil, err
	}

	// Fetch the existing binding so we can surface NotFound before we
	// attempt the Update (the K8s API would otherwise return a less
	// informative error) and also preserve unset immutable fields. The
	// reachability and target-project checks below run AFTER this read so
	// an Update to a non-existent binding still returns
	// connect.CodeNotFound regardless of the submitted policy_ref — clients
	// rely on that distinction for idempotent upsert flows.
	existing, err := h.k8s.GetBinding(ctx, req.Msg.GetNamespace(), name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	if err := h.validatePolicyRefReachable(ctx, scope, scopeName, req.Msg.GetNamespace(), binding.GetPolicyRef()); err != nil {
		return nil, err
	}
	if err := h.validateTargetProjects(ctx, scope, scopeName, req.Msg.GetNamespace(), binding.GetTargetRefs()); err != nil {
		return nil, err
	}

	// Proto3 scalar fields default to "" which we intentionally treat as
	// "no change" here so UI clients can send a targets-only update
	// without clobbering display name and description. A future API
	// revision may introduce field masks for explicit clears.
	var displayName, description *string
	if binding.GetDisplayName() != "" {
		dn := binding.GetDisplayName()
		displayName = &dn
	} else if existing.Spec.DisplayName == "" {
		empty := ""
		displayName = &empty
	}
	if binding.GetDescription() != "" {
		d := binding.GetDescription()
		description = &d
	} else if existing.Spec.Description == "" {
		empty := ""
		description = &empty
	}

	_, err = h.k8s.UpdateBinding(
		ctx,
		req.Msg.GetNamespace(),
		name,
		displayName,
		description,
		binding.GetPolicyRef(), true,
		binding.GetTargetRefs(), true,
	)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template policy binding updated",
		slog.String("action", "template_policy_binding_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.Int("targets", len(binding.GetTargetRefs())),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateTemplatePolicyBindingResponse{}), nil
}

// DeleteTemplatePolicyBinding deletes a binding.
func (h *Handler) DeleteTemplatePolicyBinding(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteTemplatePolicyBindingRequest],
) (*connect.Response[consolev1.DeleteTemplatePolicyBindingResponse], error) {
	scope, scopeName, err := h.extractBindingScope(req.Msg.GetNamespace())
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

	if err := h.k8s.DeleteBinding(ctx, req.Msg.GetNamespace(), name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template policy binding deleted",
		slog.String("action", "template_policy_binding_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteTemplatePolicyBindingResponse{}), nil
}

// extractBindingScope classifies an incoming namespace for the
// TemplatePolicyBindingService into the legacy (scope, scopeName) pair still
// used internally by the storage and access-check layers (HOL-621 rewrites
// those). Project namespaces are rejected directly and the rejection message
// names the project namespace so operators can debug misrouted clients. The
// same rejection applies on read and write so probing a project namespace
// cannot leak data.
func (h *Handler) extractBindingScope(namespace string) (scopeKind, string, error) {
	if namespace == "" {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace is required"))
	}
	if h.resolver == nil {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInternal, fmt.Errorf("namespace resolver not wired"))
	}
	scope, scopeName := classifyNamespace(h.resolver, namespace)
	if scope == scopeKindProject {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("template policy bindings cannot be stored in project namespace %q; use an organization or folder scope", namespace))
	}
	if scope == scopeKindUnspecified {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace must classify as organization or folder"))
	}
	if scopeName == "" {
		return scopeKindUnspecified, "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope name is required"))
	}
	return scope, scopeName, nil
}

// validateBindingNamespace enforces the proto contract that every binding
// carries its owning namespace and that it exactly matches the outer
// request namespace. Silently accepting a blank or mismatched namespace
// would let a client believe a binding was stored at one location when it
// was actually stored at another. Project namespaces are rejected via the
// caller's extractBindingScope (which has already classified reqNamespace)
// so this function only needs to check equality.
func validateBindingNamespace(bindingNamespace, reqNamespace string) error {
	if bindingNamespace == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("binding.namespace is required"))
	}
	if bindingNamespace != reqNamespace {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("binding.namespace (%q) must match request namespace (%q)", bindingNamespace, reqNamespace))
	}
	return nil
}

// validateBindingName enforces DNS-label rules and the 63-character limit so
// the generated ConfigMap name is always valid Kubernetes.
func validateBindingName(name string) error {
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

// validatePolicyRef enforces the proto contract on the LinkedTemplatePolicyRef
// carried by a binding. Every binding must reference exactly one policy; a
// half-populated ref (missing namespace or name) cannot be bound against any
// real policy so the handler rejects it up front. The referenced namespace
// must classify as organization or folder; project namespaces are rejected
// because a TemplatePolicy cannot live in a project namespace in the first
// place — a ref that targets a project is unusable.
func (h *Handler) validatePolicyRef(ref *consolev1.LinkedTemplatePolicyRef) error {
	if ref == nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("binding.policy_ref is required"))
	}
	if ref.GetNamespace() == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("binding.policy_ref.namespace is required"))
	}
	if ref.GetName() == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("binding.policy_ref.name is required"))
	}
	if h.resolver == nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("namespace resolver not wired"))
	}
	scope, scopeName := classifyNamespace(h.resolver, ref.GetNamespace())
	if scope == scopeKindProject {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("binding.policy_ref.namespace cannot be a project namespace; template policies live at organization or folder scope"))
	}
	if scope == scopeKindUnspecified || scopeName == "" {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("binding.policy_ref.namespace must classify as organization or folder"))
	}
	return nil
}

// validateTargetRefs enforces the invariants common to every target_ref:
// kind must be set to one of the two legal values; name and project_name
// must each be either the wildcard sentinel policyresolver.WildcardAny
// ("*") or a valid DNS label. Empty strings are always rejected — the
// resolver's nameMatches guard treats the empty target value as no-match
// for wildcard refs, so storing an empty value would silently make a
// binding match nothing (see HOL-772 handoff). Duplicate (kind,
// project_name, name) triples are rejected — two entries with the same
// triple make the binding semantically ambiguous and most likely signal a
// UI bug that submitted the same target twice. Two rows with
// {kind, "*", "*"} are therefore still a duplicate of each other.
//
// The wildcard short-circuit runs BEFORE dnsLabelRe because
// dnsLabelRe.MatchString("*") returns false. `kind` is never wildcarded;
// see ADR 029.
func validateTargetRefs(refs []*consolev1.TemplatePolicyBindingTargetRef) error {
	if len(refs) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("binding must have at least one target_ref"))
	}
	seen := make(map[string]int, len(refs))
	for i, ref := range refs {
		if ref == nil {
			return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("target_refs[%d]: target_ref is required", i))
		}
		switch ref.GetKind() {
		case consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE,
			consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT:
		default:
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("target_refs[%d]: kind must be PROJECT_TEMPLATE or DEPLOYMENT, got %v", i, ref.GetKind()))
		}
		if err := validateTargetRefField(i, "name", ref.GetName()); err != nil {
			return err
		}
		if err := validateTargetRefField(i, "project_name", ref.GetProjectName()); err != nil {
			return err
		}
		kindStr := targetKindString(ref.GetKind())
		key := kindStr + "|" + ref.GetProjectName() + "|" + ref.GetName()
		if prev, ok := seen[key]; ok {
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("target_refs[%d]: duplicate of target_refs[%d] (kind=%s, project=%s, name=%s)",
					i, prev, kindStr, ref.GetProjectName(), ref.GetName()))
		}
		seen[key] = i
	}
	return nil
}

// validateTargetRefField enforces the shared name/project_name syntax:
// the value must be either policyresolver.WildcardAny or a DNS label.
// Empty strings are rejected unconditionally — the resolver's wildcard
// match returns false when the target-side value is empty (HOL-770's
// storage-isolation guardrail), so allowing an empty stored value would
// silently produce a binding that matches nothing.
func validateTargetRefField(i int, field, value string) error {
	if value == "" {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("target_refs[%d]: %s is required", i, field))
	}
	if value == policyresolver.WildcardAny {
		return nil
	}
	if !dnsLabelRe.MatchString(value) {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("target_refs[%d]: %s must be a valid DNS label or %q, got %q",
				i, field, policyresolver.WildcardAny, value))
	}
	return nil
}

// validatePolicyRefReachable confirms the binding's policy_ref points at a
// TemplatePolicy that (a) actually exists and (b) lives in a scope the
// binding can reach via ancestor traversal. A wired
// PolicyExistsResolver makes (a) enforceable; a wired AncestorChainResolver
// makes (b) enforceable. When either resolver is nil the corresponding
// check is skipped — tests that don't exercise the seam do not need to
// stub it, mirroring the handler/topology pattern used in
// console/templatepolicies.
//
// The binding's own scope trivially reaches itself; the resolver is only
// consulted when the policy lives in a different scope.
func (h *Handler) validatePolicyRefReachable(
	ctx context.Context,
	scope scopeKind,
	scopeName string,
	bindingNamespace string,
	ref *consolev1.LinkedTemplatePolicyRef,
) error {
	if ref == nil {
		// validatePolicyRef would have already rejected this; belt-and-
		// suspenders in case a caller reaches past the common entry
		// points in the future.
		return nil
	}
	refNs := ref.GetNamespace()

	// First, confirm the policy exists. A missing policy is a definitive
	// authoring-time error regardless of whether the binding and policy
	// share a scope.
	if h.policyResolver != nil {
		exists, err := h.policyResolver.PolicyExists(ctx, refNs, ref.GetName())
		if err != nil {
			slog.WarnContext(ctx, "policy existence probe failed; rejecting binding",
				slog.String("policy_namespace", refNs),
				slog.String("policy_name", ref.GetName()),
				slog.Any("error", err),
			)
			return connect.NewError(connect.CodeInternal,
				fmt.Errorf("resolving policy %s/%s: %w", refNs, ref.GetName(), err))
		}
		if !exists {
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("binding.policy_ref points at unknown policy %s/%s",
					refNs, ref.GetName()))
		}
	}

	// Same namespace means the binding trivially reaches the policy via
	// the zero-length ancestor path — no resolver call needed.
	if refNs == bindingNamespace {
		return nil
	}

	if h.ancestorResolver == nil {
		return nil
	}

	contained, err := h.ancestorResolver.AncestorChainContains(ctx, bindingNamespace, refNs)
	if err != nil {
		return connect.NewError(connect.CodeInternal,
			fmt.Errorf("resolving ancestor chain for binding scope %s/%s: %w", scope, scopeName, err))
	}
	if !contained {
		return connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("binding.policy_ref namespace %q is not reachable from binding namespace %q via ancestor chain",
				refNs, bindingNamespace))
	}
	return nil
}

// validateTargetProjects confirms each target_ref's project_name names a
// project that exists under the binding's owning scope. When no
// ProjectExistsResolver is wired the check is a no-op — tests that only
// exercise the earlier validation paths may skip the wiring. An error
// from the resolver is converted to CodeInternal; a false return is
// converted to CodeInvalidArgument with the index and the offending
// project_name so the UI can highlight the bad row.
//
// A target_ref whose project_name is policyresolver.WildcardAny declares
// "every project reachable from the binding's storage scope" — there is
// no single project to probe, and the storage-scope ancestor walk already
// caps the wildcard's reach at render time. The handler skips the
// existence probe and also bypasses the project-name cache so a literal
// ref in the same request still reaches ProjectExistsResolver.
func (h *Handler) validateTargetProjects(
	ctx context.Context,
	scope scopeKind,
	scopeName string,
	parentNamespace string,
	refs []*consolev1.TemplatePolicyBindingTargetRef,
) error {
	if h.projectResolver == nil {
		return nil
	}
	// Cache lookups by project_name so a binding that targets many
	// resources inside the same project does not hammer the resolver.
	checked := make(map[string]bool, len(refs))
	for i, ref := range refs {
		project := ref.GetProjectName()
		if project == "" {
			continue // validateTargetRefs already rejected this
		}
		if project == policyresolver.WildcardAny {
			// Wildcard project_name: nothing to probe, and the cache is
			// bypassed so a literal ref with the same stored string is
			// still probed (currently impossible since "*" is not a DNS
			// label, but the bypass keeps the cache semantically correct).
			continue
		}
		if checked[project] {
			continue
		}
		exists, err := h.projectResolver.ProjectExists(ctx, parentNamespace, project)
		if err != nil {
			return connect.NewError(connect.CodeInternal,
				fmt.Errorf("target_refs[%d]: resolving project %q: %w", i, project, err))
		}
		if !exists {
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("target_refs[%d]: project %q does not exist under binding scope %s/%s",
					i, project, scope, scopeName))
		}
		checked[project] = true
	}
	return nil
}

// templatePolicyBindingCRDToProto converts a TemplatePolicyBinding CRD into
// its proto representation. HOL-662 replaced the ConfigMap-annotation path
// with structured CRD spec fields: policy_ref and target_refs come out of
// b.Spec, not JSON-encoded annotations. creator_email remains an annotation
// because the CRD does not yet have a dedicated field for it.
func templatePolicyBindingCRDToProto(b *templatesv1alpha1.TemplatePolicyBinding) *consolev1.TemplatePolicyBinding {
	binding := &consolev1.TemplatePolicyBinding{
		Name:         b.Name,
		Namespace:    b.Namespace,
		DisplayName:  b.Spec.DisplayName,
		Description:  b.Spec.Description,
		CreatorEmail: b.Annotations[v1alpha2.AnnotationCreatorEmail],
		CreatedAt:    timestamppb.New(b.CreationTimestamp.Time),
		PolicyRef:    CRDPolicyRefToProto(b.Spec.PolicyRef),
		TargetRefs:   CRDTargetRefsToProto(b.Spec.TargetRefs),
	}
	return binding
}

// targetKindString returns the short lowercase label for a target kind for
// duplicate-detection keys and error messages. It intentionally matches the
// CRD's stored enum values so debug logs line up with what operators see in
// kubectl.
func targetKindString(k consolev1.TemplatePolicyBindingTargetKind) string {
	switch k {
	case consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE:
		return "project-template"
	case consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT:
		return "deployment"
	default:
		return "unspecified"
	}
}

// mapK8sError converts Kubernetes API errors to ConnectRPC errors. The
// CEL ValidatingAdmissionPolicy shipped in HOL-618 rejects
// project-namespace creates at admission time; rpc.MapK8sError maps
// IsInvalid to CodeInvalidArgument so admission-denied surfaces as a
// client error rather than a 500.
func mapK8sError(err error) error {
	return rpc.MapK8sError(err)
}
