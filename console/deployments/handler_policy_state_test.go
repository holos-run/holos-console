package deployments

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"k8s.io/client-go/kubernetes/fake"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// stubPolicyStateProvider implements PolicyStateProvider for tests. Each field
// is consulted independently so a test can simulate a failure in exactly one
// of the two lookup paths the handler depends on.
type stubPolicyStateProvider struct {
	applied       []*consolev1.LinkedTemplateRef
	appliedErr    error
	current       []*consolev1.LinkedTemplateRef
	currentErr    error
	recordErr     error
	recordedRefs  []*consolev1.LinkedTemplateRef
	recordedCalls int
}

func (s *stubPolicyStateProvider) CurrentRenderSet(_ context.Context, _, _ string, _ []*consolev1.LinkedTemplateRef) ([]*consolev1.LinkedTemplateRef, error) {
	return s.current, s.currentErr
}

func (s *stubPolicyStateProvider) AppliedRenderSet(_ context.Context, _, _ string) ([]*consolev1.LinkedTemplateRef, error) {
	return s.applied, s.appliedErr
}

func (s *stubPolicyStateProvider) RecordAppliedRenderSet(_ context.Context, _, _ string, refs []*consolev1.LinkedTemplateRef) error {
	s.recordedCalls++
	s.recordedRefs = refs
	return s.recordErr
}

// TestGetDeploymentPolicyState_DoesNotFabricateDriftOnLookupError locks in
// the HOL-557 round-1 review fix: when either AppliedRenderSet or
// CurrentRenderSet fails, the handler must NOT synthesize a drift
// comparison from zero values. Instead the RPC returns a Connect error so
// the caller can distinguish "partial data" from "genuine drift."
func TestGetDeploymentPolicyState_DoesNotFabricateDriftOnLookupError(t *testing.T) {
	ns := projectNS("my-project")
	cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "desc")

	tests := []struct {
		name     string
		provider *stubPolicyStateProvider
	}{
		{
			name: "applied-read-fails",
			provider: &stubPolicyStateProvider{
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
			provider: &stubPolicyStateProvider{
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

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientset(ns, cm)
			pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
			handler := defaultHandler(fakeClient, pr).WithPolicyStateProvider(tt.provider)

			ctx := authedCtx("alice@example.com", nil)
			req := connect.NewRequest(&consolev1.GetDeploymentPolicyStateRequest{
				Project: "my-project",
				Name:    "web-app",
			})
			resp, err := handler.GetDeploymentPolicyState(ctx, req)
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

// TestGetDeploymentPolicyState_HappyPathReportsTrueDrift exercises the
// positive case to guarantee the error-path fix did not accidentally
// suppress legitimate drift reporting.
func TestGetDeploymentPolicyState_HappyPathReportsTrueDrift(t *testing.T) {
	ns := projectNS("my-project")
	cm := deploymentConfigMap("my-project", "web-app", "nginx", "latest", "default", "Web App", "desc")
	fakeClient := fake.NewClientset(ns, cm)
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}

	provider := &stubPolicyStateProvider{
		applied: []*consolev1.LinkedTemplateRef{
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "reference-grant"},
		},
		current: []*consolev1.LinkedTemplateRef{
			{Scope: consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, ScopeName: "acme", Name: "fluent-bit"},
		},
	}
	handler := defaultHandler(fakeClient, pr).WithPolicyStateProvider(provider)

	ctx := authedCtx("alice@example.com", nil)
	req := connect.NewRequest(&consolev1.GetDeploymentPolicyStateRequest{
		Project: "my-project",
		Name:    "web-app",
	})
	resp, err := handler.GetDeploymentPolicyState(ctx, req)
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
