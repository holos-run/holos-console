package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

// DefaultSettings returns settings with deployments_enabled=false.
func DefaultSettings(project string) *consolev1.ProjectSettings {
	return &consolev1.ProjectSettings{
		Project:            project,
		DeploymentsEnabled: false,
	}
}

// GetSettings reads the settings annotation from the project Namespace.
// Returns DefaultSettings if no annotation is present.
func (k *K8sClient) GetSettings(ctx context.Context, project string) (*consolev1.ProjectSettings, error) {
	ns := k.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "getting project settings from namespace annotation",
		slog.String("project", project),
		slog.String("namespace", ns),
	)

	nsObj, err := k.client.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting project namespace: %w", err)
	}

	raw, ok := nsObj.Annotations[v1alpha1.AnnotationSettings]
	if !ok || raw == "" {
		return DefaultSettings(project), nil
	}

	settings := &consolev1.ProjectSettings{}
	if err := json.Unmarshal([]byte(raw), settings); err != nil {
		return nil, fmt.Errorf("parsing settings annotation JSON: %w", err)
	}
	// Always set project from the request context, not from stored data
	settings.Project = project
	return settings, nil
}

// UpdateSettings writes the settings as an annotation on the project Namespace.
func (k *K8sClient) UpdateSettings(ctx context.Context, settings *consolev1.ProjectSettings) (*consolev1.ProjectSettings, error) {
	ns := k.Resolver.ProjectNamespace(settings.Project)
	slog.DebugContext(ctx, "updating project settings on namespace annotation",
		slog.String("project", settings.Project),
		slog.String("namespace", ns),
	)

	data, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("marshaling settings: %w", err)
	}

	nsObj, err := k.client.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting project namespace: %w", err)
	}

	if nsObj.Annotations == nil {
		nsObj.Annotations = make(map[string]string)
	}
	nsObj.Annotations[v1alpha1.AnnotationSettings] = string(data)

	if _, err := k.client.CoreV1().Namespaces().Update(ctx, nsObj, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("updating project namespace: %w", err)
	}
	return settings, nil
}

// GetProjectNamespaceRaw returns the raw JSON of the project Namespace with
// apiVersion and kind set.
func (k *K8sClient) GetProjectNamespaceRaw(ctx context.Context, project string) (string, error) {
	ns := k.Resolver.ProjectNamespace(project)
	nsObj, err := k.client.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting project namespace: %w", err)
	}

	nsObj.APIVersion = "v1"
	nsObj.Kind = "Namespace"

	raw, err := json.Marshal(nsObj)
	if err != nil {
		return "", fmt.Errorf("marshaling namespace to JSON: %w", err)
	}
	return string(raw), nil
}
