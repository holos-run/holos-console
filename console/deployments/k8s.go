package deployments

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/holos-run/holos-console/console/resolver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	ManagedByLabel        = "app.kubernetes.io/managed-by"
	ManagedByValue        = "console.holos.run"
	ResourceTypeLabel     = "console.holos.run/resource-type"
	ResourceTypeValue     = "deployment"
	DisplayNameAnnotation = "console.holos.run/display-name"
	DescriptionAnnotation = "console.holos.run/description"

	// Data keys in the ConfigMap.
	ImageKey    = "image"
	TagKey      = "tag"
	TemplateKey = "template"
	CommandKey  = "command"
	ArgsKey     = "args"
	EnvKey      = "env"
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
	labelSelector := ResourceTypeLabel + "=" + ResourceTypeValue
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
func (k *K8sClient) CreateDeployment(ctx context.Context, project, name, image, tag, tmpl, displayName, description string, command, args []string, env []EnvVarInput) (*corev1.ConfigMap, error) {
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
				ManagedByLabel:    ManagedByValue,
				ResourceTypeLabel: ResourceTypeValue,
			},
			Annotations: map[string]string{
				DisplayNameAnnotation: displayName,
				DescriptionAnnotation: description,
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
	return k.client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
}

// UpdateDeployment updates an existing deployment ConfigMap.
// Only non-nil scalar fields are updated. Non-empty command/args slices replace stored values.
// A non-nil env slice (even if empty) replaces the stored env vars.
func (k *K8sClient) UpdateDeployment(ctx context.Context, project, name string, image, tag, displayName, description *string, command, args []string, env []EnvVarInput) (*corev1.ConfigMap, error) {
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
		cm.Annotations[DisplayNameAnnotation] = *displayName
	}
	if description != nil {
		cm.Annotations[DescriptionAnnotation] = *description
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
	return k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
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
