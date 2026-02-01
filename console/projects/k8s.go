package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/holos-run/holos-console/console/secrets"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DisplayNameAnnotation is the annotation key for a project's display name.
const DisplayNameAnnotation = "console.holos.run/display-name"

// K8sClient wraps Kubernetes client operations for projects (namespaces).
type K8sClient struct {
	client kubernetes.Interface
}

// NewK8sClient creates a client for project operations.
func NewK8sClient(client kubernetes.Interface) *K8sClient {
	return &K8sClient{client: client}
}

// ListProjects returns all namespaces with the console managed-by label.
func (c *K8sClient) ListProjects(ctx context.Context) ([]*corev1.Namespace, error) {
	labelSelector := secrets.ManagedByLabel + "=" + secrets.ManagedByValue
	slog.DebugContext(ctx, "listing projects from kubernetes",
		slog.String("labelSelector", labelSelector),
	)
	list, err := c.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}
	result := make([]*corev1.Namespace, len(list.Items))
	for i := range list.Items {
		result[i] = &list.Items[i]
	}
	return result, nil
}

// GetProject retrieves a managed namespace by name.
// Returns an error if the namespace does not have the managed-by label.
func (c *K8sClient) GetProject(ctx context.Context, name string) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "getting project from kubernetes",
		slog.String("name", name),
	)
	ns, err := c.client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if ns.Labels == nil || ns.Labels[secrets.ManagedByLabel] != secrets.ManagedByValue {
		return nil, fmt.Errorf("namespace %q is not managed by %s", name, secrets.ManagedByValue)
	}
	return ns, nil
}

// CreateProject creates a new namespace with the managed-by label and annotations.
func (c *K8sClient) CreateProject(ctx context.Context, name, displayName, description string, shareUsers, shareGroups []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "creating project in kubernetes",
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
			Name: name,
			Labels: map[string]string{
				secrets.ManagedByLabel: secrets.ManagedByValue,
			},
			Annotations: annotations,
		},
	}
	return c.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
}

// UpdateProject updates the description and display name annotations on a managed namespace.
// Nil pointers preserve existing values.
func (c *K8sClient) UpdateProject(ctx context.Context, name string, displayName, description *string) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "updating project in kubernetes",
		slog.String("name", name),
	)
	ns, err := c.GetProject(ctx, name)
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

// DeleteProject deletes a managed namespace.
// Returns an error if the namespace does not have the managed-by label.
func (c *K8sClient) DeleteProject(ctx context.Context, name string) error {
	slog.DebugContext(ctx, "deleting project from kubernetes",
		slog.String("name", name),
	)
	// Verify the namespace is managed before deleting.
	if _, err := c.GetProject(ctx, name); err != nil {
		return err
	}
	return c.client.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
}

// UpdateProjectSharing updates the sharing annotations on a managed namespace.
func (c *K8sClient) UpdateProjectSharing(ctx context.Context, name string, shareUsers, shareGroups []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "updating project sharing in kubernetes",
		slog.String("name", name),
	)
	ns, err := c.GetProject(ctx, name)
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
