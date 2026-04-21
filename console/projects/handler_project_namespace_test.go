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

package projects

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/projects/projectapply"
	"github.com/holos-run/holos-console/console/projects/projectnspipeline"
	"github.com/holos-run/holos-console/console/templates"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// --- Fakes for the pipeline seams -------------------------------------
//
// Each test wires its own Pipeline via projectnspipeline.New and injects
// stubs for the resolver / policy getter / template getter / renderer /
// applier seams so we can cover the four HOL-812 ACs (no bindings,
// bindings happy path, render error, apply timeout) without standing up
// the full resolver + CueRenderer + dynamic-client stack.

// pipelineAdapter converts between the handler's
// ProjectNamespacePipeline interface (defined in handler.go) and the
// concrete projectnspipeline.Pipeline — mirrors the production adapter
// in console/console.go. Isolated here so handler tests do not depend
// on the wiring package.
type pipelineAdapter struct {
	p *projectnspipeline.Pipeline
}

func (a *pipelineAdapter) Run(ctx context.Context, in ProjectNamespacePipelineInput) (ProjectNamespacePipelineOutcome, error) {
	out, err := a.p.Run(ctx, projectnspipeline.Input{
		ProjectName:     in.ProjectName,
		ParentNamespace: in.ParentNamespace,
		BaseNamespace:   in.BaseNamespace,
		Platform:        in.Platform,
	})
	var mapped ProjectNamespacePipelineOutcome
	if out == projectnspipeline.OutcomeBindingsApplied {
		mapped = ProjectNamespacePipelineBindingsApplied
	}
	return mapped, err
}

// wrap converts a concrete Pipeline into the handler's interface. Tests
// call this rather than passing the Pipeline directly so the handler
// package's interface contract is exercised.
func wrap(p *projectnspipeline.Pipeline) ProjectNamespacePipeline {
	return &pipelineAdapter{p: p}
}

type fakeBindingResolver struct {
	bindings []*policyresolver.ResolvedBinding
	err      error
	calls    int
}

func (f *fakeBindingResolver) Resolve(_ context.Context, _, _ string) ([]*policyresolver.ResolvedBinding, error) {
	f.calls++
	return f.bindings, f.err
}

type fakePolicyGetter struct {
	policy *projectnspipeline.Policy
	err    error
}

func (f *fakePolicyGetter) GetPolicy(_ context.Context, _, _ string) (*projectnspipeline.Policy, error) {
	return f.policy, f.err
}

type fakeTemplateGetter struct {
	source string
	err    error
}

func (f *fakeTemplateGetter) GetTemplateSource(_ context.Context, _, _ string) (string, error) {
	return f.source, f.err
}

type fakeRenderer struct {
	result *templates.ProjectNamespaceRenderResult
	err    error
	calls  int
}

func (f *fakeRenderer) RenderForProjectNamespace(_ context.Context, in templates.ProjectNamespaceRenderInput) (*templates.ProjectNamespaceRenderResult, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	// Default: pass through a result whose Namespace mirrors the base.
	// Tests that want richer output override f.result.
	ns := &unstructured.Unstructured{}
	ns.SetAPIVersion("v1")
	ns.SetKind("Namespace")
	ns.SetName(in.BaseNamespace.Name)
	return &templates.ProjectNamespaceRenderResult{Namespace: ns}, nil
}

type fakeApplier struct {
	err   error
	calls int
}

func (f *fakeApplier) Apply(_ context.Context, _ *templates.ProjectNamespaceRenderResult) error {
	f.calls++
	return f.err
}

// --- AC 1: no bindings → existing Namespace-create path unchanged -----

func TestCreateProject_NoProjectNamespaceBindings_UsesExistingPath(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, nil)
	logHandler := &testLogHandler{}
	slog.SetDefault(slog.New(logHandler))

	// Pipeline present but resolver returns no bindings — the handler
	// must fall through to the typed Create path.
	resolver := &fakeBindingResolver{bindings: nil}
	applier := &fakeApplier{}
	renderer := &fakeRenderer{}
	handler = handler.WithProjectNamespacePipeline(wrap(projectnspipeline.New(
		resolver,
		&fakePolicyGetter{},
		&fakeTemplateGetter{},
		renderer,
		applier,
	)))

	ctx := contextWithClaims("alice@example.com")
	resp, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "no-bindings",
		Organization: "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "no-bindings" {
		t.Errorf("expected name 'no-bindings', got %q", resp.Msg.Name)
	}

	// The resolver must have been consulted exactly once.
	if resolver.calls != 1 {
		t.Errorf("expected resolver called once, got %d", resolver.calls)
	}
	// Render and apply must not have run.
	if renderer.calls != 0 {
		t.Errorf("expected renderer not called, got %d", renderer.calls)
	}
	if applier.calls != 0 {
		t.Errorf("expected applier not called, got %d", applier.calls)
	}
	// The typed create path must have written the namespace.
	if _, err := fakeClient.CoreV1().Namespaces().Get(ctx, "holos-prj-no-bindings", metav1.GetOptions{}); err != nil {
		t.Errorf("expected typed Create to persist namespace, got %v", err)
	}
	if r := logHandler.findRecord("project_ns_bindings_resolved"); r == nil {
		t.Error("expected project_ns_bindings_resolved audit log")
	}
	if r := logHandler.findRecord("project_create"); r == nil {
		t.Error("expected project_create audit log")
	}
}

// --- AC 2: one matching binding → applier path, typed Create skipped --

func TestCreateProject_OneProjectNamespaceBinding_AppliesAndSkipsTypedCreate(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, nil)
	logHandler := &testLogHandler{}
	slog.SetDefault(slog.New(logHandler))

	// One resolved binding referencing one policy → one template.
	resolver := &fakeBindingResolver{
		bindings: []*policyresolver.ResolvedBinding{
			{Name: "bind-1", Namespace: "holos-org-acme"},
		},
	}
	policyGetter := &fakePolicyGetter{
		policy: &projectnspipeline.Policy{
			Namespace: "holos-org-acme",
			Name:      "acme-baseline",
			TemplateRefs: []projectnspipeline.TemplateRef{
				{Namespace: "holos-org-acme", Name: "ns-labels"},
			},
		},
	}
	tmplGetter := &fakeTemplateGetter{source: "// cue template placeholder\npackage holos\n"}

	renderer := &fakeRenderer{}
	applier := &fakeApplier{}
	handler = handler.WithProjectNamespacePipeline(wrap(projectnspipeline.New(
		resolver,
		policyGetter,
		tmplGetter,
		renderer,
		applier,
	)))

	ctx := contextWithClaims("alice@example.com")
	resp, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "has-bindings",
		Organization: "acme",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "has-bindings" {
		t.Errorf("expected name 'has-bindings', got %q", resp.Msg.Name)
	}

	if renderer.calls != 1 {
		t.Errorf("expected renderer called once, got %d", renderer.calls)
	}
	if applier.calls != 1 {
		t.Errorf("expected applier called once, got %d", applier.calls)
	}

	// The typed Create path MUST be skipped — the applier owns the
	// Namespace SSA when bindings match. The fake clientset is blank
	// apart from the pre-existing namespace "holos-prj-existing", so
	// asking for the new project namespace should return NotFound.
	if _, err := fakeClient.CoreV1().Namespaces().Get(ctx, "holos-prj-has-bindings", metav1.GetOptions{}); err == nil {
		t.Error("expected typed Create to be skipped when applier handles SSA, but namespace exists")
	}

	if r := logHandler.findRecord("project_ns_apply_ok"); r == nil {
		t.Error("expected project_ns_apply_ok audit log")
	}
	if r := logHandler.findRecord("project_ns_render_ok"); r == nil {
		t.Error("expected project_ns_render_ok audit log")
	}
	if r := logHandler.findRecord("project_create"); r == nil {
		t.Error("expected project_create audit log")
	}
}

// --- AC 3: render error → CodeInternal surfaced via mapK8sError ------

func TestCreateProject_ProjectNamespaceRenderError_ReturnsInternalError(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, nil)
	logHandler := &testLogHandler{}
	slog.SetDefault(slog.New(logHandler))

	resolver := &fakeBindingResolver{
		bindings: []*policyresolver.ResolvedBinding{
			{Name: "bind-1", Namespace: "holos-org-acme"},
		},
	}
	policyGetter := &fakePolicyGetter{
		policy: &projectnspipeline.Policy{
			Name: "acme-baseline",
			TemplateRefs: []projectnspipeline.TemplateRef{
				{Namespace: "holos-org-acme", Name: "bad"},
			},
		},
	}
	tmplGetter := &fakeTemplateGetter{source: "// broken cue"}
	renderer := &fakeRenderer{err: errors.New("cue: synthetic render failure")}
	applier := &fakeApplier{}
	handler = handler.WithProjectNamespacePipeline(wrap(projectnspipeline.New(
		resolver,
		policyGetter,
		tmplGetter,
		renderer,
		applier,
	)))

	ctx := contextWithClaims("alice@example.com")
	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "render-err",
		Organization: "acme",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", connectErr.Code())
	}

	if applier.calls != 0 {
		t.Errorf("expected applier not called on render error, got %d", applier.calls)
	}
	// Typed Create must NOT run — render failure blocks the whole path.
	if _, err := fakeClient.CoreV1().Namespaces().Get(ctx, "holos-prj-render-err", metav1.GetOptions{}); err == nil {
		t.Error("expected namespace not to be created on render error")
	}

	if r := logHandler.findRecord("project_ns_render_error"); r == nil {
		t.Error("expected project_ns_render_error audit log")
	}
}

// --- AC 4: apply timeout → CodeDeadlineExceeded ----------------------

func TestCreateProject_ProjectNamespaceApplyTimeout_ReturnsDeadlineExceeded(t *testing.T) {
	existing := managedNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	fakeClient := fake.NewClientset(existing)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s, nil)
	logHandler := &testLogHandler{}
	slog.SetDefault(slog.New(logHandler))

	resolver := &fakeBindingResolver{
		bindings: []*policyresolver.ResolvedBinding{
			{Name: "bind-1", Namespace: "holos-org-acme"},
		},
	}
	policyGetter := &fakePolicyGetter{
		policy: &projectnspipeline.Policy{
			Name: "acme-baseline",
			TemplateRefs: []projectnspipeline.TemplateRef{
				{Namespace: "holos-org-acme", Name: "ns-labels"},
			},
		},
	}
	tmplGetter := &fakeTemplateGetter{source: "// cue template placeholder\npackage holos\n"}
	// Renderer produces a valid result (the default fakeRenderer behavior).
	// Applier returns the structured DeadlineExceededError the real
	// projectapply.Applier would return when the namespace-ready wait
	// or the namespace-scoped apply retry exhausts its budget.
	renderer := &fakeRenderer{}
	applier := &fakeApplier{err: &projectapply.DeadlineExceededError{
		Kind:       "Namespace",
		Name:       "holos-prj-timeout",
		LastPhase:  "Pending",
		Classifier: "",
	}}
	handler = handler.WithProjectNamespacePipeline(wrap(projectnspipeline.New(
		resolver,
		policyGetter,
		tmplGetter,
		renderer,
		applier,
	)))

	ctx := contextWithClaims("alice@example.com")
	_, err := handler.CreateProject(ctx, connect.NewRequest(&consolev1.CreateProjectRequest{
		Name:         "timeout",
		Organization: "acme",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeDeadlineExceeded {
		t.Errorf("expected CodeDeadlineExceeded, got %v", connectErr.Code())
	}

	// Typed Create must NOT run — the applier took ownership of the
	// Namespace even though it timed out, and running a typed Create
	// afterwards would race the applier's own SSA on retry.
	if _, err := fakeClient.CoreV1().Namespaces().Get(ctx, "holos-prj-timeout", metav1.GetOptions{}); err == nil {
		t.Error("expected namespace not to be created via typed path on apply timeout")
	}

	if r := logHandler.findRecord("project_ns_apply_timeout"); r == nil {
		t.Error("expected project_ns_apply_timeout audit log")
	}
}

// --- Interface contract assertion ------------------------------------

// Compile-time check that the in-test fakes satisfy the pipeline's
// interface seams. Keeps a refactor that renames or splits an interface
// from silently skipping these four ACs.
var (
	_ projectnspipeline.BindingResolver = (*fakeBindingResolver)(nil)
	_ projectnspipeline.PolicyGetter    = (*fakePolicyGetter)(nil)
	_ projectnspipeline.TemplateGetter  = (*fakeTemplateGetter)(nil)
	_ projectnspipeline.Renderer        = (*fakeRenderer)(nil)
	_ projectnspipeline.Applier         = (*fakeApplier)(nil)
)
