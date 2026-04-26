package deployments

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

// TestNormalizeRole verifies the role-tier normalization defaults unknown
// values to least-privilege (viewer) per ADR 036.
func TestNormalizeRole(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"viewer", RoleViewer},
		{"VIEWER", RoleViewer},
		{" Editor ", RoleEditor},
		{"editor", RoleEditor},
		{"owner", RoleOwner},
		{"", RoleViewer},
		{"banana", RoleViewer},
	}
	for _, c := range cases {
		if got := NormalizeRole(c.in); got != c.want {
			t.Errorf("NormalizeRole(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizeTarget verifies user/group target normalization.
func TestNormalizeTarget(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"user", ShareTargetUser},
		{"USER", ShareTargetUser},
		{"group", ShareTargetGroup},
		{"Group", ShareTargetGroup},
		{"", ShareTargetUser},
		{"banana", ShareTargetUser},
	}
	for _, c := range cases {
		if got := NormalizeTarget(c.in); got != c.want {
			t.Errorf("NormalizeTarget(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

// TestOIDCPrincipal verifies OIDC prefixing is idempotent.
func TestOIDCPrincipal(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"alice@example.com", "oidc:alice@example.com"},
		{"oidc:alice@example.com", "oidc:alice@example.com"},
		{"", ""},
		{"  ", ""},
	}
	for _, c := range cases {
		if got := OIDCPrincipal(c.in); got != c.want {
			t.Errorf("OIDCPrincipal(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

// TestUnprefixedPrincipal verifies OIDC prefix is stripped only when present.
func TestUnprefixedPrincipal(t *testing.T) {
	if got := UnprefixedPrincipal("oidc:alice@example.com"); got != "alice@example.com" {
		t.Errorf("got %q", got)
	}
	if got := UnprefixedPrincipal("alice@example.com"); got != "alice@example.com" {
		t.Errorf("got %q", got)
	}
}

// TestDeploymentRoles verifies that the three role tiers are produced with
// the correct verbs, resourceNames scoping, and ownerReferences. AC #1.
func TestDeploymentRoles(t *testing.T) {
	owner := metav1.OwnerReference{
		APIVersion: "deployments.holos.run/v1alpha1",
		Kind:       "Deployment",
		Name:       "web-app",
		UID:        "uid-123",
	}
	roles := DeploymentRoles("prj-acme", "web-app", []metav1.OwnerReference{owner})
	if len(roles) != 3 {
		t.Fatalf("expected 3 roles, got %d", len(roles))
	}
	wantVerbs := map[string][]string{
		RoleViewer: {"get"},
		RoleEditor: {"get", "update", "patch"},
		RoleOwner:  {"get", "update", "patch", "delete"},
	}
	for _, role := range roles {
		if role.Namespace != "prj-acme" {
			t.Errorf("Role %q namespace=%q want prj-acme", role.Name, role.Namespace)
		}
		if len(role.OwnerReferences) != 1 || role.OwnerReferences[0].UID != "uid-123" {
			t.Errorf("Role %q ownerRefs=%v", role.Name, role.OwnerReferences)
		}
		if role.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
			t.Errorf("Role %q missing managed-by label", role.Name)
		}
		if role.Labels[LabelDeploymentName] != "web-app" {
			t.Errorf("Role %q deployment label=%q", role.Name, role.Labels[LabelDeploymentName])
		}
		if len(role.Rules) != 1 {
			t.Fatalf("Role %q rules=%d", role.Name, len(role.Rules))
		}
		rule := role.Rules[0]
		if len(rule.APIGroups) != 1 || rule.APIGroups[0] != DeploymentAPIGroup {
			t.Errorf("Role %q apiGroups=%v", role.Name, rule.APIGroups)
		}
		if len(rule.Resources) != 1 || rule.Resources[0] != DeploymentResource {
			t.Errorf("Role %q resources=%v", role.Name, rule.Resources)
		}
		if len(rule.ResourceNames) != 1 || rule.ResourceNames[0] != "web-app" {
			t.Errorf("Role %q resourceNames=%v", role.Name, rule.ResourceNames)
		}
		// Identify the tier from the role name suffix.
		tier := RoleFromLabels(role.Labels)
		want, ok := wantVerbs[tier]
		if !ok {
			t.Fatalf("Role %q unknown tier", role.Name)
		}
		if !equalStrings(rule.Verbs, want) {
			t.Errorf("Role %q verbs=%v want %v", role.Name, rule.Verbs, want)
		}
	}
}

// TestRoleBinding_UserKind verifies that user share targets produce
// RoleBindings with subject Kind=User and the OIDC-prefixed name.
func TestRoleBinding_UserKind(t *testing.T) {
	rb := RoleBinding("prj-acme", "web-app", ShareTargetUser, "alice@example.com", RoleEditor, nil)
	if rb.Namespace != "prj-acme" {
		t.Errorf("namespace=%q", rb.Namespace)
	}
	if len(rb.Subjects) != 1 {
		t.Fatalf("subjects=%d", len(rb.Subjects))
	}
	subj := rb.Subjects[0]
	if subj.Kind != rbacv1.UserKind {
		t.Errorf("kind=%q want User", subj.Kind)
	}
	if subj.Name != "oidc:alice@example.com" {
		t.Errorf("name=%q", subj.Name)
	}
	if subj.APIGroup != rbacv1.GroupName {
		t.Errorf("apigroup=%q", subj.APIGroup)
	}
	if rb.RoleRef.Name != RoleName("web-app", RoleEditor) {
		t.Errorf("roleref=%q", rb.RoleRef.Name)
	}
	if rb.Annotations[AnnotationShareTargetName] != "oidc:alice@example.com" {
		t.Errorf("annotation=%q", rb.Annotations[AnnotationShareTargetName])
	}
}

// TestRoleBinding_GroupKind verifies that group share targets produce
// RoleBindings with subject Kind=Group.
func TestRoleBinding_GroupKind(t *testing.T) {
	rb := RoleBinding("prj-acme", "web-app", ShareTargetGroup, "platform-admins", RoleOwner, nil)
	if len(rb.Subjects) != 1 {
		t.Fatalf("subjects=%d", len(rb.Subjects))
	}
	if rb.Subjects[0].Kind != rbacv1.GroupKind {
		t.Errorf("kind=%q want Group", rb.Subjects[0].Kind)
	}
}

// TestRoleBinding_OwnerRefsApplied verifies ownerRefs are stamped onto the
// RoleBinding so K8s GC cascades cleanup on Deployment delete (AC #3).
func TestRoleBinding_OwnerRefsApplied(t *testing.T) {
	owner := metav1.OwnerReference{
		APIVersion: "deployments.holos.run/v1alpha1",
		Kind:       "Deployment",
		Name:       "web-app",
		UID:        "uid-987",
	}
	rb := RoleBinding("prj-acme", "web-app", ShareTargetUser, "alice@example.com", RoleViewer, []metav1.OwnerReference{owner})
	if len(rb.OwnerReferences) != 1 || rb.OwnerReferences[0].UID != "uid-987" {
		t.Errorf("ownerRefs=%v", rb.OwnerReferences)
	}
}

// TestRoleBindingName_Deterministic verifies the same inputs always produce
// the same name (deterministic SHA suffix) and different inputs differ.
func TestRoleBindingName_Deterministic(t *testing.T) {
	a := RoleBindingName("web-app", RoleEditor, ShareTargetUser, "alice@example.com")
	b := RoleBindingName("web-app", RoleEditor, ShareTargetUser, "alice@example.com")
	if a != b {
		t.Errorf("names not deterministic: %q vs %q", a, b)
	}
	c := RoleBindingName("web-app", RoleOwner, ShareTargetUser, "alice@example.com")
	if a == c {
		t.Errorf("different roles produced same name: %q", a)
	}
	d := RoleBindingName("api-app", RoleEditor, ShareTargetUser, "alice@example.com")
	if a == d {
		t.Errorf("different deployments produced same name: %q", a)
	}
	e := RoleBindingName("web-app", RoleEditor, ShareTargetGroup, "alice@example.com")
	if a == e {
		t.Errorf("user/group same name: %q", a)
	}
}

// TestRoleFromLabels verifies tier extraction from labels and that missing
// labels default to viewer (least privilege).
func TestRoleFromLabels(t *testing.T) {
	if got := RoleFromLabels(RoleLabels("web-app", RoleOwner)); got != RoleOwner {
		t.Errorf("got %q want owner", got)
	}
	if got := RoleFromLabels(RoleLabels("web-app", RoleEditor)); got != RoleEditor {
		t.Errorf("got %q want editor", got)
	}
	if got := RoleFromLabels(nil); got != RoleViewer {
		t.Errorf("got %q want viewer (default)", got)
	}
	if got := RoleFromLabels(map[string]string{LabelDeploymentRole: "junk"}); got != RoleViewer {
		t.Errorf("got %q want viewer", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
