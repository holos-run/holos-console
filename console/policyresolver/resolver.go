// Package policyresolver defines the TemplatePolicy resolution seam used by
// every render path in the console — deployments (create/update/preview),
// project-scope templates (create/update/preview), and project creation
// (REQUIRE-matched templates).
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

// PolicyResolver computes the effective set of LinkedTemplateRef values for a
// render target by applying TemplatePolicy REQUIRE/EXCLUDE rules from the
// ancestor namespace chain. The effective set formula is:
//
//	result = REQUIRE-injected − EXCLUDE-removed
//
// Only bindings whose target_refs select the current render target contribute.
// Policies with no covering binding contribute nothing. The effective set is
// derived purely from TemplatePolicyBinding rules.
//
// Implementations must not mutate any shared state. The returned slice is
// owned by the caller and may be appended to freely.
type PolicyResolver interface {
	Resolve(
		ctx context.Context,
		projectNs string,
		targetKind TargetKind,
		targetName string,
	) ([]*consolev1.LinkedTemplateRef, error)
}

// noopResolver returns an empty effective set. It is the placeholder wired
// when no real TemplatePolicy-backed implementation is available (e.g.,
// local/dev deployments without a policy resolver).
type noopResolver struct{}

// NewNoopResolver returns a PolicyResolver that always returns an empty
// effective set. Wire one instance at server startup for local/dev wiring;
// production wires the real folderResolver via NewFolderResolverWithBindings.
func NewNoopResolver() PolicyResolver {
	return noopResolver{}
}

// Resolve returns an empty slice. The context, projectNs, targetKind, and
// targetName arguments are accepted for interface compliance — the no-op
// implementation never consults them.
func (noopResolver) Resolve(
	_ context.Context,
	_ string,
	_ TargetKind,
	_ string,
) ([]*consolev1.LinkedTemplateRef, error) {
	return nil, nil
}
