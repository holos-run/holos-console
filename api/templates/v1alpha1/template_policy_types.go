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

// TemplatePolicyKind discriminates between REQUIRE and EXCLUDE rules.
//
// +kubebuilder:validation:Enum=Require;Exclude
type TemplatePolicyKind string

const (
	// TemplatePolicyKindRequire causes the referenced template to be injected
	// into the effective ref set when a deployment or project template
	// matching a TemplatePolicyBinding is rendered.
	TemplatePolicyKindRequire TemplatePolicyKind = "Require"
	// TemplatePolicyKindExclude causes the referenced template to be removed
	// from the effective ref set when a matching deployment or project
	// template is rendered, even if it would otherwise be linked.
	TemplatePolicyKindExclude TemplatePolicyKind = "Exclude"
)

// TemplatePolicyRule binds a kind and a template reference into a single rule.
// A TemplatePolicy may carry many rules; which render targets a rule applies
// to is decided entirely by TemplatePolicyBinding objects.
type TemplatePolicyRule struct {
	// Kind is Require or Exclude.
	Kind TemplatePolicyKind `json:"kind"`
	// Template identifies the template this rule applies to. The
	// referenced template may live in any scope the owning policy's
	// scope can reach.
	Template LinkedTemplateRef `json:"template"`
}

// TemplatePolicySpec describes the desired state of a TemplatePolicy.
type TemplatePolicySpec struct {
	// DisplayName is a human-readable name for UI presentation.
	DisplayName string `json:"displayName,omitempty"`
	// Description explains what the policy enforces.
	Description string `json:"description,omitempty"`
	// Rules are the Require/Exclude rules this policy enforces. Rules are
	// evaluated independently and their effects combine in the
	// render-time resolver.
	// +kubebuilder:validation:MinItems=1
	// +listType=atomic
	Rules []TemplatePolicyRule `json:"rules"`
}

// TemplatePolicyStatus describes the observed state of a TemplatePolicy.
// Follows the Gateway-API status pattern recorded in ADR 030.
type TemplatePolicyStatus struct {
	// ObservedGeneration is the most recent generation observed for this
	// TemplatePolicy by the reconciler.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions represent the latest available observations of this
	// TemplatePolicy's state. Known condition types are Accepted and
	// Ready.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// TemplatePolicy declares Require/Exclude rules over Template references.
// Policies live only in namespaces whose `console.holos.run/resource-type`
// label is `organization` or `folder`; creation in a project-annotated
// namespace is rejected by the ValidatingAdmissionPolicy shipped alongside
// this CRD (see config/admission/).
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tpol,categories=holos
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Rules",type=integer,JSONPath=`.spec.rules[*]`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type TemplatePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TemplatePolicySpec   `json:"spec,omitempty"`
	Status TemplatePolicyStatus `json:"status,omitempty"`
}

// TemplatePolicyList contains a list of TemplatePolicy.
//
// +kubebuilder:object:root=true
type TemplatePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TemplatePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TemplatePolicy{}, &TemplatePolicyList{})
}
