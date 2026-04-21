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

package crypto

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
)

// KDFID identifies a key-derivation algorithm by a stable string. The
// string travels on the wire inside [Envelope.KDF] so a verifier can route
// to the matching implementation years after the bytes were written. Values
// MUST be lowercase ASCII so case differences cannot split a single
// algorithm into two routing buckets.
type KDFID string

const (
	// KDFArgon2id identifies the default argon2id binding from argon2id.go.
	KDFArgon2id KDFID = "argon2id"
	// KDFPBKDF2HMACSHA512 identifies the -fips build variant's PBKDF2
	// binding that will land under pbkdf2.go. The placeholder file keeps
	// the constant reserved so verifier tables can route on the string
	// before the primitive ships.
	KDFPBKDF2HMACSHA512 KDFID = "pbkdf2-hmac-sha512"
)

// EnvelopeSchemaVersion is the on-wire schema version that every Envelope
// written by this package stamps into [Envelope.SchemaVersion]. Bump this
// constant in the same commit that introduces a new on-wire shape and
// extend the decoder to tolerate both versions. M2 ships version 1.
const EnvelopeSchemaVersion = 1

// Params pins the KDF cost parameters for a single Hash / Verify call. The
// fields are a superset across every KDF implementation this package hosts
// so a single [Params] value can be plumbed through the reconciler and
// dispatched to any [KDF] without type-switching. Each implementation
// documents which fields it reads and validates the rest away as ignored.
//
// The field set is deliberately flat and JSON-round-trippable: [Envelope]
// stores the effective [Params] so a verifier reconstructs the exact
// settings used to derive the hash without consulting a side channel.
type Params struct {
	// Time is the argon2id pass count (RFC 9106 §4 `t`). Ignored by
	// PBKDF2-HMAC-SHA512.
	Time uint32 `json:"time,omitempty"`
	// Memory is the argon2id memory cost in KiB (RFC 9106 §4 `m`). Ignored
	// by PBKDF2-HMAC-SHA512.
	Memory uint32 `json:"memory,omitempty"`
	// Parallelism is the argon2id lane count (RFC 9106 §4 `p`). Ignored
	// by PBKDF2-HMAC-SHA512.
	Parallelism uint8 `json:"parallelism,omitempty"`
	// KeyLength is the derived-hash length in bytes. All bindings honor
	// this field. argon2id's RFC 9106 recommendation is 32.
	KeyLength uint32 `json:"keyLength,omitempty"`
	// Iterations is the PBKDF2 iteration count. Ignored by argon2id. The
	// field is present so an -fips binary's PBKDF2 binding can use the
	// same [Params] struct.
	Iterations uint32 `json:"iterations,omitempty"`
}

// Envelope is the self-describing record the Credential reconciler (HOL-751)
// will write verbatim to the data["hash"] key of a sibling v1.Secret. The
// shape is deliberately marshal-only: Envelope has no String method, no
// GoString method, and no logging helpers so a stray %v can never leak the
// hash bytes into an operator log. The envelope is the ONLY legitimate
// carrier of hash material out of this package — callers that reach into
// the fields directly are violating the contract.
//
// The struct ships a [Envelope.SchemaVersion] so a future KDF row can be
// read unambiguously by an older verifier: the decoder rejects an unknown
// schema version rather than silently mis-parsing a v2 envelope as v1.
type Envelope struct {
	// SchemaVersion pins the on-wire shape. Always equal to
	// [EnvelopeSchemaVersion] when written by this package.
	SchemaVersion int `json:"schemaVersion"`
	// KDF identifies the algorithm that derived [Envelope.Hash]. The
	// verifier routes on this field.
	KDF KDFID `json:"kdf"`
	// KDFParams are the cost parameters used to derive [Envelope.Hash].
	// Verify MUST compare these against the caller-supplied [Params] and
	// refuse to verify on drift — a silent parameter change would be a
	// security regression.
	KDFParams Params `json:"kdfParams"`
	// PepperVersion identifies the pepper row used to derive
	// [Envelope.Hash]. The Credential reconciler uses this to route to
	// the matching pepper bytes when multiple pepper versions coexist
	// during a rotation (HOL-749).
	PepperVersion string `json:"pepperVersion"`
	// Salt is the per-credential random salt. The verifier feeds it back
	// into the KDF on Verify.
	Salt []byte `json:"salt"`
	// Hash is the derived-hash output. Compared in constant time on
	// Verify via [CompareHash].
	Hash []byte `json:"hash"`
}

// KDF is the pluggability seam. The default non-FIPS build binds
// [Argon2id] via [Default]; the -fips build variant will bind a
// PBKDF2-HMAC-SHA512 implementation by swapping [Default] in a
// build-tagged file. Reconcilers depend on the interface only.
//
// Implementations MUST:
//
//   - Refuse to operate when plaintext, salt, or pepper is nil or empty
//     (see [ErrEmptyPlaintext], [ErrEmptySalt], [ErrNilPepper]). The KDF
//     refuses rather than defaulting so a forgotten pepper lookup fails
//     loudly.
//   - Stamp [Envelope.KDFParams] with the effective parameters used so
//     the caller can observe what cost they paid, and so Verify can
//     reject parameter drift via [ErrParamMismatch].
//   - Reject a Verify call whose caller-supplied wantParams differ from
//     [Envelope.KDFParams], before touching the primitive. A silent
//     parameter bump would be a security regression; the interface
//     denies this path.
//   - Compare hashes in constant time on Verify (see [CompareHash]) to
//     deny timing side channels.
//   - Never log plaintext, pepper, salt, or hash bytes — not even at
//     debug verbosity. Error messages MUST NOT embed any of the four.
type KDF interface {
	// ID returns the stable algorithm identifier this KDF writes into
	// [Envelope.KDF]. Verifiers route on the identifier.
	ID() KDFID

	// DefaultParams returns the pinned cost parameters this KDF uses
	// when the caller does not override. Pinned values are documented on
	// the concrete implementation (for argon2id, see [Argon2idDefault]).
	DefaultParams() Params

	// Hash derives an encoded-hash [Envelope] from plaintext + salt +
	// pepper under params. pepperVersion is stamped verbatim onto
	// [Envelope.PepperVersion] so Verify can route to the matching
	// pepper bytes on future calls.
	Hash(plaintext, salt, pepper []byte, pepperVersion string, params Params) (Envelope, error)

	// Verify checks that plaintext + envelope.Salt + pepper derives
	// envelope.Hash under wantParams. The method MUST reject an
	// envelope whose [Envelope.KDFParams] differ from wantParams with
	// [ErrParamMismatch] before touching the primitive: drift rejection
	// is part of the interface contract, not a concrete-only extra. To
	// re-hash an old-parameter envelope during a migration, a caller
	// invokes Hash with the new params and writes the new envelope;
	// Verify is never permissive about cost.
	//
	// Returns nil on match, [ErrHashMismatch] on a constant-time
	// mismatch, or one of the validation errors when the inputs are
	// unusable.
	Verify(plaintext, pepper []byte, envelope Envelope, wantParams Params) error
}

// Errors returned by this package. They are sentinel values so callers can
// match with [errors.Is] without string parsing.
var (
	// ErrNilPepper is returned when a KDF is invoked with a nil or empty
	// pepper. The KDF refuses to fall back to an unpeppered hash so a
	// missing pepper bootstrap is loud rather than silent.
	ErrNilPepper = errors.New("crypto: pepper must be non-empty")
	// ErrEmptyPlaintext is returned when a KDF is invoked with a nil or
	// empty plaintext.
	ErrEmptyPlaintext = errors.New("crypto: plaintext must be non-empty")
	// ErrEmptySalt is returned when a KDF is invoked with a nil or empty
	// salt.
	ErrEmptySalt = errors.New("crypto: salt must be non-empty")
	// ErrEmptyPepperVersion is returned by Hash when the caller does not
	// supply a pepper version string. A pepper version is required so a
	// verifier can route to the right pepper bytes after rotation.
	ErrEmptyPepperVersion = errors.New("crypto: pepper version must be non-empty")
	// ErrKDFMismatch is returned by Verify when the envelope's KDF
	// identifier does not match the receiver's [KDF.ID].
	ErrKDFMismatch = errors.New("crypto: envelope KDF does not match verifier")
	// ErrParamMismatch is returned by Verify when the envelope's stored
	// [Params] differ from the caller-supplied [Params]. A silent
	// parameter change would be a security regression; the KDF refuses
	// to verify on drift.
	ErrParamMismatch = errors.New("crypto: envelope params differ from verifier params")
	// ErrUnknownSchemaVersion is returned by Verify when the envelope's
	// [Envelope.SchemaVersion] is not recognized. A future schema row
	// extends the decoder in lock-step with the producer.
	ErrUnknownSchemaVersion = errors.New("crypto: envelope schema version is not recognized")
	// ErrHashMismatch is returned by Verify on a constant-time hash
	// mismatch. The error does NOT embed the hash bytes.
	ErrHashMismatch = errors.New("crypto: hash mismatch")
	// ErrInvalidParams is returned by Hash / Verify when a [Params] field
	// that the concrete KDF relies on is zero or otherwise nonsensical.
	ErrInvalidParams = errors.New("crypto: invalid params for KDF")
)

// CompareHash reports whether a and b are equal in time that does not
// depend on their contents. Callers use the helper to avoid a timing side
// channel where a byte-wise short-circuit comparator leaks hash prefix
// length through wall-clock time.
//
// The function is a thin wrapper around [subtle.ConstantTimeCompare] so
// the intent is legible at the call site and the package under review
// does not have a free-floating import of crypto/subtle scattered across
// files. When lengths differ, CompareHash returns false in constant time
// relative to min(len(a), len(b)).
func CompareHash(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// MarshalEnvelope serializes e into the canonical JSON shape the
// Credential reconciler writes to v1.Secret.data["hash"]. The output is
// deterministic for a given envelope because [json.Marshal] iterates
// struct fields in declaration order and byte slices encode as base64.
// Callers MUST feed the returned bytes verbatim to v1.Secret; mutating
// the bytes breaks Verify.
func MarshalEnvelope(e Envelope) ([]byte, error) {
	return json.Marshal(e)
}

// UnmarshalEnvelope is the inverse of [MarshalEnvelope]. The decoder
// rejects an envelope whose schema version is outside the set of versions
// this binary understands, so a future envelope shape does not silently
// parse as a v1 record.
func UnmarshalEnvelope(data []byte, e *Envelope) error {
	if e == nil {
		return fmt.Errorf("crypto: UnmarshalEnvelope destination is nil")
	}
	if err := json.Unmarshal(data, e); err != nil {
		return fmt.Errorf("crypto: unmarshal envelope: %w", err)
	}
	if e.SchemaVersion != EnvelopeSchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnknownSchemaVersion, e.SchemaVersion, EnvelopeSchemaVersion)
	}
	return nil
}

// paramsEqual reports whether two [Params] values are equal across every
// field the verifier cares about. Used by concrete KDF Verify
// implementations to flag parameter drift before attempting a compare.
// The helper lives here rather than on [Params] so the equality check is
// owned by the package that defines the sentinel errors.
func paramsEqual(a, b Params) bool {
	return a.Time == b.Time &&
		a.Memory == b.Memory &&
		a.Parallelism == b.Parallelism &&
		a.KeyLength == b.KeyLength &&
		a.Iterations == b.Iterations
}

// validateInputs enforces the non-empty-input invariants shared by every
// KDF binding. Concrete implementations call this before invoking the
// primitive so a missing pepper fails loudly instead of deriving an
// unpeppered hash.
func validateInputs(plaintext, salt, pepper []byte) error {
	if len(plaintext) == 0 {
		return ErrEmptyPlaintext
	}
	if len(salt) == 0 {
		return ErrEmptySalt
	}
	if len(pepper) == 0 {
		return ErrNilPepper
	}
	return nil
}
