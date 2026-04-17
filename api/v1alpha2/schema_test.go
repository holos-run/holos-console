package v1alpha2

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

// TestAPIVersionConstant verifies that APIVersion is the expected v1alpha2 string.
func TestAPIVersionConstant(t *testing.T) {
	if APIVersion != "console.holos.run/v1alpha2" {
		t.Errorf("APIVersion = %q, want %q", APIVersion, "console.holos.run/v1alpha2")
	}
}

// TestPlatformInputWithFoldersCUEValidation validates a PlatformInput containing
// a non-empty Folders slice against the generated #PlatformInput CUE definition.
func TestPlatformInputWithFoldersCUEValidation(t *testing.T) {
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
		Folders: []FolderInfo{
			{Name: "payments", Namespace: "holos-fld-a4b9c1-payments"},
			{Name: "eu", Namespace: "holos-fld-c3d4e5-eu"},
		},
	}

	validateAgainstSchema(t, pi, "#PlatformInput")
}

// TestPlatformInputNoFoldersCUEValidation validates a PlatformInput without any
// folders (project is a direct child of the org).
func TestPlatformInputNoFoldersCUEValidation(t *testing.T) {
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
		},
	}

	validateAgainstSchema(t, pi, "#PlatformInput")
}

// TestProjectInputCUEValidation validates a ProjectInput against the schema.
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

// TestFolderInfoCUEValidation validates a FolderInfo against the schema.
func TestFolderInfoCUEValidation(t *testing.T) {
	fi := FolderInfo{
		Name:      "payments",
		Namespace: "holos-fld-a4b9c1-payments",
	}

	validateAgainstSchema(t, fi, "#FolderInfo")
}

// TestFolderSpecCUEValidation validates a FolderSpec against the schema.
func TestFolderSpecCUEValidation(t *testing.T) {
	// Nested folder (has parent)
	fs := FolderSpec{
		DisplayName:  "EU Payments",
		Organization: "acme",
		Parent:       "payments",
	}
	validateAgainstSchema(t, fs, "#FolderSpec")

	// Top-level folder (no parent slug)
	fsTop := FolderSpec{
		DisplayName:  "Payments",
		Organization: "acme",
	}
	validateAgainstSchema(t, fsTop, "#FolderSpec")
}

// TestOutputCUEValidation validates table-driven Output snippets against the
// generated #Output definition and asserts that the url round-trips through a
// CUE compile. The Output schema gives platform templates a contractual place
// to publish values (initially url) intended for the UI.
func TestOutputCUEValidation(t *testing.T) {
	tests := []struct {
		name    string
		snippet string
		wantURL string
	}{
		{
			name:    "url set",
			snippet: `output: {url: "https://example.com"}`,
			wantURL: "https://example.com",
		},
		{
			name:    "url empty",
			snippet: `output: {url: ""}`,
			wantURL: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := cuecontext.New()

			schema := ctx.CompileString(GeneratedSchema)
			if err := schema.Err(); err != nil {
				t.Fatalf("schema compile: %v", err)
			}

			outputDef := schema.LookupPath(cue.ParsePath("#Output"))
			if !outputDef.Exists() {
				t.Fatal("definition #Output not found in GeneratedSchema")
			}

			snippetVal := ctx.CompileString(tc.snippet)
			if err := snippetVal.Err(); err != nil {
				t.Fatalf("snippet compile: %v", err)
			}

			got := snippetVal.LookupPath(cue.ParsePath("output"))
			if !got.Exists() {
				t.Fatal("output path not present in compiled snippet")
			}

			unified := outputDef.Unify(got)
			if err := unified.Validate(cue.Concrete(true)); err != nil {
				t.Fatalf("validation against #Output failed: %v", err)
			}

			urlVal := unified.LookupPath(cue.ParsePath("url"))
			gotURL, err := urlVal.String()
			if err != nil {
				t.Fatalf("url not a concrete string: %v", err)
			}
			if gotURL != tc.wantURL {
				t.Errorf("url = %q, want %q", gotURL, tc.wantURL)
			}
		})
	}
}

// TestResourceSetSpecOutputCUEValidation validates that a ResourceSetSpec
// carrying an Output struct round-trips through the schema. It mirrors the
// pattern used by the Defaults field: marshal the Go value to JSON, unify
// against #ResourceSetSpec, and assert no validation error.
func TestResourceSetSpecOutputCUEValidation(t *testing.T) {
	spec := ResourceSetSpec{
		Output: &Output{Url: "https://example.com"},
		PlatformInput: PlatformInput{
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
			},
		},
		ProjectInput: ProjectInput{
			Name:  "my-app",
			Image: "ghcr.io/example/app",
			Tag:   "v1.2.3",
			Port:  8080,
		},
	}

	validateAgainstSchema(t, spec, "#ResourceSetSpec")
}

// TestInvalidProjectInputCUEValidation verifies that invalid JSON is rejected.
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
