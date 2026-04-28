package admission_test

import (
	"os"
	"strings"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	envtesthelpers "github.com/holos-run/holos-console/internal/envtest"
)

func TestNamespaceShareAnnotationsPolicyProtectsLabelsAndShares(t *testing.T) {
	data, err := os.ReadFile("namespace-share-annotations-console-only.yaml")
	if err != nil {
		t.Fatalf("reading policy: %v", err)
	}
	docs := envtesthelpers.SplitYAMLDocuments(data)
	if len(docs) == 0 {
		t.Fatal("policy file has no YAML documents")
	}

	policy := &admissionregistrationv1.ValidatingAdmissionPolicy{}
	if err := yaml.Unmarshal(docs[0], policy); err != nil {
		t.Fatalf("unmarshal policy: %v", err)
	}

	var matchExpression string
	for _, condition := range policy.Spec.MatchConditions {
		if condition.Name == "console-managed-or-share-annotated-namespace" {
			matchExpression = condition.Expression
			break
		}
	}
	for _, want := range []string{
		`app.kubernetes.io/managed-by`,
		`console.holos.run/share-users`,
		`console.holos.run/share-roles`,
		`console.holos.run/default-share-users`,
		`console.holos.run/default-share-roles`,
		`console.holos.run/rbac-share-users`,
		`console.holos.run/creator-email`,
		`console.holos.run/creator-sub`,
		`console.holos.run/resource-type`,
		`console.holos.run/organization`,
		`console.holos.run/folder`,
		`console.holos.run/project`,
		`console.holos.run/parent`,
	} {
		if !strings.Contains(matchExpression, want) {
			t.Fatalf("match condition does not cover %q: %s", want, matchExpression)
		}
	}

	for _, name := range []string{
		"oldShareUsers",
		"newShareUsers",
		"oldShareRoles",
		"newShareRoles",
		"oldDefaultShareUsers",
		"newDefaultShareUsers",
		"oldDefaultShareRoles",
		"newDefaultShareRoles",
		"oldRBACShareUsers",
		"newRBACShareUsers",
		"oldCreatorEmail",
		"newCreatorEmail",
		"oldCreatorSubject",
		"newCreatorSubject",
		"oldManagedBy",
		"newManagedBy",
		"oldResourceType",
		"newResourceType",
		"oldOrganization",
		"newOrganization",
		"oldFolder",
		"newFolder",
		"oldProject",
		"newProject",
		"oldParent",
		"newParent",
	} {
		assertPolicyVariable(t, policy, name)
	}

	if len(policy.Spec.Validations) != 1 {
		t.Fatalf("validations = %d, want 1", len(policy.Spec.Validations))
	}
	validation := policy.Spec.Validations[0].Expression
	for _, want := range []string{
		`variables.oldShareUsers == variables.newShareUsers`,
		`variables.oldShareRoles == variables.newShareRoles`,
		`variables.oldDefaultShareUsers == variables.newDefaultShareUsers`,
		`variables.oldDefaultShareRoles == variables.newDefaultShareRoles`,
		`variables.oldRBACShareUsers == variables.newRBACShareUsers`,
		`variables.oldCreatorEmail == variables.newCreatorEmail`,
		`variables.oldCreatorSubject == variables.newCreatorSubject`,
		`variables.oldManagedBy == variables.newManagedBy`,
		`variables.oldResourceType == variables.newResourceType`,
		`variables.oldOrganization == variables.newOrganization`,
		`variables.oldFolder == variables.newFolder`,
		`variables.oldProject == variables.newProject`,
		`variables.oldParent == variables.newParent`,
	} {
		if !strings.Contains(validation, want) {
			t.Fatalf("validation does not enforce %q: %s", want, validation)
		}
	}

	for _, name := range []string{"excluded-holos-console", "excluded-cluster-admins"} {
		if !hasMatchCondition(policy, name) {
			t.Fatalf("missing match condition %q", name)
		}
	}
	if strings.Contains(matchExpression, "console.holos.run/default-folder") {
		t.Fatalf("match condition still references default-folder: %s", matchExpression)
	}
	if strings.Contains(validation, "DefaultFolder") || strings.Contains(validation, "default-folder") {
		t.Fatalf("validation still references default-folder: %s", validation)
	}
}

func assertPolicyVariable(t *testing.T, policy *admissionregistrationv1.ValidatingAdmissionPolicy, name string) {
	t.Helper()
	for _, variable := range policy.Spec.Variables {
		if variable.Name == name {
			return
		}
	}
	t.Fatalf("missing policy variable %q", name)
}

func hasMatchCondition(policy *admissionregistrationv1.ValidatingAdmissionPolicy, name string) bool {
	for _, condition := range policy.Spec.MatchConditions {
		if condition.Name == name {
			return true
		}
	}
	return false
}
