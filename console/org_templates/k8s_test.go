package org_templates

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
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
				v1alpha1.LabelManagedBy:    v1alpha1.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
			},
		},
	}
}

func orgTemplateConfigMap(org, name, displayName, description, cueTemplate string, mandatory, enabled bool) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "org-" + org,
			Labels: map[string]string{
				v1alpha1.LabelManagedBy:    v1alpha1.ManagedByValue,
				v1alpha1.LabelResourceType: v1alpha1.ResourceTypeOrgTemplate,
			},
			Annotations: map[string]string{
				v1alpha1.AnnotationDisplayName: displayName,
				v1alpha1.AnnotationDescription: description,
				v1alpha1.AnnotationMandatory:   boolToStr(mandatory),
				v1alpha1.AnnotationEnabled:     boolToStr(enabled),
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

func TestListOrgTemplates(t *testing.T) {
	t.Run("returns empty list when no templates exist", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListOrgTemplates(context.Background(), "my-org")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(cms) != 0 {
			t.Errorf("expected 0 templates, got %d", len(cms))
		}
	})

	t.Run("returns templates with correct label", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := orgTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "A test template", "#Input: {}\n", true, false)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		cms, err := k8s.ListOrgTemplates(context.Background(), "my-org")
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

func TestGetOrgTemplate(t *testing.T) {
	t.Run("returns existing template", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := orgTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "A test template", "#Input: {}\n", true, false)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		result, err := k8s.GetOrgTemplate(context.Background(), "my-org", "ref-grant")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result.Name != "ref-grant" {
			t.Errorf("expected name 'ref-grant', got %q", result.Name)
		}
		if result.Data[CueTemplateKey] != "#Input: {}\n" {
			t.Errorf("expected cue template content, got %q", result.Data[CueTemplateKey])
		}
	})

	t.Run("returns error for nonexistent template", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		_, err := k8s.GetOrgTemplate(context.Background(), "my-org", "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestCreateOrgTemplate(t *testing.T) {
	t.Run("creates template with mandatory flag and enabled flag", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateOrgTemplate(context.Background(), "my-org", "ref-grant", "ReferenceGrant", "A test template", "#Input: {}\n", true, true)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Labels[v1alpha1.LabelManagedBy] != v1alpha1.ManagedByValue {
			t.Error("expected managed-by label")
		}
		if cm.Labels[v1alpha1.LabelResourceType] != v1alpha1.ResourceTypeOrgTemplate {
			t.Error("expected resource-type label")
		}
		if cm.Annotations[v1alpha1.AnnotationDisplayName] != "ReferenceGrant" {
			t.Errorf("expected display name 'ReferenceGrant', got %q", cm.Annotations[v1alpha1.AnnotationDisplayName])
		}
		if cm.Annotations[v1alpha1.AnnotationMandatory] != "true" {
			t.Errorf("expected mandatory annotation 'true', got %q", cm.Annotations[v1alpha1.AnnotationMandatory])
		}
		if cm.Annotations[v1alpha1.AnnotationEnabled] != "true" {
			t.Errorf("expected enabled annotation 'true', got %q", cm.Annotations[v1alpha1.AnnotationEnabled])
		}
		if cm.Data[CueTemplateKey] != "#Input: {}\n" {
			t.Errorf("expected cue template content, got %q", cm.Data[CueTemplateKey])
		}
	})

	t.Run("new templates default enabled to false", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		cm, err := k8s.CreateOrgTemplate(context.Background(), "my-org", "ref-grant", "ReferenceGrant", "A test template", "#Input: {}\n", false, false)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cm.Annotations[v1alpha1.AnnotationEnabled] != "false" {
			t.Errorf("expected enabled annotation 'false', got %q", cm.Annotations[v1alpha1.AnnotationEnabled])
		}
	})

	t.Run("stores in org namespace not project namespace", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		_, err := k8s.CreateOrgTemplate(context.Background(), "my-org", "ref-grant", "ReferenceGrant", "desc", "#Input: {}\n", true, false)
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

func TestUpdateOrgTemplate(t *testing.T) {
	t.Run("updates mandatory flag", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := orgTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "#Input: {}\n", false, false)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		mandatory := true
		updated, err := k8s.UpdateOrgTemplate(context.Background(), "my-org", "ref-grant", nil, nil, nil, &mandatory, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Annotations[v1alpha1.AnnotationMandatory] != "true" {
			t.Errorf("expected mandatory annotation 'true', got %q", updated.Annotations[v1alpha1.AnnotationMandatory])
		}
	})

	t.Run("updates enabled flag", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := orgTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "#Input: {}\n", true, false)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		enabled := true
		updated, err := k8s.UpdateOrgTemplate(context.Background(), "my-org", "ref-grant", nil, nil, nil, nil, &enabled)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.Annotations[v1alpha1.AnnotationEnabled] != "true" {
			t.Errorf("expected enabled annotation 'true', got %q", updated.Annotations[v1alpha1.AnnotationEnabled])
		}
	})

	t.Run("returns error for nonexistent template", func(t *testing.T) {
		ns := orgNS("my-org")
		fakeClient := fake.NewClientset(ns)
		k8s := NewK8sClient(fakeClient, testResolver())

		newName := "Updated"
		_, err := k8s.UpdateOrgTemplate(context.Background(), "my-org", "nonexistent", &newName, nil, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestDeleteOrgTemplate(t *testing.T) {
	t.Run("deletes existing template", func(t *testing.T) {
		ns := orgNS("my-org")
		cm := orgTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "#Input: {}\n", true, false)
		fakeClient := fake.NewClientset(ns, cm)
		k8s := NewK8sClient(fakeClient, testResolver())

		err := k8s.DeleteOrgTemplate(context.Background(), "my-org", "ref-grant")
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

		err := k8s.DeleteOrgTemplate(context.Background(), "my-org", "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent template")
		}
	})
}

func TestListOrgTemplateSourcesForRender(t *testing.T) {
	// Template combinations for the test matrix (name, mandatory, enabled, src).
	type tmplSpec struct {
		name      string
		mandatory bool
		enabled   bool
		src       string
	}

	// Helper: build a fake K8sClient with the given templates.
	setup := func(specs []tmplSpec) *K8sClient {
		ns := orgNS("my-org")
		objects := []runtime.Object{ns}
		for _, s := range specs {
			objects = append(objects, orgTemplateConfigMap("my-org", s.name, s.name, "", s.src, s.mandatory, s.enabled))
		}
		fakeClient := fake.NewClientset(objects...)
		return NewK8sClient(fakeClient, testResolver())
	}

	t.Run("mandatory+enabled template always included without linking", func(t *testing.T) {
		k8s := setup([]tmplSpec{
			{name: "policy", mandatory: true, enabled: true, src: "// policy"},
		})
		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(sources))
		}
		if sources[0] != "// policy" {
			t.Errorf("unexpected source: %q", sources[0])
		}
	})

	t.Run("non-mandatory enabled template NOT included when not linked", func(t *testing.T) {
		k8s := setup([]tmplSpec{
			{name: "archetype", mandatory: false, enabled: true, src: "// archetype"},
		})
		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected 0 sources (non-mandatory not linked), got %d: %v", len(sources), sources)
		}
	})

	t.Run("non-mandatory enabled template included when explicitly linked", func(t *testing.T) {
		k8s := setup([]tmplSpec{
			{name: "archetype", mandatory: false, enabled: true, src: "// archetype"},
		})
		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", []string{"archetype"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source (explicitly linked), got %d", len(sources))
		}
	})

	t.Run("linked+mandatory deduplicates — template appears once", func(t *testing.T) {
		k8s := setup([]tmplSpec{
			{name: "policy", mandatory: true, enabled: true, src: "// policy"},
		})
		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", []string{"policy"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source (deduplication), got %d", len(sources))
		}
	})

	t.Run("disabled template not included even when linked", func(t *testing.T) {
		k8s := setup([]tmplSpec{
			{name: "disabled", mandatory: false, enabled: false, src: "// disabled"},
		})
		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", []string{"disabled"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected 0 sources (disabled cannot be linked), got %d", len(sources))
		}
	})

	t.Run("mandatory disabled template not included", func(t *testing.T) {
		k8s := setup([]tmplSpec{
			{name: "disabled-mandatory", mandatory: true, enabled: false, src: "// disabled"},
		})
		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected 0 sources (mandatory disabled), got %d", len(sources))
		}
	})

	t.Run("mixed: mandatory+enabled + linked non-mandatory + unlinked non-mandatory", func(t *testing.T) {
		k8s := setup([]tmplSpec{
			{name: "policy", mandatory: true, enabled: true, src: "// policy"},
			{name: "archetype-a", mandatory: false, enabled: true, src: "// archetype-a"},
			{name: "archetype-b", mandatory: false, enabled: true, src: "// archetype-b"},
		})
		// Link only archetype-a; archetype-b should NOT be included.
		sources, err := k8s.ListOrgTemplateSourcesForRender(context.Background(), "my-org", []string{"archetype-a"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 2 {
			t.Fatalf("expected 2 sources (policy + archetype-a), got %d: %v", len(sources), sources)
		}
	})
}

func TestConfigMapToOrgTemplate(t *testing.T) {
	t.Run("reads mandatory and enabled flags correctly", func(t *testing.T) {
		cm := orgTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "#Input: {}\n", true, true)
		tmpl := configMapToOrgTemplate(cm, "my-org")
		if !tmpl.Mandatory {
			t.Error("expected mandatory=true")
		}
		if !tmpl.Enabled {
			t.Error("expected enabled=true")
		}
		if tmpl.Org != "my-org" {
			t.Errorf("expected org 'my-org', got %q", tmpl.Org)
		}
	})

	t.Run("reads enabled=false when annotation is false", func(t *testing.T) {
		cm := orgTemplateConfigMap("my-org", "ref-grant", "ReferenceGrant", "desc", "#Input: {}\n", false, false)
		tmpl := configMapToOrgTemplate(cm, "my-org")
		if tmpl.Enabled {
			t.Error("expected enabled=false")
		}
	})
}
