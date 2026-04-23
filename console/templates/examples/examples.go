// Package examples provides a registry of built-in CUE example templates that
// the UI can offer as drop-in starting points when creating a new template.
//
// Each example is a single CUE file that declares its own display metadata
// (displayName, name, description) and the template body (cueTemplate) as
// top-level fields. The outer CUE file is valid CUE; the template body is kept
// as a multi-line string so it can reference #PlatformInput, #ProjectInput,
// etc. without those types needing to be in scope in this file.
//
// Adding a new example requires dropping a new *.cue file in this directory
// and updating the test counts and name lists in examples_test.go and
// console/templates/handler_examples_test.go. See AGENTS.md for the full
// drop-in workflow.
package examples

import (
	"embed"
	"fmt"
	"io/fs"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

// Example holds the parsed metadata and template body for a single example.
type Example struct {
	// DisplayName is the human-readable name shown in the UI picker.
	DisplayName string
	// Name is the URL-safe slug identifier (e.g. "httproute-v1").
	Name string
	// Description is a short sentence describing what the example does.
	Description string
	// CueTemplate is the full CUE source the user will see in the editor.
	CueTemplate string
}

//go:embed *.cue
var examplesFS embed.FS

// Examples loads and returns all example templates embedded in this package.
// Each *.cue file in the directory is parsed and returned as an Example. The
// order of the returned list is deterministic (lexicographic file name order).
func Examples() ([]Example, error) {
	cueCtx := cuecontext.New()

	entries, err := fs.ReadDir(examplesFS, ".")
	if err != nil {
		return nil, fmt.Errorf("reading embedded examples directory: %w", err)
	}

	var list []Example
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		src, err := fs.ReadFile(examplesFS, name)
		if err != nil {
			return nil, fmt.Errorf("reading embedded example %s: %w", name, err)
		}

		ex, err := parseExample(cueCtx, name, string(src))
		if err != nil {
			return nil, fmt.Errorf("parsing example %s: %w", name, err)
		}
		list = append(list, ex)
	}

	return list, nil
}

// parseExample compiles a single CUE example file and extracts its metadata
// fields. The v1alpha2 generated schema is prepended so that the template body
// string field can mention schema types without breaking the outer file.
func parseExample(cueCtx *cue.Context, filename, src string) (Example, error) {
	// Prepend the generated schema exactly as the renderer does (see defaults.go).
	fullSrc := v1alpha2.GeneratedSchema + "\n" + src
	val := cueCtx.CompileString(fullSrc, cue.Filename(filename))
	if err := val.Err(); err != nil {
		return Example{}, fmt.Errorf("compiling CUE: %w", err)
	}

	ex := Example{}
	for _, f := range []struct {
		path string
		dest *string
	}{
		{"displayName", &ex.DisplayName},
		{"name", &ex.Name},
		{"description", &ex.Description},
		{"cueTemplate", &ex.CueTemplate},
	} {
		v := val.LookupPath(cue.ParsePath(f.path))
		if !v.Exists() {
			return Example{}, fmt.Errorf("missing required field %q", f.path)
		}
		if err := v.Err(); err != nil {
			return Example{}, fmt.Errorf("evaluating field %q: %w", f.path, err)
		}
		s, err := v.String()
		if err != nil {
			return Example{}, fmt.Errorf("reading field %q as string: %w", f.path, err)
		}
		*f.dest = s
	}

	return ex, nil
}
