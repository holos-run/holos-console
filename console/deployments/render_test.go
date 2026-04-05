package deployments

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// structuredTemplate uses the namespaced/cluster structured output format.
const structuredTemplate = `
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

// structuredCrossNamespaceTemplate tries to set metadata.namespace to a different value than the struct key.
const structuredCrossNamespaceTemplate = `
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

// structuredMissingManagedByTemplate is missing the required managed-by label.
const structuredMissingManagedByTemplate = `
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
			namespace: system.namespace
		}
		spec: {}
	}
}

cluster: {}
`

// validTemplate produces a single Deployment resource using structured output.
const validTemplate = `
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
			namespace: system.namespace
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
	}
}

cluster: {}
`

// crossNamespaceTemplate tries to write into a different namespace using structured output.
const crossNamespaceTemplate = `
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

// disallowedKindTemplate uses a kind not in the allowlist (structured output).
const disallowedKindTemplate = `
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
	Job: (input.name): {
		apiVersion: "batch/v1"
		kind:       "Job"
		metadata: {
			name:      input.name
			namespace: system.namespace
			labels: "app.kubernetes.io/managed-by": "console.holos.run"
		}
		spec: {}
	}
}

cluster: {}
`

// missingManagedByTemplate is missing the required managed-by label (structured output).
const missingManagedByTemplate = `
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
			namespace: system.namespace
		}
		spec: {}
	}
}

cluster: {}
`

// invalidCUETemplate contains invalid CUE syntax.
const invalidCUETemplate = `this is { not valid cue !!!`

func defaultSystem(namespace string) SystemInput {
	return SystemInput{
		Project:   "my-project",
		Namespace: namespace,
	}
}

func defaultUser() UserInput {
	return UserInput{
		Name:  "web-app",
		Image: "nginx",
		Tag:   "1.25",
	}
}

func TestCueRenderer_Render(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("valid template produces expected resources", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), validTemplate, defaultSystem(namespace), defaultUser())
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
		_, err := renderer.Render(context.Background(), invalidCUETemplate, defaultSystem(namespace), defaultUser())
		if err == nil {
			t.Fatal("expected error for invalid CUE syntax")
		}
	})

	t.Run("cross-namespace resource rejected", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), crossNamespaceTemplate, defaultSystem(namespace), defaultUser())
		if err == nil {
			t.Fatal("expected error for cross-namespace resource")
		}
	})

	t.Run("disallowed resource kind rejected", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), disallowedKindTemplate, defaultSystem(namespace), defaultUser())
		if err == nil {
			t.Fatal("expected error for disallowed resource kind")
		}
	})

	t.Run("missing managed-by label rejected", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), missingManagedByTemplate, defaultSystem(namespace), defaultUser())
		if err == nil {
			t.Fatal("expected error for missing managed-by label")
		}
	})

	t.Run("timeout enforced for slow evaluation", func(t *testing.T) {
		// A valid template should not time out (5s limit, evaluation is fast).
		ctx := context.Background()
		_, err := renderer.Render(ctx, validTemplate, defaultSystem(namespace), defaultUser())
		if err != nil {
			t.Fatalf("fast template should not time out: %v", err)
		}
	})

	t.Run("input values are available in template", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), validTemplate,
			SystemInput{Project: "my-project", Namespace: namespace},
			UserInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v2.0.0"},
		)
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

// commandArgsTemplate renders command and args into a container spec (structured output).
const commandArgsTemplate = `
package deployment

input: {
	name:    string
	image:   string
	tag:     string
	command: [...string] | *[]
	args:    [...string] | *[]
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
			namespace: system.namespace
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
	}
}

cluster: {}
`

// envTemplate renders env vars into a container spec (structured output).
const envTemplate = `
package deployment

input: {
	name:  string
	image: string
	tag:   string
	env: [...{name: string, value?: string, secretKeyRef?: {name: string, key: string}, configMapKeyRef?: {name: string, key: string}}] | *[]
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
			namespace: system.namespace
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
	}
}

cluster: {}
`

func TestCueRenderer_Env(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("literal env var is passed to template", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), envTemplate,
			SystemInput{Project: "my-project", Namespace: namespace},
			UserInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v1.0.0", Env: []EnvVarInput{{Name: "FOO", Value: "bar"}}},
		)
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
		resources, err := renderer.Render(context.Background(), envTemplate,
			SystemInput{Project: "my-project", Namespace: namespace},
			UserInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v1.0.0"},
		)
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
		resources, err := renderer.Render(context.Background(), commandArgsTemplate,
			SystemInput{Project: "my-project", Namespace: namespace},
			UserInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v1.0.0", Command: []string{"/bin/sh", "-c"}, Args: []string{"echo hello"}},
		)
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
		resources, err := renderer.Render(context.Background(), commandArgsTemplate,
			SystemInput{Project: "my-project", Namespace: namespace},
			UserInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v1.0.0"},
		)
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

// portTemplate renders a containerPort using input.port (structured output).
const portTemplate = `
package deployment

input: {
	name:  string
	image: string
	tag:   string
	port:  int & >0 & <=65535 | *8080
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
			namespace: system.namespace
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
					ports: [{containerPort: input.port, name: "http"}]
				}]
			}
		}
	}
	Service: (input.name): {
		apiVersion: "v1"
		kind:       "Service"
		metadata: {
			name:      input.name
			namespace: system.namespace
			labels: {
				"app.kubernetes.io/managed-by": "console.holos.run"
				"app.kubernetes.io/name":       input.name
			}
		}
		spec: {
			selector: "app.kubernetes.io/name": input.name
			ports: [{port: 80, targetPort: "http", name: "http"}]
		}
	}
}

cluster: {}
`

func TestCueRenderer_Port(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("explicit port is used in containerPort", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), portTemplate,
			SystemInput{Project: "my-project", Namespace: namespace},
			UserInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v1.0.0", Port: 9090},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		var deployment unstructured.Unstructured
		for _, r := range resources {
			if r.GetKind() == "Deployment" {
				deployment = r
				break
			}
		}
		containers, _, _ := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
		if len(containers) == 0 {
			t.Fatal("expected at least 1 container")
		}
		c, ok := containers[0].(map[string]any)
		if !ok {
			t.Fatal("container is not a map")
		}
		ports, _, _ := unstructured.NestedSlice(map[string]any{"c": c}, "c", "ports")
		if len(ports) != 1 {
			t.Fatalf("expected 1 port, got %d", len(ports))
		}
		port, ok := ports[0].(map[string]any)
		if !ok {
			t.Fatal("port is not a map")
		}
		// CUE decodes integers as int64 in Go.
		gotPort, _, _ := unstructured.NestedFieldNoCopy(map[string]any{"p": port}, "p", "containerPort")
		if gotPort != int64(9090) {
			t.Errorf("expected containerPort 9090, got %v (%T)", gotPort, gotPort)
		}
		if port["name"] != "http" {
			t.Errorf("expected port name 'http', got %v", port["name"])
		}
	})

	t.Run("zero port uses CUE default 8080", func(t *testing.T) {
		// When Port is 0 (omitempty), the JSON omits the field and CUE applies default 8080.
		resources, err := renderer.Render(context.Background(), portTemplate,
			SystemInput{Project: "my-project", Namespace: namespace},
			UserInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v1.0.0", Port: 0}, // zero → omitempty → CUE default applies
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		var deployment unstructured.Unstructured
		for _, r := range resources {
			if r.GetKind() == "Deployment" {
				deployment = r
				break
			}
		}
		containers, _, _ := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
		if len(containers) == 0 {
			t.Fatal("expected at least 1 container")
		}
		c, ok := containers[0].(map[string]any)
		if !ok {
			t.Fatal("container is not a map")
		}
		ports, _, _ := unstructured.NestedSlice(map[string]any{"c": c}, "c", "ports")
		if len(ports) != 1 {
			t.Fatalf("expected 1 port, got %d", len(ports))
		}
		port, ok := ports[0].(map[string]any)
		if !ok {
			t.Fatal("port is not a map")
		}
		gotPort, _, _ := unstructured.NestedFieldNoCopy(map[string]any{"p": port}, "p", "containerPort")
		if gotPort != int64(8080) {
			t.Errorf("expected default containerPort 8080, got %v (%T)", gotPort, gotPort)
		}
	})
}

// structuredDuplicateTemplate tries to define the same Kind/name twice.
// CUE struct semantics naturally enforce uniqueness — setting the same path
// twice merges values or produces a conflict error if they are incompatible.
const structuredDuplicateTemplate = `
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
			namespace: system.namespace
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
			namespace: system.namespace
			labels: "app.kubernetes.io/managed-by": "console.holos.run"
		}
		spec: replicas: 2
	}
}

cluster: {}
`

// TestCueRenderer_StructuredOutput tests the namespaced/cluster structured output format.
func TestCueRenderer_StructuredOutput(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("structured template produces expected resources", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), structuredTemplate,
			SystemInput{Project: "my-project", Namespace: namespace},
			UserInput{Name: "web-app", Image: "nginx", Tag: "1.25"},
		)
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
		_, err := renderer.Render(context.Background(), structuredCrossNamespaceTemplate,
			SystemInput{Project: "my-project", Namespace: namespace},
			UserInput{Name: "web-app", Image: "nginx", Tag: "1.25"},
		)
		if err == nil {
			t.Fatal("expected error for cross-namespace resource")
		}
	})

	t.Run("structured template rejects missing managed-by label", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), structuredMissingManagedByTemplate,
			SystemInput{Project: "my-project", Namespace: namespace},
			UserInput{Name: "web-app", Image: "nginx", Tag: "1.25"},
		)
		if err == nil {
			t.Fatal("expected error for missing managed-by label")
		}
	})

	t.Run("duplicate Kind/name with incompatible values causes CUE conflict", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), structuredDuplicateTemplate,
			SystemInput{Project: "my-project", Namespace: namespace},
			UserInput{Name: "web-app", Image: "nginx", Tag: "1.25"},
		)
		if err == nil {
			t.Fatal("expected error for duplicate Kind/name with conflicting values")
		}
	})
}
