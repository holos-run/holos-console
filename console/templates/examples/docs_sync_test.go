package examples_test

// docs_sync_test.go verifies that the demo CUE snippets hosted in
// holos-console-docs/demo/ remain valid against the v1alpha2 generated schema.
//
// # Design
//
// The registry (console/templates/examples/*.cue) provides generic drop-in
// starting points for contributors. The docs repo (holos-console-docs/demo/)
// provides fully-featured walkthrough templates tied to the CI demo
// environment. The two sets of files serve different roles and are NOT
// byte-for-byte identical — differing on details like hard-coded gateway
// namespaces, ReferenceGrant resources, and cluster-specific let bindings.
//
// The sync contract is therefore: BOTH must compile cleanly against the
// v1alpha2 schema. Schema drift in this repo (e.g. renaming a field in
// api/v1alpha2) must be detected here before the docs snippets silently break.
//
// # Maintenance
//
// The files under testdata/docs-snippets/ are pinned copies sourced from
// holos-run/holos-console-docs/demo/. When docs-side snippets change, run:
//
//	cp /path/to/holos-console-docs/demo/httpbin-v1/httpbin-v1.cue \
//	    console/templates/examples/testdata/docs-snippets/httpbin-v1/httpbin-v1.cue
//	cp /path/to/holos-console-docs/demo/allowed-resources/allowed-resources.cue \
//	    console/templates/examples/testdata/docs-snippets/allowed-resources/allowed-resources.cue
//
// Then run make test-go to confirm the updated snippets still compile.

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/cue/cuecontext"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

// TestDocsSyncSnippets verifies that the pinned demo CUE snippets from
// holos-console-docs/demo/ compile cleanly against the v1alpha2 generated
// schema. This catches schema renames or removals in this repo before they
// silently invalidate the publicly-hosted docs examples.
func TestDocsSyncSnippets(t *testing.T) {
	// testdata/docs-snippets/ mirrors the subset of holos-console-docs/demo/
	// that corresponds to registry template scenarios.
	root := filepath.Join("testdata", "docs-snippets")

	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Fatalf("testdata/docs-snippets/ directory missing — run the copy commands in the test file header to populate it")
	}

	// Walk all *.cue files under the testdata directory.
	var cueFiles []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".cue" {
			cueFiles = append(cueFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	if len(cueFiles) == 0 {
		t.Fatalf("no *.cue files found under %s — testdata appears empty", root)
	}

	cueCtx := cuecontext.New()

	for _, path := range cueFiles {
		name := filepath.ToSlash(path)
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}

			// Prepend the generated schema exactly as the renderer does (see
			// console/templates/defaults.go). Then add stub declarations for
			// the well-known template variables (platform, input) so that
			// docs snippets that reference these as free variables — exactly
			// as they would be injected by the renderer at runtime — compile
			// without "reference not found" errors. This mirrors the approach
			// the production renderer takes: it unifies the template body with
			// a concrete #PlatformInput and #ProjectInput before evaluation.
			const templateStubs = `
// Stub declarations injected by the test harness to mirror what the
// renderer injects at evaluation time.
platform: #PlatformInput
input: #ProjectInput
`
			fullSrc := v1alpha2.GeneratedSchema + templateStubs + "\n" + string(src)
			val := cueCtx.CompileString(fullSrc)
			if err := val.Err(); err != nil {
				t.Errorf("docs snippet %s failed to compile against v1alpha2 schema: %v", path, err)
			}
		})
	}
}
