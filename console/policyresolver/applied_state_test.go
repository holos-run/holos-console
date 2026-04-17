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

// TestFolderNamespaceForProject_NestedFolder picks the immediate folder
// parent when the project lives under one or more folders.
func TestFolderNamespaceForProject_NestedFolder(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}
	c := NewAppliedRenderStateClient(client, r, walker)

	got, err := c.FolderNamespaceForProject(context.Background(), ns["projectRoses"])
	if err != nil {
		t.Fatalf("FolderNamespaceForProject: %v", err)
	}
	if got != ns["folderTeamA"] {
		t.Errorf("got %q, want %q", got, ns["folderTeamA"])
	}
}

// TestFolderNamespaceForProject_FolderOnly picks the folder when the project
// is directly under a folder with no intermediate folder.
func TestFolderNamespaceForProject_FolderOnly(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}
	c := NewAppliedRenderStateClient(client, r, walker)

	got, err := c.FolderNamespaceForProject(context.Background(), ns["projectLilies"])
	if err != nil {
		t.Fatalf("FolderNamespaceForProject: %v", err)
	}
	if got != ns["folderEng"] {
		t.Errorf("got %q, want %q", got, ns["folderEng"])
	}
}

// TestFolderNamespaceForProject_DirectOrg falls back to the organization
// namespace when a project's immediate parent is an org (no folder between).
func TestFolderNamespaceForProject_DirectOrg(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}
	c := NewAppliedRenderStateClient(client, r, walker)

	got, err := c.FolderNamespaceForProject(context.Background(), ns["projectOrchids"])
	if err != nil {
		t.Fatalf("FolderNamespaceForProject: %v", err)
	}
	if got != ns["org"] {
		t.Errorf("got %q, want %q", got, ns["org"])
	}
}

// TestRecordAppliedRenderSet_WritesToFolderNamespace asserts the HOL-557
// storage-location invariant: the applied render set MUST be stored in the
// owning folder namespace, not the project namespace.
func TestRecordAppliedRenderSet_WritesToFolderNamespace(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}
	c := NewAppliedRenderStateClient(client, r, walker)

	refs := []*consolev1.LinkedTemplateRef{
		{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "t1"},
		{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, ScopeName: "eng", Name: "t2", VersionConstraint: ">=1.0"},
	}
	err := c.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", refs)
	if err != nil {
		t.Fatalf("RecordAppliedRenderSet: %v", err)
	}

	// Present in folder namespace.
	cmName := renderStateConfigMapName(TargetKindDeployment, "lilies", "api")
	cm, err := client.CoreV1().ConfigMaps(ns["folderEng"]).Get(context.Background(), cmName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected ConfigMap in folder namespace, got error: %v", err)
	}
	if cm.Labels[v1alpha2.LabelResourceType] != v1alpha2.ResourceTypeRenderState {
		t.Errorf("missing resource-type label: got %q", cm.Labels[v1alpha2.LabelResourceType])
	}
	if cm.Labels[v1alpha2.LabelRenderTargetProject] != "lilies" {
		t.Errorf("wrong project label: got %q", cm.Labels[v1alpha2.LabelRenderTargetProject])
	}

	// Absent from project namespace.
	_, err = client.CoreV1().ConfigMaps(ns["projectLilies"]).Get(context.Background(), cmName, metav1.GetOptions{})
	if err == nil {
		t.Errorf("applied render set leaked into project namespace; expected NotFound")
	}
}

// TestRecordAppliedRenderSet_RoundTripViaRead: write then read returns the
// same set and ok=true.
func TestRecordAppliedRenderSet_RoundTripViaRead(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}
	c := NewAppliedRenderStateClient(client, r, walker)

	refs := []*consolev1.LinkedTemplateRef{
		{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "httproute"},
		{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, ScopeName: "eng", Name: "audit", VersionConstraint: ">=2.0.0"},
	}
	if err := c.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", refs); err != nil {
		t.Fatalf("RecordAppliedRenderSet: %v", err)
	}

	got, ok, err := c.ReadAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api")
	if err != nil {
		t.Fatalf("ReadAppliedRenderSet: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true, got ok=false")
	}
	if len(got) != 2 {
		t.Fatalf("got %d refs, want 2", len(got))
	}
	if got[0].GetName() != "httproute" || got[1].GetName() != "audit" {
		t.Errorf("unexpected ref names: got %+v", got)
	}
	if got[1].GetVersionConstraint() != ">=2.0.0" {
		t.Errorf("version constraint lost: got %q", got[1].GetVersionConstraint())
	}
}

// TestReadAppliedRenderSet_NotFound returns ok=false with no error.
func TestReadAppliedRenderSet_NotFound(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}
	c := NewAppliedRenderStateClient(client, r, walker)

	refs, ok, err := c.ReadAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "never-applied")
	if err != nil {
		t.Fatalf("ReadAppliedRenderSet: %v", err)
	}
	if ok {
		t.Errorf("expected ok=false, got ok=true")
	}
	if len(refs) != 0 {
		t.Errorf("expected empty slice, got %v", refs)
	}
}

// TestReadAppliedRenderSet_IgnoresProjectNamespaceAnnotation: a stale
// project-namespace annotation must NOT satisfy a read. The HOL-554
// storage-isolation guardrail requires reads to consult only folder/org
// storage. Writing a project-namespace ConfigMap by hand and asserting the
// read returns ok=false proves the guardrail holds.
func TestReadAppliedRenderSet_IgnoresProjectNamespaceAnnotation(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}

	cmName := renderStateConfigMapName(TargetKindDeployment, "lilies", "api")
	payload, _ := MarshalAppliedRenderSet([]*consolev1.LinkedTemplateRef{
		{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "stale"},
	})
	cm := buildForbiddenRenderStateCM(cmName, ns["projectLilies"], string(payload))
	if _, err := client.CoreV1().ConfigMaps(ns["projectLilies"]).Create(context.Background(), &cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed forbidden CM: %v", err)
	}

	c := NewAppliedRenderStateClient(client, r, walker)
	refs, ok, err := c.ReadAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api")
	if err != nil {
		t.Fatalf("ReadAppliedRenderSet: %v", err)
	}
	if ok {
		t.Errorf("read consumed project-namespace artifact; expected ok=false")
	}
	if len(refs) != 0 {
		t.Errorf("expected empty refs, got %v", refs)
	}
}

func buildForbiddenRenderStateCM(name, namespace, payload string) corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				v1alpha2.AnnotationAppliedRenderSet: payload,
			},
		},
	}
}

// TestRecordAppliedRenderSet_Idempotent: calling Record twice on the same
// target overwrites rather than erroring with AlreadyExists.
func TestRecordAppliedRenderSet_Idempotent(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}
	c := NewAppliedRenderStateClient(client, r, walker)

	v1 := []*consolev1.LinkedTemplateRef{
		{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "a"},
	}
	v2 := []*consolev1.LinkedTemplateRef{
		{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "b"},
	}
	if err := c.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", v1); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	if err := c.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", v2); err != nil {
		t.Fatalf("second Record: %v", err)
	}
	got, ok, err := c.ReadAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api")
	if err != nil || !ok {
		t.Fatalf("Read: ok=%v err=%v", ok, err)
	}
	if len(got) != 1 || got[0].GetName() != "b" {
		t.Errorf("second write did not overwrite: got %v", refNames(got))
	}
}

// TestRecordAppliedRenderSet_BothTargetKinds asserts the applied render set
// is recorded for both target kinds under a single project without overwriting
// each other's storage.
func TestRecordAppliedRenderSet_BothTargetKinds(t *testing.T) {
	client, r, ns := buildFixture()
	walker := &resolver.Walker{Client: client, Resolver: r}
	c := NewAppliedRenderStateClient(client, r, walker)

	dep := []*consolev1.LinkedTemplateRef{
		{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "dep-tmpl"},
	}
	prj := []*consolev1.LinkedTemplateRef{
		{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "prj-tmpl"},
	}
	if err := c.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", dep); err != nil {
		t.Fatalf("Record deployment: %v", err)
	}
	if err := c.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindProjectTemplate, "my-tmpl", prj); err != nil {
		t.Fatalf("Record project-template: %v", err)
	}

	depGot, ok, err := c.ReadAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api")
	if err != nil || !ok {
		t.Fatalf("Read dep: ok=%v err=%v", ok, err)
	}
	if len(depGot) != 1 || depGot[0].GetName() != "dep-tmpl" {
		t.Errorf("deployment kind: got %v", refNames(depGot))
	}
	prjGot, ok, err := c.ReadAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindProjectTemplate, "my-tmpl")
	if err != nil || !ok {
		t.Fatalf("Read prj: ok=%v err=%v", ok, err)
	}
	if len(prjGot) != 1 || prjGot[0].GetName() != "prj-tmpl" {
		t.Errorf("project-template kind: got %v", refNames(prjGot))
	}
}

// TestDiffRenderSets covers the diff classification used by drift detection.
func TestDiffRenderSets(t *testing.T) {
	a := &consolev1.LinkedTemplateRef{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "a"}
	b := &consolev1.LinkedTemplateRef{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "b"}
	c := &consolev1.LinkedTemplateRef{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "c"}

	tests := []struct {
		name     string
		applied  []*consolev1.LinkedTemplateRef
		current  []*consolev1.LinkedTemplateRef
		wantAdd  []string
		wantRem  []string
		wantDrift bool
	}{
		{
			name:    "equal sets",
			applied: []*consolev1.LinkedTemplateRef{a, b},
			current: []*consolev1.LinkedTemplateRef{b, a},
		},
		{
			name:      "addition only",
			applied:   []*consolev1.LinkedTemplateRef{a},
			current:   []*consolev1.LinkedTemplateRef{a, b},
			wantAdd:   []string{"b"},
			wantDrift: true,
		},
		{
			name:      "removal only",
			applied:   []*consolev1.LinkedTemplateRef{a, b},
			current:   []*consolev1.LinkedTemplateRef{a},
			wantRem:   []string{"b"},
			wantDrift: true,
		},
		{
			name:      "add and remove",
			applied:   []*consolev1.LinkedTemplateRef{a, b},
			current:   []*consolev1.LinkedTemplateRef{a, c},
			wantAdd:   []string{"c"},
			wantRem:   []string{"b"},
			wantDrift: true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			add, rem, drift := DiffRenderSets(tc.applied, tc.current)
			if drift != tc.wantDrift {
				t.Errorf("drift: got %v, want %v", drift, tc.wantDrift)
			}
			if !equalRefNames(add, tc.wantAdd) {
				t.Errorf("added: got %v, want %v", refNames(add), tc.wantAdd)
			}
			if !equalRefNames(rem, tc.wantRem) {
				t.Errorf("removed: got %v, want %v", refNames(rem), tc.wantRem)
			}
		})
	}
}

func equalRefNames(refs []*consolev1.LinkedTemplateRef, want []string) bool {
	if len(refs) != len(want) {
		return false
	}
	set := make(map[string]struct{}, len(want))
	for _, n := range want {
		set[n] = struct{}{}
	}
	for _, r := range refs {
		if _, ok := set[r.GetName()]; !ok {
			return false
		}
	}
	return true
}

// Silences an unused-import warning on platforms where the fake package is
// only referenced via buildFixture. Any call to NewClientset in this file
// satisfies the lint check.
var _ = fake.NewClientset
