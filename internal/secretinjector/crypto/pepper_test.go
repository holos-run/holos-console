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
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// testNamespace is the namespace every pepper test uses. The pepper
// Secret is deliberately cluster-namespace-scoped in production but for
// unit tests a single fixed namespace keeps assertions concise.
const testNamespace = "holos-system"

// pepperScheme is a local runtime.Scheme carrying only the corev1 types
// the loader and bootstrap helpers need. Keeping the scheme local to the
// test file avoids an import cycle on the controller package's Scheme.
var pepperScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	return s
}()

// newFakeClient returns a fake controller-runtime client.Client seeded
// with the supplied objects. The status subresource is not split out
// because the pepper Secret does not use a status subresource.
func newFakeClient(objs ...client.Object) client.Client {
	return ctrlfake.NewClientBuilder().
		WithScheme(pepperScheme).
		WithObjects(objs...).
		Build()
}

// TestBootstrapFirstSealWritesVersion1 verifies the cold-start path:
// Bootstrap against an empty namespace seals a fresh Secret with a single
// "pepper-1" row of PepperSeedLength bytes, and the returned
// BootstrapResult reports Created=true, ActiveVersion=1.
func TestBootstrapFirstSealWritesVersion1(t *testing.T) {
	ctx := context.Background()
	c := newFakeClient()

	result, err := Bootstrap(ctx, c, testNamespace)
	if err != nil {
		t.Fatalf("Bootstrap: unexpected error: %v", err)
	}
	if !result.Created {
		t.Errorf("result.Created = false, want true on first seal")
	}
	if result.ActiveVersion != 1 {
		t.Errorf("result.ActiveVersion = %d, want 1", result.ActiveVersion)
	}
	if result.BytesLength != PepperSeedLength {
		t.Errorf("result.BytesLength = %d, want %d", result.BytesLength, PepperSeedLength)
	}

	// Assert the Secret was actually written with the expected shape.
	var s corev1.Secret
	if err := c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: PepperSecretName}, &s); err != nil {
		t.Fatalf("Get sealed pepper Secret: %v", err)
	}
	if s.Type != corev1.SecretTypeOpaque {
		t.Errorf("Secret.Type = %q, want %q", s.Type, corev1.SecretTypeOpaque)
	}
	if got := len(s.Data); got != 1 {
		t.Errorf("len(Secret.Data) = %d, want 1 row", got)
	}
	row, ok := s.Data["pepper-1"]
	if !ok {
		t.Fatalf("Secret.Data missing key %q; got keys %v", "pepper-1", keysOf(s.Data))
	}
	if len(row) != PepperSeedLength {
		t.Errorf("len(pepper-1) = %d, want %d", len(row), PepperSeedLength)
	}
}

// TestBootstrapIsIdempotent verifies the warm-restart path: a second
// Bootstrap call against a Secret that already exists does not rewrite
// .data, and reports Created=false with the same ActiveVersion as the
// first call.
func TestBootstrapIsIdempotent(t *testing.T) {
	ctx := context.Background()
	c := newFakeClient()

	first, err := Bootstrap(ctx, c, testNamespace)
	if err != nil {
		t.Fatalf("first Bootstrap: %v", err)
	}

	// Snapshot the sealed bytes so we can assert the second call leaves
	// them untouched.
	var afterFirst corev1.Secret
	if err := c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: PepperSecretName}, &afterFirst); err != nil {
		t.Fatalf("Get after first: %v", err)
	}
	wantBytes := append([]byte(nil), afterFirst.Data["pepper-1"]...)

	second, err := Bootstrap(ctx, c, testNamespace)
	if err != nil {
		t.Fatalf("second Bootstrap: %v", err)
	}
	if second.Created {
		t.Errorf("second.Created = true, want false on warm restart")
	}
	if second.ActiveVersion != first.ActiveVersion {
		t.Errorf("second.ActiveVersion = %d, want %d (first)", second.ActiveVersion, first.ActiveVersion)
	}

	var afterSecond corev1.Secret
	if err := c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: PepperSecretName}, &afterSecond); err != nil {
		t.Fatalf("Get after second: %v", err)
	}
	if !bytes.Equal(afterSecond.Data["pepper-1"], wantBytes) {
		t.Errorf("pepper-1 mutated across idempotent Bootstrap calls")
	}
}

// TestBootstrapDetectsMaxVersion seeds the Secret with multiple versions
// and asserts Bootstrap parses the max correctly. Exercises the
// operator-migrated flow where a future rotation controller may have
// written pepper-2, pepper-3, and retired pepper-1.
func TestBootstrapDetectsMaxVersion(t *testing.T) {
	ctx := context.Background()
	c := newFakeClient(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      PepperSecretName,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"pepper-1": bytes.Repeat([]byte{0x01}, PepperSeedLength),
			"pepper-3": bytes.Repeat([]byte{0x03}, PepperSeedLength),
			"pepper-2": bytes.Repeat([]byte{0x02}, PepperSeedLength),
		},
	})

	result, err := Bootstrap(ctx, c, testNamespace)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if result.Created {
		t.Errorf("result.Created = true, want false (Secret already exists)")
	}
	if result.ActiveVersion != 3 {
		t.Errorf("result.ActiveVersion = %d, want 3", result.ActiveVersion)
	}
	if result.BytesLength != PepperSeedLength {
		t.Errorf("result.BytesLength = %d, want %d", result.BytesLength, PepperSeedLength)
	}
}

// TestBootstrapIgnoresMalformedKeys confirms keys that are not
// "pepper-<positive-int32>" are filtered out rather than crashing the
// parser. The Secret has one valid row ("pepper-2") alongside several
// malformed ones; Bootstrap must report ActiveVersion=2.
func TestBootstrapIgnoresMalformedKeys(t *testing.T) {
	ctx := context.Background()
	c := newFakeClient(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      PepperSecretName,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"foo":        []byte("ignore"),
			"pepper-":    []byte("ignore-empty-suffix"),
			"pepper-abc": []byte("ignore-nonnumeric"),
			"pepper-0":   []byte("ignore-zero-version"),
			"pepper--1":  []byte("ignore-negative"),
			"pepper-2":   bytes.Repeat([]byte{0x02}, PepperSeedLength),
		},
	})

	result, err := Bootstrap(ctx, c, testNamespace)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if result.ActiveVersion != 2 {
		t.Errorf("result.ActiveVersion = %d, want 2 (only valid row)", result.ActiveVersion)
	}
}

// TestBootstrapEmptyNamespaceRejected confirms Bootstrap refuses to run
// with an empty namespace so a missing POD_NAMESPACE env var does not
// silently seal the pepper into the wrong place.
func TestBootstrapEmptyNamespaceRejected(t *testing.T) {
	ctx := context.Background()
	c := newFakeClient()

	_, err := Bootstrap(ctx, c, "")
	if err == nil {
		t.Fatalf("Bootstrap(empty namespace) = nil error, want rejection")
	}
}

// TestBootstrapNilClientRejected confirms Bootstrap refuses to run
// without a client. A nil client would panic on the first Get; the
// helper returns a structured error instead.
func TestBootstrapNilClientRejected(t *testing.T) {
	_, err := Bootstrap(context.Background(), nil, testNamespace)
	if err == nil {
		t.Fatalf("Bootstrap(nil client) = nil error, want rejection")
	}
}

// TestBootstrapExistingSecretWithNoValidRows returns
// ErrNoPepperVersions so an operator who truncates .data sees a loud
// error on manager restart rather than Bootstrap silently re-sealing
// version 1 on top of the broken material.
func TestBootstrapExistingSecretWithNoValidRows(t *testing.T) {
	ctx := context.Background()
	c := newFakeClient(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      PepperSecretName,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"not-a-pepper-row": []byte("junk"),
		},
	})

	_, err := Bootstrap(ctx, c, testNamespace)
	if !errors.Is(err, ErrNoPepperVersions) {
		t.Fatalf("Bootstrap error = %v, want ErrNoPepperVersions", err)
	}
}

// TestSecretLoaderActiveReadsHighestVersion seeds a pre-existing
// multi-version Secret and asserts Loader.Active returns the max and
// that the returned bytes match the row.
func TestSecretLoaderActiveReadsHighestVersion(t *testing.T) {
	ctx := context.Background()
	threeBytes := bytes.Repeat([]byte{0x03}, PepperSeedLength)
	c := newFakeClient(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      PepperSecretName,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"pepper-1": bytes.Repeat([]byte{0x01}, PepperSeedLength),
			"pepper-3": threeBytes,
			"pepper-2": bytes.Repeat([]byte{0x02}, PepperSeedLength),
		},
	})

	loader, err := NewSecretLoader(c, testNamespace)
	if err != nil {
		t.Fatalf("NewSecretLoader: %v", err)
	}
	version, got, err := loader.Active(ctx)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if version != 3 {
		t.Errorf("active version = %d, want 3", version)
	}
	if !bytes.Equal(got, threeBytes) {
		t.Errorf("active bytes mismatch")
	}
}

// TestSecretLoaderGetByVersion returns the specific row.
func TestSecretLoaderGetByVersion(t *testing.T) {
	ctx := context.Background()
	twoBytes := bytes.Repeat([]byte{0x02}, PepperSeedLength)
	c := newFakeClient(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      PepperSecretName,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"pepper-1": bytes.Repeat([]byte{0x01}, PepperSeedLength),
			"pepper-2": twoBytes,
		},
	})

	loader, err := NewSecretLoader(c, testNamespace)
	if err != nil {
		t.Fatalf("NewSecretLoader: %v", err)
	}
	got, err := loader.Get(ctx, 2)
	if err != nil {
		t.Fatalf("Get(2): %v", err)
	}
	if !bytes.Equal(got, twoBytes) {
		t.Errorf("Get(2) bytes mismatch")
	}
}

// TestSecretLoaderGetUnknownVersion surfaces ErrPepperVersionNotFound.
func TestSecretLoaderGetUnknownVersion(t *testing.T) {
	ctx := context.Background()
	c := newFakeClient(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      PepperSecretName,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"pepper-1": bytes.Repeat([]byte{0x01}, PepperSeedLength),
		},
	})

	loader, err := NewSecretLoader(c, testNamespace)
	if err != nil {
		t.Fatalf("NewSecretLoader: %v", err)
	}
	_, err = loader.Get(ctx, 5)
	if !errors.Is(err, ErrPepperVersionNotFound) {
		t.Fatalf("Get(5) error = %v, want ErrPepperVersionNotFound", err)
	}
}

// TestSecretLoaderActiveMissingSecret surfaces ErrPepperSecretNotFound.
func TestSecretLoaderActiveMissingSecret(t *testing.T) {
	ctx := context.Background()
	c := newFakeClient()

	loader, err := NewSecretLoader(c, testNamespace)
	if err != nil {
		t.Fatalf("NewSecretLoader: %v", err)
	}
	_, _, err = loader.Active(ctx)
	if !errors.Is(err, ErrPepperSecretNotFound) {
		t.Fatalf("Active error = %v, want ErrPepperSecretNotFound", err)
	}
}

// TestSecretLoaderActiveNoValidRows seeds a Secret whose .data carries
// only malformed keys; Active reports ErrNoPepperVersions.
func TestSecretLoaderActiveNoValidRows(t *testing.T) {
	ctx := context.Background()
	c := newFakeClient(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      PepperSecretName,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"foo": []byte("junk"),
		},
	})

	loader, err := NewSecretLoader(c, testNamespace)
	if err != nil {
		t.Fatalf("NewSecretLoader: %v", err)
	}
	_, _, err = loader.Active(ctx)
	if !errors.Is(err, ErrNoPepperVersions) {
		t.Fatalf("Active error = %v, want ErrNoPepperVersions", err)
	}
}

// TestSecretLoaderReturnsDefensiveCopy verifies callers cannot mutate
// the loader's notion of the active bytes by writing to the returned
// slice. This is belt-and-braces: the loader re-fetches on every call
// today, but the contract remains safe if that changes.
func TestSecretLoaderReturnsDefensiveCopy(t *testing.T) {
	ctx := context.Background()
	original := bytes.Repeat([]byte{0xab}, PepperSeedLength)
	c := newFakeClient(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      PepperSecretName,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"pepper-1": append([]byte(nil), original...),
		},
	})

	loader, err := NewSecretLoader(c, testNamespace)
	if err != nil {
		t.Fatalf("NewSecretLoader: %v", err)
	}
	_, got, err := loader.Active(ctx)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	// Mutate the returned slice; a subsequent Active must still see the
	// original bytes.
	for i := range got {
		got[i] = 0
	}
	_, got2, err := loader.Active(ctx)
	if err != nil {
		t.Fatalf("Active (second): %v", err)
	}
	if !bytes.Equal(got2, original) {
		t.Errorf("caller mutation of returned slice leaked into loader state")
	}
}

// TestControllerNamespaceReadsEnv verifies the POD_NAMESPACE env var
// contract. Uses t.Setenv so the env state resets after the test.
func TestControllerNamespaceReadsEnv(t *testing.T) {
	t.Setenv(PodNamespaceEnv, "my-ns")
	if got := ControllerNamespace(); got != "my-ns" {
		t.Errorf("ControllerNamespace() = %q, want %q", got, "my-ns")
	}
}

// TestControllerNamespaceUnsetReturnsEmpty confirms the empty-env-var
// fallback returns "" rather than a silent default. Callers check for
// empty and fail loudly; a "default" default would be a silent
// misconfiguration landmine.
func TestControllerNamespaceUnsetReturnsEmpty(t *testing.T) {
	t.Setenv(PodNamespaceEnv, "")
	if got := ControllerNamespace(); got != "" {
		t.Errorf("ControllerNamespace() = %q, want empty", got)
	}
}

// TestParsePepperVersion exercises the parse contract directly so the
// matrix of malformed-key cases is legible in one place. The parser is
// shared by every call site above.
func TestParsePepperVersion(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		want   int32
		wantOK bool
	}{
		{"valid one", "pepper-1", 1, true},
		{"valid three-digit", "pepper-100", 100, true},
		{"missing prefix", "foo-1", 0, false},
		{"empty suffix", "pepper-", 0, false},
		{"non-numeric suffix", "pepper-abc", 0, false},
		{"zero suffix rejected", "pepper-0", 0, false},
		{"negative suffix rejected", "pepper--1", 0, false},
		{"overflow int32 rejected", "pepper-9999999999999", 0, false},
		{"bare prefix", "pepper", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parsePepperVersion(tc.key)
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("parsePepperVersion(%q) = (%d, %v), want (%d, %v)", tc.key, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// keysOf is a test helper for stable key list rendering in error
// messages. Returns keys in insertion order (map iteration randomness
// is OK because the helper is only used when a test is already
// failing).
func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
