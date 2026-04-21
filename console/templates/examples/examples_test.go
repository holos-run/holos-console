package examples_test

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/templates/examples"
)

// TestExamples verifies the example registry loads both built-in examples and
// that each example satisfies the structural and compilation requirements.
func TestExamples(t *testing.T) {
	list, err := examples.Examples()
	if err != nil {
		t.Fatalf("Examples() error: %v", err)
	}

	// There must be exactly two examples.
	if got, want := len(list), 2; got != want {
		t.Fatalf("Examples() returned %d examples, want %d", got, want)
	}

	// Index by name for deterministic lookup.
	byName := make(map[string]examples.Example, len(list))
	for _, ex := range list {
		byName[ex.Name] = ex
	}

	wantNames := []string{"httproute-v1", "allowed-project-resource-kinds-v1"}
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
