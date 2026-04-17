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
	// OutputJSON carries the JSON-serialized `output` section of the unified
	// CUE value (ResourceSetSpec.output). Nil when the template has no
	// `output` block or the section is non-concrete. Present-but-empty
	// (e.g. `{}`) is preserved so the frontend can decide whether to render.
	OutputJSON *string
}

// RenderInputs carries the structured inputs every render needs: the trusted
// platform-side PlatformInput and the user-supplied ProjectInput. Both are
// marshaled to JSON and unified with the template at the "platform" and
// "input" CUE paths respectively.
type RenderInputs struct {
	Platform v1alpha2.PlatformInput
	Project  v1alpha2.ProjectInput
}

// CueRenderer evaluates CUE templates with deployment parameters.
type CueRenderer struct{}

// Render evaluates the CUE template unified with zero or more ancestor
// template CUE sources and the provided inputs, returning resources grouped
// by origin (platform vs project).
//
// When ancestorSources is empty this is a project-level render and
// platformResources is not read (ADR 016 Decision 8); when ancestorSources is
// non-empty both collections are read (organization/folder-level).
//
// The deployment template and any ancestor sources are concatenated before
// compilation so ancestor templates can reference top-level identifiers
// defined by the deployment template (input, platform, _labels, etc.).
func (r *CueRenderer) Render(ctx context.Context, cueSource string, ancestorSources []string, inputs RenderInputs) (*GroupedResources, error) {
	evalCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	type result struct {
		grouped *GroupedResources
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		grouped, err := evaluateWithInputs(cueSource, ancestorSources, inputs)
		ch <- result{grouped, err}
	}()

	select {
	case <-evalCtx.Done():
		return nil, fmt.Errorf("CUE template evaluation timed out after %s", renderTimeout)
	case res := <-ch:
		return res.grouped, res.err
	}
}

// EvaluateGroupedCUE compiles and evaluates a pre-concatenated CUE source
// document (template + any already-embedded raw CUE input) and returns the
// rendered Kubernetes resources grouped by origin. This is the raw-CUE entry
// point used by callers that assemble the full CUE document themselves (for
// example, the templates preview path which receives CUE strings for
// "platform" and "input" from the client, or the mandatory-template applier
// which marshals its own inputs to CUE).
//
// When readPlatformResources is true the renderer reads both
// platformResources and projectResources; when false only projectResources is
// read (per ADR 016 Decision 8 the project-level path must not emit
// platformResources).
//
// This helper exists so the unified evaluation core lives in one place; the
// raw-CUE call sites in the templates package will be removed in later phases
// (see HOL-562).
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

// evaluateWithInputs performs synchronous CUE template evaluation with
// structured Platform and Project inputs. The deployment template is
// concatenated with any ancestor sources before compilation so ancestor
// templates can reference top-level identifiers (input, platform, _labels,
// etc.) defined by the deployment template. Generated schema definitions are
// always prepended so templates can reference #PlatformInput, #ProjectInput,
// #Claims, etc.
//
// Inputs are encoded as JSON and unified at the "input" (project) and
// "platform" paths. When ancestorSources is empty the render is project-level
// and platformResources is not read (ADR 016 Decision 8); otherwise both
// collections are read.
func evaluateWithInputs(cueSource string, ancestorSources []string, inputs RenderInputs) (*GroupedResources, error) {
	cueCtx := cuecontext.New()

	combined := v1alpha2.GeneratedSchema + "\n" + cueSource
	for _, src := range ancestorSources {
		combined = combined + "\n" + src
	}

	unified := cueCtx.CompileString(combined)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("invalid CUE template: %w", err)
	}

	inputJSON, err := json.Marshal(inputs.Project)
	if err != nil {
		return nil, fmt.Errorf("encoding project input: %w", err)
	}
	inputValue := cueCtx.CompileBytes(inputJSON)
	if err := inputValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling project input: %w", err)
	}

	platformJSON, err := json.Marshal(inputs.Platform)
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
		return nil, fmt.Errorf("template must define 'projectResources.namespacedResources' (structured output format required)")
	}

	readPlatformResources := len(ancestorSources) > 0
	return evaluateStructuredGrouped(unified, readPlatformResources)
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

// populateStructuredJSON extracts the structured JSON sections from the
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
		{"output", &gr.OutputJSON},
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

// evaluateStructuredGrouped walks the structured output fields of a unified
// CUE value and returns validated Kubernetes resources partitioned into
// Platform and Project groups.
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
// Per ADR 016 Decision 8, the project-level render path passes false so that
// a project template cannot produce platformResources. Organization and
// folder level paths pass true to read both collections. This is a hard
// boundary enforced in Go code, not in CUE.
//
// There is no restriction on which namespaces resources may target. The
// struct-key/metadata consistency check ensures internal consistency within
// the template (ADR 026).
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
