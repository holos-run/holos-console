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

// TemplateRequirementTargetRef identifies a render target that is required to
// have rendered successfully before the TemplateRequirement's owning template
// is eligible to render. Mirrors the TemplatePolicyBindingTargetRef shape and
// supports the HOL-767 "*" wildcard on the ProjectName field.
type TemplateRequirementTargetRef struct {
	// Kind discriminates between ProjectTemplate and Deployment targets.
	Kind TemplatePolicyBindingTargetKind `json:"kind"`
	// Name is the target's DNS label slug within ProjectName. The literal
	// wildcard "*" matches all targets of the given Kind within ProjectName.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// ProjectName is the project that owns the target. The literal wildcard
	// "*" matches all projects reachable via the ancestor chain from the
	// namespace that owns this TemplateRequirement (HOL-767 semantics).
	// +kubebuilder:validation:MinLength=1
	ProjectName string `json:"projectName"`
}

// TemplateRequirementSpec describes the desired state of a
// TemplateRequirement.
type TemplateRequirementSpec struct {
	// Requires identifies the template that must have rendered successfully
	// before the targets enumerated in TargetRefs are allowed to render.
	// The template may live in any namespace reachable via a TemplateGrant.
	Requires LinkedTemplateRef `json:"requires"`
	// TargetRefs enumerates the render targets that are gated on Requires.
	// At least one target is required; duplicates are rejected by the
	// reconciler.
	// +kubebuilder:validation:MinItems=1
	// +listType=atomic
	TargetRefs []TemplateRequirementTargetRef `json:"targetRefs"`
	// CascadeDelete controls whether deleting the Requires template
	// triggers deletion of the target resources enumerated in TargetRefs.
	// Defaults to true when nil.
	// +kubebuilder:default=true
	CascadeDelete *bool `json:"cascadeDelete,omitempty"`
}

// TemplateRequirementStatus describes the observed state of a
// TemplateRequirement. Follows the Gateway-API status pattern recorded in
// ADR 030.
type TemplateRequirementStatus struct {
	// ObservedGeneration is the most recent generation observed for this
	// TemplateRequirement by the reconciler.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions represent the latest available observations of this
	// TemplateRequirement's state. Known condition types are Accepted,
	// ResolvedRefs, and Ready.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// TemplateRequirement gates a set of render targets on a prerequisite
// template. Before any target in TargetRefs is eligible to render, the
// template identified by Requires must have rendered successfully. The
// TargetRef shape and wildcard semantics mirror TemplatePolicyBindingTargetRef
// and the HOL-767 "*" wildcards exactly; use that type as the gold reference.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=treq,categories=holos
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Resolved",type=string,JSONPath=`.status.conditions[?(@.type=="ResolvedRefs")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type TemplateRequirement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TemplateRequirementSpec   `json:"spec,omitempty"`
	Status TemplateRequirementStatus `json:"status,omitempty"`
}

// TemplateRequirementList contains a list of TemplateRequirement.
//
// +kubebuilder:object:root=true
type TemplateRequirementList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TemplateRequirement `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TemplateRequirement{}, &TemplateRequirementList{})
}
