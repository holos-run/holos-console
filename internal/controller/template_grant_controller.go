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

// Package controller — TemplateGrantController.
//
// # Design: informer-backed cache for TemplateGrant validation
//
// TemplateGrantController watches every TemplateGrant and every Namespace in
// the cluster. On each reconcile it rebuilds the full list snapshots and
// pushes them to the TemplateGrantCache defined in
// console/deployments/grant_validator.go. The cache then serves ValidateGrant
// reads under a read lock without any further API server calls.
//
// # Hard-revoke semantics (ADR 035, Decision 10)
//
// When a TemplateGrant is deleted, the reconcile triggered by the delete event
// re-lists all surviving grants and overwrites the cache. From that point on,
// ValidateGrant immediately rejects new cross-namespace materialisation
// attempts. Existing materialised dependency Deployments are preserved — the
// controller does not cascade deletes. Project Owners must clean up orphans
// manually; the deleted grant blocks only new materialisations.
//
// # Namespace informer
//
// The Namespace watch is required for NamespaceSelector evaluation: the cache
// must hold namespace labels so the validator can call
// metav1.LabelSelectorAsSelector without a live API round-trip.
package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	"github.com/holos-run/holos-console/console/deployments"
)

// TemplateGrantReconciler watches TemplateGrant and Namespace objects and
// keeps the supplied TemplateGrantCache current. On each reconcile it
// re-lists all grants and namespaces and calls SetGrants / SetNamespaces so
// the cache is always consistent with the API server's current state.
//
// RBAC markers for this reconciler live on the package doc comment in
// rbac.go — controller-gen's rbac generator ignores markers on struct or
// method doc comments.
type TemplateGrantReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Cache  *deployments.TemplateGrantCache
}

// SetupWithManager registers the reconciler with the supplied manager.
// In addition to the primary For(&TemplateGrant{}) watch it adds a secondary
// Namespace watch: Namespace label changes affect NamespaceSelector evaluation
// inside the cache, so any namespace event must re-sync the cache snapshot.
func (r *TemplateGrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.TemplateGrant{}).
		Named("template-grant-controller").
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.grantsForNamespace),
		).
		Complete(r)
}

// Reconcile re-syncs the TemplateGrantCache whenever a TemplateGrant or
// Namespace event fires. The reconcile key is the TemplateGrant that triggered
// the event; we ignore it and do a full list-sync so the cache is always a
// consistent snapshot of all grants and namespaces.
func (r *TemplateGrantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Re-list all TemplateGrants.
	var grantList v1alpha1.TemplateGrantList
	if err := r.List(ctx, &grantList); err != nil {
		return ctrl.Result{}, fmt.Errorf("list TemplateGrants: %w", err)
	}

	// Re-list all Namespaces.
	var nsList corev1.NamespaceList
	if err := r.List(ctx, &nsList); err != nil {
		return ctrl.Result{}, fmt.Errorf("list Namespaces: %w", err)
	}

	r.Cache.SetGrants(grantList.Items)
	r.Cache.SetNamespaces(nsList.Items)

	logger.V(1).Info("template grant cache refreshed",
		"grants", len(grantList.Items),
		"namespaces", len(nsList.Items),
	)
	return ctrl.Result{}, nil
}

// grantsForNamespace is the watch-handler mapper for Namespace events. It
// returns a reconcile.Request for every TemplateGrant in the cluster so the
// full cache sync runs on any namespace label change.
func (r *TemplateGrantReconciler) grantsForNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	var list v1alpha1.TemplateGrantList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0, len(list.Items))
	for _, g := range list.Items {
		out = append(out, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&g),
		})
	}
	// If there are no grants, still run one synthetic reconcile via a
	// well-known sentinel so the namespace snapshot stays current even
	// when no grants exist. We use an empty NamespacedName; Reconcile
	// re-lists unconditionally so the key value is irrelevant.
	if len(out) == 0 {
		out = append(out, reconcile.Request{})
	}
	return out
}
