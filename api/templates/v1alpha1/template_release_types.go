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

// TemplateReleaseSpec describes an immutable, published version of a Template.
// A TemplateRelease snapshots the CUE payload, defaults, and authoring notes
// (changelog, upgrade advice) from a Template at publish time. The object is
// intended to be treated as append-only: once created for a given
// (templateName, version) tuple, its spec should not change.
type TemplateReleaseSpec struct {
	// TemplateName is the DNS label slug of the Template this release
	// snapshots. Releases live in the same namespace as the owning
	// Template.
	// +kubebuilder:validation:MinLength=1
	TemplateName string `json:"templateName"`
	// Version is the semver string (for example "1.2.3") identifying
	// this release.
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`
	// CueTemplate is the CUE source snapshot the render path evaluates
	// when resolving a versioned reference to this release.
	CueTemplate string `json:"cueTemplate,omitempty"`
	// DefaultsJSON is the release's TemplateDefaults serialized as the
	// canonical proto JSON. Stored as an opaque string rather than a
	// structured CRD subtree because a release is an immutable version
	// pin — it must round-trip the full proto TemplateDefaults surface
	// (including env vars with secret_key_ref / config_map_key_ref) that
	// the structured Template.Spec.Defaults does not currently model. The
	// retired release ConfigMaps stored the proto JSON verbatim in the
	// `defaults.json` data key; this field preserves that fidelity.
	// Empty when the published Template had no defaults.
	DefaultsJSON string `json:"defaultsJSON,omitempty"`
	// Changelog is a free-form description of what changed in this
	// release relative to the prior one.
	Changelog string `json:"changelog,omitempty"`
	// UpgradeAdvice is a free-form description of steps operators must
	// take to upgrade downstream consumers to this release.
	UpgradeAdvice string `json:"upgradeAdvice,omitempty"`
}

// TemplateReleaseStatus describes the observed state of a TemplateRelease.
// Follows the Gateway-API status pattern recorded in ADR 030.
type TemplateReleaseStatus struct {
	// ObservedGeneration is the most recent generation observed for this
	// TemplateRelease by the reconciler.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions represent the latest available observations of this
	// TemplateRelease's state.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// TemplateRelease snapshots a published version of a Template. The owning
// Template and the release share a namespace; the release's
// `.spec.templateName` identifies the Template and `.spec.version` identifies
// the semver version. The resource name is a deterministic function of those
// two fields (see console/templates.ReleaseObjectName).
//
// The spec is immutable after creation (see the CEL rule on Spec). This
// mirrors the `Immutable: true` guarantee the retired release ConfigMaps
// offered: published releases are a version pin, so rewriting their CUE
// payload in place would silently break downstream template references that
// resolved against an earlier snapshot.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tmplrel,categories=holos
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Template",type=string,JSONPath=`.spec.templateName`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type TemplateRelease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="TemplateRelease spec is immutable after creation"
	Spec   TemplateReleaseSpec   `json:"spec,omitempty"`
	Status TemplateReleaseStatus `json:"status,omitempty"`
}

// TemplateReleaseList contains a list of TemplateRelease.
//
// +kubebuilder:object:root=true
type TemplateReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TemplateRelease `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TemplateRelease{}, &TemplateReleaseList{})
}
