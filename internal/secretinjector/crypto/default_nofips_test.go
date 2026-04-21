//go:build !fips

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
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestDefaultBindsArgon2id pins the package-level [Default] contract: the
// non-FIPS build wires the argon2id implementation, not any future
// primitive. This test lives under the !fips tag (matching
// default_nofips.go) so the -fips build variant's own test of its own
// [Default] binding does not collide here.
func TestDefaultBindsArgon2id(t *testing.T) {
	k := Default()
	if k.ID() != KDFArgon2id {
		t.Fatalf("Default().ID() = %q, want %q", k.ID(), KDFArgon2id)
	}
	if diff := cmp.Diff(Argon2idDefault, k.DefaultParams()); diff != "" {
		t.Fatalf("Default().DefaultParams() mismatch (-want +got):\n%s", diff)
	}
}
