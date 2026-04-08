package v1alpha1

import (
	"encoding/json"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

// TestGeneratedSchemaCompiles verifies that GeneratedSchema is valid CUE.
func TestGeneratedSchemaCompiles(t *testing.T) {
	if GeneratedSchema == "" {
		t.Fatal("GeneratedSchema is empty")
	}
	ctx := cuecontext.New()
	val := ctx.CompileString(GeneratedSchema)
	if err := val.Err(); err != nil {
		t.Fatalf("GeneratedSchema does not compile: %v", err)
	}
}

// TestPlatformInputCUEValidation marshals a PlatformInput to JSON and validates
// it against the generated #PlatformInput CUE definition.
func TestPlatformInputCUEValidation(t *testing.T) {
	pi := PlatformInput{
		Project:          "frontend",
		Namespace:        "holos-prj-frontend",
		GatewayNamespace: "istio-ingress",
		Organization:     "acme",
		Claims: Claims{
			Iss:           "https://dex.example.com",
			Sub:           "user-123",
			Exp:           1700000000,
			Iat:           1699990000,
			Email:         "alice@example.com",
			EmailVerified: true,
			Name:          "Alice",
			Groups:        []string{"engineering"},
		},
	}

	validateAgainstSchema(t, pi, "#PlatformInput")
}

// TestProjectInputCUEValidation marshals a ProjectInput to JSON and validates
// it against the generated #ProjectInput CUE definition.
func TestProjectInputCUEValidation(t *testing.T) {
	proj := ProjectInput{
		Name:    "my-app",
		Image:   "ghcr.io/example/app",
		Tag:     "v1.2.3",
		Command: []string{"/bin/app"},
		Args:    []string{"--port", "8080"},
		Env: []EnvVar{
			{Name: "DB_HOST", Value: "postgres.default.svc"},
			{Name: "DB_PASSWORD", SecretKeyRef: &KeyRef{Name: "db-creds", Key: "password"}},
		},
		Port: 8080,
	}

	validateAgainstSchema(t, proj, "#ProjectInput")
}

// TestProjectInputMinimalCUEValidation validates a minimal ProjectInput (no
// optional fields) against the generated schema.
func TestProjectInputMinimalCUEValidation(t *testing.T) {
	proj := ProjectInput{
		Name:  "simple",
		Image: "nginx",
		Tag:   "latest",
		Port:  80,
	}

	validateAgainstSchema(t, proj, "#ProjectInput")
}

// TestClaimsCUEValidation validates Claims against the generated schema.
func TestClaimsCUEValidation(t *testing.T) {
	c := Claims{
		Iss:           "https://dex.example.com",
		Sub:           "user-1",
		Exp:           1700000000,
		Iat:           1699990000,
		Email:         "test@example.com",
		EmailVerified: true,
	}

	validateAgainstSchema(t, c, "#Claims")
}

// TestInvalidProjectInputCUEValidation verifies that invalid JSON is rejected
// by the generated schema.
func TestInvalidProjectInputCUEValidation(t *testing.T) {
	ctx := cuecontext.New()

	schema := ctx.CompileString(GeneratedSchema)
	if err := schema.Err(); err != nil {
		t.Fatalf("schema compile: %v", err)
	}

	// port should be int, not string
	invalidJSON := `{"name": "test", "image": "nginx", "tag": "latest", "port": "not-a-number"}`
	jsonVal := ctx.CompileString(invalidJSON)
	if err := jsonVal.Err(); err != nil {
		t.Fatalf("JSON compile: %v", err)
	}

	def := schema.LookupPath(cue.ParsePath("#ProjectInput"))
	unified := def.Unify(jsonVal)
	if err := unified.Validate(); err == nil {
		t.Error("expected validation error for port as string, got nil")
	}
}

// validateAgainstSchema marshals v to JSON, compiles it as CUE, unifies with
// the named definition from GeneratedSchema, and asserts no error.
func validateAgainstSchema(t *testing.T, v any, defName string) {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	ctx := cuecontext.New()

	schema := ctx.CompileString(GeneratedSchema)
	if err := schema.Err(); err != nil {
		t.Fatalf("schema compile: %v", err)
	}

	jsonVal := ctx.CompileBytes(data)
	if err := jsonVal.Err(); err != nil {
		t.Fatalf("JSON compile: %v", err)
	}

	def := schema.LookupPath(cue.ParsePath(defName))
	if !def.Exists() {
		t.Fatalf("definition %s not found in GeneratedSchema", defName)
	}

	unified := def.Unify(jsonVal)
	if err := unified.Validate(); err != nil {
		t.Errorf("validation against %s failed: %v\nJSON: %s", defName, err, string(data))
	}
}
