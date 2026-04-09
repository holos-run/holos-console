package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	CueTemplateKey = "template.cue"
	// DefaultsKey is the ConfigMap data key that stores DeploymentDefaults as JSON.
	DefaultsKey = "defaults.json"
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
	labelSelector := v1alpha1.LabelResourceType + "=" + v1alpha1.ResourceTypeDeploymentTemplate
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
// If defaults is non-nil, it is serialized to JSON and stored under DefaultsKey.
// If linkedOrgTemplates is non-nil and non-empty, it is stored as a JSON array
// in the console.holos.run/linked-org-templates annotation.
func (k *K8sClient) CreateTemplate(ctx context.Context, project, name, displayName, description, cueTemplate string, defaults *consolev1.DeploymentDefaults, linkedOrgTemplates []string) (*corev1.ConfigMap, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "creating deployment template in kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	data := map[string]string{
		CueTemplateKey: cueTemplate,
	}
	if defaults != nil {
		b, err := json.Marshal(defaults)
		if err != nil {
			return nil, fmt.Errorf("serializing deployment defaults: %w", err)
		}
		data[DefaultsKey] = string(b)
	}
	annotations := map[string]string{
		v1alpha1.AnnotationDisplayName: displayName,
		v1alpha1.AnnotationDescription: description,
	}
	if len(linkedOrgTemplates) > 0 {
		b, err := json.Marshal(linkedOrgTemplates)
		if err != nil {
			return nil, fmt.Errorf("serializing linked org templates: %w", err)
		}
		annotations[v1alpha1.AnnotationLinkedOrgTemplates] = string(b)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha1.LabelManagedBy:    v1alpha1.ManagedByValue,
				v1alpha1.LabelResourceType: v1alpha1.ResourceTypeDeploymentTemplate,
			},
			Annotations: annotations,
		},
		Data: data,
	}
	return k.client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
}

// UpdateTemplate updates an existing deployment template ConfigMap.
// Only non-nil fields are updated. If defaults is non-nil, it is serialized to
// JSON and stored under DefaultsKey. If clearDefaults is true, the DefaultsKey
// is removed from the ConfigMap data regardless of the defaults parameter.
// linkedOrgTemplates replaces the entire linking annotation when present (even
// when empty, to allow clearing all links). Pass nil to leave the annotation unchanged.
func (k *K8sClient) UpdateTemplate(ctx context.Context, project, name string, displayName, description, cueTemplate *string, defaults *consolev1.DeploymentDefaults, clearDefaults bool, linkedOrgTemplates []string) (*corev1.ConfigMap, error) {
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
		cm.Annotations[v1alpha1.AnnotationDisplayName] = *displayName
	}
	if description != nil {
		cm.Annotations[v1alpha1.AnnotationDescription] = *description
	}
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	if cueTemplate != nil {
		cm.Data[CueTemplateKey] = *cueTemplate
	}
	if clearDefaults {
		delete(cm.Data, DefaultsKey)
	} else if defaults != nil {
		b, err := json.Marshal(defaults)
		if err != nil {
			return nil, fmt.Errorf("serializing deployment defaults: %w", err)
		}
		cm.Data[DefaultsKey] = string(b)
	}
	// linkedOrgTemplates is non-nil when the caller wants to update the linking
	// list (including clearing it with an empty slice). Nil means "no change".
	if linkedOrgTemplates != nil {
		if len(linkedOrgTemplates) == 0 {
			delete(cm.Annotations, v1alpha1.AnnotationLinkedOrgTemplates)
		} else {
			b, err := json.Marshal(linkedOrgTemplates)
			if err != nil {
				return nil, fmt.Errorf("serializing linked org templates: %w", err)
			}
			cm.Annotations[v1alpha1.AnnotationLinkedOrgTemplates] = string(b)
		}
	}
	return k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
}

// CloneTemplate copies an existing deployment template to a new name.
// The clone inherits the CUE template, description, defaults, and linked org
// templates from the source.
func (k *K8sClient) CloneTemplate(ctx context.Context, project, sourceName, newName, newDisplayName string) (*corev1.ConfigMap, error) {
	source, err := k.GetTemplate(ctx, project, sourceName)
	if err != nil {
		return nil, fmt.Errorf("getting source deployment template for clone: %w", err)
	}
	// Extract defaults from source if present.
	var defaults *consolev1.DeploymentDefaults
	if rawJSON, ok := source.Data[DefaultsKey]; ok && rawJSON != "" {
		var d consolev1.DeploymentDefaults
		if err := json.Unmarshal([]byte(rawJSON), &d); err == nil {
			defaults = &d
		}
	}
	// Inherit linked org templates from source.
	var linkedOrgTemplates []string
	if raw, ok := source.Annotations[v1alpha1.AnnotationLinkedOrgTemplates]; ok && raw != "" {
		_ = json.Unmarshal([]byte(raw), &linkedOrgTemplates)
	}
	return k.CreateTemplate(
		ctx,
		project,
		newName,
		newDisplayName,
		source.Annotations[v1alpha1.AnnotationDescription],
		source.Data[CueTemplateKey],
		defaults,
		linkedOrgTemplates,
	)
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
