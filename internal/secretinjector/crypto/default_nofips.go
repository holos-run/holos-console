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

// Default returns the non-FIPS default [KDF] used by every reconciler in a
// default build. The return type is the interface so a caller cannot
// accidentally rely on a concrete type that changes when the -fips build
// variant swaps the binding. This file is excluded from -fips builds by
// the !fips build tag above; the -fips override lives under the fips tag
// in pbkdf2.go so an -fips build that forgets to land the override fails
// loudly at link time on a missing Default symbol rather than silently
// reverting to argon2id.
func Default() KDF {
	return Argon2id{}
}
