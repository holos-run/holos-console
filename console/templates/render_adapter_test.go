package templates

import (
	"context"
	"strings"
	"testing"
)

// adapterStructuredTemplate uses the namespaced/cluster structured output format
// with the new system/input split structure.
const adapterStructuredTemplate = `
package deployment

input: {
	name:  string
	image: string
	tag:   string
}

system: {
	project:   string
	namespace: string
}

_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

namespaced: (system.namespace): {
	ServiceAccount: (input.name): {
		apiVersion: "v1"
		kind:       "ServiceAccount"
		metadata: {
			name:      input.name
			namespace: system.namespace
			labels:    _labels
		}
	}
	Deployment: (input.name): {
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: {
			name:      input.name
			namespace: system.namespace
			labels:    _labels
		}
		spec: {
			selector: matchLabels: "app.kubernetes.io/name": input.name
			template: {
				metadata: labels: _labels
				spec: {
					serviceAccountName: input.name
					containers: [{
						name:  input.name
						image: input.image + ":" + input.tag
					}]
				}
			}
		}
	}
}

cluster: {}
`

// adapterInvalidTemplate contains invalid CUE syntax.
const adapterInvalidTemplate = `this is { not valid cue !!!`

// adapterCrossNamespaceTemplate tries to place a resource in a different namespace.
const adapterCrossNamespaceTemplate = `
package deployment

input: {
	name:  string
	image: string
	tag:   string
}

system: {
	project:   string
	namespace: string
}

namespaced: (system.namespace): {
	Deployment: (input.name): {
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: {
			name:      input.name
			namespace: "other-namespace"
			labels: "app.kubernetes.io/managed-by": "console.holos.run"
		}
		spec: {}
	}
}

cluster: {}
`

// cueInput builds a CUE input string for the adapter tests using the new system/input split.
func cueInput(name, image, tag, project, namespace string) string {
	return `input: {
	name:  "` + name + `"
	image: "` + image + `"
	tag:   "` + tag + `"
}
system: {
	project:   "` + project + `"
	namespace: "` + namespace + `"
}`
}

func TestCueRendererAdapter_Render(t *testing.T) {
	adapter := NewCueRendererAdapter()
	namespace := "prj-my-project"

	t.Run("structured template produces YAML resources", func(t *testing.T) {
		resources, err := adapter.Render(context.Background(), adapterStructuredTemplate,
			cueInput("web-app", "nginx", "1.25", "my-project", namespace))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 2 {
			t.Fatalf("expected 2 resources (ServiceAccount, Deployment), got %d", len(resources))
		}

		// Each resource must have non-empty YAML.
		for i, r := range resources {
			if r.YAML == "" {
				t.Errorf("resource %d: expected non-empty YAML", i)
			}
		}

		// Collect YAML to verify resource types are present.
		allYAML := resources[0].YAML + resources[1].YAML
		if !strings.Contains(allYAML, "ServiceAccount") {
			t.Error("expected YAML to contain ServiceAccount")
		}
		if !strings.Contains(allYAML, "Deployment") {
			t.Error("expected YAML to contain Deployment")
		}
	})

	t.Run("input values are reflected in rendered YAML", func(t *testing.T) {
		resources, err := adapter.Render(context.Background(), adapterStructuredTemplate,
			cueInput("my-app", "myrepo/myapp", "v2.0.0", "my-project", namespace))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 2 {
			t.Fatalf("expected 2 resources, got %d", len(resources))
		}

		allYAML := resources[0].YAML + resources[1].YAML
		if !strings.Contains(allYAML, "my-app") {
			t.Error("expected YAML to contain resource name 'my-app'")
		}
		if !strings.Contains(allYAML, "myrepo/myapp:v2.0.0") {
			t.Error("expected YAML to contain image 'myrepo/myapp:v2.0.0'")
		}
		if !strings.Contains(allYAML, namespace) {
			t.Errorf("expected YAML to contain namespace %q", namespace)
		}
	})

	t.Run("invalid CUE template syntax returns error", func(t *testing.T) {
		_, err := adapter.Render(context.Background(), adapterInvalidTemplate,
			cueInput("web-app", "nginx", "1.25", "my-project", namespace))
		if err == nil {
			t.Fatal("expected error for invalid CUE syntax")
		}
	})

	t.Run("invalid CUE input syntax returns error", func(t *testing.T) {
		_, err := adapter.Render(context.Background(), adapterStructuredTemplate, `this is { not valid cue !!!`)
		if err == nil {
			t.Fatal("expected error for invalid CUE input syntax")
		}
	})

	t.Run("cross-namespace resource rejected", func(t *testing.T) {
		_, err := adapter.Render(context.Background(), adapterCrossNamespaceTemplate,
			cueInput("web-app", "nginx", "1.25", "my-project", namespace))
		if err == nil {
			t.Fatal("expected error for cross-namespace resource")
		}
	})

	t.Run("each resource YAML is valid YAML document", func(t *testing.T) {
		resources, err := adapter.Render(context.Background(), adapterStructuredTemplate,
			cueInput("web-app", "nginx", "1.25", "my-project", namespace))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		for i, r := range resources {
			if !strings.Contains(r.YAML, "apiVersion:") {
				t.Errorf("resource %d: YAML missing apiVersion field", i)
			}
			if !strings.Contains(r.YAML, "kind:") {
				t.Errorf("resource %d: YAML missing kind field", i)
			}
			if !strings.Contains(r.YAML, "metadata:") {
				t.Errorf("resource %d: YAML missing metadata field", i)
			}
		}
	})

	t.Run("each resource has non-nil Object for JSON serialization", func(t *testing.T) {
		resources, err := adapter.Render(context.Background(), adapterStructuredTemplate,
			cueInput("web-app", "nginx", "1.25", "my-project", namespace))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		for i, r := range resources {
			if r.Object == nil {
				t.Errorf("resource %d: expected non-nil Object for JSON serialization", i)
			}
			// Verify Object contains expected fields.
			if _, ok := r.Object["apiVersion"]; !ok {
				t.Errorf("resource %d: Object missing apiVersion field", i)
			}
			if _, ok := r.Object["kind"]; !ok {
				t.Errorf("resource %d: Object missing kind field", i)
			}
		}
	})
}

// TestCueRendererAdapter_WithDefaultTemplate verifies the adapter works end-to-end
// with the embedded default CUE template.
func TestCueRendererAdapter_WithDefaultTemplate(t *testing.T) {
	adapter := NewCueRendererAdapter()
	namespace := "prj-my-project"

	resources, err := adapter.Render(context.Background(), DefaultTemplate,
		cueInput("holos-console", "ghcr.io/holos-run/holos-console", "latest", "my-project", namespace))
	if err != nil {
		t.Fatalf("expected no error rendering default template, got %v", err)
	}

	if len(resources) != 3 {
		t.Fatalf("expected 3 resources (ServiceAccount, Deployment, Service), got %d", len(resources))
	}

	allYAML := ""
	for _, r := range resources {
		if r.YAML == "" {
			t.Error("expected non-empty YAML for each resource")
		}
		allYAML += r.YAML
	}

	for _, kind := range []string{"ServiceAccount", "Deployment", "Service"} {
		if !strings.Contains(allYAML, kind) {
			t.Errorf("expected YAML to contain resource of kind %q", kind)
		}
	}

	if !strings.Contains(allYAML, namespace) {
		t.Errorf("expected YAML to contain namespace %q", namespace)
	}
}
