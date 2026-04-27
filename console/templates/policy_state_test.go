package templates

import (
	"context"

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

func (s *stubProjectTemplateDriftChecker) PolicyState(_ context.Context, _, _ string) (*consolev1.PolicyState, error) {
	return s.stateResult, s.stateErr
}

func (s *stubProjectTemplateDriftChecker) RecordApplied(_ context.Context, project, name string, refs []*consolev1.LinkedTemplateRef) error {
	s.recordCalls++
	s.lastRecordProject = project
	s.lastRecordName = name
	s.lastRecordRefs = refs
	return s.recordErr
}
