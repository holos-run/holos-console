package folders

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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// K8sClient wraps Kubernetes client operations for folders (namespaces).
type K8sClient struct {
	client   kubernetes.Interface
	Resolver *resolver.Resolver
}

// NewK8sClient creates a client for folder operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

// ListFolders returns all folder namespaces. When org is non-empty, filters by
// organization label. When parentNs is non-empty, filters to direct children of
// that parent namespace.
func (c *K8sClient) ListFolders(ctx context.Context, org, parentNs string) ([]*corev1.Namespace, error) {
	labelSelector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue + "," +
		v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeFolder
	if org != "" {
		labelSelector += "," + v1alpha2.LabelOrganization + "=" + org
	}
	slog.DebugContext(ctx, "listing folders from kubernetes",
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
		ns := &list.Items[i]
		if ns.DeletionTimestamp != nil {
			continue
		}
		if _, err := c.Resolver.FolderFromNamespace(ns.Name); err != nil {
			var pme *resolver.PrefixMismatchError
			if errors.As(err, &pme) {
				slog.DebugContext(ctx, "filtering folder namespace with prefix mismatch",
					slog.String("namespace", ns.Name),
					slog.String("reason", err.Error()),
				)
				continue
			}
		}
		// Filter by parent namespace when specified.
		if parentNs != "" {
			if ns.Labels[v1alpha2.AnnotationParent] != parentNs {
				continue
			}
		}
		result = append(result, ns)
	}
	return result, nil
}

// GetFolder retrieves a managed folder namespace by name.
// The name is the user-facing folder name (not the Kubernetes namespace).
func (c *K8sClient) GetFolder(ctx context.Context, name string) (*corev1.Namespace, error) {
	nsName := c.Resolver.FolderNamespace(name)
	slog.DebugContext(ctx, "getting folder from kubernetes",
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
	if ns.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeFolder {
		return nil, fmt.Errorf("namespace %q is not a folder", nsName)
	}
	return ns, nil
}

// GetNamespace retrieves any namespace by its full Kubernetes name.
// Used for walking the parent chain during depth enforcement.
func (c *K8sClient) GetNamespace(ctx context.Context, nsName string) (*corev1.Namespace, error) {
	return c.client.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
}

// NamespaceExists returns true if a namespace with the given name exists.
func (c *K8sClient) NamespaceExists(ctx context.Context, nsName string) (bool, error) {
	_, err := c.client.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CreateFolder creates a new namespace with folder labels and annotations.
// parentNs is the Kubernetes namespace name of the immediate parent (org or folder).
// org is the root organization name.
func (c *K8sClient) CreateFolder(
	ctx context.Context,
	name, displayName, description, org, parentNs, creatorEmail string,
	shareUsers, shareRoles []secrets.AnnotationGrant,
) (*corev1.Namespace, error) {
	nsName := c.Resolver.FolderNamespace(name)
	slog.DebugContext(ctx, "creating folder in kubernetes",
		slog.String("name", name),
		slog.String("namespace", nsName),
		slog.String("parent", parentNs),
		slog.String("org", org),
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
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelOrganization: org,
				v1alpha2.LabelFolder:       name,
				v1alpha2.AnnotationParent:  parentNs,
			},
			Annotations: annotations,
		},
	}
	return c.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
}

// UpdateFolder updates display name and description annotations on a folder.
func (c *K8sClient) UpdateFolder(ctx context.Context, name string, displayName, description *string) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "updating folder in kubernetes",
		slog.String("name", name),
	)
	ns, err := c.GetFolder(ctx, name)
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

// UpdateParentLabel updates the parent label on a folder namespace.
func (c *K8sClient) UpdateParentLabel(ctx context.Context, name, newParentNs string) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "updating folder parent label in kubernetes",
		slog.String("name", name),
		slog.String("newParent", newParentNs),
	)
	ns, err := c.GetFolder(ctx, name)
	if err != nil {
		return nil, err
	}
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	ns.Labels[v1alpha2.AnnotationParent] = newParentNs
	return c.client.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
}

// DeleteFolder deletes a managed folder namespace.
func (c *K8sClient) DeleteFolder(ctx context.Context, name string) error {
	slog.DebugContext(ctx, "deleting folder from kubernetes",
		slog.String("name", name),
	)
	ns, err := c.GetFolder(ctx, name)
	if err != nil {
		return err
	}
	return c.client.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
}

// UpdateFolderSharing updates the sharing annotations on a folder.
func (c *K8sClient) UpdateFolderSharing(ctx context.Context, name string, shareUsers, shareRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "updating folder sharing in kubernetes",
		slog.String("name", name),
	)
	ns, err := c.GetFolder(ctx, name)
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

// UpdateFolderDefaultSharing updates the default sharing annotations on a folder.
func (c *K8sClient) UpdateFolderDefaultSharing(ctx context.Context, name string, defaultUsers, defaultRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "updating folder default sharing in kubernetes",
		slog.String("name", name),
	)
	ns, err := c.GetFolder(ctx, name)
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

// ListChildFolders returns all folder namespaces whose parent label equals the given namespace.
func (c *K8sClient) ListChildFolders(ctx context.Context, parentNs string) ([]*corev1.Namespace, error) {
	labelSelector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue + "," +
		v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeFolder
	list, err := c.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}
	var result []*corev1.Namespace
	for i := range list.Items {
		ns := &list.Items[i]
		if ns.DeletionTimestamp != nil {
			continue
		}
		if ns.Labels[v1alpha2.AnnotationParent] == parentNs {
			result = append(result, ns)
		}
	}
	return result, nil
}

// ListChildProjects returns all project namespaces whose parent label equals the given namespace.
func (c *K8sClient) ListChildProjects(ctx context.Context, parentNs string) ([]*corev1.Namespace, error) {
	labelSelector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue + "," +
		v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeProject
	list, err := c.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}
	var result []*corev1.Namespace
	for i := range list.Items {
		ns := &list.Items[i]
		if ns.DeletionTimestamp != nil {
			continue
		}
		if ns.Labels[v1alpha2.AnnotationParent] == parentNs {
			result = append(result, ns)
		}
	}
	return result, nil
}

// GetShareUsers parses the share-users annotation from a namespace.
func GetShareUsers(ns *corev1.Namespace) ([]secrets.AnnotationGrant, error) {
	return parseGrantAnnotation(ns, v1alpha2.AnnotationShareUsers)
}

// GetShareRoles parses the share-roles annotation from a namespace.
func GetShareRoles(ns *corev1.Namespace) ([]secrets.AnnotationGrant, error) {
	return parseGrantAnnotation(ns, v1alpha2.AnnotationShareRoles)
}

// GetDefaultShareUsers parses the default-share-users annotation from a namespace.
func GetDefaultShareUsers(ns *corev1.Namespace) ([]secrets.AnnotationGrant, error) {
	return parseGrantAnnotation(ns, v1alpha2.AnnotationDefaultShareUsers)
}

// GetDefaultShareRoles parses the default-share-roles annotation from a namespace.
func GetDefaultShareRoles(ns *corev1.Namespace) ([]secrets.AnnotationGrant, error) {
	return parseGrantAnnotation(ns, v1alpha2.AnnotationDefaultShareRoles)
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
