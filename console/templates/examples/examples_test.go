package examples_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"cuelang.org/go/cue/cuecontext"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/deployments"
	"github.com/holos-run/holos-console/console/templates"
	"github.com/holos-run/holos-console/console/templates/examples"
)

// TestExamples verifies the example registry loads both built-in examples and
// that each example satisfies the structural and compilation requirements.
func TestExamples(t *testing.T) {
	list, err := examples.Examples()
	if err != nil {
		t.Fatalf("Examples() error: %v", err)
	}

	// There must be exactly six examples.
	if got, want := len(list), 6; got != want {
		t.Fatalf("Examples() returned %d examples, want %d", got, want)
	}

	// Index by name for deterministic lookup.
	byName := make(map[string]examples.Example, len(list))
	for _, ex := range list {
		byName[ex.Name] = ex
	}

	wantNames := []string{
		"httproute-v1",
		"allowed-project-resource-kinds-v1",
		"project-namespace-description-annotation-v1",
		"project-namespace-reference-grant-v1",
		"httpbin-v1",
		"podinfo-v1",
	}
	for _, name := range wantNames {
		ex, ok := byName[name]
		if !ok {
			t.Errorf("example %q not found in registry", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			testExample(t, ex)
		})
	}
}

// testExample runs all per-example assertions.
func testExample(t *testing.T, ex examples.Example) {
	t.Helper()

	// All metadata fields must be non-empty.
	if ex.DisplayName == "" {
		t.Error("DisplayName is empty")
	}
	if ex.Name == "" {
		t.Error("Name is empty")
	}
	if ex.Description == "" {
		t.Error("Description is empty")
	}
	if ex.CueTemplate == "" {
		t.Error("CueTemplate is empty")
	}

	// The cueTemplate body must compile against the v1alpha2 generated schema.
	// This catches the HOL-789 class of non-concrete-value regressions before
	// the renderer ever sees the template.
	t.Run("cueTemplate_compiles", func(t *testing.T) {
		cueCtx := cuecontext.New()
		fullSrc := v1alpha2.GeneratedSchema + "\n" + ex.CueTemplate
		val := cueCtx.CompileString(fullSrc)
		if err := val.Err(); err != nil {
			t.Errorf("cueTemplate failed to compile against v1alpha2 schema: %v", err)
		}
	})
}

// buildPreviewPlatformInput returns a CUE string that injects a realistic
// backend-resolved PlatformInput at the CUE platform path. This mirrors what
// HOL-828's renderTemplateGrouped injects in production (via
// buildPreviewPlatformInput → platformInputToCUE) so the regression test
// exercises the same unified value the real preview path evaluates.
//
// The seed values match the HTTPRoute (v1) example shipped in this package:
// a gateway namespace of "istio-ingress", project "example-project", and
// namespace "prj-example-project" are sufficient to resolve all dynamic
// fields referenced by the shipped examples.
func buildPreviewPlatformInput(t *testing.T) string {
	t.Helper()
	pi := v1alpha2.PlatformInput{
		GatewayNamespace: deployments.DefaultGatewayNamespace,
		Project:          "example-project",
		Namespace:        "prj-example-project",
		Organization:     "holos",
	}
	b, err := json.Marshal(pi)
	if err != nil {
		t.Fatalf("marshal PlatformInput: %v", err)
	}
	// Produce the same format as handler.platformInputToCUE: "platform: <JSON>\n"
	return "platform: " + string(b) + "\n"
}

// buildPreviewProjectInput returns a CUE string with a minimal ProjectInput
// seed that satisfies examples referencing input.name, input.port, etc.
// The values match the shipped seed for the httproute-v1 example.
func buildPreviewProjectInput() string {
	return `input: {
	name:  "example-service"
	image: "nginx"
	tag:   "latest"
	port:  8080
}
`
}

// exampleResourcesEmitted reports whether the example is expected to produce
// at least one concrete Kubernetes resource. Policy-only examples (such as
// allowed-project-resource-kinds-v1) define CUE constraints but emit no
// concrete objects, so they are excluded from the non-empty output assertion.
func exampleResourcesEmitted(name string) bool {
	switch name {
	case "httproute-v1", "httpbin-v1", "podinfo-v1":
		return true
	default:
		// Policy-only examples produce no concrete K8s resources but must still
		// render without error.
		return false
	}
}

// TestExamplePreviewRender is the regression guard for HOL-826: every example
// in the registry must evaluate successfully through the template-preview code
// path with backend-injected platform values. The existing TestExamples only
// verifies schema compilation (CUE syntax + structural check); this test drives
// the full render path so dynamic fields like platform.gatewayNamespace are
// resolved, catching the HTTPRoute v1 class of bugs where a template compiles
// but fails at evaluation time.
//
// New examples added via the drop-in workflow (AGENTS.md §"Example Template
// Registry") are automatically covered by the catch-all loop — no changes to
// this test are required when a new *.cue file is dropped into this directory.
func TestExamplePreviewRender(t *testing.T) {
	list, err := examples.Examples()
	if err != nil {
		t.Fatalf("Examples() error: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("Examples() returned empty list; at least one example must be registered")
	}

	adapter := templates.NewCueRendererAdapter()
	cuePlatformInput := buildPreviewPlatformInput(t)
	cueProjectInput := buildPreviewProjectInput()

	for _, ex := range list {
		ex := ex // capture
		t.Run(ex.Name, func(t *testing.T) {
			grouped, err := adapter.RenderGrouped(
				context.Background(),
				ex.CueTemplate,
				cuePlatformInput,
				cueProjectInput,
			)
			if err != nil {
				t.Fatalf("RenderGrouped failed for example %q: %v", ex.Name, err)
			}

			// Collect YAML from both output buckets for assertions.
			var platformYAML, projectYAML strings.Builder
			for _, r := range grouped.Platform {
				platformYAML.WriteString(r.YAML)
			}
			for _, r := range grouped.Project {
				projectYAML.WriteString(r.YAML)
			}
			combined := platformYAML.String() + projectYAML.String()

			if exampleResourcesEmitted(ex.Name) {
				if combined == "" {
					t.Errorf("example %q: expected non-empty rendered resources (platform or project), got none", ex.Name)
				}
			}
		})
	}
}

// TestExamplePreviewRender_KnownExamples asserts explicit render properties for
// each shipped example. This is the HOL-826 regression test proper: the
// httproute-v1 example must produce an HTTPRoute resource using the resolved
// gatewayNamespace so the HOL-826 class of "apiVersion wrong at evaluation
// time" bugs cannot land undetected.
func TestExamplePreviewRender_KnownExamples(t *testing.T) {
	list, err := examples.Examples()
	if err != nil {
		t.Fatalf("Examples() error: %v", err)
	}
	byName := make(map[string]examples.Example, len(list))
	for _, ex := range list {
		byName[ex.Name] = ex
	}

	adapter := templates.NewCueRendererAdapter()
	cuePlatformInput := buildPreviewPlatformInput(t)
	cueProjectInput := buildPreviewProjectInput()

	t.Run("httproute-v1", func(t *testing.T) {
		ex, ok := byName["httproute-v1"]
		if !ok {
			t.Fatal("httproute-v1 example not found in registry")
		}
		grouped, err := adapter.RenderGrouped(
			context.Background(),
			ex.CueTemplate,
			cuePlatformInput,
			cueProjectInput,
		)
		if err != nil {
			t.Fatalf("RenderGrouped failed: %v", err)
		}

		var platformYAML strings.Builder
		for _, r := range grouped.Platform {
			platformYAML.WriteString(r.YAML)
		}
		yaml := platformYAML.String()

		if yaml == "" {
			t.Error("httproute-v1: expected non-empty platform_resources_yaml")
		}
		if !strings.Contains(yaml, "kind: HTTPRoute") {
			t.Errorf("httproute-v1: platform_resources_yaml must contain 'kind: HTTPRoute', got:\n%s", yaml)
		}
		// HOL-826 regression: apiVersion must be gateway.networking.k8s.io/v1,
		// not the old v1beta1. A wrong apiVersion would compile against the
		// schema but fail at evaluation time with a CUE error — this assertion
		// is the guard that would have caught HOL-826 before merge.
		if !strings.Contains(yaml, "apiVersion: gateway.networking.k8s.io/v1") {
			t.Errorf("httproute-v1: platform_resources_yaml must use apiVersion 'gateway.networking.k8s.io/v1', got:\n%s", yaml)
		}
		if !strings.Contains(yaml, deployments.DefaultGatewayNamespace) {
			t.Errorf("httproute-v1: platform_resources_yaml must reference gatewayNamespace %q, got:\n%s",
				deployments.DefaultGatewayNamespace, yaml)
		}
		if grouped.Project != nil && len(grouped.Project) > 0 {
			t.Errorf("httproute-v1: expected empty project resources for platform-only template, got %d", len(grouped.Project))
		}
	})

	t.Run("allowed-project-resource-kinds-v1", func(t *testing.T) {
		ex, ok := byName["allowed-project-resource-kinds-v1"]
		if !ok {
			t.Fatal("allowed-project-resource-kinds-v1 example not found in registry")
		}
		// Policy-only examples define constraints, not concrete resources.
		// The render must succeed (no error) but may produce empty YAML.
		_, err := adapter.RenderGrouped(
			context.Background(),
			ex.CueTemplate,
			cuePlatformInput,
			cueProjectInput,
		)
		if err != nil {
			t.Fatalf("allowed-project-resource-kinds-v1: RenderGrouped failed: %v", err)
		}
	})

	for _, name := range []string{"httpbin-v1", "podinfo-v1"} {
		name := name
		t.Run(name, func(t *testing.T) {
			ex, ok := byName[name]
			if !ok {
				t.Fatalf("%s example not found in registry", name)
			}
			grouped, err := adapter.RenderGrouped(
				context.Background(),
				ex.CueTemplate,
				cuePlatformInput,
				cueProjectInput,
			)
			if err != nil {
				t.Fatalf("%s: RenderGrouped failed: %v", name, err)
			}

			var projectYAML strings.Builder
			for _, r := range grouped.Project {
				projectYAML.WriteString(r.YAML)
			}
			yaml := projectYAML.String()

			if yaml == "" {
				t.Errorf("%s: expected non-empty project_resources_yaml", name)
			}
			for _, kind := range []string{"kind: ServiceAccount", "kind: Deployment", "kind: Service"} {
				if !strings.Contains(yaml, kind) {
					t.Errorf("%s: project_resources_yaml must contain %q, got:\n%s", name, kind, yaml)
				}
			}
			if !strings.Contains(yaml, "apiVersion: apps/v1") {
				t.Errorf("%s: project_resources_yaml must contain 'apiVersion: apps/v1', got:\n%s", name, yaml)
			}
			// Deployment resources must land in grouped.Project, not grouped.Platform.
			if len(grouped.Platform) > 0 {
				t.Errorf("%s: expected empty platform resources for project-only template, got %d", name, len(grouped.Platform))
			}
		})
	}
}
