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

// TemplateGrantFromRef identifies the namespace(s) that are permitted to
// reference templates owned by the TemplateGrant's namespace. Mirrors the
// Gateway API ReferenceGrant From structure.
//
// Exactly one of Namespace or NamespaceSelector must be set, unless Namespace
// is the literal wildcard "*", in which case NamespaceSelector is ignored.
type TemplateGrantFromRef struct {
	// Namespace is the Kubernetes namespace from which cross-namespace
	// template references are allowed. Use the literal wildcard "*" to
	// permit all namespaces. Must be non-empty.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
	// NamespaceSelector is a label selector applied to namespaces when
	// Namespace is not the literal wildcard "*". Only namespaces matching
	// this selector AND whose name equals Namespace (or when Namespace is
	// "*") are permitted. Optional.
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

// TemplateGrantSpec describes the desired state of a TemplateGrant.
// A TemplateGrant lives in the namespace that owns the templates being shared
// (the "from" side) and names one or more source namespaces that are allowed
// to reference those templates.
type TemplateGrantSpec struct {
	// From lists the namespaces whose template references are authorized by
	// this grant. An empty list denies all cross-namespace references.
	// +kubebuilder:validation:MinItems=1
	// +listType=atomic
	From []TemplateGrantFromRef `json:"from"`
	// To optionally narrows which templates in this namespace may be
	// referenced. When omitted, all templates in the namespace are
	// reachable by the permitted From namespaces.
	// +listType=atomic
	To []LinkedTemplateRef `json:"to,omitempty"`
}

// TemplateGrantStatus describes the observed state of a TemplateGrant.
// Follows the Gateway-API status pattern recorded in ADR 030.
type TemplateGrantStatus struct {
	// ObservedGeneration is the most recent generation observed for this
	// TemplateGrant by the reconciler.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions represent the latest available observations of this
	// TemplateGrant's state. Known condition types are Accepted and Ready.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// TemplateGrant authorizes cross-namespace template references, mirroring
// the Gateway API ReferenceGrant pattern. A TemplateGrant lives in the
// namespace that owns the templates being referenced; without a TemplateGrant
// in the owning namespace a cross-namespace LinkedTemplateRef is denied.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tg,categories=holos
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type TemplateGrant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TemplateGrantSpec   `json:"spec,omitempty"`
	Status TemplateGrantStatus `json:"status,omitempty"`
}

// TemplateGrantList contains a list of TemplateGrant.
//
// +kubebuilder:object:root=true
type TemplateGrantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TemplateGrant `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TemplateGrant{}, &TemplateGrantList{})
}
