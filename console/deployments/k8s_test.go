package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
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
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
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
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeDeployment,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: displayName,
				v1alpha2.AnnotationDescription: description,
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
		if cm.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeDeployment {
			t.Errorf("expected label %q=%q, got %q", v1alpha2.LabelResourceType, v1alpha2.ResourceTypeDeployment, cm.Labels[v1alpha2.LabelResourceType])
		}
		if cm.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
			t.Errorf("expected label %q=%q, got %q", v1alpha2.LabelManagedBy, v1alpha2.ManagedByValue, cm.Labels[v1alpha2.LabelManagedBy])
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
		if cm.Annotations[v1alpha2.AnnotationDisplayName] != "Web App" {
			t.Errorf("expected displayName 'Web App', got %q", cm.Annotations[v1alpha2.AnnotationDisplayName])
		}
		if cm.Annotations[v1alpha2.AnnotationDescription] != "A web app" {
			t.Errorf("expected description 'A web app', got %q", cm.Annotations[v1alpha2.AnnotationDescription])
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
		if updated.Annotations[v1alpha2.AnnotationDisplayName] != "Web App" {
			t.Errorf("expected displayName unchanged 'Web App', got %q", updated.Annotations[v1alpha2.AnnotationDisplayName])
		}
		if updated.Annotations[v1alpha2.AnnotationDescription] != "updated desc" {
			t.Errorf("expected description 'updated desc', got %q", updated.Annotations[v1alpha2.AnnotationDescription])
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

		env := []v1alpha2.EnvVar{
			{Name: "FOO", Value: "bar"},
			{Name: "FROM_SECRET", SecretKeyRef: &v1alpha2.KeyRef{Name: "mysecret", Key: "mykey"}},
			{Name: "FROM_CM", ConfigMapKeyRef: &v1alpha2.KeyRef{Name: "mycm", Key: "mykey"}},
		}
		cm, err := k8s.CreateDeployment(context.Background(), "my-project", "web-app", "nginx", "1.25", "default", "", "", nil, nil, env, 0)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		raw, ok := cm.Data[EnvKey]
		if !ok {
			t.Fatal("expected env key to be present")
		}
		var got []v1alpha2.EnvVar
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

		env := []v1alpha2.EnvVar{
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
		var got []v1alpha2.EnvVar
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

// TestSetAggregatedLinksAnnotation covers the cache-write path used by the
// link aggregator (HOL-574). Mirrors the SetOutputURLAnnotation tests:
// non-empty payload writes, empty payload clears, identical payload is a
// no-op (avoids a needless Update), and a missing ConfigMap surfaces an
// error so the handler can decide what to log.
func TestSetAggregatedLinksAnnotation(t *testing.T) {
	t.Run("writes payload to annotation when previously absent", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "", "")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.SetAggregatedLinksAnnotation(context.Background(), "my-project", "web-app", `{"primary_url":"https://app.example.com"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := k8s.GetDeployment(context.Background(), "my-project", "web-app")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Annotations[v1alpha2.AnnotationAggregatedLinks] != `{"primary_url":"https://app.example.com"}` {
			t.Errorf("annotation mismatch, got %q", got.Annotations[v1alpha2.AnnotationAggregatedLinks])
		}
	})

	t.Run("empty payload clears existing annotation", func(t *testing.T) {
		ns := projectNS("my-project")
		cm := deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "", "")
		cm.Annotations[v1alpha2.AnnotationAggregatedLinks] = `{"primary_url":"https://stale.example.com"}`
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.SetAggregatedLinksAnnotation(context.Background(), "my-project", "web-app", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := k8s.GetDeployment(context.Background(), "my-project", "web-app")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := got.Annotations[v1alpha2.AnnotationAggregatedLinks]; ok {
			t.Errorf("expected annotation to be cleared, got %q", got.Annotations[v1alpha2.AnnotationAggregatedLinks])
		}
	})

	t.Run("missing deployment surfaces an error", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		err := k8s.SetAggregatedLinksAnnotation(context.Background(), "my-project", "missing", "payload")
		if err == nil {
			t.Fatal("expected error for missing deployment")
		}
	})
}

// fakeDynamicSchemeForK8sTest builds a scheme that knows about every kind
// the dynamic client may be asked to list. It mirrors fakeDynamicScheme in
// apply_test.go but lives next to the k8s tests so this file does not
// reach into apply test fixtures.
func fakeDynamicSchemeForK8sTest() *runtime.Scheme {
	return fakeDynamicScheme()
}

// makeOwnedUnstructured constructs an Unstructured with the deployment
// ownership labels and the supplied per-resource annotations. Used by the
// ListDeploymentResources tests so the same selector apply.go writes is
// exercised by the read path.
func makeOwnedUnstructured(apiVersion, kind, namespace, name, project, deployment string, annotations map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(apiVersion)
	u.SetKind(kind)
	u.SetNamespace(namespace)
	u.SetName(name)
	u.SetLabels(map[string]string{
		v1alpha2.LabelProject:         project,
		v1alpha2.AnnotationDeployment: deployment,
	})
	if annotations != nil {
		u.SetAnnotations(annotations)
	}
	return u
}

// TestListDeploymentResources covers the HOL-574 multi-kind scan. The scan
// must (a) return resources matching the project + deployment labels, (b)
// span every kind apply.go writes, (c) ignore resources that do not match
// the ownership selector, and (d) return (nil, nil) when no dynamic client
// is configured (local/dev wiring).
func TestListDeploymentResources(t *testing.T) {
	t.Run("nil dynamic client returns nil without error", func(t *testing.T) {
		ns := projectNS("my-project")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())
		got, err := k8s.ListDeploymentResources(context.Background(), "my-project", "web-app")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil resources, got %v", got)
		}
	})

	t.Run("validates project and deployment", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		dyn := dynamicfake.NewSimpleDynamicClient(fakeDynamicSchemeForK8sTest())
		k8s := NewK8sClient(fakeClient, testResolver()).WithDynamicClient(dyn)
		if _, err := k8s.ListDeploymentResources(context.Background(), "", "x"); err == nil {
			t.Error("expected error for empty project")
		}
		if _, err := k8s.ListDeploymentResources(context.Background(), "x", ""); err == nil {
			t.Error("expected error for empty deployment")
		}
	})

	t.Run("returns owned resources matching the selector", func(t *testing.T) {
		project := "my-project"
		deployment := "web-app"
		namespace := "prj-my-project"

		owned := makeOwnedUnstructured("apps/v1", "Deployment", namespace, deployment, project, deployment,
			map[string]string{v1alpha2.AnnotationExternalLinkPrefix + "logs": `{"url":"https://logs.example.com","title":"Logs"}`})
		ownedSvc := makeOwnedUnstructured("v1", "Service", namespace, deployment, project, deployment,
			map[string]string{v1alpha2.AnnotationArgoCDLinkPrefix + "grafana": "https://grafana.example.com"})
		// Same labels but different deployment — must not appear.
		other := makeOwnedUnstructured("apps/v1", "Deployment", namespace, "other", project, "other-deployment", nil)

		dyn := dynamicfake.NewSimpleDynamicClient(fakeDynamicSchemeForK8sTest(), owned, ownedSvc, other)
		k8s := NewK8sClient(fake.NewClientset(), testResolver()).WithDynamicClient(dyn)

		got, err := k8s.ListDeploymentResources(context.Background(), project, deployment)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Two resources match (Deployment + Service); the "other"
		// deployment must be filtered out by the label selector.
		if len(got) != 2 {
			t.Fatalf("expected 2 owned resources, got %d: %+v", len(got), got)
		}
		// Verify both kinds are represented.
		kinds := map[string]bool{}
		for _, r := range got {
			kinds[r.GetKind()] = true
		}
		if !kinds["Deployment"] || !kinds["Service"] {
			t.Errorf("expected both Deployment and Service, got kinds %v", kinds)
		}
	})

	t.Run("returns empty when no resources match", func(t *testing.T) {
		dyn := dynamicfake.NewSimpleDynamicClient(fakeDynamicSchemeForK8sTest())
		k8s := NewK8sClient(fake.NewClientset(), testResolver()).WithDynamicClient(dyn)
		got, err := k8s.ListDeploymentResources(context.Background(), "p", "d")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected 0 resources, got %d", len(got))
		}
	})

	t.Run("uses the ownership label selector with project+deployment", func(t *testing.T) {
		// Stamp the deployment label but a different project — must not
		// surface (HOL-571 disjoint-selector semantics).
		other := makeOwnedUnstructured("apps/v1", "Deployment", "prj-other", "web-app", "other-project", "web-app", nil)
		dyn := dynamicfake.NewSimpleDynamicClient(fakeDynamicSchemeForK8sTest(), other)
		k8s := NewK8sClient(fake.NewClientset(), testResolver()).WithDynamicClient(dyn)
		got, err := k8s.ListDeploymentResources(context.Background(), "my-project", "web-app")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected 0 cross-project matches, got %d", len(got))
		}
	})

	t.Run("scoped to project namespace - cross-namespace resources are not surfaced", func(t *testing.T) {
		// Same project + deployment labels but in a different
		// namespace (e.g. an HTTPRoute landing in istio-ingress).
		// The scan is namespace-scoped to align with the existing
		// console RBAC posture (HOL-574 review round 1 P1) — cross-
		// namespace resources are intentionally not harvested.
		project := "my-project"
		deployment := "web-app"
		inProj := makeOwnedUnstructured("apps/v1", "Deployment", "prj-my-project", deployment, project, deployment, nil)
		crossNS := makeOwnedUnstructured("apps/v1", "Deployment", "istio-ingress", deployment, project, deployment, nil)
		dyn := dynamicfake.NewSimpleDynamicClient(fakeDynamicSchemeForK8sTest(), inProj, crossNS)
		k8s := NewK8sClient(fake.NewClientset(), testResolver()).WithDynamicClient(dyn)
		got, err := k8s.ListDeploymentResources(context.Background(), project, deployment)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected exactly 1 in-namespace resource, got %d (cross-NS leak?): %+v", len(got), got)
		}
		if got[0].GetNamespace() != "prj-my-project" {
			t.Errorf("expected resource in prj-my-project, got %q", got[0].GetNamespace())
		}
	})
}

// TestListDeploymentResources_PartialScan verifies that ListDeployment
// Resources signals a partial scan (via ErrPartialScan) when at least
// one per-kind list call fails. Without this the GetDeployment refresh
// path would treat a transient failure as authoritative drift and wipe
// legitimate cached links (HOL-574 review round 2 P1).
func TestListDeploymentResources_PartialScan(t *testing.T) {
	project := "my-project"
	deployment := "web-app"
	namespace := "prj-my-project"

	owned := makeOwnedUnstructured("apps/v1", "Deployment", namespace, deployment, project, deployment, nil)
	dyn := dynamicfake.NewSimpleDynamicClient(fakeDynamicSchemeForK8sTest(), owned)
	// Make Service list calls fail to simulate a transient API error
	// or RBAC gap on a single resource type. The Deployment list
	// should still succeed, so the returned slice carries the
	// successful items but the error wraps ErrPartialScan.
	dyn.PrependReactor("list", "services", func(_ ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated transient failure")
	})

	k8s := NewK8sClient(fake.NewClientset(), testResolver()).WithDynamicClient(dyn)
	got, err := k8s.ListDeploymentResources(context.Background(), project, deployment)
	if !errors.Is(err, ErrPartialScan) {
		t.Fatalf("expected ErrPartialScan, got %v", err)
	}
	// Successful kinds still surface so the caller may render a
	// partial view if it chooses.
	if len(got) != 1 || got[0].GetKind() != "Deployment" {
		t.Errorf("expected Deployment item to surface despite partial failure, got %+v", got)
	}
}

// TestListDeploymentResources_OptionalCRDsNotPartialScan verifies that
// per-kind NotFound (or NoKindMatch) errors — which the API server
// returns when an optional CRD like HTTPRoute or ReferenceGrant is not
// installed on the cluster — do NOT trigger ErrPartialScan. Treating a
// missing optional CRD as a partial scan would prevent the cache from
// ever being seeded on clusters without Gateway API installed (HOL-574
// review round 4).
func TestListDeploymentResources_OptionalCRDsNotPartialScan(t *testing.T) {
	project := "my-project"
	deployment := "web-app"
	namespace := "prj-my-project"

	owned := makeOwnedUnstructured("apps/v1", "Deployment", namespace, deployment, project, deployment, nil)
	dyn := dynamicfake.NewSimpleDynamicClient(fakeDynamicSchemeForK8sTest(), owned)
	// Simulate a cluster without the Gateway API CRDs — list calls
	// for HTTPRoute / ReferenceGrant return NotFound from the API
	// server. The aggregator must treat this as "kind is absent" not
	// "partial scan".
	dyn.PrependReactor("list", "httproutes", func(_ ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "httproutes"}, "")
	})
	dyn.PrependReactor("list", "referencegrants", func(_ ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "referencegrants"}, "")
	})

	k8s := NewK8sClient(fake.NewClientset(), testResolver()).WithDynamicClient(dyn)
	got, err := k8s.ListDeploymentResources(context.Background(), project, deployment)
	if err != nil {
		t.Fatalf("optional missing CRDs must not surface as ErrPartialScan, got %v", err)
	}
	if len(got) != 1 || got[0].GetKind() != "Deployment" {
		t.Errorf("expected only the Deployment surfaced, got %+v", got)
	}
}

// TestListDeploymentResources_DeterministicOrder verifies that the
// resource slice ordering does not depend on Go's randomized map
// iteration (HOL-574 review round 2 P2). A stable order keeps the
// aggregator's first-wins de-duplication and primary-url promotion
// repeatable so cached values do not flap across requests.
func TestListDeploymentResources_DeterministicOrder(t *testing.T) {
	project := "my-project"
	deployment := "web-app"
	namespace := "prj-my-project"

	// Seed one resource per kind we care about. Stable iteration
	// across allowedKinds means the slice ordering is identical from
	// run to run.
	dep := makeOwnedUnstructured("apps/v1", "Deployment", namespace, deployment, project, deployment, nil)
	svc := makeOwnedUnstructured("v1", "Service", namespace, deployment, project, deployment, nil)
	sa := makeOwnedUnstructured("v1", "ServiceAccount", namespace, deployment, project, deployment, nil)

	dyn := dynamicfake.NewSimpleDynamicClient(fakeDynamicSchemeForK8sTest(), dep, svc, sa)
	k8s := NewK8sClient(fake.NewClientset(), testResolver()).WithDynamicClient(dyn)

	// Run the scan multiple times; the slice ordering must be
	// identical across calls. If allowedKinds were iterated as a map
	// this would flake under -count.
	const runs = 8
	var first []string
	for i := 0; i < runs; i++ {
		got, err := k8s.ListDeploymentResources(context.Background(), project, deployment)
		if err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}
		order := make([]string, 0, len(got))
		for _, r := range got {
			order = append(order, r.GetKind())
		}
		if i == 0 {
			first = order
			continue
		}
		if len(order) != len(first) {
			t.Fatalf("run %d: ordering length differs from run 0", i)
		}
		for j := range order {
			if order[j] != first[j] {
				t.Errorf("run %d: ordering differs at index %d (got %q, want %q from run 0)", i, j, order[j], first[j])
				break
			}
		}
	}
}

// TestHasDynamicClient covers the boolean accessor used by the handler to
// distinguish "no scan possible" (preserve cache) from "scan returned
// nothing" (clear cache). HOL-574 review round 1 P2.
func TestHasDynamicClient(t *testing.T) {
	t.Run("returns false when no dynamic client is configured", func(t *testing.T) {
		k8s := NewK8sClient(fake.NewClientset(), testResolver())
		if k8s.HasDynamicClient() {
			t.Error("expected HasDynamicClient to return false on default constructor")
		}
	})
	t.Run("returns true after WithDynamicClient", func(t *testing.T) {
		dyn := dynamicfake.NewSimpleDynamicClient(fakeDynamicSchemeForK8sTest())
		k8s := NewK8sClient(fake.NewClientset(), testResolver()).WithDynamicClient(dyn)
		if !k8s.HasDynamicClient() {
			t.Error("expected HasDynamicClient to return true after WithDynamicClient")
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
					v1alpha2.LabelResourceType: "deployment",
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
