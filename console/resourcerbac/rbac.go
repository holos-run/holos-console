// Package resourcerbac provides per-resource RBAC helpers for console-owned
// resources.
package resourcerbac

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rbacname"
	"github.com/holos-run/holos-console/console/secrets"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

const (
	TemplatesAPIGroup = "templates.holos.run"

	LabelRolePurpose     = "console.holos.run/role-purpose"
	LabelResourceKind    = "console.holos.run/resource-kind"
	LabelResourceName    = "console.holos.run/resource-name"
	LabelShareTarget     = "console.holos.run/share-target"
	LabelShareTargetName = "console.holos.run/share-target-name"
	LabelResourceRole    = "holos.run/role"

	AnnotationCreatorSubject  = "console.holos.run/creator-sub"
	AnnotationShareTargetName = LabelShareTargetName

	RoleViewer = "viewer"
	RoleEditor = "editor"
	RoleOwner  = "owner"

	ShareTargetUser  = "user"
	ShareTargetGroup = "group"

	OIDCPrefix = "oidc:"
)

// KindConfig describes the RBAC surface for one console-managed resource kind.
type KindConfig struct {
	Kind            string
	Resource        string
	RolePurpose     string
	ControllerName  string
	NewObject       func() metav1.Object
	APIGroup        string
	OwnerAPIVersion string
	OwnerKind       string
	ObjectName      func(metav1.Object) string
	RBACNamespace   func(metav1.Object) string
	Matches         func(metav1.Object) bool
}

var (
	Templates = KindConfig{
		Kind:           "Template",
		Resource:       "templates",
		RolePurpose:    "template",
		ControllerName: "template-rbac-controller",
		NewObject:      func() metav1.Object { return &templatesv1alpha1.Template{} },
	}
	TemplatePolicies = KindConfig{
		Kind:           "TemplatePolicy",
		Resource:       "templatepolicies",
		RolePurpose:    "templatepolicy",
		ControllerName: "template-policy-rbac-controller",
		NewObject:      func() metav1.Object { return &templatesv1alpha1.TemplatePolicy{} },
	}
	TemplatePolicyBindings = KindConfig{
		Kind:           "TemplatePolicyBinding",
		Resource:       "templatepolicybindings",
		RolePurpose:    "templatepolicybinding",
		ControllerName: "template-policy-binding-rbac-controller",
		NewObject:      func() metav1.Object { return &templatesv1alpha1.TemplatePolicyBinding{} },
	}
	TemplateGrants = KindConfig{
		Kind:           "TemplateGrant",
		Resource:       "templategrants",
		RolePurpose:    "templategrant",
		ControllerName: "template-grant-rbac-controller",
		NewObject:      func() metav1.Object { return &templatesv1alpha1.TemplateGrant{} },
	}
	TemplateDependencies = KindConfig{
		Kind:           "TemplateDependency",
		Resource:       "templatedependencies",
		RolePurpose:    "templatedependency",
		ControllerName: "template-dependency-rbac-controller",
		NewObject:      func() metav1.Object { return &templatesv1alpha1.TemplateDependency{} },
	}
	TemplateRequirements = KindConfig{
		Kind:           "TemplateRequirement",
		Resource:       "templaterequirements",
		RolePurpose:    "templaterequirement",
		ControllerName: "template-requirement-rbac-controller",
		NewObject:      func() metav1.Object { return &templatesv1alpha1.TemplateRequirement{} },
	}
	Organizations = namespaceKindConfig(
		"Organization",
		"organization",
		"organization-rbac-controller",
		v1alpha2.ResourceTypeOrganization,
	)
	Folders = namespaceKindConfig(
		"Folder",
		"folder",
		"folder-rbac-controller",
		v1alpha2.ResourceTypeFolder,
	)
	Projects = namespaceKindConfig(
		"Project",
		"project",
		"project-rbac-controller",
		v1alpha2.ResourceTypeProject,
	)
	Resources = KindConfig{
		Kind:            "Resource",
		Resource:        "namespaces",
		RolePurpose:     "resource",
		ControllerName:  "resource-rbac-controller",
		NewObject:       func() metav1.Object { return &corev1.Namespace{} },
		APIGroup:        "",
		OwnerAPIVersion: "v1",
		OwnerKind:       "Namespace",
		ObjectName:      namespaceObjectName,
		RBACNamespace:   namespaceRBACNamespace,
		Matches: func(obj metav1.Object) bool {
			// TODO(HOL-1061 cleanup): delete this generic Resource surface
			// when console/resources is retired; until then it mirrors the
			// folder/project namespaces currently listed by that handler.
			resourceType := obj.GetLabels()[v1alpha2.LabelResourceType]
			return isManagedNamespace(obj) && (resourceType == v1alpha2.ResourceTypeFolder || resourceType == v1alpha2.ResourceTypeProject)
		},
	}
)

func namespaceKindConfig(kind, rolePurpose, controllerName, resourceType string) KindConfig {
	return KindConfig{
		Kind:            kind,
		Resource:        "namespaces",
		RolePurpose:     rolePurpose,
		ControllerName:  controllerName,
		NewObject:       func() metav1.Object { return &corev1.Namespace{} },
		APIGroup:        "",
		OwnerAPIVersion: "v1",
		OwnerKind:       "Namespace",
		ObjectName:      namespaceObjectName,
		RBACNamespace:   namespaceRBACNamespace,
		Matches: func(obj metav1.Object) bool {
			return isManagedNamespace(obj) && obj.GetLabels()[v1alpha2.LabelResourceType] == resourceType
		},
	}
}

// AllKindConfigs returns every templates.holos.run kind managed by this
// package.
func AllKindConfigs() []KindConfig {
	return []KindConfig{
		Templates,
		TemplatePolicies,
		TemplatePolicyBindings,
		TemplateGrants,
		TemplateDependencies,
		TemplateRequirements,
	}
}

// TopResourceKindConfigs returns the namespace-backed resources managed by
// the top-level console resource handlers.
func TopResourceKindConfigs() []KindConfig {
	return []KindConfig{
		Organizations,
		Folders,
		Projects,
		Resources,
	}
}

// EnsureResourceRBAC provisions the viewer/editor/owner Roles for obj and
// reconciles RoleBindings from creator/share annotations. Roles and
// RoleBindings are owner-referenced to obj so Kubernetes garbage collection
// removes them when the resource is deleted.
func EnsureResourceRBAC(ctx context.Context, client kubernetes.Interface, obj metav1.Object, cfg KindConfig) error {
	if client == nil {
		return fmt.Errorf("resource RBAC client is required")
	}
	if obj == nil {
		return fmt.Errorf("resource object is required")
	}
	if !matches(obj, cfg) {
		return nil
	}
	namespace, name := rbacNamespace(obj, cfg), objectName(obj, cfg)
	if namespace == "" || name == "" {
		return fmt.Errorf("resource namespace and name are required")
	}
	ownerRefs := OwnerReferences(obj, cfg)
	for _, role := range ResourceRoles(namespace, name, cfg, ownerRefs) {
		if err := applyRole(ctx, client, role); err != nil {
			return fmt.Errorf("applying %s role %q: %w", cfg.Kind, role.Name, err)
		}
	}
	return reconcileRoleBindings(ctx, client, obj, cfg, namespace, name, ownerRefs)
}

func creatorSubject(obj metav1.Object) string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return ""
	}
	return strings.TrimSpace(annotations[AnnotationCreatorSubject])
}

func ResourceRoles(namespace, name string, cfg KindConfig, ownerRefs []metav1.OwnerReference) []*rbacv1.Role {
	return []*rbacv1.Role{
		resourceRole(namespace, name, cfg, RoleViewer, viewerVerbs(), nil, ownerRefs),
		resourceRole(namespace, name, cfg, RoleEditor, editorVerbs(), nil, ownerRefs),
		resourceRole(namespace, name, cfg, RoleOwner, ownerVerbs(), ownerRules(name, cfg), ownerRefs),
	}
}

func resourceRole(namespace, name string, cfg KindConfig, role string, verbs []string, extraRules []rbacv1.PolicyRule, ownerRefs []metav1.OwnerReference) *rbacv1.Role {
	apiGroup := cfg.APIGroup
	if cfg.APIGroup == "" && cfg.OwnerAPIVersion == "" {
		apiGroup = TemplatesAPIGroup
	}
	rules := []rbacv1.PolicyRule{{
		APIGroups:     []string{apiGroup},
		Resources:     []string{cfg.Resource},
		ResourceNames: []string{name},
		Verbs:         verbs,
	}}
	rules = append(rules, extraRules...)
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            RoleName(name, cfg, role),
			Namespace:       namespace,
			Labels:          RoleLabels(name, cfg, role),
			OwnerReferences: ownerRefs,
		},
		Rules: rules,
	}
}

func viewerVerbs() []string {
	return []string{"get"}
}

func editorVerbs() []string {
	return []string{"get", "update", "patch"}
}

func ownerVerbs() []string {
	return []string{"get", "update", "patch", "delete"}
}

func ownerRules(name string, cfg KindConfig) []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{rbacv1.GroupName},
			Resources: []string{"rolebindings"},
			Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
		},
		{
			APIGroups:     []string{rbacv1.GroupName},
			Resources:     []string{"roles"},
			ResourceNames: []string{RoleName(name, cfg, RoleViewer), RoleName(name, cfg, RoleEditor), RoleName(name, cfg, RoleOwner)},
			Verbs:         []string{"get", "list", "watch", "bind"},
		},
	}
}

func RoleName(name string, cfg KindConfig, role string) string {
	return "holos-" + cfg.RolePurpose + "-" + name + "-" + NormalizeRole(role)
}

func RoleLabels(name string, cfg KindConfig, role string) map[string]string {
	return map[string]string{
		v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
		LabelRolePurpose:        cfg.RolePurpose,
		LabelResourceKind:       strings.ToLower(cfg.Kind),
		LabelResourceName:       name,
		LabelResourceRole:       cfg.RolePurpose + "-" + NormalizeRole(role),
	}
}

func RoleBindingLabels(name string, cfg KindConfig, target, principal, role string) map[string]string {
	labels := RoleLabels(name, cfg, role)
	labels[LabelShareTarget] = NormalizeTarget(target)
	labels[LabelShareTargetName] = labelValue(principal)
	return labels
}

func RoleBinding(namespace, name string, cfg KindConfig, target, principal, role string, ownerRefs []metav1.OwnerReference) *rbacv1.RoleBinding {
	target = NormalizeTarget(target)
	role = NormalizeRole(role)
	subjectKind := rbacv1.UserKind
	if target == ShareTargetGroup {
		subjectKind = rbacv1.GroupKind
	}
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            RoleBindingName(name, cfg, role, target, principal),
			Namespace:       namespace,
			Labels:          RoleBindingLabels(name, cfg, target, principal, role),
			Annotations:     map[string]string{AnnotationShareTargetName: OIDCPrincipal(principal)},
			OwnerReferences: ownerRefs,
		},
		Subjects: []rbacv1.Subject{{
			Kind:     subjectKind,
			APIGroup: rbacv1.GroupName,
			Name:     OIDCPrincipal(principal),
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     RoleName(name, cfg, role),
		},
	}
}

func RoleBindingName(name string, cfg KindConfig, role, target, principal string) string {
	rolePurpose := cfg.RolePurpose + "-" + name + "-" + NormalizeRole(role)
	return rbacname.RoleBindingName(rolePurpose, target, OIDCPrincipal(principal))
}

func OwnerReferences(obj metav1.Object, cfg KindConfig) []metav1.OwnerReference {
	controller := true
	blockOwnerDeletion := true
	apiVersion := cfg.OwnerAPIVersion
	if apiVersion == "" {
		apiVersion = templatesv1alpha1.GroupVersion.String()
	}
	kind := cfg.OwnerKind
	if kind == "" {
		kind = cfg.Kind
	}
	return []metav1.OwnerReference{{
		APIVersion:         apiVersion,
		Kind:               kind,
		Name:               obj.GetName(),
		UID:                obj.GetUID(),
		Controller:         &controller,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}}
}

func matches(obj metav1.Object, cfg KindConfig) bool {
	if cfg.Matches == nil {
		return true
	}
	return cfg.Matches(obj)
}

func objectName(obj metav1.Object, cfg KindConfig) string {
	if cfg.ObjectName != nil {
		return cfg.ObjectName(obj)
	}
	return obj.GetName()
}

func rbacNamespace(obj metav1.Object, cfg KindConfig) string {
	if cfg.RBACNamespace != nil {
		return cfg.RBACNamespace(obj)
	}
	return obj.GetNamespace()
}

func namespaceObjectName(obj metav1.Object) string {
	return obj.GetName()
}

func namespaceRBACNamespace(obj metav1.Object) string {
	return obj.GetName()
}

func isManagedNamespace(obj metav1.Object) bool {
	return obj.GetLabels()[v1alpha2.LabelManagedBy] == v1alpha2.ManagedByValue
}

func OIDCPrincipal(principal string) string {
	principal = strings.TrimSpace(principal)
	if principal == "" || strings.HasPrefix(principal, OIDCPrefix) {
		return principal
	}
	return OIDCPrefix + principal
}

func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleOwner:
		return RoleOwner
	case RoleEditor:
		return RoleEditor
	default:
		return RoleViewer
	}
}

func NormalizeTarget(target string) string {
	if strings.EqualFold(strings.TrimSpace(target), ShareTargetGroup) {
		return ShareTargetGroup
	}
	return ShareTargetUser
}

func RoleFromLabels(labels map[string]string) string {
	if labels != nil {
		configs := append(AllKindConfigs(), TopResourceKindConfigs()...)
		for _, cfg := range configs {
			if value := strings.TrimPrefix(labels[LabelResourceRole], cfg.RolePurpose+"-"); value != labels[LabelResourceRole] {
				return NormalizeRole(value)
			}
		}
	}
	return RoleViewer
}

func reconcileRoleBindings(ctx context.Context, client kubernetes.Interface, obj metav1.Object, cfg KindConfig, namespace, name string, ownerRefs []metav1.OwnerReference) error {
	desired := make(map[string]*rbacv1.RoleBinding)
	addDesired := func(target string, grant secrets.AnnotationGrant) {
		if strings.TrimSpace(grant.Principal) == "" {
			return
		}
		binding := RoleBinding(namespace, name, cfg, target, grant.Principal, grant.Role, ownerRefs)
		desired[binding.Name] = binding
	}
	if creatorSub := creatorSubject(obj); creatorSub != "" {
		addDesired(ShareTargetUser, secrets.AnnotationGrant{Principal: creatorSub, Role: RoleOwner})
	}
	users, err := parseShareGrants(obj.GetAnnotations(), v1alpha2.AnnotationShareUsers)
	if err != nil {
		return err
	}
	for _, grant := range secrets.DeduplicateGrants(users) {
		addDesired(ShareTargetUser, grant)
	}
	groups, err := parseShareGrants(obj.GetAnnotations(), v1alpha2.AnnotationShareRoles)
	if err != nil {
		return err
	}
	for _, grant := range secrets.DeduplicateGrants(groups) {
		addDesired(ShareTargetGroup, grant)
	}

	selector := labels.SelectorFromSet(labels.Set{
		LabelRolePurpose:  cfg.RolePurpose,
		LabelResourceName: name,
	}).String()
	current, err := client.RbacV1().RoleBindings(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return fmt.Errorf("listing %s role bindings: %w", cfg.Kind, err)
	}
	for i := range current.Items {
		existing := current.Items[i]
		if _, ok := desired[existing.Name]; ok {
			continue
		}
		if err := client.RbacV1().RoleBindings(namespace).Delete(ctx, existing.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting stale %s role binding %q: %w", cfg.Kind, existing.Name, err)
		}
	}
	for _, binding := range desired {
		if err := applyRoleBinding(ctx, client, binding); err != nil {
			return fmt.Errorf("applying %s annotated role binding %q: %w", cfg.Kind, binding.Name, err)
		}
	}
	return nil
}

func parseShareGrants(annotations map[string]string, key string) ([]secrets.AnnotationGrant, error) {
	if annotations == nil || annotations[key] == "" {
		return nil, nil
	}
	var grants []secrets.AnnotationGrant
	if err := json.Unmarshal([]byte(annotations[key]), &grants); err != nil {
		return nil, fmt.Errorf("invalid %s annotation: %w", key, err)
	}
	return grants, nil
}

func applyRole(ctx context.Context, client kubernetes.Interface, role *rbacv1.Role) error {
	created, err := client.RbacV1().Roles(role.Namespace).Create(ctx, role, metav1.CreateOptions{})
	if err == nil {
		*role = *created
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	existing, err := client.RbacV1().Roles(role.Namespace).Get(ctx, role.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	existing.Labels = role.Labels
	existing.Rules = role.Rules
	existing.OwnerReferences = role.OwnerReferences
	updated, err := client.RbacV1().Roles(role.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*role = *updated
	return nil
}

func applyRoleBinding(ctx context.Context, client kubernetes.Interface, binding *rbacv1.RoleBinding) error {
	created, err := client.RbacV1().RoleBindings(binding.Namespace).Create(ctx, binding, metav1.CreateOptions{})
	if err == nil {
		*binding = *created
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	existing, err := client.RbacV1().RoleBindings(binding.Namespace).Get(ctx, binding.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if existing.RoleRef != binding.RoleRef {
		if err := client.RbacV1().RoleBindings(binding.Namespace).Delete(ctx, binding.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		recreated, err := client.RbacV1().RoleBindings(binding.Namespace).Create(ctx, binding, metav1.CreateOptions{})
		if err == nil {
			*binding = *recreated
		}
		return err
	}
	existing.Labels = binding.Labels
	existing.Annotations = binding.Annotations
	existing.Subjects = binding.Subjects
	existing.OwnerReferences = binding.OwnerReferences
	updated, err := client.RbacV1().RoleBindings(binding.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*binding = *updated
	return nil
}

func labelValue(value string) string {
	var b strings.Builder
	lastSep := false
	for _, r := range value {
		ok := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
		if ok {
			b.WriteRune(r)
			lastSep = false
			continue
		}
		if !lastSep {
			b.WriteByte('_')
			lastSep = true
		}
	}
	return strings.Trim(b.String(), "-_.")
}
