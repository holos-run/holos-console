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

// PolicyRefScope narrows the scope a SecretInjectionPolicyBinding may
// reference a policy in. v1alpha1 admission only accepts organization or
// folder scopes — project-scope policy references are rejected so the
// binding does not need to reason about cross-tenant policy inheritance
// inside a single project namespace. The Go-level enum mirrors this so
// the type check fails fast.
//
// +kubebuilder:validation:Enum=organization;folder
type PolicyRefScope string

const (
	// PolicyRefScopeOrganization references a policy in an organization
	// namespace.
	PolicyRefScopeOrganization PolicyRefScope = "organization"
	// PolicyRefScopeFolder references a policy in a folder namespace.
	PolicyRefScopeFolder PolicyRefScope = "folder"
)

// PolicyRef references a SecretInjectionPolicy by (scope, namespace, name).
// The referenced policy must live in a scope reachable from the binding's
// own namespace (ancestor chain or same scope). Project-scope policy
// references are rejected at the Go level (see PolicyRefScope).
type PolicyRef struct {
	// Scope narrows where the reconciler resolves Namespace. Only
	// `organization` and `folder` are accepted; project-scope policy
	// references are rejected.
	//
	// +kubebuilder:validation:Required
	Scope PolicyRefScope `json:"scope"`

	// Namespace is the Kubernetes namespace that owns the referenced
	// SecretInjectionPolicy. Admission (VAP
	// `secretinjectionpolicybinding-policyref-same-namespace-or-ancestor`)
	// accepts this value only when it is one of: (1) the binding's own
	// namespace, (2) the value of the binding namespace's
	// `console.holos.run/parent` label (direct parent), or (3) the
	// synthesized `holos-org-<console.holos.run/organization>` namespace
	// from the binding namespace's labels (root organization). Scope does
	// not gate admission — it narrows the reconciler's resolution path.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`

	// Name is the referenced SecretInjectionPolicy's metadata.name.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// TargetRefKind enumerates the Kubernetes kinds a binding may attach a
// policy to. Restricting Kind to the Gateway-API-compatible subset —
// `ServiceAccount` for identity-based bindings and `Service` for
// workload-addressed bindings — keeps the reconciler's policy-generation
// logic tractable in M1.
//
// +kubebuilder:validation:Enum=ServiceAccount;Service
type TargetRefKind string

const (
	// TargetRefKindServiceAccount binds the policy to every pod
	// presenting the named ServiceAccount identity.
	TargetRefKindServiceAccount TargetRefKind = "ServiceAccount"
	// TargetRefKindService binds the policy to the pods backing the
	// named Service.
	TargetRefKindService TargetRefKind = "Service"
)

// TargetRef selects a specific Kubernetes object a binding attaches its
// policy to. Shape mirrors the Gateway-API target reference to keep
// tooling interchangeable; Group defaults to the core ("") group because
// both supported kinds live there.
type TargetRef struct {
	// Group is the API group of the target. Empty ("") denotes the core
	// API group (v1) and is the only value accepted by v1alpha1
	// admission for the supported Kind set.
	//
	// +kubebuilder:validation:Optional
	Group string `json:"group,omitempty"`

	// Kind is the target's kind. v1alpha1 admission accepts only
	// `ServiceAccount` or `Service`.
	//
	// +kubebuilder:validation:Required
	Kind TargetRefKind `json:"kind"`

	// Namespace is the target's metadata.namespace. Admission enforces
	// that the namespace is reachable from the binding's own namespace
	// (same namespace or a descendant of the binding's scope).
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`

	// Name is the target's metadata.name.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// SecretInjectionPolicyBindingSpec describes the desired state of a
// SecretInjectionPolicyBinding. Per the package doc.go invariant, no
// field here may carry sensitive byte material — the binding only names
// a policy and the targets it applies to.
type SecretInjectionPolicyBindingSpec struct {
	// PolicyRef identifies the SecretInjectionPolicy this binding
	// attaches. A binding references exactly one policy; use multiple
	// bindings to attach multiple policies to overlapping target sets.
	//
	// +kubebuilder:validation:Required
	PolicyRef PolicyRef `json:"policyRef"`

	// TargetRefs enumerates the Kubernetes objects the referenced policy
	// applies to. Order is not significant; duplicates (same
	// (group, kind, namespace, name) tuple) are rejected by the
	// reconciler.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +listType=atomic
	TargetRefs []TargetRef `json:"targetRefs"`

	// WorkloadSelector additionally narrows the bound set to pods whose
	// labels match. nil means "no workload filter" — every pod reachable
	// from TargetRefs is bound. Gateway-API binding patterns use a
	// pointer for optionality; matching those semantics here keeps
	// tooling interchangeable.
	//
	// +kubebuilder:validation:Optional
	WorkloadSelector *metav1.LabelSelector `json:"workloadSelector,omitempty"`
}

// SecretInjectionPolicyBindingStatus describes the observed state of a
// SecretInjectionPolicyBinding. Follows the Gateway-API status pattern
// recorded in ADR 030.
type SecretInjectionPolicyBindingStatus struct {
	// ObservedGeneration is the most recent metadata.generation the
	// reconciler has acted on.
	//
	// +kubebuilder:validation:Optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest observations of the
	// SecretInjectionPolicyBinding's state. Known condition types are
	// Accepted, ResolvedRefs, Programmed, and Ready. The Programmed
	// condition name mirrors the Gateway API Programmed condition —
	// platform operators already understand its meaning. See
	// api/secrets/v1alpha1/conditions.go for the reason-string catalog.
	//
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// SecretInjectionPolicyBinding attaches a SecretInjectionPolicy to a set
// of Kubernetes targets (ServiceAccount or Service) plus an optional
// workload selector. The binding lives in a namespace whose
// `console.holos.run/resource-type` label is organization or folder; the
// referenced policy lives in the same scope. See ADR 031 and the parent
// plan (HOL-675) for the full design.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=sipb,categories=holos;secrets
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Policy",type=string,JSONPath=`.spec.policyRef.name`
// +kubebuilder:printcolumn:name="Resolved",type=string,JSONPath=`.status.conditions[?(@.type=="ResolvedRefs")].status`
// +kubebuilder:printcolumn:name="Programmed",type=string,JSONPath=`.status.conditions[?(@.type=="Programmed")].status`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type SecretInjectionPolicyBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SecretInjectionPolicyBindingSpec   `json:"spec,omitempty"`
	Status SecretInjectionPolicyBindingStatus `json:"status,omitempty"`
}

// SecretInjectionPolicyBindingList contains a list of
// SecretInjectionPolicyBinding.
//
// +kubebuilder:object:root=true
type SecretInjectionPolicyBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SecretInjectionPolicyBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SecretInjectionPolicyBinding{}, &SecretInjectionPolicyBindingList{})
}
