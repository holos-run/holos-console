package v1alpha1

// TypeMeta identifies the API version and kind of a resource.
// Every top-level configuration resource embeds TypeMeta so that consumers can
// dispatch on apiVersion and kind without knowing the concrete Go type.
type TypeMeta struct {
	// APIVersion is the versioned schema identifier, e.g. "console.holos.run/v1alpha1".
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

// ResourceSetSpec groups the input and output sections of a ResourceSet.
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
}
