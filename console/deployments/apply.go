package deployments

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const (
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

// Apply performs server-side apply of the rendered manifests, adding ownership
// labels so resources can be cleaned up when the deployment is deleted.
// Each resource is applied to its own metadata.namespace. Cluster-scoped
// resources (namespace == "") use the unnamespaced client.
//
// Both the project and deployment name are stamped as labels so that
// Reconcile and Cleanup can scope queries to a single project, preventing
// cross-project collisions in shared namespaces.
func (a *Applier) Apply(ctx context.Context, project, deploymentName string, resources []unstructured.Unstructured) error {
	for i := range resources {
		r := resources[i].DeepCopy()

		// Inject ownership labels (project + deployment).
		labels := r.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[v1alpha2.LabelProject] = project
		labels[v1alpha2.AnnotationDeployment] = deploymentName
		r.SetLabels(labels)

		kind := r.GetKind()
		gvr, ok := allowedKinds[kind]
		if !ok {
			return fmt.Errorf("unsupported kind %q for resource %s", kind, r.GetName())
		}

		data, err := json.Marshal(r.Object)
		if err != nil {
			return fmt.Errorf("marshaling resource %s/%s: %w", kind, r.GetName(), err)
		}

		namespace := r.GetNamespace()
		slog.DebugContext(ctx, "applying resource",
			slog.String("kind", kind),
			slog.String("name", r.GetName()),
			slog.String("namespace", namespace),
			slog.String("deployment", deploymentName),
		)

		// Use the namespaced or cluster-scoped client depending on the resource.
		var rc dynamic.ResourceInterface
		if namespace != "" {
			rc = a.client.Resource(gvr).Namespace(namespace)
		} else {
			rc = a.client.Resource(gvr)
		}

		_, err = rc.Patch(
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

// Reconcile applies the desired resources via SSA and then deletes any owned
// resources that are no longer in the desired set (orphan cleanup). This
// implements a K8s controller-style reconciliation loop:
//  1. Apply all desired resources.
//  2. If apply fails, return the error immediately — orphan cleanup is skipped
//     to preserve the previously working state.
//  3. After a successful apply, scan the union of desired namespaces and
//     previousNamespaces for orphans. Delete any owned resource whose
//     (kind, namespace, name) tuple is not in the desired set.
//
// previousNamespaces should contain namespaces that previously held resources
// for this deployment (e.g. derived from the last successful render). This
// ensures orphans are cleaned up when resources move between namespaces.
func (a *Applier) Reconcile(ctx context.Context, project, deploymentName string, resources []unstructured.Unstructured, previousNamespaces ...string) error {
	// Step 1: Apply all desired resources via SSA.
	if err := a.Apply(ctx, project, deploymentName, resources); err != nil {
		return err
	}

	// Build a set of (kind, namespace, name) tuples from the desired resources
	// so we can quickly check whether a cluster resource is still wanted.
	type kindNsName struct{ kind, namespace, name string }
	desired := make(map[kindNsName]struct{}, len(resources))
	scanNS := make(map[string]struct{})
	for _, r := range resources {
		desired[kindNsName{kind: r.GetKind(), namespace: r.GetNamespace(), name: r.GetName()}] = struct{}{}
		if ns := r.GetNamespace(); ns != "" {
			scanNS[ns] = struct{}{}
		}
	}
	// Include previous namespaces in the scan set so orphans from namespace
	// moves are detected and cleaned up.
	for _, ns := range previousNamespaces {
		if ns != "" {
			scanNS[ns] = struct{}{}
		}
	}

	// Step 2: Delete orphaned resources — those with the ownership labels that
	// are no longer in the desired set. Scan the union of desired + previous
	// namespaces. The selector includes both project and deployment to avoid
	// cross-project collisions in shared namespaces.
	labelSelector := fmt.Sprintf("%s=%s,%s=%s",
		v1alpha2.LabelProject, project,
		v1alpha2.AnnotationDeployment, deploymentName)

	for kind, gvr := range allowedKinds {
		for namespace := range scanNS {
			list, err := a.client.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			if err != nil {
				// Some GVRs may not exist in the cluster; log and continue.
				slog.DebugContext(ctx, "reconcile: list error (resource type may not exist)",
					slog.String("kind", kind),
					slog.String("namespace", namespace),
					slog.Any("error", err),
				)
				continue
			}

			for _, item := range list.Items {
				if _, ok := desired[kindNsName{kind: kind, namespace: namespace, name: item.GetName()}]; ok {
					continue // still desired; keep it
				}
				slog.InfoContext(ctx, "reconcile: deleting orphaned resource",
					slog.String("kind", kind),
					slog.String("name", item.GetName()),
					slog.String("namespace", namespace),
					slog.String("deployment", deploymentName),
				)
				if err := a.client.Resource(gvr).Namespace(namespace).Delete(
					ctx, item.GetName(), metav1.DeleteOptions{}); err != nil {
					return fmt.Errorf("deleting orphaned %s/%s in %s: %w", kind, item.GetName(), namespace, err)
				}
			}
		}
	}
	return nil
}

// Cleanup deletes all K8s resources that carry the deployment ownership labels
// across all provided namespaces. The selector includes both project and
// deployment name to avoid cross-project collisions in shared namespaces.
func (a *Applier) Cleanup(ctx context.Context, namespaces []string, project, deploymentName string) error {
	labelSelector := fmt.Sprintf("%s=%s,%s=%s",
		v1alpha2.LabelProject, project,
		v1alpha2.AnnotationDeployment, deploymentName)

	for kind, gvr := range allowedKinds {
		for _, namespace := range namespaces {
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
					return fmt.Errorf("deleting %s/%s in %s: %w", kind, item.GetName(), namespace, err)
				}
			}
		}
	}
	return nil
}

// ResourceNamespaces extracts the unique set of namespaces from the given
// resources. This is used by callers to derive the namespace set for Cleanup
// and Reconcile operations.
func ResourceNamespaces(resources []unstructured.Unstructured) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, r := range resources {
		ns := r.GetNamespace()
		if ns == "" {
			continue
		}
		if _, ok := seen[ns]; !ok {
			seen[ns] = struct{}{}
			result = append(result, ns)
		}
	}
	return result
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool { return &b }
