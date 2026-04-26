/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package deployments

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// singletonNameFor binds the test's singleton key to the production helper so
// a future format change to SingletonName fails this test rather than going
// undetected.
func singletonNameFor(name string) string {
	return SingletonName(v1alpha1.LinkedTemplateRef{Name: name})
}

// stubDependencyEdgeProvider satisfies DependencyEdgeProvider for tests. The
// listProject map is keyed by deployment name, mirroring the real
// AppliedRenderStateClient's grouping by SingletonName.
type stubDependencyEdgeProvider struct {
	listProject map[string][]*consolev1.DeploymentDependency
	listProjErr error
	listOneErr  error
}

func (s *stubDependencyEdgeProvider) ListProjectDependencyEdges(_ context.Context, _ string) (map[string][]*consolev1.DeploymentDependency, error) {
	if s.listProjErr != nil {
		return nil, s.listProjErr
	}
	return s.listProject, nil
}

func (s *stubDependencyEdgeProvider) ListDependencyEdgesForDeployment(_ context.Context, _, name string) ([]*consolev1.DeploymentDependency, error) {
	if s.listOneErr != nil {
		return nil, s.listOneErr
	}
	return s.listProject[name], nil
}

// edgeFixture builds a single DeploymentDependency with an originating
// TemplateDependency object reference, suitable for assertions on shape.
func edgeFixture() *consolev1.DeploymentDependency {
	return &consolev1.DeploymentDependency{
		Template: &consolev1.LinkedTemplateRef{
			Namespace: "org-acme",
			Name:      "waypoint",
		},
		OriginatingObject: &consolev1.OriginatingObject{
			Namespace: "prj-my-project",
			Name:      "api-needs-waypoint",
			Kind:      "TemplateDependency",
		},
	}
}

// TestHandler_ListDeployments_PopulatesDependencies asserts that
// ListDeployments attaches dependency edges to the singleton row matched by
// name. A non-singleton row receives no edges.
func TestHandler_ListDeployments_PopulatesDependencies(t *testing.T) {
	ns := projectNS("my-project")
	singleton := singletonNameFor("waypoint")
	cm := deploymentConfigMap("my-project", singleton, "waypoint", "v1", "default", "Waypoint", "shared")
	fakeClient := fake.NewClientset(ns, cm)
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
	provider := &stubDependencyEdgeProvider{
		listProject: map[string][]*consolev1.DeploymentDependency{
			singleton: {edgeFixture()},
		},
	}
	handler := defaultHandler(fakeClient, pr).WithDependencyEdgeProvider(provider)

	ctx := authedCtx("alice@example.com", nil)
	req := connect.NewRequest(&consolev1.ListDeploymentsRequest{Project: "my-project"})
	resp, err := handler.ListDeployments(ctx, req)
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(resp.Msg.Deployments) != 1 {
		t.Fatalf("expected 1 deployment, got %d", len(resp.Msg.Deployments))
	}
	dep := resp.Msg.Deployments[0]
	if len(dep.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency edge on singleton row, got %d", len(dep.Dependencies))
	}
	if got := dep.Dependencies[0].GetOriginatingObject().GetName(); got != "api-needs-waypoint" {
		t.Errorf("OriginatingObject.Name: got %q, want api-needs-waypoint", got)
	}
}

// TestHandler_ListDeployments_DependencyProviderErrorIsNonFatal verifies that
// when the provider returns an error, the listing still succeeds — the
// shared-dependency badge is suppressed but no row is dropped.
func TestHandler_ListDeployments_DependencyProviderErrorIsNonFatal(t *testing.T) {
	ns := projectNS("my-project")
	cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "desc")
	fakeClient := fake.NewClientset(ns, cm)
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
	provider := &stubDependencyEdgeProvider{listProjErr: errors.New("boom")}
	handler := defaultHandler(fakeClient, pr).WithDependencyEdgeProvider(provider)

	ctx := authedCtx("alice@example.com", nil)
	req := connect.NewRequest(&consolev1.ListDeploymentsRequest{Project: "my-project"})
	resp, err := handler.ListDeployments(ctx, req)
	if err != nil {
		t.Fatalf("ListDeployments must tolerate provider error, got %v", err)
	}
	if len(resp.Msg.Deployments) != 1 {
		t.Fatalf("expected 1 deployment, got %d", len(resp.Msg.Deployments))
	}
	if len(resp.Msg.Deployments[0].Dependencies) != 0 {
		t.Errorf("expected no dependencies when provider errors, got %d", len(resp.Msg.Deployments[0].Dependencies))
	}
}

// TestHandler_GetDeployment_DependencyProviderErrorIsNonFatal verifies that
// GetDeployment returns the deployment payload even when the provider fails;
// only the dependencies field is suppressed.
func TestHandler_GetDeployment_DependencyProviderErrorIsNonFatal(t *testing.T) {
	ns := projectNS("my-project")
	singleton := singletonNameFor("waypoint")
	cm := deploymentConfigMap("my-project", singleton, "waypoint", "v1", "default", "Waypoint", "shared")
	fakeClient := fake.NewClientset(ns, cm)
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
	provider := &stubDependencyEdgeProvider{listOneErr: errors.New("boom")}
	handler := defaultHandler(fakeClient, pr).WithDependencyEdgeProvider(provider)

	ctx := authedCtx("alice@example.com", nil)
	req := connect.NewRequest(&consolev1.GetDeploymentRequest{Project: "my-project", Name: singleton})
	resp, err := handler.GetDeployment(ctx, req)
	if err != nil {
		t.Fatalf("GetDeployment must tolerate provider error, got %v", err)
	}
	if len(resp.Msg.Deployment.Dependencies) != 0 {
		t.Errorf("expected no dependencies when provider errors, got %d", len(resp.Msg.Deployment.Dependencies))
	}
}

// TestHandler_GetDeployment_PopulatesDependencies asserts that GetDeployment
// attaches edges for the requested singleton via ListDependencyEdgesForDeployment.
func TestHandler_GetDeployment_PopulatesDependencies(t *testing.T) {
	ns := projectNS("my-project")
	singleton := singletonNameFor("waypoint")
	cm := deploymentConfigMap("my-project", singleton, "waypoint", "v1", "default", "Waypoint", "shared")
	fakeClient := fake.NewClientset(ns, cm)
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
	provider := &stubDependencyEdgeProvider{
		listProject: map[string][]*consolev1.DeploymentDependency{
			singleton: {edgeFixture()},
		},
	}
	handler := defaultHandler(fakeClient, pr).WithDependencyEdgeProvider(provider)

	ctx := authedCtx("alice@example.com", nil)
	req := connect.NewRequest(&consolev1.GetDeploymentRequest{Project: "my-project", Name: singleton})
	resp, err := handler.GetDeployment(ctx, req)
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if len(resp.Msg.Deployment.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency edge, got %d", len(resp.Msg.Deployment.Dependencies))
	}
	if got := resp.Msg.Deployment.Dependencies[0].GetOriginatingObject().GetKind(); got != "TemplateDependency" {
		t.Errorf("OriginatingObject.Kind: got %q, want TemplateDependency", got)
	}
}
