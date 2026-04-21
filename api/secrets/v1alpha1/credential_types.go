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

// RotationGroupLabel is the label key the Credential reconciler uses to
// correlate a retiring credential with its successor during a rotation.
// When two Credentials in the same namespace carry the same value for this
// label, the older (by metadata.creationTimestamp) is treated as the
// predecessor and transitions Phase=Rotating the moment the successor
// appears. The retirement then fires once the predecessor's
// spec.rotation.graceSeconds window has elapsed.
//
// Label keys under secrets.holos.run are the group's sanctioned namespace
// for reconciler-observed rotation state; the value is opaque to the
// reconciler and is typically a short human-legible string (for example,
// "vendor-apikey"). The label MUST be stamped on both predecessor and
// successor by whatever tooling creates the successor Credential — the
// reconciler never writes or mutates this label itself.
const RotationGroupLabel = "secrets.holos.run/rotation-group"

// APIKeySettings captures the transport-layer knobs for an API-key
// credential. v1alpha1 exposes only the header name; later versions will
// broaden this to cover value templates and rotation-grace projection. The
// CR never stores the API-key value itself — the value lives in a
// controller-owned v1.Secret and is materialised by the injector on the hot
// path (M2).
type APIKeySettings struct {
	// HeaderName is the HTTP header name the injector writes when
	// projecting this credential. Admission (HOL-703) constrains the
	// value to the RFC 7230 token production; this CR deliberately does
	// not carry a Go-level default so server-side defaulting lands in M2
	// alongside the admission policy that enforces the token regex.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	HeaderName string `json:"headerName"`
}

// Authentication selects the authentication scheme for a Credential. The
// Type field drives which sibling sub-struct is consulted; APIKey is the
// only scheme honoured by v1alpha1, and admission rejects Type=OIDC at
// creation time (HOL-703 credential-authn-type-apikey-only). The OIDC
// branch is intentionally representable in Go so a rejection surfaces as a
// typed admission error rather than a marshaling failure.
type Authentication struct {
	// Type selects the authentication scheme.
	//
	// +kubebuilder:validation:Required
	Type AuthenticationType `json:"type"`

	// APIKey carries the API-key-specific knobs. Admission (HOL-703)
	// requires APIKey to be present when Type=APIKey.
	//
	// +kubebuilder:validation:Optional
	APIKey *APIKeySettings `json:"apiKey,omitempty"`
}

// Rotation describes the overlap window between a retiring credential and
// its successor. During the grace window the data plane accepts both
// credentials so clients can drain without a hard cut. The grace counter
// is a plain integer — no wall-clock time appears on this sub-struct so
// there is no opportunity for a future field to leak rotation entropy.
type Rotation struct {
	// GraceSeconds is the number of seconds the retiring credential
	// remains valid after a successor is issued. Zero (the default) means
	// "retire immediately on rotation". Admission caps this at a
	// reasonable upper bound in a later milestone.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0
	GraceSeconds int32 `json:"graceSeconds,omitempty"`
}

// TargetReference selects a specific Kubernetes object whose identity may
// present this Credential. The M1 scope narrows the accepted set to
// ServiceAccount in the Credential's own namespace (enforced by admission);
// additional kinds are reserved for later milestones.
type TargetReference struct {
	// Group is the API group of the target. Empty ("") denotes the core
	// API group (v1), which is the only value accepted by v1alpha1
	// admission. Admission rejects cross-group targeting to keep the M1
	// authorisation surface tractable.
	//
	// +kubebuilder:validation:Optional
	Group string `json:"group,omitempty"`

	// Kind is the target's kind. v1alpha1 admission accepts only
	// "ServiceAccount".
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// Name is the target's metadata.name. Lookup is scoped to the
	// Credential's own namespace — cross-namespace targeting is rejected
	// at admission.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// Selector describes the principals allowed to present this Credential.
// TargetRefs enumerates explicit objects (the typical case — a
// ServiceAccount in the same namespace); WorkloadSelector covers label-based
// pod selection for workloads that cannot be named ahead of time. The two
// inputs are OR-combined by the injector.
type Selector struct {
	// TargetRefs lists explicit Kubernetes objects whose identity grants
	// use of this Credential. The common case is a single
	// ServiceAccount in the same namespace.
	//
	// +kubebuilder:validation:Optional
	// +listType=atomic
	TargetRefs []TargetReference `json:"targetRefs,omitempty"`

	// WorkloadSelector selects pod workloads by label. The injector
	// consults this alongside TargetRefs.
	//
	// +kubebuilder:validation:Optional
	WorkloadSelector *metav1.LabelSelector `json:"workloadSelector,omitempty"`
}

// CredentialSpec describes the desired state of a Credential. Per the
// package doc.go invariant, no field may carry plaintext credential
// material, hash bytes, salt bytes, pepper bytes, or any prefix, last-4,
// fingerprint, or truncation of the credential plaintext. All sensitive
// bytes live in the sibling v1.Secret materialised by the reconciler on
// Credential accept (M2); this spec is a pure control object.
type CredentialSpec struct {
	// Authentication selects the authentication scheme and its
	// transport-specific knobs.
	//
	// +kubebuilder:validation:Required
	Authentication Authentication `json:"authentication"`

	// UpstreamSecretRef binds this Credential to the sibling
	// v1.Secret whose bytes are swapped onto the request when the
	// Credential authenticates a client. The namespace field is optional;
	// when empty the referencing Credential's own namespace is used and
	// admission (HOL-703 credential-upstreamref-same-namespace) is
	// trivially satisfied.
	//
	// +kubebuilder:validation:Required
	UpstreamSecretRef NamespacedSecretKeyReference `json:"upstreamSecretRef"`

	// ExpiresAt is the wall-clock time after which the data plane no
	// longer accepts this credential. The reconciler transitions Phase to
	// Expired when ExpiresAt has elapsed; clock-skew resolution is the
	// data plane's responsibility.
	//
	// +kubebuilder:validation:Optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// Revoked requests administrative revocation. When set to true the
	// reconciler moves Phase to Revoked; revocation is terminal.
	//
	// +kubebuilder:validation:Optional
	Revoked bool `json:"revoked,omitempty"`

	// BindToSourcePrincipal is reserved for M3. When true, admission will
	// require the presenting principal to match the ServiceAccount named
	// by Selector.TargetRefs; v1alpha1 accepts the field but does not act
	// on it. The pointer type preserves the tristate distinction between
	// "absent" and "explicitly false" for future schema evolution.
	//
	// +optional
	BindToSourcePrincipal *bool `json:"bindToSourcePrincipal,omitempty"`

	// Rotation describes the overlap window between a retiring
	// Credential and its successor.
	//
	// +kubebuilder:validation:Optional
	Rotation Rotation `json:"rotation,omitempty"`

	// Selector describes the principals allowed to present this
	// Credential.
	//
	// +kubebuilder:validation:Optional
	Selector Selector `json:"selector,omitempty"`
}

// CredentialStatus describes the observed state of a Credential. Per the
// package doc.go invariant, no field may carry plaintext, hash bytes, salt
// bytes, pepper bytes, or any prefix/truncation that reveals credential
// entropy. The only references permitted are the opaque credentialID, the
// pepper-version counter, and the SecretKeyReference at HashSecretRef.
type CredentialStatus struct {
	// ObservedGeneration is the most recent metadata.generation the
	// reconciler has acted on.
	//
	// +kubebuilder:validation:Optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the current lifecycle phase.
	//
	// +kubebuilder:validation:Optional
	Phase PhaseType `json:"phase,omitempty"`

	// CredentialID is the opaque identifier the issuer returned when this
	// Credential was created. It is a KSUID (27 characters of base62)
	// carrying no sensitive entropy. The CRD schema enforces the KSUID
	// shape so a buggy reconciler or a direct status patch cannot
	// persist an arbitrary string in this field.
	//
	// MUST NOT be or contain the plaintext, a prefix, a last-4, or any
	// substring of the plaintext. See the package doc.go invariant.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MinLength=27
	// +kubebuilder:validation:MaxLength=27
	// +kubebuilder:validation:Pattern=`^[0-9A-Za-z]{27}$`
	CredentialID string `json:"credentialID,omitempty"`

	// HashSecretRef names the sibling v1.Secret (same namespace) that
	// stores the argon2id hash and per-credential salt. The v1.Secret is
	// owned by the reconciler via an ownerReference (enforced in M2), so
	// deleting the Credential reclaims its hash material.
	//
	// Pointer type (not a value struct with omitempty): Go's encoding/json
	// always marshals a value-struct field, so the zero value would emit
	// "hashSecretRef: {}" and fail the generated CRD schema that requires
	// name and key when the object is present. A pointer lets the
	// reconciler leave the field truly absent before the hash Secret is
	// materialised.
	//
	// +kubebuilder:validation:Optional
	HashSecretRef *SecretKeyReference `json:"hashSecretRef,omitempty"`

	// PepperVersion is the monotonic counter of pepper rotations this
	// credential has observed. It is an integer — it MUST NOT hint at
	// pepper material. Increments monotonically per rotation.
	//
	// +kubebuilder:validation:Optional
	PepperVersion int32 `json:"pepperVersion,omitempty"`

	// Conditions represent the latest observations of the Credential's
	// state. Known condition types are Accepted, HashMaterialized, Ready,
	// and Expired. See api/secrets/v1alpha1/conditions.go for the
	// reason-string catalog.
	//
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// Credential is the project-scoped CRD that describes the declarative
// lifecycle of a holos-issued API key. All sensitive bytes (plaintext,
// argon2id hash, salt, pepper) live in the sibling v1.Secret named by
// Status.HashSecretRef and the UpstreamSecret v1.Secret named by
// Spec.UpstreamSecretRef; this CR carries only the opaque credentialID, the
// pepper-version counter, phase, conditions, and cross-object references.
// See the package doc.go "no sensitive values on CRs" invariant, ADR 031,
// and the parent plan (HOL-675) for the full design.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=cred,categories=holos;secrets
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Expires",type=date,JSONPath=`.spec.expiresAt`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Credential struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CredentialSpec   `json:"spec,omitempty"`
	Status CredentialStatus `json:"status,omitempty"`
}

// CredentialList contains a list of Credential.
//
// +kubebuilder:object:root=true
type CredentialList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Credential `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Credential{}, &CredentialList{})
}
