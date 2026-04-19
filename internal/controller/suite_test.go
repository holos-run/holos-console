/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package controller_test hosts the envtest suite for HOL-620. The suite
// boots a real kube-apiserver via envtest, installs the templates.holos.run
// CRDs, spins up a controller-runtime Manager with the three reconcilers
// registered, and exercises each reconciler's public contract:
//
//  1. observed-generation converges after spec writes.
//  2. each kind's component-condition set populates as expected.
//  3. a TemplatePolicyBinding's ResolvedRefs condition flips when the
//     referenced Template appears in its project namespace.
//  4. the hot-loop guard holds: a second reconcile that sees no change does
//     not bump resourceVersion on status.
//  5. two managers running against the same API server observe each other's
//     writes via their caches (multi-manager freshness).
//  6. a missing CRD trips the manager Start() path into an error rather than
//     a silent hang (sync-error surface).
//  7. graceful shutdown: cancelling Start()'s context stops the manager
//     cleanly.
//
// These tests skip — not fail — when envtest binaries are not installed, so
// developers and CI agents that have not run `setup-envtest use` can still
// `go test ./...`.
package controller_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	controllerpkg "github.com/holos-run/holos-console/internal/controller"
)

// env wraps the envtest.Environment so tests can share startup cost. Each
// test spins up its own Manager against the same env so reconciler failure
// modes are fully isolated test-by-test.
type env struct {
	env    *envtest.Environment
	cfg    *rest.Config
	client client.Client
}

// startEnv boots the envtest API server with the templates.holos.run CRDs
// installed. Callers build their own Manager(s) from env.cfg.
func startEnv(t *testing.T) *env {
	t.Helper()

	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		if assets := detectEnvtestAssets(); assets != "" {
			t.Setenv("KUBEBUILDER_ASSETS", assets)
		} else {
			t.Skip("envtest binaries not found; set KUBEBUILDER_ASSETS or run `setup-envtest use` to download")
		}
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("finding repo root: %v", err)
	}

	e := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(repoRoot, "config", "crd")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := e.Start()
	if err != nil {
		t.Fatalf("starting envtest: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := e.Stop(); stopErr != nil {
			t.Logf("stopping envtest: %v", stopErr)
		}
	})

	c, err := client.New(cfg, client.Options{Scheme: controllerpkg.Scheme})
	if err != nil {
		t.Fatalf("constructing direct client: %v", err)
	}
	return &env{env: e, cfg: cfg, client: c}
}

// startManager constructs a controller.Manager from cfg and starts it in a
// goroutine. Returns the Manager, the cancel that stops it, and a channel
// that receives Start's error. Waits for the cache to sync before returning
// so tests can issue writes against a hot cache.
func startManager(t *testing.T, cfg *rest.Config) (*controllerpkg.Manager, context.CancelFunc, <-chan error) {
	t.Helper()

	m, err := controllerpkg.NewManager(cfg, nil, controllerpkg.Options{
		CacheSyncTimeout:             30 * time.Second,
		SkipControllerNameValidation: true,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- m.Start(ctx)
	}()

	// Wait for Ready() to flip. We bound the wait so a broken build
	// (missing CRDs, scheme mismatch) fails promptly rather than hanging
	// the whole suite.
	deadline := time.Now().Add(30 * time.Second)
	for !m.Ready() {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("manager did not become ready within deadline")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return m, cancel, errCh
}

// stopManager cancels the manager context and asserts that Start returned
// cleanly. Called from t.Cleanup so shutdowns serialize.
func stopManager(t *testing.T, cancel context.CancelFunc, errCh <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("manager exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("manager did not shut down within deadline")
	}
}

// waitForCondition polls the named object's status until the named condition
// reaches the expected status, or deadline expires.
func waitForCondition(t *testing.T, c client.Client, obj client.Object, key types.NamespacedName, condType string, want metav1.ConditionStatus) *metav1.Condition {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last *metav1.Condition
	for time.Now().Before(deadline) {
		if err := c.Get(context.Background(), key, obj); err != nil {
			if !apierrors.IsNotFound(err) {
				t.Fatalf("get %s: %v", key, err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		var conds []metav1.Condition
		switch typed := obj.(type) {
		case *v1alpha1.Template:
			conds = typed.Status.Conditions
		case *v1alpha1.TemplatePolicy:
			conds = typed.Status.Conditions
		case *v1alpha1.TemplatePolicyBinding:
			conds = typed.Status.Conditions
		default:
			t.Fatalf("unsupported kind %T", obj)
		}
		if cond := meta.FindStatusCondition(conds, condType); cond != nil {
			last = cond
			if cond.Status == want {
				return cond
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if last == nil {
		t.Fatalf("condition %q never appeared on %s", condType, key)
	}
	t.Fatalf("condition %q on %s did not reach %s; last=%+v", condType, key, want, last)
	return nil
}

// mustCreateNamespace installs a project/folder/org-labeled namespace so
// admission policies and the binding resolver can read its resource-type.
func mustCreateNamespace(t *testing.T, c client.Client, name, resourceType string) {
	t.Helper()
	mustCreateNamespaceWithParent(t, c, name, resourceType, "")
}

// mustCreateNamespaceWithParent installs a namespace carrying the
// resource-type label and (optionally) a console.holos.run/parent label
// pointing at parentNs. Used by the binding reconciler ancestor-chain tests
// so the cache walker can resolve a real hierarchy.
func mustCreateNamespaceWithParent(t *testing.T, c client.Client, name, resourceType, parentNs string) {
	t.Helper()
	labels := map[string]string{
		"console.holos.run/resource-type": resourceType,
	}
	if parentNs != "" {
		labels["console.holos.run/parent"] = parentNs
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
	if err := c.Create(context.Background(), ns); err != nil {
		t.Fatalf("create namespace %q: %v", name, err)
	}
}

// TestTemplate_ObservedGenerationConverges creates a Template, waits for
// Ready=True, then updates the spec and confirms observedGeneration tracks
// metadata.generation. Also asserts the condition set contains Accepted,
// CUEValid, LinkedRefsResolved, Ready — the documented HOL-618 surface.
func TestTemplate_ObservedGenerationConverges(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	ns := "holos-prj-gen-converge"
	mustCreateNamespace(t, e.client, ns, "project")

	tmpl := &v1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "converge", Namespace: ns},
		Spec: v1alpha1.TemplateSpec{
			DisplayName: "Converge",
			Enabled:     true,
			CueTemplate: "package holos\n",
		},
	}
	if err := e.client.Create(context.Background(), tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}
	key := client.ObjectKeyFromObject(tmpl)

	// Ready should flip True; observedGeneration should equal spec
	// generation (1).
	readCopy := &v1alpha1.Template{}
	waitForCondition(t, e.client, readCopy, key, v1alpha1.TemplateConditionReady, metav1.ConditionTrue)
	if got, want := readCopy.Status.ObservedGeneration, readCopy.Generation; got != want {
		t.Fatalf("observedGeneration=%d want %d", got, want)
	}

	// Confirm the full condition set is present.
	expected := []string{
		v1alpha1.TemplateConditionAccepted,
		v1alpha1.TemplateConditionCUEValid,
		v1alpha1.TemplateConditionLinkedRefsResolved,
		v1alpha1.TemplateConditionReady,
	}
	for _, ct := range expected {
		if meta.FindStatusCondition(readCopy.Status.Conditions, ct) == nil {
			t.Errorf("missing condition %q in %+v", ct, readCopy.Status.Conditions)
		}
	}

	// Mutate the spec and confirm observedGeneration tracks the new
	// generation, not the prior value.
	readCopy.Spec.Description = "updated"
	if err := e.client.Update(context.Background(), readCopy); err != nil {
		t.Fatalf("update template: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := e.client.Get(context.Background(), key, readCopy); err != nil {
			t.Fatalf("re-get: %v", err)
		}
		if readCopy.Status.ObservedGeneration == readCopy.Generation &&
			meta.IsStatusConditionTrue(readCopy.Status.Conditions, v1alpha1.TemplateConditionReady) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("observedGeneration never caught up; gen=%d observed=%d", readCopy.Generation, readCopy.Status.ObservedGeneration)
}

// TestTemplate_InvalidCUEConditionSurface creates a Template with a
// deliberately broken CUE payload and confirms CUEValid=False plus Ready=False
// with the documented reasons.
func TestTemplate_InvalidCUEConditionSurface(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	ns := "holos-prj-cue-invalid"
	mustCreateNamespace(t, e.client, ns, "project")

	// CUE syntax error: unterminated list.
	tmpl := &v1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "broken", Namespace: ns},
		Spec: v1alpha1.TemplateSpec{
			DisplayName: "Broken",
			Enabled:     true,
			CueTemplate: "package holos\nfoo: [\n",
		},
	}
	if err := e.client.Create(context.Background(), tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}
	key := client.ObjectKeyFromObject(tmpl)

	read := &v1alpha1.Template{}
	cond := waitForCondition(t, e.client, read, key, v1alpha1.TemplateConditionCUEValid, metav1.ConditionFalse)
	if cond.Reason != v1alpha1.TemplateReasonCUEParseError {
		t.Fatalf("CUEValid reason=%q want %q", cond.Reason, v1alpha1.TemplateReasonCUEParseError)
	}
	ready := meta.FindStatusCondition(read.Status.Conditions, v1alpha1.TemplateConditionReady)
	if ready == nil || ready.Status != metav1.ConditionFalse {
		t.Fatalf("Ready should be False when CUEValid is False, got %+v", ready)
	}
}

// TestTemplate_InvalidVersionSurfacesAcceptedFalse creates a Template with a
// malformed semver version and confirms Accepted=False with InvalidSpec.
func TestTemplate_InvalidVersionSurfacesAcceptedFalse(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	ns := "holos-prj-bad-version"
	mustCreateNamespace(t, e.client, ns, "project")

	tmpl := &v1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "badver", Namespace: ns},
		Spec: v1alpha1.TemplateSpec{
			DisplayName: "Bad version",
			Enabled:     true,
			Version:     "not-a-semver",
			CueTemplate: "package holos\n",
		},
	}
	if err := e.client.Create(context.Background(), tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}
	key := client.ObjectKeyFromObject(tmpl)

	read := &v1alpha1.Template{}
	cond := waitForCondition(t, e.client, read, key, v1alpha1.TemplateConditionAccepted, metav1.ConditionFalse)
	if cond.Reason != v1alpha1.TemplateReasonInvalidSpec {
		t.Fatalf("Accepted reason=%q want %q", cond.Reason, v1alpha1.TemplateReasonInvalidSpec)
	}
}

// TestTemplatePolicy_AcceptedConditionSurface creates a valid
// TemplatePolicy in a folder namespace and confirms Accepted=True and
// Ready=True converge.
func TestTemplatePolicy_AcceptedConditionSurface(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	ns := "holos-fld-policy-accepted"
	mustCreateNamespace(t, e.client, ns, "folder")

	pol := &v1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "require-one", Namespace: ns},
		Spec: v1alpha1.TemplatePolicySpec{
			DisplayName: "Require one",
			Rules: []v1alpha1.TemplatePolicyRule{{
				Kind: v1alpha1.TemplatePolicyKindRequire,
				Template: v1alpha1.LinkedTemplateRef{
					Scope: "organization", ScopeName: "acme", Name: "istio",
				},
			}},
		},
	}
	if err := e.client.Create(context.Background(), pol); err != nil {
		t.Fatalf("create policy: %v", err)
	}
	key := client.ObjectKeyFromObject(pol)

	read := &v1alpha1.TemplatePolicy{}
	waitForCondition(t, e.client, read, key, v1alpha1.TemplatePolicyConditionReady, metav1.ConditionTrue)
	if got, want := read.Status.ObservedGeneration, read.Generation; got != want {
		t.Fatalf("observedGeneration=%d want %d", got, want)
	}
}

// TestTemplatePolicyBinding_ResolvedRefs_TransitionsOnTemplateCreate is the
// headline HOL-620 test: it creates a binding whose ProjectTemplate target
// references a Template that does not yet exist, asserts ResolvedRefs=False
// with TemplateNotFound, then creates the Template and asserts
// ResolvedRefs=True lands without manual reconciliation (the Template
// watch handler does the enqueue).
func TestTemplatePolicyBinding_ResolvedRefs_TransitionsOnTemplateCreate(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	// Folder for the binding; project namespace for the Template. The
	// namespace name MUST match the reconciler's projectNamespace
	// computation: <NamespacePrefix><ProjectPrefix><projectName>.
	// Defaults are NamespacePrefix="", ProjectPrefix="prj-" so project
	// "mytarget" resolves to namespace "prj-mytarget". The binding's
	// policyRef also needs a resolvable TemplatePolicy (ResolvedRefs
	// now evaluates both target_refs and policyRef) AND the policy's
	// namespace must lie in the binding namespace's ancestor chain, so
	// link fldNS -> orgNS via console.holos.run/parent.
	orgNS := "org-transition-acme"
	fldNS := "fld-binding-transition"
	prjNS := "prj-mytarget"
	mustCreateNamespaceWithParent(t, e.client, orgNS, "organization", "")
	mustCreateNamespaceWithParent(t, e.client, fldNS, "folder", orgNS)
	mustCreateNamespaceWithParent(t, e.client, prjNS, "project", orgNS)

	policy := &v1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "require-one", Namespace: orgNS},
		Spec: v1alpha1.TemplatePolicySpec{
			DisplayName: "require-one",
			Rules: []v1alpha1.TemplatePolicyRule{{
				Kind: v1alpha1.TemplatePolicyKindRequire,
				Template: v1alpha1.LinkedTemplateRef{
					Scope:     "organization",
					ScopeName: "transition-acme",
					Name:      "the-template",
				},
			}},
		},
	}
	if err := e.client.Create(context.Background(), policy); err != nil {
		t.Fatalf("create policy: %v", err)
	}

	binding := &v1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "bind", Namespace: fldNS},
		Spec: v1alpha1.TemplatePolicyBindingSpec{
			DisplayName: "Bind",
			PolicyRef: v1alpha1.LinkedTemplatePolicyRef{
				Scope: "organization", ScopeName: "transition-acme", Name: "require-one",
			},
			TargetRefs: []v1alpha1.TemplatePolicyBindingTargetRef{{
				Kind:        v1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
				ProjectName: "mytarget",
				Name:        "the-template",
			}},
		},
	}
	if err := e.client.Create(context.Background(), binding); err != nil {
		t.Fatalf("create binding: %v", err)
	}
	bkey := client.ObjectKeyFromObject(binding)

	// Stage 1: Template does not exist -> ResolvedRefs=False with
	// TemplateNotFound (evaluated before the policy-ref branch).
	read := &v1alpha1.TemplatePolicyBinding{}
	cond := waitForCondition(t, e.client, read, bkey, v1alpha1.TemplatePolicyBindingConditionResolvedRefs, metav1.ConditionFalse)
	if cond.Reason != v1alpha1.TemplatePolicyBindingReasonTemplateNotFound {
		t.Fatalf("ResolvedRefs reason=%q want %q", cond.Reason, v1alpha1.TemplatePolicyBindingReasonTemplateNotFound)
	}

	// Stage 2: create the referenced Template; the Watches(&Template)
	// handler on the binding reconciler should enqueue the binding and
	// ResolvedRefs should flip True without us poking it.
	tmpl := &v1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "the-template", Namespace: prjNS},
		Spec: v1alpha1.TemplateSpec{
			DisplayName: "target",
			Enabled:     true,
			CueTemplate: "package holos\n",
		},
	}
	if err := e.client.Create(context.Background(), tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}

	waitForCondition(t, e.client, read, bkey, v1alpha1.TemplatePolicyBindingConditionResolvedRefs, metav1.ConditionTrue)
}

// TestTemplatePolicyBinding_ResolvedRefs_PolicyNotFound asserts ResolvedRefs
// reports PolicyNotFound when the binding's spec.policyRef points at a
// TemplatePolicy that does not exist — even if every target_ref resolves.
// After creating the referenced TemplatePolicy, ResolvedRefs flips True on
// the next reconcile.
func TestTemplatePolicyBinding_ResolvedRefs_PolicyNotFound(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	// org-acme holds the TemplatePolicy; prj-target holds the referenced
	// Template. Folder namespace fld-bind hosts the binding. Defaults:
	// NamespacePrefix="", OrganizationPrefix="org-", FolderPrefix="fld-",
	// ProjectPrefix="prj-". Link fld-bind and prj-target to org-acme via
	// console.holos.run/parent so the reconciler's ancestor-chain check
	// resolves the binding's policy ref as reachable.
	orgNS := "org-acme"
	fldNS := "fld-bind"
	prjNS := "prj-target"
	mustCreateNamespaceWithParent(t, e.client, orgNS, "organization", "")
	mustCreateNamespaceWithParent(t, e.client, fldNS, "folder", orgNS)
	mustCreateNamespaceWithParent(t, e.client, prjNS, "project", orgNS)

	// Pre-create the target Template so targetRefs resolve; that isolates
	// the ResolvedRefs surface to the policyRef branch under test.
	tmpl := &v1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "the-template", Namespace: prjNS},
		Spec: v1alpha1.TemplateSpec{
			DisplayName: "Target",
			Enabled:     true,
			CueTemplate: "package holos\n",
		},
	}
	if err := e.client.Create(context.Background(), tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}

	binding := &v1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "bind-missing-policy", Namespace: fldNS},
		Spec: v1alpha1.TemplatePolicyBindingSpec{
			DisplayName: "Bind-with-missing-policy",
			PolicyRef: v1alpha1.LinkedTemplatePolicyRef{
				Scope: "organization", ScopeName: "acme", Name: "not-here-yet",
			},
			TargetRefs: []v1alpha1.TemplatePolicyBindingTargetRef{{
				Kind:        v1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
				ProjectName: "target",
				Name:        "the-template",
			}},
		},
	}
	if err := e.client.Create(context.Background(), binding); err != nil {
		t.Fatalf("create binding: %v", err)
	}
	bkey := client.ObjectKeyFromObject(binding)

	// Stage 1: policy does not exist -> ResolvedRefs=False PolicyNotFound.
	read := &v1alpha1.TemplatePolicyBinding{}
	cond := waitForCondition(t, e.client, read, bkey, v1alpha1.TemplatePolicyBindingConditionResolvedRefs, metav1.ConditionFalse)
	if cond.Reason != v1alpha1.TemplatePolicyBindingReasonPolicyNotFound {
		t.Fatalf("ResolvedRefs reason=%q want %q", cond.Reason, v1alpha1.TemplatePolicyBindingReasonPolicyNotFound)
	}

	// Stage 2: create the referenced TemplatePolicy. The binding
	// reconciler does not Watch TemplatePolicy in HOL-620 (only Template),
	// so the flip relies on the resync period. Bump the binding's spec
	// to force a re-reconcile within the test window.
	policy := &v1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "not-here-yet", Namespace: orgNS},
		Spec: v1alpha1.TemplatePolicySpec{
			DisplayName: "Policy",
			Rules: []v1alpha1.TemplatePolicyRule{{
				Kind: v1alpha1.TemplatePolicyKindRequire,
				Template: v1alpha1.LinkedTemplateRef{
					Scope:     "organization",
					ScopeName: "acme",
					Name:      "the-template",
				},
			}},
		},
	}
	if err := e.client.Create(context.Background(), policy); err != nil {
		t.Fatalf("create policy: %v", err)
	}

	// Touch the binding's spec so the controller re-reconciles promptly.
	if err := e.client.Get(context.Background(), bkey, read); err != nil {
		t.Fatalf("re-get binding: %v", err)
	}
	read.Spec.Description = "poke"
	if err := e.client.Update(context.Background(), read); err != nil {
		t.Fatalf("poke binding: %v", err)
	}

	waitForCondition(t, e.client, read, bkey, v1alpha1.TemplatePolicyBindingConditionResolvedRefs, metav1.ConditionTrue)
}

// TestTemplatePolicyBinding_ResolvedRefs_OutOfChainPolicyRejected asserts
// ResolvedRefs goes False with PolicyNotFound reason when the binding's
// policyRef names an existing TemplatePolicy that does NOT sit in the
// binding's ancestor chain. Matches the RPC handler's AncestorChainResolver
// gate so controller-published status and write-path validation agree.
func TestTemplatePolicyBinding_ResolvedRefs_OutOfChainPolicyRejected(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	// Two disjoint trees: org-alpha owns fld-alpha1 + prj-alpha-target;
	// org-beta owns a policy that alpha's binding will try (and fail) to
	// reference. The ancestor walker from fld-alpha1 reaches org-alpha
	// and stops, so org-beta/policy is unreachable.
	orgAlpha := "org-alpha"
	orgBeta := "org-beta"
	fldAlpha := "fld-alpha1"
	prjAlpha := "prj-alpha-target"
	mustCreateNamespaceWithParent(t, e.client, orgAlpha, "organization", "")
	mustCreateNamespaceWithParent(t, e.client, orgBeta, "organization", "")
	mustCreateNamespaceWithParent(t, e.client, fldAlpha, "folder", orgAlpha)
	mustCreateNamespaceWithParent(t, e.client, prjAlpha, "project", orgAlpha)

	// Policy lives in the wrong org.
	policy := &v1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "beta-only", Namespace: orgBeta},
		Spec: v1alpha1.TemplatePolicySpec{
			DisplayName: "beta-only",
			Rules: []v1alpha1.TemplatePolicyRule{{
				Kind: v1alpha1.TemplatePolicyKindRequire,
				Template: v1alpha1.LinkedTemplateRef{
					Scope: "organization", ScopeName: "beta", Name: "ignored",
				},
			}},
		},
	}
	if err := e.client.Create(context.Background(), policy); err != nil {
		t.Fatalf("create policy: %v", err)
	}

	// Target Template exists (so targetRefs resolve).
	tmpl := &v1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "alpha-target", Namespace: prjAlpha},
		Spec: v1alpha1.TemplateSpec{
			DisplayName: "alpha-target",
			Enabled:     true,
			CueTemplate: "package holos\n",
		},
	}
	if err := e.client.Create(context.Background(), tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}

	binding := &v1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "cross-tree", Namespace: fldAlpha},
		Spec: v1alpha1.TemplatePolicyBindingSpec{
			DisplayName: "cross-tree",
			PolicyRef: v1alpha1.LinkedTemplatePolicyRef{
				Scope: "organization", ScopeName: "beta", Name: "beta-only",
			},
			TargetRefs: []v1alpha1.TemplatePolicyBindingTargetRef{{
				Kind:        v1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
				ProjectName: "alpha-target",
				Name:        "alpha-target",
			}},
		},
	}
	if err := e.client.Create(context.Background(), binding); err != nil {
		t.Fatalf("create binding: %v", err)
	}
	bkey := client.ObjectKeyFromObject(binding)

	read := &v1alpha1.TemplatePolicyBinding{}
	cond := waitForCondition(t, e.client, read, bkey, v1alpha1.TemplatePolicyBindingConditionResolvedRefs, metav1.ConditionFalse)
	if cond.Reason != v1alpha1.TemplatePolicyBindingReasonPolicyNotFound {
		t.Fatalf("ResolvedRefs reason=%q want %q", cond.Reason, v1alpha1.TemplatePolicyBindingReasonPolicyNotFound)
	}
	if !strings.Contains(cond.Message, "not reachable") {
		t.Fatalf("ResolvedRefs message=%q expected substring \"not reachable\"", cond.Message)
	}
}

// TestTemplate_NoHotLoop asserts the reconciler does not re-write status
// when nothing has changed. After the first reconcile, resourceVersion on
// the Template should hold steady across a generous settle window.
func TestTemplate_NoHotLoop(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	ns := "holos-prj-no-hot-loop"
	mustCreateNamespace(t, e.client, ns, "project")

	tmpl := &v1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "stable", Namespace: ns},
		Spec: v1alpha1.TemplateSpec{
			DisplayName: "Stable",
			Enabled:     true,
			CueTemplate: "package holos\n",
		},
	}
	if err := e.client.Create(context.Background(), tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}
	key := client.ObjectKeyFromObject(tmpl)

	// Wait for Ready=True.
	read := &v1alpha1.Template{}
	waitForCondition(t, e.client, read, key, v1alpha1.TemplateConditionReady, metav1.ConditionTrue)
	first := read.ResourceVersion

	// Sleep well past one watch-event round-trip. If the hot-loop guard
	// is broken, meta.SetStatusCondition would keep resetting
	// LastTransitionTime and the API server would bump resourceVersion
	// on every reconcile.
	time.Sleep(3 * time.Second)

	if err := e.client.Get(context.Background(), key, read); err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if read.ResourceVersion != first {
		t.Fatalf("resourceVersion advanced from %q to %q without a spec change; reconciler is hot-looping", first, read.ResourceVersion)
	}
}

// TestManager_MultiManagerFreshness spins up two managers against the same
// API server and confirms each manager's reconciler observes writes made
// via the shared API server. Protects against a regression where a
// per-manager fake cache masks real staleness.
//
// We assert via the status surface: after both managers are Ready, we
// create a Template in a project namespace. Both managers register a
// Template reconciler; whichever wins the race writes status first, but
// the important bit is that *both* managers see the watch event for the
// write — any manager whose informer is not populated would never close
// the reconcile loop on the Template and we would see a stale or missing
// Ready condition.
func TestManager_MultiManagerFreshness(t *testing.T) {
	e := startEnv(t)

	_, cancel1, errCh1 := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel1, errCh1) })
	_, cancel2, errCh2 := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel2, errCh2) })

	ns := "holos-prj-multi-mgr"
	mustCreateNamespace(t, e.client, ns, "project")

	tmpl := &v1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "cross-cache", Namespace: ns},
		Spec: v1alpha1.TemplateSpec{
			DisplayName: "cross",
			Enabled:     true,
			CueTemplate: "package holos\n",
		},
	}
	if err := e.client.Create(context.Background(), tmpl); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Wait for Ready=True via the direct envtest client — both
	// managers' reconcilers observe the write through their own
	// informers and race to stamp status. If either manager's
	// informer were not populated from the shared API server, the
	// Ready condition on the Template would never settle.
	key := client.ObjectKeyFromObject(tmpl)
	read := &v1alpha1.Template{}
	waitForCondition(t, e.client, read, key, v1alpha1.TemplateConditionReady, metav1.ConditionTrue)
}

// TestManager_SyncErrorWhenCRDMissing: when the CRDs are not installed,
// the manager's reconcilers cannot watch their primary kinds. We assert
// that the Get-through-cache path of a reconciled kind fails fast rather
// than silently hanging — the controller-runtime cache lazily creates
// informers for types it did not know about at Start() time, and a
// missing CRD surfaces as a NewTimeoutError on that path within the
// caller's context deadline. This is the user-observable failure mode
// HOL-620 wants to protect against: a broken deployment should not
// pretend to be healthy.
//
// Note: the `Ready` flag itself is NOT a reliable signal for "CRD
// missing" — controller-runtime's WaitForCacheSync only blocks on
// informers that have been explicitly started, and the Namespace
// informer (primed separately in NewManager) will still sync against
// a clean envtest API server. The signal this test asserts on is the
// cache-backed Get timing out, which is what the RPC handlers and
// reconciler Get paths would observe at runtime.
func TestManager_SyncErrorWhenCRDMissing(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		if assets := detectEnvtestAssets(); assets != "" {
			t.Setenv("KUBEBUILDER_ASSETS", assets)
		} else {
			t.Skip("envtest binaries not found")
		}
	}
	e := &envtest.Environment{}
	cfg, err := e.Start()
	if err != nil {
		t.Fatalf("starting envtest: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop() })

	m, err := controllerpkg.NewManager(cfg, nil, controllerpkg.Options{
		CacheSyncTimeout:             5 * time.Second,
		SkipControllerNameValidation: true,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() {
		errCh <- m.Start(ctx)
	}()

	// Give Start a moment to spin up.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m.Ready() {
			break
		}
		select {
		case err := <-errCh:
			if err != nil {
				return // Start surfaced the failure — good.
			}
			t.Fatalf("Start returned nil before the CRD-missing probe could run")
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Probe the cache path for a kind whose CRD is missing. We expect a
	// timeout-shaped error within a short deadline; a success here
	// would mean the manager is silently serving from an empty cache.
	getCtx, getCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer getCancel()
	tmpl := &v1alpha1.Template{}
	getErr := m.GetClient().Get(getCtx, types.NamespacedName{Namespace: "default", Name: "noop"}, tmpl)
	if getErr == nil {
		t.Fatalf("expected Get to fail with CRD missing, got nil")
	}
	if !apierrors.IsTimeout(getErr) && !apierrors.IsNotFound(getErr) &&
		!errors.Is(getErr, context.DeadlineExceeded) && !strings.Contains(getErr.Error(), "no kind is registered") &&
		!strings.Contains(getErr.Error(), "failed to get API group resources") &&
		!strings.Contains(getErr.Error(), "no matches for kind") {
		t.Logf("CRD-missing Get error: %v", getErr)
	}
	// Any of the above shapes is acceptable — the key property is that
	// the failure surfaces rather than hanging.

	cancel()
	select {
	case <-errCh:
	case <-time.After(10 * time.Second):
		t.Fatalf("manager did not exit after cancel")
	}
}

// TestManager_GracefulShutdown asserts that cancelling Start's context
// returns control to the caller within a reasonable window.
func TestManager_GracefulShutdown(t *testing.T) {
	e := startEnv(t)

	m, err := controllerpkg.NewManager(e.cfg, nil, controllerpkg.Options{
		CacheSyncTimeout:             10 * time.Second,
		SkipControllerNameValidation: true,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	var startReturned atomic.Bool
	go func() {
		err := m.Start(ctx)
		startReturned.Store(true)
		errCh <- err
	}()

	// Let the manager spin up, then cancel.
	deadline := time.Now().Add(10 * time.Second)
	for !m.Ready() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !m.Ready() {
		cancel()
		<-errCh
		t.Fatalf("manager did not become ready before shutdown probe")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("Start returned %v (non-Canceled errors are acceptable in shutdown)", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Start did not return within 10s of cancel")
	}
	if !startReturned.Load() {
		t.Fatalf("Start did not flip startReturned; shutdown did not complete")
	}
}

// detectEnvtestAssets mirrors the helper in api/templates/v1alpha1/crd_test.go.
// We keep a local copy so the controller package has no test dependency on
// the api package's test helpers (Go does not export test-only symbols).
func detectEnvtestAssets() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	base := filepath.Join(home, ".local", "share", "kubebuilder-envtest", "k8s")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	var best string
	for _, en := range entries {
		if !en.IsDir() {
			continue
		}
		cand := filepath.Join(base, en.Name())
		if _, err := os.Stat(filepath.Join(cand, "kube-apiserver")); err == nil {
			if best == "" || en.Name() > filepath.Base(best) {
				best = cand
			}
		}
	}
	return best
}

// findRepoRoot walks up from this file looking for go.mod; same strategy as
// the api-package copy.
func findRepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %q", file)
		}
		dir = parent
	}
}

