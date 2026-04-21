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

package templates

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

// projectNamespaceBase builds the RPC-constructed base Namespace the
// RenderForProjectNamespace tests unify with template patches. Mirrors the
// shape CreateProject produces in console/projects/k8s.go:149.
func projectNamespaceBase(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":    "console.holos.run",
				"console.holos.run/resource-type": "project",
				"console.holos.run/project":       "my-project",
			},
			Annotations: map[string]string{
				"console.holos.run/display-name": "My Project",
			},
		},
	}
}

// projectNamespaceInputs builds a ProjectNamespaceRenderInput pointing at
// the given template sources. The platform input uses fixed values that
// match what the CreateProject RPC wire-up (HOL-812) will supply.
func projectNamespaceInputs(projectName, namespaceName string, sources []string) ProjectNamespaceRenderInput {
	return ProjectNamespaceRenderInput{
		ProjectName:   projectName,
		NamespaceName: namespaceName,
		Platform: v1alpha2.PlatformInput{
			Project:          projectName,
			Namespace:        namespaceName,
			GatewayNamespace: "istio-ingress",
			Organization:     "acme",
			Claims: v1alpha2.Claims{
				Iss: "https://dex.example.com",
				Sub: "u1", Exp: 9999999999, Iat: 1700000000,
				Email: "creator@example.com", EmailVerified: true,
			},
		},
		TemplateSources: sources,
		BaseNamespace:   projectNamespaceBase(namespaceName),
	}
}

// TestRenderForProjectNamespace_EmptyBindings covers AC (a): an empty
// template set returns the base Namespace unchanged with zero additional
// resources.
func TestRenderForProjectNamespace_EmptyBindings(t *testing.T) {
	adapter := NewCueRendererAdapter()
	in := projectNamespaceInputs("my-project", "holos-prj-my-project", nil)

	got, err := adapter.RenderForProjectNamespace(context.Background(), in)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Namespace == nil {
		t.Fatal("expected Namespace to be populated from BaseNamespace")
	}
	if got.Namespace.GetName() != "holos-prj-my-project" {
		t.Errorf("expected Namespace name 'holos-prj-my-project', got %q", got.Namespace.GetName())
	}
	if got.Namespace.GetAPIVersion() != "v1" {
		t.Errorf("expected apiVersion v1, got %q", got.Namespace.GetAPIVersion())
	}
	if got.Namespace.GetKind() != "Namespace" {
		t.Errorf("expected kind Namespace, got %q", got.Namespace.GetKind())
	}
	labels := got.Namespace.GetLabels()
	if labels["app.kubernetes.io/managed-by"] != "console.holos.run" {
		t.Errorf("expected managed-by label preserved, got %q", labels["app.kubernetes.io/managed-by"])
	}
	if len(got.ClusterScoped) != 0 {
		t.Errorf("expected no cluster-scoped resources, got %d", len(got.ClusterScoped))
	}
	if len(got.NamespaceScoped) != 0 {
		t.Errorf("expected no namespace-scoped resources, got %d", len(got.NamespaceScoped))
	}
}

// projectNamespaceAnnotationTemplate emits a Namespace object under
// platformResources.clusterResources that adds a single annotation. The
// Namespace name is parameterised on platform.namespace so the template
// is reusable across every project under the ancestor binding.
const projectNamespaceAnnotationTemplate = `
platformResources: {
	namespacedResources: {}
	clusterResources: {
		Namespace: (platform.namespace): {
			apiVersion: "v1"
			kind:       "Namespace"
			metadata: {
				name: platform.namespace
				annotations: {
					"example.com/owner": "platform-team"
				}
			}
		}
	}
}
`

// TestRenderForProjectNamespace_AnnotationPatch covers AC (b): a template
// that produces a Namespace with one annotation results in that
// annotation being present on the unified Namespace, alongside the labels
// and annotations already set on the base.
func TestRenderForProjectNamespace_AnnotationPatch(t *testing.T) {
	adapter := NewCueRendererAdapter()
	in := projectNamespaceInputs("my-project", "holos-prj-my-project", []string{
		projectNamespaceAnnotationTemplate,
	})

	got, err := adapter.RenderForProjectNamespace(context.Background(), in)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	anns := got.Namespace.GetAnnotations()
	if anns["example.com/owner"] != "platform-team" {
		t.Errorf("expected template annotation example.com/owner=platform-team, got %q",
			anns["example.com/owner"])
	}
	// The base annotation must still be present — the merge is additive.
	if anns["console.holos.run/display-name"] != "My Project" {
		t.Errorf("expected base annotation preserved, got %q",
			anns["console.holos.run/display-name"])
	}
	// Namespace itself must not leak into the ClusterScoped group; it is
	// always merged into result.Namespace.
	for _, u := range got.ClusterScoped {
		if u.GetKind() == "Namespace" {
			t.Errorf("Namespace unexpectedly leaked into ClusterScoped: %s/%s",
				u.GetKind(), u.GetName())
		}
	}
	if len(got.NamespaceScoped) != 0 {
		t.Errorf("expected no namespace-scoped resources, got %d", len(got.NamespaceScoped))
	}
}

// projectNamespaceReferenceGrantTemplate emits a namespace-scoped
// ReferenceGrant under platformResources.namespacedResources. The
// ReferenceGrant targets the gateway namespace so it models the
// canonical ADR 034 use case — letting a newly-provisioned project's
// HTTPRoutes reference a Service sitting in istio-ingress.
const projectNamespaceReferenceGrantTemplate = `
platformResources: {
	namespacedResources: (platform.namespace): {
		ReferenceGrant: "allow-from-gateway": {
			apiVersion: "gateway.networking.k8s.io/v1beta1"
			kind:       "ReferenceGrant"
			metadata: {
				name:      "allow-from-gateway"
				namespace: platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
				}
			}
			spec: {
				from: [{
					group:     "gateway.networking.k8s.io"
					kind:      "HTTPRoute"
					namespace: platform.gatewayNamespace
				}]
				to: [{
					group: ""
					kind:  "Service"
				}]
			}
		}
	}
	clusterResources: {}
}
`

// TestRenderForProjectNamespace_ReferenceGrant covers AC (c): a template
// producing a ReferenceGrant returns it in the NamespaceScoped group with
// metadata.namespace equal to the new project's namespace.
func TestRenderForProjectNamespace_ReferenceGrant(t *testing.T) {
	adapter := NewCueRendererAdapter()
	in := projectNamespaceInputs("my-project", "holos-prj-my-project", []string{
		projectNamespaceReferenceGrantTemplate,
	})

	got, err := adapter.RenderForProjectNamespace(context.Background(), in)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got.NamespaceScoped) != 1 {
		t.Fatalf("expected 1 namespace-scoped resource, got %d", len(got.NamespaceScoped))
	}
	rg := got.NamespaceScoped[0]
	if rg.GetKind() != "ReferenceGrant" {
		t.Errorf("expected ReferenceGrant kind, got %q", rg.GetKind())
	}
	if rg.GetName() != "allow-from-gateway" {
		t.Errorf("expected name 'allow-from-gateway', got %q", rg.GetName())
	}
	if rg.GetNamespace() != "holos-prj-my-project" {
		t.Errorf("expected namespace 'holos-prj-my-project', got %q", rg.GetNamespace())
	}
	if len(got.ClusterScoped) != 0 {
		t.Errorf("expected 0 cluster-scoped resources, got %d", len(got.ClusterScoped))
	}
}

// projectNamespaceLabelConflictA sets a namespace label to "team-a".
const projectNamespaceLabelConflictA = `
platformResources: {
	namespacedResources: {}
	clusterResources: {
		Namespace: (platform.namespace): {
			apiVersion: "v1"
			kind:       "Namespace"
			metadata: {
				name: platform.namespace
				labels: {
					"example.com/team": "team-a"
				}
			}
		}
	}
}
`

// projectNamespaceLabelConflictB sets the same namespace label to
// "team-b". Rendering both templates together must fail the merge.
const projectNamespaceLabelConflictB = `
platformResources: {
	namespacedResources: {}
	clusterResources: {
		Namespace: (platform.namespace): {
			apiVersion: "v1"
			kind:       "Namespace"
			metadata: {
				name: platform.namespace
				labels: {
					"example.com/team": "team-b"
				}
			}
		}
	}
}
`

// TestRenderForProjectNamespace_ConflictFailsRender covers AC (d): two
// templates that independently set metadata.labels["example.com/team"] to
// different values must fail the render with a descriptive error.
func TestRenderForProjectNamespace_ConflictFailsRender(t *testing.T) {
	adapter := NewCueRendererAdapter()
	in := projectNamespaceInputs("my-project", "holos-prj-my-project", []string{
		projectNamespaceLabelConflictA,
		projectNamespaceLabelConflictB,
	})

	_, err := adapter.RenderForProjectNamespace(context.Background(), in)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "example.com/team") {
		t.Errorf("expected error to mention conflicting label key; got %v", err)
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Errorf("expected error to mention 'conflict'; got %v", err)
	}
}

// TestRenderForProjectNamespace_SameKeySameValueIsNoop verifies the
// unification short-circuit: two templates setting the same label to the
// same value merge without error. This complements the conflict test —
// the ADR defines the behavior explicitly ("same key and same value is a
// no-op").
func TestRenderForProjectNamespace_SameKeySameValueIsNoop(t *testing.T) {
	adapter := NewCueRendererAdapter()
	in := projectNamespaceInputs("my-project", "holos-prj-my-project", []string{
		projectNamespaceLabelConflictA,
		projectNamespaceLabelConflictA,
	})

	got, err := adapter.RenderForProjectNamespace(context.Background(), in)
	if err != nil {
		t.Fatalf("expected no error for same-value merge, got %v", err)
	}
	labels := got.Namespace.GetLabels()
	if labels["example.com/team"] != "team-a" {
		t.Errorf("expected merged label example.com/team=team-a, got %q",
			labels["example.com/team"])
	}
}

// TestRenderForProjectNamespace_BaseLabelPreservedOnTemplateMerge ensures
// labels on the RPC-built base Namespace are preserved when a template
// adds new labels — the merge is additive, not replacement.
func TestRenderForProjectNamespace_BaseLabelPreservedOnTemplateMerge(t *testing.T) {
	adapter := NewCueRendererAdapter()
	in := projectNamespaceInputs("my-project", "holos-prj-my-project", []string{
		projectNamespaceLabelConflictA,
	})

	got, err := adapter.RenderForProjectNamespace(context.Background(), in)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	labels := got.Namespace.GetLabels()
	// Base label from projectNamespaceBase
	if labels["app.kubernetes.io/managed-by"] != "console.holos.run" {
		t.Errorf("expected base label preserved, got %q", labels["app.kubernetes.io/managed-by"])
	}
	// Template-added label
	if labels["example.com/team"] != "team-a" {
		t.Errorf("expected template label present, got %q", labels["example.com/team"])
	}
}

// projectNamespaceClusterRoleTemplate emits a ClusterRole under
// platformResources.clusterResources. A ClusterRole is in the
// cluster-scoped kind list independent of CUE placement so it lands in
// ClusterScoped rather than NamespaceScoped.
const projectNamespaceClusterRoleTemplate = `
platformResources: {
	namespacedResources: {}
	clusterResources: {
		ClusterRole: "project-reader": {
			apiVersion: "rbac.authorization.k8s.io/v1"
			kind:       "ClusterRole"
			metadata: {
				name: "project-reader"
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
				}
			}
			rules: [{
				apiGroups: [""]
				resources: ["namespaces"]
				verbs: ["get", "list"]
			}]
		}
	}
}
`

// TestRenderForProjectNamespace_ClusterScopedRouting verifies a
// cluster-scoped kind (ClusterRole) emitted via
// platformResources.clusterResources lands in result.ClusterScoped — the
// "apply before the namespace" bucket.
func TestRenderForProjectNamespace_ClusterScopedRouting(t *testing.T) {
	adapter := NewCueRendererAdapter()
	in := projectNamespaceInputs("my-project", "holos-prj-my-project", []string{
		projectNamespaceClusterRoleTemplate,
	})

	got, err := adapter.RenderForProjectNamespace(context.Background(), in)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got.ClusterScoped) != 1 {
		t.Fatalf("expected 1 cluster-scoped resource, got %d", len(got.ClusterScoped))
	}
	cr := got.ClusterScoped[0]
	if cr.GetKind() != "ClusterRole" {
		t.Errorf("expected ClusterRole kind, got %q", cr.GetKind())
	}
	if cr.GetNamespace() != "" {
		t.Errorf("expected cluster-scoped resource to have no namespace, got %q", cr.GetNamespace())
	}
	if len(got.NamespaceScoped) != 0 {
		t.Errorf("expected 0 namespace-scoped resources, got %d", len(got.NamespaceScoped))
	}
}

// TestRenderForProjectNamespace_IgnoresProjectResources verifies ADR 034
// Decision 2: a template that emits resources under projectResources (the
// project-level CUE half) is ignored by the ProjectNamespace render path.
func TestRenderForProjectNamespace_IgnoresProjectResources(t *testing.T) {
	adapter := NewCueRendererAdapter()
	in := projectNamespaceInputs("my-project", "holos-prj-my-project", []string{
		`
projectResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: "project-sa": {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      "project-sa"
				namespace: platform.namespace
				labels: "app.kubernetes.io/managed-by": "console.holos.run"
			}
		}
	}
	clusterResources: {}
}

platformResources: {
	namespacedResources: {}
	clusterResources: {}
}
`,
	})

	got, err := adapter.RenderForProjectNamespace(context.Background(), in)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got.ClusterScoped) != 0 {
		t.Errorf("expected 0 cluster-scoped resources (projectResources must be ignored), got %d",
			len(got.ClusterScoped))
	}
	if len(got.NamespaceScoped) != 0 {
		t.Errorf("expected 0 namespace-scoped resources (projectResources must be ignored), got %d",
			len(got.NamespaceScoped))
	}
}

// TestRenderForProjectNamespace_MetadataFinalizersUnion verifies the
// finalizer merge path: two templates declaring different finalizers at
// metadata.finalizers union their values (no conflict error) because
// finalizers are additive — every declared finalizer must run before the
// Namespace is deleted.
func TestRenderForProjectNamespace_MetadataFinalizersUnion(t *testing.T) {
	adapter := NewCueRendererAdapter()
	finalizerA := `
platformResources: {
	namespacedResources: {}
	clusterResources: {
		Namespace: (platform.namespace): {
			apiVersion: "v1"
			kind:       "Namespace"
			metadata: {
				name:       platform.namespace
				finalizers: ["example.com/a"]
			}
		}
	}
}
`
	finalizerB := `
platformResources: {
	namespacedResources: {}
	clusterResources: {
		Namespace: (platform.namespace): {
			apiVersion: "v1"
			kind:       "Namespace"
			metadata: {
				name:       platform.namespace
				finalizers: ["example.com/b"]
			}
		}
	}
}
`
	in := projectNamespaceInputs("my-project", "holos-prj-my-project", []string{finalizerA, finalizerB})

	got, err := adapter.RenderForProjectNamespace(context.Background(), in)
	if err != nil {
		t.Fatalf("expected no error for metadata.finalizers merge, got %v", err)
	}
	finalizers, _, err := unstructured.NestedStringSlice(got.Namespace.Object, "metadata", "finalizers")
	if err != nil {
		t.Fatalf("reading metadata.finalizers: %v", err)
	}
	haveA, haveB := false, false
	for _, f := range finalizers {
		if f == "example.com/a" {
			haveA = true
		}
		if f == "example.com/b" {
			haveB = true
		}
	}
	if !haveA || !haveB {
		t.Errorf("expected both finalizers in metadata.finalizers; got %v", finalizers)
	}
}

// TestRenderForProjectNamespace_InvalidInputs covers misuse paths: the
// CreateProject wire-up forgetting to populate a required field should
// surface a descriptive error rather than an obscure CUE failure.
func TestRenderForProjectNamespace_InvalidInputs(t *testing.T) {
	adapter := NewCueRendererAdapter()
	tests := []struct {
		name    string
		mutate  func(*ProjectNamespaceRenderInput)
		wantMsg string
	}{
		{
			name:    "missing project name",
			mutate:  func(in *ProjectNamespaceRenderInput) { in.ProjectName = "" },
			wantMsg: "ProjectName",
		},
		{
			name:    "missing namespace name",
			mutate:  func(in *ProjectNamespaceRenderInput) { in.NamespaceName = "" },
			wantMsg: "NamespaceName",
		},
		{
			name:    "missing base namespace",
			mutate:  func(in *ProjectNamespaceRenderInput) { in.BaseNamespace = nil },
			wantMsg: "BaseNamespace",
		},
		{
			name: "base namespace name mismatch",
			mutate: func(in *ProjectNamespaceRenderInput) {
				in.BaseNamespace = projectNamespaceBase("holos-prj-wrong")
			},
			wantMsg: "does not match NamespaceName",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := projectNamespaceInputs("my-project", "holos-prj-my-project", nil)
			tc.mutate(&in)
			_, err := adapter.RenderForProjectNamespace(context.Background(), in)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("expected error to contain %q, got %v", tc.wantMsg, err)
			}
		})
	}
}

// TestRenderForProjectNamespace_MultipleTemplatesMerge verifies that two
// templates each contributing a different annotation merge into a single
// Namespace object with both annotations.
func TestRenderForProjectNamespace_MultipleTemplatesMerge(t *testing.T) {
	adapter := NewCueRendererAdapter()
	templateOwner := `
platformResources: {
	namespacedResources: {}
	clusterResources: {
		Namespace: (platform.namespace): {
			apiVersion: "v1"
			kind:       "Namespace"
			metadata: {
				name: platform.namespace
				annotations: "example.com/owner": "platform-team"
			}
		}
	}
}
`
	templateCostCenter := `
platformResources: {
	namespacedResources: {}
	clusterResources: {
		Namespace: (platform.namespace): {
			apiVersion: "v1"
			kind:       "Namespace"
			metadata: {
				name: platform.namespace
				annotations: "example.com/cost-center": "eng-platform"
			}
		}
	}
}
`
	in := projectNamespaceInputs("my-project", "holos-prj-my-project", []string{templateOwner, templateCostCenter})

	got, err := adapter.RenderForProjectNamespace(context.Background(), in)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	anns := got.Namespace.GetAnnotations()
	if anns["example.com/owner"] != "platform-team" {
		t.Errorf("expected owner annotation from first template, got %q", anns["example.com/owner"])
	}
	if anns["example.com/cost-center"] != "eng-platform" {
		t.Errorf("expected cost-center annotation from second template, got %q", anns["example.com/cost-center"])
	}
}
