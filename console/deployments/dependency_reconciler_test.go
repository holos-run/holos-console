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

package deployments_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	"github.com/holos-run/holos-console/console/deployments"
)

// allowAllValidator is a Validator that always allows cross-namespace refs.
type allowAllValidator struct{}

func (allowAllValidator) ValidateGrant(_ context.Context, _ v1alpha1.LinkedTemplateRef, _ v1alpha1.LinkedTemplateRef) error {
	return nil
}

// denyAllValidator is a Validator that always denies cross-namespace refs.
type denyAllValidator struct{}

func (denyAllValidator) ValidateGrant(_ context.Context, dependent v1alpha1.LinkedTemplateRef, requires v1alpha1.LinkedTemplateRef) error {
	return &deployments.GrantNotFoundError{
		DependentNamespace: dependent.Namespace,
		RequiresNamespace:  requires.Namespace,
		RequiresName:       requires.Name,
	}
}

// buildScheme returns a scheme with the deployments v1alpha1 types registered.
func buildScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := deploymentsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return s
}

// buildDeployment constructs a minimal Deployment with UID set so ownerRef
// equality checks work.
func buildDeployment(namespace, name, templateNS, templateName string) *deploymentsv1alpha1.Deployment {
	return &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID("test-uid-" + name),
		},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: namespace,
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: templateNS,
				Name:      templateName,
			},
		},
	}
}

// TestSingletonName_Formats verifies the singleton naming helper produces the
// expected names for a variety of VersionConstraint values. The name format is
// documented in the package comment of dependency_reconciler.go.
func TestSingletonName_Formats(t *testing.T) {
	// SingletonName is exercised end-to-end here through
	// EnsureSingletonDependencyDeployment, which keeps the assertion close to
	// the user-visible behaviour (the created object's name).
	tests := []struct {
		versionConstraint string
		wantSuffix        string
	}{
		{"", "-shared"},
		{"v1", "-v1-shared"},
		{">=1.0.0 <2.0.0", "-1.0.0-2.0.0-shared"}, // spaces become hyphens
	}

	for _, tc := range tests {
		t.Run(tc.versionConstraint, func(t *testing.T) {
			s := buildScheme(t)
			c := fake.NewClientBuilder().WithScheme(s).Build()
			ns := "prj-alpha"

			dep := buildDeployment(ns, "mcp-server", "org-shared", "waypoint")
			if err := c.Create(context.Background(), dep); err != nil {
				t.Fatalf("create dependent Deployment: %v", err)
			}

			requires := v1alpha1.LinkedTemplateRef{
				Namespace:         "org-shared",
				Name:              "waypoint",
				VersionConstraint: tc.versionConstraint,
			}

			if err := deployments.EnsureSingletonDependencyDeployment(
				context.Background(), c, allowAllValidator{}, requires, dep, true,
			); err != nil {
				t.Fatalf("EnsureSingleton: %v", err)
			}

			var list deploymentsv1alpha1.DeploymentList
			if err := c.List(context.Background(), &list, client.InNamespace(ns)); err != nil {
				t.Fatalf("list Deployments: %v", err)
			}
			found := false
			for _, d := range list.Items {
				if d.Name == dep.Name {
					continue // skip the original dependent
				}
				// Only the singleton should remain.
				want := "waypoint" + tc.wantSuffix
				if d.Name != want {
					t.Errorf("singleton name=%q want %q", d.Name, want)
				}
				found = true
			}
			if !found {
				t.Fatal("no singleton Deployment found after EnsureSingleton")
			}
		})
	}
}

// TestEnsureSingleton_CreateOnFirstCall creates a singleton on first invocation.
func TestEnsureSingleton_CreateOnFirstCall(t *testing.T) {
	s := buildScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	ns := "prj-alpha"

	dep := buildDeployment(ns, "mcp-server", "org-shared", "waypoint")
	if err := c.Create(context.Background(), dep); err != nil {
		t.Fatalf("create dependent: %v", err)
	}

	requires := v1alpha1.LinkedTemplateRef{
		Namespace:         ns, // same-namespace — always allowed
		Name:              "waypoint",
		VersionConstraint: "v1",
	}

	if err := deployments.EnsureSingletonDependencyDeployment(
		context.Background(), c, allowAllValidator{}, requires, dep, true,
	); err != nil {
		t.Fatalf("first call: %v", err)
	}

	var list deploymentsv1alpha1.DeploymentList
	if err := c.List(context.Background(), &list, client.InNamespace(ns)); err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *deploymentsv1alpha1.Deployment
	for i := range list.Items {
		if list.Items[i].Name != dep.Name {
			found = &list.Items[i]
		}
	}
	if found == nil {
		t.Fatal("singleton not found")
	}
	if want := "waypoint-v1-shared"; found.Name != want {
		t.Errorf("singleton name=%q want %q", found.Name, want)
	}
	// OwnerReference must be present and non-controller.
	if len(found.OwnerReferences) != 1 {
		t.Fatalf("len(ownerRefs)=%d want 1", len(found.OwnerReferences))
	}
	ref := found.OwnerReferences[0]
	if ref.UID != dep.UID {
		t.Errorf("ownerRef.UID=%q want %q", ref.UID, dep.UID)
	}
	if ref.Controller != nil && *ref.Controller {
		t.Error("ownerRef.Controller must be false or nil; got true")
	}
	if ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion {
		t.Error("ownerRef.BlockOwnerDeletion must be true")
	}
}

// TestEnsureSingleton_SecondOwnerAppended verifies that a second dependent
// appends its ownerReference to the existing singleton rather than creating
// a second singleton.
func TestEnsureSingleton_SecondOwnerAppended(t *testing.T) {
	s := buildScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	ns := "prj-alpha"

	dep1 := buildDeployment(ns, "mcp-server", ns, "waypoint")
	dep2 := buildDeployment(ns, "mcp-server-2", ns, "waypoint")
	if err := c.Create(context.Background(), dep1); err != nil {
		t.Fatalf("create dep1: %v", err)
	}
	if err := c.Create(context.Background(), dep2); err != nil {
		t.Fatalf("create dep2: %v", err)
	}

	requires := v1alpha1.LinkedTemplateRef{Namespace: ns, Name: "waypoint"}

	// First call: creates the singleton with dep1's ownerRef.
	if err := deployments.EnsureSingletonDependencyDeployment(
		context.Background(), c, allowAllValidator{}, requires, dep1, true,
	); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Second call: should append dep2's ownerRef.
	if err := deployments.EnsureSingletonDependencyDeployment(
		context.Background(), c, allowAllValidator{}, requires, dep2, true,
	); err != nil {
		t.Fatalf("second call: %v", err)
	}

	var singleton deploymentsv1alpha1.Deployment
	key := client.ObjectKey{Namespace: ns, Name: "waypoint-shared"}
	if err := c.Get(context.Background(), key, &singleton); err != nil {
		t.Fatalf("get singleton: %v", err)
	}
	if got := len(singleton.OwnerReferences); got != 2 {
		t.Fatalf("len(ownerRefs)=%d want 2", got)
	}
	uids := map[string]bool{}
	for _, r := range singleton.OwnerReferences {
		uids[string(r.UID)] = true
	}
	if !uids[string(dep1.UID)] {
		t.Error("dep1 UID not found in ownerRefs")
	}
	if !uids[string(dep2.UID)] {
		t.Error("dep2 UID not found in ownerRefs")
	}
}

// TestEnsureSingleton_Idempotent verifies calling EnsureSingleton twice for
// the same dependent does not add a duplicate ownerReference.
func TestEnsureSingleton_Idempotent(t *testing.T) {
	s := buildScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	ns := "prj-alpha"

	dep := buildDeployment(ns, "mcp-server", ns, "waypoint")
	if err := c.Create(context.Background(), dep); err != nil {
		t.Fatalf("create dep: %v", err)
	}

	requires := v1alpha1.LinkedTemplateRef{Namespace: ns, Name: "waypoint"}

	for i := 0; i < 3; i++ {
		if err := deployments.EnsureSingletonDependencyDeployment(
			context.Background(), c, allowAllValidator{}, requires, dep, true,
		); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	var singleton deploymentsv1alpha1.Deployment
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: ns, Name: "waypoint-shared"}, &singleton); err != nil {
		t.Fatalf("get singleton: %v", err)
	}
	if got := len(singleton.OwnerReferences); got != 1 {
		t.Errorf("len(ownerRefs)=%d want 1 (idempotent)", got)
	}
}

// TestEnsureSingleton_CascadeDeleteFalse verifies that cascadeDelete=false
// skips adding an ownerReference to the singleton.
func TestEnsureSingleton_CascadeDeleteFalse(t *testing.T) {
	s := buildScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	ns := "prj-alpha"

	dep := buildDeployment(ns, "mcp-server", ns, "waypoint")
	if err := c.Create(context.Background(), dep); err != nil {
		t.Fatalf("create dep: %v", err)
	}

	requires := v1alpha1.LinkedTemplateRef{Namespace: ns, Name: "waypoint"}

	if err := deployments.EnsureSingletonDependencyDeployment(
		context.Background(), c, allowAllValidator{}, requires, dep, false,
	); err != nil {
		t.Fatalf("EnsureSingleton: %v", err)
	}

	var singleton deploymentsv1alpha1.Deployment
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: ns, Name: "waypoint-shared"}, &singleton); err != nil {
		t.Fatalf("get singleton: %v", err)
	}
	if got := len(singleton.OwnerReferences); got != 0 {
		t.Errorf("len(ownerRefs)=%d want 0 (cascadeDelete=false)", got)
	}
}

// TestEnsureSingleton_CrossNamespaceGrantDenied verifies that a cross-namespace
// Requires reference is rejected when the validator returns GrantNotFoundError.
func TestEnsureSingleton_CrossNamespaceGrantDenied(t *testing.T) {
	s := buildScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	ns := "prj-alpha"

	dep := buildDeployment(ns, "mcp-server", ns, "waypoint")
	if err := c.Create(context.Background(), dep); err != nil {
		t.Fatalf("create dep: %v", err)
	}

	requires := v1alpha1.LinkedTemplateRef{
		Namespace: "org-different", // cross-namespace
		Name:      "waypoint",
	}

	err := deployments.EnsureSingletonDependencyDeployment(
		context.Background(), c, denyAllValidator{}, requires, dep, true,
	)
	if err == nil {
		t.Fatal("expected GrantNotFoundError; got nil")
	}
	var notFound *deployments.GrantNotFoundError
	if !isGrantNotFound(err, &notFound) {
		t.Errorf("expected *GrantNotFoundError; got %T: %v", err, err)
	}

	// No singleton should have been created.
	var list deploymentsv1alpha1.DeploymentList
	if err2 := c.List(context.Background(), &list, client.InNamespace(ns)); err2 != nil {
		t.Fatalf("list: %v", err2)
	}
	// Only the original dependent should exist.
	if got := len(list.Items); got != 1 {
		t.Errorf("Deployment count=%d want 1 (only the dependent)", got)
	}
}

// isGrantNotFound is a type-assertion helper compatible with the unexported
// errors.As logic so the test can remain in the external _test package.
func isGrantNotFound(err error, target **deployments.GrantNotFoundError) bool {
	if err == nil {
		return false
	}
	// errors.As would work too; this is a direct type assertion for clarity.
	if e, ok := err.(*deployments.GrantNotFoundError); ok {
		*target = e
		return true
	}
	return false
}
