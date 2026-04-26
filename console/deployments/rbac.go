// Package deployments — Per-Deployment RBAC helpers.
//
// Following ADR 036, deployments.holos.run/v1alpha1.Deployment access is
// represented as native Kubernetes RBAC: each Deployment CR owns a tier of
// Roles scoped to the named resource via `resourceNames`, plus one RoleBinding
// per resolved sharing entry. The Roles and RoleBindings carry an
// OwnerReference on the Deployment so K8s garbage collection cascades the
// cleanup when the Deployment is deleted (HOL-1033).
//
// `resourceNames` does not apply to `list`/`watch` verbs. List access is
// granted at the project-namespace tier (separate from this per-deployment
// Role) — see HOL-1032 / `console/secretrbac` for the project-secrets
// precedent. Per-deployment Roles only carry verbs that work with
// `resourceNames`: get/update/patch/delete (and the SSA verbs needed to apply
// rendered owned resources).
package deployments

import (
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rbacname"
)

const (
	// DeploymentAPIGroup is the API group of the Deployment CRD whose access
	// these Roles gate.
	DeploymentAPIGroup = "deployments.holos.run"
	// DeploymentResource is the plural resource name for the Deployment CRD.
	DeploymentResource = "deployments"

	// RolePurposeDeployment is the canonical role-purpose label value used
	// to select RoleBindings the deployments handler manages, distinct from
	// the project-secrets purpose (HOL-1032).
	RolePurposeDeployment = "deployment"

	// LabelRolePurpose marks Role/RoleBinding objects with the purpose tag
	// the handler uses to scope its reconcile operations.
	LabelRolePurpose = "console.holos.run/role-purpose"
	// LabelDeploymentName records the deployment a per-deployment Role or
	// RoleBinding scopes access to. Used to filter sharing reconciliation
	// to a single deployment without listing every RoleBinding in the
	// project namespace.
	LabelDeploymentName = "console.holos.run/deployment"
	// LabelShareTarget marks whether a RoleBinding represents a user or a
	// group share (mirrors secretrbac).
	LabelShareTarget = "console.holos.run/share-target"
	// LabelShareTargetName encodes a DNS-safe form of the principal name on
	// the RoleBinding so it can be looked up by label selector.
	LabelShareTargetName = "console.holos.run/share-target-name"
	// LabelDeploymentRole records which role tier a RoleBinding is part of
	// (viewer / editor / owner) so handlers can read it back without
	// resolving the RoleRef name to a tier separately.
	LabelDeploymentRole = "holos.run/role"

	// AnnotationShareTargetName carries the original (un-sanitized,
	// OIDC-prefixed) principal name on the RoleBinding so it can be
	// recovered without un-sanitizing the label form.
	AnnotationShareTargetName = LabelShareTargetName

	// Role tier names. Mirror the secretrbac tiers so the wire `Role` enum
	// maps to the same set of names regardless of the resource family.
	RoleViewer = "viewer"
	RoleEditor = "editor"
	RoleOwner  = "owner"

	// Sharing target kinds.
	ShareTargetUser  = "user"
	ShareTargetGroup = "group"

	// OIDCPrefix is the principal-namespace prefix used by the impersonated
	// client (mirrors secretrbac.OIDCPrefix; we redefine it locally to
	// avoid an import cycle).
	OIDCPrefix = "oidc:"
)

// roleNames maps a role tier to the canonical Role object name for a
// deployment. The names embed the deployment so a single project namespace
// can hold per-deployment Roles for many deployments at once. The role
// tier is the suffix so the deterministic RoleBindingName helper can key
// off it without re-parsing the deployment name.
func roleObjectName(deployment, role string) string {
	return "holos-deployment-" + deployment + "-" + NormalizeRole(role)
}

// DeploymentRoles returns the managed Roles for the named Deployment in the
// given namespace. Each Role lists `resourceNames: [<deployment>]` so the
// principal it grants is bound to that single CR. ownerRefs should reference
// the Deployment CR so K8s GC cleans up the Roles when the deployment is
// deleted (AC #3).
func DeploymentRoles(namespace, deployment string, ownerRefs []metav1.OwnerReference) []*rbacv1.Role {
	return []*rbacv1.Role{
		deploymentRole(namespace, deployment, RoleViewer, viewerVerbs(), ownerRefs),
		deploymentRole(namespace, deployment, RoleEditor, editorVerbs(), ownerRefs),
		deploymentRole(namespace, deployment, RoleOwner, ownerVerbs(), ownerRefs),
	}
}

func deploymentRole(namespace, deployment, role string, verbs []string, ownerRefs []metav1.OwnerReference) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            roleObjectName(deployment, role),
			Namespace:       namespace,
			Labels:          RoleLabels(deployment, role),
			OwnerReferences: ownerRefs,
		},
		Rules: []rbacv1.PolicyRule{{
			APIGroups:     []string{DeploymentAPIGroup},
			Resources:     []string{DeploymentResource},
			ResourceNames: []string{deployment},
			Verbs:         verbs,
		}},
	}
}

// viewerVerbs returns the verbs granted by the per-deployment Viewer Role.
// `list`/`watch` cannot be combined with `resourceNames`, so they are not
// included here — the project-tier Role (kept by an out-of-band cluster
// operator or the cleanup phase HOL-1036) grants `list`/`watch` for the
// resource group at the namespace tier so a Viewer can see deployments in
// their projects.
func viewerVerbs() []string {
	return []string{"get"}
}

// editorVerbs returns the verbs the per-deployment Editor Role grants.
// Editors can mutate the CR but not change its sharing — the Owner tier
// owns rolebindings.
func editorVerbs() []string {
	return []string{"get", "update", "patch"}
}

// ownerVerbs returns the verbs the per-deployment Owner Role grants. Owner
// is a superset that also delegates RoleBinding management for sharing.
func ownerVerbs() []string {
	return []string{"get", "update", "patch", "delete"}
}

// RoleName returns the Role object name for the given deployment and tier.
// Used by RoleBinding to populate RoleRef and by reconcile helpers to
// list/delete Roles bound to a deployment.
func RoleName(deployment, role string) string {
	return roleObjectName(deployment, role)
}

// RoleLabels returns the labels stamped on a per-deployment Role so the
// handler can find every Role scoped to a single deployment via a label
// selector without parsing names.
func RoleLabels(deployment, role string) map[string]string {
	return map[string]string{
		v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
		LabelRolePurpose:        RolePurposeDeployment,
		LabelDeploymentName:     deployment,
		LabelDeploymentRole:     "deployment-" + NormalizeRole(role),
	}
}

// RoleBindingLabels returns the labels stamped on a per-deployment
// RoleBinding so the handler can scope reconcile operations.
func RoleBindingLabels(deployment, target, principal, role string) map[string]string {
	labels := RoleLabels(deployment, role)
	labels[LabelShareTarget] = target
	labels[LabelShareTargetName] = labelValue(principal)
	return labels
}

// RoleBinding returns a RoleBinding that grants the given principal access to
// the named Deployment at the requested tier. ownerRefs should reference the
// Deployment CR so cluster GC removes the RoleBinding alongside the
// Deployment.
func RoleBinding(namespace, deployment, target, principal, role string, ownerRefs []metav1.OwnerReference) *rbacv1.RoleBinding {
	target = NormalizeTarget(target)
	role = NormalizeRole(role)
	subjectKind := rbacv1.UserKind
	if target == ShareTargetGroup {
		subjectKind = rbacv1.GroupKind
	}
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            RoleBindingName(deployment, role, target, principal),
			Namespace:       namespace,
			Labels:          RoleBindingLabels(deployment, target, principal, role),
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
			Name:     RoleName(deployment, role),
		},
	}
}

// RoleBindingName returns the deterministic RoleBinding name described in
// ADR 036, scoped per deployment + role tier + share target so multiple
// deployments in the same namespace cannot collide.
func RoleBindingName(deployment, role, target, principal string) string {
	rolePurpose := RolePurposeDeployment + "-" + deployment + "-" + NormalizeRole(role)
	return rbacname.RoleBindingName(rolePurpose, target, OIDCPrincipal(principal))
}

// OIDCPrincipal returns the principal string in the OIDC-prefixed form used
// by the impersonated client for subject names.
func OIDCPrincipal(principal string) string {
	principal = strings.TrimSpace(principal)
	if principal == "" || strings.HasPrefix(principal, OIDCPrefix) {
		return principal
	}
	return OIDCPrefix + principal
}

// UnprefixedPrincipal removes the OIDC prefix when present so callers that
// surface a principal back to the user see the un-prefixed form.
func UnprefixedPrincipal(principal string) string {
	return strings.TrimPrefix(principal, OIDCPrefix)
}

// NormalizeRole maps an arbitrary role string onto one of the canonical
// tier names. Unknown values default to viewer (least privilege).
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

// NormalizeTarget maps a target string onto either user or group, defaulting
// to user when the value is unrecognized.
func NormalizeTarget(target string) string {
	if strings.EqualFold(strings.TrimSpace(target), ShareTargetGroup) {
		return ShareTargetGroup
	}
	return ShareTargetUser
}

// RoleFromLabels extracts the tier name from RoleBinding/Role labels.
// Returns Viewer when the label is absent or unrecognized so the caller
// surfaces the lowest privilege tier on degraded data.
func RoleFromLabels(labels map[string]string) string {
	if labels != nil {
		if value := strings.TrimPrefix(labels[LabelDeploymentRole], "deployment-"); value != "" {
			return NormalizeRole(value)
		}
	}
	return RoleViewer
}

// labelValue strips characters that are not valid in Kubernetes label
// values from `value`. Mirrors secretrbac.labelValue. Used to produce a
// DNS-safe form of a principal name for label-based selection.
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
