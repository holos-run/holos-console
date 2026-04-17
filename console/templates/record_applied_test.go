package templates

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// recordingResolver is a PolicyResolver test double that captures its last
// invocation and returns a caller-controlled resolved set. Tests use it to
// assert the handler invokes the seam once with the right inputs and
// forwards the policy-expanded set into RecordApplied (not the raw explicit
// refs).
type recordingResolver struct {
	resolved         []*consolev1.LinkedTemplateRef
	err              error
	calls            int
	lastProjectNs    string
	lastTargetKind   policyresolver.TargetKind
	lastTargetName   string
	lastExplicitRefs []*consolev1.LinkedTemplateRef
}

func (r *recordingResolver) Resolve(
	_ context.Context,
	projectNs string,
	targetKind policyresolver.TargetKind,
	targetName string,
	explicitRefs []*consolev1.LinkedTemplateRef,
) ([]*consolev1.LinkedTemplateRef, error) {
	r.calls++
	r.lastProjectNs = projectNs
	r.lastTargetKind = targetKind
	r.lastTargetName = targetName
	r.lastExplicitRefs = explicitRefs
	if r.err != nil {
		return nil, r.err
	}
	if r.resolved != nil {
		return r.resolved, nil
	}
	return explicitRefs, nil
}

// recordAppliedTemplateHandler wires a templates handler with project-scope
// write access for owner@localhost, a configurable PolicyResolver, and an
// optional ProjectTemplateDriftChecker. seedObjs are additional runtime.Object
// fixtures written into the fake clientset (e.g. an existing template
// ConfigMap for update-path tests).
func recordAppliedTemplateHandler(
	t *testing.T,
	policyResolver policyresolver.PolicyResolver,
	checker ProjectTemplateDriftChecker,
	seedObjs ...runtime.Object,
) *Handler {
	t.Helper()
	objs := []runtime.Object{projectNS("my-project")}
	objs = append(objs, seedObjs...)
	fakeClient := fake.NewClientset(objs...)

	h := newTestHandler(fakeClient, map[string]string{"owner@localhost": "owner"})
	// Replace the no-op resolver wired by newTestHandler with the test's
	// controlled instance so assertions can pin the effective ref set.
	h.policyResolver = policyResolver
	if checker != nil {
		h = h.WithProjectTemplateDriftChecker(checker)
	}
	return h
}

// ownerCtx returns an authenticated context for owner@localhost, the
// principal mapped to "owner" by recordAppliedTemplateHandler.
func ownerCtx() context.Context {
	return authedCtx("owner@localhost", nil)
}

// existingProjectTemplateWithLinks returns a seeded project-scope template
// ConfigMap carrying the supplied explicit link list on the
// AnnotationLinkedTemplates annotation. Used by UpdateTemplate tests that
// exercise the "preserve existing links" branch.
func existingProjectTemplateWithLinks(project, name string, refs []*consolev1.LinkedTemplateRef) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "prj-" + project,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationEnabled: "true",
			},
		},
		Data: map[string]string{
			CueTemplateKey: "#Input: { name: string }\n",
		},
	}
	if len(refs) > 0 {
		raw, err := marshalLinkedTemplatesForTest(refs)
		if err == nil {
			cm.Annotations[v1alpha2.AnnotationLinkedTemplates] = raw
		}
	}
	return cm
}

// marshalLinkedTemplatesForTest serializes linked refs using the same JSON
// shape the production helper emits. Re-implementing the shape here avoids
// taking a dependency on an unexported helper.
func marshalLinkedTemplatesForTest(refs []*consolev1.LinkedTemplateRef) (string, error) {
	type storedRef struct {
		Scope             string `json:"scope"`
		ScopeName         string `json:"scope_name"`
		Name              string `json:"name"`
		VersionConstraint string `json:"version_constraint,omitempty"`
	}
	stored := make([]storedRef, 0, len(refs))
	for _, r := range refs {
		stored = append(stored, storedRef{
			Scope:             scopeLabelValue(r.GetScope()),
			ScopeName:         r.GetScopeName(),
			Name:              r.GetName(),
			VersionConstraint: r.GetVersionConstraint(),
		})
	}
	b, err := json.Marshal(stored)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// TestHandler_CreateTemplate_RecordsAppliedOnSuccess verifies that a
// successful project-scope CreateTemplate calls RecordApplied with the
// policy-resolved effective ref set (explicit ∪ REQUIRE − EXCLUDE), not
// the raw explicit list.
func TestHandler_CreateTemplate_RecordsAppliedOnSuccess(t *testing.T) {
	explicit := []*consolev1.LinkedTemplateRef{
		orgLinkedRef("acme", "httproute"),
	}
	required := []*consolev1.LinkedTemplateRef{
		orgLinkedRef("acme", "httproute"),
		folderLinkedRef("payments", "audit"),
	}
	resolver := &recordingResolver{resolved: required}
	checker := &stubProjectTemplateDriftChecker{}

	h := recordAppliedTemplateHandler(t, resolver, checker)

	req := connect.NewRequest(&consolev1.CreateTemplateRequest{
		Scope: projectScopeRef("my-project"),
		Template: &consolev1.Template{
			Name:            "web-app",
			CueTemplate:     validCue,
			LinkedTemplates: explicit,
		},
	})
	if _, err := h.CreateTemplate(ownerCtx(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolver.calls != 1 {
		t.Fatalf("resolver.Resolve called %d times, want 1", resolver.calls)
	}
	if resolver.lastTargetKind != policyresolver.TargetKindProjectTemplate {
		t.Errorf("resolver targetKind: got %v, want TargetKindProjectTemplate", resolver.lastTargetKind)
	}
	if resolver.lastProjectNs != "prj-my-project" {
		t.Errorf("resolver projectNs: got %q, want prj-my-project", resolver.lastProjectNs)
	}
	if resolver.lastTargetName != "web-app" {
		t.Errorf("resolver targetName: got %q, want web-app", resolver.lastTargetName)
	}

	if checker.recordCalls != 1 {
		t.Fatalf("RecordApplied called %d times, want 1", checker.recordCalls)
	}
	if checker.lastRecordProject != "my-project" || checker.lastRecordName != "web-app" {
		t.Errorf("RecordApplied (project,name): got (%q,%q)", checker.lastRecordProject, checker.lastRecordName)
	}
	if len(checker.lastRecordRefs) != 2 {
		t.Fatalf("RecordApplied refs length: got %d, want 2 (explicit + REQUIRE)", len(checker.lastRecordRefs))
	}
	foundAudit := false
	for _, r := range checker.lastRecordRefs {
		if r.GetName() == "audit" {
			foundAudit = true
		}
	}
	if !foundAudit {
		t.Errorf("RecordApplied refs: missing REQUIRE-injected 'audit', got %+v", checker.lastRecordRefs)
	}
}

// TestHandler_CreateTemplate_NoRecordAtOrgOrFolderScope verifies that
// non-project-scope templates do not record applied state. The stub
// checker and resolver would record the call if it was invoked, so a
// recordCalls value of 0 is the assertion. Covers the acceptance criterion
// that org/folder CreateTemplate is a no-op for the write-through.
func TestHandler_CreateTemplate_NoRecordAtOrgOrFolderScope(t *testing.T) {
	resolver := &recordingResolver{}
	checker := &stubProjectTemplateDriftChecker{}

	// Seed org and folder namespaces so CreateTemplate can write into them.
	orgNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "org-acme"},
	}
	folderNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "fld-payments"},
	}
	// newTestHandler only wires a project grant; the in-memory handler's
	// owner gets access to org/folder templates via the same
	// shareUsers map because newTestHandler only adds ProjectGrantResolver.
	// For org/folder we need the corresponding grant resolvers; since this
	// test only exercises the write-through side, we bypass the checkAccess
	// gate by calling the internal helper.
	//
	// Instead, assert the write-through is scope-gated by invoking
	// recordProjectTemplateApplied directly with each non-project scope.
	// This keeps the assertion narrow: if the helper is scope-gated,
	// RecordApplied is never called.
	h := recordAppliedTemplateHandler(t, resolver, checker, orgNs, folderNs)

	h.recordProjectTemplateApplied(
		context.Background(),
		consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
		"acme",
		"httproute",
		nil,
	)
	if checker.recordCalls != 0 {
		t.Errorf("RecordApplied called at org scope (%d times), want 0", checker.recordCalls)
	}

	h.recordProjectTemplateApplied(
		context.Background(),
		consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER,
		"payments",
		"audit",
		nil,
	)
	if checker.recordCalls != 0 {
		t.Errorf("RecordApplied called at folder scope (%d times), want 0", checker.recordCalls)
	}
	if resolver.calls != 0 {
		t.Errorf("resolver.Resolve called at non-project scope (%d times), want 0", resolver.calls)
	}
}

// TestHandler_CreateTemplate_WarnButSucceedOnRecordFailure verifies that
// a RecordApplied error on the create path is logged at warn level and
// swallowed — the RPC returns success because the template ConfigMap
// was persisted.
func TestHandler_CreateTemplate_WarnButSucceedOnRecordFailure(t *testing.T) {
	resolver := &recordingResolver{}
	checker := &stubProjectTemplateDriftChecker{recordErr: errors.New("applied-state write failed")}

	h := recordAppliedTemplateHandler(t, resolver, checker)

	req := connect.NewRequest(&consolev1.CreateTemplateRequest{
		Scope: projectScopeRef("my-project"),
		Template: &consolev1.Template{
			Name:        "web-app",
			CueTemplate: validCue,
		},
	})
	if _, err := h.CreateTemplate(ownerCtx(), req); err != nil {
		t.Fatalf("expected success despite record failure, got %v", err)
	}
	if checker.recordCalls != 1 {
		t.Errorf("RecordApplied called %d times, want 1", checker.recordCalls)
	}
}

// TestHandler_CreateTemplate_NilCheckerIsSafe verifies that a nil drift
// checker is a silent no-op on the project-scope create path. Also
// verifies the resolver is not invoked when there is nothing to record —
// a nil checker short-circuits the write-through.
func TestHandler_CreateTemplate_NilCheckerIsSafe(t *testing.T) {
	resolver := &recordingResolver{}
	h := recordAppliedTemplateHandler(t, resolver, nil)

	req := connect.NewRequest(&consolev1.CreateTemplateRequest{
		Scope: projectScopeRef("my-project"),
		Template: &consolev1.Template{
			Name:        "web-app",
			CueTemplate: validCue,
		},
	})
	if _, err := h.CreateTemplate(ownerCtx(), req); err != nil {
		t.Fatalf("unexpected error with nil checker: %v", err)
	}
	if resolver.calls != 0 {
		t.Errorf("resolver.Resolve called %d times with nil checker, want 0", resolver.calls)
	}
}

// TestHandler_CreateTemplate_NoRecordOnPersistFailure verifies that a
// failure to persist the ConfigMap aborts the RPC before RecordApplied
// can run. Seeding an AlreadyExists collision drives the K8s error path.
func TestHandler_CreateTemplate_NoRecordOnPersistFailure(t *testing.T) {
	resolver := &recordingResolver{}
	checker := &stubProjectTemplateDriftChecker{}

	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-app",
			Namespace: "prj-my-project",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
			},
		},
	}
	h := recordAppliedTemplateHandler(t, resolver, checker, existing)

	req := connect.NewRequest(&consolev1.CreateTemplateRequest{
		Scope: projectScopeRef("my-project"),
		Template: &consolev1.Template{
			Name:        "web-app",
			CueTemplate: validCue,
		},
	})
	if _, err := h.CreateTemplate(ownerCtx(), req); err == nil {
		t.Fatal("expected AlreadyExists error from persist failure")
	}
	if checker.recordCalls != 0 {
		t.Errorf("RecordApplied called %d times on persist failure, want 0", checker.recordCalls)
	}
}

// TestHandler_UpdateTemplate_RecordsAppliedOnSuccess_NewLinks verifies the
// update_linked_templates=true path: the new linked list is resolved and
// recorded.
func TestHandler_UpdateTemplate_RecordsAppliedOnSuccess_NewLinks(t *testing.T) {
	newLinks := []*consolev1.LinkedTemplateRef{
		folderLinkedRef("payments", "audit"),
	}
	resolver := &recordingResolver{resolved: newLinks}
	checker := &stubProjectTemplateDriftChecker{}

	existing := existingProjectTemplateWithLinks("my-project", "web-app", []*consolev1.LinkedTemplateRef{
		orgLinkedRef("acme", "httproute"),
	})
	h := recordAppliedTemplateHandler(t, resolver, checker, existing)

	req := connect.NewRequest(&consolev1.UpdateTemplateRequest{
		Scope: projectScopeRef("my-project"),
		Template: &consolev1.Template{
			Name:            "web-app",
			CueTemplate:     validCue,
			LinkedTemplates: newLinks,
		},
		UpdateLinkedTemplates: true,
	})
	if _, err := h.UpdateTemplate(ownerCtx(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolver.calls != 1 {
		t.Errorf("resolver.Resolve called %d times, want 1", resolver.calls)
	}
	if len(resolver.lastExplicitRefs) != 1 || resolver.lastExplicitRefs[0].GetName() != "audit" {
		t.Errorf("resolver explicitRefs: got %+v, want [audit]", resolver.lastExplicitRefs)
	}
	if checker.recordCalls != 1 {
		t.Errorf("RecordApplied called %d times, want 1", checker.recordCalls)
	}
}

// TestHandler_UpdateTemplate_RecordsAppliedOnSuccess_PreserveLinks verifies
// the update_linked_templates=false path: the pre-existing linked list is
// resolved and recorded so drift is computed against the unchanged link
// state.
func TestHandler_UpdateTemplate_RecordsAppliedOnSuccess_PreserveLinks(t *testing.T) {
	existingLinks := []*consolev1.LinkedTemplateRef{
		orgLinkedRef("acme", "httproute"),
	}
	resolver := &recordingResolver{}
	checker := &stubProjectTemplateDriftChecker{}

	existing := existingProjectTemplateWithLinks("my-project", "web-app", existingLinks)
	h := recordAppliedTemplateHandler(t, resolver, checker, existing)

	req := connect.NewRequest(&consolev1.UpdateTemplateRequest{
		Scope: projectScopeRef("my-project"),
		Template: &consolev1.Template{
			Name:        "web-app",
			CueTemplate: validCue,
		},
		UpdateLinkedTemplates: false,
	})
	if _, err := h.UpdateTemplate(ownerCtx(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolver.calls != 1 {
		t.Fatalf("resolver.Resolve called %d times, want 1", resolver.calls)
	}
	if len(resolver.lastExplicitRefs) != 1 || resolver.lastExplicitRefs[0].GetName() != "httproute" {
		t.Errorf("resolver explicitRefs (preserve-links path): got %+v, want [httproute]", resolver.lastExplicitRefs)
	}
	if checker.recordCalls != 1 {
		t.Errorf("RecordApplied called %d times, want 1", checker.recordCalls)
	}
	if len(checker.lastRecordRefs) != 1 || checker.lastRecordRefs[0].GetName() != "httproute" {
		t.Errorf("RecordApplied refs (preserve-links path): got %+v, want [httproute]", checker.lastRecordRefs)
	}
}

// TestHandler_UpdateTemplate_NoRecordOnPersistFailure verifies that when
// the template ConfigMap does not exist (NotFound on the Update path),
// RecordApplied is not called.
func TestHandler_UpdateTemplate_NoRecordOnPersistFailure(t *testing.T) {
	resolver := &recordingResolver{}
	checker := &stubProjectTemplateDriftChecker{}

	// No existing template seeded — UpdateTemplate should fail NotFound.
	h := recordAppliedTemplateHandler(t, resolver, checker)

	req := connect.NewRequest(&consolev1.UpdateTemplateRequest{
		Scope: projectScopeRef("my-project"),
		Template: &consolev1.Template{
			Name:        "web-app",
			CueTemplate: validCue,
		},
		UpdateLinkedTemplates: false,
	})
	if _, err := h.UpdateTemplate(ownerCtx(), req); err == nil {
		t.Fatal("expected error from missing template")
	}
	if checker.recordCalls != 0 {
		t.Errorf("RecordApplied called %d times on persist failure, want 0", checker.recordCalls)
	}
}

// TestHandler_UpdateTemplate_WarnButSucceedOnRecordFailure verifies that a
// RecordApplied error after a successful Update is logged and swallowed.
func TestHandler_UpdateTemplate_WarnButSucceedOnRecordFailure(t *testing.T) {
	resolver := &recordingResolver{}
	checker := &stubProjectTemplateDriftChecker{recordErr: errors.New("applied-state write failed")}

	existing := existingProjectTemplateWithLinks("my-project", "web-app", nil)
	h := recordAppliedTemplateHandler(t, resolver, checker, existing)

	req := connect.NewRequest(&consolev1.UpdateTemplateRequest{
		Scope: projectScopeRef("my-project"),
		Template: &consolev1.Template{
			Name:        "web-app",
			CueTemplate: validCue,
		},
		UpdateLinkedTemplates: false,
	})
	if _, err := h.UpdateTemplate(ownerCtx(), req); err != nil {
		t.Fatalf("expected success despite record failure, got %v", err)
	}
	if checker.recordCalls != 1 {
		t.Errorf("RecordApplied called %d times, want 1", checker.recordCalls)
	}
}

// TestHandler_UpdateTemplate_NilCheckerIsSafe verifies that a nil drift
// checker is a silent no-op on the project-scope update path and the
// resolver is not invoked.
func TestHandler_UpdateTemplate_NilCheckerIsSafe(t *testing.T) {
	resolver := &recordingResolver{}
	existing := existingProjectTemplateWithLinks("my-project", "web-app", nil)
	h := recordAppliedTemplateHandler(t, resolver, nil, existing)

	req := connect.NewRequest(&consolev1.UpdateTemplateRequest{
		Scope: projectScopeRef("my-project"),
		Template: &consolev1.Template{
			Name:        "web-app",
			CueTemplate: validCue,
		},
		UpdateLinkedTemplates: false,
	})
	if _, err := h.UpdateTemplate(ownerCtx(), req); err != nil {
		t.Fatalf("unexpected error with nil checker: %v", err)
	}
	if resolver.calls != 0 {
		t.Errorf("resolver.Resolve called %d times with nil checker, want 0", resolver.calls)
	}
}

// TestHandler_UpdateTemplate_ResolverFailureIsSwallowed verifies that a
// resolver failure on the write-through path does not fail the RPC — the
// template was persisted and the warn log captures the diagnostic.
func TestHandler_UpdateTemplate_ResolverFailureIsSwallowed(t *testing.T) {
	resolver := &recordingResolver{err: errors.New("policy fetch failed")}
	checker := &stubProjectTemplateDriftChecker{}

	existing := existingProjectTemplateWithLinks("my-project", "web-app", nil)
	h := recordAppliedTemplateHandler(t, resolver, checker, existing)

	req := connect.NewRequest(&consolev1.UpdateTemplateRequest{
		Scope: projectScopeRef("my-project"),
		Template: &consolev1.Template{
			Name:        "web-app",
			CueTemplate: validCue,
		},
		UpdateLinkedTemplates: false,
	})
	if _, err := h.UpdateTemplate(ownerCtx(), req); err != nil {
		t.Fatalf("expected success despite resolver failure, got %v", err)
	}
	if checker.recordCalls != 0 {
		t.Errorf("RecordApplied called %d times on resolver failure, want 0 (write-through aborted)", checker.recordCalls)
	}
}
