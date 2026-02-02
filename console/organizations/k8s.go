package organizations

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/secrets"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DisplayNameAnnotation is the annotation key for an organization's display name.
const DisplayNameAnnotation = "console.holos.run/display-name"

// K8sClient wraps Kubernetes client operations for organizations (namespaces).
type K8sClient struct {
	client   kubernetes.Interface
	resolver *resolver.Resolver
}

// NewK8sClient creates a client for organization operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, resolver: r}
}

// ListOrganizations returns all namespaces with the organization resource-type label.
func (c *K8sClient) ListOrganizations(ctx context.Context) ([]*corev1.Namespace, error) {
	labelSelector := secrets.ManagedByLabel + "=" + secrets.ManagedByValue + "," +
		resolver.ResourceTypeLabel + "=" + resolver.ResourceTypeOrganization
	slog.DebugContext(ctx, "listing organizations from kubernetes",
		slog.String("labelSelector", labelSelector),
	)
	list, err := c.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}
	result := make([]*corev1.Namespace, 0, len(list.Items))
	for i := range list.Items {
		if list.Items[i].DeletionTimestamp != nil {
			continue
		}
		result = append(result, &list.Items[i])
	}
	return result, nil
}

// GetOrganization retrieves a managed organization namespace by name.
// Returns an error if the namespace does not have the expected labels.
func (c *K8sClient) GetOrganization(ctx context.Context, name string) (*corev1.Namespace, error) {
	nsName := c.resolver.OrgNamespace(name)
	slog.DebugContext(ctx, "getting organization from kubernetes",
		slog.String("name", name),
		slog.String("namespace", nsName),
	)
	ns, err := c.client.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if ns.Labels == nil || ns.Labels[secrets.ManagedByLabel] != secrets.ManagedByValue {
		return nil, fmt.Errorf("namespace %q is not managed by %s", nsName, secrets.ManagedByValue)
	}
	if ns.Labels[resolver.ResourceTypeLabel] != resolver.ResourceTypeOrganization {
		return nil, fmt.Errorf("namespace %q is not an organization", nsName)
	}
	return ns, nil
}

// CreateOrganization creates a new namespace with organization labels and annotations.
func (c *K8sClient) CreateOrganization(ctx context.Context, name, displayName, description string, shareUsers, shareGroups []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	nsName := c.resolver.OrgNamespace(name)
	slog.DebugContext(ctx, "creating organization in kubernetes",
		slog.String("name", name),
		slog.String("namespace", nsName),
	)
	usersJSON, err := json.Marshal(shareUsers)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-users: %w", err)
	}
	groupsJSON, err := json.Marshal(shareGroups)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-groups: %w", err)
	}
	annotations := map[string]string{
		secrets.ShareUsersAnnotation:  string(usersJSON),
		secrets.ShareGroupsAnnotation: string(groupsJSON),
	}
	if displayName != "" {
		annotations[DisplayNameAnnotation] = displayName
	}
	if description != "" {
		annotations[secrets.DescriptionAnnotation] = description
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
				resolver.OrganizationLabel: name,
			},
			Annotations: annotations,
		},
	}
	return c.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
}

// UpdateOrganization updates the description and display name annotations on an organization namespace.
// Nil pointers preserve existing values.
func (c *K8sClient) UpdateOrganization(ctx context.Context, name string, displayName, description *string) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "updating organization in kubernetes",
		slog.String("name", name),
	)
	ns, err := c.GetOrganization(ctx, name)
	if err != nil {
		return nil, err
	}
	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}
	if displayName != nil {
		if *displayName == "" {
			delete(ns.Annotations, DisplayNameAnnotation)
		} else {
			ns.Annotations[DisplayNameAnnotation] = *displayName
		}
	}
	if description != nil {
		if *description == "" {
			delete(ns.Annotations, secrets.DescriptionAnnotation)
		} else {
			ns.Annotations[secrets.DescriptionAnnotation] = *description
		}
	}
	return c.client.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
}

// DeleteOrganization deletes a managed organization namespace.
// Returns an error if the namespace does not have the expected labels.
func (c *K8sClient) DeleteOrganization(ctx context.Context, name string) error {
	slog.DebugContext(ctx, "deleting organization from kubernetes",
		slog.String("name", name),
	)
	// Verify the namespace is a managed organization before deleting.
	ns, err := c.GetOrganization(ctx, name)
	if err != nil {
		return err
	}
	return c.client.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
}

// UpdateOrganizationSharing updates the sharing annotations on an organization namespace.
func (c *K8sClient) UpdateOrganizationSharing(ctx context.Context, name string, shareUsers, shareGroups []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "updating organization sharing in kubernetes",
		slog.String("name", name),
	)
	ns, err := c.GetOrganization(ctx, name)
	if err != nil {
		return nil, err
	}
	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}
	usersJSON, err := json.Marshal(shareUsers)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-users: %w", err)
	}
	groupsJSON, err := json.Marshal(shareGroups)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-groups: %w", err)
	}
	ns.Annotations[secrets.ShareUsersAnnotation] = string(usersJSON)
	ns.Annotations[secrets.ShareGroupsAnnotation] = string(groupsJSON)
	return c.client.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
}

// GetDisplayName returns the display-name annotation value from a namespace.
func GetDisplayName(ns *corev1.Namespace) string {
	if ns.Annotations == nil {
		return ""
	}
	return ns.Annotations[DisplayNameAnnotation]
}

// GetDescription returns the description annotation value from a namespace.
func GetDescription(ns *corev1.Namespace) string {
	if ns.Annotations == nil {
		return ""
	}
	return ns.Annotations[secrets.DescriptionAnnotation]
}

// GetShareUsers parses the share-users annotation from a namespace.
func GetShareUsers(ns *corev1.Namespace) ([]secrets.AnnotationGrant, error) {
	return parseGrantAnnotation(ns, secrets.ShareUsersAnnotation)
}

// GetShareGroups parses the share-groups annotation from a namespace.
func GetShareGroups(ns *corev1.Namespace) ([]secrets.AnnotationGrant, error) {
	return parseGrantAnnotation(ns, secrets.ShareGroupsAnnotation)
}

func parseGrantAnnotation(ns *corev1.Namespace, key string) ([]secrets.AnnotationGrant, error) {
	if ns.Annotations == nil {
		return nil, nil
	}
	value, ok := ns.Annotations[key]
	if !ok {
		return nil, nil
	}
	var grants []secrets.AnnotationGrant
	if err := json.Unmarshal([]byte(value), &grants); err != nil {
		return nil, fmt.Errorf("invalid %s annotation: %w", key, err)
	}
	return grants, nil
}
