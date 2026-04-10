package projects

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

func TestCheckProjectCreateAccess_OwnerOnExistingProjectAllows(t *testing.T) {
	projects := []*corev1.Namespace{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "existing-project",
				Labels: map[string]string{v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue},
				Annotations: map[string]string{
					v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"owner"}]`,
				},
			},
		},
	}
	err := CheckProjectCreateAccess("alice@example.com", nil, projects)
	if err != nil {
		t.Errorf("expected access granted, got: %v", err)
	}
}

func TestCheckProjectCreateAccess_EditorOnExistingProjectDenies(t *testing.T) {
	projects := []*corev1.Namespace{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "existing-project",
				Labels: map[string]string{v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue},
				Annotations: map[string]string{
					v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"editor"}]`,
				},
			},
		},
	}
	err := CheckProjectCreateAccess("alice@example.com", nil, projects)
	if err == nil {
		t.Fatal("expected PermissionDenied, got nil")
	}
}

func TestCheckProjectCreateAccess_NoProjectsDenies(t *testing.T) {
	err := CheckProjectCreateAccess("alice@example.com", nil, nil)
	if err == nil {
		t.Fatal("expected PermissionDenied, got nil")
	}
}
