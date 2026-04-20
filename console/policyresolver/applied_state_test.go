package policyresolver

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// renderStateScheme registers the kinds the AppliedRenderStateClient writes
// against. Tests build their own scheme rather than reaching into
// folder_resolver_test.go's helpers because the RenderState CRD lives in
// the templates v1alpha1 package, which the legacy ConfigMap scheme did
// not register.
func renderStateScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("registering corev1: %v", err)
	}
	if err := templatesv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("registering templates v1alpha1: %v", err)
	}
	return s
}

// buildRenderStateFixture wraps buildCtrlFixture to also register the
// RenderState CRD on the controller-runtime client. The base fixture only
// registers corev1 because the policy/binding tests do not need the
// templates types.
func buildRenderStateFixture(t *testing.T) (ctrlclient.Client, *resolver.Resolver, map[string]string) {
	t.Helper()
	_, r, ns := buildCtrlFixture()
	scheme := renderStateScheme(t)
	objects := []ctrlclient.Object{
		mkNs(ns["org"], v1alpha2.ResourceTypeOrganization, ""),
		mkNs(ns["folderEng"], v1alpha2.ResourceTypeFolder, ns["org"]),
		mkNs(ns["folderTeamA"], v1alpha2.ResourceTypeFolder, ns["folderEng"]),
		mkNs(ns["projectOrchids"], v1alpha2.ResourceTypeProject, ns["org"]),
		mkNs(ns["projectLilies"], v1alpha2.ResourceTypeProject, ns["folderEng"]),
		mkNs(ns["projectRoses"], v1alpha2.ResourceTypeProject, ns["folderTeamA"]),
	}
	client := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	return client, r, ns
}

// walkerForCtrl builds a resolver.Walker backed by a controller-runtime
// ctrlclient.Client. This mirrors production wiring (HOL-622), where the
// render-state client and the ancestor walker share the same cache-backed
// client instead of threading two different client shapes through the call
// sites.
func walkerForCtrl(c ctrlclient.Client, r *resolver.Resolver) *resolver.Walker {
	return &resolver.Walker{
		Getter:   &resolver.CtrlRuntimeNamespaceGetter{Client: c},
		Resolver: r,
	}
}

// TestFolderNamespaceForProject_NestedFolder picks the immediate folder
// parent when the project lives under one or more folders.
func TestFolderNamespaceForProject_NestedFolder(t *testing.T) {
	client, r, ns := buildRenderStateFixture(t)
	walker := walkerForCtrl(client, r)
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
	client, r, ns := buildRenderStateFixture(t)
	walker := walkerForCtrl(client, r)
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
	client, r, ns := buildRenderStateFixture(t)
	walker := walkerForCtrl(client, r)
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
	client, r, ns := buildRenderStateFixture(t)
	walker := walkerForCtrl(client, r)
	c := NewAppliedRenderStateClient(client, r, walker)

	refs := []*consolev1.LinkedTemplateRef{
		&consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "t1"},
		&consolev1.LinkedTemplateRef{Namespace: "holos-fld-eng", Name: "t2", VersionConstraint: ">=1.0"},
	}
	err := c.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", refs)
	if err != nil {
		t.Fatalf("RecordAppliedRenderSet: %v", err)
	}

	// Present in folder namespace.
	rsName := renderStateObjectName(TargetKindDeployment, "lilies", "api")
	rs := &templatesv1alpha1.RenderState{}
	if err := client.Get(context.Background(), types.NamespacedName{Namespace: ns["folderEng"], Name: rsName}, rs); err != nil {
		t.Fatalf("expected RenderState in folder namespace, got error: %v", err)
	}
	if rs.Labels[v1alpha2.LabelManagedBy] != v1alpha2.ManagedByValue {
		t.Errorf("missing managed-by label: got %q", rs.Labels[v1alpha2.LabelManagedBy])
	}
	if rs.Labels[v1alpha2.LabelProject] != "lilies" {
		t.Errorf("wrong project label: got %q", rs.Labels[v1alpha2.LabelProject])
	}
	if rs.Spec.TargetKind != templatesv1alpha1.RenderTargetKindDeployment {
		t.Errorf("targetKind: got %q, want %q", rs.Spec.TargetKind, templatesv1alpha1.RenderTargetKindDeployment)
	}
	if rs.Spec.TargetName != "api" {
		t.Errorf("targetName: got %q, want %q", rs.Spec.TargetName, "api")
	}
	if rs.Spec.Project != "lilies" {
		t.Errorf("project: got %q, want %q", rs.Spec.Project, "lilies")
	}

	// Absent from project namespace.
	stray := &templatesv1alpha1.RenderState{}
	if err := client.Get(context.Background(), types.NamespacedName{Namespace: ns["projectLilies"], Name: rsName}, stray); err == nil {
		t.Errorf("applied render set leaked into project namespace; expected NotFound")
	} else if !k8serrors.IsNotFound(err) {
		t.Errorf("expected NotFound, got %v", err)
	}
}

// TestRecordAppliedRenderSet_RoundTripViaRead: write then read returns the
// same set and ok=true.
func TestRecordAppliedRenderSet_RoundTripViaRead(t *testing.T) {
	client, r, ns := buildRenderStateFixture(t)
	walker := walkerForCtrl(client, r)
	c := NewAppliedRenderStateClient(client, r, walker)

	refs := []*consolev1.LinkedTemplateRef{
		&consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "httproute"},
		&consolev1.LinkedTemplateRef{Namespace: "holos-fld-eng", Name: "audit", VersionConstraint: ">=2.0.0"},
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
	client, r, ns := buildRenderStateFixture(t)
	walker := walkerForCtrl(client, r)
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

// TestReadAppliedRenderSet_IgnoresProjectNamespaceArtifact: a stale
// project-namespace RenderState must NOT satisfy a read. The HOL-554
// storage-isolation guardrail requires reads to consult only folder/org
// storage. Seeding a project-namespace RenderState by hand and asserting
// the read returns ok=false proves the guardrail holds.
//
// Note on coverage scope: the controller-runtime fake client used here does
// not enforce ValidatingAdmissionPolicy, so the seed succeeds at the
// fixture layer. In a live cluster the same write is rejected by the
// `renderstate-folder-or-org-only` admission policy shipped under
// `config/admission/`. This test asserts the *handler-side* invariant
// (reads ignore project-namespace storage); the admission-side invariant
// (writes refused at the API server) is exercised separately by
// `internal/controller`'s envtest suite.
func TestReadAppliedRenderSet_IgnoresProjectNamespaceArtifact(t *testing.T) {
	client, r, ns := buildRenderStateFixture(t)
	walker := walkerForCtrl(client, r)

	rsName := renderStateObjectName(TargetKindDeployment, "lilies", "api")
	rs := buildForbiddenRenderState(rsName, ns["projectLilies"], "lilies", "api",
		[]templatesv1alpha1.RenderStateLinkedTemplateRef{{
			Namespace: "holos-org-acme",
			Name:      "stale",
		}},
	)
	if err := client.Create(context.Background(), &rs); err != nil {
		t.Fatalf("seed forbidden RenderState: %v", err)
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

func buildForbiddenRenderState(name, namespace, project, targetName string, refs []templatesv1alpha1.RenderStateLinkedTemplateRef) templatesv1alpha1.RenderState {
	return templatesv1alpha1.RenderState{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: templatesv1alpha1.RenderStateSpec{
			TargetKind:  templatesv1alpha1.RenderTargetKindDeployment,
			TargetName:  targetName,
			Project:     project,
			AppliedRefs: refs,
		},
	}
}

// TestRecordAppliedRenderSet_Idempotent: calling Record twice on the same
// target overwrites rather than erroring with AlreadyExists.
func TestRecordAppliedRenderSet_Idempotent(t *testing.T) {
	client, r, ns := buildRenderStateFixture(t)
	walker := walkerForCtrl(client, r)
	c := NewAppliedRenderStateClient(client, r, walker)

	v1 := []*consolev1.LinkedTemplateRef{
		&consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "a"},
	}
	v2 := []*consolev1.LinkedTemplateRef{
		&consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "b"},
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
	client, r, ns := buildRenderStateFixture(t)
	walker := walkerForCtrl(client, r)
	c := NewAppliedRenderStateClient(client, r, walker)

	dep := []*consolev1.LinkedTemplateRef{
		&consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "dep-tmpl"},
	}
	prj := []*consolev1.LinkedTemplateRef{
		&consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "prj-tmpl"},
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

// flakyRenderStateClient wraps a controller-runtime client and injects
// per-method failures for the first N invocations of Get and Update.
// Used to regress the AlreadyExists → Get → Update retry loop on the
// two recoverable failure modes:
//
//   - cache-stale NotFound on Get (informer hasn't observed our Create
//     yet),
//   - Conflict on Update (a peer console replica raced us between Get
//     and Update).
//
// Both must be tolerated by RecordAppliedRenderSet so a transient race
// never surfaces as a drift-record write failure.
type flakyRenderStateClient struct {
	ctrlclient.Client
	getNotFoundCount int
	updateConflicts  int
}

func (f *flakyRenderStateClient) Get(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
	if _, ok := obj.(*templatesv1alpha1.RenderState); ok && f.getNotFoundCount > 0 {
		f.getNotFoundCount--
		return k8serrors.NewNotFound(
			templatesv1alpha1.GroupVersion.WithResource("renderstates").GroupResource(),
			key.Name,
		)
	}
	return f.Client.Get(ctx, key, obj, opts...)
}

func (f *flakyRenderStateClient) Update(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.UpdateOption) error {
	if _, ok := obj.(*templatesv1alpha1.RenderState); ok && f.updateConflicts > 0 {
		f.updateConflicts--
		return k8serrors.NewConflict(
			templatesv1alpha1.GroupVersion.WithResource("renderstates").GroupResource(),
			obj.GetName(),
			fmt.Errorf("simulated conflict"),
		)
	}
	return f.Client.Update(ctx, obj, opts...)
}

// TestRecordAppliedRenderSet_TransientNotFoundOnRetry simulates the
// cache-stale window: Create returns AlreadyExists (the apiserver has
// the object) but the cache-backed Get briefly serves NotFound. The
// retry loop must resolve once the cache catches up.
func TestRecordAppliedRenderSet_TransientNotFoundOnRetry(t *testing.T) {
	client, r, ns := buildRenderStateFixture(t)
	walker := walkerForCtrl(client, r)

	v1 := []*consolev1.LinkedTemplateRef{
		{Namespace: "holos-org-acme", Name: "a"},
	}
	v2 := []*consolev1.LinkedTemplateRef{
		{Namespace: "holos-org-acme", Name: "b"},
	}
	c := NewAppliedRenderStateClient(client, r, walker)
	if err := c.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", v1); err != nil {
		t.Fatalf("seed Record: %v", err)
	}

	// Wrap the client so the first two Gets return NotFound, simulating
	// two informer-watch cycles of cache lag.
	flaky := &flakyRenderStateClient{Client: client, getNotFoundCount: 2}
	cFlaky := NewAppliedRenderStateClient(flaky, r, walker)
	if err := cFlaky.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", v2); err != nil {
		t.Fatalf("retry Record: %v", err)
	}
	if flaky.getNotFoundCount != 0 {
		t.Errorf("expected all 2 NotFound attempts consumed, got %d remaining", flaky.getNotFoundCount)
	}
	got, ok, err := c.ReadAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api")
	if err != nil || !ok {
		t.Fatalf("Read after retry: ok=%v err=%v", ok, err)
	}
	if len(got) != 1 || got[0].GetName() != "b" {
		t.Errorf("retry overwrite did not land: got %v", refNames(got))
	}
}

// TestRecordAppliedRenderSet_TransientConflictOnRetry simulates a peer
// console replica winning the Update race once: the first Update returns
// Conflict, the second succeeds. The retry loop must absorb the
// Conflict and re-apply the Get-then-Update against the fresh
// resourceVersion.
func TestRecordAppliedRenderSet_TransientConflictOnRetry(t *testing.T) {
	client, r, ns := buildRenderStateFixture(t)
	walker := walkerForCtrl(client, r)

	v1 := []*consolev1.LinkedTemplateRef{
		{Namespace: "holos-org-acme", Name: "a"},
	}
	v2 := []*consolev1.LinkedTemplateRef{
		{Namespace: "holos-org-acme", Name: "b"},
	}
	c := NewAppliedRenderStateClient(client, r, walker)
	if err := c.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", v1); err != nil {
		t.Fatalf("seed Record: %v", err)
	}

	flaky := &flakyRenderStateClient{Client: client, updateConflicts: 1}
	cFlaky := NewAppliedRenderStateClient(flaky, r, walker)
	if err := cFlaky.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", v2); err != nil {
		t.Fatalf("retry Record: %v", err)
	}
	if flaky.updateConflicts != 0 {
		t.Errorf("expected Conflict attempt consumed, got %d remaining", flaky.updateConflicts)
	}
}

// TestDiffRenderSets covers the diff classification used by drift detection.
func TestDiffRenderSets(t *testing.T) {
	a := &consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "a"}
	b := &consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "b"}
	c := &consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "c"}

	tests := []struct {
		name      string
		applied   []*consolev1.LinkedTemplateRef
		current   []*consolev1.LinkedTemplateRef
		wantAdd   []string
		wantRem   []string
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
