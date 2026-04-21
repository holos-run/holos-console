//go:build fips

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

// This file is the FIPS-variant placeholder for the PBKDF2-HMAC-SHA512
// binding. It is excluded from every non-FIPS build by the //go:build fips
// tag above, so the default holos-secret-injector binary that M2 ships
// never compiles against it.
//
// A future FIPS build-variant ticket (tracked as a post-M2 sub-issue of
// HOL-747 — the M2 plan explicitly scopes the pluggability seam here and
// defers the PBKDF2-HMAC-SHA512 primitive) will replace this body with a
// real implementation that satisfies the [KDF] interface and is bound as
// [Default] for FIPS builds via a build-tagged override.
//
// The body is intentionally empty under the fips tag so an -fips build
// that does NOT land the override still fails loudly at link time on a
// missing [Default] symbol rather than silently reverting to the argon2id
// binding. A verifier that encounters an [Envelope] whose [Envelope.KDF]
// is [KDFPBKDF2HMACSHA512] and that runs in a non-FIPS binary returns
// [ErrKDFMismatch] via the normal routing path in kdf.go.

package crypto
