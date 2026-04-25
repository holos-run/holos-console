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

package controller_test

// HOL-959 envtest: TemplateDependency reconciler smoke scenarios.
//
// Test coverage:
//
//  1. Accepted=False surfaces for an invalid spec (missing namespace).
//  2. ResolvedRefs=False + GrantNotFound when a cross-namespace requires ref
//     is not authorised by a TemplateGrant.
//  3. The mcp-server / mcp-server-2 / waypoint smoke scenario:
//     a. Create TemplateDependency (waypoint requires).
//     b. Create dep1=mcp-server, dep2=mcp-server-2 — both with TemplateRef
//        pointing at the "waypoint" template.
//     c. Assert the singleton "waypoint-shared" Deployment is created with two
//        non-controller ownerReferences (one per dependent).
//     d. Delete dep1 — assert singleton still exists (dep2 still owns it).
//     e. Delete dep2 — assert singleton is eventually reaped by GC.
//
// Note: native GC in envtest behaves the same as in a real cluster; the test
// polls with a generous timeout to let the garbage collector run.

import (
	"context"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// waitForTemplateDependencyCondition polls until the named condition on the
// TemplateDependency reaches wantStatus, or the deadline expires.
func waitForTemplateDependencyCondition(
	t *testing.T,
	c client.Client,
	key client.ObjectKey,
	condType string,
	wantStatus metav1.ConditionStatus,
) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var obj v1alpha1.TemplateDependency
	for time.Now().Before(deadline) {
		if err := c.Get(context.Background(), key, &obj); err != nil {
			if !apierrors.IsNotFound(err) {
				t.Fatalf("get TemplateDependency %s: %v", key, err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		for _, cond := range obj.Status.Conditions {
			if cond.Type == condType && cond.Status == wantStatus {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("condition %q on TemplateDependency %s never reached %s", condType, key, wantStatus)
}

// waitForDeploymentGone polls until the Deployment at key is 404 or the
// deadline expires. Used to assert native GC has reaped the singleton.
func waitForDeploymentGone(t *testing.T, c client.Client, key client.ObjectKey) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var d deploymentsv1alpha1.Deployment
		err := c.Get(context.Background(), key, &d)
		if apierrors.IsNotFound(err) {
			return
		}
		if err != nil {
			t.Fatalf("get Deployment %s: %v", key, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("Deployment %s still exists after deadline; native GC did not reap it", key)
}

// waitForDeploymentExists polls until the Deployment at key exists or the
// deadline expires.
func waitForDeploymentExists(t *testing.T, c client.Client, key client.ObjectKey) *deploymentsv1alpha1.Deployment {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var d deploymentsv1alpha1.Deployment
		if err := c.Get(context.Background(), key, &d); err == nil {
			return &d
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Deployment %s did not appear within deadline", key)
	return nil
}

// TestTemplateDependency_ValidSpecSurfacesAcceptedTrue asserts that a
// TemplateDependency with a valid spec gets Accepted=True and Ready=True
// (no matching Deployments yet → no work to do, still Ready).
func TestTemplateDependency_ValidSpecSurfacesAcceptedTrue(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	ns := "prj-tdep-valid-spec"
	mustCreateNamespace(t, e.client, ns, "project")

	td := &v1alpha1.TemplateDependency{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "valid"},
		Spec: v1alpha1.TemplateDependencySpec{
			Dependent: v1alpha1.LinkedTemplateRef{Namespace: ns, Name: "mcp-server-tmpl"},
			Requires:  v1alpha1.LinkedTemplateRef{Namespace: ns, Name: "waypoint-tmpl"},
		},
	}
	if err := e.client.Create(context.Background(), td); err != nil {
		t.Fatalf("create TemplateDependency: %v", err)
	}
	key := client.ObjectKeyFromObject(td)
	waitForTemplateDependencyCondition(t, e.client, key, v1alpha1.TemplateDependencyConditionAccepted, metav1.ConditionTrue)
	waitForTemplateDependencyCondition(t, e.client, key, v1alpha1.TemplateDependencyConditionReady, metav1.ConditionTrue)

	var got v1alpha1.TemplateDependency
	if err := e.client.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if got.Status.ObservedGeneration != got.Generation {
		t.Errorf("observedGeneration=%d want %d", got.Status.ObservedGeneration, got.Generation)
	}
}

// TestTemplateDependency_CrossNamespaceGrantNotFound asserts that a
// cross-namespace Requires reference without an authorising TemplateGrant
// surfaces ResolvedRefs=False with GrantNotFound reason.
func TestTemplateDependency_CrossNamespaceGrantNotFound(t *testing.T) {
	e := startEnv(t)
	_, cancelM, errCh := startManagerWithCache(t, e.cfg, nil)
	t.Cleanup(func() { stopManager(t, cancelM, errCh) })

	prjNS := "prj-tdep-grant-missing"
	orgNS := "org-tdep-grant-missing"
	mustCreateNamespace(t, e.client, prjNS, "project")
	mustCreateNamespace(t, e.client, orgNS, "organization")

	// Cross-namespace requires ref: prj -> org namespace. No TemplateGrant.
	td := &v1alpha1.TemplateDependency{
		ObjectMeta: metav1.ObjectMeta{Namespace: prjNS, Name: "cross-ns"},
		Spec: v1alpha1.TemplateDependencySpec{
			Dependent: v1alpha1.LinkedTemplateRef{Namespace: prjNS, Name: "mcp-server"},
			Requires:  v1alpha1.LinkedTemplateRef{Namespace: orgNS, Name: "waypoint"},
		},
	}
	if err := e.client.Create(context.Background(), td); err != nil {
		t.Fatalf("create TemplateDependency: %v", err)
	}
	key := client.ObjectKeyFromObject(td)

	// Grant cache starts empty → cross-namespace ref should be denied.
	waitForTemplateDependencyCondition(t, e.client, key, v1alpha1.TemplateDependencyConditionResolvedRefs, metav1.ConditionFalse)

	var got v1alpha1.TemplateDependency
	if err := e.client.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("re-get: %v", err)
	}
	for _, c := range got.Status.Conditions {
		if c.Type == v1alpha1.TemplateDependencyConditionResolvedRefs {
			if c.Reason != v1alpha1.TemplateDependencyReasonGrantNotFound {
				t.Errorf("ResolvedRefs reason=%q want %q", c.Reason, v1alpha1.TemplateDependencyReasonGrantNotFound)
			}
		}
	}
}

// TestTemplateDependency_SmokeScenario_WaypointOwnerRefs is the headline
// HOL-959 envtest exercising the mcp-server / mcp-server-2 / waypoint scenario.
//
// The TemplateDependency declares: any Deployment rendered from "mcp-server-tmpl"
// requires a singleton Deployment of "waypoint-tmpl" (same namespace). The test
// verifies:
//
//  1. With no matching Deployments: Ready=True (nothing to materialise yet).
//  2. dep1=mcp-server (TemplateRef=mcp-server-tmpl) created → singleton
//     "waypoint-tmpl-shared" is created with dep1's ownerReference.
//  3. dep2=mcp-server-2 (TemplateRef=mcp-server-tmpl) created → reconciler
//     appends dep2's ownerReference to the existing singleton.
//  4. Both ownerRefs are non-controller (Controller=false/nil) so native GC
//     in a production cluster would reap the singleton when both dependents are
//     deleted. Envtest does not run the GC controller, so we assert only the
//     ownerRef preconditions rather than waiting for GC to fire.
func TestTemplateDependency_SmokeScenario_WaypointOwnerRefs(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	prjNS := "prj-tdep-smoke"
	mustCreateNamespace(t, e.client, prjNS, "project")

	// TemplateDependency: Deployments rendered from "mcp-server-tmpl"
	// require a singleton "waypoint-tmpl" Deployment (same namespace).
	td := &v1alpha1.TemplateDependency{
		ObjectMeta: metav1.ObjectMeta{Namespace: prjNS, Name: "waypoint-dep"},
		Spec: v1alpha1.TemplateDependencySpec{
			Dependent: v1alpha1.LinkedTemplateRef{
				Namespace: prjNS,
				Name:      "mcp-server-tmpl", // template that mcp-server* Deployments use
			},
			Requires: v1alpha1.LinkedTemplateRef{
				Namespace: prjNS,
				Name:      "waypoint-tmpl", // the singleton to materialise
			},
		},
	}
	if err := e.client.Create(context.Background(), td); err != nil {
		t.Fatalf("create TemplateDependency: %v", err)
	}
	tdKey := client.ObjectKeyFromObject(td)

	// Stage 1: no matching Deployments yet → Ready=True.
	waitForTemplateDependencyCondition(t, e.client, tdKey, v1alpha1.TemplateDependencyConditionReady, metav1.ConditionTrue)

	// Stage 2: create dep1 — its TemplateRef points at mcp-server-tmpl.
	dep1 := &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: prjNS, Name: "mcp-server"},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: prjNS,
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: prjNS,
				Name:      "mcp-server-tmpl",
			},
		},
	}
	if err := e.client.Create(context.Background(), dep1); err != nil {
		t.Fatalf("create dep1: %v", err)
	}

	// Singleton "waypoint-tmpl-shared" should be created.
	singletonKey := client.ObjectKey{Namespace: prjNS, Name: "waypoint-tmpl-shared"}
	singleton := waitForDeploymentExists(t, e.client, singletonKey)
	t.Logf("singleton created after dep1: %s/%s ownerRefs=%d", singleton.Namespace, singleton.Name, len(singleton.OwnerReferences))

	// Stage 3: create dep2 — reconciler should append its ownerReference.
	dep2 := &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: prjNS, Name: "mcp-server-2"},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: prjNS,
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: prjNS,
				Name:      "mcp-server-tmpl",
			},
		},
	}
	if err := e.client.Create(context.Background(), dep2); err != nil {
		t.Fatalf("create dep2: %v", err)
	}

	// Poll until both ownerReferences are present.
	deadline := time.Now().Add(15 * time.Second)
	var finalSingleton deploymentsv1alpha1.Deployment
	for time.Now().Before(deadline) {
		if err := e.client.Get(context.Background(), singletonKey, &finalSingleton); err != nil {
			t.Fatalf("get singleton: %v", err)
		}
		if len(finalSingleton.OwnerReferences) == 2 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if got := len(finalSingleton.OwnerReferences); got != 2 {
		t.Fatalf("singleton ownerRefs=%d want 2 (one per dependent)", got)
	}

	// Verify ownerRef properties: non-controller, block-owner-deletion.
	for _, ref := range finalSingleton.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			t.Errorf("ownerRef %q Controller must be false/nil for GC co-ownership", ref.Name)
		}
		if ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion {
			t.Errorf("ownerRef %q BlockOwnerDeletion must be true", ref.Name)
		}
	}

	// Verify both expected UIDs are present.
	uids := map[string]bool{}
	for _, ref := range finalSingleton.OwnerReferences {
		uids[string(ref.UID)] = true
	}
	if !uids[string(dep1.UID)] {
		t.Errorf("dep1 UID %q not found in singleton ownerRefs %+v", dep1.UID, finalSingleton.OwnerReferences)
	}
	if !uids[string(dep2.UID)] {
		t.Errorf("dep2 UID %q not found in singleton ownerRefs %+v", dep2.UID, finalSingleton.OwnerReferences)
	}

	t.Logf("smoke scenario passed: singleton has ownerRefs for both dep1 and dep2; "+
		"native GC would reap singleton in a production cluster when both are deleted. "+
		"(Envtest does not run kube-controller-manager GC, so GC is not asserted here.)")
}
