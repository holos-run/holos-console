package v1alpha2

// Organization represents the root of a configuration management hierarchy.
// In v1alpha2, Organization.Spec remains empty — all hierarchy context is
// carried via labels on the Kubernetes Namespace that backs the organization.
type Organization struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata Metadata `json:"metadata" yaml:"metadata" cue:"metadata"`
}

// Folder represents an intermediate grouping level in the organization
// hierarchy between an Organization and a Project (or between two Folder
// levels). A Folder is stored as a Kubernetes Namespace with labels that
// identify its type, parent, and root organization.
//
// Folders are optional. An Organization may contain Projects directly without
// any Folders. Up to three Folder levels are supported between any Organization
// and Project (ADR 016 Decision 4, ADR 020 Decision 5).
type Folder struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata Metadata   `json:"metadata" yaml:"metadata" cue:"metadata"`
	Spec     FolderSpec `json:"spec"     yaml:"spec"     cue:"spec"`
}

// FolderSpec carries the mutable configuration of a Folder.
type FolderSpec struct {
	// DisplayName is a human-readable label for the folder shown in the UI.
	DisplayName string `json:"displayName" yaml:"displayName" cue:"displayName"`

	// Organization is the root organization name (slug) for this folder. This
	// is a convenience field; the [AnnotationOrganization] label on the backing
	// Namespace is the authoritative source. Stored here so CUE templates can
	// reference the root org without walking the full ancestor chain.
	Organization string `json:"organization" yaml:"organization" cue:"organization"`

	// Parent is the slug (name) of the immediate parent. For a top-level folder
	// this is the organization slug; for nested folders it is the parent folder
	// slug. The backing Namespace carries [AnnotationParent] which stores the
	// parent's Kubernetes namespace name — the runtime resolver maps between
	// these two representations.
	//
	// An empty Parent value indicates a top-level folder (immediate child of
	// the organization).
	Parent string `json:"parent,omitempty" yaml:"parent,omitempty" cue:"parent?"`
}

// Project represents a project within an organization, optionally nested
// inside one or more Folders (ADR 020 Decision 2).
type Project struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata Metadata    `json:"metadata" yaml:"metadata" cue:"metadata"`
	Spec     ProjectSpec `json:"spec"     yaml:"spec"     cue:"spec"`
}

// ProjectSpec defines the specification for a Project.
type ProjectSpec struct {
	// Organization is the root organization name (slug). Retained for
	// convenience so callers do not need to walk the full ancestor chain to
	// identify the owning organization.
	Organization string `json:"organization" yaml:"organization" cue:"organization"`

	// Parent is the slug of the immediate parent. For a project that lives
	// directly under an organization this equals the organization slug. For a
	// project nested inside one or more folders this is the containing folder
	// slug. The corresponding Namespace carries [AnnotationParent] with the
	// Kubernetes namespace name of the parent.
	Parent string `json:"parent" yaml:"parent" cue:"parent"`
}
