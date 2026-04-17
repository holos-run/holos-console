package templates

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"k8s.io/client-go/kubernetes/fake"

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
}

func (s *stubTemplatePolicyStateProvider) CurrentRenderSet(_ context.Context, _, _ string, _ []*consolev1.LinkedTemplateRef) ([]*consolev1.LinkedTemplateRef, error) {
	return s.current, s.currentErr
}

func (s *stubTemplatePolicyStateProvider) AppliedRenderSet(_ context.Context, _, _ string) ([]*consolev1.LinkedTemplateRef, error) {
	return s.applied, s.appliedErr
}

func (s *stubTemplatePolicyStateProvider) RecordAppliedRenderSet(_ context.Context, _, _ string, _ []*consolev1.LinkedTemplateRef) error {
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
