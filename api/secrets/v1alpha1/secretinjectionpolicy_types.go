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

// Direction enumerates the traffic direction a SecretInjectionPolicy
// applies to. Ingress policies inspect calls inbound to the bound target;
// Egress policies rewrite calls the bound target makes outbound.
//
// +kubebuilder:validation:Enum=Ingress;Egress
type Direction string

const (
	// DirectionIngress applies the policy to traffic arriving at the
	// bound target.
	DirectionIngress Direction = "Ingress"
	// DirectionEgress applies the policy to traffic leaving the bound
	// target.
	DirectionEgress Direction = "Egress"
)

// UpstreamScope enumerates where an UpstreamRef resolves relative to the
// referencing SecretInjectionPolicy. `project` is the only scope admitted
// in v1alpha1; admission (HOL-703) rejects organization/folder upstream
// references until the cross-tenant resolver lands in a later milestone.
// The wider enum is defined so future milestones can extend the resolver
// without an API break.
//
// +kubebuilder:validation:Enum=project;folder;organization
type UpstreamScope string

const (
	// UpstreamScopeProject resolves UpstreamRef inside the project that
	// owns this SecretInjectionPolicy. This is the only scope accepted in
	// v1alpha1.
	UpstreamScopeProject UpstreamScope = "project"
	// UpstreamScopeFolder resolves UpstreamRef inside a folder namespace
	// reachable from the referencing project's ancestor chain. Reserved
	// for later milestones.
	UpstreamScopeFolder UpstreamScope = "folder"
	// UpstreamScopeOrganization resolves UpstreamRef inside an
	// organization namespace. Reserved for later milestones.
	UpstreamScopeOrganization UpstreamScope = "organization"
)

// Match captures the HTTP-layer predicate a SecretInjectionPolicy applies
// to. A request matches when every non-empty field matches: Hosts is
// checked against the :authority header, PathPrefixes against the request
// path, Methods against the request verb. Fields are OR-combined within a
// field and AND-combined across fields — the usual Gateway API match
// semantics.
type Match struct {
	// Hosts is the list of :authority values (host or host:port) that a
	// request must match. Each entry matches exactly (no wildcards in
	// v1alpha1).
	//
	// +kubebuilder:validation:Optional
	// +listType=atomic
	Hosts []string `json:"hosts,omitempty"`

	// PathPrefixes is the list of URL path prefixes a request must start
	// with. Matching is literal; no percent-decoding is performed by the
	// control plane (the data plane normalises prior to match).
	//
	// Invariant exemption: the field name contains "Prefix" because it
	// is a URL-path match predicate, not a credential-prefix leak. The
	// no-sensitive-values field-name guard (invariant_helper_test.go)
	// allowlists "PathPrefixes" for exactly this reason; no other
	// *Prefix-named field is permitted in this API group.
	//
	// +kubebuilder:validation:Optional
	// +listType=atomic
	PathPrefixes []string `json:"pathPrefixes,omitempty"`

	// Methods is the list of HTTP methods a request must use. Each entry
	// must be a non-empty RFC 7231 method token. The Pattern below is
	// the common-practice subset of tchar (alpha-leading, alphanumeric +
	// hyphen): every IANA-registered method (GET, POST, PROPFIND, MKCOL,
	// PATCH, …) satisfies it, and junk or whitespace-bearing values are
	// rejected by the CRD schema. Admission (HOL-703) layers any
	// stricter cross-field checks on top.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:items:Pattern=`^[A-Za-z][A-Za-z0-9-]*$`
	// +listType=atomic
	Methods []string `json:"methods,omitempty"`
}

// CallerAuth selects the authentication scheme the policy expects on the
// matched request. The referenced AuthenticationType is the same enum the
// Credential CR uses — admission rejects OIDC in v1alpha1 so the scheme
// rejection surfaces as a typed admission error rather than a marshaling
// failure.
type CallerAuth struct {
	// Type selects the authentication scheme the policy expects.
	//
	// +kubebuilder:validation:Required
	Type AuthenticationType `json:"type"`
}

// UpstreamRef resolves the UpstreamSecret (or, at M2, the Credential +
// UpstreamSecret pair) whose bytes are swapped onto the matched request.
// Resolution is scoped by Scope + ScopeName so a project-scope policy can
// pin to a specific project's upstream without leaking cross-tenant names.
type UpstreamRef struct {
	// Scope narrows where the reconciler resolves Name. Only `project`
	// is accepted in v1alpha1; other values are rejected by admission.
	//
	// +kubebuilder:validation:Required
	Scope UpstreamScope `json:"scope"`

	// ScopeName is the project, folder, or organization name that
	// narrows the resolution. The referenced scope must be one the
	// referencing SecretInjectionPolicy can reach (same project in
	// v1alpha1).
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ScopeName string `json:"scopeName"`

	// Name is the metadata.name of the UpstreamSecret (M1) or Credential
	// (M2) the policy swaps in on the hot path. The referenced object
	// must live in the scope named by Scope + ScopeName.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// SecretInjectionPolicySpec describes the desired state of a
// SecretInjectionPolicy. Per the package doc.go invariant, no field on
// this spec may carry plaintext credential bytes, hash material, or any
// truncation of the backing secret; the policy carries only the match
// predicate, the authentication scheme, and the name of the object that
// actually holds the bytes.
type SecretInjectionPolicySpec struct {
	// Direction selects whether the policy applies to inbound or
	// outbound traffic at the bound target.
	//
	// +kubebuilder:validation:Required
	Direction Direction `json:"direction"`

	// Match describes the HTTP-layer predicate a request must satisfy
	// for the policy to apply.
	//
	// +kubebuilder:validation:Optional
	Match Match `json:"match,omitempty"`

	// CallerAuth selects the authentication scheme the policy expects
	// on the matched request. The referenced scheme's bytes are sourced
	// from the object named by UpstreamRef.
	//
	// +kubebuilder:validation:Required
	CallerAuth CallerAuth `json:"callerAuth"`

	// UpstreamRef names the UpstreamSecret (M1) or Credential (M2)
	// whose bytes are swapped in on the matched request.
	//
	// +kubebuilder:validation:Required
	UpstreamRef UpstreamRef `json:"upstreamRef"`
}

// SecretInjectionPolicyStatus describes the observed state of a
// SecretInjectionPolicy. Follows the Gateway-API status pattern recorded
// in ADR 030.
type SecretInjectionPolicyStatus struct {
	// ObservedGeneration is the most recent metadata.generation the
	// reconciler has acted on.
	//
	// +kubebuilder:validation:Optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest observations of the
	// SecretInjectionPolicy's state. Known condition types are Accepted
	// and Ready. See api/secrets/v1alpha1/conditions.go for the
	// reason-string catalog.
	//
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// SecretInjectionPolicy declares the per-direction match + authentication
// + upstream contract that a SecretInjectionPolicyBinding attaches to a
// set of workloads. The policy itself is workload-agnostic; the binding
// supplies the target set. See ADR 031 and the parent plan (HOL-675) for
// the full design.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=sip,categories=holos;secrets
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Direction",type=string,JSONPath=`.spec.direction`
// +kubebuilder:printcolumn:name="Upstream",type=string,JSONPath=`.spec.upstreamRef.name`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type SecretInjectionPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SecretInjectionPolicySpec   `json:"spec,omitempty"`
	Status SecretInjectionPolicyStatus `json:"status,omitempty"`
}

// SecretInjectionPolicyList contains a list of SecretInjectionPolicy.
//
// +kubebuilder:object:root=true
type SecretInjectionPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SecretInjectionPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SecretInjectionPolicy{}, &SecretInjectionPolicyList{})
}
