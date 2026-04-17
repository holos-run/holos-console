// Package policyresolver defines the TemplatePolicy resolution seam used by
// every render path in the console — deployments (create/update/preview),
// project-scope templates (create/update/preview), and project creation
// (REQUIRE-matched templates).
//
// The package exists so Phase 5 of HOL-562 (HOL-567) can swap the no-op
// implementation for a real TemplatePolicy-backed resolver without touching
// call sites. In Phase 4 (HOL-566) the interface is introduced and wired
// everywhere, but every call path still receives the no-op implementation
// that returns explicit refs unchanged.
//
// Keeping the resolver in its own package (rather than in
// console/templates/) prevents the PolicyResolver abstraction from leaking
// into the CUE renderer and related apply/preview machinery. The renderer
// operates on CUE sources; resolution decisions belong to the caller.
package policyresolver

import (
	"context"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// TargetKind identifies the kind of render target driving a call to the
// resolver. Phase 4 wires the value through every call site; Phase 5 uses it
// to key REQUIRE/EXCLUDE evaluation so the resolver can apply different
// policies to deployment renders vs project-scope template previews.
type TargetKind int

const (
	// TargetKindProjectTemplate is the preview render path for project-scope
	// templates (the RenderTemplate RPC and the project-scope Create/Update
	// template handlers).
	TargetKindProjectTemplate TargetKind = iota
	// TargetKindDeployment is the apply render path for deployments
	// (AncestorTemplateProvider on the deployments handler).
	TargetKindDeployment
)

// PolicyResolver filters and augments the caller's explicit linked-template
// refs according to TemplatePolicy REQUIRE/EXCLUDE rules before the
// ancestor-source helper walks the namespace chain.
//
// Phase 4 (HOL-566) introduces this contract; every production wire-up
// receives a no-op implementation that returns explicitRefs unchanged. Phase
// 5 (HOL-567) swaps in a real implementation backed by the TemplatePolicy
// service.
//
// Implementations must not mutate the input slice. The returned slice is owned
// by the caller and may be appended to freely.
type PolicyResolver interface {
	Resolve(
		ctx context.Context,
		projectNs string,
		targetKind TargetKind,
		targetName string,
		explicitRefs []*consolev1.LinkedTemplateRef,
	) ([]*consolev1.LinkedTemplateRef, error)
}

// noopResolver returns explicitRefs unchanged. It is the Phase 4 placeholder
// that every render call site receives until Phase 5 lands a real
// TemplatePolicy-backed implementation.
type noopResolver struct{}

// NewNoopResolver returns a PolicyResolver that returns its inputs unchanged.
// Wire one instance at server startup and pass it into every handler that
// owns a render path.
func NewNoopResolver() PolicyResolver {
	return noopResolver{}
}

// Resolve returns explicitRefs verbatim. The context, projectNs, targetKind,
// and targetName arguments are accepted for signature stability — Phase 5
// consults them, but the no-op implementation never does.
func (noopResolver) Resolve(
	_ context.Context,
	_ string,
	_ TargetKind,
	_ string,
	explicitRefs []*consolev1.LinkedTemplateRef,
) ([]*consolev1.LinkedTemplateRef, error) {
	return explicitRefs, nil
}
