package secretrbac

import (
	"strings"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rbacname"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RolePurposeProjectSecrets = "project-secrets"

	LabelRolePurpose     = "console.holos.run/role-purpose"
	LabelShareTarget     = "console.holos.run/share-target"
	LabelShareTargetName = "console.holos.run/share-target-name"
	LabelSecretRole      = "holos.run/role"

	AnnotationShareTargetName = LabelShareTargetName

	RoleViewer = "viewer"
	RoleEditor = "editor"
	RoleOwner  = "owner"

	ShareTargetUser  = "user"
	ShareTargetGroup = "group"

	OIDCPrefix = "oidc:"
)

var roleNames = map[string]string{
	RoleViewer: "holos-project-secrets-viewer",
	RoleEditor: "holos-project-secrets-editor",
	RoleOwner:  "holos-project-secrets-owner",
}

// ProjectSecretRoles returns the managed Roles for project-scoped Secret RBAC.
func ProjectSecretRoles(namespace string, ownerRefs []metav1.OwnerReference) []*rbacv1.Role {
	roles := []*rbacv1.Role{
		projectSecretRole(namespace, RoleViewer, []string{"get", "list", "watch"}, nil, ownerRefs),
		projectSecretRole(namespace, RoleEditor, []string{"get", "list", "watch", "create", "update", "patch"}, nil, ownerRefs),
		projectSecretRole(namespace, RoleOwner, []string{"*"}, ownerRules(), ownerRefs),
	}
	return roles
}

func projectSecretRole(namespace, role string, secretVerbs []string, extraRules []rbacv1.PolicyRule, ownerRefs []metav1.OwnerReference) *rbacv1.Role {
	rules := []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"secrets"},
		Verbs:     secretVerbs,
	}}
	rules = append(rules, extraRules...)
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            RoleName(role),
			Namespace:       namespace,
			Labels:          RoleLabels(role),
			OwnerReferences: ownerRefs,
		},
		Rules: rules,
	}
}

func ownerRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"rbac.authorization.k8s.io"},
			Resources: []string{"rolebindings"},
			Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
		},
		{
			APIGroups:     []string{"rbac.authorization.k8s.io"},
			Resources:     []string{"roles"},
			ResourceNames: []string{RoleName(RoleViewer), RoleName(RoleEditor), RoleName(RoleOwner)},
			Verbs:         []string{"get", "list", "watch", "bind"},
		},
	}
}

func RoleName(role string) string {
	if name, ok := roleNames[NormalizeRole(role)]; ok {
		return name
	}
	return roleNames[RoleViewer]
}

func RoleLabels(role string) map[string]string {
	role = NormalizeRole(role)
	return map[string]string{
		v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
		LabelRolePurpose:        RolePurposeProjectSecrets,
		LabelSecretRole:         "secrets-" + role,
	}
}

func RoleBindingLabels(target, principal, role string) map[string]string {
	labels := RoleLabels(role)
	labels[LabelShareTarget] = target
	labels[LabelShareTargetName] = labelValue(principal)
	return labels
}

func RoleBinding(namespace, target, principal, role string, ownerRefs []metav1.OwnerReference) *rbacv1.RoleBinding {
	target = NormalizeTarget(target)
	role = NormalizeRole(role)
	subjectKind := rbacv1.UserKind
	if target == ShareTargetGroup {
		subjectKind = rbacv1.GroupKind
	}
	roleName := RoleName(role)
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            RoleBindingName(role, target, principal),
			Namespace:       namespace,
			Labels:          RoleBindingLabels(target, principal, role),
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
			Name:     roleName,
		},
	}
}

func RoleBindingName(role, target, principal string) string {
	rolePurpose := RolePurposeProjectSecrets + "-" + NormalizeRole(role)
	return rbacname.RoleBindingName(rolePurpose, target, OIDCPrincipal(principal))
}

func OIDCPrincipal(principal string) string {
	principal = strings.TrimSpace(principal)
	if principal == "" || strings.HasPrefix(principal, OIDCPrefix) {
		return principal
	}
	return OIDCPrefix + principal
}

func UnprefixedPrincipal(principal string) string {
	return strings.TrimPrefix(principal, OIDCPrefix)
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

func RoleFromLabels(labels map[string]string, roleRefName string) string {
	if labels != nil {
		if value := strings.TrimPrefix(labels[LabelSecretRole], "secrets-"); value != "" {
			return NormalizeRole(value)
		}
	}
	for role, name := range roleNames {
		if name == roleRefName {
			return role
		}
	}
	return RoleViewer
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
