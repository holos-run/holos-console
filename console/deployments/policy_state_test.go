package deployments

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// stubPolicyDriftChecker is a test double for PolicyDriftChecker that lets a
// test control each of the three methods independently. Records the last
// arguments so drift call-site assertions can pin the (project, name)
// forwarding without stubbing every method separately.
type stubPolicyDriftChecker struct {
	driftResult bool
	driftHasApp bool
	driftErr    error

	stateResult *consolev1.PolicyState
	stateErr    error

	recordErr error

	// Capture fields for the write-through acceptance tests (HOL-569).
	recordCalls       int
	lastRecordProject string
	lastRecordName    string
	lastRecordRefs    []*consolev1.LinkedTemplateRef
}

func (s *stubPolicyDriftChecker) Drift(_ context.Context, _, _ string) (bool, bool, error) {
	return s.driftResult, s.driftHasApp, s.driftErr
}

func (s *stubPolicyDriftChecker) PolicyState(_ context.Context, _, _ string) (*consolev1.PolicyState, error) {
	return s.stateResult, s.stateErr
}

func (s *stubPolicyDriftChecker) RecordApplied(_ context.Context, project, name string, refs []*consolev1.LinkedTemplateRef) error {
	s.recordCalls++
	s.lastRecordProject = project
	s.lastRecordName = name
	s.lastRecordRefs = refs
	return s.recordErr
}

// deploymentCMForPolicy builds a minimal deployment ConfigMap for policy-state
// tests — only the namespace, name, and the v1alpha2 linked-templates
// annotation matter for the RPC under test.
func deploymentCMForPolicy(project, name string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "prj-" + project,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeDeployment,
			},
		},
	}
}

// TestGetDeploymentPolicyState covers the handler validation branches, the
// unauthenticated/unauthorized branches, the no-checker-wired branch, and the
// happy-path response shape. This mirrors the table style used by
// TestGetDeploymentStatusSummary.
func TestGetDeploymentPolicyState(t *testing.T) {
	const (
		project = "my-project"
		name    = "web-app"
	)

	happyState := &consolev1.PolicyState{
		HasAppliedState: true,
		Drift:           true,
		AppliedSet: []*consolev1.LinkedTemplateRef{
			&consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "httproute"},
		},
		CurrentSet: []*consolev1.LinkedTemplateRef{
			&consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "httproute"},
			&consolev1.LinkedTemplateRef{Namespace: "holos-fld-eng", Name: "audit"},
		},
		AddedRefs: []*consolev1.LinkedTemplateRef{
			&consolev1.LinkedTemplateRef{Namespace: "holos-fld-eng", Name: "audit"},
		},
	}

	type tc struct {
		desc       string
		ctx        context.Context
		req        *consolev1.GetDeploymentPolicyStateRequest
		checker    PolicyDriftChecker
		seedDepCM  bool
		wantCode   connect.Code // zero means success
		wantEmpty  bool         // expect empty PolicyState (no checker wired)
		wantDrift  bool
		wantHasApp bool
	}

	cases := []tc{
		{
			desc:     "empty project is rejected",
			ctx:      authedCtx("viewer@example.com", nil),
			req:      &consolev1.GetDeploymentPolicyStateRequest{Project: "", Name: name},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			desc:     "empty name is rejected",
			ctx:      authedCtx("viewer@example.com", nil),
			req:      &consolev1.GetDeploymentPolicyStateRequest{Project: project, Name: ""},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			desc:     "unauthenticated is rejected",
			ctx:      context.Background(),
			req:      &consolev1.GetDeploymentPolicyStateRequest{Project: project, Name: name},
			wantCode: connect.CodeUnauthenticated,
		},
		{
			desc:     "caller without project grant is denied",
			ctx:      authedCtx("nobody@example.com", nil),
			req:      &consolev1.GetDeploymentPolicyStateRequest{Project: project, Name: name},
			wantCode: connect.CodePermissionDenied,
		},
		{
			desc:      "missing deployment ConfigMap returns NotFound",
			ctx:       authedCtx("viewer@example.com", nil),
			req:       &consolev1.GetDeploymentPolicyStateRequest{Project: project, Name: name},
			seedDepCM: false,
			wantCode:  connect.CodeNotFound,
		},
		{
			desc:      "no checker wired returns empty PolicyState",
			ctx:       authedCtx("viewer@example.com", nil),
			req:       &consolev1.GetDeploymentPolicyStateRequest{Project: project, Name: name},
			seedDepCM: true,
			checker:   nil,
			wantEmpty: true,
		},
		{
			desc:      "checker error surfaces as Internal",
			ctx:       authedCtx("viewer@example.com", nil),
			req:       &consolev1.GetDeploymentPolicyStateRequest{Project: project, Name: name},
			seedDepCM: true,
			checker:   &stubPolicyDriftChecker{stateErr: errors.New("resolver down")},
			wantCode:  connect.CodeInternal,
		},
		{
			desc:       "happy path returns the checker's PolicyState verbatim",
			ctx:        authedCtx("viewer@example.com", nil),
			req:        &consolev1.GetDeploymentPolicyStateRequest{Project: project, Name: name},
			seedDepCM:  true,
			checker:    &stubPolicyDriftChecker{stateResult: happyState},
			wantDrift:  true,
			wantHasApp: true,
		},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			var objs []runtime.Object
			if c.seedDepCM {
				objs = append(objs, deploymentCMForPolicy(project, name))
			}
			fakeClient := fake.NewClientset(objs...)
			h := newStatusHandler(fakeClient)
			if c.checker != nil {
				h = h.WithPolicyDriftChecker(c.checker)
			}

			resp, err := h.GetDeploymentPolicyState(c.ctx, connect.NewRequest(c.req))
			if c.wantCode != 0 {
				if err == nil {
					t.Fatalf("expected error with code %v, got nil", c.wantCode)
				}
				if connect.CodeOf(err) != c.wantCode {
					t.Errorf("code: got %v, want %v", connect.CodeOf(err), c.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := resp.Msg.GetState()
			if got == nil {
				t.Fatal("State: got nil, want non-nil")
			}
			if c.wantEmpty {
				if got.HasAppliedState || got.Drift || len(got.AppliedSet) != 0 || len(got.CurrentSet) != 0 {
					t.Errorf("expected empty PolicyState, got %+v", got)
				}
				return
			}
			if got.Drift != c.wantDrift {
				t.Errorf("drift: got %v, want %v", got.Drift, c.wantDrift)
			}
			if got.HasAppliedState != c.wantHasApp {
				t.Errorf("has_applied_state: got %v, want %v", got.HasAppliedState, c.wantHasApp)
			}
		})
	}
}

// TestApplyPolicyDrift covers the three code paths in applyPolicyDrift: no
// checker (no-op), checker error (silently swallowed — drift is advisory),
// and the happy-path merge into DeploymentStatusSummary.PolicyDrift.
func TestApplyPolicyDrift(t *testing.T) {
	const (
		project = "my-project"
		name    = "web-app"
	)

	type tc struct {
		desc        string
		checker     PolicyDriftChecker
		summary     *consolev1.DeploymentStatusSummary
		wantNilSet  bool  // PolicyDrift should remain nil (no-op or nil summary)
		wantDriftOK *bool // if set, PolicyDrift must be non-nil and equal
	}
	tTrue := true
	tFalse := false
	cases := []tc{
		{
			desc:       "nil summary is no-op (defensive)",
			checker:    &stubPolicyDriftChecker{driftHasApp: true},
			summary:    nil,
			wantNilSet: true,
		},
		{
			desc:       "no checker wired leaves summary.policy_drift unset",
			checker:    nil,
			summary:    &consolev1.DeploymentStatusSummary{},
			wantNilSet: true,
		},
		{
			desc:       "checker error is silently swallowed",
			checker:    &stubPolicyDriftChecker{driftErr: errors.New("resolver down")},
			summary:    &consolev1.DeploymentStatusSummary{},
			wantNilSet: true,
		},
		{
			desc:       "no applied state leaves policy_drift unset",
			checker:    &stubPolicyDriftChecker{driftResult: true, driftHasApp: false},
			summary:    &consolev1.DeploymentStatusSummary{},
			wantNilSet: true,
		},
		{
			desc:        "has applied state and no drift populates policy_drift=false",
			checker:     &stubPolicyDriftChecker{driftResult: false, driftHasApp: true},
			summary:     &consolev1.DeploymentStatusSummary{},
			wantDriftOK: &tFalse,
		},
		{
			desc:        "has applied state and drift populates policy_drift=true",
			checker:     &stubPolicyDriftChecker{driftResult: true, driftHasApp: true},
			summary:     &consolev1.DeploymentStatusSummary{},
			wantDriftOK: &tTrue,
		},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			fakeClient := fake.NewClientset()
			h := newStatusHandler(fakeClient)
			if c.checker != nil {
				h = h.WithPolicyDriftChecker(c.checker)
			}
			h.applyPolicyDrift(context.Background(), project, name, c.summary)
			if c.summary == nil {
				return // nothing to assert
			}
			if c.wantNilSet {
				if c.summary.PolicyDrift != nil {
					t.Errorf("policy_drift: got %v, want nil", *c.summary.PolicyDrift)
				}
				return
			}
			if c.summary.PolicyDrift == nil {
				t.Fatal("policy_drift: got nil, want non-nil")
			}
			if *c.summary.PolicyDrift != *c.wantDriftOK {
				t.Errorf("policy_drift: got %v, want %v", *c.summary.PolicyDrift, *c.wantDriftOK)
			}
		})
	}
}
