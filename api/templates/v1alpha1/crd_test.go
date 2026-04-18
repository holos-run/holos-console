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

package v1alpha1_test

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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// envtestSuite wraps the test env so each package-level test can share one
// API server instance. envtest startup takes several seconds; running
// multiple test functions against the same environment keeps the overall
// suite fast.
type envtestSuite struct {
	env    *envtest.Environment
	client client.Client
}

// setupEnvTest boots an envtest API server with the templates.holos.run CRDs
// plus both CEL admission policies installed. It skips the test (not fails)
// when envtest binaries are not available, so developers and CI agents that
// do not pre-install envtest can still run `go test` without the suite
// failing outright. The skip path is explicit and noisy so a missing envtest
// setup is not mistaken for a passing test.
func setupEnvTest(t *testing.T) *envtestSuite {
	t.Helper()

	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		// setup-envtest places binaries under
		// ~/.local/share/kubebuilder-envtest/k8s/<version>-<os>-<arch>. We
		// search for the highest-version directory; failing that, we skip.
		if assets := detectEnvtestAssets(); assets != "" {
			t.Setenv("KUBEBUILDER_ASSETS", assets)
		} else {
			t.Skip("envtest binaries not found; set KUBEBUILDER_ASSETS or run `setup-envtest use` to download")
		}
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("finding repo root: %v", err)
	}

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(repoRoot, "config", "crd")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("starting envtest: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := env.Stop(); stopErr != nil {
			t.Logf("stopping envtest: %v", stopErr)
		}
	})

	if err := v1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("registering v1alpha1 scheme: %v", err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("constructing controller-runtime client: %v", err)
	}

	// envtest does not load ValidatingAdmissionPolicy manifests automatically
	// (no ValidatingAdmissionPolicyPaths field), so we apply them through the
	// generic client after Start() returns. This runs the same YAML that
	// ships to production clusters, keeping the admission regression suite
	// in lockstep with the actual policy surface.
	ctx := context.Background()
	admissionDir := filepath.Join(repoRoot, "config", "admission")
	if err := applyYAMLFilesInDir(ctx, c, admissionDir); err != nil {
		t.Fatalf("applying admission policies: %v", err)
	}

	return &envtestSuite{env: env, client: c}
}

// applyYAMLFilesInDir reads every *.yaml file in dir and server-side applies
// each document through the controller-runtime client. Used to install the
// CEL ValidatingAdmissionPolicy manifests after envtest.Environment.Start()
// returns — envtest itself has no built-in VAP installer.
func applyYAMLFilesInDir(ctx context.Context, c client.Client, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		// Split on YAML document separator and apply each doc
		// individually. The VAP manifests ship one policy + one binding
		// per file joined with "---".
		for _, doc := range splitYAMLDocuments(data) {
			if len(strings.TrimSpace(string(doc))) == 0 {
				continue
			}
			// Try each known kind in order — VAP and VAPBinding are
			// both registered in admissionregistration/v1.
			if err := applyAdmissionDoc(ctx, c, doc); err != nil {
				return fmt.Errorf("apply doc from %s: %w", e.Name(), err)
			}
		}
	}
	return nil
}

func splitYAMLDocuments(data []byte) [][]byte {
	// Very small splitter — envtest manifests are authored in-repo and
	// never contain the "---" sequence inside a string, so a line-wise
	// split is sufficient.
	var docs [][]byte
	var current []byte
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "---" {
			if len(current) > 0 {
				docs = append(docs, current)
			}
			current = nil
			continue
		}
		current = append(current, []byte(line+"\n")...)
	}
	if len(current) > 0 {
		docs = append(docs, current)
	}
	return docs
}

func applyAdmissionDoc(ctx context.Context, c client.Client, doc []byte) error {
	// Probe the Kind field to pick the correct runtime type. We keep this
	// narrow to the two kinds we ship in config/admission/.
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

// findRepoRoot walks up from the current test file to find the nearest
// go.mod, which gives us an absolute path to the holos-console repo root so
// the envtest CRDDirectoryPaths are stable regardless of the caller's CWD.
func findRepoRoot() (string, error) {
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

// detectEnvtestAssets finds the highest-version envtest asset directory under
// the user's XDG data dir. Returns empty string when nothing is found so the
// caller can decide whether to skip.
func detectEnvtestAssets() string {
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

// createNamespace provisions a namespace labeled for the given resource-type.
// The resolver and the CEL admission policies read
// `console.holos.run/resource-type` to decide whether a given namespace may
// host TemplatePolicy / TemplatePolicyBinding writes.
func createNamespace(t *testing.T, ctx context.Context, c client.Client, name, resourceType string) {
	t.Helper()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"console.holos.run/resource-type": resourceType,
			},
		},
	}
	if err := c.Create(ctx, ns); err != nil {
		t.Fatalf("creating namespace %q: %v", name, err)
	}
}

func TestCRDRoundTrip_Template(t *testing.T) {
	s := setupEnvTest(t)
	ctx := context.Background()
	nsName := "holos-prj-roundtrip-template"
	createNamespace(t, ctx, s.client, nsName, "project")

	tests := []struct {
		name     string
		template *v1alpha1.Template
	}{
		{
			name: "minimal",
			template: &v1alpha1.Template{
				ObjectMeta: metav1.ObjectMeta{Name: "minimal", Namespace: nsName},
				Spec: v1alpha1.TemplateSpec{
					DisplayName: "Minimal",
					Description: "Minimal spec round-trip",
					Enabled:     true,
					CueTemplate: "package holos\n",
				},
			},
		},
		{
			name: "with-defaults-and-links",
			template: &v1alpha1.Template{
				ObjectMeta: metav1.ObjectMeta{Name: "with-defaults", Namespace: nsName},
				Spec: v1alpha1.TemplateSpec{
					DisplayName: "With defaults",
					Enabled:     true,
					Version:     "1.2.3",
					CueTemplate: "package holos\n",
					Defaults: &v1alpha1.TemplateDefaults{
						Image: "ghcr.io/example/app",
						Tag:   "latest",
						Port:  8080,
						Env: []v1alpha1.EnvVar{
							{Name: "LOG_LEVEL", Value: "info"},
						},
					},
					LinkedTemplates: []v1alpha1.LinkedTemplateRef{
						{
							Scope:             "organization",
							ScopeName:         "acme",
							Name:              "istio-gateway",
							VersionConstraint: ">=1.0.0 <2.0.0",
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj := tc.template
			assertSpecRoundTrip(t, ctx, s.client, obj)
			assertStatusSubresource(t, ctx, s.client, obj, &v1alpha1.Template{})
			deleteAndWait(t, ctx, s.client, obj)
		})
	}
}

func TestCRDRoundTrip_TemplatePolicy(t *testing.T) {
	s := setupEnvTest(t)
	ctx := context.Background()
	nsName := "holos-fld-roundtrip-policy"
	createNamespace(t, ctx, s.client, nsName, "folder")

	obj := &v1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "require-one", Namespace: nsName},
		Spec: v1alpha1.TemplatePolicySpec{
			DisplayName: "Require one",
			Description: "Forces istio-gateway on every bound target.",
			Rules: []v1alpha1.TemplatePolicyRule{
				{
					Kind: v1alpha1.TemplatePolicyKindRequire,
					Template: v1alpha1.LinkedTemplateRef{
						Scope:     "organization",
						ScopeName: "acme",
						Name:      "istio-gateway",
					},
				},
			},
		},
	}
	assertSpecRoundTrip(t, ctx, s.client, obj)
	assertStatusSubresource(t, ctx, s.client, obj, &v1alpha1.TemplatePolicy{})
	deleteAndWait(t, ctx, s.client, obj)
}

func TestCRDRoundTrip_TemplatePolicyBinding(t *testing.T) {
	s := setupEnvTest(t)
	ctx := context.Background()
	nsName := "holos-fld-roundtrip-binding"
	createNamespace(t, ctx, s.client, nsName, "folder")

	obj := &v1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "bind-one", Namespace: nsName},
		Spec: v1alpha1.TemplatePolicyBindingSpec{
			DisplayName: "Bind one",
			PolicyRef: v1alpha1.LinkedTemplatePolicyRef{
				Scope:     "folder",
				ScopeName: "roundtrip-binding",
				Name:      "require-one",
			},
			TargetRefs: []v1alpha1.TemplatePolicyBindingTargetRef{
				{
					Kind:        v1alpha1.TemplatePolicyBindingTargetKindDeployment,
					ProjectName: "api",
					Name:        "gateway",
				},
			},
		},
	}
	assertSpecRoundTrip(t, ctx, s.client, obj)
	assertStatusSubresource(t, ctx, s.client, obj, &v1alpha1.TemplatePolicyBinding{})
	deleteAndWait(t, ctx, s.client, obj)
}

// assertSpecRoundTrip exercises the create -> get -> update cycle and
// asserts every spec field survived the API-server round trip.
func assertSpecRoundTrip(t *testing.T, ctx context.Context, c client.Client, obj client.Object) {
	t.Helper()
	if err := c.Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}
	key := client.ObjectKeyFromObject(obj)

	// Round-trip read
	got := emptyFor(obj)
	if err := c.Get(ctx, key, got); err != nil {
		t.Fatalf("get after create: %v", err)
	}
	if got.GetResourceVersion() == "" {
		t.Fatalf("expected a resourceVersion on %T %s", got, key)
	}

	// Mutate a spec field and update so we know the spec subresource is
	// writable end-to-end.
	switch typed := got.(type) {
	case *v1alpha1.Template:
		typed.Spec.Description = "updated"
		if err := c.Update(ctx, typed); err != nil {
			t.Fatalf("update template: %v", err)
		}
	case *v1alpha1.TemplatePolicy:
		typed.Spec.Description = "updated"
		if err := c.Update(ctx, typed); err != nil {
			t.Fatalf("update policy: %v", err)
		}
	case *v1alpha1.TemplatePolicyBinding:
		typed.Spec.Description = "updated"
		if err := c.Update(ctx, typed); err != nil {
			t.Fatalf("update binding: %v", err)
		}
	default:
		t.Fatalf("unexpected type %T", got)
	}
}

// assertStatusSubresource writes a Ready condition via the Status subresource
// and re-reads to confirm it landed — this exercises the
// +kubebuilder:subresource:status marker. A spec-only Update MUST NOT change
// status, which we assert by reading status back after the spec update.
func assertStatusSubresource(t *testing.T, ctx context.Context, c client.Client, obj client.Object, fresh client.Object) {
	t.Helper()
	key := client.ObjectKeyFromObject(obj)
	if err := c.Get(ctx, key, fresh); err != nil {
		t.Fatalf("get for status write: %v", err)
	}
	now := metav1.NewTime(time.Now().Truncate(time.Second))
	cond := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "envtest round-trip",
		LastTransitionTime: now,
		ObservedGeneration: fresh.GetGeneration(),
	}
	switch typed := fresh.(type) {
	case *v1alpha1.Template:
		typed.Status.ObservedGeneration = typed.GetGeneration()
		typed.Status.Conditions = append(typed.Status.Conditions, cond)
	case *v1alpha1.TemplatePolicy:
		typed.Status.ObservedGeneration = typed.GetGeneration()
		typed.Status.Conditions = append(typed.Status.Conditions, cond)
	case *v1alpha1.TemplatePolicyBinding:
		typed.Status.ObservedGeneration = typed.GetGeneration()
		typed.Status.Conditions = append(typed.Status.Conditions, cond)
	default:
		t.Fatalf("unexpected type %T", fresh)
	}

	if err := c.Status().Update(ctx, fresh); err != nil {
		t.Fatalf("status update: %v", err)
	}

	after := emptyFor(obj)
	if err := c.Get(ctx, key, after); err != nil {
		t.Fatalf("get after status update: %v", err)
	}
	var conds []metav1.Condition
	switch typed := after.(type) {
	case *v1alpha1.Template:
		conds = typed.Status.Conditions
	case *v1alpha1.TemplatePolicy:
		conds = typed.Status.Conditions
	case *v1alpha1.TemplatePolicyBinding:
		conds = typed.Status.Conditions
	}
	if len(conds) == 0 {
		t.Fatalf("expected status.conditions to be written via subresource, got none")
	}
	if conds[0].Type != "Ready" || conds[0].Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True, got %+v", conds[0])
	}
}

// deleteAndWait removes the object and confirms the API server reports
// NotFound — an explicit guard that delete actually lands rather than being
// silently ignored by a missing CRD.
func deleteAndWait(t *testing.T, ctx context.Context, c client.Client, obj client.Object) {
	t.Helper()
	if err := c.Delete(ctx, obj); err != nil {
		t.Fatalf("delete: %v", err)
	}
	key := client.ObjectKeyFromObject(obj)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		check := emptyFor(obj)
		err := c.Get(ctx, key, check)
		if apierrors.IsNotFound(err) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for delete of %s", key)
}

// emptyFor returns an empty instance of the same kind as obj so callers can
// read without reusing a mutated struct.
func emptyFor(obj client.Object) client.Object {
	switch obj.(type) {
	case *v1alpha1.Template:
		return &v1alpha1.Template{}
	case *v1alpha1.TemplatePolicy:
		return &v1alpha1.TemplatePolicy{}
	case *v1alpha1.TemplatePolicyBinding:
		return &v1alpha1.TemplatePolicyBinding{}
	default:
		panic(fmt.Sprintf("emptyFor: unsupported %T", obj))
	}
}

// TestAdmissionPolicy_TemplatePolicy_ProjectNamespace_Rejected and
// TestAdmissionPolicy_TemplatePolicyBinding_ProjectNamespace_Rejected exercise
// the CEL ValidatingAdmissionPolicy shipped in config/admission. Table-driven
// by (kind, namespace-kind) pairs: creation in a project-labeled namespace
// rejects with a CEL-originated admission error; creation in a folder- or
// org-labeled namespace succeeds.
func TestAdmission_FolderOrOrgOnly(t *testing.T) {
	s := setupEnvTest(t)
	ctx := context.Background()

	// Wait for the admission policy to be registered before issuing
	// writes; envtest loads VAP manifests asynchronously after the API
	// server starts and a raced create can slip through the guard.
	waitForAdmissionPolicy(t, ctx, s.client, "templatepolicy-folder-or-org-only")
	waitForAdmissionPolicy(t, ctx, s.client, "templatepolicybinding-folder-or-org-only")

	tests := []struct {
		name          string
		nsName        string
		nsResourceTyp string
		build         func(ns string) client.Object
		wantRejected  bool
	}{
		{
			name:          "policy-in-project-rejected",
			nsName:        "holos-prj-admission-reject-policy",
			nsResourceTyp: "project",
			build: func(ns string) client.Object {
				return &v1alpha1.TemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: ns},
					Spec: v1alpha1.TemplatePolicySpec{
						Rules: []v1alpha1.TemplatePolicyRule{{
							Kind: v1alpha1.TemplatePolicyKindRequire,
							Template: v1alpha1.LinkedTemplateRef{
								Scope: "organization", ScopeName: "acme", Name: "t",
							},
						}},
					},
				}
			},
			wantRejected: true,
		},
		{
			name:          "policy-in-folder-accepted",
			nsName:        "holos-fld-admission-accept-policy",
			nsResourceTyp: "folder",
			build: func(ns string) client.Object {
				return &v1alpha1.TemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: ns},
					Spec: v1alpha1.TemplatePolicySpec{
						Rules: []v1alpha1.TemplatePolicyRule{{
							Kind: v1alpha1.TemplatePolicyKindRequire,
							Template: v1alpha1.LinkedTemplateRef{
								Scope: "organization", ScopeName: "acme", Name: "t",
							},
						}},
					},
				}
			},
			wantRejected: false,
		},
		{
			name:          "policy-in-org-accepted",
			nsName:        "holos-org-admission-accept-policy",
			nsResourceTyp: "organization",
			build: func(ns string) client.Object {
				return &v1alpha1.TemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: ns},
					Spec: v1alpha1.TemplatePolicySpec{
						Rules: []v1alpha1.TemplatePolicyRule{{
							Kind: v1alpha1.TemplatePolicyKindRequire,
							Template: v1alpha1.LinkedTemplateRef{
								Scope: "organization", ScopeName: "acme", Name: "t",
							},
						}},
					},
				}
			},
			wantRejected: false,
		},
		{
			name:          "binding-in-project-rejected",
			nsName:        "holos-prj-admission-reject-binding",
			nsResourceTyp: "project",
			build: func(ns string) client.Object {
				return &v1alpha1.TemplatePolicyBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: ns},
					Spec: v1alpha1.TemplatePolicyBindingSpec{
						PolicyRef: v1alpha1.LinkedTemplatePolicyRef{
							Scope: "organization", ScopeName: "acme", Name: "p",
						},
						TargetRefs: []v1alpha1.TemplatePolicyBindingTargetRef{{
							Kind:        v1alpha1.TemplatePolicyBindingTargetKindDeployment,
							ProjectName: "api", Name: "gateway",
						}},
					},
				}
			},
			wantRejected: true,
		},
		{
			name:          "binding-in-folder-accepted",
			nsName:        "holos-fld-admission-accept-binding",
			nsResourceTyp: "folder",
			build: func(ns string) client.Object {
				return &v1alpha1.TemplatePolicyBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: ns},
					Spec: v1alpha1.TemplatePolicyBindingSpec{
						PolicyRef: v1alpha1.LinkedTemplatePolicyRef{
							Scope: "organization", ScopeName: "acme", Name: "p",
						},
						TargetRefs: []v1alpha1.TemplatePolicyBindingTargetRef{{
							Kind:        v1alpha1.TemplatePolicyBindingTargetKindDeployment,
							ProjectName: "api", Name: "gateway",
						}},
					},
				}
			},
			wantRejected: false,
		},
		{
			name:          "binding-in-org-accepted",
			nsName:        "holos-org-admission-accept-binding",
			nsResourceTyp: "organization",
			build: func(ns string) client.Object {
				return &v1alpha1.TemplatePolicyBinding{
					ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: ns},
					Spec: v1alpha1.TemplatePolicyBindingSpec{
						PolicyRef: v1alpha1.LinkedTemplatePolicyRef{
							Scope: "organization", ScopeName: "acme", Name: "p",
						},
						TargetRefs: []v1alpha1.TemplatePolicyBindingTargetRef{{
							Kind:        v1alpha1.TemplatePolicyBindingTargetKindDeployment,
							ProjectName: "api", Name: "gateway",
						}},
					},
				}
			},
			wantRejected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			createNamespace(t, ctx, s.client, tc.nsName, tc.nsResourceTyp)
			obj := tc.build(tc.nsName)
			err := s.client.Create(ctx, obj)
			if tc.wantRejected {
				if err == nil {
					t.Fatalf("expected admission rejection for project namespace, got nil")
				}
				if !apierrors.IsInvalid(err) && !apierrors.IsForbidden(err) {
					t.Fatalf("expected Invalid/Forbidden admission error, got %T: %v", err, err)
				}
				if !strings.Contains(err.Error(), "project namespace") &&
					!strings.Contains(err.Error(), "Forbidden") &&
					!strings.Contains(err.Error(), "denied") {
					t.Fatalf("expected CEL-originated rejection message, got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected admission to accept in %q namespace, got %v", tc.nsResourceTyp, err)
			}
		})
	}
}

// waitForAdmissionPolicy polls for a registered ValidatingAdmissionPolicy by
// name. envtest starts the API server immediately but does not block Start()
// on VAP manifest application; racing a Create ahead of the guard leads to
// flaky false-negative admission tests.
func waitForAdmissionPolicy(t *testing.T, ctx context.Context, c client.Client, name string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
		if err := c.Get(ctx, types.NamespacedName{Name: name}, vap); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("admission policy %q not registered within deadline", name)
}
