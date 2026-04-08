package deployments

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
	"github.com/holos-run/holos-console/console/resolver"
)

func testResolver() *resolver.Resolver {
	return &resolver.Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
}

func projectNS(project string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prj-" + project,
			Labels: map[string]string{
				ManagedByLabel:             ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeProject,
				resolver.ProjectLabel:      project,
			},
		},
	}
}

func deploymentConfigMap(project, name, image, tag, tmpl, displayName, description string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "prj-" + project,
			Labels: map[string]string{
				ManagedByLabel:    ManagedByValue,
				ResourceTypeLabel: ResourceTypeValue,
			},
			Annotations: map[string]string{
				DisplayNameAnnotation: displayName,
				DescriptionAnnotation: description,
			},
		},
		Data: map[string]string{
			ImageKey:    image,
			TagKey:      tag,
			TemplateKey: tmpl,
		},
	}
}

func TestListDeployments(t *testing.T) {
	t.Run("returns empty list when no deployments exist", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListDeployments(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 0 {
			t.Errorf("expected 0 deployments, got %d", len(cms))
		}
	})

	t.Run("returns deployments with correct label", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "A web application")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListDeployments(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 1 {
			t.Fatalf("expected 1 deployment, got %d", len(cms))
		}
		if cms[0].Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", cms[0].Name)
		}
	})

	t.Run("does not return unlabeled configmaps", func(t *testing.T) {
		ns := projectNS("my-project")
		unlabeled := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-cm",
				Namespace: "prj-my-project",
			},
		}
		fakeClient := fake.NewClientset(ns, unlabeled)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListDeployments(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 0 {
			t.Errorf("expected 0 deployments (unlabeled CM should not appear), got %d", len(cms))
		}
	})
}

func TestGetDeployment(t *testing.T) {
	t.Run("returns deployment by name", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		got, err := k8s.GetDeployment(context.Background(), "my-project", "web-app")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got.Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", got.Name)
		}
		if got.Data[ImageKey] != "nginx" {
			t.Errorf("expected image 'nginx', got %q", got.Data[ImageKey])
		}
		if got.Data[TagKey] != "1.25" {
			t.Errorf("expected tag '1.25', got %q", got.Data[TagKey])
		}
	})

	t.Run("returns error for non-existent deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		_, err := k8s.GetDeployment(context.Background(), "my-project", "does-not-exist")
		if err == nil {
			t.Fatal("expected error for non-existent deployment")
		}
	})
}

func TestCreateDeployment(t *testing.T) {
	t.Run("creates deployment with correct fields", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateDeployment(context.Background(), "my-project", "web-app", "nginx", "1.25", "default", "Web App", "A web app", nil, nil, nil, 0)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Name != "web-app" {
			t.Errorf("expected name 'web-app', got %q", cm.Name)
		}
		if cm.Labels[ResourceTypeLabel] != ResourceTypeValue {
			t.Errorf("expected label %q=%q, got %q", ResourceTypeLabel, ResourceTypeValue, cm.Labels[ResourceTypeLabel])
		}
		if cm.Labels[ManagedByLabel] != ManagedByValue {
			t.Errorf("expected label %q=%q, got %q", ManagedByLabel, ManagedByValue, cm.Labels[ManagedByLabel])
		}
		if cm.Data[ImageKey] != "nginx" {
			t.Errorf("expected image 'nginx', got %q", cm.Data[ImageKey])
		}
		if cm.Data[TagKey] != "1.25" {
			t.Errorf("expected tag '1.25', got %q", cm.Data[TagKey])
		}
		if cm.Data[TemplateKey] != "default" {
			t.Errorf("expected template 'default', got %q", cm.Data[TemplateKey])
		}
		if cm.Annotations[DisplayNameAnnotation] != "Web App" {
			t.Errorf("expected displayName 'Web App', got %q", cm.Annotations[DisplayNameAnnotation])
		}
		if cm.Annotations[DescriptionAnnotation] != "A web app" {
			t.Errorf("expected description 'A web app', got %q", cm.Annotations[DescriptionAnnotation])
		}
	})

	t.Run("stores command and args as JSON", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cmd := []string{"myapp"}
		args := []string{"--port", "8080"}
		cm, err := k8s.CreateDeployment(context.Background(), "my-project", "web-app", "nginx", "1.25", "default", "", "", cmd, args, nil, 0)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Data[CommandKey] != `["myapp"]` {
			t.Errorf("expected command JSON %q, got %q", `["myapp"]`, cm.Data[CommandKey])
		}
		if cm.Data[ArgsKey] != `["--port","8080"]` {
			t.Errorf("expected args JSON %q, got %q", `["--port","8080"]`, cm.Data[ArgsKey])
		}
	})

	t.Run("omits command and args keys when empty", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateDeployment(context.Background(), "my-project", "web-app", "nginx", "1.25", "default", "", "", nil, nil, nil, 0)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := cm.Data[CommandKey]; ok {
			t.Error("expected command key to be absent when nil")
		}
		if _, ok := cm.Data[ArgsKey]; ok {
			t.Error("expected args key to be absent when nil")
		}
	})
}

func TestUpdateDeployment(t *testing.T) {
	t.Run("updates only non-nil fields", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "original desc")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newTag := "1.26"
		newDesc := "updated desc"
		updated, err := k8s.UpdateDeployment(context.Background(), "my-project", "web-app", nil, &newTag, nil, &newDesc, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Data[ImageKey] != "nginx" {
			t.Errorf("expected image unchanged 'nginx', got %q", updated.Data[ImageKey])
		}
		if updated.Data[TagKey] != "1.26" {
			t.Errorf("expected tag '1.26', got %q", updated.Data[TagKey])
		}
		if updated.Annotations[DisplayNameAnnotation] != "Web App" {
			t.Errorf("expected displayName unchanged 'Web App', got %q", updated.Annotations[DisplayNameAnnotation])
		}
		if updated.Annotations[DescriptionAnnotation] != "updated desc" {
			t.Errorf("expected description 'updated desc', got %q", updated.Annotations[DescriptionAnnotation])
		}
	})

	t.Run("returns error for non-existent deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		newTag := "1.26"
		_, err := k8s.UpdateDeployment(context.Background(), "my-project", "does-not-exist", nil, &newTag, nil, nil, nil, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error for non-existent deployment")
		}
	})
}

func TestCreateDeployment_Env(t *testing.T) {
	t.Run("stores env vars as JSON", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		env := []v1alpha1.EnvVar{
			{Name: "FOO", Value: "bar"},
			{Name: "FROM_SECRET", SecretKeyRef: &v1alpha1.KeyRef{Name: "mysecret", Key: "mykey"}},
			{Name: "FROM_CM", ConfigMapKeyRef: &v1alpha1.KeyRef{Name: "mycm", Key: "mykey"}},
		}
		cm, err := k8s.CreateDeployment(context.Background(), "my-project", "web-app", "nginx", "1.25", "default", "", "", nil, nil, env, 0)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		raw, ok := cm.Data[EnvKey]
		if !ok {
			t.Fatal("expected env key to be present")
		}
		var got []v1alpha1.EnvVar
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("expected valid JSON in env key, got error: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 env vars, got %d", len(got))
		}
		if got[0].Name != "FOO" || got[0].Value != "bar" {
			t.Errorf("unexpected first env var: %+v", got[0])
		}
		if got[1].Name != "FROM_SECRET" || got[1].SecretKeyRef == nil || got[1].SecretKeyRef.Name != "mysecret" {
			t.Errorf("unexpected second env var: %+v", got[1])
		}
		if got[2].Name != "FROM_CM" || got[2].ConfigMapKeyRef == nil || got[2].ConfigMapKeyRef.Name != "mycm" {
			t.Errorf("unexpected third env var: %+v", got[2])
		}
	})

	t.Run("omits env key when empty", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateDeployment(context.Background(), "my-project", "web-app", "nginx", "1.25", "default", "", "", nil, nil, nil, 0)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := cm.Data[EnvKey]; ok {
			t.Error("expected env key to be absent when nil")
		}
	})
}

func TestUpdateDeployment_Env(t *testing.T) {
	t.Run("stores env vars as JSON on update", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		env := []v1alpha1.EnvVar{
			{Name: "PORT", Value: "8080"},
		}
		updated, err := k8s.UpdateDeployment(context.Background(), "my-project", "web-app", nil, nil, nil, nil, nil, nil, env, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		raw, ok := updated.Data[EnvKey]
		if !ok {
			t.Fatal("expected env key to be present after update")
		}
		var got []v1alpha1.EnvVar
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("unexpected JSON error: %v", err)
		}
		if len(got) != 1 || got[0].Name != "PORT" || got[0].Value != "8080" {
			t.Errorf("unexpected env vars: %+v", got)
		}
	})
}

func TestCreateDeployment_Port(t *testing.T) {
	t.Run("stores port as string in ConfigMap", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateDeployment(context.Background(), "my-project", "web-app", "nginx", "1.25", "default", "", "", nil, nil, nil, 9090)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Data[PortKey] != "9090" {
			t.Errorf("expected port %q, got %q", "9090", cm.Data[PortKey])
		}
	})

	t.Run("omits port key when zero", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateDeployment(context.Background(), "my-project", "web-app", "nginx", "1.25", "default", "", "", nil, nil, nil, 0)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := cm.Data[PortKey]; ok {
			t.Error("expected port key to be absent when zero")
		}
	})

	t.Run("round-trip: create with port then get returns same port", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		_, err := k8s.CreateDeployment(context.Background(), "my-project", "web-app", "nginx", "1.25", "default", "", "", nil, nil, nil, 3000)
		if err != nil {
			t.Fatalf("expected no error creating deployment, got %v", err)
		}

		got, err := k8s.GetDeployment(context.Background(), "my-project", "web-app")
		if err != nil {
			t.Fatalf("expected no error getting deployment, got %v", err)
		}
		dep := configMapToDeployment(got, "my-project")
		if dep.Port != 3000 {
			t.Errorf("expected port 3000, got %d", dep.Port)
		}
	})
}

func TestUpdateDeployment_Port(t *testing.T) {
	t.Run("updates port in ConfigMap", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "", "")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newPort := int32(8081)
		updated, err := k8s.UpdateDeployment(context.Background(), "my-project", "web-app", nil, nil, nil, nil, nil, nil, nil, &newPort)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Data[PortKey] != "8081" {
			t.Errorf("expected port %q, got %q", "8081", updated.Data[PortKey])
		}
	})

	t.Run("does not update port when nil", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "", "")
		cm.Data[PortKey] = "9090"
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		updated, err := k8s.UpdateDeployment(context.Background(), "my-project", "web-app", nil, nil, nil, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Data[PortKey] != "9090" {
			t.Errorf("expected port unchanged %q, got %q", "9090", updated.Data[PortKey])
		}
	})
}

func TestDeleteDeployment(t *testing.T) {
	t.Run("deletes existing deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "", "")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.DeleteDeployment(context.Background(), "my-project", "web-app")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Verify it was deleted.
		_, err = k8s.GetDeployment(context.Background(), "my-project", "web-app")
		if err == nil {
			t.Fatal("expected error after deletion")
		}
	})

	t.Run("returns error for non-existent deployment", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.DeleteDeployment(context.Background(), "my-project", "does-not-exist")
		if err == nil {
			t.Fatal("expected error for non-existent deployment")
		}
	})
}

func TestListNamespaceSecrets(t *testing.T) {
	t.Run("returns secrets with keys", func(t *testing.T) {
		ns := projectNS("my-project")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "prj-my-project",
			},
			Data: map[string][]byte{
				"password": []byte("s3cr3t"),
				"username": []byte("admin"),
			},
		}
		fakeClient := fake.NewClientset(ns, secret)
		k8s := NewK8sClient(fakeClient, testResolver())

		secrets, err := k8s.ListNamespaceSecrets(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(secrets) != 1 {
			t.Fatalf("expected 1 secret, got %d", len(secrets))
		}
		if secrets[0].Name != "my-secret" {
			t.Errorf("expected name 'my-secret', got %q", secrets[0].Name)
		}
		if len(secrets[0].Keys) != 2 {
			t.Errorf("expected 2 keys, got %d: %v", len(secrets[0].Keys), secrets[0].Keys)
		}
	})

	t.Run("excludes service-account-token secrets", func(t *testing.T) {
		ns := projectNS("my-project")
		saToken := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sa-token",
				Namespace: "prj-my-project",
			},
			Type: corev1.SecretTypeServiceAccountToken,
			Data: map[string][]byte{"token": []byte("tok")},
		}
		userSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "user-secret",
				Namespace: "prj-my-project",
			},
			Data: map[string][]byte{"key": []byte("val")},
		}
		fakeClient := fake.NewClientset(ns, saToken, userSecret)
		k8s := NewK8sClient(fakeClient, testResolver())

		secrets, err := k8s.ListNamespaceSecrets(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(secrets) != 1 {
			t.Fatalf("expected 1 secret (sa-token excluded), got %d", len(secrets))
		}
		if secrets[0].Name != "user-secret" {
			t.Errorf("expected name 'user-secret', got %q", secrets[0].Name)
		}
	})

	t.Run("returns empty list when no secrets exist", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		secrets, err := k8s.ListNamespaceSecrets(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(secrets) != 0 {
			t.Errorf("expected 0 secrets, got %d", len(secrets))
		}
	})
}

func TestListNamespaceConfigMaps(t *testing.T) {
	t.Run("returns configmaps with keys", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-config",
				Namespace: "prj-my-project",
			},
			Data: map[string]string{
				"app.conf": "port=8080",
				"debug":    "false",
			},
		}
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListNamespaceConfigMaps(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 1 {
			t.Fatalf("expected 1 configmap, got %d", len(cms))
		}
		if cms[0].Name != "my-config" {
			t.Errorf("expected name 'my-config', got %q", cms[0].Name)
		}
		if len(cms[0].Keys) != 2 {
			t.Errorf("expected 2 keys, got %d: %v", len(cms[0].Keys), cms[0].Keys)
		}
	})

	t.Run("excludes console-managed configmaps", func(t *testing.T) {
		ns := projectNS("my-project")
		consoleCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "console-deployment",
				Namespace: "prj-my-project",
				Labels: map[string]string{
					ResourceTypeLabel: "deployment",
				},
			},
			Data: map[string]string{"image": "nginx"},
		}
		userCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "user-config",
				Namespace: "prj-my-project",
			},
			Data: map[string]string{"key": "val"},
		}
		fakeClient := fake.NewClientset(ns, consoleCM, userCM)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListNamespaceConfigMaps(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 1 {
			t.Fatalf("expected 1 configmap (console-managed excluded), got %d", len(cms))
		}
		if cms[0].Name != "user-config" {
			t.Errorf("expected name 'user-config', got %q", cms[0].Name)
		}
	})

	t.Run("returns empty list when no configmaps exist", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListNamespaceConfigMaps(context.Background(), "my-project")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 0 {
			t.Errorf("expected 0 configmaps, got %d", len(cms))
		}
	})
}
