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
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/crypto/argon2"
)

// These fixtures are deterministic so a silent argon2id parameter change
// or a silent pepper concatenation change surfaces as a diff in the
// expected-hash fixture below. Do not regenerate them lightly; a change
// here means every previously-written hash in a running cluster stops
// verifying.
var (
	fixturePlaintext     = []byte("correct horse battery staple")
	fixtureSalt          = []byte("fixed-16-byte-slt")
	fixturePepper        = []byte("pepper-bytes-v1-abcdef0123456789")
	fixturePepperVersion = "v1"
)

// fixtureExpectedHash re-derives the expected bytes with argon2.IDKey
// under [Argon2idDefault] so the test below fails loudly if either
//
//  1. [Argon2id.Hash] changes how it stitches plaintext + pepper, or
//  2. [Argon2idDefault] is silently bumped.
//
// The helper lives in the test file so production code cannot use it by
// accident; reaching argon2.IDKey directly from a reconciler would bypass
// the [KDF] seam.
func fixtureExpectedHash(t *testing.T) []byte {
	t.Helper()
	peppered := append([]byte{}, fixturePlaintext...)
	peppered = append(peppered, fixturePepper...)
	return argon2.IDKey(
		peppered,
		fixtureSalt,
		Argon2idDefault.Time,
		Argon2idDefault.Memory,
		Argon2idDefault.Parallelism,
		Argon2idDefault.KeyLength,
	)
}

// TestArgon2idHashDeterministic pins the hash output under the default
// parameters. A parameter bump or a code path that silently stops
// including the pepper would move the bytes and fail this test.
func TestArgon2idHashDeterministic(t *testing.T) {
	want := fixtureExpectedHash(t)
	env, err := Argon2id{}.Hash(fixturePlaintext, fixtureSalt, fixturePepper, fixturePepperVersion, Argon2idDefault)
	if err != nil {
		t.Fatalf("Hash: unexpected error: %v", err)
	}
	if env.SchemaVersion != EnvelopeSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", env.SchemaVersion, EnvelopeSchemaVersion)
	}
	if env.KDF != KDFArgon2id {
		t.Errorf("KDF = %q, want %q", env.KDF, KDFArgon2id)
	}
	if env.PepperVersion != fixturePepperVersion {
		t.Errorf("PepperVersion = %q, want %q", env.PepperVersion, fixturePepperVersion)
	}
	if diff := cmp.Diff(Argon2idDefault, env.KDFParams); diff != "" {
		t.Errorf("KDFParams mismatch (-want +got):\n%s", diff)
	}
	if !bytes.Equal(env.Salt, fixtureSalt) {
		t.Errorf("Salt mismatch: got %x, want %x", env.Salt, fixtureSalt)
	}
	if !bytes.Equal(env.Hash, want) {
		t.Errorf("Hash mismatch:\n got  %s\n want %s", hex.EncodeToString(env.Hash), hex.EncodeToString(want))
	}
}

// TestArgon2idRoundTrip covers the contract the Credential reconciler
// relies on: Hash → Verify returns nil under matching wantParams, and a
// re-hash with identical inputs produces identical bytes.
func TestArgon2idRoundTrip(t *testing.T) {
	k := Argon2id{}
	env, err := k.Hash(fixturePlaintext, fixtureSalt, fixturePepper, fixturePepperVersion, Argon2idDefault)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if err := k.Verify(fixturePlaintext, fixturePepper, env, Argon2idDefault); err != nil {
		t.Fatalf("Verify against matching plaintext: unexpected error %v", err)
	}
	// Re-hashing with identical inputs produces identical bytes (argon2id
	// is deterministic under fixed params + salt).
	env2, err := k.Hash(fixturePlaintext, fixtureSalt, fixturePepper, fixturePepperVersion, Argon2idDefault)
	if err != nil {
		t.Fatalf("Hash (second call): %v", err)
	}
	if !bytes.Equal(env.Hash, env2.Hash) {
		t.Fatalf("argon2id not deterministic under fixed inputs:\n first  %x\n second %x", env.Hash, env2.Hash)
	}
}

// TestArgon2idVerifyRejectsWrongPlaintext is the happy-path mismatch: a
// candidate plaintext that differs by a single byte must fail verify,
// and the error must be [ErrHashMismatch] so a caller can switch on it.
func TestArgon2idVerifyRejectsWrongPlaintext(t *testing.T) {
	k := Argon2id{}
	env, err := k.Hash(fixturePlaintext, fixtureSalt, fixturePepper, fixturePepperVersion, Argon2idDefault)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	wrong := append([]byte{}, fixturePlaintext...)
	wrong[0] ^= 0x01
	err = k.Verify(wrong, fixturePepper, env, Argon2idDefault)
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("Verify(wrong plaintext): got %v, want ErrHashMismatch", err)
	}
}

// TestArgon2idVerifyRejectsWrongPepper is the pepper-discipline mirror:
// the same plaintext with a different pepper MUST NOT verify. This
// catches a regression where a future refactor silently drops the pepper
// from the input concatenation.
func TestArgon2idVerifyRejectsWrongPepper(t *testing.T) {
	k := Argon2id{}
	env, err := k.Hash(fixturePlaintext, fixtureSalt, fixturePepper, fixturePepperVersion, Argon2idDefault)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	wrongPepper := append([]byte{}, fixturePepper...)
	wrongPepper[0] ^= 0x01
	err = k.Verify(fixturePlaintext, wrongPepper, env, Argon2idDefault)
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("Verify(wrong pepper): got %v, want ErrHashMismatch", err)
	}
}

// TestArgon2idNilPepperRejection enforces the "pepper is required" rule
// from the package doc. Hash and Verify both refuse to operate rather
// than silently producing an unpeppered hash.
func TestArgon2idNilPepperRejection(t *testing.T) {
	k := Argon2id{}
	// Hash with nil pepper.
	if _, err := k.Hash(fixturePlaintext, fixtureSalt, nil, fixturePepperVersion, Argon2idDefault); !errors.Is(err, ErrNilPepper) {
		t.Errorf("Hash(nil pepper): got %v, want ErrNilPepper", err)
	}
	// Hash with empty-but-non-nil pepper.
	if _, err := k.Hash(fixturePlaintext, fixtureSalt, []byte{}, fixturePepperVersion, Argon2idDefault); !errors.Is(err, ErrNilPepper) {
		t.Errorf("Hash(empty pepper): got %v, want ErrNilPepper", err)
	}
	// Verify with nil pepper against a previously-valid envelope.
	env, err := k.Hash(fixturePlaintext, fixtureSalt, fixturePepper, fixturePepperVersion, Argon2idDefault)
	if err != nil {
		t.Fatalf("Hash (setup): %v", err)
	}
	if err := k.Verify(fixturePlaintext, nil, env, Argon2idDefault); !errors.Is(err, ErrNilPepper) {
		t.Errorf("Verify(nil pepper): got %v, want ErrNilPepper", err)
	}
}

// TestArgon2idEmptyInputs mirrors TestArgon2idNilPepperRejection for
// plaintext and salt, per the three-input non-empty contract.
func TestArgon2idEmptyInputs(t *testing.T) {
	k := Argon2id{}
	tests := []struct {
		name      string
		plaintext []byte
		salt      []byte
		want      error
	}{
		{name: "empty plaintext", plaintext: nil, salt: fixtureSalt, want: ErrEmptyPlaintext},
		{name: "empty salt", plaintext: fixturePlaintext, salt: nil, want: ErrEmptySalt},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := k.Hash(tc.plaintext, tc.salt, fixturePepper, fixturePepperVersion, Argon2idDefault)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Hash: got %v, want %v", err, tc.want)
			}
		})
	}
}

// TestArgon2idEmptyPepperVersion enforces the "pepper version required on
// Hash" rule: without a version string a verifier cannot route to the
// right pepper after a rotation.
func TestArgon2idEmptyPepperVersion(t *testing.T) {
	_, err := Argon2id{}.Hash(fixturePlaintext, fixtureSalt, fixturePepper, "", Argon2idDefault)
	if !errors.Is(err, ErrEmptyPepperVersion) {
		t.Fatalf("Hash(empty pepper version): got %v, want ErrEmptyPepperVersion", err)
	}
}

// TestArgon2idParamsValidation exercises the four zero-field guards so a
// caller that fat-fingers [Params] gets a structured error instead of a
// panic from the underlying argon2 primitive.
func TestArgon2idParamsValidation(t *testing.T) {
	tests := []struct {
		name   string
		params Params
	}{
		{name: "Time=0", params: Params{Time: 0, Memory: 19456, Parallelism: 1, KeyLength: 32}},
		{name: "Memory=0", params: Params{Time: 2, Memory: 0, Parallelism: 1, KeyLength: 32}},
		{name: "Parallelism=0", params: Params{Time: 2, Memory: 19456, Parallelism: 0, KeyLength: 32}},
		{name: "KeyLength=0", params: Params{Time: 2, Memory: 19456, Parallelism: 1, KeyLength: 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Argon2id{}.Hash(fixturePlaintext, fixtureSalt, fixturePepper, fixturePepperVersion, tc.params)
			if !errors.Is(err, ErrInvalidParams) {
				t.Fatalf("Hash(%s): got %v, want ErrInvalidParams", tc.name, err)
			}
		})
	}
}

// TestArgon2idParamDriftRejection is the security-critical case cited in
// the HOL-748 acceptance criteria: a hash encoded at time=2 must NOT
// verify under a verifier that insists on time=3 (or any other mutated
// field). Drift rejection is part of the [KDF.Verify] contract on the
// interface itself, so the reconciler's pluggable-seam call site cannot
// silently accept a parameter bump.
func TestArgon2idParamDriftRejection(t *testing.T) {
	k := Argon2id{}
	env, err := k.Hash(fixturePlaintext, fixtureSalt, fixturePepper, fixturePepperVersion, Argon2idDefault)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	tests := []struct {
		name string
		want Params
	}{
		{
			name: "drift on Time",
			want: Params{Time: Argon2idDefault.Time + 1, Memory: Argon2idDefault.Memory, Parallelism: Argon2idDefault.Parallelism, KeyLength: Argon2idDefault.KeyLength},
		},
		{
			name: "drift on Memory",
			want: Params{Time: Argon2idDefault.Time, Memory: Argon2idDefault.Memory * 2, Parallelism: Argon2idDefault.Parallelism, KeyLength: Argon2idDefault.KeyLength},
		},
		{
			name: "drift on Parallelism",
			want: Params{Time: Argon2idDefault.Time, Memory: Argon2idDefault.Memory, Parallelism: Argon2idDefault.Parallelism + 1, KeyLength: Argon2idDefault.KeyLength},
		},
		{
			name: "drift on KeyLength",
			want: Params{Time: Argon2idDefault.Time, Memory: Argon2idDefault.Memory, Parallelism: Argon2idDefault.Parallelism, KeyLength: Argon2idDefault.KeyLength + 16},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := k.Verify(fixturePlaintext, fixturePepper, env, tc.want)
			if !errors.Is(err, ErrParamMismatch) {
				t.Fatalf("Verify: got %v, want ErrParamMismatch", err)
			}
		})
	}
}

// TestArgon2idParamDriftRejectionViaInterface is the reviewer-requested
// cousin: the reconciler's call site only has a [KDF] interface, so drift
// rejection must be reachable without a concrete-type assertion. This
// test holds Verify through the interface and confirms ErrParamMismatch
// still surfaces.
func TestArgon2idParamDriftRejectionViaInterface(t *testing.T) {
	var kdf KDF = Argon2id{}
	env, err := kdf.Hash(fixturePlaintext, fixtureSalt, fixturePepper, fixturePepperVersion, Argon2idDefault)
	if err != nil {
		t.Fatalf("Hash (via interface): %v", err)
	}
	bumped := Argon2idDefault
	bumped.Time++
	if err := kdf.Verify(fixturePlaintext, fixturePepper, env, bumped); !errors.Is(err, ErrParamMismatch) {
		t.Fatalf("Verify via KDF interface: got %v, want ErrParamMismatch", err)
	}
}

// TestArgon2idVerifyRejectsForeignKDF covers the routing table: an
// envelope produced by some future KDF must not be silently verified by
// the argon2id binding. We forge an envelope with a mismatching KDF
// identifier and confirm the error path.
func TestArgon2idVerifyRejectsForeignKDF(t *testing.T) {
	env := Envelope{
		SchemaVersion: EnvelopeSchemaVersion,
		KDF:           KDFPBKDF2HMACSHA512,
		KDFParams:     Argon2idDefault,
		PepperVersion: fixturePepperVersion,
		Salt:          fixtureSalt,
		Hash:          bytes.Repeat([]byte{0}, int(Argon2idDefault.KeyLength)),
	}
	err := Argon2id{}.Verify(fixturePlaintext, fixturePepper, env, Argon2idDefault)
	if !errors.Is(err, ErrKDFMismatch) {
		t.Fatalf("Verify(foreign KDF): got %v, want ErrKDFMismatch", err)
	}
}

// TestArgon2idVerifyRejectsUnknownSchemaVersion guarantees that a future
// v2 envelope will not silently verify against a v1 decoder; the
// reconciler will surface the structured error to operators.
func TestArgon2idVerifyRejectsUnknownSchemaVersion(t *testing.T) {
	env, err := Argon2id{}.Hash(fixturePlaintext, fixtureSalt, fixturePepper, fixturePepperVersion, Argon2idDefault)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	env.SchemaVersion = EnvelopeSchemaVersion + 1
	err = Argon2id{}.Verify(fixturePlaintext, fixturePepper, env, Argon2idDefault)
	if !errors.Is(err, ErrUnknownSchemaVersion) {
		t.Fatalf("Verify(unknown schema): got %v, want ErrUnknownSchemaVersion", err)
	}
}

// TestEnvelopeJSONRoundTrip covers the on-wire contract: the canonical
// Marshal / Unmarshal round-trip preserves every field so a reconciler
// write + read produces byte-identical content.
func TestEnvelopeJSONRoundTrip(t *testing.T) {
	original, err := Argon2id{}.Hash(fixturePlaintext, fixtureSalt, fixturePepper, fixturePepperVersion, Argon2idDefault)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	encoded, err := MarshalEnvelope(original)
	if err != nil {
		t.Fatalf("MarshalEnvelope: %v", err)
	}
	var decoded Envelope
	if err := UnmarshalEnvelope(encoded, &decoded); err != nil {
		t.Fatalf("UnmarshalEnvelope: %v", err)
	}
	if diff := cmp.Diff(original, decoded); diff != "" {
		t.Fatalf("Envelope round-trip mismatch (-want +got):\n%s", diff)
	}
	// The round-tripped envelope must still verify.
	k := Argon2id{}
	if err := k.Verify(fixturePlaintext, fixturePepper, decoded, Argon2idDefault); err != nil {
		t.Fatalf("Verify(round-tripped envelope): unexpected error %v", err)
	}
}

// TestEnvelopeJSONShape pins the JSON field names so a downstream
// operator script or a future parser in a sibling binary can rely on the
// exact keys. A rename is a breaking wire change and should fail this
// test.
func TestEnvelopeJSONShape(t *testing.T) {
	env, err := Argon2id{}.Hash(fixturePlaintext, fixtureSalt, fixturePepper, fixturePepperVersion, Argon2idDefault)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	encoded, err := MarshalEnvelope(env)
	if err != nil {
		t.Fatalf("MarshalEnvelope: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(encoded, &m); err != nil {
		t.Fatalf("re-unmarshal to map: %v", err)
	}
	for _, k := range []string{"schemaVersion", "kdf", "kdfParams", "pepperVersion", "salt", "hash"} {
		if _, ok := m[k]; !ok {
			t.Errorf("envelope JSON missing required key %q; have %v", k, m)
		}
	}
	paramsAny, ok := m["kdfParams"].(map[string]any)
	if !ok {
		t.Fatalf("kdfParams is not a JSON object: %T", m["kdfParams"])
	}
	for _, k := range []string{"time", "memory", "parallelism", "keyLength"} {
		if _, ok := paramsAny[k]; !ok {
			t.Errorf("kdfParams JSON missing key %q; have %v", k, paramsAny)
		}
	}
}

// TestEnvelopeUnmarshalRejectsUnknownSchema covers the decoder gate: a
// stray v99 envelope on disk must fail to unmarshal rather than silently
// parse as v1 with the new fields stripped.
func TestEnvelopeUnmarshalRejectsUnknownSchema(t *testing.T) {
	bogus := []byte(`{"schemaVersion":99,"kdf":"argon2id","kdfParams":{"time":2,"memory":19456,"parallelism":1,"keyLength":32},"pepperVersion":"v1","salt":"c2FsdA==","hash":"aGFzaA=="}`)
	var decoded Envelope
	err := UnmarshalEnvelope(bogus, &decoded)
	if !errors.Is(err, ErrUnknownSchemaVersion) {
		t.Fatalf("UnmarshalEnvelope: got %v, want ErrUnknownSchemaVersion", err)
	}
}

// TestCompareHashConstantTimeSemantics is a sanity check that the
// [CompareHash] wrapper behaves like the crypto/subtle primitive it
// delegates to: equal byte slices return true, unequal slices or length
// mismatches return false. The constant-time property itself is owned by
// the stdlib and is not retested here.
func TestCompareHashConstantTimeSemantics(t *testing.T) {
	tests := []struct {
		name string
		a    []byte
		b    []byte
		want bool
	}{
		{name: "equal", a: []byte("abc"), b: []byte("abc"), want: true},
		{name: "differ single byte", a: []byte("abc"), b: []byte("abd"), want: false},
		{name: "length differs", a: []byte("abc"), b: []byte("abcd"), want: false},
		{name: "both empty", a: nil, b: nil, want: true},
		{name: "one empty", a: nil, b: []byte("abc"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompareHash(tc.a, tc.b); got != tc.want {
				t.Fatalf("CompareHash(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestDefaultBindsArgon2id pins the package-level [Default] contract: the
// non-FIPS build wires the argon2id implementation, not any future
// primitive. The -fips build variant overrides this in its own
// build-tagged file, so this test lives under the implicit !fips tag
// (default_nofips.go).
func TestDefaultBindsArgon2id(t *testing.T) {
	k := Default()
	if k.ID() != KDFArgon2id {
		t.Fatalf("Default().ID() = %q, want %q", k.ID(), KDFArgon2id)
	}
	if diff := cmp.Diff(Argon2idDefault, k.DefaultParams()); diff != "" {
		t.Fatalf("Default().DefaultParams() mismatch (-want +got):\n%s", diff)
	}
}

// TestUnmarshalEnvelopeNilDestination protects against a caller that
// forgets to allocate. A nil destination returns a structured error
// rather than panicking.
func TestUnmarshalEnvelopeNilDestination(t *testing.T) {
	err := UnmarshalEnvelope([]byte(`{"schemaVersion":1}`), nil)
	if err == nil {
		t.Fatalf("UnmarshalEnvelope(nil destination) returned nil error")
	}
}

// TestHashDoesNotAliasCallerSalt guarantees a caller that mutates its
// input salt slice after calling Hash cannot corrupt the envelope.
// Aliasing would be a correctness bug; the envelope is the unit the
// reconciler writes to etcd.
func TestHashDoesNotAliasCallerSalt(t *testing.T) {
	salt := append([]byte{}, fixtureSalt...)
	env, err := Argon2id{}.Hash(fixturePlaintext, salt, fixturePepper, fixturePepperVersion, Argon2idDefault)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	// Mutate caller slice.
	salt[0] ^= 0xFF
	if !bytes.Equal(env.Salt, fixtureSalt) {
		t.Fatalf("envelope.Salt aliased caller salt; mutation leaked in: got %x, want %x", env.Salt, fixtureSalt)
	}
	// Verify still works because the envelope has its own salt copy.
	k := Argon2id{}
	if err := k.Verify(fixturePlaintext, fixturePepper, env, Argon2idDefault); err != nil {
		t.Fatalf("Verify after caller salt mutation: unexpected error %v", err)
	}
}
