package templates

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// stubTemplatePolicyStateProvider implements
// ProjectTemplatePolicyStateProvider for tests. Each lookup path has its own
// (value, error) pair so tests can simulate a failure in exactly one of the
// two paths the handler depends on.
type stubTemplatePolicyStateProvider struct {
	applied    []*consolev1.LinkedTemplateRef
	appliedErr error
	current    []*consolev1.LinkedTemplateRef
	currentErr error
	recordErr  error

	// Recorded state for assertions on write paths.
	recordedCalls      int
	recordedRefs       []*consolev1.LinkedTemplateRef
	lastCurrentBaseLen int
	lastCurrentBase    []*consolev1.LinkedTemplateRef
}

func (s *stubTemplatePolicyStateProvider) CurrentRenderSet(_ context.Context, _, _ string, base []*consolev1.LinkedTemplateRef) ([]*consolev1.LinkedTemplateRef, error) {
	s.lastCurrentBaseLen = len(base)
	s.lastCurrentBase = append([]*consolev1.LinkedTemplateRef(nil), base...)
	return s.current, s.currentErr
}

func (s *stubTemplatePolicyStateProvider) AppliedRenderSet(_ context.Context, _, _ string) ([]*consolev1.LinkedTemplateRef, error) {
	return s.applied, s.appliedErr
}

func (s *stubTemplatePolicyStateProvider) RecordAppliedRenderSet(_ context.Context, _, _ string, refs []*consolev1.LinkedTemplateRef) error {
	s.recordedCalls++
	s.recordedRefs = refs
	return s.recordErr
}

// TestGetProjectTemplatePolicyState_DoesNotFabricateDriftOnLookupError
// locks in the HOL-557 round-1 review fix: when either AppliedRenderSet or
// CurrentRenderSet fails, the handler must NOT synthesize a drift
// comparison from zero values. The RPC returns a Connect error so the
// caller can distinguish "partial data" from "genuine drift."
func TestGetProjectTemplatePolicyState_DoesNotFabricateDriftOnLookupError(t *testing.T) {
	const project = "my-project"
	const ownerEmail = "platform@localhost"
	const templateName = "web-app"

	// A project-scope template seeded in the fake clientset. The handler
	// reads this to extract the linked-templates baseline before consulting
	// the policy provider.
	tmpl := makeTemplateWithLinks("prj-"+project, templateName, nil)

	tests := []struct {
		name     string
		provider *stubTemplatePolicyStateProvider
	}{
		{
			name: "applied-read-fails",
			provider: &stubTemplatePolicyStateProvider{
				appliedErr: errors.New("transient k8s read error"),
				// current is populated so a naive diff would declare every
				// current ref as "added" against the zero-valued applied set.
				current: []*consolev1.LinkedTemplateRef{
					{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "reference-grant"},
				},
			},
		},
		{
			name: "current-resolve-fails",
			provider: &stubTemplatePolicyStateProvider{
				// applied is populated so a naive diff would declare every
				// applied ref as "removed" against the zero-valued current
				// set.
				applied: []*consolev1.LinkedTemplateRef{
					{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "reference-grant"},
				},
				currentErr: errors.New("transient resolver error"),
			},
		},
	}

	shareUsers := map[string]string{ownerEmail: "owner"}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientset(projectNS(project), tmpl)
			handler := newTestHandler(fakeClient, shareUsers).WithPolicyStateProvider(tt.provider)

			ctx := authedCtx(ownerEmail, nil)
			req := connect.NewRequest(&consolev1.GetProjectTemplatePolicyStateRequest{
				Scope: projectScopeRef(project),
				Name:  templateName,
			})
			resp, err := handler.GetProjectTemplatePolicyState(ctx, req)
			if err == nil {
				t.Fatalf("expected error when lookup fails, got state=%+v", resp.Msg.GetState())
			}
			var connectErr *connect.Error
			if !errors.As(err, &connectErr) {
				t.Fatalf("expected connect.Error, got %T: %v", err, err)
			}
			if connectErr.Code() != connect.CodeUnavailable {
				t.Errorf("expected CodeUnavailable, got %v", connectErr.Code())
			}
		})
	}
}

// TestGetProjectTemplatePolicyState_HappyPathReportsTrueDrift guards the
// positive path so the error-path fix cannot silently suppress legitimate
// drift reporting.
func TestGetProjectTemplatePolicyState_HappyPathReportsTrueDrift(t *testing.T) {
	const project = "my-project"
	const ownerEmail = "platform@localhost"
	const templateName = "web-app"
	tmpl := makeTemplateWithLinks("prj-"+project, templateName, nil)

	provider := &stubTemplatePolicyStateProvider{
		applied: []*consolev1.LinkedTemplateRef{
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "reference-grant"},
		},
		current: []*consolev1.LinkedTemplateRef{
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "fluent-bit"},
		},
	}
	fakeClient := fake.NewClientset(projectNS(project), tmpl)
	handler := newTestHandler(fakeClient, map[string]string{ownerEmail: "owner"}).WithPolicyStateProvider(provider)

	ctx := authedCtx(ownerEmail, nil)
	req := connect.NewRequest(&consolev1.GetProjectTemplatePolicyStateRequest{
		Scope: projectScopeRef(project),
		Name:  templateName,
	})
	resp, err := handler.GetProjectTemplatePolicyState(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	state := resp.Msg.GetState()
	if !state.GetDrift() {
		t.Errorf("expected drift=true for disjoint applied/current sets")
	}
	if len(state.GetAddedRefs()) != 1 || state.GetAddedRefs()[0].GetName() != "fluent-bit" {
		t.Errorf("expected added=[fluent-bit], got %+v", state.GetAddedRefs())
	}
	if len(state.GetRemovedRefs()) != 1 || state.GetRemovedRefs()[0].GetName() != "reference-grant" {
		t.Errorf("expected removed=[reference-grant], got %+v", state.GetRemovedRefs())
	}
}

// TestListTemplates_PopulatesPolicyDrift verifies that project-scope Template
// rows in a ListTemplates response carry the policy_drift bool computed from
// the same store/resolver pair the dedicated GetProjectTemplatePolicyState RPC
// uses. Covers both drift=true and drift=false paths, and guards against a
// transient lookup error fabricating drift on a listing row.
func TestListTemplates_PopulatesPolicyDrift(t *testing.T) {
	const project = "my-project"
	const ownerEmail = "platform@localhost"
	const templateName = "web-app"

	shareUsers := map[string]string{ownerEmail: "owner"}

	t.Run("drift=true when applied and current differ", func(t *testing.T) {
		tmpl := makeTemplateWithLinks("prj-"+project, templateName, nil)
		provider := &stubTemplatePolicyStateProvider{
			applied: []*consolev1.LinkedTemplateRef{
				{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "reference-grant"},
			},
			current: []*consolev1.LinkedTemplateRef{
				{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "fluent-bit"},
			},
		}
		fakeClient := fake.NewClientset(projectNS(project), tmpl)
		handler := newTestHandler(fakeClient, shareUsers).WithPolicyStateProvider(provider)

		ctx := authedCtx(ownerEmail, nil)
		resp, err := handler.ListTemplates(ctx, connect.NewRequest(&consolev1.ListTemplatesRequest{
			Scope: projectScopeRef(project),
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := len(resp.Msg.GetTemplates()); got != 1 {
			t.Fatalf("expected 1 template, got %d", got)
		}
		if !resp.Msg.GetTemplates()[0].GetPolicyDrift() {
			t.Error("expected policy_drift=true on drift row")
		}
	})

	t.Run("drift=false when applied and current match", func(t *testing.T) {
		tmpl := makeTemplateWithLinks("prj-"+project, templateName, nil)
		same := []*consolev1.LinkedTemplateRef{
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "reference-grant"},
		}
		provider := &stubTemplatePolicyStateProvider{
			applied: same,
			current: same,
		}
		fakeClient := fake.NewClientset(projectNS(project), tmpl)
		handler := newTestHandler(fakeClient, shareUsers).WithPolicyStateProvider(provider)

		ctx := authedCtx(ownerEmail, nil)
		resp, err := handler.ListTemplates(ctx, connect.NewRequest(&consolev1.ListTemplatesRequest{
			Scope: projectScopeRef(project),
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := len(resp.Msg.GetTemplates()); got != 1 {
			t.Fatalf("expected 1 template, got %d", got)
		}
		if resp.Msg.GetTemplates()[0].GetPolicyDrift() {
			t.Error("expected policy_drift=false when applied==current")
		}
	})

	t.Run("drift=false and RPC still succeeds when applied lookup fails (degrades gracefully)", func(t *testing.T) {
		tmpl := makeTemplateWithLinks("prj-"+project, templateName, nil)
		provider := &stubTemplatePolicyStateProvider{
			appliedErr: errors.New("transient k8s read error"),
			current: []*consolev1.LinkedTemplateRef{
				{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "fluent-bit"},
			},
		}
		fakeClient := fake.NewClientset(projectNS(project), tmpl)
		handler := newTestHandler(fakeClient, shareUsers).WithPolicyStateProvider(provider)

		ctx := authedCtx(ownerEmail, nil)
		resp, err := handler.ListTemplates(ctx, connect.NewRequest(&consolev1.ListTemplatesRequest{
			Scope: projectScopeRef(project),
		}))
		if err != nil {
			t.Fatalf("unexpected error from ListTemplates: %v", err)
		}
		if got := len(resp.Msg.GetTemplates()); got != 1 {
			t.Fatalf("expected 1 template, got %d", got)
		}
		if resp.Msg.GetTemplates()[0].GetPolicyDrift() {
			t.Error("expected policy_drift=false on lookup failure (must not fabricate drift)")
		}
	})
}

// TestGetTemplate_PopulatesPolicyDrift mirrors the list-view coverage for the
// single-row read path.
func TestGetTemplate_PopulatesPolicyDrift(t *testing.T) {
	const project = "my-project"
	const ownerEmail = "platform@localhost"
	const templateName = "web-app"
	shareUsers := map[string]string{ownerEmail: "owner"}

	t.Run("drift=true propagates on GetTemplate", func(t *testing.T) {
		tmpl := makeTemplateWithLinks("prj-"+project, templateName, nil)
		provider := &stubTemplatePolicyStateProvider{
			applied: []*consolev1.LinkedTemplateRef{
				{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "reference-grant"},
			},
			current: []*consolev1.LinkedTemplateRef{
				{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "fluent-bit"},
			},
		}
		fakeClient := fake.NewClientset(projectNS(project), tmpl)
		handler := newTestHandler(fakeClient, shareUsers).WithPolicyStateProvider(provider)

		ctx := authedCtx(ownerEmail, nil)
		resp, err := handler.GetTemplate(ctx, connect.NewRequest(&consolev1.GetTemplateRequest{
			Scope: projectScopeRef(project),
			Name:  templateName,
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Msg.GetTemplate().GetPolicyDrift() {
			t.Error("expected policy_drift=true on GetTemplate")
		}
	})

	t.Run("drift=false propagates on GetTemplate", func(t *testing.T) {
		tmpl := makeTemplateWithLinks("prj-"+project, templateName, nil)
		same := []*consolev1.LinkedTemplateRef{
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "reference-grant"},
		}
		provider := &stubTemplatePolicyStateProvider{applied: same, current: same}
		fakeClient := fake.NewClientset(projectNS(project), tmpl)
		handler := newTestHandler(fakeClient, shareUsers).WithPolicyStateProvider(provider)

		ctx := authedCtx(ownerEmail, nil)
		resp, err := handler.GetTemplate(ctx, connect.NewRequest(&consolev1.GetTemplateRequest{
			Scope: projectScopeRef(project),
			Name:  templateName,
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Msg.GetTemplate().GetPolicyDrift() {
			t.Error("expected policy_drift=false when applied==current")
		}
	})
}

// TestUpdateTemplate_PreservesAppliedSetWhenLinksPreserved locks in the
// HOL-557 round-2 fix: when update_linked_templates=false the preserved
// (from-storage) links must be recorded as the applied set. Previously the
// raw request payload (nil) was used, which emptied the applied set and
// fabricated drift for every previously-linked ancestor on the next read.
func TestUpdateTemplate_PreservesAppliedSetWhenLinksPreserved(t *testing.T) {
	const project = "my-project"
	const ownerEmail = "platform@localhost"
	const templateName = "web-app"

	// Seed a template with two preserved links.
	type storedRef struct {
		Scope     string `json:"scope"`
		ScopeName string `json:"scope_name"`
		Name      string `json:"name"`
	}
	preserved := []storedRef{
		{Scope: "organization", ScopeName: "acme", Name: "reference-grant"},
		{Scope: "folder", ScopeName: "payments", Name: "payments-policy"},
	}
	linkedJSON, _ := json.Marshal(preserved)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      templateName,
			Namespace: "prj-" + project,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeProject,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName:     "Web App",
				v1alpha2.AnnotationDescription:     "old",
				v1alpha2.AnnotationMandatory:       "false",
				v1alpha2.AnnotationEnabled:         "true",
				v1alpha2.AnnotationLinkedTemplates: string(linkedJSON),
			},
		},
		Data: map[string]string{
			CueTemplateKey: validCue,
		},
	}

	fakeClient := fake.NewClientset(projectNS(project), cm)
	shareUsers := map[string]string{ownerEmail: "owner"}
	// Provider returns current == preserved so the recorded set should also
	// equal preserved; any drift observed here would indicate the handler
	// passed the wrong baseline to CurrentRenderSet.
	provider := &stubTemplatePolicyStateProvider{
		current: []*consolev1.LinkedTemplateRef{
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "reference-grant"},
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, ScopeName: "payments", Name: "payments-policy"},
		},
	}
	handler := newTestHandler(fakeClient, shareUsers).WithPolicyStateProvider(provider)

	// Update metadata only; do NOT touch links.
	ctx := authedCtx(ownerEmail, nil)
	req := connect.NewRequest(&consolev1.UpdateTemplateRequest{
		Scope: projectScopeRef(project),
		Template: &consolev1.Template{
			Name:        templateName,
			Description: "new description",
			CueTemplate: validCue,
			// LinkedTemplates intentionally nil — protobuf can't distinguish
			// "not set" from "empty" and the handler must fall back to the
			// preserved links from storage.
		},
		UpdateLinkedTemplates: false,
	})
	if _, err := handler.UpdateTemplate(ctx, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if provider.recordedCalls != 1 {
		t.Fatalf("expected RecordAppliedRenderSet to be called once, got %d", provider.recordedCalls)
	}
	// Verify that CurrentRenderSet saw the preserved baseline (2 refs), not
	// the empty request payload. The handler resolves the effective set via
	// the provider, so the baseline the provider received is the right
	// invariant to assert.
	if provider.lastCurrentBaseLen != 2 {
		t.Errorf("expected CurrentRenderSet baseline len=2 (preserved from storage), got %d", provider.lastCurrentBaseLen)
	}
	names := map[string]bool{}
	for _, r := range provider.lastCurrentBase {
		names[r.GetName()] = true
	}
	if !names["reference-grant"] || !names["payments-policy"] {
		t.Errorf("expected baseline to contain reference-grant and payments-policy, got %v", names)
	}

	// Sanity check: a subsequent ListTemplates should report drift=false
	// because the recorded applied set now matches the current set.
	provider.applied = provider.current
	listResp, listErr := handler.ListTemplates(ctx, connect.NewRequest(&consolev1.ListTemplatesRequest{
		Scope: projectScopeRef(project),
	}))
	if listErr != nil {
		t.Fatalf("unexpected error from ListTemplates: %v", listErr)
	}
	if got := len(listResp.Msg.GetTemplates()); got != 1 {
		t.Fatalf("expected 1 template, got %d", got)
	}
	if listResp.Msg.GetTemplates()[0].GetPolicyDrift() {
		t.Error("expected policy_drift=false after update that preserved links — fabricated drift indicates the applied set was overwritten with an empty baseline")
	}
}
