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

// DeploymentInput is the standard input passed to CUE templates.
type DeploymentInput struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	Tag       string `json:"tag"`
	Project   string `json:"project"`
	Namespace string `json:"namespace"`
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

	// Extract the resources field.
	resourcesValue := unified.LookupPath(cue.ParsePath("resources"))
	if err := resourcesValue.Err(); err != nil {
		return nil, fmt.Errorf("extracting resources field: %w", err)
	}

	// Decode the resources list.
	var rawResources []map[string]interface{}
	if err := resourcesValue.Decode(&rawResources); err != nil {
		return nil, fmt.Errorf("decoding resources: %w", err)
	}

	return validateResources(rawResources, input.Namespace)
}

// validateResources validates each resource against safety constraints.
func validateResources(rawResources []map[string]interface{}, expectedNamespace string) ([]unstructured.Unstructured, error) {
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
