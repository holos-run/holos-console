package deployments

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

// KeyRefInput identifies a key within a Kubernetes Secret or ConfigMap.
type KeyRefInput struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// EnvVarInput represents a container environment variable passed to CUE templates.
// Exactly one of Value, SecretKeyRef, or ConfigMapKeyRef should be set.
type EnvVarInput struct {
	Name            string       `json:"name"`
	Value           string       `json:"value,omitempty"`
	SecretKeyRef    *KeyRefInput `json:"secretKeyRef,omitempty"`
	ConfigMapKeyRef *KeyRefInput `json:"configMapKeyRef,omitempty"`
}

// ClaimsInput carries OIDC ID token claims passed to CUE templates.
type ClaimsInput struct {
	Iss           string   `json:"iss"`
	Sub           string   `json:"sub"`
	Aud           string   `json:"aud,omitempty"`
	Exp           int64    `json:"exp"`
	Iat           int64    `json:"iat"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Name          string   `json:"name,omitempty"`
	Groups        []string `json:"groups,omitempty"`
}

// SystemInput contains trusted values set by the console backend.
// These values are derived from authenticated context (project namespace
// resolution and OIDC token claims) and are not supplied by the user.
type SystemInput struct {
	Project          string      `json:"project"`
	Namespace        string      `json:"namespace"`
	GatewayNamespace string      `json:"gatewayNamespace,omitempty"`
	Claims           ClaimsInput `json:"claims"`
}

// UserInput contains user-provided deployment parameters.
type UserInput struct {
	Name    string        `json:"name"`
	Image   string        `json:"image"`
	Tag     string        `json:"tag"`
	Command []string      `json:"command,omitempty"`
	Args    []string      `json:"args,omitempty"`
	Env     []EnvVarInput `json:"env,omitempty"`
	Port    int32         `json:"port,omitempty"`
}

// stripPackageDecl removes the first "package <name>" line from a CUE source
// string. This is used when concatenating multiple CUE files that all share the
// same package declaration; only the first file needs to declare the package.
func stripPackageDecl(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") {
			// Remove this line and return the rest.
			rest := lines[i+1:]
			return strings.Join(rest, "\n")
		}
	}
	return src
}

// CueRenderer evaluates CUE templates with deployment parameters.
type CueRenderer struct{}

// Render evaluates the CUE template with the given system and user inputs and
// returns a list of K8s resource manifests as unstructured objects.
func (r *CueRenderer) Render(ctx context.Context, cueSource string, system SystemInput, user UserInput) ([]unstructured.Unstructured, error) {
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
		resources, err := evaluate(cueSource, system, user)
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
// deployment template before filling in the system and user inputs.
// System templates contribute resources via output.systemNamespacedResources and
// output.systemClusterResources; the deployment template contributes resources via
// output.namespacedResources and output.clusterResources.
func (r *CueRenderer) RenderWithSystemTemplates(ctx context.Context, deploymentCUE string, systemCUESources []string, system SystemInput, user UserInput) ([]unstructured.Unstructured, error) {
	evalCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	type result struct {
		resources []unstructured.Unstructured
		err       error
	}
	ch := make(chan result, 1)
	go func() {
		resources, err := evaluateWithSystemTemplates(deploymentCUE, systemCUESources, system, user)
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
// All CUE sources share the same package (package deployment) and are concatenated
// before compilation so that system templates can reference top-level identifiers
// (input, system, _labels, etc.) defined by the deployment template.
// System templates contribute resources via output.systemNamespacedResources and
// output.systemClusterResources; the deployment template contributes resources via
// output.namespacedResources and output.clusterResources.
func evaluateWithSystemTemplates(deploymentCUE string, systemCUESources []string, system SystemInput, user UserInput) ([]unstructured.Unstructured, error) {
	cueCtx := cuecontext.New()

	// Concatenate all CUE sources. System templates use the same package as
	// deployment templates (package deployment) and may reference identifiers
	// defined in the deployment template (input, system, _labels, etc.).
	// Combining them into a single compilation unit allows those cross-references
	// to resolve correctly.
	combined := deploymentCUE
	for _, sysSrc := range systemCUESources {
		// Strip the package declaration from system template sources since the
		// deployment template already declares the package. We preserve the rest
		// of the source (constraints, output fields, etc.).
		stripped := stripPackageDecl(sysSrc)
		combined = combined + "\n" + stripped
	}

	unified := cueCtx.CompileString(combined)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("invalid CUE template (deployment + system templates): %w", err)
	}

	// Encode user input as JSON then compile to a CUE value and unify at "input".
	inputJSON, err := json.Marshal(user)
	if err != nil {
		return nil, fmt.Errorf("encoding user input: %w", err)
	}
	inputValue := cueCtx.CompileBytes(inputJSON)
	if err := inputValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling user input: %w", err)
	}

	// Encode system input as JSON then compile to a CUE value and unify at "system".
	systemJSON, err := json.Marshal(system)
	if err != nil {
		return nil, fmt.Errorf("encoding system input: %w", err)
	}
	systemValue := cueCtx.CompileBytes(systemJSON)
	if err := systemValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling system input: %w", err)
	}

	// Unify template with inputs.
	unified = unified.FillPath(cue.ParsePath("input"), inputValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with user input: %w", err)
	}
	unified = unified.FillPath(cue.ParsePath("system"), systemValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with system input: %w", err)
	}

	// Require the structured output format: output.namespacedResources must exist.
	namespacedValue := unified.LookupPath(cue.ParsePath("output.namespacedResources"))
	if namespacedValue.Err() != nil || !namespacedValue.Exists() {
		return nil, fmt.Errorf("deployment template must define 'output.namespacedResources' (structured output format required)")
	}

	return evaluateStructured(unified, system.Namespace)
}

// evaluate performs synchronous CUE template evaluation.
// Templates must use the structured output format under the output: key.
// The system input (project, namespace, claims) and user input (name, image,
// tag, etc.) are encoded separately and unified with the template.
func evaluate(cueSource string, system SystemInput, user UserInput) ([]unstructured.Unstructured, error) {
	cueCtx := cuecontext.New()

	// Compile the template source.
	tmpl := cueCtx.CompileString(cueSource)
	if err := tmpl.Err(); err != nil {
		return nil, fmt.Errorf("invalid CUE template: %w", err)
	}

	// Encode user input as JSON then compile to a CUE value and unify at "input".
	inputJSON, err := json.Marshal(user)
	if err != nil {
		return nil, fmt.Errorf("encoding user input: %w", err)
	}
	inputValue := cueCtx.CompileBytes(inputJSON)
	if err := inputValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling user input: %w", err)
	}

	// Encode system input as JSON then compile to a CUE value and unify at "system".
	systemJSON, err := json.Marshal(system)
	if err != nil {
		return nil, fmt.Errorf("encoding system input: %w", err)
	}
	systemValue := cueCtx.CompileBytes(systemJSON)
	if err := systemValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling system input: %w", err)
	}

	// Unify template with the user input at the "input" path and system input
	// at the "system" path.
	unified := tmpl.FillPath(cue.ParsePath("input"), inputValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with user input: %w", err)
	}
	unified = unified.FillPath(cue.ParsePath("system"), systemValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with system input: %w", err)
	}

	// Require the structured output format: output.namespacedResources must exist.
	namespacedValue := unified.LookupPath(cue.ParsePath("output.namespacedResources"))
	if namespacedValue.Err() != nil || !namespacedValue.Exists() {
		return nil, fmt.Errorf("template must define 'output.namespacedResources' (structured output format required)")
	}

	return evaluateStructured(unified, system.Namespace)
}

// evaluateWithCueInput performs synchronous CUE template evaluation using a raw
// CUE string as input.  The cueInput is a CUE document that provides both
// "input" (user-provided values) and "system" (trusted backend values) at the
// top level.  The template source and input are compiled together so that
// cross-references (e.g. input.name used in the template) resolve correctly.
// The expected namespace is derived from system.namespace in the unified value.
func evaluateWithCueInput(cueSource, cueInput string) ([]unstructured.Unstructured, error) {
	cueCtx := cuecontext.New()

	// Compile the template source together with the CUE input document.
	// Concatenating them in a single compilation unit allows the template to
	// reference top-level identifiers (input.name, system.namespace, etc.)
	// provided by the input document.
	// The cueInput is bare CUE (no package declaration); we strip the
	// package declaration from cueSource before appending to avoid duplicate
	// package clauses in the combined source.
	combined := cueSource + "\n" + cueInput
	unified := cueCtx.CompileString(combined)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("invalid CUE template: %w", err)
	}

	// Require the structured output format.
	// System templates define output.systemNamespacedResources; deployment
	// templates define output.namespacedResources.  At minimum one of these
	// must exist.  For system template standalone preview we check for either.
	namespacedValue := unified.LookupPath(cue.ParsePath("output.namespacedResources"))
	sysNamespacedValue := unified.LookupPath(cue.ParsePath("output.systemNamespacedResources"))
	if (namespacedValue.Err() != nil || !namespacedValue.Exists()) &&
		(sysNamespacedValue.Err() != nil || !sysNamespacedValue.Exists()) {
		return nil, fmt.Errorf("template must define 'output.namespacedResources' or 'output.systemNamespacedResources' (structured output format required)")
	}

	// Extract the expected namespace from the unified system value.
	nsValue := unified.LookupPath(cue.ParsePath("system.namespace"))
	if nsValue.Err() != nil || !nsValue.Exists() {
		return nil, fmt.Errorf("cue_input must provide a 'system.namespace' field")
	}
	expectedNamespace, err := nsValue.String()
	if err != nil {
		return nil, fmt.Errorf("system.namespace must be a concrete string: %w", err)
	}

	return evaluateStructured(unified, expectedNamespace)
}

// evaluateStructured walks the output.namespacedResources, output.clusterResources,
// output.systemNamespacedResources, and output.systemClusterResources structured
// output fields and returns validated Kubernetes resources.
//
// namespacedResources structure: output.namespacedResources.<namespace>.<Kind>.<name>
// clusterResources structure:    output.clusterResources.<Kind>.<name>
// systemNamespacedResources and systemClusterResources follow the same structure
// but are only populated by system template evaluations.
func evaluateStructured(unified cue.Value, expectedNamespace string) ([]unstructured.Unstructured, error) {
	var result []unstructured.Unstructured

	// Walk output.namespacedResources: output.namespacedResources.<namespace>.<Kind>.<name>
	namespacedValue := unified.LookupPath(cue.ParsePath("output.namespacedResources"))
	if namespacedValue.Err() == nil && namespacedValue.Exists() {
		resources, err := walkNamespacedResources(namespacedValue, expectedNamespace, "output.namespacedResources")
		if err != nil {
			return nil, err
		}
		result = append(result, resources...)
	}

	// Walk output.clusterResources: output.clusterResources.<Kind>.<name>
	clusterValue := unified.LookupPath(cue.ParsePath("output.clusterResources"))
	if clusterValue.Err() == nil && clusterValue.Exists() {
		resources, err := walkClusterResources(clusterValue, "output.clusterResources")
		if err != nil {
			return nil, err
		}
		result = append(result, resources...)
	}

	// Walk output.systemNamespacedResources (populated by system templates).
	sysNamespacedValue := unified.LookupPath(cue.ParsePath("output.systemNamespacedResources"))
	if sysNamespacedValue.Err() == nil && sysNamespacedValue.Exists() {
		resources, err := walkNamespacedResources(sysNamespacedValue, expectedNamespace, "output.systemNamespacedResources")
		if err != nil {
			return nil, err
		}
		result = append(result, resources...)
	}

	// Walk output.systemClusterResources (populated by system templates).
	sysClusterValue := unified.LookupPath(cue.ParsePath("output.systemClusterResources"))
	if sysClusterValue.Err() == nil && sysClusterValue.Exists() {
		resources, err := walkClusterResources(sysClusterValue, "output.systemClusterResources")
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

