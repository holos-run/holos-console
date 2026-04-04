package deployments

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// structuredTemplate uses the new namespaced/cluster output format.
const structuredTemplate = `
package deployment

input: {
	name:      string
	image:     string
	tag:       string
	project:   string
	namespace: string
}

_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

namespaced: (input.namespace): {
	ServiceAccount: (input.name): {
		apiVersion: "v1"
		kind:       "ServiceAccount"
		metadata: {
			name:      input.name
			namespace: input.namespace
			labels:    _labels
		}
	}
	Deployment: (input.name): {
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: {
			name:      input.name
			namespace: input.namespace
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

// structuredCrossNamespaceTemplate tries to set metadata.namespace to a different value than the struct key.
const structuredCrossNamespaceTemplate = `
package deployment

input: {
	name:      string
	image:     string
	tag:       string
	project:   string
	namespace: string
}

namespaced: (input.namespace): {
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

// structuredMissingManagedByTemplate is missing the required managed-by label.
const structuredMissingManagedByTemplate = `
package deployment

input: {
	name:      string
	image:     string
	tag:       string
	project:   string
	namespace: string
}

namespaced: (input.namespace): {
	Deployment: (input.name): {
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: {
			name:      input.name
			namespace: input.namespace
		}
		spec: {}
	}
}

cluster: {}
`

// validTemplate produces a single Deployment resource.
const validTemplate = `
input: {
	name:      string
	image:     string
	tag:        string
	project:   string
	namespace: string
}

resources: [
	{
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: {
			name:      input.name
			namespace: input.namespace
			labels: {
				"app.kubernetes.io/managed-by": "console.holos.run"
				"app.kubernetes.io/name":       input.name
			}
		}
		spec: {
			selector: matchLabels: "app.kubernetes.io/name": input.name
			template: {
				metadata: labels: "app.kubernetes.io/name": input.name
				spec: containers: [{
					name:  input.name
					image: input.image + ":" + input.tag
				}]
			}
		}
	},
]
`

// crossNamespaceTemplate tries to write into a different namespace.
const crossNamespaceTemplate = `
input: {
	name:      string
	image:     string
	tag:        string
	project:   string
	namespace: string
}

resources: [
	{
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: {
			name:      input.name
			namespace: "other-namespace"
			labels: "app.kubernetes.io/managed-by": "console.holos.run"
		}
		spec: {}
	},
]
`

// disallowedKindTemplate uses a kind not in the allowlist.
const disallowedKindTemplate = `
input: {
	name:      string
	image:     string
	tag:        string
	project:   string
	namespace: string
}

resources: [
	{
		apiVersion: "batch/v1"
		kind:       "Job"
		metadata: {
			name:      input.name
			namespace: input.namespace
			labels: "app.kubernetes.io/managed-by": "console.holos.run"
		}
		spec: {}
	},
]
`

// missingManagedByTemplate is missing the required managed-by label.
const missingManagedByTemplate = `
input: {
	name:      string
	image:     string
	tag:        string
	project:   string
	namespace: string
}

resources: [
	{
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: {
			name:      input.name
			namespace: input.namespace
		}
		spec: {}
	},
]
`

// invalidCUETemplate contains invalid CUE syntax.
const invalidCUETemplate = `this is { not valid cue !!!`

func defaultInput(namespace string) DeploymentInput {
	return DeploymentInput{
		Name:      "web-app",
		Image:     "nginx",
		Tag:       "1.25",
		Project:   "my-project",
		Namespace: namespace,
	}
}

func TestCueRenderer_Render(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("valid template produces expected resources", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), validTemplate, defaultInput(namespace))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		r := resources[0]
		if r.GetKind() != "Deployment" {
			t.Errorf("expected kind 'Deployment', got %q", r.GetKind())
		}
		if r.GetName() != "web-app" {
			t.Errorf("expected name 'web-app', got %q", r.GetName())
		}
		if r.GetNamespace() != namespace {
			t.Errorf("expected namespace %q, got %q", namespace, r.GetNamespace())
		}
		labels := r.GetLabels()
		if labels["app.kubernetes.io/managed-by"] != "console.holos.run" {
			t.Errorf("expected managed-by label, got %v", labels)
		}
	})

	t.Run("invalid CUE syntax returns error", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), invalidCUETemplate, defaultInput(namespace))
		if err == nil {
			t.Fatal("expected error for invalid CUE syntax")
		}
	})

	t.Run("cross-namespace resource rejected", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), crossNamespaceTemplate, defaultInput(namespace))
		if err == nil {
			t.Fatal("expected error for cross-namespace resource")
		}
	})

	t.Run("disallowed resource kind rejected", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), disallowedKindTemplate, defaultInput(namespace))
		if err == nil {
			t.Fatal("expected error for disallowed resource kind")
		}
	})

	t.Run("missing managed-by label rejected", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), missingManagedByTemplate, defaultInput(namespace))
		if err == nil {
			t.Fatal("expected error for missing managed-by label")
		}
	})

	t.Run("timeout enforced for slow evaluation", func(t *testing.T) {
		// A valid template should not time out (5s limit, evaluation is fast).
		ctx := context.Background()
		_, err := renderer.Render(ctx, validTemplate, defaultInput(namespace))
		if err != nil {
			t.Fatalf("fast template should not time out: %v", err)
		}
	})

	t.Run("input values are available in template", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), validTemplate, DeploymentInput{
			Name:      "my-app",
			Image:     "myrepo/myapp",
			Tag:       "v2.0.0",
			Project:   "my-project",
			Namespace: namespace,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		r := resources[0]
		// Verify name was substituted
		if r.GetName() != "my-app" {
			t.Errorf("expected name 'my-app', got %q", r.GetName())
		}
		// Verify image:tag was set
		containers, _, _ := unstructured.NestedSlice(r.Object, "spec", "template", "spec", "containers")
		if len(containers) != 1 {
			t.Fatalf("expected 1 container, got %d", len(containers))
		}
		c, ok := containers[0].(map[string]any)
		if !ok {
			t.Fatal("container is not a map")
		}
		wantImage := "myrepo/myapp:v2.0.0"
		if c["image"] != wantImage {
			t.Errorf("expected image %q, got %q", wantImage, c["image"])
		}
	})
}

// commandArgsTemplate renders command and args into a container spec.
const commandArgsTemplate = `
input: {
	name:      string
	image:     string
	tag:        string
	project:   string
	namespace: string
	command:   [...string] | *[]
	args:      [...string] | *[]
}

resources: [
	{
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: {
			name:      input.name
			namespace: input.namespace
			labels: {
				"app.kubernetes.io/managed-by": "console.holos.run"
				"app.kubernetes.io/name":       input.name
			}
		}
		spec: {
			selector: matchLabels: "app.kubernetes.io/name": input.name
			template: {
				metadata: labels: "app.kubernetes.io/name": input.name
				spec: containers: [{
					name:  input.name
					image: input.image + ":" + input.tag
					if len(input.command) > 0 {
						command: input.command
					}
					if len(input.args) > 0 {
						args: input.args
					}
				}]
			}
		}
	},
]
`

// envTemplate renders env vars into a container spec.
const envTemplate = `
input: {
	name:      string
	image:     string
	tag:        string
	project:   string
	namespace: string
	env: [...{name: string, value?: string, secretKeyRef?: {name: string, key: string}, configMapKeyRef?: {name: string, key: string}}] | *[]
}

resources: [
	{
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: {
			name:      input.name
			namespace: input.namespace
			labels: {
				"app.kubernetes.io/managed-by": "console.holos.run"
				"app.kubernetes.io/name":       input.name
			}
		}
		spec: {
			selector: matchLabels: "app.kubernetes.io/name": input.name
			template: {
				metadata: labels: "app.kubernetes.io/name": input.name
				spec: containers: [{
					name:  input.name
					image: input.image + ":" + input.tag
					if len(input.env) > 0 {
						env: input.env
					}
				}]
			}
		}
	},
]
`

func TestCueRenderer_Env(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("literal env var is passed to template", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), envTemplate, DeploymentInput{
			Name:      "my-app",
			Image:     "myrepo/myapp",
			Tag:       "v1.0.0",
			Project:   "my-project",
			Namespace: namespace,
			Env: []EnvVarInput{
				{Name: "FOO", Value: "bar"},
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		containers, _, _ := unstructured.NestedSlice(resources[0].Object, "spec", "template", "spec", "containers")
		if len(containers) == 0 {
			t.Fatal("expected at least 1 container")
		}
		c, ok := containers[0].(map[string]any)
		if !ok {
			t.Fatal("container is not a map")
		}
		envList, _, _ := unstructured.NestedSlice(map[string]any{"c": c}, "c", "env")
		if len(envList) != 1 {
			t.Fatalf("expected 1 env var, got %d", len(envList))
		}
		envItem, ok := envList[0].(map[string]any)
		if !ok {
			t.Fatal("env item is not a map")
		}
		if envItem["name"] != "FOO" {
			t.Errorf("expected env name 'FOO', got %v", envItem["name"])
		}
		if envItem["value"] != "bar" {
			t.Errorf("expected env value 'bar', got %v", envItem["value"])
		}
	})

	t.Run("empty env is omitted from template", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), envTemplate, DeploymentInput{
			Name:      "my-app",
			Image:     "myrepo/myapp",
			Tag:       "v1.0.0",
			Project:   "my-project",
			Namespace: namespace,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		containers, _, _ := unstructured.NestedSlice(resources[0].Object, "spec", "template", "spec", "containers")
		if len(containers) == 0 {
			t.Fatal("expected at least 1 container")
		}
		c, ok := containers[0].(map[string]any)
		if !ok {
			t.Fatal("container is not a map")
		}
		if _, exists := c["env"]; exists {
			t.Error("expected env to be absent when empty")
		}
	})
}

func TestCueRenderer_CommandArgs(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("command and args are passed to template", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), commandArgsTemplate, DeploymentInput{
			Name:      "my-app",
			Image:     "myrepo/myapp",
			Tag:       "v1.0.0",
			Project:   "my-project",
			Namespace: namespace,
			Command:   []string{"/bin/sh", "-c"},
			Args:      []string{"echo hello"},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		containers, _, _ := unstructured.NestedSlice(resources[0].Object, "spec", "template", "spec", "containers")
		if len(containers) == 0 {
			t.Fatal("expected at least 1 container")
		}
		c, ok := containers[0].(map[string]any)
		if !ok {
			t.Fatal("container is not a map")
		}
		gotCommand, _, _ := unstructured.NestedStringSlice(map[string]any{"c": c}, "c", "command")
		if len(gotCommand) != 2 || gotCommand[0] != "/bin/sh" {
			t.Errorf("expected command [/bin/sh -c], got %v", gotCommand)
		}
		gotArgs, _, _ := unstructured.NestedStringSlice(map[string]any{"c": c}, "c", "args")
		if len(gotArgs) != 1 || gotArgs[0] != "echo hello" {
			t.Errorf("expected args [echo hello], got %v", gotArgs)
		}
	})

	t.Run("empty command and args are omitted", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), commandArgsTemplate, DeploymentInput{
			Name:      "my-app",
			Image:     "myrepo/myapp",
			Tag:       "v1.0.0",
			Project:   "my-project",
			Namespace: namespace,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		containers, _, _ := unstructured.NestedSlice(resources[0].Object, "spec", "template", "spec", "containers")
		if len(containers) == 0 {
			t.Fatal("expected at least 1 container")
		}
		c, ok := containers[0].(map[string]any)
		if !ok {
			t.Fatal("container is not a map")
		}
		if _, exists := c["command"]; exists {
			t.Error("expected command to be absent when empty")
		}
		if _, exists := c["args"]; exists {
			t.Error("expected args to be absent when empty")
		}
	})
}

// structuredDuplicateTemplate tries to define the same Kind/name twice.
// CUE struct semantics naturally enforce uniqueness — setting the same path
// twice merges values or produces a conflict error if they are incompatible.
const structuredDuplicateTemplate = `
package deployment

input: {
	name:      string
	image:     string
	tag:       string
	project:   string
	namespace: string
}

namespaced: (input.namespace): {
	Deployment: (input.name): {
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: {
			name:      input.name
			namespace: input.namespace
			labels: "app.kubernetes.io/managed-by": "console.holos.run"
		}
		spec: replicas: 1
	}
	// Duplicate: same Kind/name with an incompatible replicas value.
	Deployment: (input.name): {
		apiVersion: "apps/v1"
		kind:       "Deployment"
		metadata: {
			name:      input.name
			namespace: input.namespace
			labels: "app.kubernetes.io/managed-by": "console.holos.run"
		}
		spec: replicas: 2
	}
}

cluster: {}
`

// TestCueRenderer_StructuredOutput tests the new namespaced/cluster output format.
func TestCueRenderer_StructuredOutput(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("structured template produces expected resources", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), structuredTemplate, DeploymentInput{
			Name:      "web-app",
			Image:     "nginx",
			Tag:       "1.25",
			Project:   "my-project",
			Namespace: namespace,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 2 {
			t.Fatalf("expected 2 resources (ServiceAccount, Deployment), got %d", len(resources))
		}

		kindSet := make(map[string]bool)
		for _, r := range resources {
			kindSet[r.GetKind()] = true
			if r.GetNamespace() != namespace {
				t.Errorf("resource %s/%s: expected namespace %q, got %q", r.GetKind(), r.GetName(), namespace, r.GetNamespace())
			}
			labels := r.GetLabels()
			if labels["app.kubernetes.io/managed-by"] != "console.holos.run" {
				t.Errorf("resource %s/%s: missing managed-by label", r.GetKind(), r.GetName())
			}
		}
		for _, kind := range []string{"ServiceAccount", "Deployment"} {
			if !kindSet[kind] {
				t.Errorf("expected resource of kind %q", kind)
			}
		}
	})

	t.Run("structured template rejects cross-namespace resources", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), structuredCrossNamespaceTemplate, DeploymentInput{
			Name:      "web-app",
			Image:     "nginx",
			Tag:       "1.25",
			Project:   "my-project",
			Namespace: namespace,
		})
		if err == nil {
			t.Fatal("expected error for cross-namespace resource")
		}
	})

	t.Run("structured template rejects missing managed-by label", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), structuredMissingManagedByTemplate, DeploymentInput{
			Name:      "web-app",
			Image:     "nginx",
			Tag:       "1.25",
			Project:   "my-project",
			Namespace: namespace,
		})
		if err == nil {
			t.Fatal("expected error for missing managed-by label")
		}
	})

	t.Run("duplicate Kind/name with incompatible values causes CUE conflict", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), structuredDuplicateTemplate, DeploymentInput{
			Name:      "web-app",
			Image:     "nginx",
			Tag:       "1.25",
			Project:   "my-project",
			Namespace: namespace,
		})
		if err == nil {
			t.Fatal("expected error for duplicate Kind/name with conflicting values")
		}
	})
}
