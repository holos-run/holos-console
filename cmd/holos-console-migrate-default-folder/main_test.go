package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

func organizationNamespaceFixture(name string, annotations map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
			},
			Annotations: annotations,
		},
	}
}

func projectNamespaceFixture(name string, annotations map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
			},
			Annotations: annotations,
		},
	}
}

func TestMigrate_DryRunReportsDefaultFolderWithoutStripping(t *testing.T) {
	withAnnotation := organizationNamespaceFixture("holos-org-acme", map[string]string{
		removedAnnotationKey:           "holos-fld-acme-default",
		v1alpha2.AnnotationDisplayName: "Acme",
	})
	withoutAnnotation := organizationNamespaceFixture("holos-org-beta", nil)
	client := fake.NewClientset(withAnnotation, withoutAnnotation)

	report, err := Migrate(context.Background(), client, false)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	if len(report.Namespaces) != 2 {
		t.Fatalf("expected 2 namespace reports, got %d", len(report.Namespaces))
	}
	first := report.Namespaces[0]
	if first.Namespace != withAnnotation.Name {
		t.Fatalf("expected sorted first namespace %q, got %q", withAnnotation.Name, first.Namespace)
	}
	if !first.AnnotationFound || !first.AnnotationRemoved {
		t.Fatalf("expected dry-run to report planned strip, got %+v", first)
	}
	live, _ := client.CoreV1().Namespaces().Get(context.Background(), withAnnotation.Name, metav1.GetOptions{})
	if _, ok := live.Annotations[removedAnnotationKey]; !ok {
		t.Fatal("dry-run stripped default-folder annotation")
	}
}

func TestMigrate_ApplyStripsDefaultFolderFromOrganizationNamespacesOnly(t *testing.T) {
	org := organizationNamespaceFixture("holos-org-acme", map[string]string{
		removedAnnotationKey:           "holos-fld-acme-default",
		v1alpha2.AnnotationDisplayName: "Acme",
	})
	project := projectNamespaceFixture("holos-prj-acme-app", map[string]string{
		removedAnnotationKey: "stale-but-not-an-org",
	})
	client := fake.NewClientset(org, project)

	report, err := Migrate(context.Background(), client, true)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	if len(report.Namespaces) != 1 {
		t.Fatalf("expected only organization namespace report, got %d", len(report.Namespaces))
	}
	if !report.Namespaces[0].AnnotationRemoved {
		t.Fatalf("expected organization annotation strip, got %+v", report.Namespaces[0])
	}
	liveOrg, _ := client.CoreV1().Namespaces().Get(context.Background(), org.Name, metav1.GetOptions{})
	if _, ok := liveOrg.Annotations[removedAnnotationKey]; ok {
		t.Fatal("default-folder annotation still present on organization namespace")
	}
	if got := liveOrg.Annotations[v1alpha2.AnnotationDisplayName]; got != "Acme" {
		t.Fatalf("non-target annotation changed: %q", got)
	}
	liveProject, _ := client.CoreV1().Namespaces().Get(context.Background(), project.Name, metav1.GetOptions{})
	if _, ok := liveProject.Annotations[removedAnnotationKey]; !ok {
		t.Fatal("project namespace should not be part of the organization cleanup")
	}
}

func TestMigrate_ApplyIsIdempotent(t *testing.T) {
	org := organizationNamespaceFixture("holos-org-acme", map[string]string{
		removedAnnotationKey: "holos-fld-acme-default",
	})
	client := fake.NewClientset(org)

	if _, err := Migrate(context.Background(), client, true); err != nil {
		t.Fatalf("first Migrate returned error: %v", err)
	}
	second, err := Migrate(context.Background(), client, true)
	if err != nil {
		t.Fatalf("second Migrate returned error: %v", err)
	}
	if second.Namespaces[0].AnnotationFound {
		t.Fatalf("expected second run to find no annotation, got %+v", second.Namespaces[0])
	}
}

func TestPrintReport_ApplyAndDryRunHeaders(t *testing.T) {
	report := &Report{
		Namespaces: []NamespaceReport{{
			Namespace:         "holos-org-acme",
			AnnotationFound:   true,
			AnnotationRemoved: true,
		}},
	}
	var dry, applied bytes.Buffer
	if err := PrintReport(&dry, report, false); err != nil {
		t.Fatalf("PrintReport(dry): %v", err)
	}
	if err := PrintReport(&applied, report, true); err != nil {
		t.Fatalf("PrintReport(applied): %v", err)
	}
	if !strings.Contains(dry.String(), "DRY-RUN") {
		t.Errorf("dry-run output missing DRY-RUN marker: %s", dry.String())
	}
	if !strings.Contains(applied.String(), "APPLIED") {
		t.Errorf("applied output missing APPLIED marker: %s", applied.String())
	}
}

func TestParseFlags_DefaultsToDryRun(t *testing.T) {
	opts, err := parseFlags(nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if opts.apply {
		t.Errorf("expected --apply default false, got true")
	}
}

func TestParseFlags_ApplyFlag(t *testing.T) {
	opts, err := parseFlags([]string{"--apply"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if !opts.apply {
		t.Errorf("expected --apply true, got false")
	}
}
