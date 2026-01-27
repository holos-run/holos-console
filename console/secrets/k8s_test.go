package secrets

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetSecret(t *testing.T) {
	t.Run("returns secret by name from current namespace", func(t *testing.T) {
		// Given: Secret "my-secret" exists in namespace
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
			},
			Data: map[string][]byte{
				"username": []byte("admin"),
				"password": []byte("secret123"),
			},
		}
		fakeClient := fake.NewSimpleClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")

		// When: GetSecret("my-secret") is called
		result, err := k8sClient.GetSecret(context.Background(), "my-secret")

		// Then: Returns the Secret object
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result == nil {
			t.Fatal("expected secret, got nil")
		}
		if result.Name != "my-secret" {
			t.Errorf("expected name 'my-secret', got %q", result.Name)
		}
		if string(result.Data["username"]) != "admin" {
			t.Errorf("expected username 'admin', got %q", string(result.Data["username"]))
		}
	})

	t.Run("returns NotFound error for non-existent secret", func(t *testing.T) {
		// Given: Secret "missing" does not exist
		fakeClient := fake.NewSimpleClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")

		// When: GetSecret("missing") is called
		_, err := k8sClient.GetSecret(context.Background(), "missing")

		// Then: Returns NotFound error
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected NotFound error, got %v", err)
		}
	})
}

func TestGetAllowedGroups(t *testing.T) {
	t.Run("parses allowed-groups annotation", func(t *testing.T) {
		// Given: Secret with annotation holos.run/allowed-groups: ["admin","ops"]
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-secret",
				Annotations: map[string]string{
					AllowedGroupsAnnotation: `["admin","ops"]`,
				},
			},
		}

		// When: GetAllowedGroups(secret) is called
		groups, err := GetAllowedGroups(secret)

		// Then: Returns ["admin", "ops"]
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(groups) != 2 {
			t.Fatalf("expected 2 groups, got %d", len(groups))
		}
		if groups[0] != "admin" {
			t.Errorf("expected first group 'admin', got %q", groups[0])
		}
		if groups[1] != "ops" {
			t.Errorf("expected second group 'ops', got %q", groups[1])
		}
	})

	t.Run("returns empty slice when annotation is missing", func(t *testing.T) {
		// Given: Secret without holos.run/allowed-groups annotation
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-secret",
				Annotations: map[string]string{},
			},
		}

		// When: GetAllowedGroups(secret) is called
		groups, err := GetAllowedGroups(secret)

		// Then: Returns empty slice (no groups allowed)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(groups) != 0 {
			t.Errorf("expected empty slice, got %v", groups)
		}
	})

	t.Run("returns empty slice when annotations map is nil", func(t *testing.T) {
		// Given: Secret with nil annotations
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-secret",
			},
		}

		// When: GetAllowedGroups(secret) is called
		groups, err := GetAllowedGroups(secret)

		// Then: Returns empty slice (no groups allowed)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(groups) != 0 {
			t.Errorf("expected empty slice, got %v", groups)
		}
	})

	t.Run("returns error for malformed annotation", func(t *testing.T) {
		// Given: Secret with annotation holos.run/allowed-groups: not-json
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-secret",
				Annotations: map[string]string{
					AllowedGroupsAnnotation: "not-json",
				},
			},
		}

		// When: GetAllowedGroups(secret) is called
		_, err := GetAllowedGroups(secret)

		// Then: Returns error (invalid JSON)
		if err == nil {
			t.Fatal("expected error for malformed JSON, got nil")
		}
	})

	t.Run("returns error for wrong JSON type", func(t *testing.T) {
		// Given: Secret with annotation that is valid JSON but wrong type
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-secret",
				Annotations: map[string]string{
					AllowedGroupsAnnotation: `{"not": "an array"}`,
				},
			},
		}

		// When: GetAllowedGroups(secret) is called
		_, err := GetAllowedGroups(secret)

		// Then: Returns error (expected array, got object)
		if err == nil {
			t.Fatal("expected error for wrong JSON type, got nil")
		}
	})
}
