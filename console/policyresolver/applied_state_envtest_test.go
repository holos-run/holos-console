// applied_state_envtest_test.go exercises AppliedRenderStateClient
// against a real apiserver via the shared envtest bootstrap. The unit
// tests in applied_state_test.go cover the per-method semantics through
// a controller-runtime fake client (which neither runs admission nor
// drives a real informer cache); this file regresses the two
// production-only invariants the unit tests cannot reach:
//
//  1. Cache-backed reads observe writes within the watch round-trip
//     (HOL-622 freshness contract, applied to RenderState in HOL-694).
//  2. The renderstate-folder-or-org-only ValidatingAdmissionPolicy
//     shipped under config/holos-console/admission/ rejects RenderState writes into
//     project namespaces — the HOL-554 storage-isolation guardrail.
//
// The tests are gated on envtest binaries (StartManager calls t.Skip
// when KUBEBUILDER_ASSETS is unset and no cached install is present) so
// `go test ./...` still passes on a developer machine without
// `setup-envtest use`.
package policyresolver

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	crdmgrtesting "github.com/holos-run/holos-console/console/crdmgr/testing"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// renderStateEnvtestPrefixes carry the per-test slug roots so namespace
// cleanup in one test cannot collide with fixture creation in another
// when the suite runs against the shared envtest apiserver. Namespace
// Delete is asynchronous (it transitions to Terminating, then GCs the
// contents), so reusing the same name across tests races the
// finalizer.
const (
	renderStateEnvtestRoundTripPrefix = "rt"
	renderStateEnvtestAdmissionPrefix = "ad"
	renderStateEnvtestSelectorPrefix  = "ls"
)

// startRenderStateEnvtest boots a Manager primed with the RenderState
// informer and waits for the renderstate-folder-or-org-only admission
// policy to register. Returns nil when envtest is unavailable so callers
// can short-circuit (StartManager already issued t.Skip in that case).
func startRenderStateEnvtest(t *testing.T) *crdmgrtesting.Env {
	t.Helper()
	env := crdmgrtesting.StartManager(t, crdmgrtesting.Options{
		Scheme:                   cacheBackedTestScheme(t),
		InformerObjects:          []ctrlclient.Object{&templatesv1alpha1.RenderState{}},
		WaitForAdmissionPolicies: []string{"renderstate-folder-or-org-only"},
	})
	return env
}

// ensureRenderStateNamespace creates a namespace with the resolver's
// expected labels. Mirrors templatepolicies.ensureNamespace; duplicated
// here to avoid a cross-package test dependency.
func ensureRenderStateNamespace(t *testing.T, c ctrlclient.Client, name, resourceType, parent string) {
	t.Helper()
	labels := map[string]string{
		v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
		v1alpha2.LabelResourceType: resourceType,
	}
	if parent != "" {
		labels[v1alpha2.AnnotationParent] = parent
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
	if err := c.Create(context.Background(), ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace %q: %v", name, err)
	}
}

// renderStateEnvtestFixture seeds an org/folder/project hierarchy
// through the uncached client and returns the namespace map plus an
// AppliedRenderStateClient wired through the cache-backed Manager
// client (production-equivalent wiring). The prefix scopes namespace
// names per-test so concurrent or sequential tests sharing the apiserver
// do not collide on Terminating namespaces during cleanup.
func renderStateEnvtestFixture(t *testing.T, env *crdmgrtesting.Env, prefix string) (
	*AppliedRenderStateClient,
	map[string]string,
) {
	t.Helper()
	r := &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
	orgNs := r.OrgNamespace(prefix + "-acme")
	folderEngNs := r.FolderNamespace(prefix + "-eng")
	projectLiliesNs := r.ProjectNamespace(prefix + "-lilies")

	ensureRenderStateNamespace(t, env.Direct, orgNs, v1alpha2.ResourceTypeOrganization, "")
	ensureRenderStateNamespace(t, env.Direct, folderEngNs, v1alpha2.ResourceTypeFolder, orgNs)
	ensureRenderStateNamespace(t, env.Direct, projectLiliesNs, v1alpha2.ResourceTypeProject, folderEngNs)

	t.Cleanup(func() {
		for _, name := range []string{projectLiliesNs, folderEngNs, orgNs} {
			_ = env.Direct.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
	})

	walker := &resolver.Walker{
		Getter:   &resolver.CtrlRuntimeNamespaceGetter{Client: env.Client},
		Resolver: r,
	}
	c := NewAppliedRenderStateClient(env.Client, r, walker)
	ns := map[string]string{
		"org":           orgNs,
		"folderEng":     folderEngNs,
		"projectLilies": projectLiliesNs,
	}
	return c, ns
}

// TestAppliedRenderStateClient_EnvtestRoundTrip is the cache-backed
// freshness regression for the RenderState read path. Record writes
// directly to the apiserver (via the delegating client); the subsequent
// Read consults the informer cache. The test polls until the cache
// catches up, bounding the watch lag with a generous deadline so a slow
// CI host does not flake.
func TestAppliedRenderStateClient_EnvtestRoundTrip(t *testing.T) {
	env := startRenderStateEnvtest(t)
	if env == nil {
		return
	}
	client, ns := renderStateEnvtestFixture(t, env, renderStateEnvtestRoundTripPrefix)

	refs := []*consolev1.LinkedTemplateRef{
		{Namespace: "holos-org-" + renderStateEnvtestRoundTripPrefix + "-acme", Name: "audit"},
		{Namespace: "holos-fld-" + renderStateEnvtestRoundTripPrefix + "-eng", Name: "netpol", VersionConstraint: ">=1.0"},
	}
	if err := client.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", refs); err != nil {
		t.Fatalf("RecordAppliedRenderSet: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got, ok, err := client.ReadAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api")
		if err != nil {
			t.Fatalf("ReadAppliedRenderSet: %v", err)
		}
		if ok && len(got) == 2 && got[0].GetName() == "audit" && got[1].GetName() == "netpol" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("cache-backed ReadAppliedRenderSet did not observe Record within deadline")
}

// TestAppliedRenderStateClient_AdmissionRejectsProjectNamespace locks in
// the HOL-554 storage-isolation guardrail at the apiserver layer. A
// direct Create of a RenderState into a project-labelled namespace must
// be rejected by the renderstate-folder-or-org-only VAP. Asserting via
// the uncached client isolates the failure to admission (no cache
// propagation involved).
func TestAppliedRenderStateClient_AdmissionRejectsProjectNamespace(t *testing.T) {
	env := startRenderStateEnvtest(t)
	if env == nil {
		return
	}
	_, ns := renderStateEnvtestFixture(t, env, renderStateEnvtestAdmissionPrefix)

	rs := &templatesv1alpha1.RenderState{
		ObjectMeta: metav1.ObjectMeta{
			Name:      renderStateObjectName(TargetKindDeployment, renderStateEnvtestAdmissionPrefix+"-lilies", "api"),
			Namespace: ns["projectLilies"],
		},
		Spec: templatesv1alpha1.RenderStateSpec{
			TargetKind: templatesv1alpha1.RenderTargetKindDeployment,
			TargetName: "api",
			Project:    renderStateEnvtestAdmissionPrefix + "-lilies",
		},
	}
	err := env.Direct.Create(context.Background(), rs)
	if err == nil {
		t.Fatal("expected admission rejection for RenderState in project namespace")
	}
	if !apierrors.IsForbidden(err) && !apierrors.IsInvalid(err) {
		t.Fatalf("expected Forbidden or Invalid, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "HOL-554") && !strings.Contains(err.Error(), ns["projectLilies"]) {
		t.Logf("admission error did not cite HOL-554 or namespace; saw: %v", err)
	}
}

// TestAppliedRenderStateClient_LabelSelectorLookup proves the three
// label keys mirrored from spec.{targetKind,targetName,project} make a
// label-selector list the cheap path callers expect. The deterministic
// object name covers Get-by-(kind, project, target); listings keyed by
// (kind, target) without a project filter need the labels.
func TestAppliedRenderStateClient_LabelSelectorLookup(t *testing.T) {
	env := startRenderStateEnvtest(t)
	if env == nil {
		return
	}
	client, ns := renderStateEnvtestFixture(t, env, renderStateEnvtestSelectorPrefix)

	dep := []*consolev1.LinkedTemplateRef{
		{Namespace: "holos-org-" + renderStateEnvtestSelectorPrefix + "-acme", Name: "dep-tmpl"},
	}
	prj := []*consolev1.LinkedTemplateRef{
		{Namespace: "holos-org-" + renderStateEnvtestSelectorPrefix + "-acme", Name: "prj-tmpl"},
	}
	if err := client.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindDeployment, "api", dep); err != nil {
		t.Fatalf("Record deployment: %v", err)
	}
	if err := client.RecordAppliedRenderSet(context.Background(), ns["projectLilies"], TargetKindProjectTemplate, "my-tmpl", prj); err != nil {
		t.Fatalf("Record project-template: %v", err)
	}

	// Wait for the cache to catch up to both writes via deterministic Get
	// before testing the selector path — otherwise this test would race
	// the watch instead of the selector logic.
	depKey := types.NamespacedName{
		Namespace: ns["folderEng"],
		Name:      renderStateObjectName(TargetKindDeployment, renderStateEnvtestSelectorPrefix+"-lilies", "api"),
	}
	prjKey := types.NamespacedName{
		Namespace: ns["folderEng"],
		Name:      renderStateObjectName(TargetKindProjectTemplate, renderStateEnvtestSelectorPrefix+"-lilies", "my-tmpl"),
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		dRS := &templatesv1alpha1.RenderState{}
		pRS := &templatesv1alpha1.RenderState{}
		if env.Client.Get(context.Background(), depKey, dRS) == nil &&
			env.Client.Get(context.Background(), prjKey, pRS) == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// (kind, target) selector returns exactly the deployment record.
	depList := &templatesv1alpha1.RenderStateList{}
	if err := env.Client.List(context.Background(), depList,
		ctrlclient.InNamespace(ns["folderEng"]),
		ctrlclient.MatchingLabels{
			templatesv1alpha1.RenderStateTargetKindLabel: string(templatesv1alpha1.RenderTargetKindDeployment),
			templatesv1alpha1.RenderStateTargetNameLabel: "api",
		},
	); err != nil {
		t.Fatalf("List by (targetKind, targetName): %v", err)
	}
	if len(depList.Items) != 1 || depList.Items[0].Spec.TargetName != "api" ||
		depList.Items[0].Spec.TargetKind != templatesv1alpha1.RenderTargetKindDeployment {
		t.Fatalf("(kind, target) selector returned %d items, want 1 deployment 'api'; got %+v",
			len(depList.Items), depList.Items)
	}

	// project selector returns both records under the same project.
	projList := &templatesv1alpha1.RenderStateList{}
	if err := env.Client.List(context.Background(), projList,
		ctrlclient.InNamespace(ns["folderEng"]),
		ctrlclient.MatchingLabels{
			templatesv1alpha1.RenderStateTargetProjectLabel: renderStateEnvtestSelectorPrefix + "-lilies",
		},
	); err != nil {
		t.Fatalf("List by project: %v", err)
	}
	if len(projList.Items) != 2 {
		t.Fatalf("project selector returned %d items, want 2; got %+v",
			len(projList.Items), projList.Items)
	}
}
