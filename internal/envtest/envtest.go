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

// Package envtest collects the boot helpers shared by every CRD
// round-trip and admission regression suite in the repo. Per-package
// test files still own their own envtest.Environment{} construction —
// CRD directories and scheme registrations differ per group — but the
// four mechanical chores they share (finding an envtest assets
// directory, locating the repo root, splitting + applying
// ValidatingAdmissionPolicy manifests, and polling for a VAP to
// register) live here so drift between api/templates/v1alpha1 and
// api/secrets/v1alpha1 is not possible.
package envtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DetectAssets finds the highest-version envtest asset directory under
// the user's XDG data dir. Returns empty string when nothing is found so
// the caller can decide whether to skip. setup-envtest places binaries
// under ~/.local/share/kubebuilder-envtest/k8s/<version>-<os>-<arch>.
func DetectAssets() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	base := filepath.Join(home, ".local", "share", "kubebuilder-envtest", "k8s")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	var best string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(base, e.Name())
		if _, err := os.Stat(filepath.Join(candidate, "kube-apiserver")); err == nil {
			if best == "" || e.Name() > filepath.Base(best) {
				best = candidate
			}
		}
	}
	return best
}

// FindRepoRoot walks up from the current file to find the nearest
// go.mod, which gives an absolute path to the holos-console repo root
// so envtest CRDDirectoryPaths are stable regardless of the caller's
// CWD. Walks up from this source file (internal/envtest/envtest.go), so
// the walk is invariant to the caller's package location.
func FindRepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %q", file)
		}
		dir = parent
	}
}

// SplitYAMLDocuments splits a byte buffer on the YAML "---" document
// separator. Envtest manifests are authored in-repo and never contain
// the separator inside a string, so a line-wise split is sufficient and
// intentionally simpler than a full YAML parser.
func SplitYAMLDocuments(data []byte) [][]byte {
	var docs [][]byte
	var current []byte
	flush := func() {
		if len(strings.TrimSpace(string(current))) > 0 {
			docs = append(docs, current)
		}
		current = nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "---" {
			flush()
			continue
		}
		current = append(current, []byte(line+"\n")...)
	}
	flush()
	return docs
}

// ApplyAdmissionDoc decodes a single YAML document and creates the
// corresponding admissionregistration/v1 object via the
// controller-runtime client. Supported kinds are
// ValidatingAdmissionPolicy and ValidatingAdmissionPolicyBinding —
// anything else returns an error so an accidentally committed
// non-admission YAML does not silently slip through the bootstrap.
func ApplyAdmissionDoc(ctx context.Context, c client.Client, doc []byte) error {
	kindProbe := struct {
		Kind string `json:"kind"`
	}{}
	if err := yaml.Unmarshal(doc, &kindProbe); err != nil {
		return fmt.Errorf("unmarshal kind: %w", err)
	}
	switch kindProbe.Kind {
	case "ValidatingAdmissionPolicy":
		policy := &admissionregistrationv1.ValidatingAdmissionPolicy{}
		if err := yaml.Unmarshal(doc, policy); err != nil {
			return fmt.Errorf("unmarshal policy: %w", err)
		}
		return c.Create(ctx, policy)
	case "ValidatingAdmissionPolicyBinding":
		binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
		if err := yaml.Unmarshal(doc, binding); err != nil {
			return fmt.Errorf("unmarshal binding: %w", err)
		}
		return c.Create(ctx, binding)
	default:
		return fmt.Errorf("unsupported admission kind %q", kindProbe.Kind)
	}
}

// ApplyYAMLFilesInDir reads every *.yaml file in dir and applies each
// document it contains through ApplyAdmissionDoc. Used to install the
// CEL ValidatingAdmissionPolicy manifests after
// envtest.Environment.Start() returns — envtest has no built-in VAP
// installer. Kustomization.yaml is skipped because it is a kustomize
// index file, not a runtime manifest.
func ApplyYAMLFilesInDir(ctx context.Context, c client.Client, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		if e.Name() == "kustomization.yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		for _, doc := range SplitYAMLDocuments(data) {
			if len(strings.TrimSpace(string(doc))) == 0 {
				continue
			}
			if err := ApplyAdmissionDoc(ctx, c, doc); err != nil {
				return fmt.Errorf("apply doc from %s: %w", e.Name(), err)
			}
		}
	}
	return nil
}

// WaitForAdmissionPolicy polls for a registered
// ValidatingAdmissionPolicy and its same-named binding. envtest starts
// the API server immediately but does not block Start() on VAP
// manifest application; racing a Create ahead of the guard leads to
// flaky false-negative admission tests. Waiting for the binding is
// required too — the admission plugin only compiles and activates a
// policy once BOTH the VAP and its binding are visible in the API
// server's policy cache. Our admission manifests follow a one-VAP +
// one-same-named-binding convention, so this helper assumes the
// binding shares the policy's name.
func WaitForAdmissionPolicy(t *testing.T, ctx context.Context, c client.Client, name string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
		vapErr := c.Get(ctx, types.NamespacedName{Name: name}, vap)
		binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
		bindingErr := c.Get(ctx, types.NamespacedName{Name: name}, binding)
		if vapErr == nil && bindingErr == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("admission policy %q not registered within deadline", name)
}
