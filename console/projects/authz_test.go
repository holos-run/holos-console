package projects

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/holos-run/holos-console/console/secrets"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCheckProjectReadAccess_UserGrantAllows(t *testing.T) {
	err := CheckProjectReadAccess(
		"alice@example.com",
		nil,
		map[string]string{"alice@example.com": "viewer"},
		nil,
	)
	if err != nil {
		t.Errorf("expected access granted, got: %v", err)
	}
}

func TestCheckProjectReadAccess_GroupGrantAllows(t *testing.T) {
	err := CheckProjectReadAccess(
		"bob@example.com",
		[]string{"engineering"},
		nil,
		map[string]string{"engineering": "viewer"},
	)
	if err != nil {
		t.Errorf("expected access granted via role, got: %v", err)
	}
}

func TestCheckProjectReadAccess_NoGrantDenies(t *testing.T) {
	err := CheckProjectReadAccess(
		"nobody@example.com",
		[]string{"unknown"},
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("expected PermissionDenied, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodePermissionDenied {
		t.Errorf("expected CodePermissionDenied, got %v", connectErr.Code())
	}
}

func TestCheckProjectWriteAccess_EditorAllows(t *testing.T) {
	err := CheckProjectWriteAccess(
		"alice@example.com",
		nil,
		map[string]string{"alice@example.com": "editor"},
		nil,
	)
	if err != nil {
		t.Errorf("expected access granted, got: %v", err)
	}
}

func TestCheckProjectWriteAccess_ViewerDenies(t *testing.T) {
	err := CheckProjectWriteAccess(
		"alice@example.com",
		nil,
		map[string]string{"alice@example.com": "viewer"},
		nil,
	)
	if err == nil {
		t.Fatal("expected PermissionDenied, got nil")
	}
}

func TestCheckProjectDeleteAccess_OwnerAllows(t *testing.T) {
	err := CheckProjectDeleteAccess(
		"alice@example.com",
		nil,
		map[string]string{"alice@example.com": "owner"},
		nil,
	)
	if err != nil {
		t.Errorf("expected access granted, got: %v", err)
	}
}

func TestCheckProjectDeleteAccess_EditorDenies(t *testing.T) {
	err := CheckProjectDeleteAccess(
		"alice@example.com",
		nil,
		map[string]string{"alice@example.com": "editor"},
		nil,
	)
	if err == nil {
		t.Fatal("expected PermissionDenied, got nil")
	}
}

func TestCheckProjectAdminAccess_OwnerAllows(t *testing.T) {
	err := CheckProjectAdminAccess(
		"alice@example.com",
		nil,
		map[string]string{"alice@example.com": "owner"},
		nil,
	)
	if err != nil {
		t.Errorf("expected access granted, got: %v", err)
	}
}

func TestCheckProjectAdminAccess_EditorDenies(t *testing.T) {
	err := CheckProjectAdminAccess(
		"alice@example.com",
		nil,
		map[string]string{"alice@example.com": "editor"},
		nil,
	)
	if err == nil {
		t.Fatal("expected PermissionDenied, got nil")
	}
}

func TestCheckProjectCreateAccess_OwnerOnExistingProjectAllows(t *testing.T) {
	projects := []*corev1.Namespace{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "existing-project",
				Labels: map[string]string{secrets.ManagedByLabel: secrets.ManagedByValue},
				Annotations: map[string]string{
					secrets.ShareUsersAnnotation: `[{"principal":"alice@example.com","role":"owner"}]`,
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
				Labels: map[string]string{secrets.ManagedByLabel: secrets.ManagedByValue},
				Annotations: map[string]string{
					secrets.ShareUsersAnnotation: `[{"principal":"alice@example.com","role":"editor"}]`,
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
