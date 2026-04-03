package templates

import (
	"context"
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
	ResourceTypeValue     = "deployment-template"
	DisplayNameAnnotation = "console.holos.run/display-name"
	DescriptionAnnotation = "console.holos.run/description"
	CueTemplateKey        = "template.cue"
)

// K8sClient wraps Kubernetes client operations for deployment templates.
type K8sClient struct {
	client   kubernetes.Interface
	Resolver *resolver.Resolver
}

// NewK8sClient creates a client for deployment template operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

// ListTemplates returns all deployment template ConfigMaps in the project namespace.
func (k *K8sClient) ListTemplates(ctx context.Context, project string) ([]corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	labelSelector := ResourceTypeLabel + "=" + ResourceTypeValue
	slog.DebugContext(ctx, "listing deployment templates from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("labelSelector", labelSelector),
	)
	list, err := k.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing deployment templates: %w", err)
	}
	return list.Items, nil
}

// GetTemplate retrieves a deployment template ConfigMap by name.
func (k *K8sClient) GetTemplate(ctx context.Context, project, name string) (*corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "getting deployment template from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
}

// CreateTemplate creates a new deployment template ConfigMap.
func (k *K8sClient) CreateTemplate(ctx context.Context, project, name, displayName, description, cueTemplate string) (*corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "creating deployment template in kubernetes",
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
			CueTemplateKey: cueTemplate,
		},
	}
	return k.client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
}

// UpdateTemplate updates an existing deployment template ConfigMap.
// Only non-nil fields are updated.
func (k *K8sClient) UpdateTemplate(ctx context.Context, project, name string, displayName, description, cueTemplate *string) (*corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "updating deployment template in kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting deployment template for update: %w", err)
	}
	if cm.Annotations == nil {
		cm.Annotations = make(map[string]string)
	}
	if displayName != nil {
		cm.Annotations[DisplayNameAnnotation] = *displayName
	}
	if description != nil {
		cm.Annotations[DescriptionAnnotation] = *description
	}
	if cueTemplate != nil {
		if cm.Data == nil {
			cm.Data = make(map[string]string)
		}
		cm.Data[CueTemplateKey] = *cueTemplate
	}
	return k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
}

// DeleteTemplate deletes a deployment template ConfigMap.
func (k *K8sClient) DeleteTemplate(ctx context.Context, project, name string) error {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "deleting deployment template from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{})
}
