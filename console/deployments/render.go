package deployments

import (
	"context"
	"encoding/json"
	"fmt"
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

// DeploymentInput is the standard input passed to CUE templates.
type DeploymentInput struct {
	Name      string        `json:"name"`
	Image     string        `json:"image"`
	Tag       string        `json:"tag"`
	Project   string        `json:"project"`
	Namespace string        `json:"namespace"`
	Command   []string      `json:"command,omitempty"`
	Args      []string      `json:"args,omitempty"`
	Env       []EnvVarInput `json:"env,omitempty"`
}

// CueRenderer evaluates CUE templates with deployment parameters.
type CueRenderer struct{}

// Render evaluates the CUE template with the given input and returns a list of
// K8s resource manifests as unstructured objects.
func (r *CueRenderer) Render(ctx context.Context, cueSource string, input DeploymentInput) ([]unstructured.Unstructured, error) {
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
		resources, err := evaluate(cueSource, input)
		ch <- result{resources, err}
	}()

	select {
	case <-evalCtx.Done():
		return nil, fmt.Errorf("CUE template evaluation timed out after %s", renderTimeout)
	case res := <-ch:
		return res.resources, res.err
	}
}

// evaluate performs synchronous CUE template evaluation.
// It supports both the structured namespaced/cluster format (preferred) and
// the legacy flat resources list format.
func evaluate(cueSource string, input DeploymentInput) ([]unstructured.Unstructured, error) {
	cueCtx := cuecontext.New()

	// Compile the template source.
	tmpl := cueCtx.CompileString(cueSource)
	if err := tmpl.Err(); err != nil {
		return nil, fmt.Errorf("invalid CUE template: %w", err)
	}

	// Encode input as JSON then compile to a CUE value.
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encoding input: %w", err)
	}
	inputValue := cueCtx.CompileBytes(inputJSON)
	if err := inputValue.Err(); err != nil {
		return nil, fmt.Errorf("compiling input: %w", err)
	}

	// Unify template with the input field.
	unified := tmpl.FillPath(cue.ParsePath("input"), inputValue)
	if err := unified.Err(); err != nil {
		return nil, fmt.Errorf("unifying template with input: %w", err)
	}

	// Check which output format the template uses.
	namespacedValue := unified.LookupPath(cue.ParsePath("namespaced"))
	if namespacedValue.Err() == nil && namespacedValue.Exists() {
		// New structured format: walk namespaced and cluster fields.
		return evaluateStructured(unified, input.Namespace)
	}

	// Legacy flat list format.
	resourcesValue := unified.LookupPath(cue.ParsePath("resources"))
	if err := resourcesValue.Err(); err != nil {
		return nil, fmt.Errorf("extracting resources field: %w", err)
	}

	var rawResources []map[string]any
	if err := resourcesValue.Decode(&rawResources); err != nil {
		return nil, fmt.Errorf("decoding resources: %w", err)
	}

	return validateResources(rawResources, input.Namespace)
}

// evaluateStructured walks the namespaced and cluster structured output fields
// and returns validated Kubernetes resources.
//
// namespaced structure: namespaced.<namespace>.<Kind>.<name>
// cluster structure:    cluster.<Kind>.<name>
func evaluateStructured(unified cue.Value, expectedNamespace string) ([]unstructured.Unstructured, error) {
	var result []unstructured.Unstructured

	// Walk namespaced resources: namespaced.<namespace>.<Kind>.<name>
	namespacedValue := unified.LookupPath(cue.ParsePath("namespaced"))
	if namespacedValue.Err() == nil && namespacedValue.Exists() {
		nsIter, err := namespacedValue.Fields()
		if err != nil {
			return nil, fmt.Errorf("iterating namespaced keys: %w", err)
		}
		for nsIter.Next() {
			nsKey := nsIter.Selector().Unquoted()
			kindIter, err := nsIter.Value().Fields()
			if err != nil {
				return nil, fmt.Errorf("iterating Kind keys under namespace %q: %w", nsKey, err)
			}
			for kindIter.Next() {
				kindKey := kindIter.Selector().Unquoted()
				nameIter, err := kindIter.Value().Fields()
				if err != nil {
					return nil, fmt.Errorf("iterating name keys under %q/%q: %w", nsKey, kindKey, err)
				}
				for nameIter.Next() {
					nameKey := nameIter.Selector().Unquoted()
					var raw map[string]any
					if err := nameIter.Value().Decode(&raw); err != nil {
						return nil, fmt.Errorf("decoding namespaced/%s/%s/%s: %w", nsKey, kindKey, nameKey, err)
					}
					u := unstructured.Unstructured{Object: raw}

					// Enforce struct-key / metadata consistency.
					if u.GetNamespace() != nsKey {
						return nil, fmt.Errorf("namespaced/%s/%s/%s: metadata.namespace %q does not match struct key %q",
							nsKey, kindKey, nameKey, u.GetNamespace(), nsKey)
					}
					if u.GetKind() != kindKey {
						return nil, fmt.Errorf("namespaced/%s/%s/%s: kind %q does not match struct key %q",
							nsKey, kindKey, nameKey, u.GetKind(), kindKey)
					}
					if u.GetName() != nameKey {
						return nil, fmt.Errorf("namespaced/%s/%s/%s: metadata.name %q does not match struct key %q",
							nsKey, kindKey, nameKey, u.GetName(), nameKey)
					}

					// Enforce project namespace constraint.
					if u.GetNamespace() != expectedNamespace {
						return nil, fmt.Errorf("namespaced resource %s/%s: namespace %q does not match project namespace %q",
							u.GetKind(), u.GetName(), u.GetNamespace(), expectedNamespace)
					}

					// Run common resource validations.
					if err := validateResource(u); err != nil {
						return nil, err
					}

					result = append(result, u)
				}
			}
		}
	}

	// Walk cluster-scoped resources: cluster.<Kind>.<name>
	clusterValue := unified.LookupPath(cue.ParsePath("cluster"))
	if clusterValue.Err() == nil && clusterValue.Exists() {
		kindIter, err := clusterValue.Fields()
		if err != nil {
			return nil, fmt.Errorf("iterating cluster Kind keys: %w", err)
		}
		for kindIter.Next() {
			kindKey := kindIter.Selector().Unquoted()
			nameIter, err := kindIter.Value().Fields()
			if err != nil {
				return nil, fmt.Errorf("iterating name keys under cluster/%q: %w", kindKey, err)
			}
			for nameIter.Next() {
				nameKey := nameIter.Selector().Unquoted()
				var raw map[string]any
				if err := nameIter.Value().Decode(&raw); err != nil {
					return nil, fmt.Errorf("decoding cluster/%s/%s: %w", kindKey, nameKey, err)
				}
				u := unstructured.Unstructured{Object: raw}

				// Enforce struct-key / metadata consistency.
				if u.GetKind() != kindKey {
					return nil, fmt.Errorf("cluster/%s/%s: kind %q does not match struct key %q",
						kindKey, nameKey, u.GetKind(), kindKey)
				}
				if u.GetName() != nameKey {
					return nil, fmt.Errorf("cluster/%s/%s: metadata.name %q does not match struct key %q",
						kindKey, nameKey, u.GetName(), nameKey)
				}

				// Cluster-scoped resources must NOT have a namespace.
				if u.GetNamespace() != "" {
					return nil, fmt.Errorf("cluster resource %s/%s: must not have metadata.namespace", kindKey, nameKey)
				}

				result = append(result, u)
			}
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

// validateResources validates each resource against safety constraints.
func validateResources(rawResources []map[string]any, expectedNamespace string) ([]unstructured.Unstructured, error) {
	result := make([]unstructured.Unstructured, 0, len(rawResources))
	for i, raw := range rawResources {
		u := unstructured.Unstructured{Object: raw}

		// Require apiVersion and kind.
		if u.GetAPIVersion() == "" {
			return nil, fmt.Errorf("resource[%d]: missing apiVersion", i)
		}
		kind := u.GetKind()
		if kind == "" {
			return nil, fmt.Errorf("resource[%d]: missing kind", i)
		}
		if u.GetName() == "" {
			return nil, fmt.Errorf("resource[%d]: missing metadata.name", i)
		}

		// Enforce kind allowlist.
		if !allowedKindSet[kind] {
			return nil, fmt.Errorf("resource[%d] kind %q is not allowed; permitted kinds: Deployment, Service, ServiceAccount, Role, RoleBinding, HTTPRoute, ConfigMap, Secret", i, kind)
		}

		// Enforce namespace constraint.
		ns := u.GetNamespace()
		if ns == "" {
			return nil, fmt.Errorf("resource[%d] %s/%s: missing metadata.namespace", i, kind, u.GetName())
		}
		if ns != expectedNamespace {
			return nil, fmt.Errorf("resource[%d] %s/%s: namespace %q does not match project namespace %q", i, kind, u.GetName(), ns, expectedNamespace)
		}

		// Enforce managed-by label.
		labels := u.GetLabels()
		if labels["app.kubernetes.io/managed-by"] != "console.holos.run" {
			return nil, fmt.Errorf("resource[%d] %s/%s: missing required label app.kubernetes.io/managed-by=console.holos.run", i, kind, u.GetName())
		}

		result = append(result, u)
	}
	return result, nil
}
