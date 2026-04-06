package system_templates

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/holos-run/holos-console/console/resolver"
)

func testResolver() *resolver.Resolver {
	return &resolver.Resolver{OrganizationPrefix: "org-", ProjectPrefix: "prj-"}
}

func orgNS(org string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "org-" + org,
			Labels: map[string]string{
				ManagedByLabel:             ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
			},
		},
	}
}

func sysTemplateConfigMap(org, name, displayName, description, cueTemplate string, mandatory bool, gatewayNs string) *corev1.ConfigMap {
	if gatewayNs == "" {
		gatewayNs = DefaultGatewayNamespace
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "org-" + org,
			Labels: map[string]string{
				ManagedByLabel:    ManagedByValue,
				ResourceTypeLabel: ResourceTypeValue,
			},
			Annotations: map[string]string{
				DisplayNameAnnotation: displayName,
				DescriptionAnnotation: description,
				MandatoryAnnotation:   boolToStr(mandatory),
				GatewayNsAnnotation:   gatewayNs,
			},
		},
		Data: map[string]string{
			CueTemplateKey: cueTemplate,
		},
	}
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestListSystemTemplates(t *testing.T) {
	t.Run("returns empty list when no templates exist", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListSystemTemplates(context.Background(), "my-org")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 0 {
			t.Errorf("expected 0 templates, got %d", len(cms))
		}
	})

	t.Run("returns templates with correct label", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "A test template", "package system_template\n", true, "istio-ingress")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListSystemTemplates(context.Background(), "my-org")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 1 {
			t.Fatalf("expected 1 template, got %d", len(cms))
		}
		if cms[0].Name != "ref-grant" {
			t.Errorf("expected name 'ref-grant', got %q", cms[0].Name)
		}
	})
}

func TestGetSystemTemplate(t *testing.T) {
	t.Run("returns existing template", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "A test template", "package system_template\n", true, "istio-ingress")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		result, err := k8s.GetSystemTemplate(context.Background(), "my-org", "ref-grant")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result.Name != "ref-grant" {
			t.Errorf("expected name 'ref-grant', got %q", result.Name)
		}
		if result.Data[CueTemplateKey] != "package system_template\n" {
			t.Errorf("expected cue template content, got %q", result.Data[CueTemplateKey])
		}
	})

	t.Run("returns error for nonexistent template", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		_, err := k8s.GetSystemTemplate(context.Background(), "my-org", "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestCreateSystemTemplate(t *testing.T) {
	t.Run("creates template with mandatory flag and gateway namespace", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateSystemTemplate(context.Background(), "my-org", "ref-grant", "ReferenceGrant", "A test template", "package system_template\n", true, "istio-ingress")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Labels[ManagedByLabel] != ManagedByValue {
			t.Error("expected managed-by label")
		}
		if cm.Labels[ResourceTypeLabel] != ResourceTypeValue {
			t.Error("expected resource-type label")
		}
		if cm.Annotations[DisplayNameAnnotation] != "ReferenceGrant" {
			t.Errorf("expected display name 'ReferenceGrant', got %q", cm.Annotations[DisplayNameAnnotation])
		}
		if cm.Annotations[MandatoryAnnotation] != "true" {
			t.Errorf("expected mandatory annotation 'true', got %q", cm.Annotations[MandatoryAnnotation])
		}
		if cm.Annotations[GatewayNsAnnotation] != "istio-ingress" {
			t.Errorf("expected gateway-namespace 'istio-ingress', got %q", cm.Annotations[GatewayNsAnnotation])
		}
		if cm.Data[CueTemplateKey] != "package system_template\n" {
			t.Errorf("expected cue template content, got %q", cm.Data[CueTemplateKey])
		}
	})

	t.Run("defaults gateway namespace to istio-ingress when empty", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateSystemTemplate(context.Background(), "my-org", "ref-grant", "ReferenceGrant", "A test template", "package system_template\n", false, "")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Annotations[GatewayNsAnnotation] != DefaultGatewayNamespace {
			t.Errorf("expected default gateway namespace %q, got %q", DefaultGatewayNamespace, cm.Annotations[GatewayNsAnnotation])
		}
	})

	t.Run("stores in org namespace not project namespace", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		_, err := k8s.CreateSystemTemplate(context.Background(), "my-org", "ref-grant", "ReferenceGrant", "desc", "package system_template\n", true, "istio-ingress")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		got, err := fakeClient.CoreV1().ConfigMaps("org-my-org").Get(context.Background(), "ref-grant", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected ConfigMap in org namespace, got %v", err)
		}
		if got.Namespace != "org-my-org" {
			t.Errorf("expected namespace 'org-my-org', got %q", got.Namespace)
		}
	})
}

func TestUpdateSystemTemplate(t *testing.T) {
	t.Run("updates mandatory flag", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "package system_template\n", false, "istio-ingress")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		mandatory := true
		updated, err := k8s.UpdateSystemTemplate(context.Background(), "my-org", "ref-grant", nil, nil, nil, &mandatory, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Annotations[MandatoryAnnotation] != "true" {
			t.Errorf("expected mandatory annotation 'true', got %q", updated.Annotations[MandatoryAnnotation])
		}
	})

	t.Run("updates gateway namespace", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "package system_template\n", true, "istio-ingress")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		newGatewayNs := "my-gateway"
		updated, err := k8s.UpdateSystemTemplate(context.Background(), "my-org", "ref-grant", nil, nil, nil, nil, &newGatewayNs)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Annotations[GatewayNsAnnotation] != "my-gateway" {
			t.Errorf("expected gateway-namespace 'my-gateway', got %q", updated.Annotations[GatewayNsAnnotation])
		}
	})

	t.Run("returns error for nonexistent template", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		newName := "Updated"
		_, err := k8s.UpdateSystemTemplate(context.Background(), "my-org", "nonexistent", &newName, nil, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestDeleteSystemTemplate(t *testing.T) {
	t.Run("deletes existing template", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "package system_template\n", true, "istio-ingress")
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.DeleteSystemTemplate(context.Background(), "my-org", "ref-grant")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		_, err = fakeClient.CoreV1().ConfigMaps("org-my-org").Get(context.Background(), "ref-grant", metav1.GetOptions{})
		if err == nil {
			t.Fatal("expected ConfigMap to be deleted")
		}
	})

	t.Run("returns error for nonexistent template", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.DeleteSystemTemplate(context.Background(), "my-org", "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestConfigMapToSystemTemplate(t *testing.T) {
	t.Run("reads mandatory flag correctly", func(t *testing.T) {
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "package system_template\n", true, "istio-ingress")
		tmpl := configMapToSystemTemplate(cm, "my-org")
		if !tmpl.Mandatory {
			t.Error("expected mandatory=true")
		}
		if tmpl.GatewayNamespace != "istio-ingress" {
			t.Errorf("expected gateway_namespace 'istio-ingress', got %q", tmpl.GatewayNamespace)
		}
		if tmpl.Org != "my-org" {
			t.Errorf("expected org 'my-org', got %q", tmpl.Org)
		}
	})

	t.Run("defaults gateway namespace when annotation is empty", func(t *testing.T) {
		cm := sysTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "package system_template\n", false, "")
		// Explicitly remove the annotation to simulate missing annotation.
		delete(cm.Annotations, GatewayNsAnnotation)
		tmpl := configMapToSystemTemplate(cm, "my-org")
		if tmpl.GatewayNamespace != DefaultGatewayNamespace {
			t.Errorf("expected default gateway namespace %q, got %q", DefaultGatewayNamespace, tmpl.GatewayNamespace)
		}
	})
}
