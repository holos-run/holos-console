/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package policyresolver

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/deployments"
)

// seedRenderState builds a Deployment-target RenderState with the given
// dependencies in the folder namespace, labelled to match
// ListProjectDependencyEdges' selector.
func seedRenderState(t *testing.T, c ctrlclient.Client, folderNs, project, target string, deps []templatesv1alpha1.RenderStateDependency) {
	t.Helper()
	rs := &templatesv1alpha1.RenderState{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: folderNs,
			Name:      renderStateObjectName(TargetKindDeployment, project, target),
			Labels: map[string]string{
				templatesv1alpha1.RenderStateTargetKindLabel:    string(templatesv1alpha1.RenderTargetKindDeployment),
				templatesv1alpha1.RenderStateTargetProjectLabel: project,
			},
		},
		Spec: templatesv1alpha1.RenderStateSpec{
			Project:      project,
			TargetKind:   templatesv1alpha1.RenderTargetKindDeployment,
			TargetName:   target,
			Dependencies: deps,
		},
	}
	if err := c.Create(context.Background(), rs); err != nil {
		t.Fatalf("seed RenderState %q: %v", rs.Name, err)
	}
}

// TestListProjectDependencyEdges_AggregatesBySingletonName verifies that
// dependency edges from multiple per-Deployment RenderStates are aggregated
// under their deterministic singleton-Deployment name, so the handler can
// attach them to the singleton row in the Deployments list.
func TestListProjectDependencyEdges_AggregatesBySingletonName(t *testing.T) {
	ctx := context.Background()
	r := baseResolver()
	folderEng := r.FolderNamespace("eng")
	orgNs := r.OrgNamespace("acme")
	projectLilies := r.ProjectNamespace("lilies")

	scheme := depsScheme(t)
	objects := []ctrlclient.Object{
		mkNs(orgNs, v1alpha2.ResourceTypeOrganization, ""),
		mkNs(folderEng, v1alpha2.ResourceTypeFolder, orgNs),
		mkNs(projectLilies, v1alpha2.ResourceTypeProject, folderEng),
	}
	client := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	walker := walkerForCtrl(client, r)
	c := NewAppliedRenderStateClient(client, r, walker)

	// Two deployments (api, web) both depend on the waypoint template via a
	// TemplateDependency in the project namespace. A third deployment (worker)
	// requires istio via a TemplateRequirement in the folder namespace.
	waypointDep := templatesv1alpha1.RenderStateDependency{
		Template: templatesv1alpha1.LinkedTemplateRef{Namespace: orgNs, Name: "waypoint"},
		Source:   templatesv1alpha1.RenderStateDependencySourceTemplateDependency,
		OriginatingObject: templatesv1alpha1.RenderStateDependencyOriginatingRef{
			Namespace: projectLilies,
			Name:      "api-needs-waypoint",
			Kind:      templatesv1alpha1.RenderStateDependencySourceTemplateDependency,
		},
	}
	istioDep := templatesv1alpha1.RenderStateDependency{
		Template: templatesv1alpha1.LinkedTemplateRef{Namespace: orgNs, Name: "istio"},
		Source:   templatesv1alpha1.RenderStateDependencySourceTemplateRequirement,
		OriginatingObject: templatesv1alpha1.RenderStateDependencyOriginatingRef{
			Namespace: folderEng,
			Name:      "require-istio",
			Kind:      templatesv1alpha1.RenderStateDependencySourceTemplateRequirement,
		},
	}

	seedRenderState(t, client, folderEng, "lilies", "api", []templatesv1alpha1.RenderStateDependency{waypointDep})
	seedRenderState(t, client, folderEng, "lilies", "web", []templatesv1alpha1.RenderStateDependency{waypointDep})
	seedRenderState(t, client, folderEng, "lilies", "worker", []templatesv1alpha1.RenderStateDependency{istioDep})

	got, err := c.ListProjectDependencyEdges(ctx, projectLilies)
	if err != nil {
		t.Fatalf("ListProjectDependencyEdges: %v", err)
	}

	waypointSingleton := deployments.SingletonName(templatesv1alpha1.LinkedTemplateRef{Namespace: orgNs, Name: "waypoint"})
	istioSingleton := deployments.SingletonName(templatesv1alpha1.LinkedTemplateRef{Namespace: orgNs, Name: "istio"})

	if len(got[waypointSingleton]) != 2 {
		t.Errorf("waypoint singleton edges: got %d, want 2 (one per dependent)", len(got[waypointSingleton]))
	}
	if len(got[istioSingleton]) != 1 {
		t.Errorf("istio singleton edges: got %d, want 1", len(got[istioSingleton]))
	}
	for _, e := range got[waypointSingleton] {
		if e.GetOriginatingObject().GetKind() != string(templatesv1alpha1.RenderStateDependencySourceTemplateDependency) {
			t.Errorf("waypoint edge OriginatingObject.Kind: got %q", e.GetOriginatingObject().GetKind())
		}
		if e.GetOriginatingObject().GetName() != "api-needs-waypoint" {
			t.Errorf("waypoint edge OriginatingObject.Name: got %q", e.GetOriginatingObject().GetName())
		}
	}
}

// TestListProjectDependencyEdges_EmptyProject confirms that a project with no
// RenderStates returns an empty (non-nil) map without error.
func TestListProjectDependencyEdges_EmptyProject(t *testing.T) {
	ctx := context.Background()
	r := baseResolver()
	folderEng := r.FolderNamespace("eng")
	orgNs := r.OrgNamespace("acme")
	projectLilies := r.ProjectNamespace("lilies")

	scheme := depsScheme(t)
	objects := []ctrlclient.Object{
		mkNs(orgNs, v1alpha2.ResourceTypeOrganization, ""),
		mkNs(folderEng, v1alpha2.ResourceTypeFolder, orgNs),
		mkNs(projectLilies, v1alpha2.ResourceTypeProject, folderEng),
	}
	client := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	c := NewAppliedRenderStateClient(client, r, walkerForCtrl(client, r))

	got, err := c.ListProjectDependencyEdges(ctx, projectLilies)
	if err != nil {
		t.Fatalf("ListProjectDependencyEdges: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries", len(got))
	}
}

// TestListProjectDependencyEdges_NilClientReturnsNil verifies the fail-open
// path: a client constructed without a controller-runtime backend returns
// nil, nil so call sites in dry-run/test modes do not need conditionals.
func TestListProjectDependencyEdges_NilClientReturnsNil(t *testing.T) {
	var c *AppliedRenderStateClient
	got, err := c.ListProjectDependencyEdges(context.Background(), "any-ns")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil map, got %v", got)
	}
}

// TestListDependencyEdgesForDeployment_Found returns the edges for one
// singleton Deployment by name. Confirms the single-deployment helper picks
// the correct slice from the project-wide map.
func TestListDependencyEdgesForDeployment_Found(t *testing.T) {
	ctx := context.Background()
	r := baseResolver()
	folderEng := r.FolderNamespace("eng")
	orgNs := r.OrgNamespace("acme")
	projectLilies := r.ProjectNamespace("lilies")

	scheme := depsScheme(t)
	objects := []ctrlclient.Object{
		mkNs(orgNs, v1alpha2.ResourceTypeOrganization, ""),
		mkNs(folderEng, v1alpha2.ResourceTypeFolder, orgNs),
		mkNs(projectLilies, v1alpha2.ResourceTypeProject, folderEng),
	}
	client := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	c := NewAppliedRenderStateClient(client, r, walkerForCtrl(client, r))

	dep := templatesv1alpha1.RenderStateDependency{
		Template: templatesv1alpha1.LinkedTemplateRef{Namespace: orgNs, Name: "waypoint"},
		Source:   templatesv1alpha1.RenderStateDependencySourceTemplateDependency,
		OriginatingObject: templatesv1alpha1.RenderStateDependencyOriginatingRef{
			Namespace: projectLilies,
			Name:      "api-needs-waypoint",
			Kind:      templatesv1alpha1.RenderStateDependencySourceTemplateDependency,
		},
	}
	seedRenderState(t, client, folderEng, "lilies", "api", []templatesv1alpha1.RenderStateDependency{dep})

	singleton := deployments.SingletonName(templatesv1alpha1.LinkedTemplateRef{Namespace: orgNs, Name: "waypoint"})
	got, err := c.ListDependencyEdgesForDeployment(ctx, projectLilies, singleton)
	if err != nil {
		t.Fatalf("ListDependencyEdgesForDeployment: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d edges, want 1", len(got))
	}
	if got[0].GetTemplate().GetName() != "waypoint" {
		t.Errorf("Template.Name: got %q, want waypoint", got[0].GetTemplate().GetName())
	}
}

// TestListDependencyEdgesForDeployment_Missing returns nil when the requested
// deployment name is not a singleton (no edges aggregated for it).
func TestListDependencyEdgesForDeployment_Missing(t *testing.T) {
	ctx := context.Background()
	r := baseResolver()
	folderEng := r.FolderNamespace("eng")
	orgNs := r.OrgNamespace("acme")
	projectLilies := r.ProjectNamespace("lilies")

	scheme := depsScheme(t)
	objects := []ctrlclient.Object{
		mkNs(orgNs, v1alpha2.ResourceTypeOrganization, ""),
		mkNs(folderEng, v1alpha2.ResourceTypeFolder, orgNs),
		mkNs(projectLilies, v1alpha2.ResourceTypeProject, folderEng),
	}
	client := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	c := NewAppliedRenderStateClient(client, r, walkerForCtrl(client, r))

	got, err := c.ListDependencyEdgesForDeployment(ctx, projectLilies, "user-named-app")
	if err != nil {
		t.Fatalf("ListDependencyEdgesForDeployment: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-singleton deployment, got %v", got)
	}
}
