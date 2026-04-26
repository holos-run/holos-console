package rbacname

import (
	"regexp"
	"strings"
	"testing"
)

func TestRoleBindingNameIsDeterministicAndDNS1123(t *testing.T) {
	name := RoleBindingName("project-secrets-owner", "user", "oidc:admin@localhost")

	if got, wantPrefix := name, "project-secrets-owner-u-"; !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("RoleBindingName() = %q, want prefix %q", got, wantPrefix)
	}
	if !regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`).MatchString(name) {
		t.Fatalf("RoleBindingName() = %q, want DNS-1123 label", name)
	}
	if again := RoleBindingName("project-secrets-owner", "user", "oidc:admin@localhost"); again != name {
		t.Fatalf("RoleBindingName() is not deterministic: %q then %q", name, again)
	}
}
