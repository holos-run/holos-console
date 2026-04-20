package templates

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// stubProjectTemplateDriftChecker is a test double for the
// ProjectTemplateDriftChecker interface used by the templates handler.
type stubProjectTemplateDriftChecker struct {
	stateResult *consolev1.PolicyState
	stateErr    error
	recordErr   error

	// Capture fields for the HOL-569 write-through acceptance tests.
	recordCalls       int
	lastRecordProject string
	lastRecordName    string
	lastRecordRefs    []*consolev1.LinkedTemplateRef
}

func (s *stubProjectTemplateDriftChecker) PolicyState(_ context.Context, _, _ string, _ []*consolev1.LinkedTemplateRef) (*consolev1.PolicyState, error) {
	return s.stateResult, s.stateErr
}

func (s *stubProjectTemplateDriftChecker) RecordApplied(_ context.Context, project, name string, refs []*consolev1.LinkedTemplateRef) error {
	s.recordCalls++
	s.lastRecordProject = project
	s.lastRecordName = name
	s.lastRecordRefs = refs
	return s.recordErr
}

// TestGetProjectTemplatePolicyState covers validation, RBAC enforcement (the
// HOL-567 Round-1 [CRITICAL] regression — an authenticated caller without
// PermissionTemplatesRead on the owning project MUST be denied), the no-
// checker-wired branch, and the happy-path response shape.
func TestGetProjectTemplatePolicyState(t *testing.T) {
	const (
		project    = "my-project"
		ownerEmail = "owner@localhost"
		otherEmail = "nobody@localhost"
		tmplName   = "web-app"
	)

	happyState := &consolev1.PolicyState{
		HasAppliedState: true,
		Drift:           false,
		AppliedSet: []*consolev1.LinkedTemplateRef{
			&consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "httproute"},
		},
		CurrentSet: []*consolev1.LinkedTemplateRef{
			&consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "httproute"},
		},
	}

	type tc struct {
		desc         string
		ctx          context.Context
		req          *consolev1.GetProjectTemplatePolicyStateRequest
		seedTemplate bool
		checker      ProjectTemplateDriftChecker
		wantCode     connect.Code // zero means success
		wantEmpty    bool
	}

	ownerCtx := authedCtx(ownerEmail, nil)
	otherCtx := authedCtx(otherEmail, nil)

	cases := []tc{
		{
			desc:     "empty namespace is rejected",
			ctx:      ownerCtx,
			req:      &consolev1.GetProjectTemplatePolicyStateRequest{Name: tmplName},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			desc: "non-project namespace is rejected",
			ctx:  ownerCtx,
			req: &consolev1.GetProjectTemplatePolicyStateRequest{
				Namespace: testResolver.OrgNamespace("acme"),
				Name:      tmplName,
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			desc: "empty template name is rejected",
			ctx:  ownerCtx,
			req: &consolev1.GetProjectTemplatePolicyStateRequest{
				Namespace: projectScopeRef(project),
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			desc: "unauthenticated is rejected",
			ctx:  context.Background(),
			req: &consolev1.GetProjectTemplatePolicyStateRequest{
				Namespace: projectScopeRef(project),
				Name:  tmplName,
			},
			wantCode: connect.CodeUnauthenticated,
		},
		{
			// HOL-567 Round-1 [CRITICAL]: an authenticated caller without
			// PermissionTemplatesRead on the owning project must NOT be able
			// to read the template's policy state. This test is the
			// regression guard for that fix.
			desc: "caller without project read grant is denied (RBAC regression guard)",
			ctx:  otherCtx,
			req: &consolev1.GetProjectTemplatePolicyStateRequest{
				Namespace: projectScopeRef(project),
				Name:  tmplName,
			},
			seedTemplate: true,
			wantCode:     connect.CodePermissionDenied,
		},
		{
			desc: "owner with no template present returns NotFound",
			ctx:  ownerCtx,
			req: &consolev1.GetProjectTemplatePolicyStateRequest{
				Namespace: projectScopeRef(project),
				Name:  tmplName,
			},
			seedTemplate: false,
			wantCode:     connect.CodeNotFound,
		},
		{
			desc: "no checker wired returns empty PolicyState",
			ctx:  ownerCtx,
			req: &consolev1.GetProjectTemplatePolicyStateRequest{
				Namespace: projectScopeRef(project),
				Name:  tmplName,
			},
			seedTemplate: true,
			checker:      nil,
			wantEmpty:    true,
		},
		{
			desc: "checker error surfaces as Internal",
			ctx:  ownerCtx,
			req: &consolev1.GetProjectTemplatePolicyStateRequest{
				Namespace: projectScopeRef(project),
				Name:  tmplName,
			},
			seedTemplate: true,
			checker:      &stubProjectTemplateDriftChecker{stateErr: errors.New("resolver down")},
			wantCode:     connect.CodeInternal,
		},
		{
			desc: "happy path returns the checker's PolicyState verbatim",
			ctx:  ownerCtx,
			req: &consolev1.GetProjectTemplatePolicyStateRequest{
				Namespace: projectScopeRef(project),
				Name:  tmplName,
			},
			seedTemplate: true,
			checker:      &stubProjectTemplateDriftChecker{stateResult: happyState},
		},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			objs := []runtime.Object{projectNS(project)}
			if c.seedTemplate {
				objs = append(objs, projectTemplateConfigMap(project, tmplName, "Web App", "desc", "#Input: {}\n"))
			}
			fakeClient := fake.NewClientset(objs...)

			// ownerEmail gets "owner"; no one else is in the grants map so
			// their project-read check fails closed.
			h := newTestHandler(t, fakeClient, map[string]string{ownerEmail: "owner"})
			if c.checker != nil {
				h = h.WithProjectTemplateDriftChecker(c.checker)
			}

			resp, err := h.GetProjectTemplatePolicyState(c.ctx, connect.NewRequest(c.req))
			if c.wantCode != 0 {
				if err == nil {
					t.Fatalf("expected error with code %v, got nil", c.wantCode)
				}
				if connect.CodeOf(err) != c.wantCode {
					t.Errorf("code: got %v, want %v (err=%v)", connect.CodeOf(err), c.wantCode, err)
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
			if got.HasAppliedState != happyState.HasAppliedState {
				t.Errorf("has_applied_state: got %v, want %v", got.HasAppliedState, happyState.HasAppliedState)
			}
			if len(got.AppliedSet) != 1 || got.AppliedSet[0].GetName() != "httproute" {
				t.Errorf("applied_set: got %+v, want httproute", got.AppliedSet)
			}
		})
	}
}
