package v1alpha1

// Resource is an unstructured Kubernetes resource manifest.
type Resource map[string]interface{}

// PlatformResources holds resources managed by platform and security engineers.
// These resources typically live outside the project namespace (e.g., in the
// gateway namespace or at cluster scope) or are platform-mandated resources
// within the project namespace that project templates cannot override.
type PlatformResources struct {
	// NamespacedResources maps namespace -> kind -> name -> resource manifest.
	NamespacedResources map[string]map[string]map[string]Resource `json:"namespacedResources,omitempty" yaml:"namespacedResources,omitempty" cue:"namespacedResources?"`
	// ClusterResources maps kind -> name -> resource manifest.
	ClusterResources map[string]map[string]Resource `json:"clusterResources,omitempty"    yaml:"clusterResources,omitempty"    cue:"clusterResources?"`
}

// ProjectResources holds resources managed by product engineers.
// These resources live within the project namespace. A project-level template
// writes to this collection.
type ProjectResources struct {
	// NamespacedResources maps namespace -> kind -> name -> resource manifest.
	NamespacedResources map[string]map[string]map[string]Resource `json:"namespacedResources,omitempty" yaml:"namespacedResources,omitempty" cue:"namespacedResources?"`
	// ClusterResources maps kind -> name -> resource manifest.
	ClusterResources map[string]map[string]Resource `json:"clusterResources,omitempty"    yaml:"clusterResources,omitempty"    cue:"clusterResources?"`
}
