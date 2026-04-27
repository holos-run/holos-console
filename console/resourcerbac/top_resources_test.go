package resourcerbac

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/secrets"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureTopResourceRBACProvisioningForEveryKind(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cfg          KindConfig
		resourceType string
		namespace    string
	}{
		{name: "Organization", cfg: Organizations, resourceType: v1alpha2.ResourceTypeOrganization, namespace: "holos-org-platform"},
		{name: "Folder", cfg: Folders, resourceType: v1alpha2.ResourceTypeFolder, namespace: "holos-fld-default"},
		{name: "Project", cfg: Projects, resourceType: v1alpha2.ResourceTypeProject, namespace: "holos-prj-demo"},
		{name: "ResourceFolder", cfg: Resources, resourceType: v1alpha2.ResourceTypeFolder, namespace: "holos-fld-default"},
		{name: "ResourceProject", cfg: Resources, resourceType: v1alpha2.ResourceTypeProject, namespace: "holos-prj-demo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := managedNamespace(t, tc.namespace, tc.resourceType, nil, nil)
			client := fake.NewClientset()

			if err := EnsureResourceRBAC(context.Background(), client, obj, tc.cfg); err != nil {
				t.Fatalf("EnsureResourceRBAC: %v", err)
			}

			roles, err := client.RbacV1().ClusterRoles().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("list roles: %v", err)
			}
			if len(roles.Items) != 3 {
				t.Fatalf("roles = %d, want 3", len(roles.Items))
			}
			gotVerbs := make(map[string][]string)
			var ownerRules []rbacv1.PolicyRule
			for _, role := range roles.Items {
				if len(role.OwnerReferences) != 1 {
					t.Fatalf("Role %q ownerRefs = %d, want 1", role.Name, len(role.OwnerReferences))
				}
				assertNamespaceOwnerRef(t, role.OwnerReferences[0], tc.namespace, types.UID("uid-"+tc.namespace))
				if len(role.Rules) == 0 {
					t.Fatalf("Role %q has no rules", role.Name)
				}
				rule := role.Rules[0]
				assertStringSlice(t, rule.APIGroups, []string{""})
				assertStringSlice(t, rule.Resources, []string{"namespaces"})
				assertStringSlice(t, rule.ResourceNames, []string{tc.namespace})
				gotVerbs[RoleFromLabels(role.Labels)] = append([]string(nil), rule.Verbs...)
				if RoleFromLabels(role.Labels) == RoleOwner {
					ownerRules = role.Rules
				}
			}
			assertStringSlice(t, gotVerbs[RoleViewer], []string{"get"})
			assertStringSlice(t, gotVerbs[RoleEditor], []string{"get", "update", "patch"})
			assertStringSlice(t, gotVerbs[RoleOwner], []string{"get", "update", "patch", "delete"})
			assertNoClusterOwnerDelegationRules(t, ownerRules)
		})
	}
}

func TestEnsureTopResourceRBACDevPersonaBindings(t *testing.T) {
	obj := managedNamespace(t, "holos-prj-demo", v1alpha2.ResourceTypeProject,
		[]secrets.AnnotationGrant{
			{Principal: "platform@localhost", Role: RoleOwner},
			{Principal: "product@localhost", Role: RoleEditor},
			{Principal: "sre@localhost", Role: RoleViewer},
		},
		[]secrets.AnnotationGrant{
			{Principal: "owner", Role: RoleOwner},
			{Principal: "editor", Role: RoleEditor},
			{Principal: "viewer", Role: RoleViewer},
		},
	)
	client := fake.NewClientset()

	if err := EnsureResourceRBAC(context.Background(), client, obj, Projects); err != nil {
		t.Fatalf("EnsureResourceRBAC: %v", err)
	}

	bindings, err := client.RbacV1().ClusterRoleBindings().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list rolebindings: %v", err)
	}
	assertClusterBinding(t, bindings.Items, "oidc:platform@localhost", rbacv1.UserKind, RoleName("holos-prj-demo", Projects, RoleOwner))
	assertClusterBinding(t, bindings.Items, "oidc:product@localhost", rbacv1.UserKind, RoleName("holos-prj-demo", Projects, RoleEditor))
	assertClusterBinding(t, bindings.Items, "oidc:sre@localhost", rbacv1.UserKind, RoleName("holos-prj-demo", Projects, RoleViewer))
	assertClusterBinding(t, bindings.Items, "oidc:owner", rbacv1.GroupKind, RoleName("holos-prj-demo", Projects, RoleOwner))
	assertClusterBinding(t, bindings.Items, "oidc:editor", rbacv1.GroupKind, RoleName("holos-prj-demo", Projects, RoleEditor))
	assertClusterBinding(t, bindings.Items, "oidc:viewer", rbacv1.GroupKind, RoleName("holos-prj-demo", Projects, RoleViewer))
}

func TestEnsureTopResourceRBACFiltersInactiveGrants(t *testing.T) {
	now := time.Now().Unix()
	expired := now - 10
	future := now + 60
	obj := managedNamespace(t, "holos-prj-demo", v1alpha2.ResourceTypeProject,
		[]secrets.AnnotationGrant{
			{Principal: "active@localhost", Role: RoleOwner},
			{Principal: "expired@localhost", Role: RoleOwner, Exp: &expired},
			{Principal: "future@localhost", Role: RoleOwner, Nbf: &future},
		},
		nil,
	)
	client := fake.NewClientset()

	if err := EnsureResourceRBAC(context.Background(), client, obj, Projects); err != nil {
		t.Fatalf("EnsureResourceRBAC: %v", err)
	}

	bindings, err := client.RbacV1().ClusterRoleBindings().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list clusterrolebindings: %v", err)
	}
	assertClusterBinding(t, bindings.Items, "oidc:active@localhost", rbacv1.UserKind, RoleName("holos-prj-demo", Projects, RoleOwner))
	assertNoClusterBinding(t, bindings.Items, "oidc:expired@localhost")
	assertNoClusterBinding(t, bindings.Items, "oidc:future@localhost")
	if got := NextGrantRequeueAfter(obj, time.Unix(now, 0)); got <= 0 {
		t.Fatalf("NextGrantRequeueAfter = %v, want positive duration for future grant", got)
	}
}

func TestEnsureTopResourceRBACSkipsMismatchedNamespace(t *testing.T) {
	obj := managedNamespace(t, "holos-prj-demo", v1alpha2.ResourceTypeProject, nil, nil)
	client := fake.NewClientset()

	if err := EnsureResourceRBAC(context.Background(), client, obj, Organizations); err != nil {
		t.Fatalf("EnsureResourceRBAC: %v", err)
	}
	roles, err := client.RbacV1().ClusterRoles().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	if len(roles.Items) != 0 {
		t.Fatalf("roles = %d, want 0 for mismatched namespace resource type", len(roles.Items))
	}
}

func managedNamespace(t *testing.T, name, resourceType string, users, groups []secrets.AnnotationGrant) *corev1.Namespace {
	t.Helper()
	annotations := map[string]string{}
	if users != nil {
		raw, err := json.Marshal(users)
		if err != nil {
			t.Fatalf("marshal users: %v", err)
		}
		annotations[v1alpha2.AnnotationShareUsers] = string(raw)
	}
	if groups != nil {
		raw, err := json.Marshal(groups)
		if err != nil {
			t.Fatalf("marshal groups: %v", err)
		}
		annotations[v1alpha2.AnnotationShareRoles] = string(raw)
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID("uid-" + name),
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: resourceType,
			},
			Annotations: annotations,
		},
	}
}

func assertNamespaceOwnerRef(t *testing.T, got metav1.OwnerReference, name string, uid types.UID) {
	t.Helper()
	if got.APIVersion != "v1" {
		t.Fatalf("owner apiVersion = %q, want v1", got.APIVersion)
	}
	if got.Kind != "Namespace" {
		t.Fatalf("owner kind = %q, want Namespace", got.Kind)
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
	if got.BlockOwnerDeletion == nil || *got.BlockOwnerDeletion {
		t.Fatalf("owner blockOwnerDeletion = %v, want false", got.BlockOwnerDeletion)
	}
}

func assertClusterBinding(t *testing.T, bindings []rbacv1.ClusterRoleBinding, subjectName, subjectKind, roleRef string) {
	t.Helper()
	for _, binding := range bindings {
		if binding.RoleRef.Name != roleRef || len(binding.Subjects) != 1 {
			continue
		}
		subject := binding.Subjects[0]
		if subject.Name == subjectName && subject.Kind == subjectKind {
			return
		}
	}
	t.Fatalf("missing binding subject=%s kind=%s roleRef=%s in %#v", subjectName, subjectKind, roleRef, bindings)
}

func assertNoClusterBinding(t *testing.T, bindings []rbacv1.ClusterRoleBinding, subjectName string) {
	t.Helper()
	for _, binding := range bindings {
		if len(binding.Subjects) == 1 && binding.Subjects[0].Name == subjectName {
			t.Fatalf("unexpected binding for subject=%s: %#v", subjectName, binding)
		}
	}
}

func assertNoClusterOwnerDelegationRules(t *testing.T, rules []rbacv1.PolicyRule) {
	t.Helper()
	for _, rule := range rules {
		for _, resource := range rule.Resources {
			if resource == "clusterroles" || resource == "clusterrolebindings" {
				t.Fatalf("owner rules grant cluster-scoped RBAC mutation: %#v", rules)
			}
		}
	}
}
