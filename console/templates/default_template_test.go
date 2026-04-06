package templates

import (
	"context"
	"strings"
	"testing"

	"github.com/holos-run/holos-console/console/deployments"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// defaultSystemInput returns a SystemInput with all required fields populated,
// including claims, for use in default template tests.
func defaultSystemInput(namespace string) deployments.SystemInput {
	return deployments.SystemInput{
		Project:   "my-project",
		Namespace: namespace,
		Claims: deployments.ClaimsInput{
			Iss:           "https://dex.example.com",
			Sub:           "test-user-sub",
			Exp:           9999999999,
			Iat:           1700000000,
			Email:         "deployer@example.com",
			EmailVerified: true,
		},
	}
}

// TestDefaultTemplate verifies that DefaultTemplate renders correctly through
// the full CUE render pipeline including managed-by label validation.
func TestDefaultTemplate(t *testing.T) {
	renderer := &deployments.CueRenderer{}
	namespace := "prj-my-project"
	system := defaultSystemInput(namespace)
	user := deployments.UserInput{
		Name:  "holos-console",
		Image: "ghcr.io/holos-run/holos-console",
		Tag:   "latest",
	}

	resources, err := renderer.Render(context.Background(), DefaultTemplate, system, user)
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
	system := defaultSystemInput(namespace)

	t.Run("command and args appear in container spec", func(t *testing.T) {
		user := deployments.UserInput{
			Name:    "holos-console",
			Image:   "ghcr.io/holos-run/holos-console",
			Tag:     "latest",
			Command: []string{"myapp"},
			Args:    []string{"--port", "8080"},
		}

		resources, err := renderer.Render(context.Background(), DefaultTemplate, system, user)
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
		user := deployments.UserInput{
			Name:  "holos-console",
			Image: "ghcr.io/holos-run/holos-console",
			Tag:   "latest",
		}

		resources, err := renderer.Render(context.Background(), DefaultTemplate, system, user)
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

// TestDefaultTemplate_EnvVars verifies that environment variables are rendered
// into the container spec correctly for all source types.
func TestDefaultTemplate_EnvVars(t *testing.T) {
	renderer := &deployments.CueRenderer{}
	namespace := "prj-my-project"
	system := defaultSystemInput(namespace)

	t.Run("no env vars renders without env field", func(t *testing.T) {
		user := deployments.UserInput{
			Name:  "holos-console",
			Image: "ghcr.io/holos-run/holos-console",
			Tag:   "latest",
		}
		resources, err := renderer.Render(context.Background(), DefaultTemplate, system, user)
		if err != nil {
			t.Fatalf("render failed: %v", err)
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
			if _, hasEnv := c["env"]; hasEnv {
				t.Error("expected env to be absent when no env vars provided")
			}
			return
		}
		t.Fatal("no Deployment resource found")
	})

	t.Run("literal env var renders as value", func(t *testing.T) {
		user := deployments.UserInput{
			Name:  "holos-console",
			Image: "ghcr.io/holos-run/holos-console",
			Tag:   "latest",
			Env:   []deployments.EnvVarInput{{Name: "FOO", Value: "bar"}},
		}
		resources, err := renderer.Render(context.Background(), DefaultTemplate, system, user)
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}
		container := mustGetContainer(t, resources)
		env, _ := container["env"].([]any)
		if len(env) != 1 {
			t.Fatalf("expected 1 env var, got %d", len(env))
		}
		e, _ := env[0].(map[string]any)
		if e["name"] != "FOO" {
			t.Errorf("expected name=FOO, got %v", e["name"])
		}
		if e["value"] != "bar" {
			t.Errorf("expected value=bar, got %v", e["value"])
		}
		if _, hasValueFrom := e["valueFrom"]; hasValueFrom {
			t.Error("expected no valueFrom for literal env var")
		}
	})

	t.Run("secret ref renders as valueFrom.secretKeyRef", func(t *testing.T) {
		user := deployments.UserInput{
			Name:  "holos-console",
			Image: "ghcr.io/holos-run/holos-console",
			Tag:   "latest",
			Env:   []deployments.EnvVarInput{{Name: "DB_PASS", SecretKeyRef: &deployments.KeyRefInput{Name: "my-secret", Key: "password"}}},
		}
		resources, err := renderer.Render(context.Background(), DefaultTemplate, system, user)
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}
		container := mustGetContainer(t, resources)
		env, _ := container["env"].([]any)
		if len(env) != 1 {
			t.Fatalf("expected 1 env var, got %d", len(env))
		}
		e, _ := env[0].(map[string]any)
		if e["name"] != "DB_PASS" {
			t.Errorf("expected name=DB_PASS, got %v", e["name"])
		}
		if _, hasValue := e["value"]; hasValue {
			t.Error("expected no value for secret ref env var")
		}
		valueFrom, _ := e["valueFrom"].(map[string]any)
		secretKeyRef, _ := valueFrom["secretKeyRef"].(map[string]any)
		if secretKeyRef["name"] != "my-secret" {
			t.Errorf("expected secretKeyRef.name=my-secret, got %v", secretKeyRef["name"])
		}
		if secretKeyRef["key"] != "password" {
			t.Errorf("expected secretKeyRef.key=password, got %v", secretKeyRef["key"])
		}
	})

	t.Run("configmap ref renders as valueFrom.configMapKeyRef", func(t *testing.T) {
		user := deployments.UserInput{
			Name:  "holos-console",
			Image: "ghcr.io/holos-run/holos-console",
			Tag:   "latest",
			Env:   []deployments.EnvVarInput{{Name: "APP_MODE", ConfigMapKeyRef: &deployments.KeyRefInput{Name: "my-config", Key: "mode"}}},
		}
		resources, err := renderer.Render(context.Background(), DefaultTemplate, system, user)
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}
		container := mustGetContainer(t, resources)
		env, _ := container["env"].([]any)
		if len(env) != 1 {
			t.Fatalf("expected 1 env var, got %d", len(env))
		}
		e, _ := env[0].(map[string]any)
		if e["name"] != "APP_MODE" {
			t.Errorf("expected name=APP_MODE, got %v", e["name"])
		}
		if _, hasValue := e["value"]; hasValue {
			t.Error("expected no value for configmap ref env var")
		}
		valueFrom, _ := e["valueFrom"].(map[string]any)
		configMapKeyRef, _ := valueFrom["configMapKeyRef"].(map[string]any)
		if configMapKeyRef["name"] != "my-config" {
			t.Errorf("expected configMapKeyRef.name=my-config, got %v", configMapKeyRef["name"])
		}
		if configMapKeyRef["key"] != "mode" {
			t.Errorf("expected configMapKeyRef.key=mode, got %v", configMapKeyRef["key"])
		}
	})

	t.Run("mixed env var types render correctly", func(t *testing.T) {
		user := deployments.UserInput{
			Name:  "holos-console",
			Image: "ghcr.io/holos-run/holos-console",
			Tag:   "latest",
			Env: []deployments.EnvVarInput{
				{Name: "FOO", Value: "bar"},
				{Name: "DB_PASS", SecretKeyRef: &deployments.KeyRefInput{Name: "my-secret", Key: "password"}},
				{Name: "APP_MODE", ConfigMapKeyRef: &deployments.KeyRefInput{Name: "my-config", Key: "mode"}},
			},
		}
		resources, err := renderer.Render(context.Background(), DefaultTemplate, system, user)
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}
		container := mustGetContainer(t, resources)
		env, _ := container["env"].([]any)
		if len(env) != 3 {
			t.Fatalf("expected 3 env vars, got %d", len(env))
		}

		// Verify order is preserved.
		names := make([]string, len(env))
		for i, ev := range env {
			e, _ := ev.(map[string]any)
			names[i], _ = e["name"].(string)
		}
		if names[0] != "FOO" || names[1] != "DB_PASS" || names[2] != "APP_MODE" {
			t.Errorf("unexpected env var order: %v", names)
		}
	})
}

// TestDefaultTemplate_StructuredOutput verifies the default template uses the
// output.namespacedResources/output.clusterResources structured output format.
func TestDefaultTemplate_StructuredOutput(t *testing.T) {
	renderer := &deployments.CueRenderer{}
	namespace := "prj-my-project"
	system := defaultSystemInput(namespace)
	user := deployments.UserInput{
		Name:  "holos-console",
		Image: "ghcr.io/holos-run/holos-console",
		Tag:   "latest",
	}

	resources, err := renderer.Render(context.Background(), DefaultTemplate, system, user)
	if err != nil {
		t.Fatalf("default template render failed: %v", err)
	}

	// Default template produces 3 namespaced resources: ServiceAccount, Deployment, Service.
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

		// Every resource must have the expected name.
		if r.GetName() != user.Name {
			t.Errorf("resource %s: expected name %q, got %q", r.GetKind(), user.Name, r.GetName())
		}
	}

	for _, kind := range []string{"ServiceAccount", "Deployment", "Service"} {
		if !kindSet[kind] {
			t.Errorf("expected resource of kind %q", kind)
		}
	}
}

// TestDefaultTemplate_DeployerEmailAnnotation verifies that the deployer-email
// annotation is set on all resources from system.claims.email.
func TestDefaultTemplate_DeployerEmailAnnotation(t *testing.T) {
	renderer := &deployments.CueRenderer{}
	namespace := "prj-my-project"
	const deployerEmail = "deployer@example.com"
	system := deployments.SystemInput{
		Project:   "my-project",
		Namespace: namespace,
		Claims: deployments.ClaimsInput{
			Iss:           "https://dex.example.com",
			Sub:           "test-user-sub",
			Exp:           9999999999,
			Iat:           1700000000,
			Email:         deployerEmail,
			EmailVerified: true,
		},
	}
	user := deployments.UserInput{
		Name:  "holos-console",
		Image: "ghcr.io/holos-run/holos-console",
		Tag:   "latest",
	}

	resources, err := renderer.Render(context.Background(), DefaultTemplate, system, user)
	if err != nil {
		t.Fatalf("default template render failed: %v", err)
	}
	if len(resources) == 0 {
		t.Fatal("expected at least one resource")
	}

	for _, r := range resources {
		annotations := r.GetAnnotations()
		got := annotations["console.holos.run/deployer-email"]
		if got != deployerEmail {
			t.Errorf("resource %s/%s: expected annotation console.holos.run/deployer-email=%q, got %q",
				r.GetKind(), r.GetName(), deployerEmail, got)
		}
	}
}

// TestDefaultTemplate_ClaimsEmailInAnnotation verifies that the deployer-email
// annotation carries the email from system.claims, verifying claims propagation.
func TestDefaultTemplate_ClaimsEmailInAnnotation(t *testing.T) {
	renderer := &deployments.CueRenderer{}
	namespace := "prj-my-project"

	emails := []string{"alice@example.com", "bob@corp.io", "svc-account@domain.org"}
	for _, email := range emails {
		t.Run(email, func(t *testing.T) {
			system := deployments.SystemInput{
				Project:   "my-project",
				Namespace: namespace,
				Claims: deployments.ClaimsInput{
					Iss:           "https://dex.example.com",
					Sub:           "test-sub",
					Exp:           9999999999,
					Iat:           1700000000,
					Email:         email,
					EmailVerified: true,
				},
			}
			user := deployments.UserInput{
				Name:  "holos-console",
				Image: "ghcr.io/holos-run/holos-console",
				Tag:   "latest",
			}
			resources, err := renderer.Render(context.Background(), DefaultTemplate, system, user)
			if err != nil {
				t.Fatalf("render failed: %v", err)
			}
			for _, r := range resources {
				annotations := r.GetAnnotations()
				got := annotations["console.holos.run/deployer-email"]
				if got != email {
					t.Errorf("resource %s/%s: expected deployer-email=%q, got %q",
						r.GetKind(), r.GetName(), email, got)
				}
			}
		})
	}
}

// mustGetContainer finds the first container in the Deployment resource.
func mustGetContainer(t *testing.T, resources []unstructured.Unstructured) map[string]any {
	t.Helper()
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
		return c
	}
	t.Fatal("no Deployment resource found")
	return nil
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
