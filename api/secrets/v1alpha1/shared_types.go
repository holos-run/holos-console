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

// SecretKeyReference references a specific key within a v1.Secret that lives
// in the same namespace as the referencing CR. Cross-namespace references
// must use NamespacedSecretKeyReference and are additionally guarded by
// admission (see HOL-703).
//
// The referenced v1.Secret is the sole store of the sensitive credential
// bytes; the CR itself never carries the byte payload. See the package
// doc.go "no sensitive values on CRs" invariant for the rationale.
type SecretKeyReference struct {
	// Name is the metadata.name of the referenced v1.Secret in the same
	// namespace as the referencing CR.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key is the key inside the referenced v1.Secret .data that holds
	// the credential bytes the referencing CR wants to read or project.
	// The CR never stores the bytes — only the reference.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// NamespacedSecretKeyReference references a specific key within a v1.Secret
// that may explicitly name its namespace. Admission enforces that Namespace
// equals the referencing CR's metadata.namespace (see the
// credential-upstreamref-same-namespace VAP in HOL-703); cross-tenant
// references are rejected at admission so the RBAC contract on the backing
// v1.Secret remains the authoritative access boundary.
//
// Namespace is optional: when empty, the referencing CR's own namespace is
// used. Authors on same-namespace references SHOULD omit Namespace so the
// admission guard is trivially satisfied.
type NamespacedSecretKeyReference struct {
	// Namespace optionally names the namespace of the referenced
	// v1.Secret. When empty, the CR's own namespace is used. Admission
	// rejects any non-empty value that differs from the referencing CR's
	// namespace.
	//
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`

	// Name is the metadata.name of the referenced v1.Secret.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key is the key inside the referenced v1.Secret .data that holds
	// the credential bytes.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// PhaseType is the lifecycle phase of a Credential. It never encodes pepper
// bytes, salt bytes, or any substring of the credential plaintext — see the
// package doc.go invariant. The enum is exposed as a typed string alias so
// CRD OpenAPI emits a bounded enum schema and reconcilers can switch on a
// compiler-checked value.
//
// +kubebuilder:validation:Enum=Active;Rotating;Retired;Revoked;Expired
type PhaseType string

// Credential lifecycle phases. See the Credential CRD GoDoc (HOL-699) for
// the per-phase state machine.
const (
	// PhaseActive indicates the credential is issued and accepted by the
	// data plane.
	PhaseActive PhaseType = "Active"
	// PhaseRotating indicates a successor credential has been issued and
	// the predecessor is serving inside its rotation.graceSeconds window.
	PhaseRotating PhaseType = "Rotating"
	// PhaseRetired indicates the credential's rotation grace window has
	// elapsed; the data plane no longer accepts it.
	PhaseRetired PhaseType = "Retired"
	// PhaseRevoked indicates the credential was administratively revoked
	// (.spec.revoked=true). Revocation is terminal.
	PhaseRevoked PhaseType = "Revoked"
	// PhaseExpired indicates .spec.expiresAt has elapsed. The data plane
	// no longer accepts the credential.
	PhaseExpired PhaseType = "Expired"
)

// AuthenticationType enumerates the authentication scheme carried by a
// Credential or referenced by a SecretInjectionPolicy. v1alpha1 ships with
// APIKey as the only honoured value; admission rejects OIDC at creation
// time (HOL-703 credential-authn-type-apikey-only and
// secretinjectionpolicy-authn-type-apikey-only). The OIDC constant is
// defined here so the CRD OpenAPI enum enumerates the full eventual set
// and admission policies can cite the literal.
//
// +kubebuilder:validation:Enum=APIKey;OIDC
type AuthenticationType string

const (
	// AuthenticationTypeAPIKey selects the static, holos-issued API key
	// scheme. It is the only value accepted by v1alpha1 admission.
	AuthenticationTypeAPIKey AuthenticationType = "APIKey"
	// AuthenticationTypeOIDC selects OIDC ID-token presentation. It is
	// reserved for a future version and is rejected by v1alpha1
	// admission; the constant exists so the CRD OpenAPI schema can
	// enumerate it.
	AuthenticationTypeOIDC AuthenticationType = "OIDC"
)
