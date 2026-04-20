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

// LinkedTemplatePolicyRef is a (namespace, name) reference to a
// TemplatePolicy carried by a TemplatePolicyBinding. Mirrors the existing
// proto LinkedTemplatePolicyRef message. A binding references exactly one
// policy; the referenced policy must live in a namespace the binding's own
// namespace can reach (ancestor chain or same namespace). Project
// namespaces are rejected by the ValidatingAdmissionPolicy that ships
// alongside TemplatePolicy.
type LinkedTemplatePolicyRef struct {
	// Namespace is the Kubernetes namespace that owns the referenced
	// TemplatePolicy. Must be an organization or folder namespace —
	// TemplatePolicy cannot live in a project namespace.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
	// Name is the referenced TemplatePolicy's DNS label slug.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// TemplatePolicyBindingTargetKind discriminates between the two explicit
// render targets a binding can name: a project-scope template (shared by
// every deployment in a project) or a single deployment.
//
// +kubebuilder:validation:Enum=ProjectTemplate;Deployment
type TemplatePolicyBindingTargetKind string

const (
	// TemplatePolicyBindingTargetKindProjectTemplate targets a project-scope
	// Template. Name is the template's DNS label slug within its owning
	// project; ProjectName is the project that owns the template.
	TemplatePolicyBindingTargetKindProjectTemplate TemplatePolicyBindingTargetKind = "ProjectTemplate"
	// TemplatePolicyBindingTargetKindDeployment targets a single Deployment.
	// Name is the deployment's DNS label slug within its owning project;
	// ProjectName is the project that owns the deployment.
	TemplatePolicyBindingTargetKindDeployment TemplatePolicyBindingTargetKind = "Deployment"
)

// TemplatePolicyBindingTargetRef identifies one explicit render target a
// binding attaches its policy to. Project-scope resources are disambiguated
// by (projectName, name).
type TemplatePolicyBindingTargetRef struct {
	// Kind discriminates between ProjectTemplate and Deployment targets.
	Kind TemplatePolicyBindingTargetKind `json:"kind"`
	// Name is the target's DNS label slug within projectName.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// ProjectName is the project that owns the target.
	// +kubebuilder:validation:MinLength=1
	ProjectName string `json:"projectName"`
}

// TemplatePolicyBindingSpec describes the desired state of a
// TemplatePolicyBinding. target_refs carries the HOL-590 binding contract
// unchanged.
type TemplatePolicyBindingSpec struct {
	// DisplayName is a human-readable name for UI presentation.
	DisplayName string `json:"displayName,omitempty"`
	// Description explains what the binding attaches and why.
	Description string `json:"description,omitempty"`
	// PolicyRef identifies the TemplatePolicy this binding attaches. A
	// binding references exactly one policy; use multiple bindings to
	// attach multiple policies to overlapping target sets.
	PolicyRef LinkedTemplatePolicyRef `json:"policyRef"`
	// TargetRefs enumerates every render target the referenced policy
	// applies to. Order is not significant; duplicates (same
	// (kind, projectName, name) triple) are rejected by the reconciler.
	// +kubebuilder:validation:MinItems=1
	// +listType=atomic
	TargetRefs []TemplatePolicyBindingTargetRef `json:"targetRefs"`
}

// TemplatePolicyBindingStatus describes the observed state of a
// TemplatePolicyBinding. Follows the Gateway-API status pattern recorded in
// ADR 030.
type TemplatePolicyBindingStatus struct {
	// ObservedGeneration is the most recent generation observed for this
	// TemplatePolicyBinding by the reconciler.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions represent the latest available observations of this
	// TemplatePolicyBinding's state. Known condition types are Accepted,
	// ResolvedRefs, and Ready. The ResolvedRefs condition name and
	// reason string are deliberately identical to the Gateway API
	// HTTPRoute.status ResolvedRefs condition — platform operators
	// already understand its meaning.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// TemplatePolicyBinding binds a single TemplatePolicy to an explicit list of
// project templates and/or deployments (ADR 029 / HOL-590). Bindings live
// only in namespaces whose `console.holos.run/resource-type` label is
// `organization` or `folder`; creation in a project-annotated namespace is
// rejected by the ValidatingAdmissionPolicy shipped alongside this CRD
// (see config/admission/).
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tpb,categories=holos
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Resolved",type=string,JSONPath=`.status.conditions[?(@.type=="ResolvedRefs")].status`
// +kubebuilder:printcolumn:name="Policy",type=string,JSONPath=`.spec.policyRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type TemplatePolicyBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TemplatePolicyBindingSpec   `json:"spec,omitempty"`
	Status TemplatePolicyBindingStatus `json:"status,omitempty"`
}

// TemplatePolicyBindingList contains a list of TemplatePolicyBinding.
//
// +kubebuilder:object:root=true
type TemplatePolicyBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TemplatePolicyBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TemplatePolicyBinding{}, &TemplatePolicyBindingList{})
}
