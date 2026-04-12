package deployments

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

// allowedKindSet is the set of resource kinds that CUE templates may produce.
var allowedKindSet = map[string]bool{
	"Deployment":     true,
	"Service":        true,
	"ServiceAccount": true,
	"Role":           true,
	"RoleBinding":    true,
	"HTTPRoute":      true,
	"ReferenceGrant": true,
	"ConfigMap":      true,
	"Secret":         true,
}

// renderTimeout is the maximum time allowed for CUE template evaluation.
const renderTimeout = 5 * time.Second

// GroupedResources holds Kubernetes resources partitioned by origin: resources
// from platformResources (organization/folder-level) and resources from
// projectResources (project-level).
//
// The structured JSON fields carry the JSON-serialized CUE evaluation outputs
// for each top-level section of the ResourceSetSpec. Nil means the section was
// not present or not concrete in the CUE template. An empty struct (e.g.
// platformResources with no resources) is serialized as its JSON representation
// rather than left nil.
type GroupedResources struct {
	Platform []unstructured.Unstructured
	Project  []unstructured.Unstructured
	// Structured CUE evaluation outputs as JSON.
	// Nil means the section was not evaluated or doesn't exist.
	DefaultsJSON                *string
	PlatformInputJSON           *string
	ProjectInputJSON            *string
	PlatformResourcesStructJSON *string
	ProjectResourcesStructJSON  *string
}

// CueRenderer evaluates CUE templates with deployment parameters.
type CueRenderer struct{}

// Render evaluates the CUE template with the given platform and project inputs and
// returns a list of K8s resource manifests as unstructured objects.
func (r *CueRenderer) Render(ctx context.Context, cueSource string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) ([]unstructured.Unstructured, error) {
	// Enforce evaluation timeout.
	evalCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	// Run evaluation in a goroutine so we can respect context cancellation.
	type result struct {
		resources []unstructured.Unstructured
		err       error
	}
	ch := make(chan result, 1)
	go func() {
		resources, err := evaluate(cueSource, platform, project)
		ch <- result{resources, err}
	}()

	select {
	case <-evalCtx.Done():
		return nil, fmt.Errorf("CUE template evaluation timed out after %s", renderTimeout)
	case res := <-ch:
		return res.resources, res.err
	}
}

// RenderGrouped evaluates the CUE template with the given platform and project
// inputs and returns resources grouped by origin (platform vs project). This is
// the project-level path: platformResources are not read (ADR 016 Decision 8),
// so the Platform group will always be empty.
func (r *CueRenderer) RenderGrouped(ctx context.Context, cueSource string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) (*GroupedResources, error) {
	evalCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	type result struct {
		grouped *GroupedResources
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		grouped, err := evaluateGrouped(cueSource, platform, project)
		ch <- result{grouped, err}
	}()

	select {
	case <-evalCtx.Done():
		return nil, fmt.Errorf("CUE template evaluation timed out after %s", renderTimeout)
	case res := <-ch:
		return res.grouped, res.err
	}
}

// RenderGroupedWithAncestorTemplates evaluates the deployment template unified
// with zero or more ancestor template CUE sources and returns resources grouped
// by origin (platform vs project). This is the org/folder-level path that reads
// both platformResources and projectResources.
func (r *CueRenderer) RenderGroupedWithAncestorTemplates(ctx context.Context, deploymentCUE string, ancestorTemplateCUESources []string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) (*GroupedResources, error) {
	evalCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	type result struct {
		grouped *GroupedResources
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		grouped, err := evaluateWithOrgTemplatesGrouped(deploymentCUE, ancestorTemplateCUESources, platform, project)
		ch <- result{grouped, err}
	}()

	select {
	case <-evalCtx.Done():
		return nil, fmt.Errorf("CUE template evaluation timed out after %s", renderTimeout)
	case res := <-ch:
		return res.grouped, res.err
	}
}

// RenderGroupedWithCueInput evaluates the CUE template unified with a raw CUE
// input string and returns resources grouped by origin (platform vs project).
// This is the preview path that reads both collections (ADR 016 Decision 8).
func (r *CueRenderer) RenderGroupedWithCueInput(ctx context.Context, cueSource, cueInput string) (*GroupedResources, error) {
	evalCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	type result struct {
		grouped *GroupedResources
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		grouped, err := evaluateWithCueInputGrouped(cueSource, cueInput)
		ch <- result{grouped, err}
	}()

	select {
	case <-evalCtx.Done():
		return nil, fmt.Errorf("CUE template evaluation timed out after %s", renderTimeout)
	case res := <-ch:
		return res.grouped, res.err
	}
}

// RenderWithAncestorTemplates evaluates the deployment template unified with zero
// or more ancestor template CUE sources (organization- and folder-level). Each
// ancestor template is concatenated with the deployment template before filling in
// the platform and project inputs. All templates can define values for both
// projectResources and platformResources. The renderer reads both collections when
// ancestor templates are present (organization/folder level).
func (r *CueRenderer) RenderWithAncestorTemplates(ctx context.Context, deploymentCUE string, ancestorTemplateCUESources []string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) ([]unstructured.Unstructured, error) {
	evalCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	type result struct {
		resources []unstructured.Unstructured
		err       error
	}
	ch := make(chan result, 1)
	go func() {
		resources, err := evaluateWithOrgTemplates(deploymentCUE, ancestorTemplateCUESources, platform, project)
		ch <- result{resources, err}
	}()

	select {
	case <-evalCtx.Done():
		return nil, fmt.Errorf("CUE template evaluation timed out after %s", renderTimeout)
	case res := <-ch:
		return res.resources, res.err
	}
}

// RenderWithCueInput evaluates the CUE template unified with a raw CUE input
// string at the "input" path and returns a list of K8s resource manifests as
// unstructured objects.  The cueInput must be valid CUE source that supplies
// concrete values for the template parameters (including "namespace").
func (r *CueRenderer) RenderWithCueInput(ctx context.Context, cueSource, cueInput string) ([]unstructured.Unstructured, error) {
	evalCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	type result struct {
		resources []unstructured.Unstructured
		err       error
	}
	ch := make(chan result, 1)
	go func() {
		resources, err := evaluateWithCueInput(cueSource, cueInput)
		ch <- result{resources, err}
	}()

	select {
	case <-evalCtx.Done():
		return nil, fmt.Errorf("CUE template evaluation timed out after %s", renderTimeout)
	case res := <-ch:
		return res.resources, res.err
	}
}

// evaluateWithOrgTemplates performs synchronous CUE template evaluation of a
// deployment template unified with zero or more platform template CUE sources.
// All CUE sources are concatenated before compilation so that platform templates
// can reference top-level identifiers (input, platform, _labels, etc.) defined
// by the deployment template.
// All templates can define values for both projectResources and platformResources.
// The renderer reads both collections at the organization/folder level (ADR 016).
func evaluateWithOrgTemplates(deploymentCUE string, orgTemplateCUESources []string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) ([]unstructured.Unstructured, error) {
	cueCtx := cuecontext.New()

	// Prepend generated schema definitions and concatenate all CUE sources.
	// Platform templates may reference identifiers defined in the deployment
	// template (input, platform, _labels, etc.) as well as generated type
	// definitions (#PlatformInput, #ProjectInput, etc.). Combining them into
	// a single compilation unit allows those cross-references to resolve.
	combined := v1alpha2.GeneratedSchema + "\n" + deploymentCUE
	for _, orgTemplateSrc := range orgTemplateCUESources {
		combined = combined + "\n" + orgTemplateSrc
	}

	unified := cueCtx.CompileString(combined)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("invalid CUE template (deployment + platform templates): %w", err)
	}

	// Encode project input as JSON then compile to a CUE value and unify at "input".
	inputJSON, err := json.Marshal(project)
	if err != nil {
		return nil, fmt.Errorf("encoding project input: %w", err)
	}
	inputValue := cueCtx.CompileBytes(inputJSON)
	if err := inputValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling project input: %w", err)
	}

	// Encode platform input as JSON then compile to a CUE value and unify at "platform".
	platformJSON, err := json.Marshal(platform)
	if err != nil {
		return nil, fmt.Errorf("encoding platform input: %w", err)
	}
	platformValue := cueCtx.CompileBytes(platformJSON)
	if err := platformValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling platform input: %w", err)
	}

	// Unify template with inputs.
	unified = unified.FillPath(cue.ParsePath("input"), inputValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with project input: %w", err)
	}
	unified = unified.FillPath(cue.ParsePath("platform"), platformValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with platform input: %w", err)
	}

	// Require the structured output format: projectResources.namespacedResources must exist.
	namespacedValue := unified.LookupPath(cue.ParsePath("projectResources.namespacedResources"))
	if namespacedValue.Err() != nil || !namespacedValue.Exists() {
		return nil, fmt.Errorf("deployment template must define 'projectResources.namespacedResources' (structured output format required)")
	}

	return evaluateStructured(unified, true)
}

// evaluate performs synchronous CUE template evaluation.
// Templates must use the structured output format under projectResources.
// The platform input (project, namespace, claims) and project input (name, image,
// tag, etc.) are encoded separately and unified with the template.
// This is the project-level render path. Per ADR 016, the renderer does not read
// platformResources from project-level templates.
func evaluate(cueSource string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) ([]unstructured.Unstructured, error) {
	cueCtx := cuecontext.New()

	// Prepend generated schema definitions so templates can reference
	// #PlatformInput, #ProjectInput, #Claims, etc.
	fullSource := v1alpha2.GeneratedSchema + "\n" + cueSource

	// Compile the template source.
	tmpl := cueCtx.CompileString(fullSource)
	if err := tmpl.Err(); err != nil {
		return nil, fmt.Errorf("invalid CUE template: %w", err)
	}

	// Encode project input as JSON then compile to a CUE value and unify at "input".
	inputJSON, err := json.Marshal(project)
	if err != nil {
		return nil, fmt.Errorf("encoding project input: %w", err)
	}
	inputValue := cueCtx.CompileBytes(inputJSON)
	if err := inputValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling project input: %w", err)
	}

	// Encode platform input as JSON then compile to a CUE value and unify at "platform".
	platformJSON, err := json.Marshal(platform)
	if err != nil {
		return nil, fmt.Errorf("encoding platform input: %w", err)
	}
	platformValue := cueCtx.CompileBytes(platformJSON)
	if err := platformValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling platform input: %w", err)
	}

	// Unify template with the project input at the "input" path and platform input
	// at the "platform" path.
	unified := tmpl.FillPath(cue.ParsePath("input"), inputValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with project input: %w", err)
	}
	unified = unified.FillPath(cue.ParsePath("platform"), platformValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with platform input: %w", err)
	}

	// Require the structured output format: projectResources.namespacedResources must exist.
	namespacedValue := unified.LookupPath(cue.ParsePath("projectResources.namespacedResources"))
	if namespacedValue.Err() != nil || !namespacedValue.Exists() {
		return nil, fmt.Errorf("template must define 'projectResources.namespacedResources' (structured output format required)")
	}

	// Project-level render: do not read platformResources (ADR 016 Decision 8).
	return evaluateStructured(unified, false)
}

// evaluateWithCueInput performs synchronous CUE template evaluation using a raw
// CUE string as input.  The cueInput is a CUE document that provides both
// "input" (user-provided values) and "platform" (trusted backend values) at the
// top level.  The template source and input are compiled together so that
// cross-references (e.g. input.name used in the template) resolve correctly.
func evaluateWithCueInput(cueSource, cueInput string) ([]unstructured.Unstructured, error) {
	cueCtx := cuecontext.New()

	// Prepend generated schema definitions and compile the template source
	// together with the CUE input document. Concatenating them in a single
	// compilation unit allows the template to reference top-level identifiers
	// (input.name, platform.namespace, etc.) and generated type definitions
	// (#PlatformInput, #ProjectInput, etc.).
	combined := v1alpha2.GeneratedSchema + "\n" + cueSource + "\n" + cueInput
	unified := cueCtx.CompileString(combined)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("invalid CUE template: %w", err)
	}

	// Require the structured output format.
	// Platform templates define platformResources.namespacedResources; project
	// templates define projectResources.namespacedResources.  At minimum one of
	// these must exist.  For platform template standalone preview we check for either.
	namespacedValue := unified.LookupPath(cue.ParsePath("projectResources.namespacedResources"))
	platformNamespacedValue := unified.LookupPath(cue.ParsePath("platformResources.namespacedResources"))
	if (namespacedValue.Err() != nil || !namespacedValue.Exists()) &&
		(platformNamespacedValue.Err() != nil || !platformNamespacedValue.Exists()) {
		return nil, fmt.Errorf("template must define 'projectResources.namespacedResources' or 'platformResources.namespacedResources' (structured output format required)")
	}

	// Preview mode reads both collections (ADR 016 Decision 8).
	return evaluateStructured(unified, true)
}

// evaluateGrouped performs synchronous CUE template evaluation and returns
// resources grouped by origin. This is the project-level path: platformResources
// are not read (ADR 016 Decision 8), so Platform will always be empty.
func evaluateGrouped(cueSource string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) (*GroupedResources, error) {
	cueCtx := cuecontext.New()

	fullSource := v1alpha2.GeneratedSchema + "\n" + cueSource

	tmpl := cueCtx.CompileString(fullSource)
	if err := tmpl.Err(); err != nil {
		return nil, fmt.Errorf("invalid CUE template: %w", err)
	}

	inputJSON, err := json.Marshal(project)
	if err != nil {
		return nil, fmt.Errorf("encoding project input: %w", err)
	}
	inputValue := cueCtx.CompileBytes(inputJSON)
	if err := inputValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling project input: %w", err)
	}

	platformJSON, err := json.Marshal(platform)
	if err != nil {
		return nil, fmt.Errorf("encoding platform input: %w", err)
	}
	platformValue := cueCtx.CompileBytes(platformJSON)
	if err := platformValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling platform input: %w", err)
	}

	unified := tmpl.FillPath(cue.ParsePath("input"), inputValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with project input: %w", err)
	}
	unified = unified.FillPath(cue.ParsePath("platform"), platformValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with platform input: %w", err)
	}

	namespacedValue := unified.LookupPath(cue.ParsePath("projectResources.namespacedResources"))
	if namespacedValue.Err() != nil || !namespacedValue.Exists() {
		return nil, fmt.Errorf("template must define 'projectResources.namespacedResources' (structured output format required)")
	}

	// Project-level render: do not read platformResources (ADR 016 Decision 8).
	return evaluateStructuredGrouped(unified, false)
}

// evaluateWithOrgTemplatesGrouped performs synchronous CUE template evaluation
// with org/folder templates and returns resources grouped by origin.
func evaluateWithOrgTemplatesGrouped(deploymentCUE string, orgTemplateCUESources []string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) (*GroupedResources, error) {
	cueCtx := cuecontext.New()

	combined := v1alpha2.GeneratedSchema + "\n" + deploymentCUE
	for _, orgTemplateSrc := range orgTemplateCUESources {
		combined = combined + "\n" + orgTemplateSrc
	}

	unified := cueCtx.CompileString(combined)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("invalid CUE template (deployment + platform templates): %w", err)
	}

	inputJSON, err := json.Marshal(project)
	if err != nil {
		return nil, fmt.Errorf("encoding project input: %w", err)
	}
	inputValue := cueCtx.CompileBytes(inputJSON)
	if err := inputValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling project input: %w", err)
	}

	platformJSON, err := json.Marshal(platform)
	if err != nil {
		return nil, fmt.Errorf("encoding platform input: %w", err)
	}
	platformValue := cueCtx.CompileBytes(platformJSON)
	if err := platformValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling platform input: %w", err)
	}

	unified = unified.FillPath(cue.ParsePath("input"), inputValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with project input: %w", err)
	}
	unified = unified.FillPath(cue.ParsePath("platform"), platformValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with platform input: %w", err)
	}

	namespacedValue := unified.LookupPath(cue.ParsePath("projectResources.namespacedResources"))
	if namespacedValue.Err() != nil || !namespacedValue.Exists() {
		return nil, fmt.Errorf("deployment template must define 'projectResources.namespacedResources' (structured output format required)")
	}

	return evaluateStructuredGrouped(unified, true)
}

// evaluateWithCueInputGrouped performs synchronous CUE template evaluation using
// a raw CUE string as input and returns resources grouped by origin.
func evaluateWithCueInputGrouped(cueSource, cueInput string) (*GroupedResources, error) {
	cueCtx := cuecontext.New()

	combined := v1alpha2.GeneratedSchema + "\n" + cueSource + "\n" + cueInput
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

	// Preview mode reads both collections (ADR 016 Decision 8).
	return evaluateStructuredGrouped(unified, true)
}

// extractCuePathJSON looks up a CUE path in the unified value and returns the
// JSON serialization if the path exists and is concrete. Returns nil if the path
// does not exist. Returns an error only if the path exists but cannot be
// marshaled to JSON.
func extractCuePathJSON(unified cue.Value, cuePath string) (*string, error) {
	v := unified.LookupPath(cue.ParsePath(cuePath))
	if v.Err() != nil || !v.Exists() {
		return nil, nil
	}
	b, err := v.MarshalJSON()
	if err != nil {
		// Path exists but isn't concrete enough to marshal — treat as absent.
		return nil, nil //nolint:nilerr
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("CUE path %q produced invalid JSON", cuePath)
	}
	s := string(b)
	return &s, nil
}

// populateStructuredJSON extracts the five structured JSON sections from the
// unified CUE value and sets the corresponding fields on the GroupedResources.
// Extraction errors are logged but do not fail the render.
func populateStructuredJSON(unified cue.Value, gr *GroupedResources) {
	paths := []struct {
		cuePath string
		target  **string
	}{
		{"defaults", &gr.DefaultsJSON},
		{"platform", &gr.PlatformInputJSON},
		{"input", &gr.ProjectInputJSON},
		{"platformResources", &gr.PlatformResourcesStructJSON},
		{"projectResources", &gr.ProjectResourcesStructJSON},
	}
	for _, p := range paths {
		val, err := extractCuePathJSON(unified, p.cuePath)
		if err != nil {
			// Non-fatal: log and skip.
			continue
		}
		*p.target = val
	}
}

// evaluateStructuredGrouped walks the structured output fields of a unified CUE
// value and returns validated Kubernetes resources partitioned into Platform and
// Project groups. The logic mirrors evaluateStructured but collects resources
// into separate slices instead of a single flat list.
//
// There is no restriction on which namespaces resources may target. The
// struct-key/metadata consistency check ensures internal consistency within the
// template (ADR 026).
func evaluateStructuredGrouped(unified cue.Value, readPlatformResources bool) (*GroupedResources, error) {
	var projectResources []unstructured.Unstructured
	var platformResources []unstructured.Unstructured

	// Walk projectResources.namespacedResources: <namespace>.<Kind>.<name>
	namespacedValue := unified.LookupPath(cue.ParsePath("projectResources.namespacedResources"))
	if namespacedValue.Err() == nil && namespacedValue.Exists() {
		resources, err := walkNamespacedResources(namespacedValue, "projectResources.namespacedResources")
		if err != nil {
			return nil, err
		}
		projectResources = append(projectResources, resources...)
	}

	// Walk projectResources.clusterResources: <Kind>.<name>
	clusterValue := unified.LookupPath(cue.ParsePath("projectResources.clusterResources"))
	if clusterValue.Err() == nil && clusterValue.Exists() {
		resources, err := walkClusterResources(clusterValue, "projectResources.clusterResources")
		if err != nil {
			return nil, err
		}
		projectResources = append(projectResources, resources...)
	}

	if !readPlatformResources {
		gr := &GroupedResources{
			Platform: nil,
			Project:  projectResources,
		}
		populateStructuredJSON(unified, gr)
		return gr, nil
	}

	// Walk platformResources.namespacedResources
	platformNamespacedValue := unified.LookupPath(cue.ParsePath("platformResources.namespacedResources"))
	if platformNamespacedValue.Err() == nil && platformNamespacedValue.Exists() {
		resources, err := walkNamespacedResources(platformNamespacedValue, "platformResources.namespacedResources")
		if err != nil {
			return nil, err
		}
		platformResources = append(platformResources, resources...)
	}

	// Walk platformResources.clusterResources
	platformClusterValue := unified.LookupPath(cue.ParsePath("platformResources.clusterResources"))
	if platformClusterValue.Err() == nil && platformClusterValue.Exists() {
		resources, err := walkClusterResources(platformClusterValue, "platformResources.clusterResources")
		if err != nil {
			return nil, err
		}
		platformResources = append(platformResources, resources...)
	}

	gr := &GroupedResources{
		Platform: platformResources,
		Project:  projectResources,
	}
	populateStructuredJSON(unified, gr)
	return gr, nil
}

// evaluateStructured walks the structured output fields of a unified CUE value
// and returns validated Kubernetes resources.
//
// It always reads projectResources (project-level resources):
//
//	projectResources.namespacedResources.<namespace>.<Kind>.<name>
//	projectResources.clusterResources.<Kind>.<name>
//
// When readPlatformResources is true, it also reads platformResources
// (organization/folder-level resources):
//
//	platformResources.namespacedResources.<namespace>.<Kind>.<name>
//	platformResources.clusterResources.<Kind>.<name>
//
// Per ADR 016 Decision 8, the project-level render path passes false so that a
// project template cannot produce platformResources. Organization and folder
// level paths pass true to read both collections. This is a hard boundary
// enforced in Go code, not in CUE.
//
// There is no restriction on which namespaces resources may target. The
// struct-key/metadata consistency check ensures internal consistency within the
// template (ADR 026).
func evaluateStructured(unified cue.Value, readPlatformResources bool) ([]unstructured.Unstructured, error) {
	var result []unstructured.Unstructured

	// Walk projectResources.namespacedResources: <namespace>.<Kind>.<name>
	namespacedValue := unified.LookupPath(cue.ParsePath("projectResources.namespacedResources"))
	if namespacedValue.Err() == nil && namespacedValue.Exists() {
		resources, err := walkNamespacedResources(namespacedValue, "projectResources.namespacedResources")
		if err != nil {
			return nil, err
		}
		result = append(result, resources...)
	}

	// Walk projectResources.clusterResources: <Kind>.<name>
	clusterValue := unified.LookupPath(cue.ParsePath("projectResources.clusterResources"))
	if clusterValue.Err() == nil && clusterValue.Exists() {
		resources, err := walkClusterResources(clusterValue, "projectResources.clusterResources")
		if err != nil {
			return nil, err
		}
		result = append(result, resources...)
	}

	if !readPlatformResources {
		return result, nil
	}

	// Walk platformResources.namespacedResources (populated by organization/folder templates;
	// skipped for project-level rendering).
	platformNamespacedValue := unified.LookupPath(cue.ParsePath("platformResources.namespacedResources"))
	if platformNamespacedValue.Err() == nil && platformNamespacedValue.Exists() {
		resources, err := walkNamespacedResources(platformNamespacedValue, "platformResources.namespacedResources")
		if err != nil {
			return nil, err
		}
		result = append(result, resources...)
	}

	// Walk platformResources.clusterResources (populated by organization/folder templates;
	// skipped for project-level rendering).
	platformClusterValue := unified.LookupPath(cue.ParsePath("platformResources.clusterResources"))
	if platformClusterValue.Err() == nil && platformClusterValue.Exists() {
		resources, err := walkClusterResources(platformClusterValue, "platformResources.clusterResources")
		if err != nil {
			return nil, err
		}
		result = append(result, resources...)
	}

	return result, nil
}

// walkNamespacedResources iterates a namespaced resource map of the form
// <namespace>.<Kind>.<name> and returns validated Kubernetes resources.
//
// The struct-key/metadata consistency check is enforced: metadata.namespace,
// metadata.name, and kind must match their respective struct keys. There is no
// restriction on which namespaces may appear — templates may produce resources
// targeting any namespace (ADR 026).
func walkNamespacedResources(namespacedValue cue.Value, fieldPath string) ([]unstructured.Unstructured, error) {
	var result []unstructured.Unstructured

	nsIter, err := namespacedValue.Fields()
	if err != nil {
		return nil, fmt.Errorf("iterating %s keys: %w", fieldPath, err)
	}
	for nsIter.Next() {
		nsKey := nsIter.Selector().Unquoted()
		if nsKey == "" {
			return nil, fmt.Errorf("%s: empty namespace key is not allowed", fieldPath)
		}
		kindIter, err := nsIter.Value().Fields()
		if err != nil {
			return nil, fmt.Errorf("iterating Kind keys under %s/%s: %w", fieldPath, nsKey, err)
		}
		for kindIter.Next() {
			kindKey := kindIter.Selector().Unquoted()
			nameIter, err := kindIter.Value().Fields()
			if err != nil {
				return nil, fmt.Errorf("iterating name keys under %s/%s/%s: %w", fieldPath, nsKey, kindKey, err)
			}
			for nameIter.Next() {
				nameKey := nameIter.Selector().Unquoted()
				var raw map[string]any
				if err := nameIter.Value().Decode(&raw); err != nil {
					return nil, fmt.Errorf("decoding %s/%s/%s/%s: %w", fieldPath, nsKey, kindKey, nameKey, err)
				}
				u := unstructured.Unstructured{Object: raw}

				// Enforce struct-key / metadata consistency.
				if u.GetNamespace() != nsKey {
					return nil, fmt.Errorf("%s/%s/%s/%s: metadata.namespace %q does not match struct key %q",
						fieldPath, nsKey, kindKey, nameKey, u.GetNamespace(), nsKey)
				}
				if u.GetKind() != kindKey {
					return nil, fmt.Errorf("%s/%s/%s/%s: kind %q does not match struct key %q",
						fieldPath, nsKey, kindKey, nameKey, u.GetKind(), kindKey)
				}
				if u.GetName() != nameKey {
					return nil, fmt.Errorf("%s/%s/%s/%s: metadata.name %q does not match struct key %q",
						fieldPath, nsKey, kindKey, nameKey, u.GetName(), nameKey)
				}

				// Run common resource validations.
				if err := validateResource(u); err != nil {
					return nil, err
				}

				result = append(result, u)
			}
		}
	}

	return result, nil
}

// walkClusterResources iterates a cluster-scoped resource map of the form
// <Kind>.<name> and returns validated Kubernetes resources.
func walkClusterResources(clusterValue cue.Value, fieldPath string) ([]unstructured.Unstructured, error) {
	var result []unstructured.Unstructured

	kindIter, err := clusterValue.Fields()
	if err != nil {
		return nil, fmt.Errorf("iterating %s Kind keys: %w", fieldPath, err)
	}
	for kindIter.Next() {
		kindKey := kindIter.Selector().Unquoted()
		nameIter, err := kindIter.Value().Fields()
		if err != nil {
			return nil, fmt.Errorf("iterating name keys under %s/%s: %w", fieldPath, kindKey, err)
		}
		for nameIter.Next() {
			nameKey := nameIter.Selector().Unquoted()
			var raw map[string]any
			if err := nameIter.Value().Decode(&raw); err != nil {
				return nil, fmt.Errorf("decoding %s/%s/%s: %w", fieldPath, kindKey, nameKey, err)
			}
			u := unstructured.Unstructured{Object: raw}

			// Enforce struct-key / metadata consistency.
			if u.GetKind() != kindKey {
				return nil, fmt.Errorf("%s/%s/%s: kind %q does not match struct key %q",
					fieldPath, kindKey, nameKey, u.GetKind(), kindKey)
			}
			if u.GetName() != nameKey {
				return nil, fmt.Errorf("%s/%s/%s: metadata.name %q does not match struct key %q",
					fieldPath, kindKey, nameKey, u.GetName(), nameKey)
			}

			// Cluster-scoped resources must NOT have a namespace.
			if u.GetNamespace() != "" {
				return nil, fmt.Errorf("%s resource %s/%s: must not have metadata.namespace", fieldPath, kindKey, nameKey)
			}

			result = append(result, u)
		}
	}

	return result, nil
}

// validateResource runs common safety checks on a single resource.
func validateResource(u unstructured.Unstructured) error {
	if u.GetAPIVersion() == "" {
		return fmt.Errorf("resource %s/%s: missing apiVersion", u.GetKind(), u.GetName())
	}
	if u.GetKind() == "" {
		return fmt.Errorf("resource missing kind")
	}
	if u.GetName() == "" {
		return fmt.Errorf("resource %s: missing metadata.name", u.GetKind())
	}

	// Enforce kind allowlist.
	if !allowedKindSet[u.GetKind()] {
		return fmt.Errorf("resource %s/%s: kind %q is not allowed; permitted kinds: Deployment, Service, ServiceAccount, Role, RoleBinding, HTTPRoute, ConfigMap, Secret",
			u.GetKind(), u.GetName(), u.GetKind())
	}

	// Enforce managed-by label.
	labels := u.GetLabels()
	if labels["app.kubernetes.io/managed-by"] != "console.holos.run" {
		return fmt.Errorf("resource %s/%s: missing required label app.kubernetes.io/managed-by=console.holos.run",
			u.GetKind(), u.GetName())
	}

	return nil
}
