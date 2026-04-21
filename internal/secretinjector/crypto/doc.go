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

// Package crypto hosts the pluggable key-derivation function (KDF) seam used
// by the holos-secret-injector control plane to turn a plaintext credential
// plus salt plus pepper into an encoded-hash envelope the Credential
// reconciler (HOL-751) writes to the data["hash"] key of a sibling
// v1.Secret. See docs/adrs/031-secret-injection-service.md §3 for the
// architecture.
//
// # Pluggability contract
//
// [KDF] is a small interface with two methods: [KDF.Hash] derives an
// encoded-hash [Envelope] from (plaintext, salt, pepper, params), and
// [KDF.Verify] checks an envelope against a candidate plaintext under
// caller-supplied wantParams in constant time. The default non-FIPS
// build binds [Argon2id] via [Default] (defined in default_nofips.go
// behind the !fips build tag) and is the only implementation M2 ships.
// A future -fips build variant will bind a PBKDF2-HMAC-SHA512
// implementation into pbkdf2.go and supply its own [Default] under the
// fips tag — reconcilers depend on the interface, not the concrete type.
// An -fips build that forgets to land the override fails at link time on
// the missing [Default] symbol rather than silently reverting to argon2id.
//
// [KDF.Verify] enforces parameter-drift rejection on the interface path
// itself: if envelope.KDFParams differ from wantParams, Verify returns
// [ErrParamMismatch] before touching the primitive. Drift is never
// silently accepted — migrating a cost bump requires an explicit re-hash
// via [KDF.Hash] with the new Params.
//
// # Pepper discipline
//
// Pepper is passed in by the caller on every Hash / Verify call. This
// package MUST NOT read the pepper from a file, a ConfigMap, a Secret, an
// environment variable, or any other side channel — that is the pepper
// bootstrap reconciler's job (HOL-749). Callers that pass a nil-or-empty
// pepper receive [ErrNilPepper]; the KDF refuses to operate without one so a
// forgotten pepper lookup is loud rather than silent.
//
// # Logging invariants
//
// Implementations in this package MUST NOT log plaintext, the pepper, the
// salt, or the derived hash, not even at debug verbosity. Error messages
// MUST NOT embed any of the four. [Envelope] is marshal-only: it does not
// implement [fmt.Stringer] or [fmt.GoStringer], so a stray %v / %+v print
// cannot smuggle the bytes into an operator log. The encoded envelope
// bytes live exclusively on the wire between [KDF.Hash] and the sibling
// v1.Secret — no CR, no event, no log.
//
// # Envelope versioning
//
// [Envelope] carries an integer [Envelope.SchemaVersion] so a future KDF row
// (for example, an argon2id v2 default or a PBKDF2-HMAC-SHA512 fallback row)
// can be read unambiguously by an older binary. M2 ships
// [EnvelopeSchemaVersion] = 1; bump it in the same commit that introduces a
// new on-wire shape and extend the decoder to tolerate both.
package crypto
