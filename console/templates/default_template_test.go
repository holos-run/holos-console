package templates

import (
	"context"
	"strings"
	"testing"

	"github.com/holos-run/holos-console/console/deployments"
)

// TestDefaultTemplate verifies that DefaultTemplate renders correctly through
// the full CUE render pipeline including managed-by label validation.
func TestDefaultTemplate(t *testing.T) {
	renderer := &deployments.CueRenderer{}
	namespace := "prj-my-project"
	input := deployments.DeploymentInput{
		Name:      "holos-console",
		Image:     "ghcr.io/holos-run/holos-console",
		Tag:       "latest",
		Project:   "my-project",
		Namespace: namespace,
	}

	resources, err := renderer.Render(context.Background(), DefaultTemplate, input)
	if err != nil {
		t.Fatalf("default template render failed: %v", err)
	}

	if len(resources) != 3 {
		t.Fatalf("expected 3 resources (ServiceAccount, Deployment, Service), got %d", len(resources))
	}

	kindSet := make(map[string]bool)
	for _, r := range resources {
		kindSet[r.GetKind()] = true

		// Every resource must have the managed-by label.
		labels := r.GetLabels()
		if labels["app.kubernetes.io/managed-by"] != "console.holos.run" {
			t.Errorf("resource %s/%s: missing required label app.kubernetes.io/managed-by=console.holos.run", r.GetKind(), r.GetName())
		}

		// Every resource must be in the expected namespace.
		if r.GetNamespace() != namespace {
			t.Errorf("resource %s/%s: expected namespace %q, got %q", r.GetKind(), r.GetName(), namespace, r.GetNamespace())
		}
	}

	for _, kind := range []string{"ServiceAccount", "Deployment", "Service"} {
		if !kindSet[kind] {
			t.Errorf("expected resource of kind %q", kind)
		}
	}

	// Verify the Deployment container image includes the expected image.
	for _, r := range resources {
		if r.GetKind() != "Deployment" {
			continue
		}
		containers, ok, _ := getNestedSlice(r.Object, "spec", "template", "spec", "containers")
		if !ok || len(containers) == 0 {
			t.Fatal("Deployment has no containers")
		}
		c, ok := containers[0].(map[string]any)
		if !ok {
			t.Fatal("container is not a map")
		}
		image, _ := c["image"].(string)
		if !strings.HasPrefix(image, "ghcr.io/holos-run/holos-console") {
			t.Errorf("expected container image to start with ghcr.io/holos-run/holos-console, got %q", image)
		}
	}
}

// TestDefaultTemplate_CommandArgs verifies that command and args are rendered
// into the container spec when provided.
func TestDefaultTemplate_CommandArgs(t *testing.T) {
	renderer := &deployments.CueRenderer{}
	namespace := "prj-my-project"

	t.Run("command and args appear in container spec", func(t *testing.T) {
		input := deployments.DeploymentInput{
			Name:      "holos-console",
			Image:     "ghcr.io/holos-run/holos-console",
			Tag:       "latest",
			Project:   "my-project",
			Namespace: namespace,
			Command:   []string{"myapp"},
			Args:      []string{"--port", "8080"},
		}

		resources, err := renderer.Render(context.Background(), DefaultTemplate, input)
		if err != nil {
			t.Fatalf("default template render failed: %v", err)
		}

		for _, r := range resources {
			if r.GetKind() != "Deployment" {
				continue
			}
			containers, ok, _ := getNestedSlice(r.Object, "spec", "template", "spec", "containers")
			if !ok || len(containers) == 0 {
				t.Fatal("Deployment has no containers")
			}
			c, ok := containers[0].(map[string]any)
			if !ok {
				t.Fatal("container is not a map")
			}
			cmd, _ := c["command"].([]any)
			if len(cmd) != 1 || cmd[0] != "myapp" {
				t.Errorf("expected command [myapp], got %v", cmd)
			}
			args, _ := c["args"].([]any)
			if len(args) != 2 || args[0] != "--port" || args[1] != "8080" {
				t.Errorf("expected args [--port 8080], got %v", args)
			}
			return
		}
		t.Fatal("no Deployment resource found")
	})

	t.Run("command and args absent when not provided", func(t *testing.T) {
		input := deployments.DeploymentInput{
			Name:      "holos-console",
			Image:     "ghcr.io/holos-run/holos-console",
			Tag:       "latest",
			Project:   "my-project",
			Namespace: namespace,
		}

		resources, err := renderer.Render(context.Background(), DefaultTemplate, input)
		if err != nil {
			t.Fatalf("default template render failed: %v", err)
		}

		for _, r := range resources {
			if r.GetKind() != "Deployment" {
				continue
			}
			containers, ok, _ := getNestedSlice(r.Object, "spec", "template", "spec", "containers")
			if !ok || len(containers) == 0 {
				t.Fatal("Deployment has no containers")
			}
			c, ok := containers[0].(map[string]any)
			if !ok {
				t.Fatal("container is not a map")
			}
			if _, hasCmd := c["command"]; hasCmd {
				t.Error("expected command to be absent when not provided")
			}
			if _, hasArgs := c["args"]; hasArgs {
				t.Error("expected args to be absent when not provided")
			}
			return
		}
		t.Fatal("no Deployment resource found")
	})
}

// getNestedSlice is a helper to avoid importing k8s.io/apimachinery/pkg/apis/meta/v1/unstructured
// in a test that lives in the templates package.
func getNestedSlice(obj map[string]any, fields ...string) ([]any, bool, error) {
	cur := obj
	for i, field := range fields {
		if i == len(fields)-1 {
			v, ok := cur[field]
			if !ok {
				return nil, false, nil
			}
			s, ok := v.([]any)
			return s, ok, nil
		}
		next, ok := cur[field].(map[string]any)
		if !ok {
			return nil, false, nil
		}
		cur = next
	}
	return nil, false, nil
}
