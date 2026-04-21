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

import "regexp"

// ForbiddenBytePatterns is the shared catalog of regular expressions
// that MUST NOT appear in the marshalled (JSON or YAML) form of any
// object in the secrets.holos.run/v1alpha1 API group. The list is the
// wire-level complement of DefaultForbiddenFieldNameRules in
// invariant_helper_test.go: the rule set guards against forbidden
// field names on a CR type, and this catalog guards against forbidden
// byte sequences in the marshalled output of any instance.
//
// Consumed by:
//
//   - api/secrets/v1alpha1/credential_invariant_test.go — the
//     per-kind fixture test that marshals a fully-populated Credential
//     and asserts each pattern produces zero matches.
//   - internal/secretinjector/controller/invariant_test.go — the
//     envtest cross-reconciler gate (HOL-753) that GETs every CR after
//     every Reconcile, marshals JSON + YAML, and fails on any match
//     without printing the offending bytes.
//
// Exporting this list from a non-test file is deliberate: the
// controller package cannot import the api package's *_test.go
// helpers, and duplicating the regexes per consumer invites drift the
// invariant test cannot catch. Per-pattern entries carry a Name so a
// failing envtest reports the violated pattern without dumping the
// marshalled bytes (the bytes may themselves contain credential
// material — see api/secrets/v1alpha1/doc.go).
//
// Pattern catalog:
//
//   - apiKeyPattern: `sih_[A-Za-z0-9_-]{20,}` — matches the
//     holos-issued API key prefix. A match on a CR's marshalled form
//     is a regression: caller-facing API keys must never be stored on
//     the CR; they live in a sibling v1.Secret named by
//     Status.HashSecretRef.
//   - argon2Pattern: `\$argon2id\$` — matches the canonical argon2id
//     envelope prefix (PHC-string form). A match means an envelope
//     leaked onto a CR rather than staying in the sibling v1.Secret.
var ForbiddenBytePatterns = []ForbiddenBytePattern{
	{
		Name:    "api-key-prefix",
		Pattern: regexp.MustCompile(`sih_[A-Za-z0-9_-]{20,}`),
	},
	{
		Name:    "argon2id-envelope",
		Pattern: regexp.MustCompile(`\$argon2id\$`),
	},
}

// ForbiddenBytePattern names a single regular expression in the
// ForbiddenBytePatterns catalog. The Name field exists so a failing
// assertion in an invariant test can report which pattern fired
// without printing the marshalled bytes — doing so would risk leaking
// the very material the invariant is written to prevent. See the
// per-pattern notes on ForbiddenBytePatterns for why each entry is
// present.
//
// This struct is explicitly NOT a CR field — it is a compile-time
// configuration value consumed only by the invariant tests. The
// marker directly below tells controller-gen to skip deepcopy
// generation so its *regexp.Regexp field does not produce an
// un-compilable deepcopy (regexp.Regexp is not a Kubernetes type
// and does not expose a DeepCopyInto method).
//
// +kubebuilder:object:generate=false
type ForbiddenBytePattern struct {
	// Name is the short identifier used in test failure messages.
	// Stable across releases — changing it is a test-output change,
	// not an API change.
	Name string
	// Pattern is the regexp.Regexp that must NOT match any byte of
	// any marshalled CR in this API group. Each pattern is anchored
	// loosely so incidental whitespace or key-name prefixes in the
	// marshalled form do not hide a violation.
	Pattern *regexp.Regexp
}
