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

// Deployment condition types.
const (
	// ConditionTypeAccepted tracks whether the reconciler parsed .spec and
	// accepted it, or rejected it with a typed reason.
	ConditionTypeAccepted = "Accepted"
	// ConditionTypeResolvedRefs tracks whether every referenced object needed
	// for reconciliation resolved to an existing, compatible object.
	ConditionTypeResolvedRefs = "ResolvedRefs"
	// ConditionTypeRendered tracks whether CUE evaluation completed and
	// produced the desired Kubernetes object set for the Deployment.
	ConditionTypeRendered = "Rendered"
	// ConditionTypeApplied tracks whether the rendered Kubernetes object set
	// was reconciled to the cluster with server-side apply.
	ConditionTypeApplied = "Applied"
	// ConditionTypeReady is the aggregate: Accepted, ResolvedRefs, Rendered,
	// and Applied are all True.
	ConditionTypeReady = "Ready"
)

// Deployment condition reasons.
const (
	DeploymentReasonRenderSucceeded         = "RenderSucceeded"
	DeploymentReasonRenderFailed            = "RenderFailed"
	DeploymentReasonAncestorTemplateMissing = "AncestorTemplateMissing"
	DeploymentReasonApplySucceeded          = "ApplySucceeded"
	DeploymentReasonApplyFailed             = "ApplyFailed"
)

// Deployment condition reason aliases kept for the HOL-1098 API contract.
const (
	ReasonRenderSucceeded         = DeploymentReasonRenderSucceeded
	ReasonRenderFailed            = DeploymentReasonRenderFailed
	ReasonAncestorTemplateMissing = DeploymentReasonAncestorTemplateMissing
	ReasonApplySucceeded          = DeploymentReasonApplySucceeded
	ReasonApplyFailed             = DeploymentReasonApplyFailed
)

// DeploymentTemplateRef identifies the Template used to render this
// Deployment. Namespace is the namespace that owns the Template; Name is the
// Template's DNS label slug.
type DeploymentTemplateRef struct {
	// Namespace is the Kubernetes namespace that owns the referenced Template.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
	// Name is the referenced Template's DNS label slug.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// DeploymentSpec describes the desired state of a Deployment. The spec
// captures the existing proto-defined Deployment shape field-for-field so
// both the proto store and the CR can coexist during the dual-write
// transition (Phase 3, HOL-957). Fields are derived from
// proto/holos/console/v1/deployments.proto and console/deployments/handler.go.
type DeploymentSpec struct {
	// ProjectName is the project that owns this deployment. Corresponds to
	// the proto Deployment.project field.
	// +kubebuilder:validation:MinLength=1
	ProjectName string `json:"projectName"`
	// TemplateRef identifies the Template used to render this Deployment.
	TemplateRef DeploymentTemplateRef `json:"templateRef"`
	// VersionConstraint is a semver range string (for example ">=2.0.0
	// <3.0.0") that restricts which release versions of the linked template
	// are compatible. Empty means no constraint; the latest version is used.
	VersionConstraint string `json:"versionConstraint,omitempty"`
	// DisplayName is a human-readable name for UI presentation.
	DisplayName string `json:"displayName,omitempty"`
	// Description explains the deployment's purpose.
	Description string `json:"description,omitempty"`
	// Image is the container image.
	Image string `json:"image,omitempty"`
	// Tag is the container image tag.
	Tag string `json:"tag,omitempty"`
	// Command overrides the container image ENTRYPOINT.
	// +listType=atomic
	Command []string `json:"command,omitempty"`
	// Args overrides the container image CMD.
	// +listType=atomic
	Args []string `json:"args,omitempty"`
	// Port is the container port the application listens on.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
}

// DeploymentStatus describes the observed state of a Deployment. Follows the
// Gateway-API status pattern recorded in ADR 030.
type DeploymentStatus struct {
	// ObservedGeneration is the most recent generation observed for this
	// Deployment by the reconciler.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions represent the latest available observations of this
	// Deployment's state. Known condition types are Accepted, ResolvedRefs,
	// Rendered, Applied, and Ready.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// Deployment represents a deployed application instance managed by the
// holos-console controller. The CRD shape mirrors the existing proto-defined
// Deployment message so both the ConfigMap-backed proto store and the CR can
// coexist during the dual-write transition introduced in HOL-957 (Phase 3).
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=dep,categories=holos
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.projectName`
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="Tag",type=string,JSONPath=`.spec.tag`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Deployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeploymentSpec   `json:"spec,omitempty"`
	Status DeploymentStatus `json:"status,omitempty"`
}

// DeploymentList contains a list of Deployment.
//
// +kubebuilder:object:root=true
type DeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Deployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Deployment{}, &DeploymentList{})
}
