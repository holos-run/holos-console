package secrets

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/secretrbac"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	labelSelector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue
	slog.DebugContext(ctx, "listing secrets from kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("labelSelector", labelSelector),
	)
	return c.client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
}

// CreateSecret creates a new secret with the console managed-by label. Sharing
// grants are accepted for the stable RPC surface but materialize as
// RoleBindings through UpdateSharing rather than Secret annotations.
func (c *K8sClient) CreateSecret(ctx context.Context, project, name string, data map[string][]byte, shareUsers, shareRoles []AnnotationGrant, description, url string) (*corev1.Secret, error) {
	ns := c.Resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "creating secret in kubernetes",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	annotations := map[string]string{}
	if description != "" {
		annotations[v1alpha2.AnnotationDescription] = description
	}
	if url != "" {
		annotations[v1alpha2.AnnotationURL] = url
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
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
	if secret.Labels == nil || secret.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
		return nil, fmt.Errorf("secret %q is not managed by %s", name, v1alpha2.ManagedByValue)
	}
	secret.Data = data
	if description != nil || url != nil {
		if secret.Annotations == nil {
			secret.Annotations = make(map[string]string)
		}
		if description != nil {
			if *description == "" {
				delete(secret.Annotations, v1alpha2.AnnotationDescription)
			} else {
				secret.Annotations[v1alpha2.AnnotationDescription] = *description
			}
		}
		if url != nil {
			if *url == "" {
				delete(secret.Annotations, v1alpha2.AnnotationURL)
			} else {
				secret.Annotations[v1alpha2.AnnotationURL] = *url
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
	if secret.Labels == nil || secret.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
		return fmt.Errorf("secret %q is not managed by %s", name, v1alpha2.ManagedByValue)
	}
	return c.client.CoreV1().Secrets(secret.Namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// UpdateSharing reconciles the project-level Secret RoleBindings represented by
// the stable UpdateSharing RPC. Secret access is project-namespace scoped under
// ADR 036, so the secret name is validated for existence but not encoded into
// the RoleBinding objects.
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
	if secret.Labels == nil || secret.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
		return nil, fmt.Errorf("secret %q is not managed by %s", name, v1alpha2.ManagedByValue)
	}
	if err := c.reconcileProjectSecretRoleBindings(ctx, secret.Namespace, shareUsers, shareRoles); err != nil {
		return nil, err
	}
	return secret, nil
}

func (c *K8sClient) ListSharing(ctx context.Context, project string) ([]AnnotationGrant, []AnnotationGrant, error) {
	ns := c.Resolver.ProjectNamespace(project)
	selector := labels.SelectorFromSet(labels.Set{
		v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
		secretrbac.LabelRolePurpose: secretrbac.RolePurposeProjectSecrets,
	})
	list, err := c.client.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, nil, err
	}
	return roleBindingsToGrants(list.Items), roleBindingsToGroupGrants(list.Items), nil
}

func (c *K8sClient) reconcileProjectSecretRoleBindings(ctx context.Context, namespace string, shareUsers, shareRoles []AnnotationGrant) error {
	desired := make(map[string]*rbacv1.RoleBinding)
	for _, grant := range DeduplicateGrants(shareUsers) {
		if grant.Principal == "" {
			continue
		}
		binding := secretrbac.RoleBinding(namespace, secretrbac.ShareTargetUser, grant.Principal, grant.Role, nil)
		desired[binding.Name] = binding
	}
	for _, grant := range DeduplicateGrants(shareRoles) {
		if grant.Principal == "" {
			continue
		}
		binding := secretrbac.RoleBinding(namespace, secretrbac.ShareTargetGroup, grant.Principal, grant.Role, nil)
		desired[binding.Name] = binding
	}

	selector := labels.SelectorFromSet(labels.Set{
		v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
		secretrbac.LabelRolePurpose: secretrbac.RolePurposeProjectSecrets,
	})
	current, err := c.client.RbacV1().RoleBindings(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}
	for _, existing := range current.Items {
		if _, ok := desired[existing.Name]; ok {
			continue
		}
		if err := c.client.RbacV1().RoleBindings(namespace).Delete(ctx, existing.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	for _, binding := range desired {
		if err := c.applyRoleBinding(ctx, binding); err != nil {
			return err
		}
	}
	return nil
}

func (c *K8sClient) applyRoleBinding(ctx context.Context, binding *rbacv1.RoleBinding) error {
	created, err := c.client.RbacV1().RoleBindings(binding.Namespace).Create(ctx, binding, metav1.CreateOptions{})
	if err == nil {
		*binding = *created
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	existing, err := c.client.RbacV1().RoleBindings(binding.Namespace).Get(ctx, binding.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if existing.RoleRef != binding.RoleRef {
		if err := c.client.RbacV1().RoleBindings(binding.Namespace).Delete(ctx, binding.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		created, err := c.client.RbacV1().RoleBindings(binding.Namespace).Create(ctx, binding, metav1.CreateOptions{})
		if err == nil {
			*binding = *created
		}
		return err
	}
	existing.Labels = binding.Labels
	existing.Subjects = binding.Subjects
	updated, err := c.client.RbacV1().RoleBindings(binding.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err == nil {
		*binding = *updated
	}
	return err
}

func roleBindingsToGrants(bindings []rbacv1.RoleBinding) []AnnotationGrant {
	return roleBindingsToTargetGrants(bindings, rbacv1.UserKind)
}

func roleBindingsToGroupGrants(bindings []rbacv1.RoleBinding) []AnnotationGrant {
	return roleBindingsToTargetGrants(bindings, rbacv1.GroupKind)
}

func roleBindingsToTargetGrants(bindings []rbacv1.RoleBinding, kind string) []AnnotationGrant {
	var grants []AnnotationGrant
	for _, binding := range bindings {
		role := secretrbac.RoleFromLabels(binding.Labels, binding.RoleRef.Name)
		for _, subject := range binding.Subjects {
			if subject.Kind != kind {
				continue
			}
			grants = append(grants, AnnotationGrant{
				Principal: secretrbac.UnprefixedPrincipal(subject.Name),
				Role:      role,
			})
		}
	}
	return DeduplicateGrants(grants)
}

// GetDescription returns the description annotation value from a secret.
// Returns an empty string if the annotation is absent.
func GetDescription(secret *corev1.Secret) string {
	if secret.Annotations == nil {
		return ""
	}
	return secret.Annotations[v1alpha2.AnnotationDescription]
}

// GetURL returns the URL annotation value from a secret.
// Returns an empty string if the annotation is absent.
func GetURL(secret *corev1.Secret) string {
	if secret.Annotations == nil {
		return ""
	}
	return secret.Annotations[v1alpha2.AnnotationURL]
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
