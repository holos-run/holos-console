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

// Package v1alpha1 contains API Schema definitions for the secrets.holos.run
// v1alpha1 API group. The authoritative reference for this API surface is
// the M1 plan HOL-675:
// https://linear.app/holos-run/issue/HOL-675/plan-m1-crds-admission-policies-rbac
// See docs/adrs/031-secret-injection-service.md for the architectural
// decisions; the group and version are locked by ADR 031 §1 and mirror the
// api/templates/v1alpha1 layout. The per-kind API reference lives at
// docs/api/secrets.holos.run.md.
//
// # Invariant: no sensitive values on CRs (MUST READ before editing any field)
//
// Every CRD in secrets.holos.run/v1alpha1 is a control object, not a vault.
// It carries references, selectors, lifecycle metadata, and conditions —
// never bytes an attacker could use to authenticate, replay, or grind
// offline. Operators and future contributors MUST consult HOL-675 before
// adding or changing any field: the invariant below is enforced by the
// admission policies in config/secret-injector/admission/ and by the
// field-name guards in *_invariant_test.go.
//
// Forbidden on any spec or status field of any kind in this group:
//
//   - Plaintext credential material (API keys, tokens, passwords, refresh
//     tokens).
//   - Hash output bytes, salt bytes, pepper bytes, or pepper versions encoded
//     as opaque strings that hint at rotation generation beyond a counting
//     integer.
//   - Prefix, last-4, fingerprint, or any truncation of the credential that
//     reveals non-trivial entropy.
//   - Upstream credential bytes (the thing swapped in on the hot path).
//
// Allowed on the CR: opaque IDs (credentialID is a KSUID with no secret
// entropy), {name, key} references to a sibling v1.Secret, integer
// pepperVersion, phase, and []metav1.Condition.
//
// Rationale: a CR leaks through kubectl get -o yaml, etcd snapshots,
// Velero/Gemini backups, audit logs, and any principal with get/list on
// the CRD. A v1.Secret has a separate, tighter RBAC surface, benefits from
// encryption-at-rest / KMS providers, and is what operators already know
// to protect. Hard-won: Dex CRs store OAuth refresh tokens, which has been
// a persistent complaint in the Dex community — we do not repeat that
// mistake.
//
// This principle is why we collapsed the earlier "Credential-IS-a-CRD vs.
// Credential-is-just-a-Secret" debate into a single design: Credential IS
// a CRD, AND all sensitive values live in referenced v1.Secrets. We keep
// declarative lifecycle, typed conditions, and the broader get/list/watch
// RBAC surface, while retaining tight RBAC and encryption-at-rest on the
// backing Secret.
//
// # Scope
//
// This package registers the group-version, the shared reference/enum
// types, the shared condition catalog, and the four kinds shipped in M1:
// UpstreamSecret (HOL-697), Credential (HOL-699), SecretInjectionPolicy
// and SecretInjectionPolicyBinding (HOL-701). The admission policies that
// enforce the invariant live under config/secret-injector/admission/
// (HOL-703); the negative-path envtest suite lives at
// api/secrets/v1alpha1/crd_test.go (HOL-708).
//
// +kubebuilder:object:generate=true
// +groupName=secrets.holos.run
package v1alpha1
