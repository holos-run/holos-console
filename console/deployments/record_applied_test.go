package deployments

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"k8s.io/client-go/kubernetes/fake"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// recordAppliedHandler builds a deployments handler wired with a stub
// ancestor template provider, a stub policy drift checker, and an editor-
// grant on alice@example.com. The renderer and applier behavior is
// controlled by the caller via the returned pointers so the caller can
// toggle render/apply failures to exercise the rollback branches.
//
// The stubAncestorTemplateProvider mirrors whatever refs the caller passes
// into ListAncestorTemplateSources back as the effective set (unless
// effectiveRefs is explicitly set on the stub), so write-through tests
// can pin the payload passed to RecordApplied.
func recordAppliedHandler(
	t *testing.T,
	atp *stubAncestorTemplateProvider,
	checker *stubPolicyDriftChecker,
	renderer Renderer,
	applier ResourceApplier,
) (*Handler, *fake.Clientset) {
	t.Helper()
	fakeClient := fake.NewClientset(projectNS("my-project"))
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
	k8s := NewK8sClient(fakeClient, testResolver())
	h := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, applier).
		WithAncestorTemplateProvider(atp)
	if checker != nil {
		h = h.WithPolicyDriftChecker(checker)
	}
	return h, fakeClient
}

// aliceEditorCtx returns an authenticated context for the editor test
// principal used throughout this file.
func aliceEditorCtx() context.Context {
	return authedCtx("alice@example.com", nil)
}

// TestHandler_CreateDeployment_RecordsAppliedOnSuccess verifies that a
// successful CreateDeployment happy path calls RecordApplied exactly once
// with the effective ref set returned by AncestorTemplateProvider.
func TestHandler_CreateDeployment_RecordsAppliedOnSuccess(t *testing.T) {
	wantRefs := []*consolev1.LinkedTemplateRef{
		&consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "httproute"},
		&consolev1.LinkedTemplateRef{Namespace: "holos-fld-payments", Name: "audit"},
	}
	atp := &stubAncestorTemplateProvider{
		sources:       []string{"// folder template"},
		effectiveRefs: wantRefs,
	}
	checker := &stubPolicyDriftChecker{}
	h, _ := recordAppliedHandler(t, atp, checker, &stubRenderer{}, &stubApplier{})

	req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
		Project:  "my-project",
		Name:     "web-app",
		Image:    "nginx",
		Tag:      "1.25",
		Template: "default",
	})
	if _, err := h.CreateDeployment(aliceEditorCtx(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if checker.recordCalls != 1 {
		t.Fatalf("RecordApplied called %d times, want 1", checker.recordCalls)
	}
	if checker.lastRecordProject != "my-project" {
		t.Errorf("RecordApplied project: got %q, want %q", checker.lastRecordProject, "my-project")
	}
	if checker.lastRecordName != "web-app" {
		t.Errorf("RecordApplied name: got %q, want %q", checker.lastRecordName, "web-app")
	}
	if len(checker.lastRecordRefs) != len(wantRefs) {
		t.Fatalf("RecordApplied refs length: got %d, want %d", len(checker.lastRecordRefs), len(wantRefs))
	}
	for i, r := range wantRefs {
		got := checker.lastRecordRefs[i]
		if got.GetNamespace() != r.GetNamespace() || got.GetName() != r.GetName() {
			t.Errorf("RecordApplied refs[%d]: got %+v, want %+v", i, got, r)
		}
	}
}

// TestHandler_CreateDeployment_NoRecordOnRenderFailure verifies that when
// rendering fails, the rollback branch runs and RecordApplied is NOT called —
// nothing was actually rendered, so there is nothing to record.
func TestHandler_CreateDeployment_NoRecordOnRenderFailure(t *testing.T) {
	atp := &stubAncestorTemplateProvider{sources: []string{"// folder template"}}
	checker := &stubPolicyDriftChecker{}
	renderer := &stubRenderer{err: errors.New("simulated render failure")}
	applier := &stubApplier{}
	h, _ := recordAppliedHandler(t, atp, checker, renderer, applier)

	req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
		Project:  "my-project",
		Name:     "web-app",
		Image:    "nginx",
		Tag:      "1.25",
		Template: "default",
	})
	if _, err := h.CreateDeployment(aliceEditorCtx(), req); err == nil {
		t.Fatal("expected error from render failure")
	}
	if checker.recordCalls != 0 {
		t.Errorf("RecordApplied was called %d times on render failure, want 0", checker.recordCalls)
	}
	if !applier.cleanupCalled {
		t.Error("expected rollback Cleanup to be called")
	}
}

// TestHandler_CreateDeployment_NoRecordOnApplyFailure verifies that when
// Apply fails, the rollback branch runs and RecordApplied is NOT called.
func TestHandler_CreateDeployment_NoRecordOnApplyFailure(t *testing.T) {
	atp := &stubAncestorTemplateProvider{sources: []string{"// folder template"}}
	checker := &stubPolicyDriftChecker{}
	renderer := &stubRenderer{}
	applier := &stubApplier{applyErr: errors.New("simulated apply failure")}
	h, _ := recordAppliedHandler(t, atp, checker, renderer, applier)

	req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
		Project:  "my-project",
		Name:     "web-app",
		Image:    "nginx",
		Tag:      "1.25",
		Template: "default",
	})
	if _, err := h.CreateDeployment(aliceEditorCtx(), req); err == nil {
		t.Fatal("expected error from apply failure")
	}
	if checker.recordCalls != 0 {
		t.Errorf("RecordApplied was called %d times on apply failure, want 0", checker.recordCalls)
	}
	if !applier.cleanupCalled {
		t.Error("expected rollback Cleanup to be called")
	}
}

// TestHandler_CreateDeployment_WarnButSucceedOnRecordFailure verifies that a
// RecordApplied error is logged at warn level and swallowed — the RPC returns
// success because the deployment was already rendered and applied.
func TestHandler_CreateDeployment_WarnButSucceedOnRecordFailure(t *testing.T) {
	atp := &stubAncestorTemplateProvider{sources: []string{"// folder template"}}
	checker := &stubPolicyDriftChecker{recordErr: errors.New("applied-state write failed")}
	h, _ := recordAppliedHandler(t, atp, checker, &stubRenderer{}, &stubApplier{})

	req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
		Project:  "my-project",
		Name:     "web-app",
		Image:    "nginx",
		Tag:      "1.25",
		Template: "default",
	})
	resp, err := h.CreateDeployment(aliceEditorCtx(), req)
	if err != nil {
		t.Fatalf("expected success despite record failure, got %v", err)
	}
	if resp.Msg.Name != "web-app" {
		t.Errorf("name: got %q, want web-app", resp.Msg.Name)
	}
	if checker.recordCalls != 1 {
		t.Errorf("RecordApplied called %d times, want 1", checker.recordCalls)
	}
}

// degradedAncestorProvider models the contract
// ListEffectiveTemplateSources now provides on the walker-failure and
// nil-walker branches: nil sources and nil effectiveRefs so the HOL-569
// write-through is skipped. Introduced by review round 1 P1 fix; kept as
// a dedicated stub (vs. setting effectiveRefs=nil on the shared
// stubAncestorTemplateProvider) because the shared stub mirrors input
// refs back as a non-nil empty slice to exercise the happy path.
type degradedAncestorProvider struct{}

func (degradedAncestorProvider) ListAncestorTemplateSources(_ context.Context, _, _ string) ([]string, []*consolev1.LinkedTemplateRef, error) {
	return nil, nil, nil
}

// TestHandler_CreateDeployment_RecordsEmptyOnDegradedRender verifies that
// when the ancestor template provider returns nil effectiveRefs — the
// contract for "ancestor walk failed / no walker, render is project-only"
// — the handler calls RecordApplied with a non-nil empty slice so the
// stored applied baseline honestly reflects "zero ancestor templates were
// unified into this apply" (review round 2 P2 finding). Skipping the
// record would leave any previously recorded applied set in place, and a
// subsequent GetDeploymentPolicyState read could compare the current
// policy output against that stale baseline and report false no-drift
// even though the last apply did not include those templates.
func TestHandler_CreateDeployment_RecordsEmptyOnDegradedRender(t *testing.T) {
	checker := &stubPolicyDriftChecker{}
	fakeClient := fake.NewClientset(projectNS("my-project"))
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
	k8s := NewK8sClient(fakeClient, testResolver())
	h := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, &stubRenderer{}, &stubApplier{}).
		WithAncestorTemplateProvider(degradedAncestorProvider{}).
		WithPolicyDriftChecker(checker)

	req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
		Project:  "my-project",
		Name:     "web-app",
		Image:    "nginx",
		Tag:      "1.25",
		Template: "default",
	})
	if _, err := h.CreateDeployment(aliceEditorCtx(), req); err != nil {
		t.Fatalf("unexpected error on degraded render: %v", err)
	}
	if checker.recordCalls != 1 {
		t.Fatalf("RecordApplied called %d times on degraded render; want 1 (empty baseline)", checker.recordCalls)
	}
	if checker.lastRecordRefs == nil {
		t.Errorf("RecordApplied refs: got nil on degraded render; want non-nil empty slice")
	}
	if len(checker.lastRecordRefs) != 0 {
		t.Errorf("RecordApplied refs: got %d entries on degraded render; want 0", len(checker.lastRecordRefs))
	}
}

// TestHandler_CreateDeployment_RecordsEmptyOnZeroAncestorRefs verifies the
// HOL-569 baseline contract: a successful apply with no TPB-resolved ancestor
// templates still persists an applied set (the empty slice) so
// GetDeploymentPolicyState reports has_applied_state=true on the very
// next read. The normalization in ListEffectiveTemplateSources plus the
// empty-slice write-through keeps "no refs" distinct from "degraded render".
func TestHandler_CreateDeployment_RecordsEmptyOnZeroAncestorRefs(t *testing.T) {
	// Stub provider returns a non-nil empty effective-ref slice —
	// mirroring what ListEffectiveTemplateSources now produces on a
	// successful render whose resolver output is empty.
	atp := &stubAncestorTemplateProvider{
		sources:       []string{"// deployment template only"},
		effectiveRefs: []*consolev1.LinkedTemplateRef{},
	}
	checker := &stubPolicyDriftChecker{}
	h, _ := recordAppliedHandler(t, atp, checker, &stubRenderer{}, &stubApplier{})

	req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
		Project:  "my-project",
		Name:     "web-app",
		Image:    "nginx",
		Tag:      "1.25",
		Template: "default",
	})
	if _, err := h.CreateDeployment(aliceEditorCtx(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if checker.recordCalls != 1 {
		t.Fatalf("RecordApplied called %d times, want 1 (even for zero-ref render)", checker.recordCalls)
	}
	if checker.lastRecordRefs == nil {
		t.Error("RecordApplied refs: got nil on zero-ref render; want non-nil empty slice")
	}
	if len(checker.lastRecordRefs) != 0 {
		t.Errorf("RecordApplied refs length: got %d, want 0", len(checker.lastRecordRefs))
	}
}

// TestHandler_CreateDeployment_NilCheckerIsSafe verifies that a nil drift
// checker is a silent no-op on the Create happy path — local/dev bootstraps
// without a cluster policy resolver continue to work after HOL-569.
func TestHandler_CreateDeployment_NilCheckerIsSafe(t *testing.T) {
	atp := &stubAncestorTemplateProvider{sources: []string{"// folder template"}}
	h, _ := recordAppliedHandler(t, atp, nil, &stubRenderer{}, &stubApplier{})

	req := connect.NewRequest(&consolev1.CreateDeploymentRequest{
		Project:  "my-project",
		Name:     "web-app",
		Image:    "nginx",
		Tag:      "1.25",
		Template: "default",
	})
	if _, err := h.CreateDeployment(aliceEditorCtx(), req); err != nil {
		t.Fatalf("unexpected error with nil checker: %v", err)
	}
}

// TestHandler_UpdateDeployment_RecordsAppliedOnSuccess verifies the
// UpdateDeployment happy path calls RecordApplied once with the effective
// ref set returned by AncestorTemplateProvider after a successful reconcile.
func TestHandler_UpdateDeployment_RecordsAppliedOnSuccess(t *testing.T) {
	wantRefs := []*consolev1.LinkedTemplateRef{
		&consolev1.LinkedTemplateRef{Namespace: "holos-fld-payments", Name: "audit"},
	}
	atp := &stubAncestorTemplateProvider{
		sources:       []string{"// folder template"},
		effectiveRefs: wantRefs,
	}
	checker := &stubPolicyDriftChecker{}

	// Update needs a seeded deployment ConfigMap to target.
	fakeClient := fake.NewClientset(
		projectNS("my-project"),
		deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc"),
	)
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
	k8s := NewK8sClient(fakeClient, testResolver())
	h := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, &stubRenderer{}, &stubApplier{}).
		WithAncestorTemplateProvider(atp).
		WithPolicyDriftChecker(checker)

	newTag := "1.26"
	req := connect.NewRequest(&consolev1.UpdateDeploymentRequest{
		Project: "my-project",
		Name:    "web-app",
		Tag:     &newTag,
	})
	if _, err := h.UpdateDeployment(aliceEditorCtx(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if checker.recordCalls != 1 {
		t.Fatalf("RecordApplied called %d times, want 1", checker.recordCalls)
	}
	if checker.lastRecordProject != "my-project" || checker.lastRecordName != "web-app" {
		t.Errorf("RecordApplied (project,name): got (%q,%q), want (my-project,web-app)", checker.lastRecordProject, checker.lastRecordName)
	}
	if len(checker.lastRecordRefs) != 1 || checker.lastRecordRefs[0].GetName() != "audit" {
		t.Errorf("RecordApplied refs: got %+v, want [audit]", checker.lastRecordRefs)
	}
}

// TestHandler_UpdateDeployment_NoRecordOnRenderFailure verifies that a
// render failure aborts the RPC before RecordApplied can run.
func TestHandler_UpdateDeployment_NoRecordOnRenderFailure(t *testing.T) {
	atp := &stubAncestorTemplateProvider{sources: []string{"// folder template"}}
	checker := &stubPolicyDriftChecker{}

	fakeClient := fake.NewClientset(
		projectNS("my-project"),
		deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc"),
	)
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
	k8s := NewK8sClient(fakeClient, testResolver())
	renderer := &stubRenderer{err: errors.New("simulated render failure")}
	h := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, renderer, &stubApplier{}).
		WithAncestorTemplateProvider(atp).
		WithPolicyDriftChecker(checker)

	newTag := "1.26"
	req := connect.NewRequest(&consolev1.UpdateDeploymentRequest{
		Project: "my-project",
		Name:    "web-app",
		Tag:     &newTag,
	})
	if _, err := h.UpdateDeployment(aliceEditorCtx(), req); err == nil {
		t.Fatal("expected error from render failure")
	}
	if checker.recordCalls != 0 {
		t.Errorf("RecordApplied was called %d times on render failure, want 0", checker.recordCalls)
	}
}

// TestHandler_UpdateDeployment_NoRecordOnReconcileFailure verifies that a
// reconcile failure aborts the RPC before RecordApplied can run.
func TestHandler_UpdateDeployment_NoRecordOnReconcileFailure(t *testing.T) {
	atp := &stubAncestorTemplateProvider{sources: []string{"// folder template"}}
	checker := &stubPolicyDriftChecker{}

	fakeClient := fake.NewClientset(
		projectNS("my-project"),
		deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc"),
	)
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
	k8s := NewK8sClient(fakeClient, testResolver())
	applier := &stubApplier{reconcileErr: errors.New("simulated reconcile failure")}
	h := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, &stubRenderer{}, applier).
		WithAncestorTemplateProvider(atp).
		WithPolicyDriftChecker(checker)

	newTag := "1.26"
	req := connect.NewRequest(&consolev1.UpdateDeploymentRequest{
		Project: "my-project",
		Name:    "web-app",
		Tag:     &newTag,
	})
	if _, err := h.UpdateDeployment(aliceEditorCtx(), req); err == nil {
		t.Fatal("expected error from reconcile failure")
	}
	if checker.recordCalls != 0 {
		t.Errorf("RecordApplied was called %d times on reconcile failure, want 0", checker.recordCalls)
	}
}

// TestHandler_UpdateDeployment_WarnButSucceedOnRecordFailure verifies that a
// RecordApplied error after a successful reconcile is logged and swallowed.
func TestHandler_UpdateDeployment_WarnButSucceedOnRecordFailure(t *testing.T) {
	atp := &stubAncestorTemplateProvider{sources: []string{"// folder template"}}
	checker := &stubPolicyDriftChecker{recordErr: errors.New("applied-state write failed")}

	fakeClient := fake.NewClientset(
		projectNS("my-project"),
		deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc"),
	)
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
	k8s := NewK8sClient(fakeClient, testResolver())
	h := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, &stubRenderer{}, &stubApplier{}).
		WithAncestorTemplateProvider(atp).
		WithPolicyDriftChecker(checker)

	newTag := "1.26"
	req := connect.NewRequest(&consolev1.UpdateDeploymentRequest{
		Project: "my-project",
		Name:    "web-app",
		Tag:     &newTag,
	})
	if _, err := h.UpdateDeployment(aliceEditorCtx(), req); err != nil {
		t.Fatalf("expected success despite record failure, got %v", err)
	}
	if checker.recordCalls != 1 {
		t.Errorf("RecordApplied called %d times, want 1", checker.recordCalls)
	}
}

// TestHandler_UpdateDeployment_NilCheckerIsSafe verifies that a nil drift
// checker is a silent no-op on the Update happy path.
func TestHandler_UpdateDeployment_NilCheckerIsSafe(t *testing.T) {
	atp := &stubAncestorTemplateProvider{sources: []string{"// folder template"}}

	fakeClient := fake.NewClientset(
		projectNS("my-project"),
		deploymentConfigMap("my-project", "web-app", "nginx", "1.25", "default", "Web App", "desc"),
	)
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
	k8s := NewK8sClient(fakeClient, testResolver())
	h := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, &stubTemplateResolver{cm: fakeTemplate("default")}, &stubRenderer{}, &stubApplier{}).
		WithAncestorTemplateProvider(atp)

	newTag := "1.26"
	req := connect.NewRequest(&consolev1.UpdateDeploymentRequest{
		Project: "my-project",
		Name:    "web-app",
		Tag:     &newTag,
	})
	if _, err := h.UpdateDeployment(aliceEditorCtx(), req); err != nil {
		t.Fatalf("unexpected error with nil checker: %v", err)
	}
}
