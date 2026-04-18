package deployments

import (
	"context"
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

// EvaluateGroupedCUE compiles and evaluates a pre-concatenated CUE source
// document (template + any already-embedded raw CUE input) and returns the
// rendered Kubernetes resources grouped by origin. This is the raw-CUE entry
// point used by callers that assemble the full CUE document themselves —
// for example, the templates preview path which receives CUE strings for
// "platform" and "input" from the client.
//
// When readPlatformResources is true the renderer reads both
// platformResources and projectResources; when false only projectResources is
// read (per ADR 016 Decision 8 the project-level path must not emit
// platformResources).
//
// This helper is a transitional raw-CUE entry point kept out of render.go so
// that render.go exposes exactly one public render function
// (CueRenderer.Render). The raw-CUE call sites in the templates package will
// be removed in later phases (see HOL-562); when the last caller is gone,
// this file and the helper it exports will be deleted.
func EvaluateGroupedCUE(ctx context.Context, combinedCUESource string, readPlatformResources bool) (*GroupedResources, error) {
	evalCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	type result struct {
		grouped *GroupedResources
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		grouped, err := evaluateCueInput(combinedCUESource, readPlatformResources)
		ch <- result{grouped, err}
	}()

	select {
	case <-evalCtx.Done():
		return nil, fmt.Errorf("CUE template evaluation timed out after %s", renderTimeout)
	case res := <-ch:
		return res.grouped, res.err
	}
}

// evaluateCueInput performs synchronous CUE template evaluation of a
// pre-concatenated source document. The caller is responsible for assembling
// the full CUE document (template plus any raw-CUE "platform" / "input"
// values). Generated schema definitions are prepended so templates can
// reference #PlatformInput, #ProjectInput, etc.
//
// At least one of projectResources.namespacedResources or
// platformResources.namespacedResources must exist (preview of a
// platform-only template is permitted).
func evaluateCueInput(cueSource string, readPlatformResources bool) (*GroupedResources, error) {
	cueCtx := cuecontext.New()

	combined := v1alpha2.GeneratedSchema + "\n" + cueSource
	unified := cueCtx.CompileString(combined)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("invalid CUE template: %w", err)
	}

	namespacedValue := unified.LookupPath(cue.ParsePath("projectResources.namespacedResources"))
	platformNamespacedValue := unified.LookupPath(cue.ParsePath("platformResources.namespacedResources"))
	if (namespacedValue.Err() != nil || !namespacedValue.Exists()) &&
		(platformNamespacedValue.Err() != nil || !platformNamespacedValue.Exists()) {
		return nil, fmt.Errorf("template must define 'projectResources.namespacedResources' or 'platformResources.namespacedResources' (structured output format required)")
	}

	return evaluateStructuredGrouped(unified, readPlatformResources)
}
