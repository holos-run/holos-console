package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// AllowedGroupsAnnotation is the annotation key for allowed groups on a secret.
const AllowedGroupsAnnotation = "holos.run/allowed-groups"

// K8sClient wraps Kubernetes client operations for secrets.
type K8sClient struct {
	client    kubernetes.Interface
	namespace string
}

// NewK8sClient creates a client for the given namespace.
func NewK8sClient(client kubernetes.Interface, namespace string) *K8sClient {
	return &K8sClient{client: client, namespace: namespace}
}

// GetSecret retrieves a secret by name from the configured namespace.
func (c *K8sClient) GetSecret(ctx context.Context, name string) (*corev1.Secret, error) {
	slog.DebugContext(ctx, "getting secret from kubernetes",
		slog.String("namespace", c.namespace),
		slog.String("name", name),
	)
	return c.client.CoreV1().Secrets(c.namespace).Get(ctx, name, metav1.GetOptions{})
}

// GetAllowedGroups parses the holos.run/allowed-groups annotation from a secret.
// Returns an empty slice if the annotation is missing.
// Returns an error if the annotation contains invalid JSON.
func GetAllowedGroups(secret *corev1.Secret) ([]string, error) {
	if secret.Annotations == nil {
		return []string{}, nil
	}

	value, ok := secret.Annotations[AllowedGroupsAnnotation]
	if !ok {
		return []string{}, nil
	}

	var groups []string
	if err := json.Unmarshal([]byte(value), &groups); err != nil {
		return nil, fmt.Errorf("invalid %s annotation: %w", AllowedGroupsAnnotation, err)
	}

	return groups, nil
}
