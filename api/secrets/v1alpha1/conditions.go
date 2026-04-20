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

// Condition types and reason strings are contract — renaming any of these
// constants is a breaking change for the secrets.holos.run API group. The
// per-kind tables below mirror the invariant table documented in HOL-675
// and ADR 031 (Secret Injection Service — Architecture Pre-Decisions). See
// the templates group's equivalent file (api/templates/v1alpha1/conditions.go)
// for the pattern; the two groups deliberately keep independent contracts.

// UpstreamSecret condition types.
const (
	// UpstreamSecretConditionAccepted tracks whether the reconciler parsed
	// .spec and accepted it, or rejected it with a typed reason.
	UpstreamSecretConditionAccepted = "Accepted"
	// UpstreamSecretConditionResolvedRefs tracks whether the referenced
	// v1.Secret named by .spec.secretRef exists and contains the named
	// key. The condition name mirrors the Gateway API HTTPRoute.status
	// ResolvedRefs condition — platform operators already understand its
	// meaning.
	UpstreamSecretConditionResolvedRefs = "ResolvedRefs"
	// UpstreamSecretConditionReady is the aggregate: Accepted && ResolvedRefs
	// are both True.
	UpstreamSecretConditionReady = "Ready"
)

// UpstreamSecret condition reasons.
const (
	UpstreamSecretReasonAccepted         = "Accepted"
	UpstreamSecretReasonInvalidSpec      = "InvalidSpec"
	UpstreamSecretReasonResolvedRefs     = "ResolvedRefs"
	UpstreamSecretReasonSecretNotFound   = "SecretNotFound"
	UpstreamSecretReasonSecretKeyMissing = "SecretKeyMissing"
	UpstreamSecretReasonReady            = "Ready"
	UpstreamSecretReasonNotReady         = "NotReady"
)

// Credential condition types.
const (
	// CredentialConditionAccepted tracks whether the reconciler parsed
	// .spec and accepted it, or rejected it with a typed reason (OIDC in
	// v1alpha1, cross-namespace upstreamSecretRef, etc.).
	CredentialConditionAccepted = "Accepted"
	// CredentialConditionHashMaterialized tracks whether the controller
	// has written the argon2id hash + per-credential salt to the sibling
	// v1.Secret named by .status.hashSecretRef. The hash bytes never
	// appear on the Credential CR.
	CredentialConditionHashMaterialized = "HashMaterialized"
	// CredentialConditionReady is the aggregate: Accepted &&
	// HashMaterialized are both True and .status.phase is not Revoked.
	CredentialConditionReady = "Ready"
	// CredentialConditionExpired reports whether .spec.expiresAt has
	// elapsed. It is a standalone condition (not an aggregate) so
	// operators can filter on it independently of Ready.
	CredentialConditionExpired = "Expired"
)

// Credential condition reasons.
const (
	CredentialReasonAccepted          = "Accepted"
	CredentialReasonInvalidSpec       = "InvalidSpec"
	CredentialReasonOIDCNotSupported  = "OIDCNotSupported"
	CredentialReasonHashMaterialized  = "HashMaterialized"
	CredentialReasonHashSecretMissing = "HashSecretMissing"
	CredentialReasonReady             = "Ready"
	CredentialReasonNotReady          = "NotReady"
	CredentialReasonRevoked           = "Revoked"
	CredentialReasonExpired           = "Expired"
)

// SecretInjectionPolicy condition types.
const (
	// SecretInjectionPolicyConditionAccepted tracks whether the reconciler
	// parsed .spec and accepted it, or rejected it with a typed reason.
	SecretInjectionPolicyConditionAccepted = "Accepted"
	// SecretInjectionPolicyConditionReady is the aggregate: Accepted is
	// True.
	SecretInjectionPolicyConditionReady = "Ready"
)

// SecretInjectionPolicy condition reasons.
const (
	SecretInjectionPolicyReasonAccepted    = "Accepted"
	SecretInjectionPolicyReasonInvalidSpec = "InvalidSpec"
	SecretInjectionPolicyReasonReady       = "Ready"
	SecretInjectionPolicyReasonNotReady    = "NotReady"
)

// SecretInjectionPolicyBinding condition types.
const (
	// SecretInjectionPolicyBindingConditionAccepted tracks whether the
	// reconciler parsed .spec.policyRef and .spec.targetRefs and accepted
	// the spec.
	SecretInjectionPolicyBindingConditionAccepted = "Accepted"
	// SecretInjectionPolicyBindingConditionResolvedRefs tracks whether
	// every target_ref kind is permitted and the referenced
	// SecretInjectionPolicy exists. The condition name mirrors the
	// Gateway API ResolvedRefs condition.
	SecretInjectionPolicyBindingConditionResolvedRefs = "ResolvedRefs"
	// SecretInjectionPolicyBindingConditionProgrammed reports whether the
	// reconciler has successfully written the backing
	// security.istio.io/AuthorizationPolicy (and located the
	// waypoint). The condition name mirrors the Gateway API Programmed
	// condition.
	SecretInjectionPolicyBindingConditionProgrammed = "Programmed"
	// SecretInjectionPolicyBindingConditionReady is the aggregate:
	// Accepted && ResolvedRefs && Programmed are all True.
	SecretInjectionPolicyBindingConditionReady = "Ready"
)

// SecretInjectionPolicyBinding condition reasons.
const (
	SecretInjectionPolicyBindingReasonAccepted                      = "Accepted"
	SecretInjectionPolicyBindingReasonInvalidSpec                   = "InvalidSpec"
	SecretInjectionPolicyBindingReasonResolvedRefs                  = "ResolvedRefs"
	SecretInjectionPolicyBindingReasonPolicyNotFound                = "PolicyNotFound"
	SecretInjectionPolicyBindingReasonInvalidTargetKind             = "InvalidTargetKind"
	SecretInjectionPolicyBindingReasonProgrammed                    = "Programmed"
	SecretInjectionPolicyBindingReasonAuthorizationPolicyWriteFailed = "AuthorizationPolicyWriteFailed"
	SecretInjectionPolicyBindingReasonWaypointNotFound              = "WaypointNotFound"
	SecretInjectionPolicyBindingReasonReady                         = "Ready"
	SecretInjectionPolicyBindingReasonNotReady                      = "NotReady"
)

// Finalizer is the finalizer key used by reconcilers for the
// secrets.holos.run API group when non-trivial cleanup is required before
// the API server deletes a managed object. The primary cleanup path is the
// Credential reconciler deleting its sibling hash v1.Secret so rotation
// state does not leak across Credential lifetimes.
const Finalizer = "secrets.holos.run/finalizer"
