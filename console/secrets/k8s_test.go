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

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/oidc"
	"github.com/holos-run/holos-console/console/resolver"
)

func testResolver() *resolver.Resolver {
	return &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
}

// projectNS creates a project namespace fixture.
func projectNS(project string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prj-" + project,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      project,
			},
		},
	}
}

func TestRBACUserGrantsForSubjects(t *testing.T) {
	grants := RBACUserGrantsForSubjects([]AnnotationGrant{
		{Principal: "alice@example.com", Role: "editor"},
		{Principal: "admin@localhost", Role: "owner"},
		{Principal: "product@localhost", Role: "editor"},
		{Principal: "unresolved@example.com", Role: "viewer"},
		{Principal: "subject-bob", Role: "viewer"},
		{Principal: "oidc:subject-carol", Role: "owner"},
	}, UserIdentity{Email: "alice@example.com", Subject: "subject-alice"}, UserIdentity{Email: "admin@localhost", Subject: "live-admin-sub"})

	got := map[string]string{}
	for _, grant := range grants {
		got[grant.Principal] = grant.Role
	}
	if got["subject-alice"] != "editor" {
		t.Fatalf("current user email was not resolved to subject: %v", grants)
	}
	if got["live-admin-sub"] != "owner" {
		t.Fatalf("live caller subject did not override static dev persona subject: %v", grants)
	}
	staticAdminSubject, ok := oidc.TestUserSubjectForEmail("admin@localhost")
	if !ok {
		t.Fatalf("admin dev persona subject not found")
	}
	if _, ok := got[staticAdminSubject]; ok {
		t.Fatalf("static dev persona subject overrode live caller subject: %v", grants)
	}
	if _, ok := got["alice@example.com"]; ok {
		t.Fatalf("resolved email principal was copied into RBAC grants: %v", grants)
	}
	if _, ok := got["unresolved@example.com"]; ok {
		t.Fatalf("unresolved email principal was copied into RBAC grants: %v", grants)
	}
	productSubject, ok := oidc.TestUserSubjectForEmail("product@localhost")
	if !ok {
		t.Fatalf("product dev persona subject not found")
	}
	if got[productSubject] != "editor" {
		t.Fatalf("dev persona email was not resolved to subject: %v", grants)
	}
	if _, ok = got["product@localhost"]; ok {
		t.Fatalf("dev persona email principal was copied into RBAC grants: %v", grants)
	}
	if got["subject-bob"] != "viewer" {
		t.Fatalf("subject grant not preserved: %v", grants)
	}
	if got["oidc:subject-carol"] != "owner" {
		t.Fatalf("prefixed subject grant not preserved: %v", grants)
	}
}

func TestGetSecret(t *testing.T) {
	t.Run("returns secret by name from current namespace", func(t *testing.T) {
		// Given: Secret "my-secret" exists in namespace
		ns := projectNS("test-namespace")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "prj-test-namespace",
			},
			Data: map[string][]byte{
				"username": []byte("admin"),
				"password": []byte("secret123"),
			},
		}
		fakeClient := fake.NewClientset(ns, secret)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		// When: GetSecret("my-secret") is called
		result, err := k8sClient.GetSecret(context.Background(), "test-namespace", "my-secret")

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
		ns := projectNS("test-namespace")
		fakeClient := fake.NewClientset(ns)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		// When: GetSecret("missing") is called
		_, err := k8sClient.GetSecret(context.Background(), "test-namespace", "missing")

		// Then: Returns NotFound error
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected NotFound error, got %v", err)
		}
	})

	t.Run("returns error for non-existent project", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		k8sClient := NewK8sClient(fakeClient, testResolver())

		_, err := k8sClient.GetSecret(context.Background(), "no-such-project", "my-secret")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestUpdateSecret(t *testing.T) {
	t.Run("replaces secret data", func(t *testing.T) {
		// Given: Managed secret with original data
		ns := projectNS("test-namespace")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "prj-test-namespace",
				Labels: map[string]string{
					v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
				},
			},
			Data: map[string][]byte{
				"old-key": []byte("old-value"),
			},
		}
		fakeClient := fake.NewClientset(ns, secret)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		// When: UpdateSecret is called with new data
		newData := map[string][]byte{
			"new-key": []byte("new-value"),
		}
		result, err := k8sClient.UpdateSecret(context.Background(), "test-namespace", "my-secret", newData, nil, nil)

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
		ns := projectNS("test-namespace")
		fakeClient := fake.NewClientset(ns)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		// When: UpdateSecret is called
		_, err := k8sClient.UpdateSecret(context.Background(), "test-namespace", "missing", map[string][]byte{"k": []byte("v")}, nil, nil)

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
		ns := projectNS("test-namespace")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unmanaged-secret",
				Namespace: "prj-test-namespace",
			},
			Data: map[string][]byte{
				"key": []byte("value"),
			},
		}
		fakeClient := fake.NewClientset(ns, secret)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		// When: UpdateSecret is called
		_, err := k8sClient.UpdateSecret(context.Background(), "test-namespace", "unmanaged-secret", map[string][]byte{"k": []byte("v")}, nil, nil)

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
	t.Run("creates secret with correct labels and no sharing annotations", func(t *testing.T) {
		// Given: No secrets exist
		ns := projectNS("test-namespace")
		fakeClient := fake.NewClientset(ns)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		// When: CreateSecret is called with sharing grants
		data := map[string][]byte{"key": []byte("value")}
		shareUsers := []AnnotationGrant{{Principal: "alice@example.com", Role: "owner"}}
		shareRoles := []AnnotationGrant{{Principal: "dev-team", Role: "editor"}}
		result, err := k8sClient.CreateSecret(context.Background(), "test-namespace", "new-secret", data, shareUsers, shareRoles, "", "")

		// Then: Returns created secret with labels. Sharing is represented by
		// RoleBindings, not Secret annotations.
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result.Name != "new-secret" {
			t.Errorf("expected name 'new-secret', got %q", result.Name)
		}
		if result.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
			t.Errorf("expected managed-by label, got %v", result.Labels)
		}
		if _, ok := result.Annotations[v1alpha2.AnnotationShareUsers]; ok {
			t.Fatal("did not expect share-users annotation")
		}
		if _, ok := result.Annotations[v1alpha2.AnnotationShareRoles]; ok {
			t.Fatal("did not expect share-roles annotation")
		}
		if string(result.Data["key"]) != "value" {
			t.Errorf("expected key='value', got %q", string(result.Data["key"]))
		}
	})

	t.Run("returns AlreadyExists for duplicate name", func(t *testing.T) {
		// Given: Secret already exists
		ns := projectNS("test-namespace")
		existing := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "existing-secret",
				Namespace: "prj-test-namespace",
			},
		}
		fakeClient := fake.NewClientset(ns, existing)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		// When: CreateSecret with same name
		_, err := k8sClient.CreateSecret(context.Background(), "test-namespace", "existing-secret", map[string][]byte{"k": []byte("v")}, nil, nil, "", "")

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
		ns := projectNS("test-namespace")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "prj-test-namespace",
				Labels: map[string]string{
					v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
				},
			},
		}
		fakeClient := fake.NewClientset(ns, secret)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		// When: DeleteSecret is called
		err := k8sClient.DeleteSecret(context.Background(), "test-namespace", "my-secret")

		// Then: No error
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Verify secret is gone
		_, err = k8sClient.GetSecret(context.Background(), "test-namespace", "my-secret")
		if !errors.IsNotFound(err) {
			t.Errorf("expected NotFound after delete, got %v", err)
		}
	})

	t.Run("returns NotFound for non-existent secret", func(t *testing.T) {
		ns := projectNS("test-namespace")
		fakeClient := fake.NewClientset(ns)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		err := k8sClient.DeleteSecret(context.Background(), "test-namespace", "missing")

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected NotFound error, got %v", err)
		}
	})

	t.Run("returns error for secret without managed-by label", func(t *testing.T) {
		ns := projectNS("test-namespace")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unmanaged-secret",
				Namespace: "prj-test-namespace",
			},
		}
		fakeClient := fake.NewClientset(ns, secret)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		err := k8sClient.DeleteSecret(context.Background(), "test-namespace", "unmanaged-secret")

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "not managed by") {
			t.Errorf("expected managed-by error, got %v", err)
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

func TestGetDescription(t *testing.T) {
	t.Run("returns description from annotation", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					v1alpha2.AnnotationDescription: "Database credentials for production",
				},
			},
		}
		if got := GetDescription(secret); got != "Database credentials for production" {
			t.Errorf("expected 'Database credentials for production', got %q", got)
		}
	})

	t.Run("returns empty string when annotation is missing", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		}
		if got := GetDescription(secret); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("returns empty string when annotations map is nil", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{},
		}
		if got := GetDescription(secret); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}

func TestGetURL(t *testing.T) {
	t.Run("returns URL from annotation", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					v1alpha2.AnnotationURL: "https://example.com/service",
				},
			},
		}
		if got := GetURL(secret); got != "https://example.com/service" {
			t.Errorf("expected 'https://example.com/service', got %q", got)
		}
	})

	t.Run("returns empty string when annotation is missing", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		}
		if got := GetURL(secret); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("returns empty string when annotations map is nil", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{},
		}
		if got := GetURL(secret); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}

func TestCreateSecretWithDescriptionAndURL(t *testing.T) {
	t.Run("stores description and URL annotations", func(t *testing.T) {
		ns := projectNS("test-namespace")
		fakeClient := fake.NewClientset(ns)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		data := map[string][]byte{"key": []byte("value")}
		result, err := k8sClient.CreateSecret(context.Background(), "test-namespace", "my-secret", data, nil, nil, "DB creds", "https://db.example.com")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if GetDescription(result) != "DB creds" {
			t.Errorf("expected description 'DB creds', got %q", GetDescription(result))
		}
		if GetURL(result) != "https://db.example.com" {
			t.Errorf("expected URL 'https://db.example.com', got %q", GetURL(result))
		}
	})

	t.Run("omits annotations when empty", func(t *testing.T) {
		ns := projectNS("test-namespace")
		fakeClient := fake.NewClientset(ns)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		data := map[string][]byte{"key": []byte("value")}
		result, err := k8sClient.CreateSecret(context.Background(), "test-namespace", "my-secret", data, nil, nil, "", "")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := result.Annotations[v1alpha2.AnnotationDescription]; ok {
			t.Error("expected no description annotation when empty")
		}
		if _, ok := result.Annotations[v1alpha2.AnnotationURL]; ok {
			t.Error("expected no URL annotation when empty")
		}
	})
}

func TestUpdateSecretWithDescriptionAndURL(t *testing.T) {
	t.Run("updates description and URL annotations", func(t *testing.T) {
		ns := projectNS("test-namespace")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "prj-test-namespace",
				Labels:    map[string]string{v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(ns, secret)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		desc := "Updated description"
		url := "https://updated.example.com"
		result, err := k8sClient.UpdateSecret(context.Background(), "test-namespace", "my-secret", secret.Data, &desc, &url)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if GetDescription(result) != "Updated description" {
			t.Errorf("expected description 'Updated description', got %q", GetDescription(result))
		}
		if GetURL(result) != "https://updated.example.com" {
			t.Errorf("expected URL 'https://updated.example.com', got %q", GetURL(result))
		}
	})

	t.Run("preserves existing annotations when nil", func(t *testing.T) {
		ns := projectNS("test-namespace")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "prj-test-namespace",
				Labels:    map[string]string{v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue},
				Annotations: map[string]string{
					v1alpha2.AnnotationDescription: "Original desc",
					v1alpha2.AnnotationURL:         "https://original.example.com",
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(ns, secret)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		result, err := k8sClient.UpdateSecret(context.Background(), "test-namespace", "my-secret", secret.Data, nil, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if GetDescription(result) != "Original desc" {
			t.Errorf("expected preserved description, got %q", GetDescription(result))
		}
		if GetURL(result) != "https://original.example.com" {
			t.Errorf("expected preserved URL, got %q", GetURL(result))
		}
	})

	t.Run("clears annotations when set to empty string", func(t *testing.T) {
		ns := projectNS("test-namespace")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "prj-test-namespace",
				Labels:    map[string]string{v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue},
				Annotations: map[string]string{
					v1alpha2.AnnotationDescription: "Original desc",
					v1alpha2.AnnotationURL:         "https://original.example.com",
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
		fakeClient := fake.NewClientset(ns, secret)
		k8sClient := NewK8sClient(fakeClient, testResolver())

		empty := ""
		result, err := k8sClient.UpdateSecret(context.Background(), "test-namespace", "my-secret", secret.Data, &empty, &empty)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := result.Annotations[v1alpha2.AnnotationDescription]; ok {
			t.Error("expected description annotation to be removed")
		}
		if _, ok := result.Annotations[v1alpha2.AnnotationURL]; ok {
			t.Error("expected URL annotation to be removed")
		}
	})
}
