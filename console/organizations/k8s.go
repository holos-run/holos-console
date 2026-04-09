package organizations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/secrets"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

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
	labelSelector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue + "," +
		v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeOrganization
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
		if _, err := c.resolver.OrgFromNamespace(list.Items[i].Name); err != nil {
			var pme *resolver.PrefixMismatchError
			if errors.As(err, &pme) {
				slog.DebugContext(ctx, "filtering organization namespace with prefix mismatch",
					slog.String("namespace", list.Items[i].Name),
					slog.String("reason", err.Error()),
				)
				continue
			}
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
	if ns.Labels == nil || ns.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
		return nil, fmt.Errorf("namespace %q is not managed by %s", nsName, v1alpha2.ManagedByValue)
	}
	if ns.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeOrganization {
		return nil, fmt.Errorf("namespace %q is not an organization", nsName)
	}
	return ns, nil
}

// CreateOrganization creates a new namespace with organization labels and annotations.
func (c *K8sClient) CreateOrganization(ctx context.Context, name, displayName, description, creatorEmail string, shareUsers, shareRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	nsName := c.resolver.OrgNamespace(name)
	slog.DebugContext(ctx, "creating organization in kubernetes",
		slog.String("name", name),
		slog.String("namespace", nsName),
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
		v1alpha2.AnnotationShareUsers: string(usersJSON),
		v1alpha2.AnnotationShareRoles: string(rolesJSON),
	}
	if displayName != "" {
		annotations[v1alpha2.AnnotationDisplayName] = displayName
	}
	if description != "" {
		annotations[v1alpha2.AnnotationDescription] = description
	}
	if creatorEmail != "" {
		annotations[v1alpha2.AnnotationCreatorEmail] = creatorEmail
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
				v1alpha2.LabelOrganization: name,
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
			delete(ns.Annotations, v1alpha2.AnnotationDisplayName)
		} else {
			ns.Annotations[v1alpha2.AnnotationDisplayName] = *displayName
		}
	}
	if description != nil {
		if *description == "" {
			delete(ns.Annotations, v1alpha2.AnnotationDescription)
		} else {
			ns.Annotations[v1alpha2.AnnotationDescription] = *description
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
func (c *K8sClient) UpdateOrganizationSharing(ctx context.Context, name string, shareUsers, shareRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
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
	rolesJSON, err := json.Marshal(shareRoles)
	if err != nil {
		return nil, fmt.Errorf("marshaling share-roles: %w", err)
	}
	ns.Annotations[v1alpha2.AnnotationShareUsers] = string(usersJSON)
	ns.Annotations[v1alpha2.AnnotationShareRoles] = string(rolesJSON)
	return c.client.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
}

// GetDisplayName returns the display-name annotation value from a namespace.
func GetDisplayName(ns *corev1.Namespace) string {
	if ns.Annotations == nil {
		return ""
	}
	return ns.Annotations[v1alpha2.AnnotationDisplayName]
}

// GetDescription returns the description annotation value from a namespace.
func GetDescription(ns *corev1.Namespace) string {
	if ns.Annotations == nil {
		return ""
	}
	return ns.Annotations[v1alpha2.AnnotationDescription]
}

// GetShareUsers parses the share-users annotation from a namespace.
func GetShareUsers(ns *corev1.Namespace) ([]secrets.AnnotationGrant, error) {
	return parseGrantAnnotation(ns, v1alpha2.AnnotationShareUsers)
}

// GetShareRoles parses the share-roles annotation from a namespace.
// Returns nil if the annotation is absent.
func GetShareRoles(ns *corev1.Namespace) ([]secrets.AnnotationGrant, error) {
	return parseGrantAnnotation(ns, v1alpha2.AnnotationShareRoles)
}

// GetDefaultShareUsers parses the default-share-users annotation from a namespace.
// Returns nil if the annotation is absent.
func GetDefaultShareUsers(ns *corev1.Namespace) ([]secrets.AnnotationGrant, error) {
	return parseGrantAnnotation(ns, v1alpha2.AnnotationDefaultShareUsers)
}

// GetDefaultShareRoles parses the default-share-roles annotation from a namespace.
// Returns nil if the annotation is absent.
func GetDefaultShareRoles(ns *corev1.Namespace) ([]secrets.AnnotationGrant, error) {
	return parseGrantAnnotation(ns, v1alpha2.AnnotationDefaultShareRoles)
}

// UpdateOrganizationDefaultSharing updates the default sharing annotations on an organization namespace.
func (c *K8sClient) UpdateOrganizationDefaultSharing(ctx context.Context, name string, defaultUsers, defaultRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "updating organization default sharing in kubernetes",
		slog.String("name", name),
	)
	ns, err := c.GetOrganization(ctx, name)
	if err != nil {
		return nil, err
	}
	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}
	usersJSON, err := json.Marshal(defaultUsers)
	if err != nil {
		return nil, fmt.Errorf("marshaling default-share-users: %w", err)
	}
	rolesJSON, err := json.Marshal(defaultRoles)
	if err != nil {
		return nil, fmt.Errorf("marshaling default-share-roles: %w", err)
	}
	ns.Annotations[v1alpha2.AnnotationDefaultShareUsers] = string(usersJSON)
	ns.Annotations[v1alpha2.AnnotationDefaultShareRoles] = string(rolesJSON)
	return c.client.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
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
