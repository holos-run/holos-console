package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secretrbac"
	"github.com/holos-run/holos-console/console/secrets"
	"github.com/holos-run/holos-console/console/sharing/legacy"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// K8sClient wraps Kubernetes client operations for projects (namespaces).
type K8sClient struct {
	client   kubernetes.Interface
	Resolver *resolver.Resolver
}

// NewK8sClient creates a client for project operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
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

// ListProjects returns all project namespaces. When org is non-empty, filters by organization.
// When parentNs is non-empty, additionally filters to direct children of that parent namespace.
func (c *K8sClient) ListProjects(ctx context.Context, org, parentNs string) ([]*corev1.Namespace, error) {
	labelSelector := v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue + "," +
		v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeProject
	if org != "" {
		labelSelector += "," + v1alpha2.LabelOrganization + "=" + org
	}
	slog.DebugContext(ctx, "listing projects from kubernetes",
		slog.String("labelSelector", labelSelector),
		slog.String("parentNs", parentNs),
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
		if _, err := c.Resolver.ProjectFromNamespace(list.Items[i].Name); err != nil {
			var pme *resolver.PrefixMismatchError
			if errors.As(err, &pme) {
				slog.DebugContext(ctx, "filtering project namespace with prefix mismatch",
					slog.String("namespace", list.Items[i].Name),
					slog.String("reason", err.Error()),
				)
				continue
			}
		}
		// Filter by parent namespace when specified.
		if parentNs != "" && list.Items[i].Labels[v1alpha2.AnnotationParent] != parentNs {
			continue
		}
		result = append(result, &list.Items[i])
	}
	if !rpc.HasImpersonatedClients(ctx) {
		return result, nil
	}
	authorized := make([]*corev1.Namespace, 0, len(result))
	for _, ns := range result {
		name, err := c.Resolver.ProjectFromNamespace(ns.Name)
		if err != nil {
			return nil, err
		}
		got, err := c.GetProject(ctx, name)
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

// GetProject retrieves a managed project namespace by name.
// The name is the user-facing project name (not the Kubernetes namespace).
func (c *K8sClient) GetProject(ctx context.Context, name string) (*corev1.Namespace, error) {
	nsName := c.Resolver.ProjectNamespace(name)
	slog.DebugContext(ctx, "getting project from kubernetes",
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
	if ns.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeProject {
		return nil, fmt.Errorf("namespace %q is not a project", nsName)
	}
	return ns, nil
}

// BuildProjectNamespace constructs the in-memory *corev1.Namespace object
// CreateProject would write, without contacting the cluster. The
// HOL-812 CreateProject RPC wire-up uses this helper to produce the
// "base" Namespace the HOL-810 render path unifies template-produced
// Namespace patches into (ADR 034 Decision 1 — the RPC-built namespace
// is always the base). The existing CreateProject path continues to
// call this helper and then issue a typed Create so the no-bindings
// happy path stays byte-identical to the pre-HOL-812 behavior.
func (c *K8sClient) BuildProjectNamespace(name, displayName, description, org, parentNs, creatorEmail string, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	nsName := c.Resolver.ProjectNamespace(name)
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
	if len(defaultShareUsers) > 0 {
		defaultUsersJSON, err := json.Marshal(defaultShareUsers)
		if err != nil {
			return nil, fmt.Errorf("marshaling default-share-users: %w", err)
		}
		annotations[v1alpha2.AnnotationDefaultShareUsers] = string(defaultUsersJSON)
	}
	if len(defaultShareRoles) > 0 {
		defaultRolesJSON, err := json.Marshal(defaultShareRoles)
		if err != nil {
			return nil, fmt.Errorf("marshaling default-share-roles: %w", err)
		}
		annotations[v1alpha2.AnnotationDefaultShareRoles] = string(defaultRolesJSON)
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
	labels := map[string]string{
		v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
		v1alpha2.LabelProject:      name,
	}
	if org != "" {
		labels[v1alpha2.LabelOrganization] = org
	}
	if parentNs != "" {
		labels[v1alpha2.AnnotationParent] = parentNs
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nsName,
			Labels:      labels,
			Annotations: annotations,
		},
	}, nil
}

// CreateProject creates a new namespace with managed-by and resource-type labels.
// parentNs is the Kubernetes namespace name of the immediate parent (org or folder namespace).
// When non-empty, it is stored in the v1alpha2.AnnotationParent label for hierarchy traversal.
func (c *K8sClient) CreateProject(ctx context.Context, name, displayName, description, org, parentNs, creatorEmail, creatorSubject string, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "creating project in kubernetes",
		slog.String("name", name),
		slog.String("namespace", c.Resolver.ProjectNamespace(name)),
		slog.String("parent", parentNs),
	)
	ns, err := c.BuildProjectNamespace(name, displayName, description, org, parentNs, creatorEmail, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles)
	if err != nil {
		return nil, err
	}
	if creatorSubject != "" {
		if ns.Annotations == nil {
			ns.Annotations = make(map[string]string)
		}
		ns.Annotations[v1alpha2.AnnotationCreatorSubject] = creatorSubject
	}
	rbacShareUsers := secrets.RBACUserGrantsForSubjects(shareUsers, secrets.UserIdentity{Email: creatorEmail, Subject: creatorSubject})
	if len(rbacShareUsers) > 0 {
		if ns.Annotations == nil {
			ns.Annotations = make(map[string]string)
		}
		rbacUsersJSON, err := json.Marshal(rbacShareUsers)
		if err != nil {
			return nil, fmt.Errorf("marshaling rbac-share-users: %w", err)
		}
		ns.Annotations[v1alpha2.AnnotationRBACShareUsers] = string(rbacUsersJSON)
	}
	return c.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
}

// EnsureProjectSecretRBAC materializes the project-scoped Secret Roles and the
// RoleBindings implied by the project's resolved share grants. This runs as the
// console service account per ADR 036 Decision 5 because it reconciles RBAC for
// humans rather than acting as the requesting human.
func (c *K8sClient) EnsureProjectSecretRBAC(ctx context.Context, project string, shareUsers, shareRoles []secrets.AnnotationGrant) error {
	ns, err := c.GetProject(ctx, project)
	if err != nil {
		return err
	}
	return c.EnsureProjectSecretRBACForNamespace(ctx, ns.Name, namespaceOwnerRefs(ns), shareUsers, shareRoles)
}

func (c *K8sClient) EnsureProjectSecretRBACForNamespace(ctx context.Context, namespace string, ownerRefs []metav1.OwnerReference, shareUsers, shareRoles []secrets.AnnotationGrant) error {
	roleOwners := make(map[string][]metav1.OwnerReference)
	for _, role := range secretrbac.ProjectSecretRoles(namespace, ownerRefs) {
		if err := c.applyRole(ctx, role); err != nil {
			return fmt.Errorf("applying project secret role %q: %w", role.Name, err)
		}
		roleOwners[role.Name] = []metav1.OwnerReference{{
			APIVersion: rbacv1.SchemeGroupVersion.String(),
			Kind:       "Role",
			Name:       role.Name,
			UID:        role.UID,
		}}
	}
	for _, grant := range secrets.DeduplicateGrants(shareUsers) {
		if grant.Principal == "" {
			continue
		}
		binding := secretrbac.RoleBinding(namespace, secretrbac.ShareTargetUser, grant.Principal, grant.Role, roleOwners[secretrbac.RoleName(grant.Role)])
		if err := c.applyRoleBinding(ctx, binding); err != nil {
			return fmt.Errorf("applying project secret user binding %q: %w", binding.Name, err)
		}
	}
	for _, grant := range secrets.DeduplicateGrants(shareRoles) {
		if grant.Principal == "" {
			continue
		}
		binding := secretrbac.RoleBinding(namespace, secretrbac.ShareTargetGroup, grant.Principal, grant.Role, roleOwners[secretrbac.RoleName(grant.Role)])
		if err := c.applyRoleBinding(ctx, binding); err != nil {
			return fmt.Errorf("applying project secret group binding %q: %w", binding.Name, err)
		}
	}
	return nil
}

func namespaceOwnerRefs(ns *corev1.Namespace) []metav1.OwnerReference {
	if ns == nil {
		return nil
	}
	return []metav1.OwnerReference{{
		APIVersion: "v1",
		Kind:       "Namespace",
		Name:       ns.Name,
		UID:        ns.UID,
	}}
}

func (c *K8sClient) applyRole(ctx context.Context, role *rbacv1.Role) error {
	created, err := c.client.RbacV1().Roles(role.Namespace).Create(ctx, role, metav1.CreateOptions{})
	if err == nil {
		*role = *created
		return nil
	}
	if !k8serrors.IsAlreadyExists(err) {
		return err
	}
	existing, err := c.client.RbacV1().Roles(role.Namespace).Get(ctx, role.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	existing.Labels = role.Labels
	existing.OwnerReferences = role.OwnerReferences
	existing.Rules = role.Rules
	updated, err := c.client.RbacV1().Roles(role.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err == nil {
		*role = *updated
	}
	return err
}

func (c *K8sClient) applyRoleBinding(ctx context.Context, binding *rbacv1.RoleBinding) error {
	created, err := c.client.RbacV1().RoleBindings(binding.Namespace).Create(ctx, binding, metav1.CreateOptions{})
	if err == nil {
		*binding = *created
		return nil
	}
	if !k8serrors.IsAlreadyExists(err) {
		return err
	}
	existing, err := c.client.RbacV1().RoleBindings(binding.Namespace).Get(ctx, binding.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if existing.RoleRef != binding.RoleRef {
		if err := c.client.RbacV1().RoleBindings(binding.Namespace).Delete(ctx, binding.Name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
		created, err := c.client.RbacV1().RoleBindings(binding.Namespace).Create(ctx, binding, metav1.CreateOptions{})
		if err == nil {
			*binding = *created
		}
		return err
	}
	existing.Labels = binding.Labels
	existing.OwnerReferences = binding.OwnerReferences
	existing.Subjects = binding.Subjects
	updated, err := c.client.RbacV1().RoleBindings(binding.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err == nil {
		*binding = *updated
	}
	return err
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
	return c.clientset(ctx).CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
}

// UpdateParentLabel updates the parent label on a project namespace.
func (c *K8sClient) UpdateParentLabel(ctx context.Context, name, newParentNs string) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "updating project parent label in kubernetes",
		slog.String("name", name),
		slog.String("newParent", newParentNs),
	)
	ns, err := c.GetProject(ctx, name)
	if err != nil {
		return nil, err
	}
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	ns.Labels[v1alpha2.AnnotationParent] = newParentNs
	return c.clientset(ctx).CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
}

// GetNamespace retrieves any namespace by its full Kubernetes name.
// Used for resolving parent namespaces during reparent validation.
func (c *K8sClient) GetNamespace(ctx context.Context, nsName string) (*corev1.Namespace, error) {
	return c.clientset(ctx).CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
}

// DeleteProject deletes a managed project namespace.
// Returns an error if the namespace does not have the managed-by label.
func (c *K8sClient) DeleteProject(ctx context.Context, name string) error {
	slog.DebugContext(ctx, "deleting project from kubernetes",
		slog.String("name", name),
	)
	// Verify the namespace is managed before deleting.
	ns, err := c.GetProject(ctx, name)
	if err != nil {
		return err
	}
	return c.clientset(ctx).CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
}

// UpdateProjectSharing updates the sharing annotations on a managed namespace.
func (c *K8sClient) UpdateProjectSharing(ctx context.Context, name string, shareUsers, shareRoles, rbacShareUsers []secrets.AnnotationGrant) (*corev1.Namespace, error) {
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
	return c.clientset(ctx).CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
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

// GetOrganization returns the organization label value from a namespace.
func GetOrganization(ns *corev1.Namespace) string {
	if ns.Labels == nil {
		return ""
	}
	return ns.Labels[v1alpha2.LabelOrganization]
}

// GetProjectOrg returns the organization name for the given project.
// Returns an empty string if the project is not associated with an organization.
func (c *K8sClient) GetProjectOrg(ctx context.Context, project string) (string, error) {
	ns, err := c.GetProject(ctx, project)
	if err != nil {
		return "", fmt.Errorf("getting project %q: %w", project, err)
	}
	return GetOrganization(ns), nil
}

// ProjectFolderResolver wraps K8sClient and a Walker to implement
// deployments.AncestorWalker by resolving the folder ancestry of a project.
type ProjectFolderResolver struct {
	k8s    *K8sClient
	walker walkerAncestors
}

// walkerAncestors is the minimal interface needed from resolver.Walker.
type walkerAncestors interface {
	WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error)
}

// NewProjectFolderResolver creates a resolver that returns folder names for a project.
func NewProjectFolderResolver(k8s *K8sClient, walker walkerAncestors) *ProjectFolderResolver {
	return &ProjectFolderResolver{k8s: k8s, walker: walker}
}

// GetProjectFolders returns the ordered list of folder names in the ancestor chain
// from the organization down to (but not including) the project.
// Implements deployments.AncestorWalker.
func (r *ProjectFolderResolver) GetProjectFolders(ctx context.Context, project string) ([]string, error) {
	if r.walker == nil {
		return nil, nil
	}
	projectNs := r.k8s.Resolver.ProjectNamespace(project)
	ancestors, err := r.walker.WalkAncestors(ctx, projectNs)
	if err != nil {
		return nil, fmt.Errorf("walking ancestors for project %q: %w", project, err)
	}

	// ancestors is child→parent order (project first, org last).
	// Reverse to get org→project order, then extract only folder namespaces.
	var folders []string
	for i := len(ancestors) - 1; i >= 0; i-- {
		ns := ancestors[i]
		kind, name, err := r.k8s.Resolver.ResourceTypeFromNamespace(ns.Name)
		if err != nil {
			continue
		}
		if kind == v1alpha2.ResourceTypeFolder {
			folders = append(folders, name)
		}
	}
	return folders, nil
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

// UpdateProjectDefaultSharing updates the default sharing annotations on a managed namespace.
func (c *K8sClient) UpdateProjectDefaultSharing(ctx context.Context, name string, defaultUsers, defaultRoles []secrets.AnnotationGrant) (*corev1.Namespace, error) {
	slog.DebugContext(ctx, "updating project default sharing in kubernetes",
		slog.String("name", name),
	)
	ns, err := c.GetProject(ctx, name)
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
	return c.clientset(ctx).CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
}

// ProjectCreatorAdapter adapts the projects K8sClient to satisfy the
// organizations.ProjectCreator interface.
type ProjectCreatorAdapter struct {
	K8s *K8sClient
}

// CreateProject creates a project namespace, forwarding default sharing grants
// so that seeded default projects inherit org-level defaults, mirroring the
// production path in projects.Handler.CreateProject.
func (a *ProjectCreatorAdapter) CreateProject(ctx context.Context, name, displayName, description, org, parentNs, creatorEmail, creatorSubject string, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles []secrets.AnnotationGrant) error {
	_, err := a.K8s.CreateProject(ctx, name, displayName, description, org, parentNs, creatorEmail, creatorSubject, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles)
	return err
}

// DeleteProject delegates to the K8sClient.
func (a *ProjectCreatorAdapter) DeleteProject(ctx context.Context, name string) error {
	return a.K8s.DeleteProject(ctx, name)
}

// NamespaceExists delegates to the K8sClient.
func (a *ProjectCreatorAdapter) NamespaceExists(ctx context.Context, nsName string) (bool, error) {
	return a.K8s.NamespaceExists(ctx, nsName)
}

func parseGrantAnnotation(ns *corev1.Namespace, key string) ([]secrets.AnnotationGrant, error) {
	return legacy.ParseGrants(ns.Annotations, key)
}
