package organizations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/resourcerbac"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	"github.com/holos-run/holos-console/console/sharing/legacy"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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

func (c *K8sClient) clientset(ctx context.Context) kubernetes.Interface {
	if rpc.HasImpersonatedClients(ctx) {
		return rpc.ImpersonatedClientsetFromContext(ctx)
	}
	return c.client
}

func (c *K8sClient) canVerbNamespace(ctx context.Context, verb, name string) (bool, error) {
	if !rpc.HasImpersonatedClients(ctx) {
		return true, nil
	}
	review := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Verb:     verb,
				Group:    "",
				Resource: "namespaces",
				Name:     name,
			},
		},
	}
	got, err := rpc.ImpersonatedClientsetFromContext(ctx).AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, err
	}
	return got.Status.Allowed, nil
}

// ListOrganizations returns all namespaces with the organization resource-type label.
func (c *K8sClient) ListOrganizations(ctx context.Context) ([]*corev1.Namespace, error) {
	labelSelector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue + "," +
		v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeOrganization
	slog.DebugContext(ctx, "listing organizations from kubernetes",
		slog.String("labelSelector", labelSelector),
	)
	// ADR 036 keeps Kubernetes as the authorizer. The service-account list is
	// only a candidate index; each row returned to the caller is re-read below
	// through the impersonated request client so per-resource get RBAC filters
	// the response without granting broad namespace list.
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
	if !rpc.HasImpersonatedClients(ctx) {
		return result, nil
	}
	authorized := make([]*corev1.Namespace, 0, len(result))
	for _, ns := range result {
		name, err := c.resolver.OrgFromNamespace(ns.Name)
		if err != nil {
			return nil, err
		}
		got, err := c.GetOrganization(ctx, name)
		if err == nil {
			authorized = append(authorized, got)
			continue
		}
		if k8serrors.IsForbidden(err) || k8serrors.IsNotFound(err) {
			continue
		}
		return nil, err
	}
	return authorized, nil
}

// GetOrganization retrieves a managed organization namespace by name.
// Returns an error if the namespace does not have the expected labels.
func (c *K8sClient) GetOrganization(ctx context.Context, name string) (*corev1.Namespace, error) {
	nsName := c.resolver.OrgNamespace(name)
	slog.DebugContext(ctx, "getting organization from kubernetes",
		slog.String("name", name),
		slog.String("namespace", nsName),
	)
	ns, err := c.clientset(ctx).CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
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
func (c *K8sClient) CreateOrganization(ctx context.Context, name, displayName, description, creatorEmail, creatorSubject string, shareUsers, shareRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
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
	if creatorSubject != "" {
		annotations[v1alpha2.AnnotationCreatorSubject] = creatorSubject
	}
	rbacShareUsers := secrets.RBACUserGrantsForSubjects(shareUsers, secrets.UserIdentity{Email: creatorEmail, Subject: creatorSubject})
	if len(rbacShareUsers) > 0 {
		rbacUsersJSON, err := json.Marshal(rbacShareUsers)
		if err != nil {
			return nil, fmt.Errorf("marshaling rbac-share-users: %w", err)
		}
		annotations[v1alpha2.AnnotationRBACShareUsers] = string(rbacUsersJSON)
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
	created, err := c.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	// Synchronously bootstrap per-resource RBAC for the creator before any
	// subsequent impersonated read/update/delete in the bootstrap path. This
	// avoids racing the async resourcerbac reconciler. See HOL-1064 REV2 AC2.
	if bsErr := resourcerbac.BootstrapResourceRBACAndWait(ctx, c.client, c.impersonatedOrNil(ctx), created, resourcerbac.Organizations); bsErr != nil {
		if delErr := c.client.CoreV1().Namespaces().Delete(ctx, created.Name, metav1.DeleteOptions{}); delErr != nil && !k8serrors.IsNotFound(delErr) {
			slog.ErrorContext(ctx, "rollback: deleting organization namespace after RBAC bootstrap failure",
				slog.String("namespace", created.Name),
				slog.Any("error", delErr),
			)
		}
		return nil, bsErr
	}
	return created, nil
}

func (c *K8sClient) impersonatedOrNil(ctx context.Context) kubernetes.Interface {
	if rpc.HasImpersonatedClients(ctx) {
		return rpc.ImpersonatedClientsetFromContext(ctx)
	}
	return nil
}

// UpdateOrganization updates the description, display name, and gateway
// namespace annotations on an organization namespace. Nil pointers preserve
// existing values; empty strings clear the corresponding annotation.
func (c *K8sClient) UpdateOrganization(ctx context.Context, name string, displayName, description, gatewayNamespace *string) (*corev1.Namespace, error) {
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
	if gatewayNamespace != nil {
		if *gatewayNamespace == "" {
			delete(ns.Annotations, v1alpha2.AnnotationGatewayNamespace)
		} else {
			ns.Annotations[v1alpha2.AnnotationGatewayNamespace] = *gatewayNamespace
		}
	}
	return c.clientset(ctx).CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
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
	return c.clientset(ctx).CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
}

// GetGatewayNamespace reads the gateway-namespace annotation from an org
// namespace. Returns empty string if not set.
func GetGatewayNamespace(ns *corev1.Namespace) string {
	if ns.Annotations == nil {
		return ""
	}
	return ns.Annotations[v1alpha2.AnnotationGatewayNamespace]
}

// SetGatewayNamespace writes (or clears) the gateway-namespace annotation on
// the org namespace. An empty value deletes the annotation.
func (c *K8sClient) SetGatewayNamespace(ctx context.Context, name, value string) error {
	ns, err := c.GetOrganization(ctx, name)
	if err != nil {
		return err
	}
	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}
	if value == "" {
		delete(ns.Annotations, v1alpha2.AnnotationGatewayNamespace)
	} else {
		ns.Annotations[v1alpha2.AnnotationGatewayNamespace] = value
	}
	_, err = c.clientset(ctx).CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
	return err
}

// UpdateOrganizationSharing updates the sharing annotations on an organization namespace.
func (c *K8sClient) UpdateOrganizationSharing(ctx context.Context, name string, shareUsers, shareRoles, rbacShareUsers []secrets.AnnotationGrant) (*corev1.Namespace, error) {
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
	rbacUsersJSON, err := json.Marshal(rbacShareUsers)
	if err != nil {
		return nil, fmt.Errorf("marshaling rbac-share-users: %w", err)
	}
	ns.Annotations[v1alpha2.AnnotationShareUsers] = string(usersJSON)
	ns.Annotations[v1alpha2.AnnotationShareRoles] = string(rolesJSON)
	ns.Annotations[v1alpha2.AnnotationRBACShareUsers] = string(rbacUsersJSON)
	// Locked annotations (share-users / share-roles / rbac-share-users) must
	// use the privileged client because admission denies user writes.
	updated, err := c.client.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	// Synchronously reconcile per-resource RBAC so that newly-granted users
	// get their ClusterRoleBindings immediately rather than waiting for the
	// async reconciler. Without this, callers that grant viewer/editor access
	// see a window where the granted user can authenticate but cannot list
	// the org via the impersonated client.
	if err := resourcerbac.EnsureResourceRBAC(ctx, c.client, updated, resourcerbac.Organizations); err != nil {
		return nil, fmt.Errorf("reconciling organization RBAC after sharing update: %w", err)
	}
	return updated, nil
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

// UpdateOrganizationDefaultRoleGrants writes only the
// AnnotationDefaultShareRoles annotation on an organization namespace,
// leaving AnnotationDefaultShareUsers untouched. Used when seeding the
// default role grants (Owner/Editor/Viewer) at org-create time.
func (c *K8sClient) UpdateOrganizationDefaultRoleGrants(ctx context.Context, name string, defaultRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "updating organization default role grants in kubernetes",
		slog.String("name", name),
	)
	ns, err := c.GetOrganization(ctx, name)
	if err != nil {
		return nil, err
	}
	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}
	rolesJSON, err := json.Marshal(defaultRoles)
	if err != nil {
		return nil, fmt.Errorf("marshaling default-share-roles: %w", err)
	}
	ns.Annotations[v1alpha2.AnnotationDefaultShareRoles] = string(rolesJSON)
	// Locked annotation (default-share-roles) must use the privileged client
	// because admission denies user writes.
	return c.client.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
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
	// Locked annotations (default-share-*) must use the privileged client
	// because admission denies user writes.
	return c.client.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
}

func parseGrantAnnotation(ns *corev1.Namespace, key string) ([]secrets.AnnotationGrant, error) {
	return legacy.ParseGrants(ns.Annotations, key)
}
