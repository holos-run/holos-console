package deployments

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
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

// CueRenderer evaluates CUE templates with deployment parameters.
type CueRenderer struct{}

// Render evaluates the CUE template with the given platform and project inputs and
// returns a list of K8s resource manifests as unstructured objects.
func (r *CueRenderer) Render(ctx context.Context, cueSource string, platform v1alpha1.PlatformInput, project v1alpha1.ProjectInput) ([]unstructured.Unstructured, error) {
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

// RenderWithSystemTemplates evaluates the deployment template unified with zero or
// more system template CUE sources. Each system template is unified with the
// deployment template before filling in the platform and project inputs.
// System templates contribute resources via platformResources.namespacedResources and
// platformResources.clusterResources; the deployment template contributes resources via
// projectResources.namespacedResources and projectResources.clusterResources.
func (r *CueRenderer) RenderWithSystemTemplates(ctx context.Context, deploymentCUE string, systemCUESources []string, platform v1alpha1.PlatformInput, project v1alpha1.ProjectInput) ([]unstructured.Unstructured, error) {
	evalCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	type result struct {
		resources []unstructured.Unstructured
		err       error
	}
	ch := make(chan result, 1)
	go func() {
		resources, err := evaluateWithSystemTemplates(deploymentCUE, systemCUESources, platform, project)
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

// evaluateWithSystemTemplates performs synchronous CUE template evaluation of a
// deployment template unified with zero or more system template CUE sources.
// All CUE sources are concatenated before compilation so that system templates
// can reference top-level identifiers (input, platform, _labels, etc.) defined
// by the deployment template.
// System templates contribute resources via platformResources.namespacedResources and
// platformResources.clusterResources; the deployment template contributes resources via
// projectResources.namespacedResources and projectResources.clusterResources.
func evaluateWithSystemTemplates(deploymentCUE string, systemCUESources []string, platform v1alpha1.PlatformInput, project v1alpha1.ProjectInput) ([]unstructured.Unstructured, error) {
	cueCtx := cuecontext.New()

	// Concatenate all CUE sources. System templates may reference identifiers
	// defined in the deployment template (input, platform, _labels, etc.).
	// Combining them into a single compilation unit allows those cross-references
	// to resolve correctly.
	combined := deploymentCUE
	for _, sysSrc := range systemCUESources {
		combined = combined + "\n" + sysSrc
	}

	unified := cueCtx.CompileString(combined)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("invalid CUE template (deployment + system templates): %w", err)
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

	return evaluateStructured(unified, platform.Namespace)
}

// evaluate performs synchronous CUE template evaluation.
// Templates must use the structured output format under projectResources.
// The platform input (project, namespace, claims) and project input (name, image,
// tag, etc.) are encoded separately and unified with the template.
func evaluate(cueSource string, platform v1alpha1.PlatformInput, project v1alpha1.ProjectInput) ([]unstructured.Unstructured, error) {
	cueCtx := cuecontext.New()

	// Compile the template source.
	tmpl := cueCtx.CompileString(cueSource)
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

	return evaluateStructured(unified, platform.Namespace)
}

// evaluateWithCueInput performs synchronous CUE template evaluation using a raw
// CUE string as input.  The cueInput is a CUE document that provides both
// "input" (user-provided values) and "platform" (trusted backend values) at the
// top level.  The template source and input are compiled together so that
// cross-references (e.g. input.name used in the template) resolve correctly.
// The expected namespace is derived from platform.namespace in the unified value.
func evaluateWithCueInput(cueSource, cueInput string) ([]unstructured.Unstructured, error) {
	cueCtx := cuecontext.New()

	// Compile the template source together with the CUE input document.
	// Concatenating them in a single compilation unit allows the template to
	// reference top-level identifiers (input.name, platform.namespace, etc.)
	// provided by the input document.
	combined := cueSource + "\n" + cueInput
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

	// Extract the expected namespace from the unified platform value.
	nsValue := unified.LookupPath(cue.ParsePath("platform.namespace"))
	if nsValue.Err() != nil || !nsValue.Exists() {
		return nil, fmt.Errorf("cue_input must provide a 'platform.namespace' field")
	}
	expectedNamespace, err := nsValue.String()
	if err != nil {
		return nil, fmt.Errorf("platform.namespace must be a concrete string: %w", err)
	}

	return evaluateStructured(unified, expectedNamespace)
}

// evaluateStructured walks the projectResources.namespacedResources,
// projectResources.clusterResources, platformResources.namespacedResources, and
// platformResources.clusterResources structured output fields and returns
// validated Kubernetes resources.
//
// namespacedResources structure: projectResources.namespacedResources.<namespace>.<Kind>.<name>
// clusterResources structure:    projectResources.clusterResources.<Kind>.<name>
// platformResources follow the same structure but are populated by platform template evaluations.
func evaluateStructured(unified cue.Value, expectedNamespace string) ([]unstructured.Unstructured, error) {
	var result []unstructured.Unstructured

	// Walk projectResources.namespacedResources: <namespace>.<Kind>.<name>
	namespacedValue := unified.LookupPath(cue.ParsePath("projectResources.namespacedResources"))
	if namespacedValue.Err() == nil && namespacedValue.Exists() {
		resources, err := walkNamespacedResources(namespacedValue, expectedNamespace, "projectResources.namespacedResources")
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

	// Walk platformResources.namespacedResources (populated by platform templates).
	platformNamespacedValue := unified.LookupPath(cue.ParsePath("platformResources.namespacedResources"))
	if platformNamespacedValue.Err() == nil && platformNamespacedValue.Exists() {
		resources, err := walkNamespacedResources(platformNamespacedValue, expectedNamespace, "platformResources.namespacedResources")
		if err != nil {
			return nil, err
		}
		result = append(result, resources...)
	}

	// Walk platformResources.clusterResources (populated by platform templates).
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
// All resources must reside in expectedNamespace.
func walkNamespacedResources(namespacedValue cue.Value, expectedNamespace, fieldPath string) ([]unstructured.Unstructured, error) {
	var result []unstructured.Unstructured

	nsIter, err := namespacedValue.Fields()
	if err != nil {
		return nil, fmt.Errorf("iterating %s keys: %w", fieldPath, err)
	}
	for nsIter.Next() {
		nsKey := nsIter.Selector().Unquoted()
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

				// Enforce project namespace constraint.
				if u.GetNamespace() != expectedNamespace {
					return nil, fmt.Errorf("%s resource %s/%s: namespace %q does not match project namespace %q",
						fieldPath, u.GetKind(), u.GetName(), u.GetNamespace(), expectedNamespace)
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

