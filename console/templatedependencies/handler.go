// Package templatedependencies implements TemplateDependencyService.
package templatedependencies

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"

	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"google.golang.org/protobuf/types/known/timestamppb"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "template-dependency"

var dnsLabelRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// ProjectGrantResolver resolves project namespace grants for access checks.
type ProjectGrantResolver interface {
	GetProjectGrants(ctx context.Context, project string) (shareUsers, shareRoles map[string]string, err error)
}

// K8sClient wraps TemplateDependency CRUD against the templates.holos.run CRD.
type K8sClient struct {
	client ctrlclient.Client
}

func NewK8sClient(client ctrlclient.Client) *K8sClient {
	return &K8sClient{client: client}
}

func (k *K8sClient) ListDependencies(ctx context.Context, namespace string) ([]templatesv1alpha1.TemplateDependency, error) {
	var list templatesv1alpha1.TemplateDependencyList
	if err := k.client.List(ctx, &list, ctrlclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing template dependencies in %q: %w", namespace, err)
	}
	return list.Items, nil
}

func (k *K8sClient) GetDependency(ctx context.Context, namespace, name string) (*templatesv1alpha1.TemplateDependency, error) {
	var d templatesv1alpha1.TemplateDependency
	if err := k.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (k *K8sClient) CreateDependency(ctx context.Context, dep *consolev1.TemplateDependency, creatorEmail string) (*templatesv1alpha1.TemplateDependency, error) {
	d := &templatesv1alpha1.TemplateDependency{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dep.GetName(),
			Namespace: dep.GetNamespace(),
			Labels: map[string]string{
				v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationCreatorEmail: creatorEmail,
			},
		},
		Spec: templatesv1alpha1.TemplateDependencySpec{
			Dependent:     protoLinkedTemplateRefToCRD(dep.GetDependent()),
			Requires:      protoLinkedTemplateRefToCRD(dep.GetRequires()),
			CascadeDelete: dep.CascadeDelete,
		},
	}
	if err := k.client.Create(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (k *K8sClient) UpdateDependency(ctx context.Context, namespace string, dep *consolev1.TemplateDependency) (*templatesv1alpha1.TemplateDependency, error) {
	d, err := k.GetDependency(ctx, namespace, dep.GetName())
	if err != nil {
		return nil, fmt.Errorf("getting template dependency for update: %w", err)
	}
	d.Spec.Dependent = protoLinkedTemplateRefToCRD(dep.GetDependent())
	d.Spec.Requires = protoLinkedTemplateRefToCRD(dep.GetRequires())
	if dep.CascadeDelete != nil {
		d.Spec.CascadeDelete = dep.CascadeDelete
	}
	if err := k.client.Update(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (k *K8sClient) DeleteDependency(ctx context.Context, namespace, name string) error {
	return k.client.Delete(ctx, &templatesv1alpha1.TemplateDependency{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	})
}

// Handler implements TemplateDependencyService.
type Handler struct {
	consolev1connect.UnimplementedTemplateDependencyServiceHandler
	k8s                  *K8sClient
	resolver             *resolver.Resolver
	projectGrantResolver ProjectGrantResolver
}

func NewHandler(k8s *K8sClient, r *resolver.Resolver) *Handler {
	return &Handler{k8s: k8s, resolver: r}
}

func (h *Handler) WithProjectGrantResolver(pgr ProjectGrantResolver) *Handler {
	h.projectGrantResolver = pgr
	return h
}

func (h *Handler) ListTemplateDependencies(
	ctx context.Context,
	req *connect.Request[consolev1.ListTemplateDependenciesRequest],
) (*connect.Response[consolev1.ListTemplateDependenciesResponse], error) {
	project, err := h.extractDependencyScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	claims, err := h.claimsAndAccess(ctx, project, rbac.PermissionTemplatesList)
	if err != nil {
		return nil, err
	}

	items, err := h.k8s.ListDependencies(ctx, req.Msg.GetNamespace())
	if err != nil {
		return nil, mapK8sError(err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })

	deps := make([]*consolev1.TemplateDependency, 0, len(items))
	for i := range items {
		deps = append(deps, templateDependencyCRDToProto(&items[i]))
	}

	slog.InfoContext(ctx, "template dependencies listed",
		slog.String("action", "template_dependency_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("count", len(deps)),
	)

	return connect.NewResponse(&consolev1.ListTemplateDependenciesResponse{Dependencies: deps}), nil
}

func (h *Handler) GetTemplateDependency(
	ctx context.Context,
	req *connect.Request[consolev1.GetTemplateDependencyRequest],
) (*connect.Response[consolev1.GetTemplateDependencyResponse], error) {
	project, err := h.extractDependencyScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	name := req.Msg.GetName()
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	claims, err := h.claimsAndAccess(ctx, project, rbac.PermissionTemplatesRead)
	if err != nil {
		return nil, err
	}

	dep, err := h.k8s.GetDependency(ctx, req.Msg.GetNamespace(), name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template dependency read",
		slog.String("action", "template_dependency_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.GetTemplateDependencyResponse{
		Dependency: templateDependencyCRDToProto(dep),
	}), nil
}

func (h *Handler) CreateTemplateDependency(
	ctx context.Context,
	req *connect.Request[consolev1.CreateTemplateDependencyRequest],
) (*connect.Response[consolev1.CreateTemplateDependencyResponse], error) {
	project, err := h.extractDependencyScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	dep := req.Msg.GetDependency()
	if dep == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("dependency is required"))
	}
	if err := validateDependency(req.Msg.GetNamespace(), dep); err != nil {
		return nil, err
	}
	claims, err := h.claimsAndAccess(ctx, project, rbac.PermissionTemplatesWrite)
	if err != nil {
		return nil, err
	}

	_, err = h.k8s.CreateDependency(ctx, dep, claims.Email)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template dependency created",
		slog.String("action", "template_dependency_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", dep.GetName()),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateTemplateDependencyResponse{Name: dep.GetName()}), nil
}

func (h *Handler) UpdateTemplateDependency(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateTemplateDependencyRequest],
) (*connect.Response[consolev1.UpdateTemplateDependencyResponse], error) {
	project, err := h.extractDependencyScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	dep := req.Msg.GetDependency()
	if dep == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("dependency is required"))
	}
	if err := validateDependency(req.Msg.GetNamespace(), dep); err != nil {
		return nil, err
	}
	claims, err := h.claimsAndAccess(ctx, project, rbac.PermissionTemplatesWrite)
	if err != nil {
		return nil, err
	}

	if _, err := h.k8s.UpdateDependency(ctx, req.Msg.GetNamespace(), dep); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template dependency updated",
		slog.String("action", "template_dependency_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", dep.GetName()),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateTemplateDependencyResponse{}), nil
}

func (h *Handler) DeleteTemplateDependency(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteTemplateDependencyRequest],
) (*connect.Response[consolev1.DeleteTemplateDependencyResponse], error) {
	project, err := h.extractDependencyScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	name := req.Msg.GetName()
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	claims, err := h.claimsAndAccess(ctx, project, rbac.PermissionTemplatesDelete)
	if err != nil {
		return nil, err
	}

	if err := h.k8s.DeleteDependency(ctx, req.Msg.GetNamespace(), name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template dependency deleted",
		slog.String("action", "template_dependency_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteTemplateDependencyResponse{}), nil
}

func (h *Handler) extractDependencyScope(namespace string) (string, error) {
	if namespace == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace is required"))
	}
	if h.resolver == nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("namespace resolver not wired"))
	}
	kind, name, err := h.resolver.ResourceTypeFromNamespace(namespace)
	if err != nil {
		return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace must classify as project"))
	}
	if kind != v1alpha2.ResourceTypeProject {
		return "", connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("template dependencies must be stored in a project namespace, got %q", namespace))
	}
	if name == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name is required"))
	}
	return name, nil
}

func (h *Handler) claimsAndAccess(ctx context.Context, project string, perm rbac.Permission) (*rpc.Claims, error) {
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}
	if h.projectGrantResolver == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	users, roles, err := h.projectGrantResolver.GetProjectGrants(ctx, project)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve project grants", slog.String("project", project), slog.Any("error", err))
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	if err := rbac.CheckCascadeAccess(claims.Email, claims.Roles, users, roles, perm, rbac.TemplateCascadePerms); err != nil {
		return nil, err
	}
	return claims, nil
}

func validateDependency(reqNamespace string, dep *consolev1.TemplateDependency) error {
	if dep.GetNamespace() == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("dependency.namespace is required"))
	}
	if dep.GetNamespace() != reqNamespace {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("dependency.namespace (%q) must match request namespace (%q)", dep.GetNamespace(), reqNamespace))
	}
	if err := validateDependencyName(dep.GetName()); err != nil {
		return err
	}
	dependent := dep.GetDependent()
	if dependent == nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("dependent is required"))
	}
	if dependent.GetNamespace() == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("dependent.namespace is required"))
	}
	if dependent.GetNamespace() != reqNamespace {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("dependent.namespace (%q) must match dependency namespace (%q)", dependent.GetNamespace(), reqNamespace))
	}
	if dependent.GetName() == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("dependent.name is required"))
	}
	requires := dep.GetRequires()
	if requires == nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("requires is required"))
	}
	if requires.GetNamespace() == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("requires.namespace is required"))
	}
	if requires.GetName() == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("requires.name is required"))
	}
	return nil
}

func validateDependencyName(name string) error {
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

func templateDependencyCRDToProto(d *templatesv1alpha1.TemplateDependency) *consolev1.TemplateDependency {
	if d == nil {
		return nil
	}
	return &consolev1.TemplateDependency{
		Name:          d.Name,
		Namespace:     d.Namespace,
		Dependent:     crdLinkedTemplateRefToProto(d.Spec.Dependent),
		Requires:      crdLinkedTemplateRefToProto(d.Spec.Requires),
		CascadeDelete: d.Spec.CascadeDelete,
		CreatorEmail:  d.Annotations[v1alpha2.AnnotationCreatorEmail],
		CreatedAt:     timestamppb.New(d.CreationTimestamp.Time),
		Status:        templateDependencyStatusToProto(d.Status),
	}
}

func templateDependencyStatusToProto(status templatesv1alpha1.TemplateDependencyStatus) *consolev1.TemplateDependencyStatus {
	out := &consolev1.TemplateDependencyStatus{
		ObservedGeneration: status.ObservedGeneration,
		Conditions:         make([]*consolev1.TemplateDependencyCondition, 0, len(status.Conditions)),
	}
	for i := range status.Conditions {
		c := status.Conditions[i]
		out.Conditions = append(out.Conditions, &consolev1.TemplateDependencyCondition{
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

// mapK8sError delegates to rpc.MapK8sError so the handler shares the
// canonical apierrors -> connect.Code mapping with every other console
// handler.
func mapK8sError(err error) error {
	return rpc.MapK8sError(err)
}
