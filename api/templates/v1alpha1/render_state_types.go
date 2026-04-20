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

// RenderTargetKind discriminates the kind of render target a RenderState
// snapshot belongs to. The set is closed: render targets are either
// Deployments or project-scope Templates today, and a new value here
// requires schema-aware code in the resolver and drift-checker paths.
//
// +kubebuilder:validation:Enum=Deployment;ProjectTemplate
type RenderTargetKind string

const (
	// RenderTargetKindDeployment marks a RenderState that snapshots the
	// effective render set last applied to a Deployment.
	RenderTargetKindDeployment RenderTargetKind = "Deployment"
	// RenderTargetKindProjectTemplate marks a RenderState that snapshots
	// the effective render set last applied to a project-scope Template.
	RenderTargetKindProjectTemplate RenderTargetKind = "ProjectTemplate"
)

// RenderStateLinkedTemplateRef is the structured form of a single resolved
// template reference recorded on a RenderState. The shape mirrors the
// flattened LinkedTemplateRef type used elsewhere in the templates API
// group (HOL-723) so render-state evidence stores the same `(namespace,
// name, versionConstraint)` triple the resolver consumes.
type RenderStateLinkedTemplateRef struct {
	// Namespace is the Kubernetes namespace that owns the referenced
	// template. The resolver classifies the namespace into its hierarchy
	// kind (organization, folder, project) at render time — render-state
	// readers can do the same without needing to denormalize scope at
	// write time.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`
	Namespace string `json:"namespace"`
	// Name is the DNS label slug of the referenced template.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`
	Name string `json:"name"`
	// VersionConstraint is the semver constraint expression last
	// resolved against this reference (for example "" or ">=1.2"). Empty
	// when the reference targets the live (unversioned) Template.
	VersionConstraint string `json:"versionConstraint,omitempty"`
}

// RenderStateSpec describes the effective set of LinkedTemplateRef values
// last applied to a single render target. A RenderState is evidence of a
// past render — it is never read by the live render path itself; the
// drift checker consults it to compare a freshly resolved set against the
// last applied set, and the live render path always recomputes.
type RenderStateSpec struct {
	// TargetKind identifies the kind of render target this snapshot
	// belongs to.
	TargetKind RenderTargetKind `json:"targetKind"`
	// TargetName is the render target's own name (the deployment name
	// or the project-scope template name).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`
	TargetName string `json:"targetName"`
	// Project is the slug of the project that owns the render target.
	// Records live in the folder or organization namespace that owns
	// the project, so the project slug is carried on the spec (and
	// mirrored to a label) to keep `(targetKind, project, targetName)`
	// queries efficient.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`
	Project string `json:"project"`
	// AppliedRefs is the resolved render set the live render path
	// produced at the last successful Create/Update of the render
	// target. Empty list is meaningful: it means "successfully rendered
	// with zero linked templates" and is distinct from the absence of a
	// RenderState (which means "never applied").
	// +listType=atomic
	AppliedRefs []RenderStateLinkedTemplateRef `json:"appliedRefs,omitempty"`
}

// RenderStateStatus describes the observed state of a RenderState. There
// is no reconciler for RenderState today (the live handler writes the
// object on the success path of Create/Update render-target paths), so
// the status surface stays minimal. Following the Gateway-API convention
// from ADR 030 keeps the door open for a future reconciler that emits
// e.g. a `Stale` condition without reshaping the resource.
type RenderStateStatus struct {
	// ObservedGeneration is the most recent generation observed for
	// this RenderState by a future reconciler. Currently unused.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions represent the latest available observations of this
	// RenderState's state.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// RenderState records the effective set of LinkedTemplateRef values last
// applied to a render target (a Deployment or a project-scope Template).
// Render-state snapshots are evidence of a past render — they are not
// read by the live render path. The drift checker consults them to
// surface policy drift via DeploymentStatusSummary.policy_drift,
// GetDeploymentPolicyState, and GetProjectTemplatePolicyState.
//
// Storage location is a security-relevant invariant: a RenderState that
// belongs to a folder-namespace-owned project MUST live in the folder
// namespace (or the organization namespace when the project's parent is
// the organization), NEVER in a project namespace. Project owners hold
// namespace-scoped write access and could otherwise forge drift evidence
// — write a fake `RenderState` matching the live render output, and the
// drift checker would report no drift even after a policy revoked a
// required template. The ValidatingAdmissionPolicy shipped in
// `config/admission/renderstate-folder-or-org-only.yaml` enforces the
// invariant at the API server.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=rstate,categories=holos
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.project`
// +kubebuilder:printcolumn:name="TargetKind",type=string,JSONPath=`.spec.targetKind`
// +kubebuilder:printcolumn:name="TargetName",type=string,JSONPath=`.spec.targetName`
// +kubebuilder:printcolumn:name="Refs",type=integer,JSONPath=`.spec.appliedRefs[*]`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type RenderState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RenderStateSpec   `json:"spec,omitempty"`
	Status RenderStateStatus `json:"status,omitempty"`
}

// RenderStateList contains a list of RenderState.
//
// +kubebuilder:object:root=true
type RenderStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RenderState `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RenderState{}, &RenderStateList{})
}
