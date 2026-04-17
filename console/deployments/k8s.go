package deployments

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

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
	client   kubernetes.Interface
	Resolver *resolver.Resolver
}

// NewK8sClient creates a client for deployment operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
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
