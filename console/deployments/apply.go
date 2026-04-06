package deployments

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const (
	// OwnershipLabel tracks which deployment owns a resource.
	OwnershipLabel = "console.holos.run/deployment"
	// fieldManager is used for server-side apply.
	fieldManager = "console.holos.run"
)

// allowedKinds maps Kind → GVR for the resource kinds that may be rendered.
// This mirrors allowedKindSet in render.go.
var allowedKinds = map[string]schema.GroupVersionResource{
	"Deployment":     {Group: "apps", Version: "v1", Resource: "deployments"},
	"Service":        {Group: "", Version: "v1", Resource: "services"},
	"ServiceAccount": {Group: "", Version: "v1", Resource: "serviceaccounts"},
	"Role":           {Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"},
	"RoleBinding":    {Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"},
	"HTTPRoute":      {Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"},
	"ReferenceGrant": {Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "referencegrants"},
	"ConfigMap":      {Group: "", Version: "v1", Resource: "configmaps"},
	"Secret":         {Group: "", Version: "v1", Resource: "secrets"},
}

// Applier creates/updates/deletes K8s resources produced by CUE templates.
type Applier struct {
	client dynamic.Interface
}

// NewApplier creates an Applier using the given dynamic client.
func NewApplier(client dynamic.Interface) *Applier {
	return &Applier{client: client}
}

// Apply performs server-side apply of the rendered manifests, adding the
// ownership label so resources can be cleaned up when the deployment is deleted.
func (a *Applier) Apply(ctx context.Context, namespace, deploymentName string, resources []unstructured.Unstructured) error {
	for i := range resources {
		r := resources[i].DeepCopy()

		// Inject ownership label.
		labels := r.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[OwnershipLabel] = deploymentName
		r.SetLabels(labels)

		kind := r.GetKind()
		gvr, ok := allowedKinds[kind]
		if !ok {
			return fmt.Errorf("unsupported kind %q for resource %s/%s", kind, namespace, r.GetName())
		}

		data, err := json.Marshal(r.Object)
		if err != nil {
			return fmt.Errorf("marshaling resource %s/%s: %w", kind, r.GetName(), err)
		}

		slog.DebugContext(ctx, "applying resource",
			slog.String("kind", kind),
			slog.String("name", r.GetName()),
			slog.String("namespace", namespace),
			slog.String("deployment", deploymentName),
		)

		_, err = a.client.Resource(gvr).Namespace(namespace).Patch(
			ctx,
			r.GetName(),
			types.ApplyPatchType,
			data,
			metav1.PatchOptions{
				FieldManager: fieldManager,
				Force:        boolPtr(true),
			},
		)
		if err != nil {
			return fmt.Errorf("applying %s/%s: %w", kind, r.GetName(), err)
		}
	}
	return nil
}

// Cleanup deletes all K8s resources that carry the deployment ownership label.
func (a *Applier) Cleanup(ctx context.Context, namespace, deploymentName string) error {
	labelSelector := fmt.Sprintf("%s=%s", OwnershipLabel, deploymentName)

	for kind, gvr := range allowedKinds {
		list, err := a.client.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			// Some GVRs may not exist in the cluster; log and continue.
			slog.DebugContext(ctx, "cleanup: list error (resource type may not exist)",
				slog.String("kind", kind),
				slog.String("namespace", namespace),
				slog.Any("error", err),
			)
			continue
		}

		for _, item := range list.Items {
			slog.InfoContext(ctx, "cleanup: deleting owned resource",
				slog.String("kind", kind),
				slog.String("name", item.GetName()),
				slog.String("namespace", namespace),
				slog.String("deployment", deploymentName),
			)
			if err := a.client.Resource(gvr).Namespace(namespace).Delete(
				ctx, item.GetName(), metav1.DeleteOptions{}); err != nil {
				return fmt.Errorf("deleting %s/%s: %w", kind, item.GetName(), err)
			}
		}
	}
	return nil
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool { return &b }
