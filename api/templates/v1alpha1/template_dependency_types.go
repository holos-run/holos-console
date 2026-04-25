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

// TemplateDependencySpec describes the desired state of a TemplateDependency.
// A TemplateDependency records that a Dependent template requires a Requires
// template to be rendered first (or in the same render pass). When
// CascadeDelete is true (the default) deleting the Requires template causes
// the Dependent template to be deleted as well.
type TemplateDependencySpec struct {
	// Dependent identifies the template that depends on Requires. The
	// template must live in the same namespace as the TemplateDependency.
	Dependent LinkedTemplateRef `json:"dependent"`
	// Requires identifies the template that Dependent needs. The template
	// may live in any namespace reachable via a TemplateGrant.
	Requires LinkedTemplateRef `json:"requires"`
	// CascadeDelete controls whether deleting the Requires template
	// triggers deletion of the Dependent template. Defaults to true when
	// nil, matching the safest conservative posture: dependencies are
	// owner-like relationships unless explicitly decoupled.
	// +kubebuilder:default=true
	CascadeDelete *bool `json:"cascadeDelete,omitempty"`
}

// TemplateDependencyStatus describes the observed state of a
// TemplateDependency. Follows the Gateway-API status pattern recorded in
// ADR 030.
type TemplateDependencyStatus struct {
	// ObservedGeneration is the most recent generation observed for this
	// TemplateDependency by the reconciler.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions represent the latest available observations of this
	// TemplateDependency's state. Known condition types are Accepted,
	// ResolvedRefs, and Ready.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// TemplateDependency records a dependency between two Templates: Dependent
// requires Requires. The reconciler uses this to gate render passes and to
// implement optional cascade-delete semantics.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tdep,categories=holos
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Resolved",type=string,JSONPath=`.status.conditions[?(@.type=="ResolvedRefs")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type TemplateDependency struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TemplateDependencySpec   `json:"spec,omitempty"`
	Status TemplateDependencyStatus `json:"status,omitempty"`
}

// TemplateDependencyList contains a list of TemplateDependency.
//
// +kubebuilder:object:root=true
type TemplateDependencyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TemplateDependency `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TemplateDependency{}, &TemplateDependencyList{})
}
