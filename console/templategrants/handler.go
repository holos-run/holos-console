// Package templategrants implements TemplateGrantService.
package templategrants

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
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "template-grant"

var dnsLabelRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// OrgGrantResolver resolves organization-level grants for access checks.
type OrgGrantResolver interface {
	GetOrgGrants(ctx context.Context, org string) (users, roles map[string]string, err error)
}

// FolderGrantResolver resolves folder-level grants for access checks.
type FolderGrantResolver interface {
	GetFolderGrants(ctx context.Context, folder string) (users, roles map[string]string, err error)
}

// K8sClient wraps TemplateGrant CRUD against the templates.holos.run CRD.
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

func (k *K8sClient) ListGrants(ctx context.Context, namespace string) ([]templatesv1alpha1.TemplateGrant, error) {
	var list templatesv1alpha1.TemplateGrantList
	if err := k.client.List(ctx, &list, ctrlclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing template grants in %q: %w", namespace, err)
	}
	if !rpc.HasImpersonatedClients(ctx) {
		return list.Items, nil
	}
	out := make([]templatesv1alpha1.TemplateGrant, 0, len(list.Items))
	client := k.requestClient(ctx)
	for i := range list.Items {
		var got templatesv1alpha1.TemplateGrant
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

func (k *K8sClient) GetGrant(ctx context.Context, namespace, name string) (*templatesv1alpha1.TemplateGrant, error) {
	var g templatesv1alpha1.TemplateGrant
	if err := k.requestClient(ctx).Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

func (k *K8sClient) CreateGrant(ctx context.Context, grant *consolev1.TemplateGrant, creatorEmail string) (*templatesv1alpha1.TemplateGrant, error) {
	obj := &templatesv1alpha1.TemplateGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      grant.GetName(),
			Namespace: grant.GetNamespace(),
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplateGrant,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationCreatorEmail: creatorEmail,
			},
		},
		Spec: templatesv1alpha1.TemplateGrantSpec{
			From: protoFromRefsToCRD(grant.GetFrom()),
			To:   protoToRefsToCRD(grant.GetTo()),
		},
	}
	if err := k.requestClient(ctx).Create(ctx, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (k *K8sClient) UpdateGrant(ctx context.Context, namespace string, grant *consolev1.TemplateGrant) (*templatesv1alpha1.TemplateGrant, error) {
	obj, err := k.GetGrant(ctx, namespace, grant.GetName())
	if err != nil {
		return nil, fmt.Errorf("getting template grant for update: %w", err)
	}
	obj.Spec.From = protoFromRefsToCRD(grant.GetFrom())
	obj.Spec.To = protoToRefsToCRD(grant.GetTo())
	if err := k.requestClient(ctx).Update(ctx, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (k *K8sClient) DeleteGrant(ctx context.Context, namespace, name string) error {
	return k.requestClient(ctx).Delete(ctx, &templatesv1alpha1.TemplateGrant{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	})
}

// Handler implements TemplateGrantService.
type Handler struct {
	consolev1connect.UnimplementedTemplateGrantServiceHandler
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

func (h *Handler) ListTemplateGrants(
	ctx context.Context,
	req *connect.Request[consolev1.ListTemplateGrantsRequest],
) (*connect.Response[consolev1.ListTemplateGrantsResponse], error) {
	scope, scopeName, err := h.extractGrantScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	items, err := h.k8s.ListGrants(ctx, req.Msg.GetNamespace())
	if err != nil {
		return nil, mapK8sError(err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })

	grants := make([]*consolev1.TemplateGrant, 0, len(items))
	for i := range items {
		grants = append(grants, templateGrantCRDToProto(&items[i]))
	}

	slog.InfoContext(ctx, "template grants listed",
		slog.String("action", "template_grant_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope),
		slog.String("scopeName", scopeName),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("count", len(grants)),
	)

	return connect.NewResponse(&consolev1.ListTemplateGrantsResponse{Grants: grants}), nil
}

func (h *Handler) GetTemplateGrant(
	ctx context.Context,
	req *connect.Request[consolev1.GetTemplateGrantRequest],
) (*connect.Response[consolev1.GetTemplateGrantResponse], error) {
	scope, scopeName, err := h.extractGrantScope(req.Msg.GetNamespace())
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

	grantObj, err := h.k8s.GetGrant(ctx, req.Msg.GetNamespace(), name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template grant read",
		slog.String("action", "template_grant_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.GetTemplateGrantResponse{
		Grant: templateGrantCRDToProto(grantObj),
	}), nil
}

func (h *Handler) CreateTemplateGrant(
	ctx context.Context,
	req *connect.Request[consolev1.CreateTemplateGrantRequest],
) (*connect.Response[consolev1.CreateTemplateGrantResponse], error) {
	scope, scopeName, err := h.extractGrantScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	grant := req.Msg.GetGrant()
	if grant == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("grant is required"))
	}
	if err := validateGrant(req.Msg.GetNamespace(), grant); err != nil {
		return nil, err
	}
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	_, err = h.k8s.CreateGrant(ctx, grant, claims.Email)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template grant created",
		slog.String("action", "template_grant_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope),
		slog.String("scopeName", scopeName),
		slog.String("name", grant.GetName()),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateTemplateGrantResponse{Name: grant.GetName()}), nil
}

func (h *Handler) UpdateTemplateGrant(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateTemplateGrantRequest],
) (*connect.Response[consolev1.UpdateTemplateGrantResponse], error) {
	scope, scopeName, err := h.extractGrantScope(req.Msg.GetNamespace())
	if err != nil {
		return nil, err
	}
	grant := req.Msg.GetGrant()
	if grant == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("grant is required"))
	}
	if err := validateGrant(req.Msg.GetNamespace(), grant); err != nil {
		return nil, err
	}
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if _, err := h.k8s.UpdateGrant(ctx, req.Msg.GetNamespace(), grant); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template grant updated",
		slog.String("action", "template_grant_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope),
		slog.String("scopeName", scopeName),
		slog.String("name", grant.GetName()),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateTemplateGrantResponse{}), nil
}

func (h *Handler) DeleteTemplateGrant(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteTemplateGrantRequest],
) (*connect.Response[consolev1.DeleteTemplateGrantResponse], error) {
	scope, scopeName, err := h.extractGrantScope(req.Msg.GetNamespace())
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

	if err := h.k8s.DeleteGrant(ctx, req.Msg.GetNamespace(), name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "template grant deleted",
		slog.String("action", "template_grant_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("scope", scope),
		slog.String("scopeName", scopeName),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteTemplateGrantResponse{}), nil
}

func (h *Handler) extractGrantScope(namespace string) (string, string, error) {
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
			fmt.Errorf("template grants cannot be stored in project namespace %q; use an organization or folder scope", namespace))
	default:
		return "", "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace must classify as organization or folder"))
	}
	if name == "" {
		return "", "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("scope name is required"))
	}
	return kind, name, nil
}

func validateGrant(reqNamespace string, grant *consolev1.TemplateGrant) error {
	if grant.GetNamespace() == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("grant.namespace is required"))
	}
	if grant.GetNamespace() != reqNamespace {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("grant.namespace (%q) must match request namespace (%q)", grant.GetNamespace(), reqNamespace))
	}
	if err := validateGrantName(grant.GetName()); err != nil {
		return err
	}
	if len(grant.GetFrom()) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("from must have at least one entry"))
	}
	for i, fr := range grant.GetFrom() {
		if fr == nil {
			return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("from[%d]: from_ref is required", i))
		}
		if fr.GetNamespace() == "" {
			return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("from[%d]: namespace is required", i))
		}
	}
	return nil
}

func validateGrantName(name string) error {
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

func templateGrantCRDToProto(g *templatesv1alpha1.TemplateGrant) *consolev1.TemplateGrant {
	if g == nil {
		return nil
	}
	return &consolev1.TemplateGrant{
		Name:         g.Name,
		Namespace:    g.Namespace,
		From:         crdFromRefsToProto(g.Spec.From),
		To:           crdToRefsToProto(g.Spec.To),
		CreatorEmail: g.Annotations[v1alpha2.AnnotationCreatorEmail],
		CreatedAt:    timestamppb.New(g.CreationTimestamp.Time),
		Status:       templateGrantStatusToProto(g.Status),
	}
}

func templateGrantStatusToProto(status templatesv1alpha1.TemplateGrantStatus) *consolev1.TemplateGrantStatus {
	out := &consolev1.TemplateGrantStatus{
		ObservedGeneration: status.ObservedGeneration,
		Conditions:         make([]*consolev1.TemplateGrantCondition, 0, len(status.Conditions)),
	}
	for i := range status.Conditions {
		c := status.Conditions[i]
		out.Conditions = append(out.Conditions, &consolev1.TemplateGrantCondition{
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

func protoFromRefsToCRD(refs []*consolev1.TemplateGrantFromRef) []templatesv1alpha1.TemplateGrantFromRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]templatesv1alpha1.TemplateGrantFromRef, 0, len(refs))
	for _, r := range refs {
		if r == nil {
			continue
		}
		crdRef := templatesv1alpha1.TemplateGrantFromRef{
			Namespace: r.GetNamespace(),
		}
		if sel := r.GetNamespaceSelector(); sel != nil && len(sel.GetMatchLabels()) > 0 {
			crdRef.NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: sel.GetMatchLabels(),
			}
		}
		out = append(out, crdRef)
	}
	return out
}

func crdFromRefsToProto(refs []templatesv1alpha1.TemplateGrantFromRef) []*consolev1.TemplateGrantFromRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]*consolev1.TemplateGrantFromRef, 0, len(refs))
	for i := range refs {
		r := refs[i]
		protoRef := &consolev1.TemplateGrantFromRef{
			Namespace: r.Namespace,
		}
		if r.NamespaceSelector != nil && len(r.NamespaceSelector.MatchLabels) > 0 {
			protoRef.NamespaceSelector = &consolev1.TemplateGrantNamespaceSelector{
				MatchLabels: r.NamespaceSelector.MatchLabels,
			}
		}
		out = append(out, protoRef)
	}
	return out
}

func protoToRefsToCRD(refs []*consolev1.TemplateGrantToRef) []templatesv1alpha1.LinkedTemplateRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]templatesv1alpha1.LinkedTemplateRef, 0, len(refs))
	for _, r := range refs {
		if r == nil {
			continue
		}
		out = append(out, templatesv1alpha1.LinkedTemplateRef{
			Namespace: r.GetNamespace(),
			Name:      r.GetName(),
		})
	}
	return out
}

func crdToRefsToProto(refs []templatesv1alpha1.LinkedTemplateRef) []*consolev1.TemplateGrantToRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]*consolev1.TemplateGrantToRef, 0, len(refs))
	for i := range refs {
		r := refs[i]
		out = append(out, &consolev1.TemplateGrantToRef{
			Namespace: r.Namespace,
			Name:      r.Name,
		})
	}
	return out
}

// mapK8sError delegates to rpc.MapK8sError so the handler shares the
// canonical apierrors -> connect.Code mapping with every other console
// handler.
func mapK8sError(err error) error {
	return rpc.MapK8sError(err)
}
