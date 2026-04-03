package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	SettingsConfigMapName = "project-settings"
	ManagedByLabel        = "app.kubernetes.io/managed-by"
	ManagedByValue        = "console.holos.run"
	ResourceTypeLabel     = "console.holos.run/resource-type"
	ResourceTypeValue     = "project-settings"
	SettingsDataKey       = "settings.json"
)

// K8sClient wraps Kubernetes client operations for project settings.
type K8sClient struct {
	client   kubernetes.Interface
	Resolver *resolver.Resolver
}

// NewK8sClient creates a client for project settings operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

// DefaultSettings returns settings with deployments_enabled=true.
func DefaultSettings(project string) *consolev1.ProjectSettings {
	return &consolev1.ProjectSettings{
		Project:            project,
		DeploymentsEnabled: true,
	}
}

// GetSettings returns the settings ConfigMap, or DefaultSettings if not found.
func (k *K8sClient) GetSettings(ctx context.Context, project string) (*consolev1.ProjectSettings, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "getting project settings from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
	)

	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, SettingsConfigMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return DefaultSettings(project), nil
		}
		return nil, fmt.Errorf("getting settings configmap: %w", err)
	}

	return parseSettings(cm, project)
}

// UpdateSettings creates or updates the settings ConfigMap.
func (k *K8sClient) UpdateSettings(ctx context.Context, settings *consolev1.ProjectSettings) (*consolev1.ProjectSettings, error) {
	ns := k.Resolver.ProjectNamespace(settings.Project)
	slog.DebugContext(ctx, "updating project settings in kubernetes",
		slog.String("project", settings.Project),
		slog.String("namespace", ns),
	)

	data, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("marshaling settings: %w", err)
	}

	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, SettingsConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("getting settings configmap: %w", err)
		}
		// Create new ConfigMap
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      SettingsConfigMapName,
				Namespace: ns,
				Labels: map[string]string{
					ManagedByLabel:    ManagedByValue,
					ResourceTypeLabel: ResourceTypeValue,
				},
			},
			Data: map[string]string{
				SettingsDataKey: string(data),
			},
		}
		if _, err := k.client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("creating settings configmap: %w", err)
		}
		return settings, nil
	}

	// Update existing ConfigMap
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[SettingsDataKey] = string(data)
	if _, err := k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("updating settings configmap: %w", err)
	}
	return settings, nil
}

// parseSettings reads ProjectSettings from a ConfigMap's data.
func parseSettings(cm *corev1.ConfigMap, project string) (*consolev1.ProjectSettings, error) {
	raw, ok := cm.Data[SettingsDataKey]
	if !ok {
		return DefaultSettings(project), nil
	}
	settings := &consolev1.ProjectSettings{}
	if err := json.Unmarshal([]byte(raw), settings); err != nil {
		return nil, fmt.Errorf("parsing settings JSON: %w", err)
	}
	// Always set project from the request context, not from stored data
	settings.Project = project
	return settings, nil
}
