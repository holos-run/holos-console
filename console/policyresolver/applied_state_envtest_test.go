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
	// HOL-772 wildcard cascade prefixes — one per test so the namespace
	// hierarchy each builds is uniquely named on the shared apiserver.
	wildcardFolderCascadePrefix     = "wfc"
	wildcardSiblingFolderPrefix     = "wsf"
	wildcardProjectScopeCeilingPref = "wpc"
	wildcardDeploymentByNamePresent = "wdp"
	wildcardDeploymentByNameAbsent  = "wda"
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

// startWildcardCascadeEnvtest boots a Manager that primes the
// TemplatePolicy + TemplatePolicyBinding informers (in addition to the
// Namespace informer that the cache-backed Manager always carries). The
// wildcard cascade tests Resolve through a real
// NewFolderResolverWithBindings wired against the cache-backed client,
// so a regression that bypasses the cache or short-circuits the
// ancestor walk surfaces here rather than only in the unit tests.
func startWildcardCascadeEnvtest(t *testing.T) *crdmgrtesting.Env {
	t.Helper()
	return crdmgrtesting.StartManager(t, crdmgrtesting.Options{
		Scheme: cacheBackedTestScheme(t),
		InformerObjects: []ctrlclient.Object{
			&templatesv1alpha1.RenderState{},
			&templatesv1alpha1.TemplatePolicy{},
			&templatesv1alpha1.TemplatePolicyBinding{},
		},
	})
}

// waitForBindingCacheVisible polls the cache-backed client until the
// requested TemplatePolicyBinding is observable. envtest writes go
// through the apiserver and surface in the informer cache on the next
// watch event; the tests must not race this propagation because the
// folderResolver reads through the same cache.
func waitForBindingCacheVisible(t *testing.T, c ctrlclient.Client, namespace, name string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var got templatesv1alpha1.TemplatePolicyBinding
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, &got); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("binding %s/%s not visible in cache within deadline", namespace, name)
}

// waitForPolicyCacheVisible mirrors waitForBindingCacheVisible for the
// TemplatePolicy informer.
func waitForPolicyCacheVisible(t *testing.T, c ctrlclient.Client, namespace, name string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var got templatesv1alpha1.TemplatePolicy
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, &got); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("policy %s/%s not visible in cache within deadline", namespace, name)
}

// TestFolderResolver_EnvtestWildcardFolderCascade pins HOL-772's
// folder-scope cascade contract under wildcards. A folder-scope binding
// with target_refs `[{project: "*", name: "*", kind: PROJECT_TEMPLATE}]`
// must attach to every project-template render under the folder's
// projects (cascade reach), and the envtest run uses the production
// cache-backed client so the test exercises the same code path the
// running console process does.
func TestFolderResolver_EnvtestWildcardFolderCascade(t *testing.T) {
	env := startWildcardCascadeEnvtest(t)
	if env == nil {
		return
	}

	r := baseResolver()
	prefix := wildcardFolderCascadePrefix
	orgNs := r.OrgNamespace(prefix + "-acme")
	folderEngNs := r.FolderNamespace(prefix + "-eng")
	projectLiliesNs := r.ProjectNamespace(prefix + "-lilies")
	projectRosesNs := r.ProjectNamespace(prefix + "-roses")

	ensureRenderStateNamespace(t, env.Direct, orgNs, v1alpha2.ResourceTypeOrganization, "")
	ensureRenderStateNamespace(t, env.Direct, folderEngNs, v1alpha2.ResourceTypeFolder, orgNs)
	ensureRenderStateNamespace(t, env.Direct, projectLiliesNs, v1alpha2.ResourceTypeProject, folderEngNs)
	ensureRenderStateNamespace(t, env.Direct, projectRosesNs, v1alpha2.ResourceTypeProject, folderEngNs)

	t.Cleanup(func() {
		for _, name := range []string{projectLiliesNs, projectRosesNs, folderEngNs, orgNs} {
			_ = env.Direct.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
	})

	// Org-scope policy that REQUIRE's an org template the binding targets.
	orgPolicy := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "audit", Namespace: orgNs},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, prefix+"-acme", "audit-policy"),
			},
		},
	}
	if err := env.Direct.Create(context.Background(), orgPolicy); err != nil {
		t.Fatalf("seed org policy: %v", err)
	}

	// Folder-scope binding with full wildcard {project:*, name:*}
	// targeting PROJECT_TEMPLATE. By HOL-770 this matches every
	// project-template render below the folder.
	wildcardBinding := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "wild-cascade", Namespace: folderEngNs},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{
				Namespace: orgNs,
				Name:      "audit",
			},
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{{
				Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
				Name:        WildcardAny,
				ProjectName: WildcardAny,
			}},
		},
	}
	if err := env.Direct.Create(context.Background(), wildcardBinding); err != nil {
		t.Fatalf("seed wildcard binding: %v", err)
	}

	waitForPolicyCacheVisible(t, env.Client, orgNs, "audit")
	waitForBindingCacheVisible(t, env.Client, folderEngNs, "wild-cascade")

	walker := &resolver.Walker{
		Getter:   &resolver.CtrlRuntimeNamespaceGetter{Client: env.Client},
		Resolver: r,
	}
	pl := &cacheBackedPolicyLister{c: env.Client}
	bl := &cacheBackedBindingLister{c: env.Client}
	fr := NewFolderResolverWithBindings(pl, walker, r, bl)

	// Both projects under folderEng must observe the audit-policy
	// injection on a project-template render of any name.
	for _, projectNs := range []string{projectLiliesNs, projectRosesNs} {
		for _, targetName := range []string{"web", "api", "anything"} {
			got, err := fr.Resolve(context.Background(), projectNs, TargetKindProjectTemplate, targetName, nil)
			if err != nil {
				t.Fatalf("Resolve(%s, project_template, %s): %v", projectNs, targetName, err)
			}
			if names := refNames(got); len(names) != 1 || names[0] != "audit-policy" {
				t.Errorf("Resolve(%s, project_template, %s) = %v, want [audit-policy]",
					projectNs, targetName, names)
			}
		}
	}

	// kind never wildcards: a DEPLOYMENT render must NOT match the
	// PROJECT_TEMPLATE-targeted wildcard binding even though name and
	// project_name are both "*".
	got, err := fr.Resolve(context.Background(), projectLiliesNs, TargetKindDeployment, "web", nil)
	if err != nil {
		t.Fatalf("Resolve(deployment): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("PROJECT_TEMPLATE wildcard binding leaked into DEPLOYMENT render: got %v, want empty", refNames(got))
	}
}

// TestFolderResolver_EnvtestWildcardSiblingFolderIsolation locks down
// HOL-554 storage isolation under wildcards: a wildcard binding stored
// under folder A must NOT contribute to renders under sibling folder B.
// The ancestor walk caps wildcard reach; a regression that flattened
// the cache would let a folder-scope wildcard cross folders.
func TestFolderResolver_EnvtestWildcardSiblingFolderIsolation(t *testing.T) {
	env := startWildcardCascadeEnvtest(t)
	if env == nil {
		return
	}

	r := baseResolver()
	prefix := wildcardSiblingFolderPrefix
	orgNs := r.OrgNamespace(prefix + "-acme")
	folderEngNs := r.FolderNamespace(prefix + "-eng")
	folderOpsNs := r.FolderNamespace(prefix + "-ops")
	projectInEngNs := r.ProjectNamespace(prefix + "-eng-app")
	projectInOpsNs := r.ProjectNamespace(prefix + "-ops-app")

	ensureRenderStateNamespace(t, env.Direct, orgNs, v1alpha2.ResourceTypeOrganization, "")
	ensureRenderStateNamespace(t, env.Direct, folderEngNs, v1alpha2.ResourceTypeFolder, orgNs)
	ensureRenderStateNamespace(t, env.Direct, folderOpsNs, v1alpha2.ResourceTypeFolder, orgNs)
	ensureRenderStateNamespace(t, env.Direct, projectInEngNs, v1alpha2.ResourceTypeProject, folderEngNs)
	ensureRenderStateNamespace(t, env.Direct, projectInOpsNs, v1alpha2.ResourceTypeProject, folderOpsNs)

	t.Cleanup(func() {
		for _, name := range []string{projectInEngNs, projectInOpsNs, folderEngNs, folderOpsNs, orgNs} {
			_ = env.Direct.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
	})

	// Org-scope policy. Folder-eng-scope binding with full wildcard
	// targeting PROJECT_TEMPLATE pulls audit-policy into every
	// project-template render reachable from folder eng.
	orgPolicy := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "audit", Namespace: orgNs},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, prefix+"-acme", "audit-policy"),
			},
		},
	}
	if err := env.Direct.Create(context.Background(), orgPolicy); err != nil {
		t.Fatalf("seed org policy: %v", err)
	}
	wildcardBinding := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "wild-eng", Namespace: folderEngNs},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{Namespace: orgNs, Name: "audit"},
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{{
				Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
				Name:        WildcardAny,
				ProjectName: WildcardAny,
			}},
		},
	}
	if err := env.Direct.Create(context.Background(), wildcardBinding); err != nil {
		t.Fatalf("seed eng wildcard binding: %v", err)
	}

	waitForPolicyCacheVisible(t, env.Client, orgNs, "audit")
	waitForBindingCacheVisible(t, env.Client, folderEngNs, "wild-eng")

	walker := &resolver.Walker{
		Getter:   &resolver.CtrlRuntimeNamespaceGetter{Client: env.Client},
		Resolver: r,
	}
	pl := &cacheBackedPolicyLister{c: env.Client}
	bl := &cacheBackedBindingLister{c: env.Client}
	fr := NewFolderResolverWithBindings(pl, walker, r, bl)

	// Project under folder eng: wildcard binding cascades down.
	got, err := fr.Resolve(context.Background(), projectInEngNs, TargetKindProjectTemplate, "anything", nil)
	if err != nil {
		t.Fatalf("Resolve(eng project): %v", err)
	}
	if names := refNames(got); len(names) != 1 || names[0] != "audit-policy" {
		t.Errorf("Resolve(eng project) = %v, want [audit-policy]", names)
	}

	// Project under sibling folder ops: ancestor walk does not cross
	// into folder eng, so the wildcard binding contributes nothing.
	got, err = fr.Resolve(context.Background(), projectInOpsNs, TargetKindProjectTemplate, "anything", nil)
	if err != nil {
		t.Fatalf("Resolve(ops project): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("wildcard binding stored in folder eng leaked into folder ops project: got %v, want empty",
			refNames(got))
	}
}

// TestFolderResolver_EnvtestWildcardProjectScopeCeiling enforces the
// storage-scope ceiling on a project-stored binding (HOL-554 +
// HOL-770/772). Today the templatepolicybinding-folder-or-org-only
// ValidatingAdmissionPolicy rejects every TemplatePolicyBinding stored
// in a project namespace — so even with name="*" and project_name="*",
// the binding cannot land in a project namespace at all. This is the
// admission-layer ceiling on wildcard reach: a wildcard binding cannot
// be planted inside a project to widen its blast radius beyond the
// project. (HOL-618 may revisit project-scoped bindings; that ticket
// will need to update this test alongside any admission relaxation.)
func TestFolderResolver_EnvtestWildcardProjectScopeCeiling(t *testing.T) {
	env := crdmgrtesting.StartManager(t, crdmgrtesting.Options{
		Scheme: cacheBackedTestScheme(t),
		InformerObjects: []ctrlclient.Object{
			&templatesv1alpha1.RenderState{},
			&templatesv1alpha1.TemplatePolicy{},
			&templatesv1alpha1.TemplatePolicyBinding{},
		},
		WaitForAdmissionPolicies: []string{"templatepolicybinding-folder-or-org-only"},
	})
	if env == nil {
		return
	}

	r := baseResolver()
	prefix := wildcardProjectScopeCeilingPref
	orgNs := r.OrgNamespace(prefix + "-acme")
	folderEngNs := r.FolderNamespace(prefix + "-eng")
	projectAlphaNs := r.ProjectNamespace(prefix + "-alpha")

	ensureRenderStateNamespace(t, env.Direct, orgNs, v1alpha2.ResourceTypeOrganization, "")
	ensureRenderStateNamespace(t, env.Direct, folderEngNs, v1alpha2.ResourceTypeFolder, orgNs)
	ensureRenderStateNamespace(t, env.Direct, projectAlphaNs, v1alpha2.ResourceTypeProject, folderEngNs)

	t.Cleanup(func() {
		for _, name := range []string{projectAlphaNs, folderEngNs, orgNs} {
			_ = env.Direct.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
	})

	projectStoredWildcard := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "alpha-wild", Namespace: projectAlphaNs},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{Namespace: orgNs, Name: "audit"},
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{{
				Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
				Name:        WildcardAny,
				ProjectName: WildcardAny,
			}},
		},
	}
	err := env.Direct.Create(context.Background(), projectStoredWildcard)
	if err == nil {
		t.Fatal("expected admission to reject wildcard binding stored in a project namespace")
	}
	if !apierrors.IsForbidden(err) && !apierrors.IsInvalid(err) {
		t.Fatalf("expected Forbidden or Invalid from admission, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), projectAlphaNs) {
		t.Logf("admission error did not name the project namespace; saw: %v", err)
	}
}

// TestFolderResolver_EnvtestWildcardProjectMatchesEveryReachableProject
// covers the {project: "*", name: "web", kind: DEPLOYMENT} case from
// HOL-772's AC: a folder-scope binding targets every deployment named
// "web" across every project reachable from the folder, and contributes
// zero when the queried target name does not match.
func TestFolderResolver_EnvtestWildcardProjectMatchesEveryReachableProject(t *testing.T) {
	env := startWildcardCascadeEnvtest(t)
	if env == nil {
		return
	}

	r := baseResolver()
	prefix := wildcardDeploymentByNamePresent
	orgNs := r.OrgNamespace(prefix + "-acme")
	folderEngNs := r.FolderNamespace(prefix + "-eng")
	projectOneNs := r.ProjectNamespace(prefix + "-one")
	projectTwoNs := r.ProjectNamespace(prefix + "-two")

	ensureRenderStateNamespace(t, env.Direct, orgNs, v1alpha2.ResourceTypeOrganization, "")
	ensureRenderStateNamespace(t, env.Direct, folderEngNs, v1alpha2.ResourceTypeFolder, orgNs)
	ensureRenderStateNamespace(t, env.Direct, projectOneNs, v1alpha2.ResourceTypeProject, folderEngNs)
	ensureRenderStateNamespace(t, env.Direct, projectTwoNs, v1alpha2.ResourceTypeProject, folderEngNs)

	t.Cleanup(func() {
		for _, name := range []string{projectOneNs, projectTwoNs, folderEngNs, orgNs} {
			_ = env.Direct.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
	})

	orgPolicy := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "audit", Namespace: orgNs},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, prefix+"-acme", "audit-policy"),
			},
		},
	}
	if err := env.Direct.Create(context.Background(), orgPolicy); err != nil {
		t.Fatalf("seed org policy: %v", err)
	}
	bindingProjectWildcard := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "web-everywhere", Namespace: folderEngNs},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{Namespace: orgNs, Name: "audit"},
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{{
				Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
				Name:        "web",
				ProjectName: WildcardAny,
			}},
		},
	}
	if err := env.Direct.Create(context.Background(), bindingProjectWildcard); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	waitForPolicyCacheVisible(t, env.Client, orgNs, "audit")
	waitForBindingCacheVisible(t, env.Client, folderEngNs, "web-everywhere")

	walker := &resolver.Walker{
		Getter:   &resolver.CtrlRuntimeNamespaceGetter{Client: env.Client},
		Resolver: r,
	}
	pl := &cacheBackedPolicyLister{c: env.Client}
	bl := &cacheBackedBindingLister{c: env.Client}
	fr := NewFolderResolverWithBindings(pl, walker, r, bl)

	// Both projects' "web" deployment match.
	for _, projectNs := range []string{projectOneNs, projectTwoNs} {
		got, err := fr.Resolve(context.Background(), projectNs, TargetKindDeployment, "web", nil)
		if err != nil {
			t.Fatalf("Resolve(%s, web): %v", projectNs, err)
		}
		if names := refNames(got); len(names) != 1 || names[0] != "audit-policy" {
			t.Errorf("Resolve(%s, web) = %v, want [audit-policy]", projectNs, names)
		}
	}

	// A different deployment name does not match.
	got, err := fr.Resolve(context.Background(), projectOneNs, TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve(api): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("name=web binding matched deployment 'api': got %v, want empty", refNames(got))
	}
}

// TestFolderResolver_EnvtestWildcardProjectZeroMatchesWhenNamesAbsent
// is the negative companion to the previous test: when no deployment
// in any reachable project has the target name, Resolve returns the
// empty set. This is the resolver-side guard that "wildcard project +
// literal name" still narrows by name — a regression that flattened
// names on top of projects would surface here.
func TestFolderResolver_EnvtestWildcardProjectZeroMatchesWhenNamesAbsent(t *testing.T) {
	env := startWildcardCascadeEnvtest(t)
	if env == nil {
		return
	}

	r := baseResolver()
	prefix := wildcardDeploymentByNameAbsent
	orgNs := r.OrgNamespace(prefix + "-acme")
	folderEngNs := r.FolderNamespace(prefix + "-eng")
	projectOnlyNs := r.ProjectNamespace(prefix + "-only")

	ensureRenderStateNamespace(t, env.Direct, orgNs, v1alpha2.ResourceTypeOrganization, "")
	ensureRenderStateNamespace(t, env.Direct, folderEngNs, v1alpha2.ResourceTypeFolder, orgNs)
	ensureRenderStateNamespace(t, env.Direct, projectOnlyNs, v1alpha2.ResourceTypeProject, folderEngNs)

	t.Cleanup(func() {
		for _, name := range []string{projectOnlyNs, folderEngNs, orgNs} {
			_ = env.Direct.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
	})

	orgPolicy := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "audit", Namespace: orgNs},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, prefix+"-acme", "audit-policy"),
			},
		},
	}
	if err := env.Direct.Create(context.Background(), orgPolicy); err != nil {
		t.Fatalf("seed org policy: %v", err)
	}
	binding := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "web-only", Namespace: folderEngNs},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{Namespace: orgNs, Name: "audit"},
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{{
				Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
				Name:        "web",
				ProjectName: WildcardAny,
			}},
		},
	}
	if err := env.Direct.Create(context.Background(), binding); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	waitForPolicyCacheVisible(t, env.Client, orgNs, "audit")
	waitForBindingCacheVisible(t, env.Client, folderEngNs, "web-only")

	walker := &resolver.Walker{
		Getter:   &resolver.CtrlRuntimeNamespaceGetter{Client: env.Client},
		Resolver: r,
	}
	pl := &cacheBackedPolicyLister{c: env.Client}
	bl := &cacheBackedBindingLister{c: env.Client}
	fr := NewFolderResolverWithBindings(pl, walker, r, bl)

	// No render target named "web" in this hierarchy means the resolver
	// is queried for "api" / other names and must return empty.
	for _, targetName := range []string{"api", "background", "anything"} {
		got, err := fr.Resolve(context.Background(), projectOnlyNs, TargetKindDeployment, targetName, nil)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", targetName, err)
		}
		if len(got) != 0 {
			t.Errorf("Resolve(%s) = %v, want empty (no deployment named 'web' should match the literal-name wildcard-project binding for a different name)",
				targetName, refNames(got))
		}
	}

	// A "web" render in the project DOES match — pinning that the
	// binding still attaches when the literal name aligns.
	got, err := fr.Resolve(context.Background(), projectOnlyNs, TargetKindDeployment, "web", nil)
	if err != nil {
		t.Fatalf("Resolve(web): %v", err)
	}
	if names := refNames(got); len(names) != 1 || names[0] != "audit-policy" {
		t.Errorf("Resolve(web) = %v, want [audit-policy]", names)
	}
}
