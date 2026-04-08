package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
	"github.com/holos-run/holos-console/console/resolver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// roleRank maps role strings to their privilege level for comparison.
var roleRank = map[string]int{
	"viewer": 1,
	"editor": 2,
	"owner":  3,
}

// DeduplicateGrants merges duplicate principals, keeping the grant with the
// highest role. Entries with empty principals are dropped. Insertion order of
// first-seen principals is preserved.
func DeduplicateGrants(grants []AnnotationGrant) []AnnotationGrant {
	seen := make(map[string]int) // principal -> index in result
	result := make([]AnnotationGrant, 0, len(grants))
	for _, g := range grants {
		if g.Principal == "" {
			continue
		}
		if idx, ok := seen[g.Principal]; ok {
			if roleRank[g.Role] > roleRank[result[idx].Role] {
				result[idx] = g
			}
		} else {
			seen[g.Principal] = len(result)
			result = append(result, g)
		}
	}
	return result
}

// AnnotationGrant represents a single sharing grant stored in a Kubernetes annotation.
type AnnotationGrant struct {
	Principal string `json:"principal"`
	Role      string `json:"role"`
	Nbf       *int64 `json:"nbf,omitempty"`
	Exp       *int64 `json:"exp,omitempty"`
}

// K8sClient wraps Kubernetes client operations for secrets.
type K8sClient struct {
	client   kubernetes.Interface
	Resolver *resolver.Resolver
}

// NewK8sClient creates a client for secrets operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

// GetSecret retrieves a secret by name from the project's namespace.
func (c *K8sClient) GetSecret(ctx context.Context, project, name string) (*corev1.Secret, error) {
	ns := c.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "getting secret from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return c.client.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
}

// ListSecrets retrieves secrets with the console label from the project's namespace.
func (c *K8sClient) ListSecrets(ctx context.Context, project string) (*corev1.SecretList, error) {
	ns := c.Resolver.ProjectNamespace(project)
	labelSelector := v1alpha1.LabelManagedBy + "=" + v1alpha1.ManagedByValue
	slog.DebugContext(ctx, "listing secrets from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("labelSelector", labelSelector),
	)
	return c.client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
}

// CreateSecret creates a new secret with the console managed-by label and sharing grants.
func (c *K8sClient) CreateSecret(ctx context.Context, project, name string, data map[string][]byte, shareUsers, shareRoles []AnnotationGrant, description, url string) (*corev1.Secret, error) {
	ns := c.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "creating secret in kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	usersJSON, err := json.Marshal(shareUsers)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-users: %w", err)
	}
	rolesJSON, err := json.Marshal(shareRoles)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-roles: %w", err)
	}
	annotations := map[string]string{
		v1alpha1.AnnotationShareUsers: string(usersJSON),
		v1alpha1.AnnotationShareRoles: string(rolesJSON),
	}
	if description != "" {
		annotations[v1alpha1.AnnotationDescription] = description
	}
	if url != "" {
		annotations[v1alpha1.AnnotationURL] = url
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha1.LabelManagedBy: v1alpha1.ManagedByValue,
			},
			Annotations: annotations,
		},
		Data: data,
	}
	return c.client.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
}

// UpdateSecret replaces the data of an existing secret.
// Returns FailedPrecondition if the secret does not have the console managed-by label.
// description and url are optional pointers: nil preserves the existing value, non-nil updates it.
func (c *K8sClient) UpdateSecret(ctx context.Context, project, name string, data map[string][]byte, description, url *string) (*corev1.Secret, error) {
	slog.DebugContext(ctx, "updating secret in kubernetes",
		slog.String("project", project),
		slog.String("name", name),
	)
	secret, err := c.GetSecret(ctx, project, name)
	if err != nil {
		return nil, err
	}
	if secret.Labels == nil || secret.Labels[v1alpha1.LabelManagedBy] != v1alpha1.ManagedByValue {
		return nil, fmt.Errorf("secret %q is not managed by %s", name, v1alpha1.ManagedByValue)
	}
	secret.Data = data
	if description != nil || url != nil {
		if secret.Annotations == nil {
			secret.Annotations = make(map[string]string)
		}
		if description != nil {
			if *description == "" {
				delete(secret.Annotations, v1alpha1.AnnotationDescription)
			} else {
				secret.Annotations[v1alpha1.AnnotationDescription] = *description
			}
		}
		if url != nil {
			if *url == "" {
				delete(secret.Annotations, v1alpha1.AnnotationURL)
			} else {
				secret.Annotations[v1alpha1.AnnotationURL] = *url
			}
		}
	}
	return c.client.CoreV1().Secrets(secret.Namespace).Update(ctx, secret, metav1.UpdateOptions{})
}

// DeleteSecret deletes a secret by name.
// Returns FailedPrecondition if the secret does not have the console managed-by label.
func (c *K8sClient) DeleteSecret(ctx context.Context, project, name string) error {
	slog.DebugContext(ctx, "deleting secret from kubernetes",
		slog.String("project", project),
		slog.String("name", name),
	)
	secret, err := c.GetSecret(ctx, project, name)
	if err != nil {
		return err
	}
	if secret.Labels == nil || secret.Labels[v1alpha1.LabelManagedBy] != v1alpha1.ManagedByValue {
		return fmt.Errorf("secret %q is not managed by %s", name, v1alpha1.ManagedByValue)
	}
	return c.client.CoreV1().Secrets(secret.Namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// UpdateSharing updates the sharing annotations on an existing secret.
// Returns FailedPrecondition if the secret does not have the console managed-by label.
func (c *K8sClient) UpdateSharing(ctx context.Context, project, name string, shareUsers, shareRoles []AnnotationGrant) (*corev1.Secret, error) {
	slog.DebugContext(ctx, "updating sharing on kubernetes secret",
		slog.String("project", project),
		slog.String("name", name),
	)
	secret, err := c.GetSecret(ctx, project, name)
	if err != nil {
		return nil, err
	}
	if secret.Labels == nil || secret.Labels[v1alpha1.LabelManagedBy] != v1alpha1.ManagedByValue {
		return nil, fmt.Errorf("secret %q is not managed by %s", name, v1alpha1.ManagedByValue)
	}
	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}
	usersJSON, err := json.Marshal(shareUsers)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-users: %w", err)
	}
	rolesJSON, err := json.Marshal(shareRoles)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-roles: %w", err)
	}
	secret.Annotations[v1alpha1.AnnotationShareUsers] = string(usersJSON)
	secret.Annotations[v1alpha1.AnnotationShareRoles] = string(rolesJSON)
	return c.client.CoreV1().Secrets(secret.Namespace).Update(ctx, secret, metav1.UpdateOptions{})
}

// GetShareUsers parses the console.holos.run/share-users annotation from a secret.
// Returns an empty slice if the annotation is missing.
// Returns an error if the annotation contains invalid JSON.
func GetShareUsers(secret *corev1.Secret) ([]AnnotationGrant, error) {
	return parseGrantAnnotation(secret, v1alpha1.AnnotationShareUsers)
}

// GetShareRoles parses the console.holos.run/share-roles annotation from a secret.
// Returns nil if the annotation is absent.
// Returns an error if the annotation contains invalid JSON.
func GetShareRoles(secret *corev1.Secret) ([]AnnotationGrant, error) {
	return parseGrantAnnotation(secret, v1alpha1.AnnotationShareRoles)
}

// GetDescription returns the description annotation value from a secret.
// Returns an empty string if the annotation is absent.
func GetDescription(secret *corev1.Secret) string {
	if secret.Annotations == nil {
		return ""
	}
	return secret.Annotations[v1alpha1.AnnotationDescription]
}

// GetURL returns the URL annotation value from a secret.
// Returns an empty string if the annotation is absent.
func GetURL(secret *corev1.Secret) string {
	if secret.Annotations == nil {
		return ""
	}
	return secret.Annotations[v1alpha1.AnnotationURL]
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
// suitable for passing to CheckAccessGrants. Grants with nbf > now or exp <= now are
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
