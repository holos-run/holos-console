# KDF Pluggability Reference

`internal/secretinjector/crypto` — M2 reference for HOL-747.

## Why a pluggable seam

The Secret Injection Service must satisfy two operating contexts with a
single codebase:

1. **Default builds** — most clusters, most operators. No FIPS constraint.
   Argon2id is the correct choice: it is the algorithm referenced in
   [OWASP Password Storage Cheat Sheet][owasp] and [RFC 9106][rfc9106],
   and its memory-hard design defends against ASIC-accelerated offline
   brute-force more effectively than iteration-count-only algorithms.

2. **FIPS-validated builds** (`-fips` build tag) — FedRAMP clusters where
   the NIST SP 800-131A validated algorithm set is contractually required.
   Argon2id is not in the validated set; PBKDF2-HMAC-SHA512 is. The
   `-fips` build tag swaps the binding at compile time without changing any
   reconciler logic.

The `KDF` interface is the seam that makes both contexts possible from a
single source tree. Reconcilers depend only on `KDF`; the concrete
primitive is injected by `Default()` in a build-tagged file.

## The KDF interface

`KDF` lives in `internal/secretinjector/crypto/kdf.go`. Its four-method
surface is deliberately minimal:

```
ID() KDFID
DefaultParams() Params
Hash(plaintext, salt, pepper []byte, pepperVersion string, params Params) (Envelope, error)
Verify(plaintext, pepper []byte, envelope Envelope, wantParams Params) error
```

`ID()` returns the stable algorithm identifier (`"argon2id"`,
`"pbkdf2-hmac-sha512"`) that travels on the wire inside every
`Envelope.KDF` field. A verifier routes to the matching binding by
comparing this string. Case differences are prevented: `KDFID` values are
lowercase ASCII by definition.

`DefaultParams()` returns the pinned cost parameters the reconciler uses on
the hot path. Parameters are not read from a config file or environment
variable: a silent parameter change is a reviewable diff, and an envelope
written under old parameters cannot verify against new parameters (see
`ErrParamMismatch`), which forces a re-hash at next login. Surfacing drift
as an error rather than tolerating it is a deliberate security property.

`Hash` concatenates the pepper to the plaintext before feeding the combined
buffer into the underlying primitive. The pepper is NOT folded into the
salt so a future pepper rotation can re-hash a credential without
regenerating its per-credential salt — the two can change independently.

`Verify` is strict: it rejects an envelope whose stored `KDFParams` differ
from the caller-supplied `wantParams` before touching the primitive. Drift
rejection is part of the interface contract, not a concrete-only extra. To
re-hash during a cost-bump migration the caller invokes `Hash` with the new
params and writes the new envelope; `Verify` is never permissive.

## Default binding: argon2id

`internal/secretinjector/crypto/argon2id.go`  
Build tag: `!fips` (active in all non-FIPS builds)

The default parameters track the OWASP recommendation:

| Parameter   | Value   | Meaning                  |
|-------------|---------|--------------------------|
| Time        | 2       | 2 passes (RFC 9106 §4 t) |
| Memory      | 19456   | 19 MiB in KiB            |
| Parallelism | 1       | single lane              |
| KeyLength   | 32      | 32-byte output           |

These values are pinned in `Argon2idDefault` in `argon2id.go`. Any change
to them is a breaking change to all envelopes stored under the previous
parameters: Verify will return `ErrParamMismatch` on the next login until
the hash is re-derived. That is intentional — the reconciler must explicitly
re-hash rather than silently accept a changed cost.

`argon2id.go` normalises params before stamping them onto the returned
`Envelope`: the PBKDF2-only `Iterations` field is zeroed so a shared
`Params` struct that was populated for a PBKDF2 call site cannot leak an
unrelated field into an argon2id envelope and cause spurious mismatches on
Verify.

## Reserved binding: PBKDF2-HMAC-SHA512

`internal/secretinjector/crypto/pbkdf2.go`  
Build tag: `fips` (excluded from all default builds)

The `-fips` placeholder reserves the `KDFID` constant
`"pbkdf2-hmac-sha512"` so verifier routing tables can include the string
before the primitive ships. The placeholder body is intentionally empty
under the `fips` tag: an `-fips` build that does NOT land a real
implementation fails loudly at link time on a missing `Default` symbol
rather than silently reverting to argon2id. A verifier running under a
non-FIPS binary that encounters an envelope with `kdf: "pbkdf2-hmac-sha512"`
returns `ErrKDFMismatch` via the standard routing path.

The full PBKDF2-HMAC-SHA512 implementation is deferred to a post-M2 ticket
under HOL-747.

## Build-tag wiring

`internal/secretinjector/crypto/default_nofips.go` (`!fips` build tag):

```go
func Default() KDF {
    return Argon2id{}
}
```

The `-fips` override will supply an identical `Default()` signature that
returns a `PBKDF2HMACSHA512{}` value. Reconcilers always call `Default()`
and never reference the concrete type, so the swap is invisible above the
`KDF` interface.

## Envelope: the on-wire JSON contract

Every successful `Hash` call returns an `Envelope` value. The Credential
reconciler serialises it via `MarshalEnvelope` and writes the resulting
bytes verbatim to `v1.Secret.data["envelope"]` on the hash Secret.

```json
{
  "schemaVersion": 1,
  "kdf": "argon2id",
  "kdfParams": {
    "time": 2,
    "memory": 19456,
    "parallelism": 1,
    "keyLength": 32
  },
  "pepperVersion": "1",
  "salt": "<base64-encoded 32-byte random salt — PLACEHOLDER_DO_NOT_COPY>",
  "hash": "<base64-encoded 32-byte derived hash — PLACEHOLDER_DO_NOT_COPY>"
}
```

The `Envelope` struct has no `String` method, no `GoString` method, and no
logging helpers: a stray `%v` cannot leak hash bytes into an operator log.
The envelope is the ONLY legitimate carrier of hash material out of
`internal/secretinjector/crypto`; callers that reach into fields directly
rather than round-tripping through `MarshalEnvelope`/`UnmarshalEnvelope`
violate the contract.

`UnmarshalEnvelope` rejects any envelope whose `schemaVersion` is not
equal to `EnvelopeSchemaVersion` (currently 1). A future schema extension
bumps this constant in the same commit that introduces the new shape and
extends the decoder to tolerate both versions. An older binary that sees a
`schemaVersion: 2` envelope returns `ErrUnknownSchemaVersion` rather than
silently misinterpreting the record.

## Errors

All sentinel errors live in `kdf.go` and are matchable with `errors.Is`:

| Error | Trigger |
|-------|---------|
| `ErrNilPepper` | Hash/Verify called with nil or empty pepper |
| `ErrEmptyPlaintext` | Hash/Verify called with nil or empty plaintext |
| `ErrEmptySalt` | Hash/Verify called with nil or empty salt |
| `ErrEmptyPepperVersion` | Hash called without a pepper version string |
| `ErrKDFMismatch` | Verify: envelope KDF != receiver ID |
| `ErrParamMismatch` | Verify: envelope KDFParams != wantParams |
| `ErrUnknownSchemaVersion` | Verify: schemaVersion not recognized |
| `ErrHashMismatch` | Verify: constant-time comparison failed |
| `ErrInvalidParams` | Hash/Verify: zero or nonsensical params field |

## Testing

Unit tests live at:

- `internal/secretinjector/crypto/kdf_test.go` — interface contract tests
- `internal/secretinjector/crypto/argon2id_test.go` (via `kdf_test.go`)
- `internal/secretinjector/crypto/default_nofips_test.go`

The cross-reconciler envtest suite in
`internal/secretinjector/controller/suite_test.go` exercises the full
Hash→Envelope→marshal path against a real API server and validates that no
envelope bytes appear on any CR (marshal-scan gate, see
`internal/secretinjector/controller/invariant_test.go`).

[owasp]: https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html#argon2id
[rfc9106]: https://www.rfc-editor.org/rfc/rfc9106
