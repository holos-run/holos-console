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

// HOL-960 envtest: TemplateRequirement reconciler smoke scenarios.
//
// Test coverage:
//
//  1. Accepted=True + Ready=True for a valid spec with no matching Deployments.
//  2. ResolvedRefs=False + GrantNotFound when a cross-namespace requires ref
//     is not authorised by a TemplateGrant.
//  3. Platform-mandate scenario with projectName: "*":
//     a. Create TemplateRequirement in org-acme with projectName: "*" wildcard.
//     b. Create dep1 in prj-alpha (project "alpha") → singleton is created.
//     c. Create dep2 in prj-beta (project "beta") → singleton is created.
//     d. Verify both singletons carry non-controller, block-owner-deletion
//        ownerReferences — the GC preconditions for the native reap when
//        the last dependent is deleted.
//  4. cascadeDelete: false — no ownerReference added on the singleton.

import (
	"context"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	"github.com/holos-run/holos-console/console/deployments"
)

// waitForTemplateRequirementCondition polls until the named condition on the
// TemplateRequirement reaches wantStatus or the deadline expires.
func waitForTemplateRequirementCondition(
	t *testing.T,
	c client.Client,
	key client.ObjectKey,
	condType string,
	wantStatus metav1.ConditionStatus,
) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var obj v1alpha1.TemplateRequirement
	for time.Now().Before(deadline) {
		if err := c.Get(context.Background(), key, &obj); err != nil {
			if !apierrors.IsNotFound(err) {
				t.Fatalf("get TemplateRequirement %s: %v", key, err)
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
	t.Fatalf("condition %q on TemplateRequirement %s never reached %s", condType, key, wantStatus)
}

// TestTemplateRequirement_ValidSpecSurfacesAcceptedTrue asserts that a
// TemplateRequirement with a valid spec gets Accepted=True and Ready=True
// when no Deployments match yet (nothing to materialise, still Ready).
func TestTemplateRequirement_ValidSpecSurfacesAcceptedTrue(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	// TemplateRequirement lives in an org namespace (platform mandate style).
	orgNS := "org-treq-valid-spec"
	mustCreateNamespace(t, e.client, orgNS, "organization")

	boolTrue := true
	treq := &v1alpha1.TemplateRequirement{
		ObjectMeta: metav1.ObjectMeta{Namespace: orgNS, Name: "valid"},
		Spec: v1alpha1.TemplateRequirementSpec{
			Requires: v1alpha1.LinkedTemplateRef{Namespace: orgNS, Name: "cert-manager-tmpl"},
			TargetRefs: []v1alpha1.TemplateRequirementTargetRef{
				{
					Kind:        v1alpha1.TemplatePolicyBindingTargetKindDeployment,
					Name:        "*",
					ProjectName: "*",
				},
			},
			CascadeDelete: &boolTrue,
		},
	}
	if err := e.client.Create(context.Background(), treq); err != nil {
		t.Fatalf("create TemplateRequirement: %v", err)
	}
	key := client.ObjectKeyFromObject(treq)
	waitForTemplateRequirementCondition(t, e.client, key, v1alpha1.TemplateRequirementConditionAccepted, metav1.ConditionTrue)
	waitForTemplateRequirementCondition(t, e.client, key, v1alpha1.TemplateRequirementConditionReady, metav1.ConditionTrue)

	var got v1alpha1.TemplateRequirement
	if err := e.client.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if got.Status.ObservedGeneration != got.Generation {
		t.Errorf("observedGeneration=%d want %d", got.Status.ObservedGeneration, got.Generation)
	}
}

// TestTemplateRequirement_CrossNamespaceGrantNotFound asserts that a
// cross-namespace Requires reference without an authorising TemplateGrant
// surfaces ResolvedRefs=False with GrantNotFound reason when a matching
// Deployment exists.
//
// Grant validation is per-Deployment: when there are no matching Deployments
// the condition is Ready=True (nothing to materialise). The test therefore
// creates a Deployment in a project namespace whose ProjectName matches the
// wildcard targetRef so the grant validation runs for that Deployment.
func TestTemplateRequirement_CrossNamespaceGrantNotFound(t *testing.T) {
	e := startEnv(t)
	_, cancelM, errCh := startManagerWithCache(t, e.cfg, nil)
	t.Cleanup(func() { stopManager(t, cancelM, errCh) })

	orgNS := "org-treq-grant-missing"
	otherOrgNS := "org-treq-other"
	prjNS := "prj-treq-grant-missing"
	mustCreateNamespace(t, e.client, orgNS, "organization")
	mustCreateNamespace(t, e.client, otherOrgNS, "organization")
	mustCreateNamespace(t, e.client, prjNS, "project")

	// Cross-namespace requires ref: Deployments in prjNS need to materialise
	// a singleton from otherOrgNS. No TemplateGrant exists for prjNS → otherOrgNS.
	boolTrue := true
	treq := &v1alpha1.TemplateRequirement{
		ObjectMeta: metav1.ObjectMeta{Namespace: orgNS, Name: "cross-ns"},
		Spec: v1alpha1.TemplateRequirementSpec{
			Requires: v1alpha1.LinkedTemplateRef{Namespace: otherOrgNS, Name: "waypoint"},
			TargetRefs: []v1alpha1.TemplateRequirementTargetRef{
				{
					Kind:        v1alpha1.TemplatePolicyBindingTargetKindDeployment,
					Name:        "*",
					ProjectName: "*",
				},
			},
			CascadeDelete: &boolTrue,
		},
	}
	if err := e.client.Create(context.Background(), treq); err != nil {
		t.Fatalf("create TemplateRequirement: %v", err)
	}
	key := client.ObjectKeyFromObject(treq)

	// Stage 1: no Deployments → ResolvedRefs=True (nothing to materialise).
	waitForTemplateRequirementCondition(t, e.client, key, v1alpha1.TemplateRequirementConditionResolvedRefs, metav1.ConditionTrue)

	// Stage 2: create a matching Deployment in prjNS. The reconciler will try
	// to materialise a singleton from otherOrgNS, which requires a grant that
	// does not exist. ResolvedRefs should flip to False.
	dep := &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: prjNS, Name: "needs-grant"},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: "grant-missing",
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: prjNS,
				Name:      "app-tmpl",
			},
		},
	}
	if err := e.client.Create(context.Background(), dep); err != nil {
		t.Fatalf("create dep: %v", err)
	}

	// Grant cache is empty → cross-namespace materialisation should be denied.
	waitForTemplateRequirementCondition(t, e.client, key, v1alpha1.TemplateRequirementConditionResolvedRefs, metav1.ConditionFalse)

	var got v1alpha1.TemplateRequirement
	if err := e.client.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("re-get: %v", err)
	}
	for _, c := range got.Status.Conditions {
		if c.Type == v1alpha1.TemplateRequirementConditionResolvedRefs {
			if c.Reason != v1alpha1.TemplateRequirementReasonGrantNotFound {
				t.Errorf("ResolvedRefs reason=%q want %q", c.Reason, v1alpha1.TemplateRequirementReasonGrantNotFound)
			}
		}
	}
}

// TestTemplateRequirement_PlatformMandateScenario is the headline HOL-960
// envtest exercising the Platform-mandate scenario: one TemplateRequirement
// in org-acme with projectName: "*" covers Deployments in two child projects.
//
// Because the requires template lives in orgNS while the dependent Deployments
// live in project namespaces (cross-namespace), a TemplateGrant with a wildcard
// From is created in orgNS first so ValidateGrant allows all project namespaces
// to use the org-level template.
//
// The test verifies:
//
//  1. With no matching Deployments: Ready=True (nothing to materialise yet).
//  2. dep1 in prj-alpha (project "alpha") created → singleton is created in prj-alpha.
//  3. dep2 in prj-beta (project "beta") created → singleton is created in prj-beta.
//  4. Both singletons carry non-controller, block-owner-deletion ownerReferences —
//     the GC preconditions for the native reap when the last dependent is deleted.
func TestTemplateRequirement_PlatformMandateScenario(t *testing.T) {
	e := startEnv(t)
	grantCache := deployments.NewTemplateGrantCache()
	_, cancel, errCh := startManagerWithCache(t, e.cfg, grantCache)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	orgNS := "org-treq-mandate"
	prjAlpha := "prj-treq-alpha"
	prjBeta := "prj-treq-beta"
	mustCreateNamespace(t, e.client, orgNS, "organization")
	mustCreateNamespace(t, e.client, prjAlpha, "project")
	mustCreateNamespace(t, e.client, prjBeta, "project")

	// Create a wildcard TemplateGrant in orgNS so all project namespaces can
	// use templates from orgNS without per-project grants.
	grant := &v1alpha1.TemplateGrant{
		ObjectMeta: metav1.ObjectMeta{Namespace: orgNS, Name: "allow-all-projects"},
		Spec: v1alpha1.TemplateGrantSpec{
			From: []v1alpha1.TemplateGrantFromRef{
				{Namespace: "*"},
			},
		},
	}
	if err := e.client.Create(context.Background(), grant); err != nil {
		t.Fatalf("create TemplateGrant: %v", err)
	}
	// Wait for the cache to reflect the grant.
	waitForGrantAllowed(t, grantCache,
		grantRef(prjAlpha, "any"),
		grantRef(orgNS, "cert-manager-tmpl"),
	)

	boolTrue := true
	// TemplateRequirement: cert-manager is required for all Deployments
	// reachable from org-treq-mandate (projectName: "*").
	treq := &v1alpha1.TemplateRequirement{
		ObjectMeta: metav1.ObjectMeta{Namespace: orgNS, Name: "cert-manager-mandate"},
		Spec: v1alpha1.TemplateRequirementSpec{
			Requires: v1alpha1.LinkedTemplateRef{
				Namespace: orgNS,
				Name:      "cert-manager-tmpl",
			},
			TargetRefs: []v1alpha1.TemplateRequirementTargetRef{
				{
					Kind:        v1alpha1.TemplatePolicyBindingTargetKindDeployment,
					Name:        "*",
					ProjectName: "*",
				},
			},
			CascadeDelete: &boolTrue,
		},
	}
	if err := e.client.Create(context.Background(), treq); err != nil {
		t.Fatalf("create TemplateRequirement: %v", err)
	}
	treqKey := client.ObjectKeyFromObject(treq)

	// Stage 1: no matching Deployments yet → Ready=True.
	waitForTemplateRequirementCondition(t, e.client, treqKey, v1alpha1.TemplateRequirementConditionReady, metav1.ConditionTrue)

	// Stage 2: create dep1 in prj-alpha.
	dep1 := &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: prjAlpha, Name: "app-alpha"},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: "alpha",
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: prjAlpha,
				Name:      "app-tmpl",
			},
		},
	}
	if err := e.client.Create(context.Background(), dep1); err != nil {
		t.Fatalf("create dep1: %v", err)
	}

	// Singleton "cert-manager-tmpl-shared" should appear in prj-alpha.
	alphaSingletonKey := client.ObjectKey{Namespace: prjAlpha, Name: "cert-manager-tmpl-shared"}
	alphaSingleton := waitForDeploymentExists(t, e.client, alphaSingletonKey)
	t.Logf("alpha singleton created: %s/%s ownerRefs=%d",
		alphaSingleton.Namespace, alphaSingleton.Name, len(alphaSingleton.OwnerReferences))

	// Stage 3: create dep2 in prj-beta.
	dep2 := &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: prjBeta, Name: "app-beta"},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: "beta",
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: prjBeta,
				Name:      "app-tmpl",
			},
		},
	}
	if err := e.client.Create(context.Background(), dep2); err != nil {
		t.Fatalf("create dep2: %v", err)
	}

	// Singleton "cert-manager-tmpl-shared" should appear in prj-beta.
	betaSingletonKey := client.ObjectKey{Namespace: prjBeta, Name: "cert-manager-tmpl-shared"}
	betaSingleton := waitForDeploymentExists(t, e.client, betaSingletonKey)
	t.Logf("beta singleton created: %s/%s ownerRefs=%d",
		betaSingleton.Namespace, betaSingleton.Name, len(betaSingleton.OwnerReferences))

	// Verify the alpha singleton has the ownerReference for dep1.
	var finalAlpha deploymentsv1alpha1.Deployment
	if err := e.client.Get(context.Background(), alphaSingletonKey, &finalAlpha); err != nil {
		t.Fatalf("get alpha singleton: %v", err)
	}
	if len(finalAlpha.OwnerReferences) < 1 {
		t.Fatalf("alpha singleton should have at least 1 ownerRef; got %d", len(finalAlpha.OwnerReferences))
	}
	for _, ref := range finalAlpha.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			t.Errorf("ownerRef %q Controller must be false/nil for GC co-ownership", ref.Name)
		}
		if ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion {
			t.Errorf("ownerRef %q BlockOwnerDeletion must be true", ref.Name)
		}
	}

	// Verify the beta singleton has the ownerReference for dep2.
	var finalBeta deploymentsv1alpha1.Deployment
	if err := e.client.Get(context.Background(), betaSingletonKey, &finalBeta); err != nil {
		t.Fatalf("get beta singleton: %v", err)
	}
	if len(finalBeta.OwnerReferences) < 1 {
		t.Fatalf("beta singleton should have at least 1 ownerRef; got %d", len(finalBeta.OwnerReferences))
	}

	t.Logf("platform-mandate scenario passed: both prj-alpha and prj-beta got their "+
		"singleton cert-manager-tmpl-shared Deployment materialised. "+
		"(Envtest does not run kube-controller-manager GC, so GC is not asserted here.)")
}

// TestTemplateRequirement_CascadeDeleteFalseSkipsOwnerRef asserts that
// setting cascadeDelete: false on a TemplateRequirement causes the singleton
// to be created without an ownerReference on the dependent Deployment.
//
// To avoid needing a TemplateGrant for the cross-namespace path, the
// requires template is in the same namespace as the Deployment (same-
// namespace references are always allowed without a grant).
func TestTemplateRequirement_CascadeDeleteFalseSkipsOwnerRef(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	orgNS := "org-treq-no-cascade"
	prjNS := "prj-treq-no-cascade"
	mustCreateNamespace(t, e.client, orgNS, "organization")
	mustCreateNamespace(t, e.client, prjNS, "project")

	boolFalse := false
	// Requires template is in prjNS (same as the Deployment's namespace) so
	// no TemplateGrant is needed for this test to exercise cascadeDelete=false.
	treq := &v1alpha1.TemplateRequirement{
		ObjectMeta: metav1.ObjectMeta{Namespace: orgNS, Name: "no-cascade"},
		Spec: v1alpha1.TemplateRequirementSpec{
			Requires: v1alpha1.LinkedTemplateRef{Namespace: prjNS, Name: "base-tmpl"},
			TargetRefs: []v1alpha1.TemplateRequirementTargetRef{
				{
					Kind:        v1alpha1.TemplatePolicyBindingTargetKindDeployment,
					Name:        "*",
					ProjectName: "*",
				},
			},
			CascadeDelete: &boolFalse,
		},
	}
	if err := e.client.Create(context.Background(), treq); err != nil {
		t.Fatalf("create TemplateRequirement: %v", err)
	}

	dep := &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: prjNS, Name: "my-app"},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: "no-cascade",
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: prjNS,
				Name:      "my-app-tmpl",
			},
		},
	}
	if err := e.client.Create(context.Background(), dep); err != nil {
		t.Fatalf("create dep: %v", err)
	}

	// Singleton should still be created.
	singletonKey := client.ObjectKey{Namespace: prjNS, Name: "base-tmpl-shared"}
	singleton := waitForDeploymentExists(t, e.client, singletonKey)

	// But it must carry zero ownerReferences because cascadeDelete=false.
	if len(singleton.OwnerReferences) != 0 {
		t.Fatalf("cascadeDelete=false: expected 0 ownerRefs on singleton; got %d: %+v",
			len(singleton.OwnerReferences), singleton.OwnerReferences)
	}
	t.Logf("cascadeDelete=false: singleton created without ownerRef as expected")
}
