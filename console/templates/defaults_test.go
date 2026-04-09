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
}
