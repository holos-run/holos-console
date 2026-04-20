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

// Upstream describes the endpoint that UpstreamSecret authenticates to. The
// host/scheme/port tuple identifies the upstream the injector will append a
// credential header to on the hot path; PathPrefix narrows the match to a
// subtree when the same host serves multiple authentication domains.
type Upstream struct {
	// Host is the upstream hostname (FQDN or IP literal). The injector
	// matches the hot-path request's :authority against this value; the
	// match is exact, not a wildcard.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`

	// Scheme selects the transport scheme of the upstream. v1alpha1
	// supports http and https only.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=http;https
	Scheme string `json:"scheme"`

	// Port is the upstream TCP port. Optional; when unset the injector
	// uses the scheme's default (80 for http, 443 for https).
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// PathPrefix narrows the injection match to requests whose path
	// begins with this prefix. Optional; when unset the injection applies
	// to every request reaching the host/port.
	//
	// +kubebuilder:validation:Optional
	PathPrefix string `json:"pathPrefix,omitempty"`
}

// Injection describes how the upstream credential is attached to the
// forwarded request. The header name and value template are the entire
// contract — the injector never caches the credential bytes on this CR.
type Injection struct {
	// Header is the HTTP request header the injector writes on the hot
	// path. The value must match the RFC 7230 token production so
	// downstream proxies accept the header name verbatim.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[!#$%&'*+\-.^_\x60|~0-9A-Za-z]+$`
	Header string `json:"header"`

	// ValueTemplate is a Go text/template expression evaluated against a
	// context object that exposes the resolved credential as {{.Value}}.
	// An empty template means "pass through {{.Value}} verbatim". The CR
	// never stores the resolved credential — only this template.
	//
	// +kubebuilder:validation:Optional
	ValueTemplate string `json:"valueTemplate,omitempty"`
}

// UpstreamSecretSpec describes the desired state of an UpstreamSecret. The
// spec is a control object: it references the sibling v1.Secret that holds
// the credential bytes and describes how to project that credential onto
// hot-path requests. Per the package doc.go invariant, no field on this spec
// may carry plaintext credential material, hash bytes, or any prefix of the
// credential that reveals entropy.
type UpstreamSecretSpec struct {
	// SecretRef names the sibling v1.Secret and the key within its .data
	// map that holds the upstream credential bytes. The referenced
	// v1.Secret is the sole store of the sensitive bytes.
	//
	// +kubebuilder:validation:Required
	SecretRef SecretKeyReference `json:"secretRef"`

	// Upstream describes the host/scheme/port tuple the injector attaches
	// the credential to.
	//
	// +kubebuilder:validation:Required
	Upstream Upstream `json:"upstream"`

	// Injection describes the header name and value template the
	// injector writes on the forwarded request.
	//
	// +kubebuilder:validation:Required
	Injection Injection `json:"injection"`
}

// UpstreamSecretStatus describes the observed state of an UpstreamSecret.
// Follows the Gateway-API status pattern: each condition carries its own
// observedGeneration and the top-level observedGeneration tracks
// metadata.generation. The status never caches resolved credential bytes —
// resolution happens on the hot path at request time.
type UpstreamSecretStatus struct {
	// ObservedGeneration is the most recent metadata.generation the
	// reconciler has acted on.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest observations of the
	// UpstreamSecret's state. Known condition types are Accepted,
	// ResolvedRefs, and Ready. See api/secrets/v1alpha1/conditions.go for
	// the reason-string catalog.
	//
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// UpstreamSecret is the project-scoped CRD that describes an outbound
// authentication injection: a sibling v1.Secret holds the credential bytes,
// this object describes the upstream to inject into and the header/template
// shape. The CR never carries the credential bytes themselves — see the
// package doc.go "no sensitive values on CRs" invariant. Resolution of the
// sibling v1.Secret happens on the hot path in the injector (M2), not on
// reconcile. See ADR 031 and the parent plan (HOL-675) for the full design.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=us,categories=holos;secrets
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Host",type=string,JSONPath=`.spec.upstream.host`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type UpstreamSecret struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UpstreamSecretSpec   `json:"spec,omitempty"`
	Status UpstreamSecretStatus `json:"status,omitempty"`
}

// UpstreamSecretList contains a list of UpstreamSecret.
//
// +kubebuilder:object:root=true
type UpstreamSecretList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UpstreamSecret `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UpstreamSecret{}, &UpstreamSecretList{})
}
