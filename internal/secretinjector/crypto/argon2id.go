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
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Argon2idDefault is the pinned default parameter set for the argon2id
// KDF binding. The values track the OWASP Password Storage Cheat Sheet
// argon2id recommendation "m=19456, t=2, p=1" (19 MiB, two passes, one
// lane) with a 32-byte output per RFC 9106 §4. See
// https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html#argon2id
// for the current guidance.
//
// The values are pinned in code rather than read from a config file so a
// silent parameter change is loud: bumping any field is a reviewable
// diff, and an envelope written with the old values cannot verify against
// the new values (see [ErrParamMismatch]), which forces a re-hash at
// next login.
//
// Memory is expressed in KiB to match [argon2.IDKey]'s API. 19456 KiB =
// 19 MiB.
var Argon2idDefault = Params{
	Time:        2,
	Memory:      19456,
	Parallelism: 1,
	KeyLength:   32,
}

// Argon2id is the default non-FIPS [KDF] binding. The zero value is
// ready to use: all configuration lives in [Params], and the caller
// routes params either via [Argon2id.DefaultParams] or by passing an
// explicit [Params] into [Argon2id.Hash].
type Argon2id struct{}

// ID reports the stable algorithm identifier argon2id writes into
// [Envelope.KDF].
func (Argon2id) ID() KDFID { return KDFArgon2id }

// DefaultParams returns [Argon2idDefault]. Callers are free to override
// for migrations (for example, to re-hash at higher cost), but the
// default is what the reconciler uses on the hot path.
func (Argon2id) DefaultParams() Params { return Argon2idDefault }

// Hash derives an encoded-hash [Envelope] via [argon2.IDKey]. Inputs are
// validated up front so an empty pepper or empty plaintext fails loudly
// rather than deriving a useless hash.
//
// Hash concatenates the pepper to the plaintext before feeding the
// combined buffer into [argon2.IDKey]. The pepper is NOT folded into the
// salt so a future pepper rotation can re-hash without regenerating the
// per-credential salt. See HOL-749 for the pepper rotation story.
//
// The returned envelope carries the effective [Params] so [Verify] can
// reject parameter drift.
func (a Argon2id) Hash(plaintext, salt, pepper []byte, pepperVersion string, params Params) (Envelope, error) {
	if err := validateInputs(plaintext, salt, pepper); err != nil {
		return Envelope{}, err
	}
	if pepperVersion == "" {
		return Envelope{}, ErrEmptyPepperVersion
	}
	if err := validateArgon2idParams(params); err != nil {
		return Envelope{}, err
	}
	peppered := append([]byte{}, plaintext...)
	peppered = append(peppered, pepper...)
	hash := argon2.IDKey(peppered, salt, params.Time, params.Memory, params.Parallelism, params.KeyLength)
	// Copy salt so later mutation of the caller's slice cannot corrupt
	// the envelope. The hash is freshly allocated by argon2.IDKey so no
	// defensive copy is needed on that field.
	saltCopy := append([]byte(nil), salt...)
	return Envelope{
		SchemaVersion: EnvelopeSchemaVersion,
		KDF:           KDFArgon2id,
		// Normalize params before stamping: zero out the PBKDF2-only
		// Iterations field so a caller that accidentally passed a
		// non-zero Iterations (for example, a params struct shared
		// with a PBKDF2 call site) cannot poison the envelope with a
		// field argon2id ignores. Combined with
		// argon2idParamsEqual's relevant-fields-only comparison on
		// Verify, this makes the argon2id envelope strictly describe
		// the argon2id cost and nothing else.
		KDFParams:     normalizeArgon2idParams(params),
		PepperVersion: pepperVersion,
		Salt:          saltCopy,
		Hash:          hash,
	}, nil
}

// normalizeArgon2idParams returns a [Params] that carries only the four
// fields argon2id consumes. Other fields (currently just the PBKDF2
// Iterations counter) are zeroed so the envelope is a faithful
// description of the argon2id cost actually paid.
func normalizeArgon2idParams(p Params) Params {
	return Params{
		Time:        p.Time,
		Memory:      p.Memory,
		Parallelism: p.Parallelism,
		KeyLength:   p.KeyLength,
	}
}

// Verify re-derives the hash from plaintext + envelope.Salt + pepper
// under envelope.KDFParams and compares to envelope.Hash in constant
// time. Verify rejects a KDF-identifier mismatch, a schema-version
// mismatch, and a [Params] drift (envelope.KDFParams != wantParams)
// before touching the primitive — the goal is that a silent parameter
// bump is impossible to verify against from the pluggable seam alone.
//
// To re-hash an envelope that was stored under old parameters during a
// cost-bump migration, the caller invokes [Argon2id.Hash] with the new
// Params and writes the new envelope verbatim to v1.Secret.data["hash"].
// Verify never silently accepts drift.
func (a Argon2id) Verify(plaintext, pepper []byte, envelope Envelope, wantParams Params) error {
	if envelope.SchemaVersion != EnvelopeSchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnknownSchemaVersion, envelope.SchemaVersion, EnvelopeSchemaVersion)
	}
	if envelope.KDF != KDFArgon2id {
		return fmt.Errorf("%w: envelope KDF %q, verifier %q", ErrKDFMismatch, envelope.KDF, KDFArgon2id)
	}
	if !argon2idParamsEqual(envelope.KDFParams, wantParams) {
		return ErrParamMismatch
	}
	if err := validateInputs(plaintext, envelope.Salt, pepper); err != nil {
		return err
	}
	if err := validateArgon2idParams(envelope.KDFParams); err != nil {
		return err
	}
	peppered := append([]byte{}, plaintext...)
	peppered = append(peppered, pepper...)
	candidate := argon2.IDKey(
		peppered,
		envelope.Salt,
		envelope.KDFParams.Time,
		envelope.KDFParams.Memory,
		envelope.KDFParams.Parallelism,
		envelope.KDFParams.KeyLength,
	)
	if !CompareHash(candidate, envelope.Hash) {
		return ErrHashMismatch
	}
	return nil
}

// validateArgon2idParams rejects params the argon2id primitive cannot
// operate with. The golang.org/x/crypto/argon2 API panics on
// parallelism=0, so we fail loudly with a structured error instead.
func validateArgon2idParams(p Params) error {
	if p.Time == 0 {
		return fmt.Errorf("%w: argon2id requires Time >= 1", ErrInvalidParams)
	}
	if p.Memory == 0 {
		return fmt.Errorf("%w: argon2id requires Memory >= 1 (KiB)", ErrInvalidParams)
	}
	if p.Parallelism == 0 {
		return fmt.Errorf("%w: argon2id requires Parallelism >= 1", ErrInvalidParams)
	}
	if p.KeyLength == 0 {
		return fmt.Errorf("%w: argon2id requires KeyLength >= 1", ErrInvalidParams)
	}
	return nil
}
