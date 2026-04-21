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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PepperSecretName is the pinned name of the v1.Secret that carries the
// holos-secret-injector pepper material in the controller's own namespace.
// The name is pinned (not configurable) so operators can audit the single
// object that carries the bytes; a typo in an env var cannot accidentally
// route Bootstrap to write the pepper into a differently-named Secret.
//
// Each row in the Secret's .data map is keyed as "pepper-<N>" where <N> is
// an ASCII decimal integer version. The Credential reconciler (HOL-751)
// routes to the matching pepper bytes via [Loader.Get] by supplying the
// same integer version that was stamped onto the hash Envelope at Hash
// time. The active (newest) version is reported by [Loader.Active].
const PepperSecretName = "holos-secret-injector-pepper"

// PepperDataKeyPrefix is the prefix every versioned pepper data key uses.
// Keys that do not start with this prefix are ignored by [Loader.Active]
// and [Loader.Get] so a Secret that picks up an unrelated annotation-side
// key (for example, a future "salt-seed" key) does not corrupt version
// discovery. See [parsePepperVersion] for the parse contract.
const PepperDataKeyPrefix = "pepper-"

// Errors returned by the pepper loader. They are sentinel values so callers
// can match with [errors.Is] without string parsing.
var (
	// ErrPepperSecretNotFound is returned by [Loader.Active] and
	// [Loader.Get] when the backing v1.Secret does not exist in the
	// configured namespace. The loader refuses to fall back to a
	// zero-byte pepper so a missing Bootstrap call fails loudly rather
	// than silently producing an unpeppered hash.
	ErrPepperSecretNotFound = errors.New("crypto: pepper Secret not found")
	// ErrPepperVersionNotFound is returned by [Loader.Get] when the
	// caller asks for an integer version that is not present in the
	// backing Secret's data map. An operator who deleted a retired
	// pepper row before every hash referencing it was re-hashed will
	// see this error surface on verify — the error is deliberate: the
	// loader never silently substitutes a different version.
	ErrPepperVersionNotFound = errors.New("crypto: pepper version not found")
	// ErrNoPepperVersions is returned by [Loader.Active] when the
	// backing Secret exists but carries no "pepper-<N>" data keys. This
	// indicates an operator manually cleared the Secret's data or a
	// buggy writer truncated it; either way the loader refuses to
	// report an active version because there is no material to hash
	// against.
	ErrNoPepperVersions = errors.New("crypto: pepper Secret carries no versioned rows")
)

// Loader looks up pepper bytes by integer version. The interface is the
// read-only surface the Credential reconciler (HOL-751) depends on; the
// bootstrap writer lives separately in [Bootstrap] so reconcilers cannot
// accidentally mutate the Secret during a Reconcile call.
//
// Implementations MUST:
//
//   - Refuse to return a zero-length pepper. An empty row in .data is an
//     operator error and must surface as an error, not as success.
//   - Never log the pepper bytes. Log the integer version and the byte
//     length as the only shape visible to telemetry.
//   - Read from a narrow, pinned v1.Secret in the controller's own
//     namespace; never from a CR spec/status, a ConfigMap, or an env var.
//     ADR 031 pins the no-sensitive-on-CR invariant.
type Loader interface {
	// Active returns the highest-numbered pepper version present in the
	// backing Secret and its bytes. Callers use the returned version as
	// [Envelope.PepperVersion] when they Hash a new credential so a
	// later rotation can route Verify to the right row via [Loader.Get].
	//
	// Returns [ErrPepperSecretNotFound] if Bootstrap has not run,
	// [ErrNoPepperVersions] if the Secret exists but carries no
	// "pepper-<N>" keys.
	Active(ctx context.Context) (version int32, bytes []byte, err error)

	// Get returns the pepper bytes for the supplied integer version.
	// Callers use this on Verify to look up the pepper that was active
	// at Hash time, which may differ from the Active version during a
	// rotation window.
	//
	// Returns [ErrPepperSecretNotFound] if the backing Secret is gone,
	// [ErrPepperVersionNotFound] if the version row has been removed
	// (for example, by a future retire-old-pepper job).
	Get(ctx context.Context, version int32) (bytes []byte, err error)
}

// SecretLoader is the production [Loader] implementation backed by a
// controller-runtime client.Client. The zero value is not usable; callers
// construct one via [NewSecretLoader].
//
// The supplied client MUST be a non-cached direct client (built with
// client.New, not mgr.GetClient()). The controller-runtime ClusterRole
// shipped with this binary grants `get` on core/v1 Secret only — not
// `list` or `watch` — because enumeration of Secrets is the class of
// vulnerability this service is designed to close (see
// config/secret-injector/rbac/cluster/role.yaml and ADR 031). A
// cache-backed client would lazily start a Secret informer on the
// first Get and require `list`/`watch`, which real RBAC forbids. The
// Credential reconciler (HOL-751) constructs the direct client for
// this purpose; the fake client used by unit tests mimics the direct
// client's Get-by-name shape.
//
// The loader holds no pepper bytes in its own fields. Every call fetches
// the current state of the backing Secret and parses it, which means an
// external rotation of the Secret's .data takes effect on the next call
// without requiring a manager restart. This is defence-in-depth: the
// active rotation controller is Post-MVP, but the read surface is already
// rotation-ready.
type SecretLoader struct {
	client    client.Client
	namespace string
	name      string
	// cacheMu guards the parsed map. Reads take RLock so Active/Get
	// calls in parallel don't serialise, while a refresh takes Lock.
	cacheMu sync.RWMutex
}

// NewSecretLoader constructs a [SecretLoader] that reads from the pinned
// [PepperSecretName] Secret in the supplied namespace. The client MUST be
// a non-cached direct client — see the [SecretLoader] GoDoc for why a
// cache-backed client would violate the shipped ClusterRole (no
// list/watch on core/v1 Secret). Production callers in HOL-751
// construct the direct client via client.New(cfg, client.Options{})
// from the same rest.Config the manager uses. Tests supply a fake
// client whose Get shape matches the direct client.
//
// namespace MUST be non-empty. Callers typically obtain it from
// [ControllerNamespace].
func NewSecretLoader(c client.Client, namespace string) (*SecretLoader, error) {
	if c == nil {
		return nil, errors.New("crypto: NewSecretLoader requires a non-nil client")
	}
	if namespace == "" {
		return nil, errors.New("crypto: NewSecretLoader requires a non-empty namespace")
	}
	return &SecretLoader{
		client:    c,
		namespace: namespace,
		name:      PepperSecretName,
	}, nil
}

// Active implements [Loader.Active]. See the interface doc for the
// contract; this method fetches the pinned Secret, parses the "pepper-<N>"
// keys, and returns the highest version's bytes.
func (l *SecretLoader) Active(ctx context.Context) (int32, []byte, error) {
	l.cacheMu.RLock()
	defer l.cacheMu.RUnlock()

	versions, err := l.loadVersions(ctx)
	if err != nil {
		return 0, nil, err
	}
	if len(versions) == 0 {
		return 0, nil, ErrNoPepperVersions
	}
	active := activeVersion(versions)
	bytes := versions[active]
	if len(bytes) == 0 {
		// An empty row is an operator error — surface it rather than
		// returning zero bytes that would hash every credential to the
		// same value. Do NOT include the row's bytes (there are none,
		// but even the length shape stays off the wire for the
		// versioned row to match what the Credential reconciler logs).
		return 0, nil, fmt.Errorf("crypto: pepper row for version %d is empty", active)
	}
	return active, cloneBytes(bytes), nil
}

// Get implements [Loader.Get]. See the interface doc for the contract.
func (l *SecretLoader) Get(ctx context.Context, version int32) ([]byte, error) {
	l.cacheMu.RLock()
	defer l.cacheMu.RUnlock()

	versions, err := l.loadVersions(ctx)
	if err != nil {
		return nil, err
	}
	bytes, ok := versions[version]
	if !ok {
		return nil, fmt.Errorf("%w: version %d", ErrPepperVersionNotFound, version)
	}
	if len(bytes) == 0 {
		return nil, fmt.Errorf("crypto: pepper row for version %d is empty", version)
	}
	return cloneBytes(bytes), nil
}

// loadVersions fetches the backing Secret and returns its parsed
// pepper rows. Malformed keys (keys that do not parse as
// "pepper-<positive-int32>") are silently ignored — they might be future
// extensions (a "salt-seed" row, for example) or operator typos that we
// refuse to crash on. Rows with non-numeric or non-positive suffixes are
// also ignored so a stray "pepper-foo" row does not hijack the version
// discovery.
func (l *SecretLoader) loadVersions(ctx context.Context) (map[int32][]byte, error) {
	var secret corev1.Secret
	key := types.NamespacedName{Namespace: l.namespace, Name: l.name}
	if err := l.client.Get(ctx, key, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s/%s", ErrPepperSecretNotFound, l.namespace, l.name)
		}
		return nil, fmt.Errorf("crypto: get pepper Secret %s: %w", key, err)
	}
	return parsePepperData(secret.Data), nil
}

// parsePepperData filters the supplied Secret data map down to rows keyed
// as "pepper-<positive-int32>" and returns a map keyed by the parsed
// version. Rows that fail to parse are ignored rather than rejected:
// a malformed key MUST NOT crash the loader because an operator who
// manually labelled a comment-style key should not brick the hash path.
// See [parsePepperVersion] for the parse contract.
func parsePepperData(data map[string][]byte) map[int32][]byte {
	out := make(map[int32][]byte, len(data))
	for k, v := range data {
		version, ok := parsePepperVersion(k)
		if !ok {
			continue
		}
		out[version] = v
	}
	return out
}

// parsePepperVersion decodes a "pepper-<N>" data key into its integer
// version. Returns (0, false) if key does not start with
// [PepperDataKeyPrefix], if the suffix is not a base-10 integer in the
// int32 range, or if the suffix parses to a non-positive integer (zero
// and negatives are reserved so version 1 is the first seal).
func parsePepperVersion(key string) (int32, bool) {
	suffix, ok := strings.CutPrefix(key, PepperDataKeyPrefix)
	if !ok {
		return 0, false
	}
	if suffix == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(suffix, 10, 32)
	if err != nil {
		return 0, false
	}
	if n <= 0 {
		return 0, false
	}
	return int32(n), true
}

// activeVersion returns the maximum key in versions. versions MUST be
// non-empty; callers check len before invoking so this helper can remain
// branch-free on the empty case. The active row is the highest-numbered
// version because version numbers are monotonically assigned by Bootstrap
// and any future rotation controller (Post-MVP).
func activeVersion(versions map[int32][]byte) int32 {
	var active int32
	first := true
	for v := range versions {
		if first || v > active {
			active = v
			first = false
		}
	}
	return active
}

// cloneBytes returns a defensive copy of b so a caller cannot mutate the
// backing map's value. The loader holds no retained pointer to b because
// loadVersions re-fetches on every call, but copying at the return
// boundary insulates the loader from future caching work that retains
// maps across calls.
func cloneBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
