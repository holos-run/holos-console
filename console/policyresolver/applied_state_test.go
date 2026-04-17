package policyresolver

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// TestFolderNamespaceForProject verifies the single authoritative helper for
// "where does drift state go?" across the canonical hierarchies. Each case
// seeds its own clientset so the AnnotationParent chains stay independent.
func TestFolderNamespaceForProject(t *testing.T) {
	r := &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
	cases := []struct {
		name      string
		build     func() *fake.Clientset
		project   string
		wantNs    string
		wantError bool
	}{
		{
			name: "project-under-single-folder-lands-in-folder",
			build: func() *fake.Clientset {
				return fake.NewClientset(
					orgNamespace(r, "acme"),
					folderNamespace(r, "eng", r.OrgNamespace("acme")),
					projectNamespace(r, "web", r.FolderNamespace("eng")),
				)
			},
			project: "web",
			wantNs:  r.FolderNamespace("eng"),
		},
		{
			name: "project-nested-under-multiple-folders-picks-nearest-folder",
			build: func() *fake.Clientset {
				return fake.NewClientset(
					orgNamespace(r, "acme"),
					folderNamespace(r, "eng", r.OrgNamespace("acme")),
					folderNamespace(r, "team-a", r.FolderNamespace("eng")),
					projectNamespace(r, "web", r.FolderNamespace("team-a")),
				)
			},
			project: "web",
			// The resolver returns the nearest folder, not the top-most one,
			// so RBAC on that folder naturally covers drift state.
			wantNs: r.FolderNamespace("team-a"),
		},
		{
			name: "project-directly-under-organization-uses-organization",
			build: func() *fake.Clientset {
				return fake.NewClientset(
					orgNamespace(r, "acme"),
					projectNamespace(r, "web", r.OrgNamespace("acme")),
				)
			},
			project: "web",
			wantNs:  r.OrgNamespace("acme"),
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.build()
			walker := &resolver.Walker{Client: client, Resolver: r}
			got, err := FolderNamespaceForProject(context.Background(), walker, r, tt.project)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantNs {
				t.Errorf("want %q, got %q", tt.wantNs, got)
			}
		})
	}
}

// TestRecordAndReadAppliedRenderSet confirms the round-trip contract: a
// write followed by a read yields the same slice, and the ConfigMap lands
// in the folder namespace (never the project namespace).
func TestRecordAndReadAppliedRenderSet(t *testing.T) {
	r := &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
	client := fake.NewClientset(
		orgNamespace(r, "acme"),
		folderNamespace(r, "eng", r.OrgNamespace("acme")),
		projectNamespace(r, "web", r.FolderNamespace("eng")),
	)
	walker := &resolver.Walker{Client: client, Resolver: r}

	refs := []*consolev1.LinkedTemplateRef{
		{
			Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
			ScopeName: "acme",
			Name:      "reference-grant",
		},
		{
			Scope:             consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER,
			ScopeName:         "eng",
			Name:              "istio-gateway",
			VersionConstraint: ">=1.0",
		},
	}

	if err := RecordAppliedRenderSet(context.Background(), client, walker, r, "web", TargetKindDeployment, "app", refs); err != nil {
		t.Fatalf("RecordAppliedRenderSet: %v", err)
	}

	// Drift state MUST be in the folder namespace, not the project namespace.
	projectCMs, _ := client.CoreV1().ConfigMaps(r.ProjectNamespace("web")).List(context.Background(), metav1.ListOptions{})
	for _, cm := range projectCMs.Items {
		if cm.Labels[v1alpha2.LabelResourceType] == v1alpha2.ResourceTypeRenderState {
			t.Fatalf("render-state ConfigMap leaked into project namespace %q", cm.Name)
		}
	}
	folderCMs, _ := client.CoreV1().ConfigMaps(r.FolderNamespace("eng")).List(context.Background(), metav1.ListOptions{})
	var found *corev1.ConfigMap
	for i := range folderCMs.Items {
		if folderCMs.Items[i].Labels[v1alpha2.LabelResourceType] == v1alpha2.ResourceTypeRenderState {
			found = &folderCMs.Items[i]
		}
	}
	if found == nil {
		t.Fatal("expected render-state ConfigMap in folder namespace")
	}
	if found.Annotations[v1alpha2.AnnotationRenderStateProject] != "web" {
		t.Errorf("annotation project=%q, want %q", found.Annotations[v1alpha2.AnnotationRenderStateProject], "web")
	}
	if found.Annotations[v1alpha2.AnnotationRenderStateTarget] != "deployment" {
		t.Errorf("annotation target=%q, want %q", found.Annotations[v1alpha2.AnnotationRenderStateTarget], "deployment")
	}
	if found.Annotations[v1alpha2.AnnotationRenderStateTargetName] != "app" {
		t.Errorf("annotation target-name=%q, want %q", found.Annotations[v1alpha2.AnnotationRenderStateTargetName], "app")
	}

	// Read back and assert round trip.
	round, err := ReadAppliedRenderSet(context.Background(), client, walker, r, "web", TargetKindDeployment, "app")
	if err != nil {
		t.Fatalf("ReadAppliedRenderSet: %v", err)
	}
	if len(round) != len(refs) {
		t.Fatalf("round trip len mismatch: want %d, got %d", len(refs), len(round))
	}
	for i, want := range refs {
		if round[i].GetScope() != want.GetScope() || round[i].GetScopeName() != want.GetScopeName() || round[i].GetName() != want.GetName() {
			t.Errorf("ref[%d] mismatch: want %+v, got %+v", i, want, round[i])
		}
		if round[i].GetVersionConstraint() != want.GetVersionConstraint() {
			t.Errorf("ref[%d] constraint: want %q, got %q", i, want.GetVersionConstraint(), round[i].GetVersionConstraint())
		}
	}
}

// TestReadAppliedRenderSetIgnoresProjectAnnotation exercises the
// storage-isolation invariant on the read path: a stale annotation on the
// project's Deployment ConfigMap MUST NOT influence the resolver's
// assessment of the last-applied set. Only the folder-namespace record
// matters.
func TestReadAppliedRenderSetIgnoresProjectAnnotation(t *testing.T) {
	r := &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
	client := fake.NewClientset(
		orgNamespace(r, "acme"),
		folderNamespace(r, "eng", r.OrgNamespace("acme")),
		projectNamespace(r, "web", r.FolderNamespace("eng")),
		// Plant a rogue annotation on a fake deployment ConfigMap in the
		// project namespace. This is the kind of payload a project owner
		// could write against the pre-HOL-557 design.
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "app",
				Namespace: r.ProjectNamespace("web"),
				Annotations: map[string]string{
					"console.holos.run/last-render-template-set": `[{"scope":"organization","scope_name":"acme","name":"rogue"}]`,
				},
			},
		},
	)
	walker := &resolver.Walker{Client: client, Resolver: r}

	// Nothing has been recorded in the folder namespace yet, so the helper
	// MUST report "no applied set" (nil, nil). The rogue project-namespace
	// annotation is entirely ignored.
	got, err := ReadAppliedRenderSet(context.Background(), client, walker, r, "web", TargetKindDeployment, "app")
	if err != nil {
		t.Fatalf("ReadAppliedRenderSet: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil applied-set, got %d refs: %+v", len(got), got)
	}
}

// TestRecordAppliedRenderSetIdempotent verifies a second write to the same
// tuple replaces the payload in place rather than leaking a second
// ConfigMap.
func TestRecordAppliedRenderSetIdempotent(t *testing.T) {
	r := &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
	client := fake.NewClientset(
		orgNamespace(r, "acme"),
		folderNamespace(r, "eng", r.OrgNamespace("acme")),
		projectNamespace(r, "web", r.FolderNamespace("eng")),
	)
	walker := &resolver.Walker{Client: client, Resolver: r}

	first := []*consolev1.LinkedTemplateRef{{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "a"}}
	second := []*consolev1.LinkedTemplateRef{{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "b"}}
	if err := RecordAppliedRenderSet(context.Background(), client, walker, r, "web", TargetKindDeployment, "app", first); err != nil {
		t.Fatalf("first record: %v", err)
	}
	if err := RecordAppliedRenderSet(context.Background(), client, walker, r, "web", TargetKindDeployment, "app", second); err != nil {
		t.Fatalf("second record: %v", err)
	}
	folderCMs, _ := client.CoreV1().ConfigMaps(r.FolderNamespace("eng")).List(context.Background(), metav1.ListOptions{})
	count := 0
	for _, cm := range folderCMs.Items {
		if cm.Labels[v1alpha2.LabelResourceType] == v1alpha2.ResourceTypeRenderState {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want exactly 1 render-state ConfigMap after rewrite, got %d", count)
	}
	got, err := ReadAppliedRenderSet(context.Background(), client, walker, r, "web", TargetKindDeployment, "app")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 || got[0].GetName() != "b" {
		t.Fatalf("want [b], got %+v", got)
	}
}

func orgNamespace(r *resolver.Resolver, name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   r.OrgNamespace(name),
			Labels: map[string]string{v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization},
		},
	}
}

func folderNamespace(r *resolver.Resolver, name, parent string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.FolderNamespace(name),
			Labels: map[string]string{
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.AnnotationParent:  parent,
			},
		},
	}
}

func projectNamespace(r *resolver.Resolver, name, parent string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.ProjectNamespace(name),
			Labels: map[string]string{
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.AnnotationParent:  parent,
			},
		},
	}
}
