package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ShareUsersAnnotation is the annotation key for per-user sharing grants.
// Value is a JSON object mapping email address → role name.
const ShareUsersAnnotation = "console.holos.run/share-users"

// ShareGroupsAnnotation is the annotation key for per-group sharing grants.
// Value is a JSON object mapping OIDC group name → role name.
const ShareGroupsAnnotation = "console.holos.run/share-groups"

// ManagedByLabel is the label key used to identify secrets managed by the console.
const ManagedByLabel = "app.kubernetes.io/managed-by"

// ManagedByValue is the label value that identifies secrets managed by console.holos.run.
const ManagedByValue = "console.holos.run"

// AnnotationGrant represents a single sharing grant stored in a Kubernetes annotation.
type AnnotationGrant struct {
	Principal string `json:"principal"`
	Role      string `json:"role"`
	Nbf       *int64 `json:"nbf,omitempty"`
	Exp       *int64 `json:"exp,omitempty"`
}

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

// CreateSecret creates a new secret with the console managed-by label and sharing grants.
func (c *K8sClient) CreateSecret(ctx context.Context, name string, data map[string][]byte, shareUsers, shareGroups []AnnotationGrant) (*corev1.Secret, error) {
	slog.DebugContext(ctx, "creating secret in kubernetes",
		slog.String("namespace", c.namespace),
		slog.String("name", name),
	)
	usersJSON, err := json.Marshal(shareUsers)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-users: %w", err)
	}
	groupsJSON, err := json.Marshal(shareGroups)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-groups: %w", err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.namespace,
			Labels: map[string]string{
				ManagedByLabel: ManagedByValue,
			},
			Annotations: map[string]string{
				ShareUsersAnnotation:  string(usersJSON),
				ShareGroupsAnnotation: string(groupsJSON),
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

// UpdateSharing updates the sharing annotations on an existing secret.
// Returns FailedPrecondition if the secret does not have the console managed-by label.
func (c *K8sClient) UpdateSharing(ctx context.Context, name string, shareUsers, shareGroups []AnnotationGrant) (*corev1.Secret, error) {
	slog.DebugContext(ctx, "updating sharing on kubernetes secret",
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
	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}
	usersJSON, err := json.Marshal(shareUsers)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-users: %w", err)
	}
	groupsJSON, err := json.Marshal(shareGroups)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-groups: %w", err)
	}
	secret.Annotations[ShareUsersAnnotation] = string(usersJSON)
	secret.Annotations[ShareGroupsAnnotation] = string(groupsJSON)
	return c.client.CoreV1().Secrets(c.namespace).Update(ctx, secret, metav1.UpdateOptions{})
}

// GetShareUsers parses the console.holos.run/share-users annotation from a secret.
// Returns an empty slice if the annotation is missing.
// Returns an error if the annotation contains invalid JSON.
func GetShareUsers(secret *corev1.Secret) ([]AnnotationGrant, error) {
	return parseGrantAnnotation(secret, ShareUsersAnnotation)
}

// GetShareGroups parses the console.holos.run/share-groups annotation from a secret.
// Returns an empty slice if the annotation is missing.
// Returns an error if the annotation contains invalid JSON.
func GetShareGroups(secret *corev1.Secret) ([]AnnotationGrant, error) {
	return parseGrantAnnotation(secret, ShareGroupsAnnotation)
}

// parseGrantAnnotation parses a JSON annotation value into a slice of AnnotationGrant.
func parseGrantAnnotation(secret *corev1.Secret, key string) ([]AnnotationGrant, error) {
	if secret.Annotations == nil {
		return nil, nil
	}
	value, ok := secret.Annotations[key]
	if !ok {
		return nil, nil
	}
	var grants []AnnotationGrant
	if err := json.Unmarshal([]byte(value), &grants); err != nil {
		return nil, fmt.Errorf("invalid %s annotation: %w", key, err)
	}
	return grants, nil
}

// ActiveGrantsMap filters grants by time window and returns a map of principal → role
// suitable for passing to CheckAccessSharing. Grants with nbf > now or exp <= now are
// excluded. Grants with nil nbf/exp have no time restriction.
func ActiveGrantsMap(grants []AnnotationGrant, now time.Time) map[string]string {
	nowUnix := now.Unix()
	result := make(map[string]string)
	for _, g := range grants {
		if g.Nbf != nil && *g.Nbf > nowUnix {
			continue // not yet active
		}
		if g.Exp != nil && *g.Exp <= nowUnix {
			continue // expired
		}
		if g.Principal != "" {
			result[g.Principal] = g.Role
		}
	}
	return result
}
