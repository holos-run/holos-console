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

// AllowedRolesAnnotation is the annotation key for allowed roles on a secret.
const AllowedRolesAnnotation = "holos.run/allowed-roles"

// AllowedGroupsAnnotation is the annotation key for allowed groups on a secret.
// Deprecated: Use AllowedRolesAnnotation instead.
const AllowedGroupsAnnotation = "holos.run/allowed-groups"

// ShareUsersAnnotation is the annotation key for per-user sharing grants.
// Value is a JSON object mapping email address → role name.
const ShareUsersAnnotation = "holos.run/share-users"

// ShareGroupsAnnotation is the annotation key for per-group sharing grants.
// Value is a JSON object mapping OIDC group name → role name.
const ShareGroupsAnnotation = "holos.run/share-groups"

// ManagedByLabel is the label key used to identify secrets managed by the console.
const ManagedByLabel = "app.kubernetes.io/managed-by"

// ManagedByValue is the label value that identifies secrets managed by console.holos.run.
const ManagedByValue = "console.holos.run"

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

// ListSecrets retrieves secrets with the console label from the configured namespace.
func (c *K8sClient) ListSecrets(ctx context.Context) (*corev1.SecretList, error) {
	labelSelector := ManagedByLabel + "=" + ManagedByValue
	slog.DebugContext(ctx, "listing secrets from kubernetes",
		slog.String("namespace", c.namespace),
		slog.String("labelSelector", labelSelector),
	)
	return c.client.CoreV1().Secrets(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
}

// CreateSecret creates a new secret with the console managed-by label.
func (c *K8sClient) CreateSecret(ctx context.Context, name string, data map[string][]byte, allowedRoles []string) (*corev1.Secret, error) {
	slog.DebugContext(ctx, "creating secret in kubernetes",
		slog.String("namespace", c.namespace),
		slog.String("name", name),
	)
	rolesJSON, err := json.Marshal(allowedRoles)
	if err != nil {
		return nil, fmt.Errorf("marshaling allowed roles: %w", err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.namespace,
			Labels: map[string]string{
				ManagedByLabel: ManagedByValue,
			},
			Annotations: map[string]string{
				AllowedRolesAnnotation: string(rolesJSON),
			},
		},
		Data: data,
	}
	return c.client.CoreV1().Secrets(c.namespace).Create(ctx, secret, metav1.CreateOptions{})
}

// UpdateSecret replaces the data of an existing secret.
// Returns FailedPrecondition if the secret does not have the console managed-by label.
func (c *K8sClient) UpdateSecret(ctx context.Context, name string, data map[string][]byte) (*corev1.Secret, error) {
	slog.DebugContext(ctx, "updating secret in kubernetes",
		slog.String("namespace", c.namespace),
		slog.String("name", name),
	)
	secret, err := c.GetSecret(ctx, name)
	if err != nil {
		return nil, err
	}
	if secret.Labels == nil || secret.Labels[ManagedByLabel] != ManagedByValue {
		return nil, fmt.Errorf("secret %q is not managed by %s", name, ManagedByValue)
	}
	secret.Data = data
	return c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{})
}

// DeleteSecret deletes a secret by name.
// Returns FailedPrecondition if the secret does not have the console managed-by label.
func (c *K8sClient) DeleteSecret(ctx context.Context, name string) error {
	slog.DebugContext(ctx, "deleting secret from kubernetes",
		slog.String("namespace", c.namespace),
		slog.String("name", name),
	)
	secret, err := c.GetSecret(ctx, name)
	if err != nil {
		return err
	}
	if secret.Labels == nil || secret.Labels[ManagedByLabel] != ManagedByValue {
		return fmt.Errorf("secret %q is not managed by %s", name, ManagedByValue)
	}
	return c.client.CoreV1().Secrets(c.namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// GetAllowedRoles parses the holos.run/allowed-roles annotation from a secret.
// Falls back to holos.run/allowed-groups if the new annotation is not present.
// Returns an empty slice if both annotations are missing.
// Returns an error if the annotation contains invalid JSON.
func GetAllowedRoles(secret *corev1.Secret) ([]string, error) {
	if secret.Annotations == nil {
		return []string{}, nil
	}

	// Prefer the new allowed-roles annotation
	if value, ok := secret.Annotations[AllowedRolesAnnotation]; ok {
		var roles []string
		if err := json.Unmarshal([]byte(value), &roles); err != nil {
			return nil, fmt.Errorf("invalid %s annotation: %w", AllowedRolesAnnotation, err)
		}
		return roles, nil
	}

	// Fall back to allowed-groups annotation for backward compatibility
	if value, ok := secret.Annotations[AllowedGroupsAnnotation]; ok {
		var groups []string
		if err := json.Unmarshal([]byte(value), &groups); err != nil {
			return nil, fmt.Errorf("invalid %s annotation: %w", AllowedGroupsAnnotation, err)
		}
		return groups, nil
	}

	return []string{}, nil
}

// GetShareUsers parses the holos.run/share-users annotation from a secret.
// Returns an empty map if the annotation is missing.
// Returns an error if the annotation contains invalid JSON.
func GetShareUsers(secret *corev1.Secret) (map[string]string, error) {
	if secret.Annotations == nil {
		return map[string]string{}, nil
	}
	value, ok := secret.Annotations[ShareUsersAnnotation]
	if !ok {
		return map[string]string{}, nil
	}
	var users map[string]string
	if err := json.Unmarshal([]byte(value), &users); err != nil {
		return nil, fmt.Errorf("invalid %s annotation: %w", ShareUsersAnnotation, err)
	}
	return users, nil
}

// GetShareGroups parses the holos.run/share-groups annotation from a secret.
// Returns an empty map if the annotation is missing.
// Returns an error if the annotation contains invalid JSON.
func GetShareGroups(secret *corev1.Secret) (map[string]string, error) {
	if secret.Annotations == nil {
		return map[string]string{}, nil
	}
	value, ok := secret.Annotations[ShareGroupsAnnotation]
	if !ok {
		return map[string]string{}, nil
	}
	var groups map[string]string
	if err := json.Unmarshal([]byte(value), &groups); err != nil {
		return nil, fmt.Errorf("invalid %s annotation: %w", ShareGroupsAnnotation, err)
	}
	return groups, nil
}

// GetAllowedGroups parses the holos.run/allowed-groups annotation from a secret.
// Deprecated: Use GetAllowedRoles instead, which supports backward compatibility.
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
