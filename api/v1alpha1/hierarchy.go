package v1alpha1

// Organization represents an organization in the configuration management hierarchy.
type Organization struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata Metadata `json:"metadata" yaml:"metadata" cue:"metadata"`
}

// Project represents a project within an organization.
type Project struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata Metadata    `json:"metadata" yaml:"metadata" cue:"metadata"`
	Spec     ProjectSpec `json:"spec"     yaml:"spec"     cue:"spec"`
}

// ProjectSpec defines the specification for a Project.
type ProjectSpec struct {
	Organization string `json:"organization" yaml:"organization" cue:"organization"`
}
