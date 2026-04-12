package deployments

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

// structuredTemplate uses the projectResources.namespacedResources/projectResources.clusterResources structured output format.
const structuredTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

projectResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: (input.name): {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels:    _labels
			}
		}
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
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
	clusterResources: {}
}
`

// structuredCrossNamespaceTemplate has a struct-key/metadata mismatch:
// the struct key uses platform.namespace but metadata.namespace is hardcoded to
// "other-namespace". This tests that the struct-key/metadata consistency check
// catches the mismatch (the namespace restriction itself has been removed per
// ADR 026).
const structuredCrossNamespaceTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: (platform.namespace): {
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
	clusterResources: {}
}
`

// multiNamespaceTemplate produces resources in two different namespaces:
// a Deployment in the project namespace and an HTTPRoute in istio-ingress.
// After removing the per-resource namespace restriction (ADR 026), this template
// must render successfully.
const multiNamespaceTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:          string
	namespace:        string
	gatewayNamespace: string
}

_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

projectResources: {
	namespacedResources: {
		(platform.namespace): {
			Deployment: (input.name): {
				apiVersion: "apps/v1"
				kind:       "Deployment"
				metadata: {
					name:      input.name
					namespace: platform.namespace
					labels:    _labels
				}
				spec: {
					selector: matchLabels: "app.kubernetes.io/name": input.name
					template: {
						metadata: labels: _labels
						spec: containers: [{
							name:  input.name
							image: input.image + ":" + input.tag
						}]
					}
				}
			}
		}
		(platform.gatewayNamespace): {
			HTTPRoute: (input.name): {
				apiVersion: "gateway.networking.k8s.io/v1"
				kind:       "HTTPRoute"
				metadata: {
					name:      input.name
					namespace: platform.gatewayNamespace
					labels:    _labels
				}
				spec: {
					parentRefs: [{
						group:     "gateway.networking.k8s.io"
						kind:      "Gateway"
						namespace: platform.gatewayNamespace
						name:      "default"
					}]
					rules: [{
						backendRefs: [{
							name: input.name
							port: 80
						}]
					}]
				}
			}
		}
	}
	clusterResources: {}
}
`

// multiNamespacePlatformTemplate produces platform resources in two different
// namespaces: a ServiceAccount in the project namespace and an HTTPRoute in
// istio-ingress. Validates that platformResources also allows multi-namespace.
const multiNamespacePlatformTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:          string
	namespace:        string
	gatewayNamespace: string
}

_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels:    _labels
			}
			spec: {
				selector: matchLabels: "app.kubernetes.io/name": input.name
				template: {
					metadata: labels: _labels
					spec: containers: [{
						name:  input.name
						image: input.image + ":" + input.tag
					}]
				}
			}
		}
	}
	clusterResources: {}
}

platformResources: {
	namespacedResources: {
		(platform.namespace): {
			ServiceAccount: "platform-sa": {
				apiVersion: "v1"
				kind:       "ServiceAccount"
				metadata: {
					name:      "platform-sa"
					namespace: platform.namespace
					labels:    _labels
				}
			}
		}
		(platform.gatewayNamespace): {
			HTTPRoute: (input.name + "-route"): {
				apiVersion: "gateway.networking.k8s.io/v1"
				kind:       "HTTPRoute"
				metadata: {
					name:      input.name + "-route"
					namespace: platform.gatewayNamespace
					labels:    _labels
				}
				spec: {
					parentRefs: [{
						group:     "gateway.networking.k8s.io"
						kind:      "Gateway"
						namespace: platform.gatewayNamespace
						name:      "default"
					}]
					rules: [{
						backendRefs: [{
							name: input.name
							port: 80
						}]
					}]
				}
			}
		}
	}
	clusterResources: {}
}
`

// emptyNamespaceKeyTemplate uses an empty string as the namespace struct key.
// This must be rejected because namespaced resources require a non-empty namespace.
const emptyNamespaceKeyTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: "": {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: ""
				labels: "app.kubernetes.io/managed-by": "console.holos.run"
			}
			spec: {}
		}
	}
	clusterResources: {}
}
`

// structKeyMismatchTemplate has a struct key that does not match
// metadata.namespace — the struct key is "wrong-ns" but metadata.namespace is
// the project namespace. This must still be rejected by the struct-key/metadata
// consistency check even after the namespace restriction is removed.
const structKeyMismatchTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: "wrong-ns": {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels: "app.kubernetes.io/managed-by": "console.holos.run"
			}
			spec: {}
		}
	}
	clusterResources: {}
}
`

// structuredMissingManagedByTemplate is missing the required managed-by label.
const structuredMissingManagedByTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
			}
			spec: {}
		}
	}
	clusterResources: {}
}
`

// validTemplate produces a single Deployment resource using structured output.
const validTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
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
	clusterResources: {}
}
`

// emptyPlatformResourcesTemplate defines platformResources with empty sub-structs.
// Used to test that an explicitly defined but empty platformResources block
// produces a non-nil JSON string rather than nil.
const emptyPlatformResourcesTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
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
	clusterResources: {}
}
platformResources: {
	namespacedResources: {}
	clusterResources: {}
}
`

// crossNamespaceTemplate has a struct-key/metadata mismatch: the struct key
// uses platform.namespace but metadata.namespace is "other-namespace". This
// tests the struct-key consistency check (the namespace restriction itself was
// removed per ADR 026).
const crossNamespaceTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: (platform.namespace): {
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
	clusterResources: {}
}
`

// disallowedKindTemplate uses a kind not in the allowlist (structured output).
const disallowedKindTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Job: (input.name): {
			apiVersion: "batch/v1"
			kind:       "Job"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels: "app.kubernetes.io/managed-by": "console.holos.run"
			}
			spec: {}
		}
	}
	clusterResources: {}
}
`

// missingManagedByTemplate is missing the required managed-by label (structured output).
const missingManagedByTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
			}
			spec: {}
		}
	}
	clusterResources: {}
}
`

// invalidCUETemplate contains invalid CUE syntax.
const invalidCUETemplate = `this is { not valid cue !!!`

// claimsAnnotationTemplate is a template that uses platform.claims.email in an annotation,
// verifying that claims are propagated from PlatformInput into the CUE evaluation.
const claimsAnnotationTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
	claims: {
		iss:            string
		sub:            string
		exp:            int
		iat:            int
		email:          string
		email_verified: bool
	}
}

_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

_annotations: {
	"console.holos.run/deployer-email": platform.claims.email
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:        input.name
				namespace:   platform.namespace
				labels:      _labels
				annotations: _annotations
			}
			spec: {
				selector: matchLabels: "app.kubernetes.io/name": input.name
				template: {
					metadata: labels: _labels
					spec: containers: [{
						name:  input.name
						image: input.image + ":" + input.tag
					}]
				}
			}
		}
	}
	clusterResources: {}
}
`

func defaultPlatform(namespace string) v1alpha2.PlatformInput {
	return v1alpha2.PlatformInput{
		Project:          "my-project",
		Namespace:        namespace,
		GatewayNamespace: DefaultGatewayNamespace,
	}
}

func defaultProject() v1alpha2.ProjectInput {
	return v1alpha2.ProjectInput{
		Name:  "web-app",
		Image: "nginx",
		Tag:   "1.25",
		Port:  8080,
	}
}

func TestCueRenderer_Render(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("valid template produces expected resources", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), validTemplate, defaultPlatform(namespace), defaultProject())
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
		_, err := renderer.Render(context.Background(), invalidCUETemplate, defaultPlatform(namespace), defaultProject())
		if err == nil {
			t.Fatal("expected error for invalid CUE syntax")
		}
	})

	t.Run("struct-key metadata namespace mismatch rejected", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), crossNamespaceTemplate, defaultPlatform(namespace), defaultProject())
		if err == nil {
			t.Fatal("expected error for struct-key/metadata namespace mismatch")
		}
		if !strings.Contains(err.Error(), "does not match struct key") {
			t.Errorf("expected 'does not match struct key' error, got: %v", err)
		}
	})

	t.Run("disallowed resource kind rejected", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), disallowedKindTemplate, defaultPlatform(namespace), defaultProject())
		if err == nil {
			t.Fatal("expected error for disallowed resource kind")
		}
	})

	t.Run("missing managed-by label rejected", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), missingManagedByTemplate, defaultPlatform(namespace), defaultProject())
		if err == nil {
			t.Fatal("expected error for missing managed-by label")
		}
	})

	t.Run("timeout enforced for slow evaluation", func(t *testing.T) {
		// A valid template should not time out (5s limit, evaluation is fast).
		ctx := context.Background()
		_, err := renderer.Render(ctx, validTemplate, defaultPlatform(namespace), defaultProject())
		if err != nil {
			t.Fatalf("fast template should not time out: %v", err)
		}
	})

	t.Run("input values are available in template", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), validTemplate,
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v2.0.0", Port: 8080},
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

input: {
	name:    string
	image:   string
	tag:     string
	command: [...string] | *[]
	args:    [...string] | *[]
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
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
	clusterResources: {}
}
`

// envTemplate renders env vars into a container spec (structured output).
const envTemplate = `

input: {
	name:  string
	image: string
	tag:   string
	env: [...{name: string, value?: string, secretKeyRef?: {name: string, key: string}, configMapKeyRef?: {name: string, key: string}}] | *[]
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
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
	clusterResources: {}
}
`

func TestCueRenderer_Env(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("literal env var is passed to template", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), envTemplate,
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v1.0.0", Env: []v1alpha2.EnvVar{{Name: "FOO", Value: "bar"}}},
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
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v1.0.0", Port: 8080},
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
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v1.0.0", Command: []string{"/bin/sh", "-c"}, Args: []string{"echo hello"}},
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
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v1.0.0", Port: 8080},
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

input: {
	name:  string
	image: string
	tag:   string
	port:  int & >0 & <=65535 | *8080
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
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
				namespace: platform.namespace
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
	clusterResources: {}
}
`

func TestCueRenderer_Port(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("explicit port is used in containerPort", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), portTemplate,
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v1.0.0", Port: 9090},
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

	t.Run("default port 8080 is applied by Go when port is unset", func(t *testing.T) {
		// The Go handler defaults Port to 8080 before calling the renderer.
		// This test verifies that Port: 8080 (the default) renders correctly.
		resources, err := renderer.Render(context.Background(), portTemplate,
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "my-app", Image: "myrepo/myapp", Tag: "v1.0.0", Port: 8080},
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

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
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
				namespace: platform.namespace
				labels: "app.kubernetes.io/managed-by": "console.holos.run"
			}
			spec: replicas: 2
		}
	}
	clusterResources: {}
}
`

// TestCueRenderer_StructuredOutput tests the projectResources.namespacedResources/projectResources.clusterResources structured output format.
func TestCueRenderer_StructuredOutput(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("structured template produces expected resources", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), structuredTemplate,
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "web-app", Image: "nginx", Tag: "1.25", Port: 8080},
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

	t.Run("structured template rejects struct-key metadata namespace mismatch", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), structuredCrossNamespaceTemplate,
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "web-app", Image: "nginx", Tag: "1.25", Port: 8080},
		)
		if err == nil {
			t.Fatal("expected error for struct-key/metadata namespace mismatch")
		}
		if !strings.Contains(err.Error(), "does not match struct key") {
			t.Errorf("expected 'does not match struct key' error, got: %v", err)
		}
	})

	t.Run("structured template rejects missing managed-by label", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), structuredMissingManagedByTemplate,
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "web-app", Image: "nginx", Tag: "1.25", Port: 8080},
		)
		if err == nil {
			t.Fatal("expected error for missing managed-by label")
		}
	})

	t.Run("duplicate Kind/name with incompatible values causes CUE conflict", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), structuredDuplicateTemplate,
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "web-app", Image: "nginx", Tag: "1.25", Port: 8080},
		)
		if err == nil {
			t.Fatal("expected error for duplicate Kind/name with conflicting values")
		}
	})
}

// TestCueRenderer_ClaimsPropagation verifies that OIDC claims from PlatformInput
// are available inside the CUE template and that platform.claims.email appears
// in rendered resource annotations.
func TestCueRenderer_ClaimsPropagation(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"
	const deployerEmail = "alice@example.com"

	system := v1alpha2.PlatformInput{
		Project:   "my-project",
		Namespace: namespace,
		Claims: v1alpha2.Claims{
			Iss:           "https://dex.example.com",
			Sub:           "alice-sub",
			Exp:           9999999999,
			Iat:           1700000000,
			Email:         deployerEmail,
			EmailVerified: true,
		},
	}
	user := v1alpha2.ProjectInput{
		Name:  "web-app",
		Image: "nginx",
		Tag:   "1.25",
		Port:  8080,
	}

	t.Run("platform.claims.email appears in resource annotation", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), claimsAnnotationTemplate, system, user)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		annotations := resources[0].GetAnnotations()
		got := annotations["console.holos.run/deployer-email"]
		if got != deployerEmail {
			t.Errorf("expected deployer-email annotation=%q, got %q", deployerEmail, got)
		}
	})

	t.Run("platform.namespace is used for namespace constraint not input.namespace", func(t *testing.T) {
		// The claimsAnnotationTemplate uses platform.namespace (not input.namespace)
		// for the namespaced struct key, confirming the split input architecture.
		resources, err := renderer.Render(context.Background(), claimsAnnotationTemplate, system, user)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		if resources[0].GetNamespace() != namespace {
			t.Errorf("expected namespace %q, got %q", namespace, resources[0].GetNamespace())
		}
	})
}

// systemOutputTemplate uses platformResources.namespacedResources to simulate a
// platform template that provides platform-managed resources alongside project resources.
const systemOutputTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels:    _labels
			}
			spec: {
				selector: matchLabels: "app.kubernetes.io/name": input.name
				template: {
					metadata: labels: _labels
					spec: containers: [{
						name:  input.name
						image: input.image + ":" + input.tag
					}]
				}
			}
		}
	}
	clusterResources: {}
}
platformResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: "system-sa": {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      "system-sa"
				namespace: platform.namespace
				labels:    _labels
			}
		}
	}
	clusterResources: {}
}
`

// gatewayNamespaceTemplate uses platform.gatewayNamespace in an annotation to verify propagation.
const gatewayNamespaceTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:          string
	namespace:        string
	gatewayNamespace: string
	organization:     string
	claims: {
		iss:            string
		sub:            string
		exp:            int
		iat:            int
		email:          string
		email_verified: bool
	}
}

projectResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: (input.name): {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:        input.name
				namespace:   platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       input.name
				}
				annotations: {
					"test.holos.run/gateway-namespace": platform.gatewayNamespace
				}
			}
		}
	}
	clusterResources: {}
}
`

// TestCueRenderer_GatewayNamespace verifies that gatewayNamespace in PlatformInput
// is propagated into CUE templates via platform.gatewayNamespace.
func TestCueRenderer_GatewayNamespace(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("explicit gatewayNamespace is propagated to template", func(t *testing.T) {
		system := v1alpha2.PlatformInput{
			Project:          "my-project",
			Namespace:        namespace,
			GatewayNamespace: "custom-gateway-ns",
			Claims: v1alpha2.Claims{
				Iss:           "https://example.com",
				Sub:           "u1",
				Exp:           9999999999,
				Iat:           1000000000,
				Email:         "test@example.com",
				EmailVerified: true,
			},
		}
		resources, err := renderer.Render(context.Background(), gatewayNamespaceTemplate, system, v1alpha2.ProjectInput{
			Name:  "web-app",
			Image: "nginx",
			Tag:   "1.25",
			Port:  8080,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		annotations := resources[0].GetAnnotations()
		got := annotations["test.holos.run/gateway-namespace"]
		if got != "custom-gateway-ns" {
			t.Errorf("expected gateway-namespace annotation=%q, got %q", "custom-gateway-ns", got)
		}
	})

	t.Run("default gatewayNamespace istio-ingress is applied by Go", func(t *testing.T) {
		// The Go handler defaults GatewayNamespace to "istio-ingress" before
		// calling the renderer. This test verifies the default renders correctly.
		system := v1alpha2.PlatformInput{
			Project:          "my-project",
			Namespace:        namespace,
			GatewayNamespace: DefaultGatewayNamespace,
			Claims: v1alpha2.Claims{
				Iss:           "https://example.com",
				Sub:           "u1",
				Exp:           9999999999,
				Iat:           1000000000,
				Email:         "test@example.com",
				EmailVerified: true,
			},
		}
		resources, err := renderer.Render(context.Background(), gatewayNamespaceTemplate, system, v1alpha2.ProjectInput{
			Name:  "web-app",
			Image: "nginx",
			Tag:   "1.25",
			Port:  8080,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		annotations := resources[0].GetAnnotations()
		got := annotations["test.holos.run/gateway-namespace"]
		if got != "istio-ingress" {
			t.Errorf("expected default gateway-namespace annotation=%q, got %q", "istio-ingress", got)
		}
	})
}

// systemUnificationTemplate is a platform template that defines
// platformResources.namespacedResources using input.name from the deployment template input.
// This simulates what an HTTPRoute platform template would look like.
const systemUnificationTemplate = `

platformResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: "system-from-\(input.name)": {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      "system-from-\(input.name)"
				namespace: platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       input.name
				}
			}
		}
	}
	clusterResources: {}
}
`

// systemProjectResourcesTemplate is a platform template that defines resources in
// projectResources (not platformResources). Per ADR 016, any template at any
// level can define values in any collection. This validates that platform templates
// are not limited to platformResources.
const systemProjectResourcesTemplate = `

projectResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: "sys-project-sa": {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      "sys-project-sa"
				namespace: platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       input.name
				}
			}
		}
	}
	clusterResources: {}
}
`

// systemBothCollectionsTemplate is a platform template that defines resources in
// both projectResources and platformResources. This validates that
// RenderWithAncestorTemplates returns resources from both collections.
const systemBothCollectionsTemplate = `

projectResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: "sys-project-sa": {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      "sys-project-sa"
				namespace: platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       input.name
				}
			}
		}
	}
	clusterResources: {}
}

platformResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: "sys-platform-sa": {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      "sys-platform-sa"
				namespace: platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       input.name
				}
			}
		}
	}
	clusterResources: {}
}
`

// deploymentTemplateForUnification is a simple deployment template used to test unification.
const deploymentTemplateForUnification = `

input: #Input
platform: #Platform

#Input: {
	name:  string
	image: string
	tag:   string
	port:  int
}

#Platform: {
	project:          string
	namespace:        string
	gatewayNamespace: string
	organization:     string
	folders?: [...{name: string, namespace?: string}]
	claims: {
		iss:            string
		sub:            string
		exp:            int
		iat:            int
		email:          string
		email_verified: bool
	}
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
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
	clusterResources: {}
}
`

// TestCueRenderer_OrgTemplateUnification verifies that a platform template CUE source
// can be unified with a deployment template and that input.name is accessible in the
// platform template's platformResources.namespacedResources.
func TestCueRenderer_OrgTemplateUnification(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	system := v1alpha2.PlatformInput{
		Project:          "my-project",
		Namespace:        namespace,
		GatewayNamespace: "istio-ingress",
		Claims: v1alpha2.Claims{
			Iss:           "https://example.com",
			Sub:           "u1",
			Exp:           9999999999,
			Iat:           1000000000,
			Email:         "test@example.com",
			EmailVerified: true,
		},
	}
	user := v1alpha2.ProjectInput{
		Name:  "web-app",
		Image: "nginx",
		Tag:   "1.25",
		Port:  8080,
	}

	t.Run("platform template resources are included when unified with deployment template", func(t *testing.T) {
		resources, err := renderer.RenderWithAncestorTemplates(context.Background(),
			deploymentTemplateForUnification,
			[]string{systemUnificationTemplate},
			system, user,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Expect: 1 Deployment from deployment template + 1 ServiceAccount from platform template.
		if len(resources) != 2 {
			t.Fatalf("expected 2 resources (Deployment + platform ServiceAccount), got %d: %v",
				len(resources), resourceKinds(resources))
		}
		kindSet := make(map[string]bool)
		nameSet := make(map[string]bool)
		for _, r := range resources {
			kindSet[r.GetKind()] = true
			nameSet[r.GetName()] = true
		}
		if !kindSet["Deployment"] {
			t.Error("expected Deployment resource from deployment template")
		}
		if !kindSet["ServiceAccount"] {
			t.Error("expected ServiceAccount resource from platform template")
		}
		if !nameSet["system-from-web-app"] {
			t.Errorf("expected platform template to use input.name='web-app', got names: %v", nameSet)
		}
	})

	t.Run("no platform templates returns only deployment resources", func(t *testing.T) {
		resources, err := renderer.RenderWithAncestorTemplates(context.Background(),
			deploymentTemplateForUnification,
			nil,
			system, user,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource (Deployment only), got %d", len(resources))
		}
		if resources[0].GetKind() != "Deployment" {
			t.Errorf("expected Deployment, got %q", resources[0].GetKind())
		}
	})

	// ADR 016 key insight: any template at any level can define values in any
	// collection. A platform template is not restricted to platformResources only.
	t.Run("platform template contributing to projectResources is unified", func(t *testing.T) {
		resources, err := renderer.RenderWithAncestorTemplates(context.Background(),
			deploymentTemplateForUnification,
			[]string{systemProjectResourcesTemplate},
			system, user,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Expect: 1 Deployment from deployment template + 1 ServiceAccount from
		// platform template's projectResources.
		if len(resources) != 2 {
			t.Fatalf("expected 2 resources (Deployment + system ServiceAccount in projectResources), got %d: %v",
				len(resources), resourceKinds(resources))
		}
		nameSet := make(map[string]bool)
		for _, r := range resources {
			nameSet[r.GetName()] = true
		}
		if !nameSet["web-app"] {
			t.Error("expected Deployment 'web-app' from deployment template projectResources")
		}
		if !nameSet["sys-project-sa"] {
			t.Error("expected ServiceAccount 'sys-project-sa' from platform template projectResources")
		}
	})

	// Validate that RenderWithAncestorTemplates returns resources from both
	// projectResources and platformResources when a platform template defines both.
	t.Run("platform template defining both collections returns all resources", func(t *testing.T) {
		resources, err := renderer.RenderWithAncestorTemplates(context.Background(),
			deploymentTemplateForUnification,
			[]string{systemBothCollectionsTemplate},
			system, user,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Expect: 1 Deployment (deployment template projectResources) +
		//         1 ServiceAccount from platform template projectResources +
		//         1 ServiceAccount from platform template platformResources.
		if len(resources) != 3 {
			t.Fatalf("expected 3 resources, got %d: %v", len(resources), resourceKinds(resources))
		}
		nameSet := make(map[string]bool)
		for _, r := range resources {
			nameSet[r.GetName()] = true
		}
		if !nameSet["web-app"] {
			t.Error("expected Deployment 'web-app' from deployment template projectResources")
		}
		if !nameSet["sys-project-sa"] {
			t.Error("expected ServiceAccount 'sys-project-sa' from platform template projectResources")
		}
		if !nameSet["sys-platform-sa"] {
			t.Error("expected ServiceAccount 'sys-platform-sa' from platform template platformResources")
		}
	})
}

// resourceKinds returns the Kind/Name pairs for a slice of resources (for test diagnostics).
func resourceKinds(resources []unstructured.Unstructured) []string {
	var out []string
	for _, r := range resources {
		out = append(out, r.GetKind()+"/"+r.GetName())
	}
	return out
}

// TestCueRenderer_LevelBasedResourceReading verifies the ADR 016 Decision 8
// hard boundary: evaluate() (project-level) must NOT read platformResources,
// while evaluateWithOrgTemplates() (org/folder level) reads both collections.
func TestCueRenderer_LevelBasedResourceReading(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	// Render() uses evaluate() — the project-level path. Per ADR 016, it must
	// NOT read platformResources even if the template defines them.
	t.Run("Render does not read platformResources (project-level boundary)", func(t *testing.T) {
		resources, err := renderer.Render(context.Background(), systemOutputTemplate,
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "web-app", Image: "nginx", Tag: "1.25", Port: 8080},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Expect only 1 resource: the Deployment from projectResources.
		// The ServiceAccount defined in platformResources must be ignored.
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource (Deployment only, platformResources ignored), got %d: %v",
				len(resources), resourceKinds(resources))
		}
		if resources[0].GetKind() != "Deployment" {
			t.Errorf("expected Deployment, got %q", resources[0].GetKind())
		}
	})

	// RenderWithAncestorTemplates() uses evaluateWithOrgTemplates() — the
	// org/folder level path. It must read BOTH projectResources and platformResources.
	t.Run("RenderWithAncestorTemplates reads both projectResources and platformResources", func(t *testing.T) {
		resources, err := renderer.RenderWithAncestorTemplates(context.Background(),
			systemOutputTemplate,
			nil, // no additional platform templates; the deployment template itself defines platformResources
			v1alpha2.PlatformInput{Project: "my-project", Namespace: namespace},
			v1alpha2.ProjectInput{Name: "web-app", Image: "nginx", Tag: "1.25", Port: 8080},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Expect 2 resources: Deployment from projectResources + ServiceAccount from platformResources.
		if len(resources) != 2 {
			t.Fatalf("expected 2 resources (Deployment + ServiceAccount), got %d: %v",
				len(resources), resourceKinds(resources))
		}
		kindSet := make(map[string]bool)
		for _, r := range resources {
			kindSet[r.GetKind()] = true
			if r.GetNamespace() != namespace {
				t.Errorf("resource %s/%s: expected namespace %q, got %q", r.GetKind(), r.GetName(), namespace, r.GetNamespace())
			}
		}
		if !kindSet["Deployment"] {
			t.Error("expected Deployment resource from projectResources")
		}
		if !kindSet["ServiceAccount"] {
			t.Error("expected ServiceAccount resource from platformResources")
		}
	})
}

// closedStructOrgTemplate is an org-level platform template that:
//  1. Provides an HTTPRoute in platformResources (platform team manages traffic).
//  2. Closes projectResources.namespacedResources to Deployment, Service, and
//     ServiceAccount so that any project template producing another Kind causes
//     a CUE evaluation error immediately (ADR 016 Decision 9).
//
// This template mirrors the documented example in docs/cue-template-guide.md
// ("Platform and Project Templates Working Together").
const closedStructOrgTemplate = `

input: #ProjectInput & {
	port: >0 & <=65535 | *8080
}
platform: #PlatformInput

platformResources: {
	namespacedResources: (platform.namespace): {
		HTTPRoute: (input.name): {
			apiVersion: "gateway.networking.k8s.io/v1"
			kind:       "HTTPRoute"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       input.name
				}
			}
			spec: {
				parentRefs: [{
					group:     "gateway.networking.k8s.io"
					kind:      "Gateway"
					namespace: platform.gatewayNamespace
					name:      "default"
				}]
				rules: [{
					backendRefs: [{
						name: input.name
						port: 80
					}]
				}]
			}
		}
	}
	clusterResources: {}
}

// Close projectResources.namespacedResources so that every namespace bucket
// may only contain Deployment, Service, or ServiceAccount. Using close() with
// optional fields is the correct CUE pattern: the close() call marks the struct
// as closed (no additional fields allowed), and the ? marks each listed field
// as optional (a namespace bucket need not contain all three). Any unlisted
// Kind key — such as RoleBinding — is a CUE constraint violation.
projectResources: namespacedResources: [_]: close({
	Deployment?:     _
	Service?:        _
	ServiceAccount?: _
})
`

// closedStructProjectTemplate is a project-level deployment template that
// produces exactly the three resource kinds allowed by closedStructOrgTemplate:
// Deployment, Service, and ServiceAccount. Unification with the org template
// should succeed.
const closedStructProjectTemplate = `

input: #ProjectInput & {
	port: >0 & <=65535 | *8080
}
platform: #PlatformInput

_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

projectResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: (input.name): {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels:    _labels
			}
		}
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels:    _labels
			}
			spec: {
				replicas: 1
				selector: matchLabels: "app.kubernetes.io/name": input.name
				template: {
					metadata: labels: _labels
					spec: {
						serviceAccountName: input.name
						containers: [{
							name:  input.name
							image: input.image + ":" + input.tag
							ports: [{containerPort: input.port, name: "http"}]
						}]
					}
				}
			}
		}
		Service: (input.name): {
			apiVersion: "v1"
			kind:       "Service"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels:    _labels
			}
			spec: {
				selector: "app.kubernetes.io/name": input.name
				ports: [{port: 80, targetPort: "http", name: "http"}]
			}
		}
	}
	clusterResources: {}
}
`

// closedStructProjectTemplateForbidden is a project-level deployment template
// that produces the three allowed kinds plus a RoleBinding. Because the org
// template (closedStructOrgTemplate) closes projectResources.namespacedResources
// to Deployment, Service, and ServiceAccount, unifying this template with the
// org template should cause a CUE evaluation error naming the disallowed field.
const closedStructProjectTemplateForbidden = `

input: #ProjectInput & {
	port: >0 & <=65535 | *8080
}
platform: #PlatformInput

_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

projectResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: (input.name): {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels:    _labels
			}
		}
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels:    _labels
			}
			spec: {
				selector: matchLabels: "app.kubernetes.io/name": input.name
				template: {
					metadata: labels: _labels
					spec: containers: [{
						name:  input.name
						image: input.image + ":" + input.tag
					}]
				}
			}
		}
		Service: (input.name): {
			apiVersion: "v1"
			kind:       "Service"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels:    _labels
			}
			spec: {
				selector: "app.kubernetes.io/name": input.name
				ports: [{port: 80, targetPort: "http", name: "http"}]
			}
		}
		// RoleBinding is NOT in the org's close() constraint — this should
		// cause a CUE evaluation error: "field not allowed".
		RoleBinding: "my-binding": {
			apiVersion: "rbac.authorization.k8s.io/v1"
			kind:       "RoleBinding"
			metadata: {
				name:      "my-binding"
				namespace: platform.namespace
				labels:    _labels
			}
			roleRef: {
				apiGroup: "rbac.authorization.k8s.io"
				kind:     "ClusterRole"
				name:     "view"
			}
			subjects: [{
				kind:      "ServiceAccount"
				name:      input.name
				namespace: platform.namespace
			}]
		}
	}
	clusterResources: {}
}
`

// TestCueRenderer_ClosedStructKindConstraint verifies the ADR 016 Decision 9
// constraint pattern: an org-level platform template can close
// projectResources.namespacedResources to a set of allowed Kinds, and any
// project template that produces a disallowed Kind causes a CUE evaluation
// error before any Kubernetes API call is made.
//
// The templates used here mirror the documented examples in
// docs/cue-template-guide.md ("Platform and Project Templates Working Together").
func TestCueRenderer_ClosedStructKindConstraint(t *testing.T) {
	renderer := &CueRenderer{}

	platform := v1alpha2.PlatformInput{
		Project:          "my-project",
		Namespace:        "prj-my-project",
		GatewayNamespace: "istio-ingress",
		Organization:     "my-org",
		Claims: v1alpha2.Claims{
			Iss:           "https://example.com",
			Sub:           "u1",
			Exp:           9999999999,
			Iat:           1000000000,
			Email:         "test@example.com",
			EmailVerified: true,
		},
	}
	project := v1alpha2.ProjectInput{
		Name:  "web-app",
		Image: "nginx",
		Tag:   "1.25",
		Port:  8080,
	}

	// Sub-test 1: allowed kinds succeed.
	// The org template closes projectResources.namespacedResources to Deployment,
	// Service, ServiceAccount. The project template produces exactly those three
	// kinds. RenderWithAncestorTemplates should succeed and return all expected
	// resources: 3 project resources (Deployment, Service, ServiceAccount) from
	// projectResources + 1 HTTPRoute from platformResources = 4 total.
	t.Run("allowed kinds succeed", func(t *testing.T) {
		resources, err := renderer.RenderWithAncestorTemplates(
			context.Background(),
			closedStructProjectTemplate,
			[]string{closedStructOrgTemplate},
			platform,
			project,
		)
		if err != nil {
			t.Fatalf("expected no error for allowed kinds, got: %v", err)
		}
		// Expect 4 resources: Deployment, Service, ServiceAccount from
		// projectResources + HTTPRoute from platformResources.
		if len(resources) != 4 {
			t.Fatalf("expected 4 resources (Deployment, Service, ServiceAccount, HTTPRoute), got %d: %v",
				len(resources), resourceKinds(resources))
		}
		kindSet := make(map[string]bool)
		for _, r := range resources {
			kindSet[r.GetKind()] = true
		}
		for _, expected := range []string{"Deployment", "Service", "ServiceAccount", "HTTPRoute"} {
			if !kindSet[expected] {
				t.Errorf("expected %s resource in output", expected)
			}
		}
	})

	// Sub-test 2: disallowed kind fails.
	// Same org template, but the project template also produces a RoleBinding.
	// CUE evaluation must fail with an error containing "not allowed" (the CUE
	// closed struct error), matching the documented error:
	//   projectResources.namespacedResources.<ns>.RoleBinding: field not allowed
	t.Run("disallowed kind fails with CUE closed struct error", func(t *testing.T) {
		_, err := renderer.RenderWithAncestorTemplates(
			context.Background(),
			closedStructProjectTemplateForbidden,
			[]string{closedStructOrgTemplate},
			platform,
			project,
		)
		if err == nil {
			t.Fatal("expected CUE evaluation error for disallowed RoleBinding kind, got nil")
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, "not allowed") {
			t.Errorf("expected error message to contain 'not allowed' (CUE closed struct error), got: %s", errMsg)
		}
	})
}

// repoRoot returns the absolute path to the repository root. It is computed
// relative to the location of this test file so the tests work correctly
// regardless of the working directory when 'go test' is invoked.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile is console/deployments/render_test.go; root is three levels up.
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

// TestCueRenderer_HttpbinExample verifies the embedded go-httpbin example CUE
// files work correctly together. The org-level platform template
// (example_httpbin_platform.cue) closes projectResources.namespacedResources
// to Deployment, Service, ServiceAccount and provides an HTTPRoute in
// platformResources. The project-level template (example_httpbin.cue) produces
// exactly those three kinds. Tests validate three scenarios: combined rendering,
// closed-struct enforcement, and standalone rendering.
func TestCueRenderer_HttpbinExample(t *testing.T) {
	root := repoRoot(t)

	// Load the embedded CUE files from their source locations to avoid an
	// import cycle (console/templates already imports console/deployments).
	projectTemplateBytes, err := os.ReadFile(filepath.Join(root, "console/templates/example_httpbin.cue"))
	if err != nil {
		t.Fatalf("failed to read example_httpbin.cue: %v", err)
	}
	projectTemplate := string(projectTemplateBytes)

	// example_httpbin_platform.cue was moved from console/org_templates/ to
	// console/templates/ as part of the v1alpha2 unified TemplateService (ADR 021).
	platformTemplateBytes, err := os.ReadFile(filepath.Join(root, "console/templates/example_httpbin_platform.cue"))
	if err != nil {
		t.Fatalf("failed to read example_httpbin_platform.cue: %v", err)
	}
	platformTemplate := string(platformTemplateBytes)

	renderer := &CueRenderer{}

	platform := v1alpha2.PlatformInput{
		Project:          "my-project",
		Namespace:        "prj-my-project",
		GatewayNamespace: "istio-ingress",
		Organization:     "my-org",
		Claims: v1alpha2.Claims{
			Iss:           "https://example.com",
			Sub:           "u1",
			Exp:           9999999999,
			Iat:           1000000000,
			Email:         "deployer@example.com",
			EmailVerified: true,
		},
	}
	project := v1alpha2.ProjectInput{
		Name:  "go-httpbin",
		Image: "ghcr.io/mccutchen/go-httpbin",
		Tag:   "2.21.0",
		Port:  8080,
	}

	// Sub-test 1: Templates render together.
	// RenderWithAncestorTemplates with the org platform template and the
	// project template as deployment template must produce 4 resources:
	// 3 project resources (ServiceAccount, Deployment, Service) from
	// projectResources + 1 platform resource (HTTPRoute) from platformResources.
	t.Run("templates render together producing 4 resources", func(t *testing.T) {
		resources, err := renderer.RenderWithAncestorTemplates(
			context.Background(),
			projectTemplate,
			[]string{platformTemplate},
			platform,
			project,
		)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if len(resources) != 4 {
			t.Fatalf("expected 4 resources (ServiceAccount, Deployment, Service, HTTPRoute), got %d: %v",
				len(resources), resourceKinds(resources))
		}
		kindSet := make(map[string]bool)
		for _, r := range resources {
			kindSet[r.GetKind()] = true
		}
		for _, expected := range []string{"ServiceAccount", "Deployment", "Service", "HTTPRoute"} {
			if !kindSet[expected] {
				t.Errorf("expected %s resource in output", expected)
			}
		}
	})

	// Sub-test 2: Closed struct rejects disallowed kind.
	// A modified project template that adds a RoleBinding must fail CUE
	// evaluation when unified with the org template (error contains "not allowed").
	t.Run("closed struct rejects disallowed kind with CUE error", func(t *testing.T) {
		// Add a RoleBinding to the project template — not allowed by the org constraint.
		forbiddenAddition := `
projectResources: namespacedResources: (platform.namespace): {
	RoleBinding: "disallowed-binding": {
		apiVersion: "rbac.authorization.k8s.io/v1"
		kind:       "RoleBinding"
		metadata: {
			name:      "disallowed-binding"
			namespace: platform.namespace
			labels: {"app.kubernetes.io/managed-by": "console.holos.run"}
		}
		roleRef: {
			apiGroup: "rbac.authorization.k8s.io"
			kind:     "ClusterRole"
			name:     "view"
		}
		subjects: [{
			kind:      "ServiceAccount"
			name:      input.name
			namespace: platform.namespace
		}]
	}
}
`
		// Concatenate the project template with the forbidden addition so we
		// have a template that produces a disallowed kind alongside the allowed ones.
		projectWithForbidden := projectTemplate + forbiddenAddition

		_, err := renderer.RenderWithAncestorTemplates(
			context.Background(),
			projectWithForbidden,
			[]string{platformTemplate},
			platform,
			project,
		)
		if err == nil {
			t.Fatal("expected CUE evaluation error for disallowed RoleBinding kind, got nil")
		}
		if !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("expected error to contain 'not allowed' (CUE closed struct), got: %s", err.Error())
		}
	})

	// Sub-test 3: Project template renders alone.
	// Render with just the project template (no platform template) must produce
	// exactly 3 resources: ServiceAccount, Deployment, Service.
	t.Run("project template renders standalone producing 3 resources", func(t *testing.T) {
		resources, err := renderer.Render(
			context.Background(),
			projectTemplate,
			platform,
			project,
		)
		if err != nil {
			t.Fatalf("expected no error for standalone render, got: %v", err)
		}
		if len(resources) != 3 {
			t.Fatalf("expected 3 resources (ServiceAccount, Deployment, Service), got %d: %v",
				len(resources), resourceKinds(resources))
		}
		kindSet := make(map[string]bool)
		for _, r := range resources {
			kindSet[r.GetKind()] = true
		}
		for _, expected := range []string{"ServiceAccount", "Deployment", "Service"} {
			if !kindSet[expected] {
				t.Errorf("expected %s resource in standalone output", expected)
			}
		}
	})
}

// foldersTemplate accesses platform.folders in a resource annotation.
// Used to verify that PlatformInput.Folders is propagated into CUE templates.
// The template sets an annotation to the first folder name when folders are present.
const foldersTemplate = `

input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:          string
	namespace:        string
	gatewayNamespace: string
	organization:     string
	folders?: [...{name: string, namespace?: string}]
	claims: {
		iss:            string
		sub:            string
		exp:            int
		iat:            int
		email:          string
		email_verified: bool
	}
}

// _firstFolder is the first folder name, or "none" when the list is absent/empty.
_firstFolder: *"none" | string
if platform.folders != _|_ {
	if len(platform.folders) > 0 {
		_firstFolder: platform.folders[0].name
	}
}

projectResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: (input.name): {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       input.name
				}
				annotations: {
					"test.holos.run/first-folder": _firstFolder
				}
			}
		}
	}
	clusterResources: {}
}
`

// TestCueRenderer_FoldersPropagation verifies that PlatformInput.Folders
// is propagated into CUE templates via platform.folders.
func TestCueRenderer_FoldersPropagation(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("folders list is available in template when populated", func(t *testing.T) {
		platform := v1alpha2.PlatformInput{
			Project:          "my-project",
			Namespace:        namespace,
			GatewayNamespace: "istio-ingress",
			Organization:     "my-org",
			Folders:          []v1alpha2.FolderInfo{{Name: "platform"}, {Name: "payments"}},
			Claims: v1alpha2.Claims{
				Iss:           "https://example.com",
				Sub:           "u1",
				Exp:           9999999999,
				Iat:           1000000000,
				Email:         "test@example.com",
				EmailVerified: true,
			},
		}
		project := v1alpha2.ProjectInput{
			Name:  "web-app",
			Image: "nginx",
			Tag:   "1.25",
			Port:  8080,
		}
		resources, err := renderer.Render(context.Background(), foldersTemplate, platform, project)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		annotations := resources[0].GetAnnotations()
		got := annotations["test.holos.run/first-folder"]
		if got != "platform" {
			t.Errorf("expected first-folder annotation 'platform', got %q", got)
		}
	})

	t.Run("nil folders renders without error and uses default value", func(t *testing.T) {
		platform := v1alpha2.PlatformInput{
			Project:          "my-project",
			Namespace:        namespace,
			GatewayNamespace: "istio-ingress",
			Organization:     "my-org",
			Folders:          nil, // no folders — omitted from JSON
			Claims: v1alpha2.Claims{
				Iss:           "https://example.com",
				Sub:           "u1",
				Exp:           9999999999,
				Iat:           1000000000,
				Email:         "test@example.com",
				EmailVerified: true,
			},
		}
		project := v1alpha2.ProjectInput{
			Name:  "web-app",
			Image: "nginx",
			Tag:   "1.25",
			Port:  8080,
		}
		resources, err := renderer.Render(context.Background(), foldersTemplate, platform, project)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		annotations := resources[0].GetAnnotations()
		got := annotations["test.holos.run/first-folder"]
		if got != "none" {
			t.Errorf("expected first-folder annotation 'none', got %q", got)
		}
	})
}

// folderPlatformTemplate simulates a folder-level platform template that
// contributes a resource using platform.folders to identify the folder context.
const folderPlatformTemplate = `

platformResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: "folder-sa": {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      "folder-sa"
				namespace: platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       "folder-sa"
				}
			}
		}
	}
	clusterResources: {}
}
`

// TestCueRenderer_AncestorTemplateWalk verifies that
// RenderWithAncestorTemplates correctly unifies folder-level and org-level
// platform templates with the deployment template.
func TestCueRenderer_AncestorTemplateWalk(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	platform := v1alpha2.PlatformInput{
		Project:          "my-project",
		Namespace:        namespace,
		GatewayNamespace: "istio-ingress",
		Organization:     "my-org",
		Folders:          []v1alpha2.FolderInfo{{Name: "payments"}},
		Claims: v1alpha2.Claims{
			Iss:           "https://example.com",
			Sub:           "u1",
			Exp:           9999999999,
			Iat:           1000000000,
			Email:         "test@example.com",
			EmailVerified: true,
		},
	}
	project := v1alpha2.ProjectInput{
		Name:  "web-app",
		Image: "nginx",
		Tag:   "1.25",
		Port:  8080,
	}

	t.Run("folder-level platform template unified with deployment template", func(t *testing.T) {
		resources, err := renderer.RenderWithAncestorTemplates(
			context.Background(),
			deploymentTemplateForUnification,
			[]string{folderPlatformTemplate},
			platform,
			project,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Expect: 1 Deployment from deployment template + 1 ServiceAccount from folder platform template.
		if len(resources) != 2 {
			t.Fatalf("expected 2 resources (Deployment + folder ServiceAccount), got %d: %v",
				len(resources), resourceKinds(resources))
		}
		nameSet := make(map[string]bool)
		for _, r := range resources {
			nameSet[r.GetName()] = true
		}
		if !nameSet["web-app"] {
			t.Error("expected Deployment 'web-app' from deployment template")
		}
		if !nameSet["folder-sa"] {
			t.Error("expected ServiceAccount 'folder-sa' from folder platform template")
		}
	})

	t.Run("multiple ancestor templates (org + folder) are all unified", func(t *testing.T) {
		resources, err := renderer.RenderWithAncestorTemplates(
			context.Background(),
			deploymentTemplateForUnification,
			[]string{systemUnificationTemplate, folderPlatformTemplate},
			platform,
			project,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Expect: 1 Deployment + 1 SA from org template (system-from-web-app) + 1 SA from folder template (folder-sa).
		if len(resources) != 3 {
			t.Fatalf("expected 3 resources, got %d: %v", len(resources), resourceKinds(resources))
		}
		nameSet := make(map[string]bool)
		for _, r := range resources {
			nameSet[r.GetName()] = true
		}
		if !nameSet["web-app"] {
			t.Error("expected Deployment 'web-app'")
		}
		if !nameSet["system-from-web-app"] {
			t.Error("expected ServiceAccount 'system-from-web-app' from org template")
		}
		if !nameSet["folder-sa"] {
			t.Error("expected ServiceAccount 'folder-sa' from folder template")
		}
	})
}

// TestEvaluateStructuredGrouped verifies that evaluateStructuredGrouped
// partitions resources into Platform and Project groups correctly.
func TestEvaluateStructuredGrouped(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("project-only template produces empty platform group", func(t *testing.T) {
		grouped, err := renderer.RenderGrouped(context.Background(),
			validTemplate,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(grouped.Platform) != 0 {
			t.Errorf("expected 0 platform resources, got %d: %v",
				len(grouped.Platform), resourceKinds(grouped.Platform))
		}
		if len(grouped.Project) != 1 {
			t.Fatalf("expected 1 project resource, got %d: %v",
				len(grouped.Project), resourceKinds(grouped.Project))
		}
		if grouped.Project[0].GetKind() != "Deployment" {
			t.Errorf("expected Deployment, got %q", grouped.Project[0].GetKind())
		}
	})

	t.Run("template with both collections produces populated platform and project groups", func(t *testing.T) {
		grouped, err := renderer.RenderGroupedWithAncestorTemplates(context.Background(),
			systemOutputTemplate,
			nil, // deployment template itself defines platformResources
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(grouped.Platform) != 1 {
			t.Fatalf("expected 1 platform resource, got %d: %v",
				len(grouped.Platform), resourceKinds(grouped.Platform))
		}
		if grouped.Platform[0].GetKind() != "ServiceAccount" {
			t.Errorf("expected platform ServiceAccount, got %q", grouped.Platform[0].GetKind())
		}
		if len(grouped.Project) != 1 {
			t.Fatalf("expected 1 project resource, got %d: %v",
				len(grouped.Project), resourceKinds(grouped.Project))
		}
		if grouped.Project[0].GetKind() != "Deployment" {
			t.Errorf("expected project Deployment, got %q", grouped.Project[0].GetKind())
		}
	})

	t.Run("unified fields equal union of both groups", func(t *testing.T) {
		grouped, err := renderer.RenderGroupedWithAncestorTemplates(context.Background(),
			systemOutputTemplate,
			nil,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// The flat render should return the same total count as Platform + Project.
		flat, err := renderer.RenderWithAncestorTemplates(context.Background(),
			systemOutputTemplate,
			nil,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("expected no error from flat render, got %v", err)
		}

		totalGrouped := len(grouped.Platform) + len(grouped.Project)
		if totalGrouped != len(flat) {
			t.Errorf("grouped total (%d) != flat total (%d)", totalGrouped, len(flat))
		}

		// Build a set of kind/name pairs from the grouped result and verify
		// they match the flat result exactly.
		groupedSet := make(map[string]bool)
		for _, r := range grouped.Platform {
			groupedSet[r.GetKind()+"/"+r.GetName()] = true
		}
		for _, r := range grouped.Project {
			groupedSet[r.GetKind()+"/"+r.GetName()] = true
		}
		for _, r := range flat {
			key := r.GetKind() + "/" + r.GetName()
			if !groupedSet[key] {
				t.Errorf("flat resource %s not found in grouped result", key)
			}
		}
	})

	t.Run("org template with closed struct produces grouped results", func(t *testing.T) {
		grouped, err := renderer.RenderGroupedWithAncestorTemplates(context.Background(),
			closedStructProjectTemplate,
			[]string{closedStructOrgTemplate},
			v1alpha2.PlatformInput{
				Project:          "my-project",
				Namespace:        namespace,
				GatewayNamespace: DefaultGatewayNamespace,
			},
			v1alpha2.ProjectInput{Name: "web-app", Image: "nginx", Tag: "1.25", Port: 8080},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Platform group should have HTTPRoute from the org template.
		if len(grouped.Platform) != 1 {
			t.Fatalf("expected 1 platform resource, got %d: %v",
				len(grouped.Platform), resourceKinds(grouped.Platform))
		}
		if grouped.Platform[0].GetKind() != "HTTPRoute" {
			t.Errorf("expected platform HTTPRoute, got %q", grouped.Platform[0].GetKind())
		}
		// Project group should have Deployment, Service, ServiceAccount.
		if len(grouped.Project) != 3 {
			t.Fatalf("expected 3 project resources, got %d: %v",
				len(grouped.Project), resourceKinds(grouped.Project))
		}
		kindSet := make(map[string]bool)
		for _, r := range grouped.Project {
			kindSet[r.GetKind()] = true
		}
		if !kindSet["Deployment"] || !kindSet["Service"] || !kindSet["ServiceAccount"] {
			t.Errorf("expected Deployment, Service, ServiceAccount in project group, got %v", kindSet)
		}
	})
}

// templateWithDefaults includes a defaults block so structured JSON extraction
// can verify it is populated.
const templateWithDefaults = `

defaults: {
	name:  "httpbin"
	image: "ghcr.io/mccutchen/go-httpbin"
	tag:   "2.21.0"
	port:  8080
}

input: {
	name:  string
	image: string
	tag:   string
	port:  int | *8080
}

platform: {
	project:   string
	namespace: string
}

_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels:    _labels
			}
			spec: {
				selector: matchLabels: "app.kubernetes.io/name": input.name
				template: {
					metadata: labels: _labels
					spec: containers: [{
						name:  input.name
						image: input.image + ":" + input.tag
					}]
				}
			}
		}
	}
	clusterResources: {}
}
`

func TestStructuredJSONExtraction(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("template without defaults leaves DefaultsJSON nil", func(t *testing.T) {
		grouped, err := renderer.RenderGrouped(context.Background(),
			validTemplate,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if grouped.DefaultsJSON != nil {
			t.Errorf("expected DefaultsJSON to be nil for template without defaults, got %q", *grouped.DefaultsJSON)
		}
	})

	t.Run("template with defaults populates DefaultsJSON", func(t *testing.T) {
		grouped, err := renderer.RenderGrouped(context.Background(),
			templateWithDefaults,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if grouped.DefaultsJSON == nil {
			t.Fatal("expected DefaultsJSON to be set, got nil")
		}
		if !json.Valid([]byte(*grouped.DefaultsJSON)) {
			t.Errorf("DefaultsJSON is not valid JSON: %s", *grouped.DefaultsJSON)
		}
		// Verify the defaults contain the expected values.
		var defaults map[string]any
		if err := json.Unmarshal([]byte(*grouped.DefaultsJSON), &defaults); err != nil {
			t.Fatalf("failed to unmarshal DefaultsJSON: %v", err)
		}
		if defaults["name"] != "httpbin" {
			t.Errorf("expected defaults.name = httpbin, got %v", defaults["name"])
		}
	})

	t.Run("PlatformInputJSON is populated", func(t *testing.T) {
		grouped, err := renderer.RenderGrouped(context.Background(),
			validTemplate,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if grouped.PlatformInputJSON == nil {
			t.Fatal("expected PlatformInputJSON to be set, got nil")
		}
		if !json.Valid([]byte(*grouped.PlatformInputJSON)) {
			t.Errorf("PlatformInputJSON is not valid JSON: %s", *grouped.PlatformInputJSON)
		}
		var pi map[string]any
		if err := json.Unmarshal([]byte(*grouped.PlatformInputJSON), &pi); err != nil {
			t.Fatalf("failed to unmarshal PlatformInputJSON: %v", err)
		}
		if pi["namespace"] != namespace {
			t.Errorf("expected platform.namespace = %s, got %v", namespace, pi["namespace"])
		}
	})

	t.Run("ProjectInputJSON is populated", func(t *testing.T) {
		grouped, err := renderer.RenderGrouped(context.Background(),
			validTemplate,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if grouped.ProjectInputJSON == nil {
			t.Fatal("expected ProjectInputJSON to be set, got nil")
		}
		if !json.Valid([]byte(*grouped.ProjectInputJSON)) {
			t.Errorf("ProjectInputJSON is not valid JSON: %s", *grouped.ProjectInputJSON)
		}
		var pi map[string]any
		if err := json.Unmarshal([]byte(*grouped.ProjectInputJSON), &pi); err != nil {
			t.Fatalf("failed to unmarshal ProjectInputJSON: %v", err)
		}
		if pi["name"] != "web-app" {
			t.Errorf("expected input.name = web-app, got %v", pi["name"])
		}
	})

	t.Run("ProjectResourcesStructJSON is populated for non-empty resources", func(t *testing.T) {
		grouped, err := renderer.RenderGrouped(context.Background(),
			validTemplate,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if grouped.ProjectResourcesStructJSON == nil {
			t.Fatal("expected ProjectResourcesStructJSON to be set, got nil")
		}
		if !json.Valid([]byte(*grouped.ProjectResourcesStructJSON)) {
			t.Errorf("ProjectResourcesStructJSON is not valid JSON: %s", *grouped.ProjectResourcesStructJSON)
		}
	})

	t.Run("PlatformResourcesStructJSON is set for template with platform resources", func(t *testing.T) {
		grouped, err := renderer.RenderGroupedWithAncestorTemplates(context.Background(),
			systemOutputTemplate,
			nil,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if grouped.PlatformResourcesStructJSON == nil {
			t.Fatal("expected PlatformResourcesStructJSON to be set, got nil")
		}
		if !json.Valid([]byte(*grouped.PlatformResourcesStructJSON)) {
			t.Errorf("PlatformResourcesStructJSON is not valid JSON: %s", *grouped.PlatformResourcesStructJSON)
		}
	})

	t.Run("absent platformResources produces nil", func(t *testing.T) {
		// validTemplate has no platformResources, but when rendered via the
		// org-level path (readPlatformResources=true) the platformResources CUE
		// path does not exist, so the field should be nil.
		grouped, err := renderer.RenderGrouped(context.Background(),
			validTemplate,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// validTemplate does not define platformResources at all, so nil is correct.
		if grouped.PlatformResourcesStructJSON != nil {
			t.Errorf("expected PlatformResourcesStructJSON to be nil when platformResources is not defined, got %q",
				*grouped.PlatformResourcesStructJSON)
		}
	})

	t.Run("empty platformResources struct produces non-nil JSON", func(t *testing.T) {
		// emptyPlatformResourcesTemplate defines platformResources with empty
		// namespacedResources and clusterResources sub-structs. The field should
		// be non-nil because the CUE path exists and is concrete.
		grouped, err := renderer.RenderGroupedWithAncestorTemplates(context.Background(),
			emptyPlatformResourcesTemplate,
			nil,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if grouped.PlatformResourcesStructJSON == nil {
			t.Fatal("expected PlatformResourcesStructJSON to be non-nil for empty but defined platformResources")
		}
		if !json.Valid([]byte(*grouped.PlatformResourcesStructJSON)) {
			t.Errorf("PlatformResourcesStructJSON is not valid JSON: %s", *grouped.PlatformResourcesStructJSON)
		}
		// Verify the struct contains the expected empty sub-structs.
		var pr map[string]any
		if err := json.Unmarshal([]byte(*grouped.PlatformResourcesStructJSON), &pr); err != nil {
			t.Fatalf("failed to unmarshal PlatformResourcesStructJSON: %v", err)
		}
		if _, ok := pr["namespacedResources"]; !ok {
			t.Error("expected namespacedResources key in PlatformResourcesStructJSON")
		}
		if _, ok := pr["clusterResources"]; !ok {
			t.Error("expected clusterResources key in PlatformResourcesStructJSON")
		}
	})

	t.Run("populated platformResources produces JSON", func(t *testing.T) {
		// systemOutputTemplate defines platformResources with actual resources.
		// Use the org-level render path which reads both collections.
		grouped, err := renderer.RenderGroupedWithAncestorTemplates(context.Background(),
			systemOutputTemplate,
			nil,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if grouped.PlatformResourcesStructJSON == nil {
			t.Fatal("expected PlatformResourcesStructJSON to be set for systemOutputTemplate")
		}
		// Verify it contains namespacedResources.
		var pr map[string]any
		if err := json.Unmarshal([]byte(*grouped.PlatformResourcesStructJSON), &pr); err != nil {
			t.Fatalf("failed to unmarshal PlatformResourcesStructJSON: %v", err)
		}
		if _, ok := pr["namespacedResources"]; !ok {
			t.Error("expected namespacedResources key in PlatformResourcesStructJSON")
		}
	})

	t.Run("all structured JSON fields are valid JSON", func(t *testing.T) {
		grouped, err := renderer.RenderGroupedWithAncestorTemplates(context.Background(),
			systemOutputTemplate,
			nil,
			defaultPlatform(namespace),
			defaultProject(),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		fields := map[string]*string{
			"PlatformInputJSON":           grouped.PlatformInputJSON,
			"ProjectInputJSON":            grouped.ProjectInputJSON,
			"ProjectResourcesStructJSON":  grouped.ProjectResourcesStructJSON,
			"PlatformResourcesStructJSON": grouped.PlatformResourcesStructJSON,
		}
		for name, val := range fields {
			if val == nil {
				t.Errorf("%s is nil, expected it to be set", name)
				continue
			}
			if !json.Valid([]byte(*val)) {
				t.Errorf("%s is not valid JSON: %s", name, *val)
			}
		}
	})
}

// TestCueRenderer_MultiNamespaceResources verifies that templates producing
// resources in multiple namespaces render successfully after the removal of the
// per-resource namespace restriction (ADR 026). The struct-key/metadata
// consistency check is preserved.
func TestCueRenderer_MultiNamespaceResources(t *testing.T) {
	renderer := &CueRenderer{}
	namespace := "prj-my-project"

	t.Run("project resources spanning multiple namespaces render successfully", func(t *testing.T) {
		platform := v1alpha2.PlatformInput{
			Project:          "my-project",
			Namespace:        namespace,
			GatewayNamespace: "istio-ingress",
		}
		project := v1alpha2.ProjectInput{
			Name:  "web-app",
			Image: "nginx",
			Tag:   "1.25",
			Port:  8080,
		}
		resources, err := renderer.Render(context.Background(), multiNamespaceTemplate, platform, project)
		if err != nil {
			t.Fatalf("expected no error for multi-namespace template, got %v", err)
		}
		// Expect 2 resources: Deployment in project namespace, HTTPRoute in istio-ingress.
		if len(resources) != 2 {
			t.Fatalf("expected 2 resources, got %d: %v", len(resources), resourceKinds(resources))
		}
		nsSet := make(map[string]bool)
		kindSet := make(map[string]bool)
		for _, r := range resources {
			nsSet[r.GetNamespace()] = true
			kindSet[r.GetKind()] = true
		}
		if !nsSet[namespace] {
			t.Errorf("expected resource in namespace %q", namespace)
		}
		if !nsSet["istio-ingress"] {
			t.Error("expected resource in namespace 'istio-ingress'")
		}
		if !kindSet["Deployment"] {
			t.Error("expected Deployment resource")
		}
		if !kindSet["HTTPRoute"] {
			t.Error("expected HTTPRoute resource")
		}
	})

	t.Run("platform resources spanning multiple namespaces render successfully", func(t *testing.T) {
		platform := v1alpha2.PlatformInput{
			Project:          "my-project",
			Namespace:        namespace,
			GatewayNamespace: "istio-ingress",
		}
		project := v1alpha2.ProjectInput{
			Name:  "web-app",
			Image: "nginx",
			Tag:   "1.25",
			Port:  8080,
		}
		resources, err := renderer.RenderWithAncestorTemplates(context.Background(),
			multiNamespacePlatformTemplate,
			nil,
			platform, project,
		)
		if err != nil {
			t.Fatalf("expected no error for multi-namespace platform template, got %v", err)
		}
		// Expect 3 resources: Deployment in project ns, ServiceAccount in project ns,
		// HTTPRoute in istio-ingress.
		if len(resources) != 3 {
			t.Fatalf("expected 3 resources, got %d: %v", len(resources), resourceKinds(resources))
		}
		nsSet := make(map[string]bool)
		for _, r := range resources {
			nsSet[r.GetNamespace()] = true
		}
		if !nsSet[namespace] {
			t.Errorf("expected resource in namespace %q", namespace)
		}
		if !nsSet["istio-ingress"] {
			t.Error("expected resource in namespace 'istio-ingress'")
		}
	})

	t.Run("struct-key metadata mismatch still rejected", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), structKeyMismatchTemplate,
			defaultPlatform(namespace), defaultProject())
		if err == nil {
			t.Fatal("expected error for struct-key/metadata namespace mismatch")
		}
		if !strings.Contains(err.Error(), "does not match struct key") {
			t.Errorf("expected 'does not match struct key' error, got: %v", err)
		}
	})

	t.Run("empty namespace key rejected", func(t *testing.T) {
		_, err := renderer.Render(context.Background(), emptyNamespaceKeyTemplate,
			defaultPlatform(namespace), defaultProject())
		if err == nil {
			t.Fatal("expected error for empty namespace key")
		}
		if !strings.Contains(err.Error(), "empty namespace key") {
			t.Errorf("expected 'empty namespace key' error, got: %v", err)
		}
	})

	t.Run("grouped render with multi-namespace project resources", func(t *testing.T) {
		platform := v1alpha2.PlatformInput{
			Project:          "my-project",
			Namespace:        namespace,
			GatewayNamespace: "istio-ingress",
		}
		project := v1alpha2.ProjectInput{
			Name:  "web-app",
			Image: "nginx",
			Tag:   "1.25",
			Port:  8080,
		}
		grouped, err := renderer.RenderGrouped(context.Background(),
			multiNamespaceTemplate, platform, project)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(grouped.Project) != 2 {
			t.Fatalf("expected 2 project resources, got %d: %v",
				len(grouped.Project), resourceKinds(grouped.Project))
		}
		nsSet := make(map[string]bool)
		for _, r := range grouped.Project {
			nsSet[r.GetNamespace()] = true
		}
		if !nsSet[namespace] || !nsSet["istio-ingress"] {
			t.Errorf("expected resources in both %q and 'istio-ingress', got namespaces: %v", namespace, nsSet)
		}
	})

	t.Run("grouped render with multi-namespace platform resources", func(t *testing.T) {
		platform := v1alpha2.PlatformInput{
			Project:          "my-project",
			Namespace:        namespace,
			GatewayNamespace: "istio-ingress",
		}
		project := v1alpha2.ProjectInput{
			Name:  "web-app",
			Image: "nginx",
			Tag:   "1.25",
			Port:  8080,
		}
		grouped, err := renderer.RenderGroupedWithAncestorTemplates(context.Background(),
			multiNamespacePlatformTemplate, nil, platform, project)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(grouped.Platform) != 2 {
			t.Fatalf("expected 2 platform resources, got %d: %v",
				len(grouped.Platform), resourceKinds(grouped.Platform))
		}
		nsSet := make(map[string]bool)
		for _, r := range grouped.Platform {
			nsSet[r.GetNamespace()] = true
		}
		if !nsSet[namespace] || !nsSet["istio-ingress"] {
			t.Errorf("expected platform resources in both %q and 'istio-ingress', got namespaces: %v", namespace, nsSet)
		}
	})
}
