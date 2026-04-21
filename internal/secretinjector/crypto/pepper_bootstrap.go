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
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PepperSeedLength is the number of random bytes Bootstrap generates for a
// freshly-seeded pepper row. 32 bytes (256 bits) is the standard argon2id
// pepper recommendation (RFC 9106 §3.1 "secret input") — long enough to
// defeat brute-force against the pepper itself even if an attacker obtains
// every hash in the cluster.
const PepperSeedLength = 32

// PodNamespaceEnv is the downward-API environment variable name the
// manager reads at startup to discover its own namespace. The value is set
// on the Deployment via:
//
//	env:
//	- name: POD_NAMESPACE
//	  valueFrom:
//	    fieldRef:
//	      fieldPath: metadata.namespace
//
// See config/secret-injector/deployment/deployment.yaml for the concrete
// wiring. Callers in envtest typically skip the env var and pass the
// namespace explicitly into [Bootstrap] — see the [ControllerNamespace]
// GoDoc for the fallback story.
const PodNamespaceEnv = "POD_NAMESPACE"

// ControllerNamespace returns the namespace the controller should treat
// as its own for pepper-bootstrap purposes. Resolution order:
//
//  1. POD_NAMESPACE environment variable (set by the downward API on the
//     controller's Deployment).
//  2. Empty string. Callers MUST NOT default to "default" or any other
//     concrete namespace: a silent fallback would let a misconfigured
//     Deployment write the pepper into the wrong namespace, and RBAC
//     would probably allow it because create-in-namespace RoleBindings
//     are namespace-local by definition. An empty return value is the
//     signal for the caller to fail loudly.
//
// Tests that need a deterministic namespace use [os.Setenv] on
// [PodNamespaceEnv] or pass the namespace directly into [Bootstrap].
// envtest suites typically call os.Setenv("POD_NAMESPACE", "default")
// in a TestMain hook so the pod-identity flow is exercised without a
// downward-API fixture.
func ControllerNamespace() string {
	return os.Getenv(PodNamespaceEnv)
}

// BootstrapResult carries the shape [Bootstrap] reports to its caller.
// Every field is telemetry-safe: nothing about the pepper bytes or their
// random seed is exposed. The Manager's log line and any later Prometheus
// gauge consume this struct directly.
type BootstrapResult struct {
	// ActiveVersion is the integer version [Loader.Active] will
	// subsequently report. For a first-boot seal this is always 1; for
	// an already-seeded Secret it is the maximum version that was
	// parsed out of .data.
	ActiveVersion int32
	// Created reports whether Bootstrap seeded a new Secret (true) or
	// observed an existing one (false). The flag drives the manager's
	// one-line startup log so operators can tell a first boot from a
	// warm restart at a glance.
	Created bool
	// BytesLength is the byte length of the active pepper row. Included
	// for observability: an operator who sees BytesLength=32 on first
	// boot knows the downward-API bootstrap produced a full-size seed,
	// without the log line ever carrying the bytes themselves.
	BytesLength int
}

// Bootstrap self-seals the controller-namespace pepper Secret on first
// manager boot and returns the active pepper version. The function is
// idempotent: a second invocation against a Secret that already exists
// parses the max version out of .data and returns it without touching the
// API server other than a single Get.
//
// On a missing Secret the function generates [PepperSeedLength] random
// bytes from [crypto/rand], writes data["pepper-1"] = <bytes>, sets
// Type=Opaque, and Creates the Secret. Conflicts on Create (the race where
// two replicas boot in parallel) are handled by a second Get and parse so
// the loser of the race does not overwrite the winner's pepper.
//
// Bootstrap is called exactly once per manager process, from
// controller.Manager.Start before the first reconcile runs. A failure is
// fatal: the reconciler cannot Hash without a pepper, and falling back
// would produce an unpeppered hash.
//
// Bootstrap never logs the pepper bytes, never returns them to the
// caller, and never stamps them onto the returned [BootstrapResult]. The
// only shape exposed is the integer version and the byte length.
func Bootstrap(ctx context.Context, c client.Client, namespace string) (BootstrapResult, error) {
	if c == nil {
		return BootstrapResult{}, errors.New("crypto: Bootstrap requires a non-nil client")
	}
	if namespace == "" {
		return BootstrapResult{}, errors.New("crypto: Bootstrap requires a non-empty namespace (set POD_NAMESPACE on the Deployment)")
	}

	key := types.NamespacedName{Namespace: namespace, Name: PepperSecretName}

	// Fast path: read the existing Secret via the supplied client. Most
	// manager starts hit this branch because the Secret persists across
	// pod restarts.
	existing, err := getPepperSecret(ctx, c, key)
	switch {
	case err == nil:
		return existingResult(existing)
	case !apierrors.IsNotFound(err):
		return BootstrapResult{}, fmt.Errorf("crypto: Bootstrap: get pepper Secret: %w", err)
	}

	// Secret is absent — seal a fresh version 1. Use crypto/rand so the
	// seed is unpredictable across replicas and restarts. If
	// io.ReadFull returns short, we abort rather than write a partial
	// pepper; a partial seal is worse than no seal because the manager
	// would silently carry on with a weak pepper on the next restart.
	seed := make([]byte, PepperSeedLength)
	if _, err := rand.Read(seed); err != nil {
		return BootstrapResult{}, fmt.Errorf("crypto: Bootstrap: generating pepper seed: %w", err)
	}

	fresh := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      PepperSecretName,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			pepperDataKey(1): seed,
		},
	}
	if err := c.Create(ctx, fresh); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// A parallel manager replica won the race. Re-read
			// and report its version as active. This is the only
			// branch where a Bootstrap call does two round trips
			// against the API server; single-replica deployments
			// never hit it.
			winner, readErr := getPepperSecret(ctx, c, key)
			if readErr != nil {
				return BootstrapResult{}, fmt.Errorf("crypto: Bootstrap: re-reading after AlreadyExists: %w", readErr)
			}
			return existingResult(winner)
		}
		return BootstrapResult{}, fmt.Errorf("crypto: Bootstrap: creating pepper Secret: %w", err)
	}

	return BootstrapResult{
		ActiveVersion: 1,
		Created:       true,
		BytesLength:   len(seed),
	}, nil
}

// getPepperSecret wraps a single client.Get against the pinned pepper
// Secret. Factored so both branches of Bootstrap share the same lookup
// shape.
func getPepperSecret(ctx context.Context, c client.Client, key types.NamespacedName) (*corev1.Secret, error) {
	var s corev1.Secret
	if err := c.Get(ctx, key, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// existingResult derives the BootstrapResult for a Secret that already
// exists. Returns an error if the Secret is present but carries no
// valid "pepper-<N>" rows — this is an unrecoverable state because the
// reconciler cannot hash, and Bootstrap refuses to silently re-seed
// version 1 on top of operator-edited material.
func existingResult(s *corev1.Secret) (BootstrapResult, error) {
	versions := parsePepperData(s.Data)
	if len(versions) == 0 {
		return BootstrapResult{}, fmt.Errorf("%w: %s/%s (no pepper-<N> rows parsed from .data)",
			ErrNoPepperVersions, s.Namespace, s.Name)
	}
	active := activeVersion(versions)
	return BootstrapResult{
		ActiveVersion: active,
		Created:       false,
		BytesLength:   len(versions[active]),
	}, nil
}

// pepperDataKey returns the "pepper-<N>" data-map key for version v.
// Centralised so the prefix and formatting live in one place; this is
// the only writer that must stay in lock-step with [parsePepperVersion].
func pepperDataKey(v int32) string {
	return PepperDataKeyPrefix + strconv.FormatInt(int64(v), 10)
}
