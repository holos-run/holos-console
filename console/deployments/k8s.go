package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// ErrPartialScan is returned by ListDeploymentResources when at least one
// per-kind List call failed. The returned slice still contains every
// resource the successful kinds produced — callers may surface a partial
// view — but the error signals "do not treat this slice as authoritative
// drift evidence" so cache-rewrite paths can preserve their existing
// state instead of wiping it on a transient failure.
var ErrPartialScan = errors.New("list deployment resources: partial scan")

const (
	// Data keys in the ConfigMap.
	ImageKey    = "image"
	TagKey      = "tag"
	TemplateKey = "template"
	CommandKey  = "command"
	ArgsKey     = "args"
	EnvKey      = "env"
	PortKey     = "port"
)

// K8sClient wraps Kubernetes client operations for deployments.
type K8sClient struct {
	client kubernetes.Interface
	// dynamic, when non-nil, enables multi-kind queries used by the link
	// aggregator (HOL-574) to scan resources owned by a deployment across
	// every kind apply.go writes. A nil dynamic client makes
	// ListDeploymentResources a no-op so local/dev wiring without a cluster
	// dynamic client (and unit tests that only need typed reads) keep
	// working.
	dynamic  dynamic.Interface
	Resolver *resolver.Resolver
}

// NewK8sClient creates a client for deployment operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

// WithDynamicClient configures the K8sClient with a dynamic client used by
// ListDeploymentResources to scan owned resources across every allowed kind
// (HOL-574). Returns the receiver for fluent chaining alongside the existing
// constructor so callers do not have to thread a new positional arg through
// every test that builds a K8sClient.
func (k *K8sClient) WithDynamicClient(d dynamic.Interface) *K8sClient {
	k.dynamic = d
	return k
}

// HasDynamicClient reports whether a dynamic client is configured. The link
// aggregator needs this to distinguish "scan returned zero resources"
// (legitimate empty drift — clear the cache) from "no dynamic client"
// (cannot scan at all — preserve cache as-is). Without the distinction the
// GetDeployment refresh path would either never clear stale entries
// (always preserve on empty) or wrongly wipe them on local/dev wiring
// where the dynamic client is intentionally nil.
func (k *K8sClient) HasDynamicClient() bool {
	return k.dynamic != nil
}

// ListDeployments returns all deployment ConfigMaps in the project namespace.
func (k *K8sClient) ListDeployments(ctx context.Context, project string) ([]corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	labelSelector := v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeDeployment
	slog.DebugContext(ctx, "listing deployments from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("labelSelector", labelSelector),
	)
	list, err := k.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	return list.Items, nil
}

// GetDeployment retrieves a deployment ConfigMap by name.
func (k *K8sClient) GetDeployment(ctx context.Context, project, name string) (*corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "getting deployment from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
}

// CreateDeployment creates a new deployment ConfigMap.
func (k *K8sClient) CreateDeployment(ctx context.Context, project, name, image, tag, tmpl, displayName, description string, command, args []string, env []v1alpha2.EnvVar, port int32) (*corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "creating deployment in kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeDeployment,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: displayName,
				v1alpha2.AnnotationDescription: description,
			},
		},
		Data: map[string]string{
			ImageKey:    image,
			TagKey:      tag,
			TemplateKey: tmpl,
		},
	}
	if len(command) > 0 {
		b, _ := json.Marshal(command)
		cm.Data[CommandKey] = string(b)
	}
	if len(args) > 0 {
		b, _ := json.Marshal(args)
		cm.Data[ArgsKey] = string(b)
	}
	if len(env) > 0 {
		b, _ := json.Marshal(env)
		cm.Data[EnvKey] = string(b)
	}
	if port > 0 {
		cm.Data[PortKey] = strconv.Itoa(int(port))
	}
	return k.client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
}

// UpdateDeployment updates an existing deployment ConfigMap.
// Only non-nil scalar fields are updated. Non-empty command/args slices replace stored values.
// A non-nil env slice (even if empty) replaces the stored env vars.
// A non-nil port pointer updates the stored port value.
func (k *K8sClient) UpdateDeployment(ctx context.Context, project, name string, image, tag, displayName, description *string, command, args []string, env []v1alpha2.EnvVar, port *int32) (*corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "updating deployment in kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting deployment for update: %w", err)
	}
	if cm.Annotations == nil {
		cm.Annotations = make(map[string]string)
	}
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	if image != nil {
		cm.Data[ImageKey] = *image
	}
	if tag != nil {
		cm.Data[TagKey] = *tag
	}
	if displayName != nil {
		cm.Annotations[v1alpha2.AnnotationDisplayName] = *displayName
	}
	if description != nil {
		cm.Annotations[v1alpha2.AnnotationDescription] = *description
	}
	if len(command) > 0 {
		b, _ := json.Marshal(command)
		cm.Data[CommandKey] = string(b)
	}
	if len(args) > 0 {
		b, _ := json.Marshal(args)
		cm.Data[ArgsKey] = string(b)
	}
	if env != nil {
		b, _ := json.Marshal(env)
		cm.Data[EnvKey] = string(b)
	}
	if port != nil {
		if *port > 0 {
			cm.Data[PortKey] = strconv.Itoa(int(*port))
		} else {
			delete(cm.Data, PortKey)
		}
	}
	return k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
}

// ListDeploymentResources returns every resource currently owned by the given
// deployment within the project namespace, scanned across every kind
// apply.go writes. The lookup uses the same
// `LabelProject=<project>,console.holos.run/deployment=<deployment>`
// selector applied at apply time so results are exactly the in-namespace
// subset Reconcile and Cleanup operate on. Returned objects are the live
// cluster representation — each carries its own annotations, labels,
// kind, namespace, and name — and are intended to be passed straight to
// links.ParseAnnotations by the aggregator (HOL-574).
//
// Scope: namespace-scoped (the project namespace returned by Resolver).
// This deliberately matches the existing console RBAC posture, which is
// namespaced — a cluster-wide list would silently fail in any cluster
// where the console service account does not have cluster-level list
// permissions, dropping links without a visible error and leaving the
// "RBAC unchanged" guarantee unmet. Cross-namespace owned resources
// (e.g. an HTTPRoute landing in istio-ingress) are intentionally not
// scanned here; templates that want to surface links from cross-
// namespace resources should attach `external-link.*` / `primary-url`
// annotations to a project-namespace resource instead.
//
// Determinism: kinds are iterated in lexicographic GVR order so the
// resource slice — and therefore the first-wins selection in the
// aggregator (de-duplication and `primary-url` promotion) — is stable
// across requests. Iterating the `allowedKinds` map directly would
// scramble the order on every call and cause cached values to flap
// even when the live cluster did not change (HOL-574 review round 2 P2).
//
// Partial-failure handling: if any per-kind list fails (transient API
// error, missing optional CRD, RBAC gap on a single resource type) the
// successful items are still returned but the error wraps `ErrPartialScan`
// so callers can preserve their cached state instead of treating an
// incomplete view as authoritative drift (HOL-574 review round 2 P1).
//
// When no dynamic client is configured the method returns (nil, nil) so
// the handler degrades gracefully on local/dev wiring without a cluster.
func (k *K8sClient) ListDeploymentResources(ctx context.Context, project, deployment string) ([]unstructured.Unstructured, error) {
	if k.dynamic == nil {
		return nil, nil
	}
	if project == "" || deployment == "" {
		return nil, fmt.Errorf("project and deployment are required")
	}
	ns := k.Resolver.ProjectNamespace(project)
	labelSelector := fmt.Sprintf("%s=%s,%s=%s",
		v1alpha2.LabelProject, project,
		v1alpha2.AnnotationDeployment, deployment)

	// Walk allowedKinds in a deterministic order so the aggregator's
	// first-wins de-duplication is stable across calls.
	kinds := make([]string, 0, len(allowedKinds))
	for kind := range allowedKinds {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	var out []unstructured.Unstructured
	var listErrors []error
	for _, kind := range kinds {
		gvr := allowedKinds[kind]
		list, err := k.dynamic.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			// Some optional CRDs may not be installed and some
			// transient errors are recoverable; record and continue
			// rather than aborting the whole aggregator (mirrors the
			// apply.go Cleanup / Reconcile orphan-scan precedent of
			// skip-and-log on per-kind list failures). Track the
			// error so the caller can downgrade authority on the
			// returned slice (see ErrPartialScan).
			slog.DebugContext(ctx, "list deployment resources: skipping kind",
				slog.String("kind", kind),
				slog.String("namespace", ns),
				slog.String("project", project),
				slog.String("deployment", deployment),
				slog.Any("error", err),
			)
			listErrors = append(listErrors, fmt.Errorf("listing %s: %w", kind, err))
			continue
		}
		out = append(out, list.Items...)
	}
	if len(listErrors) > 0 {
		return out, fmt.Errorf("%w (%d/%d kinds failed): %w",
			ErrPartialScan, len(listErrors), len(allowedKinds), listErrors[0])
	}
	return out, nil
}

// SetAggregatedLinksAnnotation sets (or clears) the aggregated-links cache
// annotation on the deployment ConfigMap. An empty payload removes the
// annotation so stale link sets from previous renders do not persist when a
// template edit drops every link source. A missing ConfigMap surfaces the
// underlying NotFound so the handler can decide whether to log or surface
// the error. Mirrors SetOutputURLAnnotation exactly so the two cached
// surfaces share one operational shape (HOL-574).
func (k *K8sClient) SetAggregatedLinksAnnotation(ctx context.Context, project, name, payload string) error {
	ns := k.Resolver.ProjectNamespace(project)
	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting deployment for aggregated-links annotation update: %w", err)
	}
	if cm.Annotations == nil {
		cm.Annotations = map[string]string{}
	}
	if payload == "" {
		if _, ok := cm.Annotations[v1alpha2.AnnotationAggregatedLinks]; !ok {
			// No-op: annotation not present and nothing to clear.
			return nil
		}
		delete(cm.Annotations, v1alpha2.AnnotationAggregatedLinks)
	} else {
		if cm.Annotations[v1alpha2.AnnotationAggregatedLinks] == payload {
			// Already up to date; avoid a needless write.
			return nil
		}
		cm.Annotations[v1alpha2.AnnotationAggregatedLinks] = payload
	}
	_, err = k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating deployment aggregated-links annotation: %w", err)
	}
	return nil
}

// SetOutputURLAnnotation sets (or clears) the output-url annotation on the
// deployment ConfigMap. An empty url removes the annotation so stale links
// from previous renders do not persist when a template edit drops the
// output block. A missing ConfigMap is returned as-is so the handler can
// decide whether to log or surface the error.
func (k *K8sClient) SetOutputURLAnnotation(ctx context.Context, project, name, url string) error {
	ns := k.Resolver.ProjectNamespace(project)
	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting deployment for annotation update: %w", err)
	}
	if cm.Annotations == nil {
		cm.Annotations = map[string]string{}
	}
	if url == "" {
		if _, ok := cm.Annotations[OutputURLAnnotation]; !ok {
			// No-op: annotation not present and nothing to clear.
			return nil
		}
		delete(cm.Annotations, OutputURLAnnotation)
	} else {
		if cm.Annotations[OutputURLAnnotation] == url {
			// Already up to date; avoid a needless write.
			return nil
		}
		cm.Annotations[OutputURLAnnotation] = url
	}
	_, err = k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating deployment output-url annotation: %w", err)
	}
	return nil
}

// DeleteDeployment deletes a deployment ConfigMap.
func (k *K8sClient) DeleteDeployment(ctx context.Context, project, name string) error {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "deleting deployment from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{})
}

// NamespaceResourceItem holds a resource name and its sorted data keys.
type NamespaceResourceItem struct {
	Name string
	Keys []string
}

// ListNamespaceSecrets lists all Secrets in the project namespace, excluding
// service-account-token type secrets which are not user data.
func (k *K8sClient) ListNamespaceSecrets(ctx context.Context, project string) ([]NamespaceResourceItem, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "listing secrets from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
	)
	list, err := k.client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing secrets: %w", err)
	}
	result := make([]NamespaceResourceItem, 0, len(list.Items))
	for _, s := range list.Items {
		if s.Type == corev1.SecretTypeServiceAccountToken {
			continue
		}
		keys := make([]string, 0, len(s.Data))
		for k := range s.Data {
			keys = append(keys, k)
		}
		result = append(result, NamespaceResourceItem{Name: s.Name, Keys: keys})
	}
	return result, nil
}

// ListNamespaceConfigMaps lists all ConfigMaps in the project namespace,
// excluding console-managed ones (those with the console.holos.run/resource-type label).
func (k *K8sClient) ListNamespaceConfigMaps(ctx context.Context, project string) ([]NamespaceResourceItem, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "listing configmaps from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
	)
	list, err := k.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing configmaps: %w", err)
	}
	result := make([]NamespaceResourceItem, 0, len(list.Items))
	for _, cm := range list.Items {
		if _, isConsoleManagedResource := cm.Labels[v1alpha2.LabelResourceType]; isConsoleManagedResource {
			continue
		}
		keys := make([]string, 0, len(cm.Data))
		for k := range cm.Data {
			keys = append(keys, k)
		}
		result = append(result, NamespaceResourceItem{Name: cm.Name, Keys: keys})
	}
	return result, nil
}
