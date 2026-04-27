// Package templatepolicybindings — K8sClient storage layer.
//
// HOL-662 rewrote this file to type the TemplatePolicyBinding CRUD surface
// against the templates.holos.run/v1alpha1 TemplatePolicyBinding CRD and
// read/write through a controller-runtime client.Client. Reads hit the
// informer cache the controller manager populates; writes fall through to
// the API server and the cache observes them on the next watch event.
//
// Signature shape: every method takes a Kubernetes namespace and a resource
// name. The namespace is the authoritative identifier per HOL-619; callers
// that still think in terms of (scope, scopeName) compute the namespace via
// the package-level resolver shim in the handler.
//
// The CEL ValidatingAdmissionPolicy shipped alongside the CRDs (HOL-618)
// rejects TemplatePolicyBinding creation in a project-labelled namespace at
// admission time, so the handler's extractBindingScope is the only
// defense-in-depth guard the client-side code needs to keep.
package templatepolicybindings

import (
	"context"
	"fmt"
	"log/slog"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgocache "k8s.io/client-go/tools/cache"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// K8sClient wraps TemplatePolicyBinding CRUD operations against the CRD.
//
// Reads hit the controller-runtime cache; writes fall through to the API
// server and the cache learns about them on the next watch event.
//
// HOL-622: when Cache is non-nil, ListBindingsInNamespace uses the shared
// informer's indexer directly to return pointers to cache-owned objects
// without the DeepCopy the delegating client performs.
type K8sClient struct {
	client   ctrlclient.Client
	cache    ctrlcache.Cache
	Resolver *resolver.Resolver
}

// NewK8sClient returns a K8sClient bound to a controller-runtime client.Client
// and a resolver. Production wiring passes the cache-backed client from the
// embedded controller manager; tests may pass a fake ctrlclient or a direct
// envtest-backed client.
func NewK8sClient(client ctrlclient.Client, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

func (k *K8sClient) requestClient(ctx context.Context) ctrlclient.Client {
	if cl := rpc.ImpersonatedCtrlClientFromContext(ctx); cl != nil && rpc.HasImpersonatedClients(ctx) {
		return cl
	}
	return k.client
}

// WithCache wires the shared informer cache onto the K8sClient so the
// hot-path ListBindingsInNamespace call used by the policy resolver can
// retrieve pointers to cache-owned TemplatePolicyBinding objects instead of
// paying the delegating client's DeepCopy per List. Production wiring from
// console.go passes mgr.GetCache().
func (k *K8sClient) WithCache(c ctrlcache.Cache) *K8sClient {
	k.cache = c
	return k
}

// ListBindings returns every TemplatePolicyBinding in the given namespace.
func (k *K8sClient) ListBindings(ctx context.Context, namespace string) ([]templatesv1alpha1.TemplatePolicyBinding, error) {
	slog.DebugContext(ctx, "listing template policy bindings from kubernetes",
		slog.String("namespace", namespace),
	)
	var list templatesv1alpha1.TemplatePolicyBindingList
	if err := k.client.List(ctx, &list, ctrlclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing template policy bindings in %q: %w", namespace, err)
	}
	if !rpc.HasImpersonatedClients(ctx) {
		return list.Items, nil
	}
	out := make([]templatesv1alpha1.TemplatePolicyBinding, 0, len(list.Items))
	client := k.requestClient(ctx)
	for i := range list.Items {
		var got templatesv1alpha1.TemplatePolicyBinding
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

// ListBindingsInNamespace returns every TemplatePolicyBinding in a namespace
// as a slice of pointers. The policyresolver binding walker (HOL-596) calls
// this per ancestor namespace on every render-time resolve.
//
// HOL-622 hot-path contract: when the K8sClient has been wired with a shared
// informer cache (WithCache), reads go through the indexer's NamespaceIndex
// and return pointers that reference cache-owned objects directly — no
// DeepCopy. The resolver treats the returned pointers as read-only.
//
// When no cache is wired we fall back to the delegating client.List which
// returns a freshly-decoded value slice.
func (k *K8sClient) ListBindingsInNamespace(ctx context.Context, namespace string) ([]*templatesv1alpha1.TemplatePolicyBinding, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	slog.DebugContext(ctx, "listing template policy bindings for resolver",
		slog.String("namespace", namespace),
		slog.Bool("cache", k.cache != nil),
	)
	if k.cache != nil {
		informer, err := k.cache.GetInformer(ctx, &templatesv1alpha1.TemplatePolicyBinding{})
		if err != nil {
			return nil, fmt.Errorf("getting template policy binding informer: %w", err)
		}
		si, ok := informer.(clientgocache.SharedIndexInformer)
		if !ok {
			return nil, fmt.Errorf("template policy binding informer is not a SharedIndexInformer (got %T)", informer)
		}
		raws, err := si.GetIndexer().ByIndex(clientgocache.NamespaceIndex, namespace)
		if err != nil {
			return nil, fmt.Errorf("listing template policy bindings via indexer in %q: %w", namespace, err)
		}
		out := make([]*templatesv1alpha1.TemplatePolicyBinding, 0, len(raws))
		for _, raw := range raws {
			b, ok := raw.(*templatesv1alpha1.TemplatePolicyBinding)
			if !ok {
				return nil, fmt.Errorf("indexer returned unexpected type %T for TemplatePolicyBinding", raw)
			}
			out = append(out, b)
		}
		return out, nil
	}
	items, err := k.ListBindings(ctx, namespace)
	if err != nil {
		return nil, err
	}
	out := make([]*templatesv1alpha1.TemplatePolicyBinding, 0, len(items))
	for i := range items {
		out = append(out, &items[i])
	}
	return out, nil
}

// GetBinding retrieves a single TemplatePolicyBinding by name.
func (k *K8sClient) GetBinding(ctx context.Context, namespace, name string) (*templatesv1alpha1.TemplatePolicyBinding, error) {
	slog.DebugContext(ctx, "getting template policy binding from kubernetes",
		slog.String("namespace", namespace),
		slog.String("name", name),
	)
	var b templatesv1alpha1.TemplatePolicyBinding
	if err := k.requestClient(ctx).Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// CreateBinding creates a new TemplatePolicyBinding CRD. Policy ref and
// target refs are stored as structured spec fields; HOL-618 removed the JSON
// annotation wire format. creatorEmail is recorded as an annotation because
// the CRD spec has no field for it.
func (k *K8sClient) CreateBinding(
	ctx context.Context,
	namespace, name, displayName, description, creatorEmail string,
	policyRef *consolev1.LinkedTemplatePolicyRef,
	targetRefs []*consolev1.TemplatePolicyBindingTargetRef,
) (*templatesv1alpha1.TemplatePolicyBinding, error) {
	slog.DebugContext(ctx, "creating template policy binding in kubernetes",
		slog.String("namespace", namespace),
		slog.String("name", name),
	)
	b := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicyBinding,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationCreatorEmail: creatorEmail,
			},
		},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			DisplayName: displayName,
			Description: description,
			PolicyRef:   protoPolicyRefToCRD(policyRef),
			TargetRefs:  protoTargetRefsToCRD(targetRefs),
		},
	}
	if err := k.requestClient(ctx).Create(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

// UpdateBinding mutates the addressable spec fields of an existing
// TemplatePolicyBinding. displayName/description follow nil-for-"leave alone"
// semantics; policy_ref / target_refs replace when their update booleans are
// true so the caller can intentionally set an empty target list.
func (k *K8sClient) UpdateBinding(
	ctx context.Context,
	namespace, name string,
	displayName, description *string,
	policyRef *consolev1.LinkedTemplatePolicyRef,
	updatePolicyRef bool,
	targetRefs []*consolev1.TemplatePolicyBindingTargetRef,
	updateTargetRefs bool,
) (*templatesv1alpha1.TemplatePolicyBinding, error) {
	slog.DebugContext(ctx, "updating template policy binding in kubernetes",
		slog.String("namespace", namespace),
		slog.String("name", name),
	)
	b, err := k.GetBinding(ctx, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("getting template policy binding for update: %w", err)
	}
	if displayName != nil {
		b.Spec.DisplayName = *displayName
	}
	if description != nil {
		b.Spec.Description = *description
	}
	if updatePolicyRef {
		b.Spec.PolicyRef = protoPolicyRefToCRD(policyRef)
	}
	if updateTargetRefs {
		b.Spec.TargetRefs = protoTargetRefsToCRD(targetRefs)
	}
	if err := k.requestClient(ctx).Update(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

// DeleteBinding deletes a TemplatePolicyBinding by name.
func (k *K8sClient) DeleteBinding(ctx context.Context, namespace, name string) error {
	slog.DebugContext(ctx, "deleting template policy binding from kubernetes",
		slog.String("namespace", namespace),
		slog.String("name", name),
	)
	b := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	return k.requestClient(ctx).Delete(ctx, b)
}

// protoPolicyRefToCRD converts the proto LinkedTemplatePolicyRef into the
// CRD's structured equivalent. Both sides carry the flat (namespace, name)
// pair post-HOL-723.
func protoPolicyRefToCRD(ref *consolev1.LinkedTemplatePolicyRef) templatesv1alpha1.LinkedTemplatePolicyRef {
	if ref == nil {
		return templatesv1alpha1.LinkedTemplatePolicyRef{}
	}
	return templatesv1alpha1.LinkedTemplatePolicyRef{
		Namespace: ref.GetNamespace(),
		Name:      ref.GetName(),
	}
}

// CRDPolicyRefToProto is the inverse of protoPolicyRefToCRD. Exported for
// policyresolver and the handler's CRD → proto conversion path.
func CRDPolicyRefToProto(ref templatesv1alpha1.LinkedTemplatePolicyRef) *consolev1.LinkedTemplatePolicyRef {
	if ref.Name == "" && ref.Namespace == "" {
		return nil
	}
	return &consolev1.LinkedTemplatePolicyRef{
		Namespace: ref.Namespace,
		Name:      ref.Name,
	}
}

func protoTargetRefsToCRD(refs []*consolev1.TemplatePolicyBindingTargetRef) []templatesv1alpha1.TemplatePolicyBindingTargetRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]templatesv1alpha1.TemplatePolicyBindingTargetRef, 0, len(refs))
	for _, r := range refs {
		if r == nil {
			continue
		}
		out = append(out, templatesv1alpha1.TemplatePolicyBindingTargetRef{
			Kind:        targetKindProtoToCRD(r.GetKind()),
			Name:        r.GetName(),
			ProjectName: r.GetProjectName(),
		})
	}
	return out
}

// CRDTargetRefsToProto converts CRD spec target refs into proto values.
// Exported for policyresolver and the handler's CRD → proto conversion.
func CRDTargetRefsToProto(refs []templatesv1alpha1.TemplatePolicyBindingTargetRef) []*consolev1.TemplatePolicyBindingTargetRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]*consolev1.TemplatePolicyBindingTargetRef, 0, len(refs))
	for i := range refs {
		r := &refs[i]
		out = append(out, &consolev1.TemplatePolicyBindingTargetRef{
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
	case consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_NAMESPACE:
		return templatesv1alpha1.TemplatePolicyBindingTargetKindProjectNamespace
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
	case templatesv1alpha1.TemplatePolicyBindingTargetKindProjectNamespace:
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_NAMESPACE
	default:
		return consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_UNSPECIFIED
	}
}
