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

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	controllerpkg "github.com/holos-run/holos-console/internal/controller"
	"github.com/holos-run/holos-console/console/deployments"
)

// grantRef is a shorthand LinkedTemplateRef constructor for tests.
func grantRef(ns, name string) v1alpha1.LinkedTemplateRef {
	return v1alpha1.LinkedTemplateRef{Namespace: ns, Name: name}
}

// startManagerWithCache constructs and starts a Manager with the provided
// TemplateGrantCache wired into Options.GrantCache. This allows tests to
// observe the same cache the TemplateGrantReconciler updates.
func startManagerWithCache(t *testing.T, cfg *rest.Config, cache *deployments.TemplateGrantCache) (*controllerpkg.Manager, context.CancelFunc, <-chan error) {
	t.Helper()

	m, err := controllerpkg.NewManager(cfg, nil, controllerpkg.Options{
		CacheSyncTimeout:             30 * time.Second,
		SkipControllerNameValidation: true,
		GrantCache:                   cache,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- m.Start(ctx)
	}()

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

// waitForGrantAllowed polls c.ValidateGrant until it returns nil (allowed) or
// the deadline expires.
func waitForGrantAllowed(t *testing.T, c *deployments.TemplateGrantCache, dependent, requires v1alpha1.LinkedTemplateRef) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := c.ValidateGrant(context.Background(), dependent, requires); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("ValidateGrant(%q→%q/%q) never became allowed within deadline",
		dependent.Namespace, requires.Namespace, requires.Name)
}

// waitForGrantDenied polls c.ValidateGrant until it returns a *GrantNotFoundError
// or the deadline expires.
func waitForGrantDenied(t *testing.T, c *deployments.TemplateGrantCache, dependent, requires v1alpha1.LinkedTemplateRef) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		err := c.ValidateGrant(context.Background(), dependent, requires)
		var notFound *deployments.GrantNotFoundError
		if errors.As(err, &notFound) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("ValidateGrant(%q→%q/%q) never became denied within deadline",
		dependent.Namespace, requires.Namespace, requires.Name)
}

// TestTemplateGrantController_CreateResolveDeleteCycle is the headline envtest
// for HOL-958. It exercises the full grant create → resolve → delete →
// re-resolve cycle against a live kube-apiserver:
//
//  1. No grant exists → cross-namespace reference is denied.
//  2. Create TemplateGrant → controller reconciles → reference is allowed.
//  3. Delete TemplateGrant → controller reconciles → reference is denied again
//     (hard-revoke: new materialisations blocked).
func TestTemplateGrantController_CreateResolveDeleteCycle(t *testing.T) {
	e := startEnv(t)

	// Allocate a fresh GrantCache and wire it into the manager via Options.
	grantCache := deployments.NewTemplateGrantCache()
	_, cancel, errCh := startManagerWithCache(t, e.cfg, grantCache)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	// Create the namespaces the grant and references will use.
	orgNS := "org-grant-cycle"
	prjNS := "prj-grant-cycle"
	mustCreateNamespace(t, e.client, orgNS, "organization")
	mustCreateNamespace(t, e.client, prjNS, "project")

	dependent := grantRef(prjNS, "my-deployment")
	requires := grantRef(orgNS, "base-template")

	// --- Stage 1: no grant → denied ----------------------------------------
	if err := grantCache.ValidateGrant(context.Background(), dependent, requires); err == nil {
		t.Fatal("stage 1: expected cross-namespace reference to be denied before grant creation; got nil")
	}

	// --- Stage 2: create grant → allowed ------------------------------------
	grant := &v1alpha1.TemplateGrant{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: orgNS,
			Name:      "allow-prj",
		},
		Spec: v1alpha1.TemplateGrantSpec{
			From: []v1alpha1.TemplateGrantFromRef{
				{Namespace: prjNS},
			},
		},
	}
	if err := e.client.Create(context.Background(), grant); err != nil {
		t.Fatalf("stage 2: create TemplateGrant: %v", err)
	}

	waitForGrantAllowed(t, grantCache, dependent, requires)

	// --- Stage 3: delete grant → denied again (hard-revoke) -----------------
	if err := e.client.Delete(context.Background(), grant); err != nil {
		t.Fatalf("stage 3: delete TemplateGrant: %v", err)
	}

	waitForGrantDenied(t, grantCache, dependent, requires)
}
