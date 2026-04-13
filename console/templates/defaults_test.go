package templates

import (
	"testing"
)

func TestExtractDefaults(t *testing.T) {
	t.Run("valid CUE with defaults block returns populated DeploymentDefaults", func(t *testing.T) {
		cueSource := `
defaults: {
    name:        "httpbin"
    image:       "ghcr.io/mccutchen/go-httpbin"
    tag:         "2.21.0"
    description: "A simple HTTP Request & Response Service"
    port:        8080
    command: []
    args:    []
}
`
		d, err := ExtractDefaults(cueSource)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if d == nil {
			t.Fatal("expected non-nil DeploymentDefaults")
		}
		if d.Name != "httpbin" {
			t.Errorf("expected name 'httpbin', got %q", d.Name)
		}
		if d.Image != "ghcr.io/mccutchen/go-httpbin" {
			t.Errorf("expected image 'ghcr.io/mccutchen/go-httpbin', got %q", d.Image)
		}
		if d.Tag != "2.21.0" {
			t.Errorf("expected tag '2.21.0', got %q", d.Tag)
		}
		if d.Description != "A simple HTTP Request & Response Service" {
			t.Errorf("expected description 'A simple HTTP Request & Response Service', got %q", d.Description)
		}
		if d.Port != 8080 {
			t.Errorf("expected port 8080, got %d", d.Port)
		}
	})

	t.Run("CUE without defaults field returns nil without error", func(t *testing.T) {
		cueSource := `
input: {
    name:  "my-app"
    image: "example/app"
    tag:   "latest"
    port:  8080
}
`
		d, err := ExtractDefaults(cueSource)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if d != nil {
			t.Errorf("expected nil DeploymentDefaults for CUE without defaults block, got %+v", d)
		}
	})

	t.Run("CUE with partial defaults returns partial DeploymentDefaults", func(t *testing.T) {
		cueSource := `
defaults: {
    name:  "myapp"
    image: "example/myapp"
    tag:   "v1.0"
    port:  3000
}
`
		d, err := ExtractDefaults(cueSource)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if d == nil {
			t.Fatal("expected non-nil DeploymentDefaults")
		}
		if d.Name != "myapp" {
			t.Errorf("expected name 'myapp', got %q", d.Name)
		}
		if d.Image != "example/myapp" {
			t.Errorf("expected image 'example/myapp', got %q", d.Image)
		}
		if d.Tag != "v1.0" {
			t.Errorf("expected tag 'v1.0', got %q", d.Tag)
		}
		if d.Port != 3000 {
			t.Errorf("expected port 3000, got %d", d.Port)
		}
		if d.Description != "" {
			t.Errorf("expected empty description, got %q", d.Description)
		}
	})

	t.Run("invalid CUE source returns error", func(t *testing.T) {
		cueSource := `this is not valid CUE <<<`
		_, err := ExtractDefaults(cueSource)
		if err == nil {
			t.Fatal("expected error for invalid CUE, got nil")
		}
	})

	t.Run("default_template.cue extracts go-httpbin defaults", func(t *testing.T) {
		d, err := ExtractDefaults(DefaultTemplate)
		if err != nil {
			t.Fatalf("expected no error extracting defaults from DefaultTemplate, got %v", err)
		}
		if d == nil {
			t.Fatal("expected non-nil DeploymentDefaults from DefaultTemplate")
		}
		if d.Name != "httpbin" {
			t.Errorf("expected name 'httpbin', got %q", d.Name)
		}
		if d.Image != "ghcr.io/mccutchen/go-httpbin" {
			t.Errorf("expected image 'ghcr.io/mccutchen/go-httpbin', got %q", d.Image)
		}
		if d.Tag != "2.21.0" {
			t.Errorf("expected tag '2.21.0', got %q", d.Tag)
		}
		if d.Description != "A simple HTTP Request & Response Service" {
			t.Errorf("expected description 'A simple HTTP Request & Response Service', got %q", d.Description)
		}
		if d.Port != 8080 {
			t.Errorf("expected port 8080, got %d", d.Port)
		}
	})

	t.Run("defaults with typed schema using #ProjectInput", func(t *testing.T) {
		// Test that templates using the generated schema type work correctly.
		cueSource := `
defaults: #ProjectInput & {
    name:        "typed-app"
    image:       "example/typed"
    tag:         "v2.0"
    description: "A typed app"
    port:        9090
}
`
		d, err := ExtractDefaults(cueSource)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if d == nil {
			t.Fatal("expected non-nil DeploymentDefaults")
		}
		if d.Name != "typed-app" {
			t.Errorf("expected name 'typed-app', got %q", d.Name)
		}
		if d.Port != 9090 {
			t.Errorf("expected port 9090, got %d", d.Port)
		}
	})

	t.Run("mixed concrete and non-concrete fields returns partial defaults", func(t *testing.T) {
		// When some fields are concrete and others are non-concrete (e.g. bare
		// string type), only the concrete fields should be extracted. The
		// non-concrete fields should be silently omitted.
		cueSource := `
defaults: #ProjectInput & {
    name:  "httpbin"
    image: string  // non-concrete — constrained but no value
    tag:   "2.21.0"
    port:  8080
}
`
		d, err := ExtractDefaults(cueSource)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if d == nil {
			t.Fatal("expected non-nil DeploymentDefaults for mixed concrete/non-concrete")
		}
		if d.Name != "httpbin" {
			t.Errorf("expected name 'httpbin', got %q", d.Name)
		}
		if d.Image != "" {
			t.Errorf("expected empty image (non-concrete), got %q", d.Image)
		}
		if d.Tag != "2.21.0" {
			t.Errorf("expected tag '2.21.0', got %q", d.Tag)
		}
		if d.Port != 8080 {
			t.Errorf("expected port 8080, got %d", d.Port)
		}
	})

	t.Run("all non-concrete fields returns nil", func(t *testing.T) {
		// When every field in the defaults block is non-concrete (bare type
		// constraints with no values), ExtractDefaults should return nil because
		// there are no meaningful defaults to pre-fill.
		cueSource := `
defaults: {
    name:  string
    image: string
    tag:   string
    port:  int
}
`
		d, err := ExtractDefaults(cueSource)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if d != nil {
			t.Errorf("expected nil for all non-concrete defaults, got %+v", d)
		}
	})

	t.Run("example_httpbin.cue extracts concrete defaults (ADR 027 regression)", func(t *testing.T) {
		// Regression guard: the shipped example_httpbin.cue must carry a
		// top-level `defaults` block whose fields ExtractDefaults can surface
		// intact. Issue #925 documented the prior state where the example
		// expressed defaults only via inline `*value | _` markers on `input`,
		// which produced zero extractable defaults at the RPC layer. This
		// assertion keeps the example aligned with ADR 027 section 7.
		d, err := ExtractDefaults(ExampleHttpbinTemplate)
		if err != nil {
			t.Fatalf("expected no error extracting defaults from ExampleHttpbinTemplate, got %v", err)
		}
		if d == nil {
			t.Fatal("expected non-nil TemplateDefaults from ExampleHttpbinTemplate — ADR 027 requires example templates to expose a defaults block")
		}
		if d.Name != "httpbin" {
			t.Errorf("expected name 'httpbin', got %q", d.Name)
		}
		if d.Image != "ghcr.io/mccutchen/go-httpbin" {
			t.Errorf("expected image 'ghcr.io/mccutchen/go-httpbin', got %q", d.Image)
		}
		if d.Tag != "2.21.0" {
			t.Errorf("expected tag '2.21.0', got %q", d.Tag)
		}
		if d.Port != 8080 {
			t.Errorf("expected port 8080, got %d", d.Port)
		}
		if d.Description != "A simple HTTP Request & Response Service" {
			t.Errorf("expected description 'A simple HTTP Request & Response Service', got %q", d.Description)
		}
		// command and args are deliberately unset so the frontend keeps
		// exercising the empty-slice path.
		if len(d.Command) != 0 {
			t.Errorf("expected empty command, got %v", d.Command)
		}
		if len(d.Args) != 0 {
			t.Errorf("expected empty args, got %v", d.Args)
		}
	})

	t.Run("issue #925 before-fix example (inline-only defaults) extracts nothing", func(t *testing.T) {
		// The exact CUE shape issue #925 called out as broken: defaults
		// expressed only through inline `*value | _` markers on `input` and
		// no top-level `defaults` block. ExtractDefaults walks the `defaults`
		// path only, so it must return nil on this input.
		cueSource := `
input: #ProjectInput & {
    name:  =~"^[a-z][a-z0-9-]*$"
    image: string | *"ghcr.io/mccutchen/go-httpbin"
    tag:   string | *"2.21.0"
    port:  >0 & <=65535 | *8080
}
`
		d, err := ExtractDefaults(cueSource)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if d != nil {
			t.Fatalf("expected nil — inline `*` defaults on input must NOT be extracted (ADR 027); got %+v", d)
		}
	})

	t.Run("issue #925 after-fix example (top-level defaults block) extracts values", func(t *testing.T) {
		// The matching "after" shape from issue #925: the same template now
		// declares a top-level `defaults` block mirroring the inline values.
		// ExtractDefaults must surface those values verbatim.
		cueSource := `
defaults: #ProjectInput & {
    name:        "httpbin"
    image:       "ghcr.io/mccutchen/go-httpbin"
    tag:         "2.21.0"
    port:        8080
    description: "A simple HTTP Request & Response Service"
}
input: #ProjectInput & {
    name:  *defaults.name | (string & =~"^[a-z][a-z0-9-]*$")
    image: *defaults.image | _
    tag:   *defaults.tag | _
    port:  *defaults.port | (>0 & <=65535)
}
`
		d, err := ExtractDefaults(cueSource)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if d == nil {
			t.Fatal("expected non-nil TemplateDefaults")
		}
		if d.Name != "httpbin" || d.Image != "ghcr.io/mccutchen/go-httpbin" || d.Tag != "2.21.0" || d.Port != 8080 || d.Description != "A simple HTTP Request & Response Service" {
			t.Errorf("expected full httpbin defaults, got %+v", d)
		}
	})

	t.Run("typed defaults with one non-concrete field extracts concrete fields", func(t *testing.T) {
		// A template using #ProjectInput & { ... } with one non-concrete field
		// should still extract all the concrete fields.
		cueSource := `
defaults: #ProjectInput & {
    name:        "my-service"
    image:       "registry.example.com/my-service"
    tag:         string  // non-concrete — user must provide
    description: "My production service"
    port:        3000
}
`
		d, err := ExtractDefaults(cueSource)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if d == nil {
			t.Fatal("expected non-nil DeploymentDefaults")
		}
		if d.Name != "my-service" {
			t.Errorf("expected name 'my-service', got %q", d.Name)
		}
		if d.Image != "registry.example.com/my-service" {
			t.Errorf("expected image 'registry.example.com/my-service', got %q", d.Image)
		}
		if d.Tag != "" {
			t.Errorf("expected empty tag (non-concrete), got %q", d.Tag)
		}
		if d.Description != "My production service" {
			t.Errorf("expected description 'My production service', got %q", d.Description)
		}
		if d.Port != 3000 {
			t.Errorf("expected port 3000, got %d", d.Port)
		}
	})
}
