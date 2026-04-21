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

package controller

import (
	sicrypto "github.com/holos-run/holos-console/internal/secretinjector/crypto"
)

// PepperLoaderForTest is the narrow shape of sicrypto.Loader an envtest
// suite must satisfy to inject a deterministic pepper into a Manager's
// CredentialReconciler without running the real Bootstrap path.
// Production code MUST NOT depend on this type — it exists so the
// envtest cross-reconciler suite (HOL-753) in
// internal/secretinjector/controller/suite_test.go can keep its
// Credential hashing deterministic and hermetic while still exercising
// the real reconciler code paths.
//
// The interface shape deliberately duplicates sicrypto.Loader rather
// than aliasing it: the envtest suite lives in a _test.go file and
// cannot be imported from sicrypto, and inlining the method set here
// makes the seam visible in the controller package GoDoc rather than
// hiding it behind a re-export. Any test implementation is converted
// to sicrypto.Loader at the point it is assigned to the reconciler's
// Pepper field, so production call sites continue to see the single
// sicrypto.Loader contract.
type PepperLoaderForTest = sicrypto.Loader

// SetCredentialPepperForTest injects the supplied PepperLoaderForTest
// into the Credential reconciler attached to the given Manager. Exists
// exclusively so the envtest suite (HOL-753) can boot a Manager with
// SkipPepperBootstrap=true and still drive Credential hashing through
// a deterministic in-memory loader rather than the real
// sicrypto.SecretLoader.
//
// Called once per suite after NewManager and before Start. Safe because
// the controller-runtime Manager does not dispatch Reconcile events
// until Start begins servicing the informer queues; the assignment
// happens before any reconciler can observe the Pepper field.
//
// Production code MUST NOT call this function; doing so would replace
// the real pepper-backed loader with an arbitrary test stub. The name
// carries the ForTest suffix as the project-wide guardrail against
// accidental non-test use (see AGENTS.md).
func SetCredentialPepperForTest(m *Manager, loader PepperLoaderForTest) {
	if m == nil || m.credentialReconciler == nil {
		return
	}
	m.credentialReconciler.Pepper = loader
}
