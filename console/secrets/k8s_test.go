package secrets

import (
	"context"
	"strings"
	"testing"
	"time"

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
		fakeClient := fake.NewClientset(secret)
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
		fakeClient := fake.NewClientset()
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

func TestUpdateSecret(t *testing.T) {
	t.Run("replaces secret data", func(t *testing.T) {
		// Given: Managed secret with original data
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
			},
			Data: map[string][]byte{
				"old-key": []byte("old-value"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")

		// When: UpdateSecret is called with new data
		newData := map[string][]byte{
			"new-key": []byte("new-value"),
		}
		result, err := k8sClient.UpdateSecret(context.Background(), "my-secret", newData)

		// Then: Returns updated secret with new data
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if string(result.Data["new-key"]) != "new-value" {
			t.Errorf("expected new-key='new-value', got %q", string(result.Data["new-key"]))
		}
		if _, ok := result.Data["old-key"]; ok {
			t.Error("expected old-key to be removed")
		}
	})

	t.Run("returns NotFound for non-existent secret", func(t *testing.T) {
		// Given: No secrets exist
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")

		// When: UpdateSecret is called
		_, err := k8sClient.UpdateSecret(context.Background(), "missing", map[string][]byte{"k": []byte("v")})

		// Then: Returns NotFound error
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected NotFound error, got %v", err)
		}
	})

	t.Run("returns error for secret without managed-by label", func(t *testing.T) {
		// Given: Secret without managed-by label
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unmanaged-secret",
				Namespace: "test-namespace",
			},
			Data: map[string][]byte{
				"key": []byte("value"),
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")

		// When: UpdateSecret is called
		_, err := k8sClient.UpdateSecret(context.Background(), "unmanaged-secret", map[string][]byte{"k": []byte("v")})

		// Then: Returns error about managed-by label
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "not managed by") {
			t.Errorf("expected managed-by error, got %v", err)
		}
	})
}

func TestCreateSecret(t *testing.T) {
	t.Run("creates secret with correct labels and sharing annotations", func(t *testing.T) {
		// Given: No secrets exist
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")

		// When: CreateSecret is called with sharing grants
		data := map[string][]byte{"key": []byte("value")}
		shareUsers := []AnnotationGrant{{Principal: "alice@example.com", Role: "owner"}}
		shareGroups := []AnnotationGrant{{Principal: "dev-team", Role: "editor"}}
		result, err := k8sClient.CreateSecret(context.Background(), "new-secret", data, shareUsers, shareGroups)

		// Then: Returns created secret with labels and sharing annotations
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result.Name != "new-secret" {
			t.Errorf("expected name 'new-secret', got %q", result.Name)
		}
		if result.Labels[ManagedByLabel] != ManagedByValue {
			t.Errorf("expected managed-by label, got %v", result.Labels)
		}
		// Verify share-users annotation
		parsedUsers, err := GetShareUsers(result)
		if err != nil {
			t.Fatalf("failed to parse share-users: %v", err)
		}
		if len(parsedUsers) != 1 || parsedUsers[0].Principal != "alice@example.com" || parsedUsers[0].Role != "owner" {
			t.Errorf("expected [{alice@example.com owner}], got %v", parsedUsers)
		}
		// Verify share-groups annotation
		parsedGroups, err := GetShareGroups(result)
		if err != nil {
			t.Fatalf("failed to parse share-groups: %v", err)
		}
		if len(parsedGroups) != 1 || parsedGroups[0].Principal != "dev-team" || parsedGroups[0].Role != "editor" {
			t.Errorf("expected [{dev-team editor}], got %v", parsedGroups)
		}
		if string(result.Data["key"]) != "value" {
			t.Errorf("expected key='value', got %q", string(result.Data["key"]))
		}
	})

	t.Run("returns AlreadyExists for duplicate name", func(t *testing.T) {
		// Given: Secret already exists
		existing := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "existing-secret",
				Namespace: "test-namespace",
			},
		}
		fakeClient := fake.NewClientset(existing)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")

		// When: CreateSecret with same name
		_, err := k8sClient.CreateSecret(context.Background(), "existing-secret", map[string][]byte{"k": []byte("v")}, nil, nil)

		// Then: Returns AlreadyExists error
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.IsAlreadyExists(err) {
			t.Errorf("expected AlreadyExists error, got %v", err)
		}
	})
}

func TestDeleteSecret(t *testing.T) {
	t.Run("deletes managed secret", func(t *testing.T) {
		// Given: Managed secret exists
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "test-namespace",
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")

		// When: DeleteSecret is called
		err := k8sClient.DeleteSecret(context.Background(), "my-secret")

		// Then: No error
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Verify secret is gone
		_, err = k8sClient.GetSecret(context.Background(), "my-secret")
		if !errors.IsNotFound(err) {
			t.Errorf("expected NotFound after delete, got %v", err)
		}
	})

	t.Run("returns NotFound for non-existent secret", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, "test-namespace")

		err := k8sClient.DeleteSecret(context.Background(), "missing")

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected NotFound error, got %v", err)
		}
	})

	t.Run("returns error for secret without managed-by label", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unmanaged-secret",
				Namespace: "test-namespace",
			},
		}
		fakeClient := fake.NewClientset(secret)
		k8sClient := NewK8sClient(fakeClient, "test-namespace")

		err := k8sClient.DeleteSecret(context.Background(), "unmanaged-secret")

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "not managed by") {
			t.Errorf("expected managed-by error, got %v", err)
		}
	})
}

func TestGetShareUsers(t *testing.T) {
	t.Run("parses share-users annotation", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"alice@example.com","role":"editor"},{"principal":"bob@example.com","role":"viewer"}]`,
				},
			},
		}
		users, err := GetShareUsers(secret)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(users) != 2 {
			t.Fatalf("expected 2 users, got %d", len(users))
		}
		if users[0].Principal != "alice@example.com" || users[0].Role != "editor" {
			t.Errorf("expected alice=editor, got %s=%s", users[0].Principal, users[0].Role)
		}
		if users[1].Principal != "bob@example.com" || users[1].Role != "viewer" {
			t.Errorf("expected bob=viewer, got %s=%s", users[1].Principal, users[1].Role)
		}
	})

	t.Run("parses grants with nbf and exp", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					ShareUsersAnnotation: `[{"principal":"alice@example.com","role":"editor","nbf":1000,"exp":2000}]`,
				},
			},
		}
		users, err := GetShareUsers(secret)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(users) != 1 {
			t.Fatalf("expected 1 user, got %d", len(users))
		}
		if users[0].Nbf == nil || *users[0].Nbf != 1000 {
			t.Errorf("expected nbf=1000, got %v", users[0].Nbf)
		}
		if users[0].Exp == nil || *users[0].Exp != 2000 {
			t.Errorf("expected exp=2000, got %v", users[0].Exp)
		}
	})

	t.Run("missing annotation returns nil", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		}
		users, err := GetShareUsers(secret)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if users != nil {
			t.Errorf("expected nil, got %v", users)
		}
	})

	t.Run("nil annotations returns nil", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{},
		}
		users, err := GetShareUsers(secret)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if users != nil {
			t.Errorf("expected nil, got %v", users)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					ShareUsersAnnotation: `{invalid`,
				},
			},
		}
		_, err := GetShareUsers(secret)
		if err == nil {
			t.Fatal("expected error for invalid JSON, got nil")
		}
	})
}

func TestGetShareGroups(t *testing.T) {
	t.Run("parses share-groups annotation", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					ShareGroupsAnnotation: `[{"principal":"platform-team","role":"owner"},{"principal":"dev-team","role":"viewer"}]`,
				},
			},
		}
		groups, err := GetShareGroups(secret)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(groups) != 2 {
			t.Fatalf("expected 2 groups, got %d", len(groups))
		}
		if groups[0].Principal != "platform-team" || groups[0].Role != "owner" {
			t.Errorf("expected platform-team=owner, got %s=%s", groups[0].Principal, groups[0].Role)
		}
	})

	t.Run("missing annotation returns nil", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		}
		groups, err := GetShareGroups(secret)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if groups != nil {
			t.Errorf("expected nil, got %v", groups)
		}
	})

	t.Run("nil annotations returns nil", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{},
		}
		groups, err := GetShareGroups(secret)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if groups != nil {
			t.Errorf("expected nil, got %v", groups)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					ShareGroupsAnnotation: `not-json`,
				},
			},
		}
		_, err := GetShareGroups(secret)
		if err == nil {
			t.Fatal("expected error for invalid JSON, got nil")
		}
	})
}

func TestActiveGrantsMap(t *testing.T) {
	now := time.Unix(1000, 0)

	t.Run("includes grants with no time bounds", func(t *testing.T) {
		grants := []AnnotationGrant{
			{Principal: "alice@example.com", Role: "editor"},
		}
		m := ActiveGrantsMap(grants, now)
		if m["alice@example.com"] != "editor" {
			t.Errorf("expected alice=editor, got %v", m)
		}
	})

	t.Run("excludes expired grants", func(t *testing.T) {
		exp := int64(999) // before now
		grants := []AnnotationGrant{
			{Principal: "alice@example.com", Role: "editor", Exp: &exp},
		}
		m := ActiveGrantsMap(grants, now)
		if _, ok := m["alice@example.com"]; ok {
			t.Error("expected expired grant to be excluded")
		}
	})

	t.Run("excludes grant expiring exactly at now", func(t *testing.T) {
		exp := int64(1000) // exactly now
		grants := []AnnotationGrant{
			{Principal: "alice@example.com", Role: "editor", Exp: &exp},
		}
		m := ActiveGrantsMap(grants, now)
		if _, ok := m["alice@example.com"]; ok {
			t.Error("expected grant expiring at now to be excluded")
		}
	})

	t.Run("includes grant not yet expired", func(t *testing.T) {
		exp := int64(1001) // after now
		grants := []AnnotationGrant{
			{Principal: "alice@example.com", Role: "editor", Exp: &exp},
		}
		m := ActiveGrantsMap(grants, now)
		if m["alice@example.com"] != "editor" {
			t.Errorf("expected alice=editor, got %v", m)
		}
	})

	t.Run("excludes not-yet-active grants", func(t *testing.T) {
		nbf := int64(1001) // after now
		grants := []AnnotationGrant{
			{Principal: "alice@example.com", Role: "editor", Nbf: &nbf},
		}
		m := ActiveGrantsMap(grants, now)
		if _, ok := m["alice@example.com"]; ok {
			t.Error("expected not-yet-active grant to be excluded")
		}
	})

	t.Run("includes grant active at nbf boundary", func(t *testing.T) {
		nbf := int64(1000) // exactly now
		grants := []AnnotationGrant{
			{Principal: "alice@example.com", Role: "editor", Nbf: &nbf},
		}
		m := ActiveGrantsMap(grants, now)
		if m["alice@example.com"] != "editor" {
			t.Errorf("expected alice=editor, got %v", m)
		}
	})

	t.Run("includes grants within valid window", func(t *testing.T) {
		nbf := int64(500)
		exp := int64(1500)
		grants := []AnnotationGrant{
			{Principal: "alice@example.com", Role: "editor", Nbf: &nbf, Exp: &exp},
		}
		m := ActiveGrantsMap(grants, now)
		if m["alice@example.com"] != "editor" {
			t.Errorf("expected alice=editor, got %v", m)
		}
	})

	t.Run("excludes grants outside valid window", func(t *testing.T) {
		nbf := int64(500)
		exp := int64(800) // expired before now
		grants := []AnnotationGrant{
			{Principal: "alice@example.com", Role: "editor", Nbf: &nbf, Exp: &exp},
		}
		m := ActiveGrantsMap(grants, now)
		if _, ok := m["alice@example.com"]; ok {
			t.Error("expected grant outside window to be excluded")
		}
	})

	t.Run("nil grants returns empty map", func(t *testing.T) {
		m := ActiveGrantsMap(nil, now)
		if len(m) != 0 {
			t.Errorf("expected empty map, got %v", m)
		}
	})

	t.Run("skips grants with empty principal", func(t *testing.T) {
		grants := []AnnotationGrant{
			{Principal: "", Role: "editor"},
		}
		m := ActiveGrantsMap(grants, now)
		if len(m) != 0 {
			t.Errorf("expected empty map, got %v", m)
		}
	})
}
