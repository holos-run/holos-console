package resourcerbac

import (
	"context"
	"sort"
	"testing"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureResourceRBACProvisioningForEveryKind(t *testing.T) {
	for _, cfg := range AllKindConfigs() {
		t.Run(cfg.Kind, func(t *testing.T) {
			obj := cfg.NewObject()
			obj.SetNamespace("holos-prj-demo")
			obj.SetName("sample")
			obj.SetUID(types.UID("uid-" + cfg.Resource))
			obj.SetAnnotations(map[string]string{
				AnnotationCreatorSubject: "user-123",
			})
			client := fake.NewClientset()

			if err := EnsureResourceRBAC(context.Background(), client, obj, cfg); err != nil {
				t.Fatalf("EnsureResourceRBAC: %v", err)
			}

			roles, err := client.RbacV1().Roles("holos-prj-demo").List(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("list roles: %v", err)
			}
			if len(roles.Items) != 3 {
				t.Fatalf("roles = %d, want 3", len(roles.Items))
			}

			gotVerbs := make(map[string][]string)
			for _, role := range roles.Items {
				if len(role.OwnerReferences) != 1 {
					t.Fatalf("Role %q ownerRefs = %d, want 1", role.Name, len(role.OwnerReferences))
				}
				assertOwnerRef(t, role.OwnerReferences[0], cfg, "sample", types.UID("uid-"+cfg.Resource))
				if len(role.Rules) == 0 {
					t.Fatalf("Role %q has no rules", role.Name)
				}
				rule := role.Rules[0]
				if len(rule.APIGroups) != 1 || rule.APIGroups[0] != TemplatesAPIGroup {
					t.Fatalf("Role %q apiGroups = %v", role.Name, rule.APIGroups)
				}
				if len(rule.Resources) != 1 || rule.Resources[0] != cfg.Resource {
					t.Fatalf("Role %q resources = %v, want %s", role.Name, rule.Resources, cfg.Resource)
				}
				if len(rule.ResourceNames) != 1 || rule.ResourceNames[0] != "sample" {
					t.Fatalf("Role %q resourceNames = %v, want sample", role.Name, rule.ResourceNames)
				}
				gotVerbs[RoleFromLabels(role.Labels)] = append([]string(nil), rule.Verbs...)
			}
			assertStringSlice(t, gotVerbs[RoleViewer], []string{"get"})
			assertStringSlice(t, gotVerbs[RoleEditor], []string{"get", "update", "patch"})
			assertStringSlice(t, gotVerbs[RoleOwner], []string{"get", "update", "patch", "delete"})

			bindings, err := client.RbacV1().RoleBindings("holos-prj-demo").List(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("list rolebindings: %v", err)
			}
			if len(bindings.Items) != 1 {
				t.Fatalf("rolebindings = %d, want 1", len(bindings.Items))
			}
			rb := bindings.Items[0]
			if len(rb.OwnerReferences) != 1 {
				t.Fatalf("RoleBinding ownerRefs = %d, want 1", len(rb.OwnerReferences))
			}
			assertOwnerRef(t, rb.OwnerReferences[0], cfg, "sample", types.UID("uid-"+cfg.Resource))
			if rb.Subjects[0].Name != "oidc:user-123" {
				t.Fatalf("RoleBinding subject = %q, want oidc:user-123", rb.Subjects[0].Name)
			}
			if rb.RoleRef.Name != RoleName("sample", cfg, RoleOwner) {
				t.Fatalf("RoleBinding roleRef = %q, want owner role", rb.RoleRef.Name)
			}
		})
	}
}

func TestEnsureResourceRBACWithoutCreatorSubjectCreatesRolesOnly(t *testing.T) {
	obj := Templates.NewObject()
	obj.SetNamespace("holos-prj-demo")
	obj.SetName("sample")
	obj.SetUID("uid-template")
	client := fake.NewClientset()

	if err := EnsureResourceRBAC(context.Background(), client, obj, Templates); err != nil {
		t.Fatalf("EnsureResourceRBAC: %v", err)
	}
	bindings, err := client.RbacV1().RoleBindings("holos-prj-demo").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list rolebindings: %v", err)
	}
	if len(bindings.Items) != 0 {
		t.Fatalf("rolebindings = %d, want 0", len(bindings.Items))
	}
}

func TestResourceRoleOwnerIncludesSharingDelegation(t *testing.T) {
	roles := ResourceRoles("holos-prj-demo", "sample", TemplatePolicies, nil)
	var owner *metav1.ObjectMeta
	var ownerRules int
	for _, role := range roles {
		if RoleFromLabels(role.Labels) == RoleOwner {
			owner = &role.ObjectMeta
			ownerRules = len(role.Rules)
		}
	}
	if owner == nil {
		t.Fatal("owner role not found")
	}
	if ownerRules != 3 {
		t.Fatalf("owner role rules = %d, want resource rule plus sharing delegation rules", ownerRules)
	}
}

func assertOwnerRef(t *testing.T, got metav1.OwnerReference, cfg KindConfig, name string, uid types.UID) {
	t.Helper()
	if got.APIVersion != templatesv1alpha1.GroupVersion.String() {
		t.Fatalf("owner apiVersion = %q, want %q", got.APIVersion, templatesv1alpha1.GroupVersion.String())
	}
	if got.Kind != cfg.Kind {
		t.Fatalf("owner kind = %q, want %q", got.Kind, cfg.Kind)
	}
	if got.Name != name {
		t.Fatalf("owner name = %q, want %q", got.Name, name)
	}
	if got.UID != uid {
		t.Fatalf("owner uid = %q, want %q", got.UID, uid)
	}
	if got.Controller == nil || !*got.Controller {
		t.Fatalf("owner controller = %v, want true", got.Controller)
	}
	if got.BlockOwnerDeletion == nil || !*got.BlockOwnerDeletion {
		t.Fatalf("owner blockOwnerDeletion = %v, want true", got.BlockOwnerDeletion)
	}
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("verbs = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("verbs = %v, want %v", got, want)
		}
	}
}
