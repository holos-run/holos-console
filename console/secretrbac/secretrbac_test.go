package secretrbac

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
)

func TestRoleBindingUsesOIDCSubjectAndSafeLabels(t *testing.T) {
	rb := RoleBinding("holos-prj-demo", ShareTargetUser, "admin@localhost", RoleOwner, nil)

	if got, want := rb.Subjects[0].Kind, rbacv1.UserKind; got != want {
		t.Fatalf("subject kind = %q, want %q", got, want)
	}
	if got, want := rb.Subjects[0].Name, "oidc:admin@localhost"; got != want {
		t.Fatalf("subject name = %q, want %q", got, want)
	}
	if got := rb.Labels[LabelShareTargetName]; got != "admin_localhost" {
		t.Fatalf("share target label = %q, want sanitized label", got)
	}
	if got, want := rb.Annotations[AnnotationShareTargetName], "oidc:admin@localhost"; got != want {
		t.Fatalf("share target annotation = %q, want %q", got, want)
	}
	if got, want := rb.RoleRef.Name, RoleName(RoleOwner); got != want {
		t.Fatalf("role ref = %q, want %q", got, want)
	}
}

func TestRoleBindingUsesOIDCGroups(t *testing.T) {
	rb := RoleBinding("holos-prj-demo", ShareTargetGroup, "platform", RoleViewer, nil)

	if got, want := rb.Subjects[0].Kind, rbacv1.GroupKind; got != want {
		t.Fatalf("subject kind = %q, want %q", got, want)
	}
	if got, want := rb.Subjects[0].Name, "oidc:platform"; got != want {
		t.Fatalf("subject name = %q, want %q", got, want)
	}
}
