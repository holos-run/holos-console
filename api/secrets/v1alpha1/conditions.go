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
//
// The Condition* catalog holds the shared condition-type string literals
// named in HOL-675. Per-kind Condition* aliases below reference these
// entries so reconcilers can set conditions either through the shared
// name (`ConditionAccepted`) or the kind-scoped alias (e.g.,
// `CredentialConditionAccepted`), and both resolve to the same string.
// Reason strings remain kind-specific because several reasons (like
// `SecretNotFound` or `WaypointNotFound`) only make sense for one kind.
const (
	// ConditionAccepted tracks whether a reconciler parsed .spec and
	// accepted it, or rejected it with a typed reason. Every kind in
	// this group carries an Accepted condition.
	ConditionAccepted = "Accepted"
	// ConditionResolvedRefs tracks whether every cross-object reference
	// on the spec resolves to an existing object at a compatible
	// version. The condition name mirrors the Gateway API
	// HTTPRoute.status ResolvedRefs condition — platform operators
	// already understand its meaning. UpstreamSecret and
	// SecretInjectionPolicyBinding carry this condition.
	ConditionResolvedRefs = "ResolvedRefs"
	// ConditionReady is the aggregate: all other non-aggregate True
	// conditions for the kind are also True. Every kind in this group
	// carries a Ready condition.
	ConditionReady = "Ready"
	// ConditionHashMaterialized tracks whether the controller has
	// written the argon2id hash + per-credential salt to the sibling
	// v1.Secret named by .status.hashSecretRef. The hash bytes never
	// appear on the Credential CR. Credential carries this condition.
	ConditionHashMaterialized = "HashMaterialized"
	// ConditionExpired reports whether .spec.expiresAt has elapsed. It
	// is a standalone condition (not an aggregate) so operators can
	// filter on it independently of Ready. Credential carries this
	// condition.
	ConditionExpired = "Expired"
	// ConditionProgrammed reports whether the reconciler has
	// successfully written the backing security.istio.io
	// AuthorizationPolicy (and located the waypoint). The condition
	// name mirrors the Gateway API Programmed condition.
	// SecretInjectionPolicyBinding carries this condition.
	ConditionProgrammed = "Programmed"
)

// UpstreamSecret condition types. Each constant aliases the shared
// Condition* catalog above; operators and reconcilers may use either form.
const (
	UpstreamSecretConditionAccepted     = ConditionAccepted
	UpstreamSecretConditionResolvedRefs = ConditionResolvedRefs
	UpstreamSecretConditionReady        = ConditionReady
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

// Credential condition types. Each constant aliases the shared
// Condition* catalog above; operators and reconcilers may use either form.
const (
	CredentialConditionAccepted         = ConditionAccepted
	CredentialConditionHashMaterialized = ConditionHashMaterialized
	CredentialConditionReady            = ConditionReady
	CredentialConditionExpired          = ConditionExpired
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

// SecretInjectionPolicy condition types. Each constant aliases the shared
// Condition* catalog above; operators and reconcilers may use either form.
const (
	SecretInjectionPolicyConditionAccepted = ConditionAccepted
	SecretInjectionPolicyConditionReady    = ConditionReady
)

// SecretInjectionPolicy condition reasons.
const (
	SecretInjectionPolicyReasonAccepted    = "Accepted"
	SecretInjectionPolicyReasonInvalidSpec = "InvalidSpec"
	SecretInjectionPolicyReasonReady       = "Ready"
	SecretInjectionPolicyReasonNotReady    = "NotReady"
)

// SecretInjectionPolicyBinding condition types. Each constant aliases the
// shared Condition* catalog above; operators and reconcilers may use either
// form.
const (
	SecretInjectionPolicyBindingConditionAccepted     = ConditionAccepted
	SecretInjectionPolicyBindingConditionResolvedRefs = ConditionResolvedRefs
	SecretInjectionPolicyBindingConditionProgrammed   = ConditionProgrammed
	SecretInjectionPolicyBindingConditionReady        = ConditionReady
)

// SecretInjectionPolicyBinding condition reasons.
const (
	SecretInjectionPolicyBindingReasonAccepted                       = "Accepted"
	SecretInjectionPolicyBindingReasonInvalidSpec                    = "InvalidSpec"
	SecretInjectionPolicyBindingReasonResolvedRefs                   = "ResolvedRefs"
	SecretInjectionPolicyBindingReasonPolicyNotFound                 = "PolicyNotFound"
	SecretInjectionPolicyBindingReasonInvalidTargetKind              = "InvalidTargetKind"
	SecretInjectionPolicyBindingReasonProgrammed                     = "Programmed"
	SecretInjectionPolicyBindingReasonAuthorizationPolicyWriteFailed = "AuthorizationPolicyWriteFailed"
	SecretInjectionPolicyBindingReasonWaypointNotFound               = "WaypointNotFound"
	SecretInjectionPolicyBindingReasonReady                          = "Ready"
	SecretInjectionPolicyBindingReasonNotReady                       = "NotReady"
)

// Finalizer is the finalizer key used by reconcilers for the
// secrets.holos.run API group when non-trivial cleanup is required before
// the API server deletes a managed object. The primary cleanup path is the
// Credential reconciler deleting its sibling hash v1.Secret so rotation
// state does not leak across Credential lifetimes.
const Finalizer = "secrets.holos.run/finalizer"
