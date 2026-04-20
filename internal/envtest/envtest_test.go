/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package envtest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/holos-run/holos-console/internal/envtest"
)

// TestFindRepoRoot returns a directory that contains go.mod, regardless
// of the caller's CWD. The helper is consumed from test files that live
// deep in the package tree, so the walk must reliably surface the
// module root.
func TestFindRepoRoot(t *testing.T) {
	root, err := envtest.FindRepoRoot()
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected go.mod under %q: %v", root, err)
	}
}

// TestSplitYAMLDocuments covers the degenerate boundaries we hit in
// real manifests: empty input, single doc, two docs separated by "---",
// and trailing separator. Any regression here would corrupt the
// admission-policy installer.
func TestSplitYAMLDocuments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "empty", in: "", want: 0},
		{name: "single", in: "a: 1\n", want: 1},
		{name: "two-docs", in: "a: 1\n---\nb: 2\n", want: 2},
		{name: "leading-separator", in: "---\na: 1\n", want: 1},
		{name: "trailing-separator", in: "a: 1\n---\n", want: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := envtest.SplitYAMLDocuments([]byte(tc.in))
			if len(got) != tc.want {
				t.Fatalf("want %d docs, got %d (%q)", tc.want, len(got), got)
			}
		})
	}
}

// TestDetectAssets exercises the search path but accepts either a
// found path (developer already ran setup-envtest) or the empty string
// (clean machine). It asserts only that the returned path, when
// non-empty, actually contains a kube-apiserver binary so a stale
// XDG layout does not mask a broken detection.
func TestDetectAssets(t *testing.T) {
	got := envtest.DetectAssets()
	if got == "" {
		return
	}
	if _, err := os.Stat(filepath.Join(got, "kube-apiserver")); err != nil {
		t.Fatalf("DetectAssets returned %q without a kube-apiserver binary: %v", got, err)
	}
}
