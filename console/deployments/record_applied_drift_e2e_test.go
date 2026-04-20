package deployments

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"k8s.io/client-go/kubernetes/fake"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// e2eDriftChecker is an in-memory combined PolicyDriftChecker that shares a
// single applied-render-set map across RecordApplied and PolicyState. It
// simulates the DriftChecker adapter in console/policyresolver without
// standing up a full ancestor-walker fixture — all we need for this test
// is to prove that:
//
//  1. CreateDeployment invokes RecordApplied with the effective ref set,
//  2. GetDeploymentPolicyState then sees has_applied_state=true,
//  3. and drift is false because the current set (from the resolver) equals
//     the applied set.
//
// The fixture mirrors the behavior shape of
// console/policyresolver/applied_state_test.go:
// RecordAppliedRenderSet + ReadAppliedRenderSet + DiffRenderSets, so the
// end-to-end proof here tracks the same wire contract as the real adapter
// without the cross-package cycle.
type e2eDriftChecker struct {
	// applied[project+"/"+name] is the set previously recorded via
	// RecordApplied. Absence means the target has never been applied.
	applied map[string][]*consolev1.LinkedTemplateRef
	// currentFn returns the resolver output for a given (project, name).
	// Production wires a real resolver; tests supply a closure that mimics
	// "policy forces a REQUIRE template" without standing up a policy CRD.
	currentFn func(project, name string, explicitRefs []*consolev1.LinkedTemplateRef) []*consolev1.LinkedTemplateRef
}

func newE2EDriftChecker(currentFn func(string, string, []*consolev1.LinkedTemplateRef) []*consolev1.LinkedTemplateRef) *e2eDriftChecker {
	return &e2eDriftChecker{
		applied:   map[string][]*consolev1.LinkedTemplateRef{},
		currentFn: currentFn,
	}
}

func (e *e2eDriftChecker) key(project, name string) string { return project + "/" + name }

func (e *e2eDriftChecker) Drift(_ context.Context, project, name string, explicitRefs []*consolev1.LinkedTemplateRef) (bool, bool, error) {
	applied, ok := e.applied[e.key(project, name)]
	if !ok {
		return false, false, nil
	}
	current := e.currentFn(project, name, explicitRefs)
	return diffRefs(applied, current), true, nil
}

func (e *e2eDriftChecker) PolicyState(_ context.Context, project, name string, explicitRefs []*consolev1.LinkedTemplateRef) (*consolev1.PolicyState, error) {
	applied, has := e.applied[e.key(project, name)]
	current := e.currentFn(project, name, explicitRefs)
	return &consolev1.PolicyState{
		AppliedSet:      applied,
		CurrentSet:      current,
		Drift:           diffRefs(applied, current),
		HasAppliedState: has,
	}, nil
}

func (e *e2eDriftChecker) RecordApplied(_ context.Context, project, name string, refs []*consolev1.LinkedTemplateRef) error {
	e.applied[e.key(project, name)] = refs
	return nil
}

// diffRefs is a local, compact equivalent of
// policyresolver.DiffRenderSets — true means the two slices differ by
// (namespace, name, version_constraint). Kept in-package so the
// end-to-end test does not pull in the full policyresolver fixture.
func diffRefs(applied, current []*consolev1.LinkedTemplateRef) bool {
	type k struct {
		ns string
		n  string
		vc string
	}
	as := make(map[k]struct{}, len(applied))
	for _, r := range applied {
		if r == nil {
			continue
		}
		as[k{r.GetNamespace(), r.GetName(), r.GetVersionConstraint()}] = struct{}{}
	}
	cs := make(map[k]struct{}, len(current))
	for _, r := range current {
		if r == nil {
			continue
		}
		cs[k{r.GetNamespace(), r.GetName(), r.GetVersionConstraint()}] = struct{}{}
	}
	if len(as) != len(cs) {
		return true
	}
	for key := range as {
		if _, ok := cs[key]; !ok {
			return true
		}
	}
	return false
}

// TestHandler_CreateDeployment_DriftBecomesFalseAfterRecord is the ticket's
// end-to-end acceptance test: seed a fake policy that forces a REQUIRE
// template, call CreateDeployment, then call GetDeploymentPolicyState, and
// assert has_applied_state=true and drift=false.
//
// The fake policy is modeled by the e2eDriftChecker's currentFn: it
// deterministically prepends a folder-scoped REQUIRE template to the
// resolver output. CreateDeployment receives the same REQUIRE-expanded set
// via the AncestorTemplateProvider and writes it through to the checker
// via RecordApplied. When GetDeploymentPolicyState then computes the
// current set from the same fake policy, the diff is empty and drift is
// false — proving the write-through wires the effective set (not the raw
// explicit list) and that the two surfaces agree.
func TestHandler_CreateDeployment_DriftBecomesFalseAfterRecord(t *testing.T) {
	// Policy output: this is the "what the fake policy decides the render
	// set should be" value. Both the AncestorTemplateProvider (applied
	// path) and the drift checker's currentFn (query path) return this
	// same value so applied == current and drift is false when recorded.
	policyOutput := []*consolev1.LinkedTemplateRef{
		&consolev1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "httproute"},
		&consolev1.LinkedTemplateRef{Namespace: "holos-fld-payments", Name: "audit"},
	}

	// The AncestorTemplateProvider stub returns the policy-resolved set as
	// the effective refs. This mirrors the production contract: the real
	// AncestorTemplateResolver delegates to K8sClient.ListEffectiveTemplateSources
	// which calls the PolicyResolver internally and surfaces the resolved
	// set alongside sources. The folder "audit" ref here represents a
	// REQUIRE-injected template — the caller did not explicitly link it,
	// but policy resolution added it before the render.
	atp := &stubAncestorTemplateProvider{
		sources:       []string{"// folder audit template"},
		effectiveRefs: policyOutput,
	}

	checker := newE2EDriftChecker(func(_, _ string, _ []*consolev1.LinkedTemplateRef) []*consolev1.LinkedTemplateRef {
		// Return the same policy output regardless of the handler-supplied
		// explicit refs. In production the PolicyResolver consults the
		// TemplatePolicy CRD the same way on every call, so the apply-time
		// and query-time resolutions agree by construction.
		return policyOutput
	})

	// Seed an existing deployment config map so GetDeploymentPolicyState
	// can read the target record. CreateDeployment will overwrite this
	// with its own ConfigMap.
	fakeClient := fake.NewClientset(projectNS("my-project"))
	pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "editor"}}
	k8s := NewK8sClient(fakeClient, testResolver())
	// Wire a TemplateResolver that surfaces the explicit linked list as
	// the annotation on the deployment template ConfigMap. The handler
	// uses this when resolving the deployment's linked-templates
	// annotation on GetDeploymentPolicyState.
	templateResolver := &stubTemplateResolver{cm: fakeTemplate("default")}

	h := NewHandler(k8s, pr, &stubSettingsResolver{settings: enabledSettings()}, templateResolver, &stubRenderer{}, &stubApplier{}).
		WithAncestorTemplateProvider(atp).
		WithPolicyDriftChecker(checker)

	// Step 1: CreateDeployment on the happy path records the applied set.
	createReq := connect.NewRequest(&consolev1.CreateDeploymentRequest{
		Project:  "my-project",
		Name:     "web-app",
		Image:    "nginx",
		Tag:      "1.25",
		Template: "default",
	})
	if _, err := h.CreateDeployment(authedCtx("alice@example.com", nil), createReq); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}

	// Step 2: GetDeploymentPolicyState observes the applied set and
	// computes drift against the same policy. has_applied_state must
	// flip to true and drift must stay false because the applied set
	// equals the resolver output.
	getReq := connect.NewRequest(&consolev1.GetDeploymentPolicyStateRequest{
		Project: "my-project",
		Name:    "web-app",
	})
	// The RBAC check uses viewer as the read role; add alice as viewer.
	pr.users["alice@example.com"] = "viewer"
	resp, err := h.GetDeploymentPolicyState(authedCtx("alice@example.com", nil), getReq)
	if err != nil {
		t.Fatalf("GetDeploymentPolicyState: %v", err)
	}
	state := resp.Msg.GetState()
	if state == nil {
		t.Fatal("GetDeploymentPolicyState returned nil state")
	}
	if !state.HasAppliedState {
		t.Errorf("has_applied_state: got false after CreateDeployment wrote through; want true")
	}
	if state.Drift {
		t.Errorf("drift: got true; want false (applied set should equal current set)")
	}
	// Sanity: both sides carry the same 2 refs.
	if len(state.AppliedSet) != 2 || len(state.CurrentSet) != 2 {
		t.Errorf("applied/current cardinality: applied=%d current=%d, want 2/2",
			len(state.AppliedSet), len(state.CurrentSet))
	}
}
