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

// render_project_namespace_examples_test.go verifies that the two embedded
// ProjectNamespace example templates (HOL-813) produce the expected output
// when rendered through RenderForProjectNamespace. Each test mirrors what an
// operator would see after wiring a TemplatePolicyBinding with
// targetRefs.kind=ProjectNamespace.
//
// Test data: the cueTemplate strings are copied verbatim from the registry CUE
// files so that changes to the examples surface as test failures here too.

import (
	"context"
	"testing"
)

// descriptionAnnotationTemplate is the cueTemplate body from
// console/templates/examples/project-namespace-description-annotation-v1.cue.
const descriptionAnnotationTemplate = `
platform: #PlatformInput

platformResources: {
	clusterResources: {
		Namespace: (platform.namespace): {
			apiVersion: "v1"
			kind:       "Namespace"
			metadata: {
				name: platform.namespace
				annotations: {
					"console.holos.run/description": "Managed by the Holos platform team."
				}
			}
		}
	}
	namespacedResources: {}
}
`

// TestRenderProjectNamespace_DescriptionAnnotationExample verifies that the
// description-annotation example template (project-namespace-description-
// annotation-v1) unifies a single "console.holos.run/description" annotation
// onto the new project namespace. No other resources are emitted.
//
// AC (HOL-813): rendering template 1 with a fixed project name produces a
// namespace with the expected annotation key and value.
func TestRenderProjectNamespace_DescriptionAnnotationExample(t *testing.T) {
	adapter := NewCueRendererAdapter()
	in := projectNamespaceInputs("my-project", "holos-prj-my-project", []string{
		descriptionAnnotationTemplate,
	})

	got, err := adapter.RenderForProjectNamespace(context.Background(), in)
	if err != nil {
		t.Fatalf("RenderForProjectNamespace error: %v", err)
	}

	// The template produces only a Namespace patch — no other resources.
	if len(got.ClusterScoped) != 0 {
		t.Errorf("expected 0 cluster-scoped resources, got %d", len(got.ClusterScoped))
	}
	if len(got.NamespaceScoped) != 0 {
		t.Errorf("expected 0 namespace-scoped resources, got %d", len(got.NamespaceScoped))
	}

	// The expected annotation must be present on the unified Namespace.
	anns := got.Namespace.GetAnnotations()
	const (
		wantKey = "console.holos.run/description"
		wantVal = "Managed by the Holos platform team."
	)
	if got := anns[wantKey]; got != wantVal {
		t.Errorf("Namespace annotation %q = %q, want %q", wantKey, got, wantVal)
	}

	// Base annotations set by the RPC must be preserved — the merge is additive.
	if anns["console.holos.run/display-name"] != "My Project" {
		t.Errorf("expected base annotation console.holos.run/display-name=My Project preserved, got %q",
			anns["console.holos.run/display-name"])
	}

	// The namespace name and identity must be unchanged.
	if got.Namespace.GetName() != "holos-prj-my-project" {
		t.Errorf("Namespace name = %q, want holos-prj-my-project", got.Namespace.GetName())
	}
}

// referenceGrantTemplate is the cueTemplate body from
// console/templates/examples/project-namespace-reference-grant-v1.cue.
const referenceGrantTemplate = `
platform: #PlatformInput

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

// TestRenderProjectNamespace_ReferenceGrantExample verifies that the
// reference-grant example template (project-namespace-reference-grant-v1)
// emits a single ReferenceGrant in the new project namespace. The cluster-
// scoped bucket must be empty and the namespace must be the project namespace.
//
// AC (HOL-813): rendering template 2 produces a ReferenceGrant with
// metadata.namespace set to the new project's namespace.
func TestRenderProjectNamespace_ReferenceGrantExample(t *testing.T) {
	adapter := NewCueRendererAdapter()
	in := projectNamespaceInputs("my-project", "holos-prj-my-project", []string{
		referenceGrantTemplate,
	})

	got, err := adapter.RenderForProjectNamespace(context.Background(), in)
	if err != nil {
		t.Fatalf("RenderForProjectNamespace error: %v", err)
	}

	// The template produces one namespace-scoped ReferenceGrant only.
	if len(got.ClusterScoped) != 0 {
		t.Errorf("expected 0 cluster-scoped resources, got %d", len(got.ClusterScoped))
	}
	if len(got.NamespaceScoped) != 1 {
		t.Fatalf("expected 1 namespace-scoped resource, got %d", len(got.NamespaceScoped))
	}

	rg := got.NamespaceScoped[0]

	// Kind and apiVersion must match the Gateway API ReferenceGrant.
	if rg.GetKind() != "ReferenceGrant" {
		t.Errorf("resource kind = %q, want ReferenceGrant", rg.GetKind())
	}
	if rg.GetAPIVersion() != "gateway.networking.k8s.io/v1beta1" {
		t.Errorf("resource apiVersion = %q, want gateway.networking.k8s.io/v1beta1", rg.GetAPIVersion())
	}

	// metadata.namespace must equal the new project namespace.
	if rg.GetNamespace() != "holos-prj-my-project" {
		t.Errorf("ReferenceGrant namespace = %q, want holos-prj-my-project", rg.GetNamespace())
	}

	// metadata.name must be set.
	if rg.GetName() != "allow-from-gateway" {
		t.Errorf("ReferenceGrant name = %q, want allow-from-gateway", rg.GetName())
	}
}
