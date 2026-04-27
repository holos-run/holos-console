package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/secretrbac"
)

// projectNamespaceFixture builds a project namespace seeded with the
// given annotations and labels.
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

// orgNamespaceFixture builds an org namespace (which the migration
// should skip).
func orgNamespaceFixture(name string, annotations map[string]string) *corev1.Namespace {
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

// withProjectRoles seeds the cluster with the three project-secret
// Roles for the given namespace so the migration tool's preflight
// passes.
func withProjectRoles(namespace string, extras ...runtime.Object) []runtime.Object {
	objs := append([]runtime.Object(nil), extras...)
	for _, role := range secretrbac.ProjectSecretRoles(namespace, nil) {
		objs = append(objs, role)
	}
	return objs
}

func TestMigrate_NamespaceLevelGrants_DryRun(t *testing.T) {
	ns := projectNamespaceFixture("holos-prj-finance", map[string]string{
		v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"viewer"}]`,
		v1alpha2.AnnotationShareRoles: `[{"principal":"team-finance","role":"editor"}]`,
	})
	objs := withProjectRoles(ns.Name, ns)
	client := fake.NewClientset(objs...)

	report, err := Migrate(context.Background(), client, false)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	if len(report.Namespaces) != 1 {
		t.Fatalf("expected 1 namespace report, got %d", len(report.Namespaces))
	}
	nr := report.Namespaces[0]
	if nr.Error != "" {
		t.Errorf("unexpected error: %s", nr.Error)
	}
	if len(nr.BindingsCreated) != 2 {
		t.Errorf("expected 2 bindings (one user + one group), got %d (%v)", len(nr.BindingsCreated), nr.BindingsCreated)
	}
	// Dry-run: verify no actual bindings were written.
	bindings, _ := client.RbacV1().RoleBindings(ns.Name).List(context.Background(), metav1.ListOptions{})
	if len(bindings.Items) != 0 {
		t.Errorf("dry-run wrote %d bindings, expected 0", len(bindings.Items))
	}
	// Dry-run: namespace annotations remain.
	live, _ := client.CoreV1().Namespaces().Get(context.Background(), ns.Name, metav1.GetOptions{})
	if _, ok := live.Annotations[v1alpha2.AnnotationShareUsers]; !ok {
		t.Errorf("dry-run stripped share-users annotation")
	}
}

func TestMigrate_NamespaceLevelGrants_Apply(t *testing.T) {
	ns := projectNamespaceFixture("holos-prj-finance", map[string]string{
		v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"viewer"}]`,
		v1alpha2.AnnotationShareRoles: `[{"principal":"team-finance","role":"editor"}]`,
	})
	objs := withProjectRoles(ns.Name, ns)
	client := fake.NewClientset(objs...)

	report, err := Migrate(context.Background(), client, true)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	nr := report.Namespaces[0]
	if nr.Error != "" {
		t.Fatalf("unexpected error: %s", nr.Error)
	}
	bindings, _ := client.RbacV1().RoleBindings(ns.Name).List(context.Background(), metav1.ListOptions{})
	if len(bindings.Items) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(bindings.Items))
	}

	// Verify Subject + RoleRef shape.
	for _, b := range bindings.Items {
		switch b.Subjects[0].Kind {
		case rbacv1.UserKind:
			if b.Subjects[0].Name != "oidc:alice@example.com" {
				t.Errorf("unexpected subject name: %q", b.Subjects[0].Name)
			}
			if b.RoleRef.Name != "holos-project-secrets-viewer" {
				t.Errorf("unexpected RoleRef.Name: %q", b.RoleRef.Name)
			}
		case rbacv1.GroupKind:
			if b.Subjects[0].Name != "oidc:team-finance" {
				t.Errorf("unexpected subject name: %q", b.Subjects[0].Name)
			}
			if b.RoleRef.Name != "holos-project-secrets-editor" {
				t.Errorf("unexpected RoleRef.Name: %q", b.RoleRef.Name)
			}
		default:
			t.Errorf("unexpected subject kind: %q", b.Subjects[0].Kind)
		}
	}

	// Annotations stripped.
	live, _ := client.CoreV1().Namespaces().Get(context.Background(), ns.Name, metav1.GetOptions{})
	if _, ok := live.Annotations[v1alpha2.AnnotationShareUsers]; ok {
		t.Errorf("share-users annotation still present after apply")
	}
	if _, ok := live.Annotations[v1alpha2.AnnotationShareRoles]; ok {
		t.Errorf("share-roles annotation still present after apply")
	}
}

func TestMigrate_AlreadyMigrated_NoOp(t *testing.T) {
	ns := projectNamespaceFixture("holos-prj-finance", nil)
	// Pre-existing binding identical to what the migration would
	// produce.
	desired := secretrbac.RoleBinding(ns.Name, secretrbac.ShareTargetUser, "alice@example.com", "viewer", nil)
	objs := withProjectRoles(ns.Name, ns, desired)
	client := fake.NewClientset(objs...)

	first, err := Migrate(context.Background(), client, true)
	if err != nil {
		t.Fatalf("first Migrate returned error: %v", err)
	}
	if got := len(first.Namespaces[0].BindingsCreated); got != 0 {
		t.Errorf("expected 0 bindings created on no-op, got %d", got)
	}

	// Re-run: still a no-op.
	second, err := Migrate(context.Background(), client, true)
	if err != nil {
		t.Fatalf("second Migrate returned error: %v", err)
	}
	if got := len(second.Namespaces[0].BindingsCreated); got != 0 {
		t.Errorf("expected 0 bindings created on second run, got %d", got)
	}
}

func TestMigrate_PerSecretGrants_Apply(t *testing.T) {
	ns := projectNamespaceFixture("holos-prj-finance", nil)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db-creds",
			Namespace: ns.Name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationShareUsers: `[{"principal":"bob@example.com","role":"editor"}]`,
			},
		},
	}
	objs := withProjectRoles(ns.Name, ns, secret)
	client := fake.NewClientset(objs...)

	report, err := Migrate(context.Background(), client, true)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	nr := report.Namespaces[0]
	if nr.Error != "" {
		t.Fatalf("unexpected error: %s", nr.Error)
	}
	bindings, _ := client.RbacV1().RoleBindings(ns.Name).List(context.Background(), metav1.ListOptions{})
	if len(bindings.Items) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(bindings.Items))
	}
	live, _ := client.CoreV1().Secrets(ns.Name).Get(context.Background(), secret.Name, metav1.GetOptions{})
	if _, ok := live.Annotations[v1alpha2.AnnotationShareUsers]; ok {
		t.Errorf("share-users still present on Secret after apply")
	}
}

func TestMigrate_MalformedAnnotation_NamespaceLevel(t *testing.T) {
	ns := projectNamespaceFixture("holos-prj-finance", map[string]string{
		v1alpha2.AnnotationShareUsers: "not-json",
	})
	objs := withProjectRoles(ns.Name, ns)
	client := fake.NewClientset(objs...)

	report, err := Migrate(context.Background(), client, true)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	nr := report.Namespaces[0]
	if nr.Error == "" {
		t.Fatal("expected per-namespace Error for malformed annotation, got none")
	}
	// No bindings written.
	bindings, _ := client.RbacV1().RoleBindings(ns.Name).List(context.Background(), metav1.ListOptions{})
	if len(bindings.Items) != 0 {
		t.Errorf("expected no bindings written, got %d", len(bindings.Items))
	}
	// Annotation still present.
	live, _ := client.CoreV1().Namespaces().Get(context.Background(), ns.Name, metav1.GetOptions{})
	if _, ok := live.Annotations[v1alpha2.AnnotationShareUsers]; !ok {
		t.Error("malformed annotation must remain in place for hand-fix")
	}
	if !containsAny(nr.Warnings, "MALFORMED") {
		t.Errorf("expected warning to mention MALFORMED; got %v", nr.Warnings)
	}
}

func TestMigrate_MalformedAnnotation_PerSecretSkipsSecretButNotNamespace(t *testing.T) {
	ns := projectNamespaceFixture("holos-prj-finance", map[string]string{
		v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"viewer"}]`,
	})
	bad := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bad",
			Namespace: ns.Name,
			Labels:    map[string]string{v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue},
			Annotations: map[string]string{
				v1alpha2.AnnotationShareUsers: "not-json",
			},
		},
	}
	objs := withProjectRoles(ns.Name, ns, bad)
	client := fake.NewClientset(objs...)

	report, err := Migrate(context.Background(), client, true)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	nr := report.Namespaces[0]
	// Namespace-level grant should still produce its binding.
	bindings, _ := client.RbacV1().RoleBindings(ns.Name).List(context.Background(), metav1.ListOptions{})
	if len(bindings.Items) != 1 {
		t.Errorf("expected 1 binding from the namespace grant, got %d", len(bindings.Items))
	}
	// Bad Secret must keep its annotation.
	live, _ := client.CoreV1().Secrets(ns.Name).Get(context.Background(), bad.Name, metav1.GetOptions{})
	if _, ok := live.Annotations[v1alpha2.AnnotationShareUsers]; !ok {
		t.Error("malformed Secret annotation must remain in place for hand-fix")
	}
	if !containsAny(nr.Warnings, "MALFORMED") {
		t.Errorf("expected MALFORMED warning, got %v", nr.Warnings)
	}
}

func TestMigrate_TimeBoundedGrants_AreDroppedWithWarning(t *testing.T) {
	ns := projectNamespaceFixture("holos-prj-finance", map[string]string{
		v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"viewer","exp":1700000000}]`,
	})
	objs := withProjectRoles(ns.Name, ns)
	client := fake.NewClientset(objs...)

	report, err := Migrate(context.Background(), client, true)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	nr := report.Namespaces[0]
	bindings, _ := client.RbacV1().RoleBindings(ns.Name).List(context.Background(), metav1.ListOptions{})
	if len(bindings.Items) != 0 {
		t.Errorf("time-bounded grant must not produce a binding; got %d", len(bindings.Items))
	}
	if !containsAny(nr.Warnings, "DROPPING time-bounded grant") {
		t.Errorf("expected DROPPING time-bounded grant warning; got %v", nr.Warnings)
	}
}

func TestMigrate_RolesMissing_FailsLoudly(t *testing.T) {
	ns := projectNamespaceFixture("holos-prj-finance", map[string]string{
		v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"viewer"}]`,
	})
	client := fake.NewClientset(ns)

	report, err := Migrate(context.Background(), client, true)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	nr := report.Namespaces[0]
	if !strings.Contains(nr.Error, "project secret Roles missing") {
		t.Errorf("expected missing-roles error; got %q", nr.Error)
	}
	bindings, _ := client.RbacV1().RoleBindings(ns.Name).List(context.Background(), metav1.ListOptions{})
	if len(bindings.Items) != 0 {
		t.Errorf("must not write any binding when Roles are missing; got %d", len(bindings.Items))
	}
	live, _ := client.CoreV1().Namespaces().Get(context.Background(), ns.Name, metav1.GetOptions{})
	if _, ok := live.Annotations[v1alpha2.AnnotationShareUsers]; !ok {
		t.Error("must not strip annotations when Roles are missing")
	}
}

func TestMigrate_NonProjectNamespacesAreSkipped(t *testing.T) {
	org := orgNamespaceFixture("holos-org-acme", map[string]string{
		v1alpha2.AnnotationDefaultShareUsers: `[{"principal":"a","role":"viewer"}]`,
	})
	client := fake.NewClientset(org)

	report, err := Migrate(context.Background(), client, true)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	nr := report.Namespaces[0]
	if nr.Error != "" {
		t.Errorf("expected no error on org namespace skip, got %q", nr.Error)
	}
	if len(nr.BindingsCreated) != 0 {
		t.Errorf("expected 0 bindings on org namespace, got %d", len(nr.BindingsCreated))
	}
	// default-share-* must remain so the cascade chain keeps working.
	live, _ := client.CoreV1().Namespaces().Get(context.Background(), org.Name, metav1.GetOptions{})
	if _, ok := live.Annotations[v1alpha2.AnnotationDefaultShareUsers]; !ok {
		t.Error("default-share-users must be preserved on org namespace")
	}
}

func TestMigrate_DefaultShareAnnotations_AreNotStripped(t *testing.T) {
	// On a project namespace, default-share-* annotations seed new
	// resources but have no equivalent RoleBinding. They must survive
	// the migration so the cascade chain keeps producing the right
	// grants on subsequent project creation.
	ns := projectNamespaceFixture("holos-prj-finance", map[string]string{
		v1alpha2.AnnotationShareUsers:        `[{"principal":"alice@example.com","role":"viewer"}]`,
		v1alpha2.AnnotationDefaultShareUsers: `[{"principal":"a","role":"viewer"}]`,
	})
	objs := withProjectRoles(ns.Name, ns)
	client := fake.NewClientset(objs...)

	if _, err := Migrate(context.Background(), client, true); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	live, _ := client.CoreV1().Namespaces().Get(context.Background(), ns.Name, metav1.GetOptions{})
	if _, ok := live.Annotations[v1alpha2.AnnotationDefaultShareUsers]; !ok {
		t.Error("default-share-users must be preserved on project namespace")
	}
	if _, ok := live.Annotations[v1alpha2.AnnotationShareUsers]; ok {
		t.Error("share-users must be stripped after migration")
	}
}

func TestMigrate_DeduplicatesAcrossNamespaceAndSecret(t *testing.T) {
	// alice gets viewer at the namespace level and editor at the
	// per-Secret level. Different roles produce different binding
	// names, so there will be one viewer + one editor.
	ns := projectNamespaceFixture("holos-prj-finance", map[string]string{
		v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"viewer"}]`,
	})
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db-creds",
			Namespace: ns.Name,
			Labels:    map[string]string{v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue},
			Annotations: map[string]string{
				v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"editor"}]`,
			},
		},
	}
	objs := withProjectRoles(ns.Name, ns, secret)
	client := fake.NewClientset(objs...)

	if _, err := Migrate(context.Background(), client, true); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	bindings, _ := client.RbacV1().RoleBindings(ns.Name).List(context.Background(), metav1.ListOptions{})
	if len(bindings.Items) != 2 {
		t.Fatalf("expected 2 bindings (one viewer, one editor for alice), got %d", len(bindings.Items))
	}
}

func TestPrintReport_ApplyAndDryRunHeaders(t *testing.T) {
	report := &Report{
		Namespaces: []NamespaceReport{
			{Namespace: "holos-prj-a", BindingsCreated: []string{"b1"}},
			{Namespace: "holos-prj-b", Error: "boom"},
		},
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
	if !strings.Contains(applied.String(), "ERROR holos-prj-b: boom") {
		t.Errorf("error detail missing: %s", applied.String())
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

// --- helpers ---

// containsAny reports whether any entry in haystack contains needle as
// a substring.
func containsAny(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
