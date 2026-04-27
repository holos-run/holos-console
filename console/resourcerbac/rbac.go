// Package resourcerbac provides per-resource RBAC helpers for
// templates.holos.run resources.
package resourcerbac

import (
	"context"
	"fmt"
	"strings"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rbacname"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// KindConfig describes the RBAC surface for one templates.holos.run resource
// kind.
type KindConfig struct {
	Kind           string
	Resource       string
	RolePurpose    string
	ControllerName string
	NewObject      func() metav1.Object
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
)

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

// EnsureResourceRBAC provisions the viewer/editor/owner Roles for obj and,
// when obj carries console.holos.run/creator-sub, an owner RoleBinding for
// the creating OIDC subject. Roles and RoleBindings are owner-referenced to
// obj so Kubernetes garbage collection removes them when the resource is
// deleted.
func EnsureResourceRBAC(ctx context.Context, client kubernetes.Interface, obj metav1.Object, cfg KindConfig) error {
	if client == nil {
		return fmt.Errorf("resource RBAC client is required")
	}
	if obj == nil {
		return fmt.Errorf("resource object is required")
	}
	namespace, name := obj.GetNamespace(), obj.GetName()
	if namespace == "" || name == "" {
		return fmt.Errorf("resource namespace and name are required")
	}
	ownerRefs := OwnerReferences(obj, cfg)
	for _, role := range ResourceRoles(namespace, name, cfg, ownerRefs) {
		if err := applyRole(ctx, client, role); err != nil {
			return fmt.Errorf("applying %s role %q: %w", cfg.Kind, role.Name, err)
		}
	}
	creatorSub := creatorSubject(obj)
	if creatorSub == "" {
		return nil
	}
	binding := RoleBinding(namespace, name, cfg, ShareTargetUser, creatorSub, RoleOwner, ownerRefs)
	if err := applyRoleBinding(ctx, client, binding); err != nil {
		return fmt.Errorf("applying %s owner role binding: %w", cfg.Kind, err)
	}
	return nil
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
	rules := []rbacv1.PolicyRule{{
		APIGroups:     []string{TemplatesAPIGroup},
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
	return []metav1.OwnerReference{{
		APIVersion:         templatesv1alpha1.GroupVersion.String(),
		Kind:               cfg.Kind,
		Name:               obj.GetName(),
		UID:                obj.GetUID(),
		Controller:         &controller,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}}
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
		for _, cfg := range AllKindConfigs() {
			if value := strings.TrimPrefix(labels[LabelResourceRole], cfg.RolePurpose+"-"); value != labels[LabelResourceRole] {
				return NormalizeRole(value)
			}
		}
	}
	return RoleViewer
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
