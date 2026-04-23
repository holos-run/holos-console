/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnvVar is a name/value pair carried on TemplateDefaults. Mirrors the shape
// of the existing proto EnvVar used by deployments — kept minimal here so the
// CRD schema validates the small default-value surface without pulling a
// separate Kubernetes-style EnvVar type (which would force valueFrom handling
// the template defaults path does not use).
type EnvVar struct {
	// Name of the environment variable.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Value of the environment variable. Optional.
	Value string `json:"value,omitempty"`
}

// TemplateDefaults carries optional default values that a template provides
// for deployment form fields. All fields are optional; an empty struct means
// the template provides no defaults. Only meaningful for templates authored
// in project-annotated namespaces. Field names match the existing proto
// TemplateDefaults message 1:1.
type TemplateDefaults struct {
	// Image is the default container image.
	Image string `json:"image,omitempty"`
	// Tag is the default image tag.
	Tag string `json:"tag,omitempty"`
	// Command overrides the container image ENTRYPOINT.
	Command []string `json:"command,omitempty"`
	// Args overrides the container image CMD.
	Args []string `json:"args,omitempty"`
	// Env sets default container environment variables.
	Env []EnvVar `json:"env,omitempty"`
	// Port is the default container port the application listens on.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
	// Name is the default deployment name.
	Name string `json:"name,omitempty"`
	// Description is a short human-readable description of the deployment.
	Description string `json:"description,omitempty"`
}

// LinkedTemplateRef is a (namespace, name) reference to another Template
// used in the explicit linking list. The namespace's
// `console.holos.run/resource-type` label classifies the linked template's
// hierarchy kind at render time — callers supply the namespace only.
type LinkedTemplateRef struct {
	// Namespace is the Kubernetes namespace that owns the linked template.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
	// Name is the linked template's DNS label slug.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// VersionConstraint is a semver range string (for example
	// `">=2.0.0 <3.0.0"`) that restricts which release versions of the
	// linked template are compatible. Empty means no constraint; the
	// latest version is used.
	VersionConstraint string `json:"versionConstraint,omitempty"`
}

// TemplateSpec describes the desired state of a Template. The spec carries the
// existing CUE template payload field-for-field — no CUE schema change relative
// to the former ConfigMap-backed storage. See ADR 030.
type TemplateSpec struct {
	// DisplayName is a human-readable name.
	DisplayName string `json:"displayName,omitempty"`
	// Description explains what the template produces.
	Description string `json:"description,omitempty"`
	// CueTemplate is the CUE source code the render path evaluates.
	CueTemplate string `json:"cueTemplate,omitempty"`
	// Defaults provides optional default values for deployment form
	// fields. Only meaningful for project-scope templates.
	Defaults *TemplateDefaults `json:"defaults,omitempty"`
	// Enabled controls whether this template is eligible to appear in
	// selection lists and to participate in render-time unification.
	// Disabled templates are filtered out of linkable-template pickers
	// and out of the effective unification set computed when rendering
	// a downstream template or deployment.
	Enabled bool `json:"enabled,omitempty"`
	// Version is the current semver version string (for example "1.2.3")
	// of this template. Empty means the template has no published
	// version yet.
	Version string `json:"version,omitempty"`
}

// TemplateStatus describes the observed state of a Template. Follows the
// Gateway-API status pattern recorded in ADR 030: each condition carries its
// own observedGeneration; the top-level observedGeneration tracks
// metadata.generation.
type TemplateStatus struct {
	// ObservedGeneration is the most recent generation observed for this
	// Template by the reconciler.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions represent the latest available observations of this
	// Template's state. Known condition types are Accepted, CUEValid, and Ready.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// Template is a CUE template that produces Kubernetes resource manifests.
// Templates live in a namespace whose `console.holos.run/resource-type` label
// identifies the hierarchy level (organization, folder, project). Project-scope
// templates are the only kind for which `.spec.defaults` is meaningful.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tmpl,categories=holos
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=`.spec.enabled`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Template struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TemplateSpec   `json:"spec,omitempty"`
	Status TemplateStatus `json:"status,omitempty"`
}

// TemplateList contains a list of Template.
//
// +kubebuilder:object:root=true
type TemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Template `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Template{}, &TemplateList{})
}
