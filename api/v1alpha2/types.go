package v1alpha2

// TypeMeta identifies the API version and kind of a resource.
// Every top-level configuration resource embeds TypeMeta so that consumers can
// dispatch on apiVersion and kind without knowing the concrete Go type.
type TypeMeta struct {
	// APIVersion is the versioned schema identifier, e.g. "console.holos.run/v1alpha2".
	APIVersion string `json:"apiVersion" yaml:"apiVersion" cue:"apiVersion"`
	// Kind is the resource type name, e.g. "ResourceSet".
	Kind string `json:"kind"       yaml:"kind"       cue:"kind"`
}

// Metadata provides identifying information for a configuration resource.
// It intentionally does not replicate Kubernetes ObjectMeta; it carries only
// what the configuration management system needs.
type Metadata struct {
	// Name is the unique identifier of the resource within its scope.
	Name string `json:"name"                  yaml:"name"                  cue:"name"`
	// Annotations carry optional key-value metadata. Used for display names,
	// descriptions, and grant storage.
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty" cue:"annotations?"`
}

// ResourceSet is the top-level resource type for the configuration management
// API. It represents the complete set of Kubernetes resources produced by
// unifying templates from all hierarchy levels with their inputs.
type ResourceSet struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata Metadata        `json:"metadata" yaml:"metadata" cue:"metadata"`
	Spec     ResourceSetSpec `json:"spec"     yaml:"spec"     cue:"spec"`
}

// ResourceSetSpec groups the top-level sections of a ResourceSet: defaults,
// platformInput, projectInput (inputs), platformResources, projectResources
// (generated K8s manifests), and the optional output section (values the
// template publishes to the UI, such as the deployment URL).
type ResourceSetSpec struct {
	// Defaults carries optional default values for ProjectInput fields.
	// Template authors specify concrete values in the CUE template's defaults
	// block; these pre-fill the Create Deployment form and serve as CUE
	// defaults that users can override at render time.
	//
	// Example defaults for go-httpbin:
	//
	//	Defaults: &ProjectInput{
	//	    Name:        "httpbin",
	//	    Image:       "ghcr.io/mccutchen/go-httpbin",
	//	    Tag:         "2.21.0",
	//	    Description: "A simple HTTP Request & Response Service",
	//	    Port:        8080,
	//	}
	Defaults *ProjectInput `json:"defaults,omitempty" yaml:"defaults,omitempty" cue:"defaults?"`
	// PlatformInput is the trusted context set by the backend and platform engineers.
	PlatformInput PlatformInput `json:"platformInput"     yaml:"platformInput"     cue:"platformInput"`
	// ProjectInput is the user-provided deployment parameters.
	ProjectInput ProjectInput `json:"projectInput"      yaml:"projectInput"      cue:"projectInput"`
	// PlatformResources is the output collection for platform-managed resources.
	PlatformResources PlatformResources `json:"platformResources" yaml:"platformResources" cue:"platformResources"`
	// ProjectResources is the output collection for project-managed resources.
	ProjectResources ProjectResources `json:"projectResources"  yaml:"projectResources"  cue:"projectResources"`
	// Output carries optional values the template wants to publish to the UI.
	// Platform templates assign concrete values inside a top-level `output`
	// block (e.g. `output: url: "https://example.com"`); the render pipeline
	// surfaces the evaluated Output on the deployment detail view. The field
	// is a pointer so the section can be absent entirely when a template
	// does not declare one.
	Output *Output `json:"output,omitempty"  yaml:"output,omitempty"  cue:"output?"`
}

// Output carries values a template publishes for UI consumption. It is the
// counterpart to the inputs on [ResourceSetSpec]: templates may unify values
// into the top-level `output` block to expose them to the deployment detail
// view without stuffing them into annotations or labels.
type Output struct {
	// Url is the primary URL (e.g. the HTTPS URL of the ingress route) that
	// the UI should show for a deployment. Left empty when the template has
	// no meaningful URL to publish.
	Url string `json:"url,omitempty" yaml:"url,omitempty" cue:"url?"`
}
