package v1alpha1

// Resource is an unstructured Kubernetes resource manifest.
type Resource map[string]interface{}

// PlatformResources holds resources managed by platform and security engineers.
// The renderer reads platformResources only from templates at the folder level
// or above. A project-level template may define values under platformResources,
// but the renderer does not read them — this is a hard boundary enforced in Go
// code (ADR 016 Decision 6).
type PlatformResources struct {
	// NamespacedResources maps namespace -> kind -> name -> resource manifest.
	NamespacedResources map[string]map[string]map[string]Resource `json:"namespacedResources,omitempty" yaml:"namespacedResources,omitempty" cue:"namespacedResources?"`
	// ClusterResources maps kind -> name -> resource manifest.
	ClusterResources map[string]map[string]Resource `json:"clusterResources,omitempty"    yaml:"clusterResources,omitempty"    cue:"clusterResources?"`
}

// ProjectResources holds resources managed by product engineers.
// Templates at any level can define values for projectResources. In CUE, all
// values — concrete data, constraints, types — are unified together. There is
// no separate "constrain" operation; it is all unification (ADR 016 Decision 6).
type ProjectResources struct {
	// NamespacedResources maps namespace -> kind -> name -> resource manifest.
	NamespacedResources map[string]map[string]map[string]Resource `json:"namespacedResources,omitempty" yaml:"namespacedResources,omitempty" cue:"namespacedResources?"`
	// ClusterResources maps kind -> name -> resource manifest.
	ClusterResources map[string]map[string]Resource `json:"clusterResources,omitempty"    yaml:"clusterResources,omitempty"    cue:"clusterResources?"`
}
