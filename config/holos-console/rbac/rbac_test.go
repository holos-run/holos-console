/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rbac_test

import (
	"os"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

const (
	consoleServiceAccountName      = "holos-console"
	consoleServiceAccountNamespace = "holos-system"
	consoleClusterRoleName         = "holos-console-templates"
)

func TestRoleGrantsImpersonationAndRBACReconciliation(t *testing.T) {
	var role rbacv1.ClusterRole
	mustReadYAML(t, "role.yaml", &role)

	if role.Name != consoleClusterRoleName {
		t.Fatalf("ClusterRole name = %q, want %q", role.Name, consoleClusterRoleName)
	}

	wantImpersonation := rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"groups", "serviceaccounts", "users"},
		Verbs:     []string{"impersonate"},
	}
	if !hasRule(role.Rules, wantImpersonation) {
		t.Fatalf("ClusterRole missing impersonation rule %+v in rules: %+v", wantImpersonation, role.Rules)
	}

	for _, forbidden := range []string{"userextras/email", "userextras/scopes"} {
		if hasResource(role.Rules, "", forbidden) {
			t.Fatalf("ClusterRole grants %q impersonation, but ADR 036 does not forward extra info", forbidden)
		}
	}

	wantRoles := rbacv1.PolicyRule{
		APIGroups: []string{"rbac.authorization.k8s.io"},
		Resources: []string{"roles"},
		Verbs:     []string{"bind", "create", "delete", "escalate", "get", "list", "patch", "update", "watch"},
	}
	if !hasRule(role.Rules, wantRoles) {
		t.Fatalf("ClusterRole missing Role reconciliation rule %+v in rules: %+v", wantRoles, role.Rules)
	}

	wantRoleBindings := rbacv1.PolicyRule{
		APIGroups: []string{"rbac.authorization.k8s.io"},
		Resources: []string{"rolebindings"},
		Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
	}
	if !hasRule(role.Rules, wantRoleBindings) {
		t.Fatalf("ClusterRole missing RoleBinding reconciliation rule %+v in rules: %+v", wantRoleBindings, role.Rules)
	}

	wantClusterRoles := rbacv1.PolicyRule{
		APIGroups: []string{"rbac.authorization.k8s.io"},
		Resources: []string{"clusterroles"},
		Verbs:     []string{"bind", "create", "delete", "escalate", "get", "list", "patch", "update", "watch"},
	}
	if !hasRule(role.Rules, wantClusterRoles) {
		t.Fatalf("ClusterRole missing ClusterRole reconciliation rule %+v in rules: %+v", wantClusterRoles, role.Rules)
	}

	wantClusterRoleBindings := rbacv1.PolicyRule{
		APIGroups: []string{"rbac.authorization.k8s.io"},
		Resources: []string{"clusterrolebindings"},
		Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
	}
	if !hasRule(role.Rules, wantClusterRoleBindings) {
		t.Fatalf("ClusterRole missing ClusterRoleBinding reconciliation rule %+v in rules: %+v", wantClusterRoleBindings, role.Rules)
	}
}

func TestClusterRoleBindingTargetsConsoleServiceAccount(t *testing.T) {
	var serviceAccount corev1.ServiceAccount
	mustReadYAML(t, "namespace/service_account.yaml", &serviceAccount)

	if serviceAccount.Name != consoleServiceAccountName {
		t.Fatalf("ServiceAccount name = %q, want %q", serviceAccount.Name, consoleServiceAccountName)
	}
	if serviceAccount.Namespace != consoleServiceAccountNamespace {
		t.Fatalf("ServiceAccount namespace = %q, want %q", serviceAccount.Namespace, consoleServiceAccountNamespace)
	}

	var binding rbacv1.ClusterRoleBinding
	mustReadYAML(t, "role_binding.yaml", &binding)

	wantRef := rbacv1.RoleRef{
		APIGroup: rbacv1.GroupName,
		Kind:     "ClusterRole",
		Name:     consoleClusterRoleName,
	}
	if binding.RoleRef != wantRef {
		t.Fatalf("roleRef = %+v, want %+v", binding.RoleRef, wantRef)
	}

	wantSubjects := []rbacv1.Subject{{
		Kind:      rbacv1.ServiceAccountKind,
		Name:      consoleServiceAccountName,
		Namespace: consoleServiceAccountNamespace,
	}}
	if !reflect.DeepEqual(binding.Subjects, wantSubjects) {
		t.Fatalf("subjects = %+v, want %+v", binding.Subjects, wantSubjects)
	}
}

func mustReadYAML(t *testing.T, path string, out any) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
}

func hasRule(rules []rbacv1.PolicyRule, want rbacv1.PolicyRule) bool {
	for _, rule := range rules {
		if reflect.DeepEqual(rule.APIGroups, want.APIGroups) &&
			containsAll(rule.Resources, want.Resources) &&
			reflect.DeepEqual(rule.ResourceNames, want.ResourceNames) &&
			reflect.DeepEqual(rule.Verbs, want.Verbs) {
			return true
		}
	}
	return false
}

func containsAll(got, want []string) bool {
	seen := make(map[string]struct{}, len(got))
	for _, value := range got {
		seen[value] = struct{}{}
	}
	for _, value := range want {
		if _, ok := seen[value]; !ok {
			return false
		}
	}
	return true
}

func hasResource(rules []rbacv1.PolicyRule, apiGroup, resource string) bool {
	for _, rule := range rules {
		if !reflect.DeepEqual(rule.APIGroups, []string{apiGroup}) {
			continue
		}
		for _, got := range rule.Resources {
			if got == resource {
				return true
			}
		}
	}
	return false
}
