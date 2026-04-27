// Package templaterequirements implements TemplateRequirementService.
package templaterequirements

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"

	"connectrpc.com/connect"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"google.golang.org/protobuf/types/known/timestamppb"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "template-requirement"

var dnsLabelRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// OrgGrantResolver resolves organization-level grants for access checks.
type OrgGrantResolver interface {
	GetOrgGrants(ctx context.Context, org string) (users, roles map[string]string, err error)
}

// FolderGrantResolver resolves folder-level grants for access checks.
type FolderGrantResolver interface {
	GetFolderGrants(ctx context.Context, folder string) (users, roles map[string]string, err error)
}

// K8sClient wraps TemplateRequirement CRUD against the templates.holos.run CRD.
type K8sClient struct {
	client ctrlclient.Client
}

func NewK8sClient(client ctrlclient.Client) *K8sClient {
	return &K8sClient{client: client}
}

func (k *K8sClient) requestClient(ctx context.Context) ctrlclient.Client {
	if cl := rpc.ImpersonatedCtrlClientFromContext(ctx); cl != nil && rpc.HasImpersonatedClients(ctx) {
		return cl
	}
	return k.client
}

func (k *K8sClient) ListRequirements(ctx context.Context, namespace string) ([]templatesv1alpha1.TemplateRequirement, error) {
	var list templatesv1alpha1.TemplateRequirementList
	if err := k.client.List(ctx, &list, ctrlclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing template requirements in %q: %w", namespace, err)
	}
	if !rpc.HasImpersonatedClients(ctx) {
		return list.Items, nil
	}
	out := make([]templatesv1alpha1.TemplateRequirement, 0, len(list.Items))
	client := k.requestClient(ctx)
	for i := range list.Items {
		var got templatesv1alpha1.TemplateRequirement
		key := types.NamespacedName{Namespace: list.Items[i].Namespace, Name: list.Items[i].Name}
		if err := client.Get(ctx, key, &got); err != nil {
			if k8serrors.IsForbidden(err) || k8serrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		out = append(out, got)
	}
	return out, nil
}

func (k *K8sClient) ListRequirementsInNamespace(ctx context.Context, namespace string) ([]*templatesv1alpha1.TemplateRequirement, error) {
	items, err := k.ListRequirements(ctx, namespace)
	if err != nil {
		return nil, err
	}
	out := make([]*templatesv1alpha1.TemplateRequirement, 0, len(items))
	for i := range items {
		out = append(out, &items[i])
	}
	return out, nil
}

func (k *K8sClient) GetRequirement(ctx context.Context, namespace, name string) (*templatesv1alpha1.TemplateRequirement, error) {
	var req templatesv1alpha1.TemplateRequirement
	if err := k.requestClient(ctx).Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func (k *K8sClient) CreateRequirement(ctx context.Context, req *consolev1.TemplateRequirement, creatorEmail string) (*templatesv1alpha1.TemplateRequirement, error) {
	obj := &templatesv1alpha1.TemplateRequirement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.GetName(),
			Namespace: req.GetNamespace(),
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplateRequirement,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationCreatorEmail: creatorEmail,
			},
		},
		Spec: templatesv1alpha1.TemplateRequirementSpec{
			Requires:      protoLinkedTemplateRefToCRD(req.GetRequires()),
			TargetRefs:    protoTargetRefsToCRD(req.GetTargetRefs()),
			CascadeDelete: req.CascadeDelete,
		},
	}
	if err := k.requestClient(ctx).Create(ctx, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (k *K8sClient) UpdateRequirement(ctx context.Context, namespace string, req *consolev1.TemplateRequirement) (*templatesv1alpha1.TemplateRequirement, error) {
	obj, err := k.GetRequirement(ctx, namespace, req.GetName())
	if err != nil {
		return nil, fmt.Errorf("getting template requirement for update: %w", err)
	}
	obj.Spec.Requires = protoLinkedTemplateRefToCRD(req.GetRequires())
	obj.Spec.TargetRefs = protoTargetRefsToCRD(req.GetTargetRefs())
	if req.CascadeDelete != nil {
		obj.Spec.CascadeDelete = req.CascadeDelete
	}
	if err := k.requestClient(ctx).Update(ctx, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (k *K8sClient) DeleteRequirement(ctx context.Context, namespace, name string) error {
	return k.requestClient(ctx).Delete(ctx, &templatesv1alpha1.TemplateRequirement{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	})
}

// Handler implements TemplateRequirementService.
type Handler struct {
	consolev1connect.UnimplementedTemplateRequirementServiceHandler
	k8s                 *K8sClient
	resolver            *resolver.Resolver
	orgGrantResolver    OrgGrantResolver
	folderGrantResolver FolderGrantResolver
}

func NewHandler(k8s *K8sClient, r *resolver.Resolver) *Handler {
	return &Handler{k8s: k8s, resolver: r}
}

func (h *Handler) WithOrgGrantResolver(ogr OrgGrantResolver) *Handler {
	h.orgGrantResolver = ogr
	return h
}

func (h *Handler) WithFolderGrantResolver(fgr FolderGrantResolver) *Handler {
	h.folderGrantResolver = fgr
	return h
}

func (h *Handler) ListTemplateRequirements(
	ctx context.Context,
	req *connect.Request[consolev1.ListTemplateRequirementsRequest],
) (*connect.Response[consolev1.ListTemplateRequirementsResponse], error) {
	scope, scopeName, err := h.extractRequirementScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	items, err := h.k8s.ListRequirements(ctx, req.Msg.GetNamespace())
	if err != nil {
		return nil, mapK8sError(err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })

	requirements := make([]*consolev1.TemplateRequirement, 0, len(items))
	for i := range items {
		requirements = append(requirements, templateRequirementCRDToProto(&items[i]))
	}

	slog.InfoContext(ctx, "template requirements listed",
		slog.String("action", "template_requirement_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope),
		slog.String("scopeName", scopeName),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("count", len(requirements)),
	)

	return connect.NewResponse(&consolev1.ListTemplateRequirementsResponse{Requirements: requirements}), nil
}

func (h *Handler) GetTemplateRequirement(
	ctx context.Context,
	req *connect.Request[consolev1.GetTemplateRequirementRequest],
) (*connect.Response[consolev1.GetTemplateRequirementResponse], error) {
	scope, scopeName, err := h.extractRequirementScope(req.Msg.GetNamespace())
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

	reqObj, err := h.k8s.GetRequirement(ctx, req.Msg.GetNamespace(), name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template requirement read",
		slog.String("action", "template_requirement_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.GetTemplateRequirementResponse{
		Requirement: templateRequirementCRDToProto(reqObj),
	}), nil
}

func (h *Handler) CreateTemplateRequirement(
	ctx context.Context,
	req *connect.Request[consolev1.CreateTemplateRequirementRequest],
) (*connect.Response[consolev1.CreateTemplateRequirementResponse], error) {
	scope, scopeName, err := h.extractRequirementScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	requirement := req.Msg.GetRequirement()
	if requirement == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("requirement is required"))
	}
	if err := validateRequirement(req.Msg.GetNamespace(), requirement); err != nil {
		return nil, err
	}
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	_, err = h.k8s.CreateRequirement(ctx, requirement, claims.Email)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template requirement created",
		slog.String("action", "template_requirement_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope),
		slog.String("scopeName", scopeName),
		slog.String("name", requirement.GetName()),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateTemplateRequirementResponse{Name: requirement.GetName()}), nil
}

func (h *Handler) UpdateTemplateRequirement(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateTemplateRequirementRequest],
) (*connect.Response[consolev1.UpdateTemplateRequirementResponse], error) {
	scope, scopeName, err := h.extractRequirementScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	requirement := req.Msg.GetRequirement()
	if requirement == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("requirement is required"))
	}
	if err := validateRequirement(req.Msg.GetNamespace(), requirement); err != nil {
		return nil, err
	}
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if _, err := h.k8s.UpdateRequirement(ctx, req.Msg.GetNamespace(), requirement); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template requirement updated",
		slog.String("action", "template_requirement_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope),
		slog.String("scopeName", scopeName),
		slog.String("name", requirement.GetName()),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateTemplateRequirementResponse{}), nil
}

func (h *Handler) DeleteTemplateRequirement(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteTemplateRequirementRequest],
) (*connect.Response[consolev1.DeleteTemplateRequirementResponse], error) {
	scope, scopeName, err := h.extractRequirementScope(req.Msg.GetNamespace())
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

	if err := h.k8s.DeleteRequirement(ctx, req.Msg.GetNamespace(), name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template requirement deleted",
		slog.String("action", "template_requirement_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteTemplateRequirementResponse{}), nil
}

func (h *Handler) extractRequirementScope(namespace string) (string, string, error) {
	if namespace == "" {
		return "", "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace is required"))
	}
	if h.resolver == nil {
		return "", "", connect.NewError(connect.CodeInternal, fmt.Errorf("namespace resolver not wired"))
	}
	kind, name, err := h.resolver.ResourceTypeFromNamespace(namespace)
	if err != nil {
		return "", "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace must classify as organization or folder"))
	}
	switch kind {
	case v1alpha2.ResourceTypeOrganization, v1alpha2.ResourceTypeFolder:
	case v1alpha2.ResourceTypeProject:
		return "", "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("template requirements cannot be stored in project namespace %q; use an organization or folder scope", namespace))
	default:
		return "", "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace must classify as organization or folder"))
	}
	if name == "" {
		return "", "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope name is required"))
	}
	return kind, name, nil
}

func validateRequirement(reqNamespace string, requirement *consolev1.TemplateRequirement) error {
	if requirement.GetNamespace() == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("requirement.namespace is required"))
	}
	if requirement.GetNamespace() != reqNamespace {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("requirement.namespace (%q) must match request namespace (%q)", requirement.GetNamespace(), reqNamespace))
	}
	if err := validateRequirementName(requirement.GetName()); err != nil {
		return err
	}
	requires := requirement.GetRequires()
	if requires == nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("requires is required"))
	}
	if requires.GetNamespace() == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("requires.namespace is required"))
	}
	if requires.GetName() == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("requires.name is required"))
	}
	return validateTargetRefs(requirement.GetTargetRefs())
}

func validateRequirementName(name string) error {
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

func validateTargetRefs(refs []*consolev1.TemplateRequirementTargetRef) error {
	if len(refs) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("requirement must have at least one target_ref"))
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
		kind := targetKindString(ref.GetKind())
		key := kind + "|" + ref.GetProjectName() + "|" + ref.GetName()
		if prev, ok := seen[key]; ok {
			return connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("target_refs[%d]: duplicate of target_refs[%d] (kind=%s, project=%s, name=%s)",
					i, prev, kind, ref.GetProjectName(), ref.GetName()))
		}
		seen[key] = i
	}
	return nil
}

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

func templateRequirementCRDToProto(req *templatesv1alpha1.TemplateRequirement) *consolev1.TemplateRequirement {
	if req == nil {
		return nil
	}
	return &consolev1.TemplateRequirement{
		Name:          req.Name,
		Namespace:     req.Namespace,
		Requires:      crdLinkedTemplateRefToProto(req.Spec.Requires),
		TargetRefs:    crdTargetRefsToProto(req.Spec.TargetRefs),
		CascadeDelete: req.Spec.CascadeDelete,
		CreatorEmail:  req.Annotations[v1alpha2.AnnotationCreatorEmail],
		CreatedAt:     timestamppb.New(req.CreationTimestamp.Time),
		Status:        templateRequirementStatusToProto(req.Status),
	}
}

func templateRequirementStatusToProto(status templatesv1alpha1.TemplateRequirementStatus) *consolev1.TemplateRequirementStatus {
	out := &consolev1.TemplateRequirementStatus{
		ObservedGeneration: status.ObservedGeneration,
		Conditions:         make([]*consolev1.TemplateRequirementCondition, 0, len(status.Conditions)),
	}
	for i := range status.Conditions {
		c := status.Conditions[i]
		out.Conditions = append(out.Conditions, &consolev1.TemplateRequirementCondition{
			Type:               c.Type,
			Status:             string(c.Status),
			Reason:             c.Reason,
			Message:            c.Message,
			ObservedGeneration: c.ObservedGeneration,
			LastTransitionTime: timestamppb.New(c.LastTransitionTime.Time),
		})
	}
	return out
}

func protoLinkedTemplateRefToCRD(ref *consolev1.LinkedTemplateRef) templatesv1alpha1.LinkedTemplateRef {
	if ref == nil {
		return templatesv1alpha1.LinkedTemplateRef{}
	}
	return templatesv1alpha1.LinkedTemplateRef{
		Namespace:         ref.GetNamespace(),
		Name:              ref.GetName(),
		VersionConstraint: ref.GetVersionConstraint(),
	}
}

func crdLinkedTemplateRefToProto(ref templatesv1alpha1.LinkedTemplateRef) *consolev1.LinkedTemplateRef {
	return &consolev1.LinkedTemplateRef{
		Namespace:         ref.Namespace,
		Name:              ref.Name,
		VersionConstraint: ref.VersionConstraint,
	}
}

func protoTargetRefsToCRD(refs []*consolev1.TemplateRequirementTargetRef) []templatesv1alpha1.TemplateRequirementTargetRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]templatesv1alpha1.TemplateRequirementTargetRef, 0, len(refs))
	for _, r := range refs {
		if r == nil {
			continue
		}
		out = append(out, templatesv1alpha1.TemplateRequirementTargetRef{
			Kind:        targetKindProtoToCRD(r.GetKind()),
			Name:        r.GetName(),
			ProjectName: r.GetProjectName(),
		})
	}
	return out
}

func crdTargetRefsToProto(refs []templatesv1alpha1.TemplateRequirementTargetRef) []*consolev1.TemplateRequirementTargetRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]*consolev1.TemplateRequirementTargetRef, 0, len(refs))
	for i := range refs {
		r := refs[i]
		out = append(out, &consolev1.TemplateRequirementTargetRef{
			Kind:        targetKindCRDToProto(r.Kind),
			Name:        r.Name,
			ProjectName: r.ProjectName,
		})
	}
	return out
}

func targetKindProtoToCRD(k consolev1.TemplatePolicyBindingTargetKind) templatesv1alpha1.TemplatePolicyBindingTargetKind {
	switch k {
	case consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE:
		return templatesv1alpha1.TemplatePolicyBindingTargetKindProjectTemplate
	case consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT:
		return templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment
	default:
		return ""
	}
}

func targetKindCRDToProto(k templatesv1alpha1.TemplatePolicyBindingTargetKind) consolev1.TemplatePolicyBindingTargetKind {
	switch k {
	case templatesv1alpha1.TemplatePolicyBindingTargetKindProjectTemplate:
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE
	case templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment:
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT
	default:
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_UNSPECIFIED
	}
}

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

// mapK8sError delegates to rpc.MapK8sError so the handler shares the
// canonical apierrors -> connect.Code mapping with every other console
// handler.
func mapK8sError(err error) error {
	return rpc.MapK8sError(err)
}
