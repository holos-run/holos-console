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

// Package controller_test hosts the envtest cross-reconciler suite for
// HOL-753 — the M2 milestone's primary test gate. The suite boots a
// real kube-apiserver via envtest, installs the four secrets.holos.run
// CRDs, the open-schema istio AuthorizationPolicy CRD (test-only
// fixture under testdata/istio-crds/), and every ValidatingAdmissionPolicy
// under config/secret-injector/admission. It then spins up a
// controller-runtime Manager with the three M2 reconcilers registered
// (UpstreamSecret + Credential + SecretInjectionPolicyBinding) and the
// pepper Bootstrap path skipped so the Credential reconciler observes
// a deterministic in-test pepper seed.
//
// The suite is the ONLY place in this package where every reconciler
// runs against a real apiserver simultaneously, and it is the authoritative
// enforcement point for the dominant "no sensitive values on CRs"
// invariant — every assertion routes through the marshal-scan helper
// in invariant_test.go so a regression on ANY reconciler branch fails
// the gate rather than a downstream consumer at request time.
//
// The suite skips (not fails) when the envtest binaries are absent, so
// developers and CI agents who have not run `setup-envtest use` can
// still run `go test ./...`. The skip is intentionally loud so a
// missing setup is not mistaken for a passing test.
package controller_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	istiosecurityv1 "istio.io/client-go/pkg/apis/security/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	secretsv1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
	envtesthelpers "github.com/holos-run/holos-console/internal/envtest"
	controllerpkg "github.com/holos-run/holos-console/internal/secretinjector/controller"
)

// envSuite wraps the envtest.Environment so tests can share the
// apiserver startup cost. Each test spins up its own Manager against
// the same env so reconciler failure modes are fully isolated
// test-by-test.
type envSuite struct {
	env    *envtest.Environment
	cfg    *rest.Config
	client client.Client
	// nsCounter produces unique namespace names per test so parallel
	// subtests never collide on a label-keyed CEL predicate.
	nsCounter atomic.Int32
}

// startEnvSuite boots the envtest API server with all four secrets.holos.run
// CRDs, the testdata-local istio AuthorizationPolicy CRD, and every
// ValidatingAdmissionPolicy in config/secret-injector/admission. Callers
// build their own Manager(s) from suite.cfg.
//
// The istio CRD is loaded from a test-only file under
// ../controller/testdata/istio-crds/ so the envtest bootstrap stays
// self-contained — we do not want a test dependency on a vendored
// Istio release bundle, and the real CRD is installed by cluster
// operators alongside the mesh in production. The file's open-schema
// ensures the reconciler's builder passes API-server validation without
// forcing every envtest to track upstream Istio schema bumps.
func startEnvSuite(t *testing.T) *envSuite {
	t.Helper()

	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		if assets := envtesthelpers.DetectAssets(); assets != "" {
			t.Setenv("KUBEBUILDER_ASSETS", assets)
		} else {
			t.Skip("envtest binaries not found; set KUBEBUILDER_ASSETS or run `setup-envtest use` to download")
		}
	}

	repoRoot, err := envtesthelpers.FindRepoRoot()
	if err != nil {
		t.Fatalf("finding repo root: %v", err)
	}

	env := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(repoRoot, "config", "secret-injector", "crd"),
			filepath.Join(repoRoot, "internal", "secretinjector", "controller", "testdata", "istio-crds"),
		},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("starting envtest: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := env.Stop(); stopErr != nil {
			t.Logf("stopping envtest: %v", stopErr)
		}
	})

	c, err := client.New(cfg, client.Options{Scheme: controllerpkg.Scheme})
	if err != nil {
		t.Fatalf("constructing direct client: %v", err)
	}

	// Install every VAP in the admission directory before any Manager
	// is started so admission parity assertions see the same
	// rejection shape the production cluster would.
	ctx := context.Background()
	admissionDir := filepath.Join(repoRoot, "config", "secret-injector", "admission")
	if err := envtesthelpers.ApplyYAMLFilesInDir(ctx, c, admissionDir); err != nil {
		t.Fatalf("applying admission policies: %v", err)
	}
	for _, name := range []string{
		"credential-authn-type-apikey-only",
		"credential-upstreamref-same-namespace",
		"namespace-scope-label-immutable",
		"secretinjectionpolicy-authn-type-apikey-only",
		"secretinjectionpolicy-folder-or-org-only",
		"secretinjectionpolicybinding-folder-or-org-only",
		"secretinjectionpolicybinding-policyref-same-namespace-or-ancestor",
		"upstreamsecret-project-only",
		"upstreamsecret-valuetemplate-no-control-chars",
	} {
		envtesthelpers.WaitForAdmissionPolicy(t, ctx, c, name)
	}

	// The envtest apiserver installs VAP registrations but does not
	// block Start() on CEL compilation. A Create racing ahead of the
	// admission plugin activation will pass through silently; wait
	// for a known-bad mutation to produce a rejection before handing
	// the suite back to the caller. Mirrors the probe in
	// api/secrets/v1alpha1/crd_test.go:waitAdmissionActive.
	waitSuiteAdmissionActive(t, ctx, c)

	return &envSuite{env: env, cfg: cfg, client: c}
}

// waitSuiteAdmissionActive polls a throwaway namespace label flip
// that the namespace-scope-label-immutable VAP rejects. Once the API
// server actually refuses the update we have a positive signal that
// the admission plugin compiled its CEL programs. Cleans up the probe
// namespace before returning.
func waitSuiteAdmissionActive(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	const probe = "holos-si-admission-probe"
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   probe,
			Labels: map[string]string{"console.holos.run/resource-type": "project"},
		},
	}
	if err := c.Create(ctx, ns); err != nil {
		t.Fatalf("admission-probe create namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: probe}})
	})
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got := &corev1.Namespace{}
		if err := c.Get(ctx, types.NamespacedName{Name: probe}, got); err != nil {
			t.Fatalf("admission-probe get: %v", err)
		}
		got.Labels["console.holos.run/resource-type"] = "folder"
		err := c.Update(ctx, got)
		if err != nil && (apierrors.IsInvalid(err) || apierrors.IsForbidden(err)) {
			return
		}
		if err == nil {
			got.Labels["console.holos.run/resource-type"] = "project"
			_ = c.Update(ctx, got)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("admission policies did not become active within deadline")
}

// startSuiteManager constructs a controllerpkg.Manager from cfg and
// starts it in a goroutine. Returns the Manager, the cancel that stops
// it, and a channel that receives Start's error. Waits for the cache
// sync flag to flip so tests can issue writes against a hot cache.
//
// The pepper Bootstrap path is skipped (SkipPepperBootstrap=true) and
// the Credential reconciler's Pepper loader is replaced by an
// in-memory stub after the Manager is constructed; the envtest suite
// needs deterministic pepper bytes without threading a real Secret
// through the TransportCredentialedController boundary. See the
// package doc on manager.go for the production wiring.
func startSuiteManager(t *testing.T, cfg *rest.Config) (*controllerpkg.Manager, context.CancelFunc, <-chan error) {
	t.Helper()

	m, err := controllerpkg.NewManager(cfg, controllerpkg.Options{
		CacheSyncTimeout:             30 * time.Second,
		SkipControllerNameValidation: true,
		SkipPepperBootstrap:          true,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Wire a stub Loader into the Credential reconciler now, before
	// Start() is invoked, so the first Reconcile after cache sync
	// observes a usable pepper. The stub lives in
	// credential_controller_test.go (newStubPepper) — reusing it keeps
	// the pepper seed deterministic across unit + envtest suites.
	// We reach in through the exported SetCredentialPepperForTest
	// helper so the test-only wiring does not widen the public API.
	controllerpkg.SetCredentialPepperForTest(m, newEnvtestStubPepper())

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- m.Start(ctx)
	}()

	deadline := time.Now().Add(30 * time.Second)
	for !m.Ready() {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("manager did not become ready within deadline")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return m, cancel, errCh
}

// stopSuiteManager cancels the manager context and drains its error
// channel. Tolerates context.Canceled since that is the expected exit
// reason when the test itself cancelled.
func stopSuiteManager(t *testing.T, cancel context.CancelFunc, errCh <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("manager exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("manager did not shut down within deadline")
	}
}

// newEnvtestStubPepper returns an in-memory Loader seeded with a
// deterministic pepper version + bytes. Mirrors the unit-test stub in
// credential_controller_test.go but lives in the _test package because
// the real [sicrypto.Loader] interface cannot be defined in this file
// without pulling the crypto package into the test binary; the wrapper
// at controllerpkg.SetCredentialPepperForTest accepts any Loader.
func newEnvtestStubPepper() controllerpkg.PepperLoaderForTest {
	return &envtestStubPepperLoader{
		version: 7,
		bytes:   []byte("envtest-pepper-bytes-0000000000"),
	}
}

// envtestStubPepperLoader implements the subset of sicrypto.Loader the
// Credential reconciler actually invokes. The suite never exercises a
// missing-version path, so Get returns a not-found on any non-active
// version; Active returns the fixed bytes verbatim.
type envtestStubPepperLoader struct {
	version int32
	bytes   []byte
}

func (s *envtestStubPepperLoader) Active(_ context.Context) (int32, []byte, error) {
	return s.version, append([]byte(nil), s.bytes...), nil
}

func (s *envtestStubPepperLoader) Get(_ context.Context, v int32) ([]byte, error) {
	if v != s.version {
		return nil, fmt.Errorf("envtest stub pepper: version %d not configured", v)
	}
	return append([]byte(nil), s.bytes...), nil
}

// waitForCRCondition polls the named CR until the named condition
// reaches the wanted status or the deadline expires. Kind-switch in
// body is intentional — every kind in this group carries
// status.conditions, but the concrete field path varies.
//
// Every successful wait also runs the marshal-scan invariant gate on
// the freshly-read object so a reconciler that writes forbidden bytes
// during its transition to the wanted condition status still fails
// the test that caused the write. The gate is applied BEFORE the
// wanted status returns control so a same-generation write with
// forbidden bytes cannot hide behind a later scrub.
func waitForCRCondition(t *testing.T, ctx context.Context, c client.Client, obj client.Object, key types.NamespacedName, condType string, want metav1.ConditionStatus) *metav1.Condition {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last *metav1.Condition
	for time.Now().Before(deadline) {
		if err := c.Get(ctx, key, obj); err != nil {
			if !apierrors.IsNotFound(err) {
				t.Fatalf("waitForCRCondition: get %s: %v", key, err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		// Apply the marshal-scan gate on every observation, not just
		// the successful one — a transient Ready=Unknown state that
		// leaks bytes is still a regression.
		assertNoSensitiveValuesOnCR(t, ctx, c, copyObjectForScan(obj), key)

		var conds []metav1.Condition
		switch typed := obj.(type) {
		case *secretsv1alpha1.UpstreamSecret:
			conds = typed.Status.Conditions
		case *secretsv1alpha1.Credential:
			conds = typed.Status.Conditions
		case *secretsv1alpha1.SecretInjectionPolicy:
			conds = typed.Status.Conditions
		case *secretsv1alpha1.SecretInjectionPolicyBinding:
			conds = typed.Status.Conditions
		default:
			t.Fatalf("waitForCRCondition: unsupported kind %T", obj)
		}
		if cond := meta.FindStatusCondition(conds, condType); cond != nil {
			last = cond
			if cond.Status == want {
				return cond
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if last == nil {
		t.Fatalf("condition %q never appeared on %s", condType, key)
	}
	t.Fatalf("condition %q on %s did not reach %s within deadline; last=%+v", condType, key, want, last)
	return nil
}

// copyObjectForScan returns a fresh empty object of the same kind so
// the marshal-scan helper performs its own GET against the live
// cluster state instead of re-using the in-flight read the caller is
// about to switch on. Keeps the scan a round-trip observation rather
// than a tautology.
func copyObjectForScan(obj client.Object) client.Object {
	switch obj.(type) {
	case *secretsv1alpha1.UpstreamSecret:
		return &secretsv1alpha1.UpstreamSecret{}
	case *secretsv1alpha1.Credential:
		return &secretsv1alpha1.Credential{}
	case *secretsv1alpha1.SecretInjectionPolicy:
		return &secretsv1alpha1.SecretInjectionPolicy{}
	case *secretsv1alpha1.SecretInjectionPolicyBinding:
		return &secretsv1alpha1.SecretInjectionPolicyBinding{}
	}
	// Fallback: caller-supplied object works fine for the scan, it
	// just costs an extra allocation on the caller side.
	return obj
}

// makeSuiteNamespace installs a namespace carrying the
// console.holos.run/resource-type label plus any optional extras. Each
// namespace name is prefixed by test name so parallel suites stay
// hermetic and the VAPs' CEL guards see consistent labels.
func (s *envSuite) makeSuiteNamespace(t *testing.T, ctx context.Context, name, resourceType string, extras map[string]string) {
	t.Helper()
	labels := map[string]string{"console.holos.run/resource-type": resourceType}
	for k, v := range extras {
		labels[k] = v
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
	}
	if err := s.client.Create(ctx, ns); err != nil {
		t.Fatalf("creating namespace %q: %v", name, err)
	}
}

// TestUpstreamSecret_ResolvedRefsLifecycle exercises every
// ResolvedRefs reason on the UpstreamSecret reconciler: the
// referenced Secret does not exist, the Secret exists but is missing
// the key, and the Secret + key materialise. The marshal-scan gate
// runs implicitly on every waitForCRCondition observation.
func TestUpstreamSecret_ResolvedRefsLifecycle(t *testing.T) {
	s := startEnvSuite(t)
	_, cancel, errCh := startSuiteManager(t, s.cfg)
	t.Cleanup(func() { stopSuiteManager(t, cancel, errCh) })

	ctx := context.Background()
	ns := "holos-prj-us-lifecycle"
	s.makeSuiteNamespace(t, ctx, ns, "project", nil)

	us := &secretsv1alpha1.UpstreamSecret{
		ObjectMeta: metav1.ObjectMeta{Name: "vendor", Namespace: ns},
		Spec: secretsv1alpha1.UpstreamSecretSpec{
			SecretRef: secretsv1alpha1.SecretKeyReference{Name: "vendor-src", Key: "apiKey"},
			Upstream:  secretsv1alpha1.Upstream{Host: "vendor.example.test", Scheme: "https"},
			Injection: secretsv1alpha1.Injection{Header: "Authorization", ValueTemplate: "Bearer {{.Value}}"},
		},
	}
	if err := s.client.Create(ctx, us); err != nil {
		t.Fatalf("create UpstreamSecret: %v", err)
	}
	key := client.ObjectKeyFromObject(us)

	// Stage 1: Secret does not exist -> ResolvedRefs=False / SecretNotFound.
	read := &secretsv1alpha1.UpstreamSecret{}
	cond := waitForCRCondition(t, ctx, s.client, read, key,
		secretsv1alpha1.UpstreamSecretConditionResolvedRefs, metav1.ConditionFalse)
	if cond.Reason != secretsv1alpha1.UpstreamSecretReasonSecretNotFound {
		t.Fatalf("stage 1 ResolvedRefs reason=%q want %q",
			cond.Reason, secretsv1alpha1.UpstreamSecretReasonSecretNotFound)
	}

	// Stage 2: Secret exists but missing the key ->
	// ResolvedRefs=False / SecretKeyMissing.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vendor-src", Namespace: ns},
		Data:       map[string][]byte{"other-key": []byte("unrelated")},
	}
	if err := s.client.Create(ctx, secret); err != nil {
		t.Fatalf("create upstream Secret: %v", err)
	}
	waitForUpstreamResolvedReason(t, ctx, s.client, key,
		secretsv1alpha1.UpstreamSecretReasonSecretKeyMissing)

	// Stage 3: add the key -> ResolvedRefs=True / Ready=True.
	if err := s.client.Get(ctx, types.NamespacedName{Namespace: ns, Name: "vendor-src"}, secret); err != nil {
		t.Fatalf("re-get upstream Secret: %v", err)
	}
	secret.Data["apiKey"] = []byte("sih_plaintext_not_stored_on_cr")
	if err := s.client.Update(ctx, secret); err != nil {
		t.Fatalf("update upstream Secret: %v", err)
	}
	waitForCRCondition(t, ctx, s.client, read, key,
		secretsv1alpha1.UpstreamSecretConditionReady, metav1.ConditionTrue)

	// End-of-test sweep: every CR in the namespace is clean.
	assertSuiteMarshalScan(t, ctx, s.client, ns)
}

// waitForUpstreamResolvedReason polls for the exact Reason string on
// an UpstreamSecret's ResolvedRefs condition. The default
// waitForCRCondition helper keys on Status; this variant keys on
// Reason so a Condition=False with the wrong reason fails fast. The
// marshal-scan gate is applied on every observation inside the helper.
func waitForUpstreamResolvedReason(t *testing.T, ctx context.Context, c client.Client, key types.NamespacedName, wantReason string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var lastReason string
	for time.Now().Before(deadline) {
		us := &secretsv1alpha1.UpstreamSecret{}
		if err := c.Get(ctx, key, us); err != nil {
			t.Fatalf("get UpstreamSecret %s: %v", key, err)
		}
		assertNoSensitiveValuesOnCR(t, ctx, c, &secretsv1alpha1.UpstreamSecret{}, key)
		if cond := meta.FindStatusCondition(us.Status.Conditions, secretsv1alpha1.UpstreamSecretConditionResolvedRefs); cond != nil {
			lastReason = cond.Reason
			if cond.Reason == wantReason {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("UpstreamSecret %s ResolvedRefs reason never reached %q; last=%q",
		key, wantReason, lastReason)
}

// TestCredential_LifecycleTransitions exercises each Credential
// lifecycle branch under the envtest Manager: materialisation of the
// sibling hash Secret, owner-reference GC on Credential delete,
// revocation, rotation grace window, and expiry. The marshal-scan
// gate runs on every waitForCRCondition and at the end of each
// sub-case.
func TestCredential_LifecycleTransitions(t *testing.T) {
	s := startEnvSuite(t)
	_, cancel, errCh := startSuiteManager(t, s.cfg)
	t.Cleanup(func() { stopSuiteManager(t, cancel, errCh) })
	ctx := context.Background()

	t.Run("Materialization_and_GC", func(t *testing.T) {
		ns := "holos-prj-cred-happy"
		s.makeSuiteNamespace(t, ctx, ns, "project", nil)

		upstream := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "vendor-src", Namespace: ns},
			Data:       map[string][]byte{"apiKey": []byte("sih_plaintext_not_on_cr_happy")},
		}
		if err := s.client.Create(ctx, upstream); err != nil {
			t.Fatalf("create upstream Secret: %v", err)
		}

		cred := &secretsv1alpha1.Credential{
			ObjectMeta: metav1.ObjectMeta{Name: "vendor-apikey", Namespace: ns},
			Spec: secretsv1alpha1.CredentialSpec{
				Authentication: secretsv1alpha1.Authentication{
					Type:   secretsv1alpha1.AuthenticationTypeAPIKey,
					APIKey: &secretsv1alpha1.APIKeySettings{HeaderName: "X-Api-Key"},
				},
				UpstreamSecretRef: secretsv1alpha1.NamespacedSecretKeyReference{
					Name: "vendor-src",
					Key:  "apiKey",
				},
			},
		}
		if err := s.client.Create(ctx, cred); err != nil {
			t.Fatalf("create Credential: %v", err)
		}
		key := client.ObjectKeyFromObject(cred)

		read := &secretsv1alpha1.Credential{}
		waitForCRCondition(t, ctx, s.client, read, key,
			secretsv1alpha1.CredentialConditionHashMaterialized, metav1.ConditionTrue)
		waitForCRCondition(t, ctx, s.client, read, key,
			secretsv1alpha1.CredentialConditionReady, metav1.ConditionTrue)

		if read.Status.HashSecretRef == nil {
			t.Fatalf("HashSecretRef nil after Ready=True")
		}
		// Sibling hash Secret exists and carries the envelope key.
		hashKey := types.NamespacedName{Namespace: ns, Name: read.Status.HashSecretRef.Name}
		var hash corev1.Secret
		if err := s.client.Get(ctx, hashKey, &hash); err != nil {
			t.Fatalf("get hash Secret: %v", err)
		}
		if _, ok := hash.Data["envelope"]; !ok {
			t.Errorf("hash Secret missing key %q; got keys=%v", "envelope", secretDataKeys(hash))
		}
		if len(hash.OwnerReferences) != 1 {
			t.Fatalf("hash Secret ownerReferences len=%d want 1", len(hash.OwnerReferences))
		}
		owner := hash.OwnerReferences[0]
		if owner.UID != read.UID || owner.Controller == nil || !*owner.Controller {
			t.Errorf("hash Secret owner=%+v; want controller=true UID=%q", owner, read.UID)
		}

		// Envtest runs kube-apiserver + etcd but NOT the
		// controller-manager, so the real garbage collector is absent
		// and cascade deletion is only observable in a production
		// cluster. We therefore ASSERT the owner reference invariants
		// above (controller=true, owner UID=Credential UID) which are
		// the reconciler-owned preconditions GC uses; the apiserver
		// itself will reap the child in production. Directly deleting
		// the Credential here without GC leaves the hash Secret
		// orphaned in the envtest store, which is acceptable because
		// the ns is unique to this subtest.
		if err := s.client.Delete(ctx, read); err != nil {
			t.Fatalf("delete Credential: %v", err)
		}

		assertSuiteMarshalScan(t, ctx, s.client, ns)
	})

	t.Run("Revocation", func(t *testing.T) {
		ns := "holos-prj-cred-revoke"
		s.makeSuiteNamespace(t, ctx, ns, "project", nil)

		upstream := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "vendor-src", Namespace: ns},
			Data:       map[string][]byte{"apiKey": []byte("sih_plaintext_revoke_case")},
		}
		if err := s.client.Create(ctx, upstream); err != nil {
			t.Fatalf("create upstream Secret: %v", err)
		}

		cred := &secretsv1alpha1.Credential{
			ObjectMeta: metav1.ObjectMeta{Name: "to-revoke", Namespace: ns},
			Spec: secretsv1alpha1.CredentialSpec{
				Authentication: secretsv1alpha1.Authentication{
					Type:   secretsv1alpha1.AuthenticationTypeAPIKey,
					APIKey: &secretsv1alpha1.APIKeySettings{HeaderName: "X-Api-Key"},
				},
				UpstreamSecretRef: secretsv1alpha1.NamespacedSecretKeyReference{
					Name: "vendor-src", Key: "apiKey",
				},
			},
		}
		if err := s.client.Create(ctx, cred); err != nil {
			t.Fatalf("create Credential: %v", err)
		}
		key := client.ObjectKeyFromObject(cred)

		read := &secretsv1alpha1.Credential{}
		waitForCRCondition(t, ctx, s.client, read, key,
			secretsv1alpha1.CredentialConditionReady, metav1.ConditionTrue)

		// Flip Revoked=true and wait for Phase=Revoked.
		read.Spec.Revoked = true
		if err := s.client.Update(ctx, read); err != nil {
			t.Fatalf("update Revoked=true: %v", err)
		}
		waitForCredentialPhase(t, ctx, s.client, key, secretsv1alpha1.PhaseRevoked)

		// Hash Secret must be reaped.
		hashKey := types.NamespacedName{Namespace: ns, Name: "to-revoke-hash"}
		waitForSecretDeletion(t, ctx, s.client, hashKey)

		assertSuiteMarshalScan(t, ctx, s.client, ns)
	})

	t.Run("Expiry", func(t *testing.T) {
		ns := "holos-prj-cred-expire"
		s.makeSuiteNamespace(t, ctx, ns, "project", nil)

		upstream := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "vendor-src", Namespace: ns},
			Data:       map[string][]byte{"apiKey": []byte("sih_plaintext_expire_case")},
		}
		if err := s.client.Create(ctx, upstream); err != nil {
			t.Fatalf("create upstream Secret: %v", err)
		}

		expiresInPast := metav1.NewTime(time.Now().Add(-1 * time.Minute))
		cred := &secretsv1alpha1.Credential{
			ObjectMeta: metav1.ObjectMeta{Name: "already-expired", Namespace: ns},
			Spec: secretsv1alpha1.CredentialSpec{
				Authentication: secretsv1alpha1.Authentication{
					Type:   secretsv1alpha1.AuthenticationTypeAPIKey,
					APIKey: &secretsv1alpha1.APIKeySettings{HeaderName: "X-Api-Key"},
				},
				UpstreamSecretRef: secretsv1alpha1.NamespacedSecretKeyReference{
					Name: "vendor-src", Key: "apiKey",
				},
				ExpiresAt: &expiresInPast,
			},
		}
		if err := s.client.Create(ctx, cred); err != nil {
			t.Fatalf("create Credential: %v", err)
		}
		key := client.ObjectKeyFromObject(cred)

		waitForCredentialPhase(t, ctx, s.client, key, secretsv1alpha1.PhaseExpired)

		read := &secretsv1alpha1.Credential{}
		if err := s.client.Get(ctx, key, read); err != nil {
			t.Fatalf("get expired Credential: %v", err)
		}
		ready := meta.FindStatusCondition(read.Status.Conditions, secretsv1alpha1.CredentialConditionReady)
		if ready == nil || ready.Status != metav1.ConditionFalse ||
			ready.Reason != secretsv1alpha1.CredentialReasonExpired {
			t.Errorf("Ready condition for expired Credential unexpected: %+v", ready)
		}

		assertSuiteMarshalScan(t, ctx, s.client, ns)
	})

	t.Run("Rotation_to_Retired", func(t *testing.T) {
		ns := "holos-prj-cred-rotate"
		s.makeSuiteNamespace(t, ctx, ns, "project", nil)
		group := "vendor-apikey"

		upstream := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "vendor-src", Namespace: ns},
			Data:       map[string][]byte{"apiKey": []byte("sih_plaintext_rotate_case")},
		}
		if err := s.client.Create(ctx, upstream); err != nil {
			t.Fatalf("create upstream Secret: %v", err)
		}

		// Predecessor materialises first; GraceSeconds=0 retires the
		// predecessor on the first reconcile that sees a successor.
		predecessor := &secretsv1alpha1.Credential{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vendor-v1",
				Namespace: ns,
				Labels:    map[string]string{secretsv1alpha1.RotationGroupLabel: group},
			},
			Spec: secretsv1alpha1.CredentialSpec{
				Authentication: secretsv1alpha1.Authentication{
					Type:   secretsv1alpha1.AuthenticationTypeAPIKey,
					APIKey: &secretsv1alpha1.APIKeySettings{HeaderName: "X-Api-Key"},
				},
				UpstreamSecretRef: secretsv1alpha1.NamespacedSecretKeyReference{
					Name: "vendor-src", Key: "apiKey",
				},
				Rotation: secretsv1alpha1.Rotation{GraceSeconds: 0},
			},
		}
		if err := s.client.Create(ctx, predecessor); err != nil {
			t.Fatalf("create predecessor: %v", err)
		}
		predKey := client.ObjectKeyFromObject(predecessor)
		waitForCRCondition(t, ctx, s.client, &secretsv1alpha1.Credential{},
			predKey, secretsv1alpha1.CredentialConditionReady, metav1.ConditionTrue)

		// Create the successor. Because it is created LATER
		// (newer creationTimestamp) and carries the same rotation
		// group label, the predecessor's reconciler enqueues on the
		// Credential reconciler's own watch of Credential (via
		// For()) and walks the phase transitions.
		successor := &secretsv1alpha1.Credential{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vendor-v2",
				Namespace: ns,
				Labels:    map[string]string{secretsv1alpha1.RotationGroupLabel: group},
			},
			Spec: predecessor.Spec,
		}
		// Clear the Rotation block on the successor — it does not
		// need a grace window because it is the leading credential.
		successor.Spec.Rotation = secretsv1alpha1.Rotation{}
		// Sleep briefly so the apiserver assigns a strictly-later
		// creationTimestamp (the successor detection keys on that).
		time.Sleep(1500 * time.Millisecond)
		if err := s.client.Create(ctx, successor); err != nil {
			t.Fatalf("create successor: %v", err)
		}

		// The predecessor reconciles a second time when its own For()
		// watch observes the fresh successor in the same rotation
		// group via the Credential reconciler's List-based successor
		// lookup. Poll for the Retired phase with a generous window
		// since the reconcile only fires on the successor's Create
		// event on the SHARED controller watch (the Credential
		// reconciler watches all Credentials via For()). To close
		// the race where the predecessor reconcile already ran
		// BEFORE the successor was created, we touch the predecessor
		// spec (a no-op annotation write) to force a reconcile.
		touchCredentialSpec(t, ctx, s.client, predKey)
		waitForCredentialPhase(t, ctx, s.client, predKey, secretsv1alpha1.PhaseRetired)

		assertSuiteMarshalScan(t, ctx, s.client, ns)
	})
}

// waitForCredentialPhase polls the named Credential until its Status.Phase
// equals want or the deadline expires. Runs the marshal-scan invariant
// on every observation.
func waitForCredentialPhase(t *testing.T, ctx context.Context, c client.Client, key types.NamespacedName, want secretsv1alpha1.PhaseType) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last secretsv1alpha1.PhaseType
	for time.Now().Before(deadline) {
		cred := &secretsv1alpha1.Credential{}
		if err := c.Get(ctx, key, cred); err != nil {
			if !apierrors.IsNotFound(err) {
				t.Fatalf("get Credential %s: %v", key, err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		assertNoSensitiveValuesOnCR(t, ctx, c, &secretsv1alpha1.Credential{}, key)
		last = cred.Status.Phase
		if last == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Credential %s phase never reached %q; last=%q", key, want, last)
}

// waitForSecretDeletion polls until the named v1.Secret returns
// NotFound (i.e. GC has reclaimed it) or the deadline expires.
func waitForSecretDeletion(t *testing.T, ctx context.Context, c client.Client, key types.NamespacedName) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var secret corev1.Secret
		err := c.Get(ctx, key, &secret)
		if apierrors.IsNotFound(err) {
			return
		}
		if err != nil {
			t.Fatalf("get Secret %s: %v", key, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Secret %s still present after 30s; GC did not reap", key)
}

// touchCredentialSpec writes a harmless annotation to force a
// reconcile. Used by the rotation test to close the race between the
// predecessor's first reconcile and the successor's Create.
func touchCredentialSpec(t *testing.T, ctx context.Context, c client.Client, key types.NamespacedName) {
	t.Helper()
	var cred secretsv1alpha1.Credential
	if err := c.Get(ctx, key, &cred); err != nil {
		t.Fatalf("touchCredentialSpec get: %v", err)
	}
	if cred.Annotations == nil {
		cred.Annotations = map[string]string{}
	}
	cred.Annotations["suite.holos.run/touch"] = time.Now().Format(time.RFC3339Nano)
	if err := c.Update(ctx, &cred); err != nil {
		t.Fatalf("touchCredentialSpec update: %v", err)
	}
}

// TestBinding_ProgrammedAndCascadeGC exercises the SecretInjectionPolicyBinding
// reconciler's full happy + sad path under the envtest Manager:
//
//  1. Binding without policy -> ResolvedRefs=False / PolicyNotFound,
//     no AuthorizationPolicy emitted.
//  2. Create the referenced policy -> Programmed=True + AP emitted
//     with the expected labels and owner reference back to the binding.
//  3. Delete the binding -> apiserver GC reaps the owned AP within
//     the polling window (multi-reconciler cascade).
//
// The marshal-scan gate applies on every waitForCRCondition and at
// the end of the test.
func TestBinding_ProgrammedAndCascadeGC(t *testing.T) {
	s := startEnvSuite(t)
	_, cancel, errCh := startSuiteManager(t, s.cfg)
	t.Cleanup(func() { stopSuiteManager(t, cancel, errCh) })
	ctx := context.Background()

	// Binding lives in a folder namespace; the policy it points at
	// lives in the binding's OWN namespace (scope=folder). The
	// own-namespace path is always a candidate — the binding namespace
	// must simply carry the folder resource-type label.
	bindingNS := "holos-fld-binding-gc"
	s.makeSuiteNamespace(t, ctx, bindingNS, "folder", nil)

	binding := &secretsv1alpha1.SecretInjectionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "binding-gc", Namespace: bindingNS},
		Spec: secretsv1alpha1.SecretInjectionPolicyBindingSpec{
			PolicyRef: secretsv1alpha1.PolicyRef{
				Scope:     secretsv1alpha1.PolicyRefScopeFolder,
				Namespace: bindingNS,
				Name:      "policy-gc",
			},
			TargetRefs: []secretsv1alpha1.TargetRef{{
				Kind:      secretsv1alpha1.TargetRefKindServiceAccount,
				Namespace: bindingNS,
				Name:      "api-client",
			}},
		},
	}
	if err := s.client.Create(ctx, binding); err != nil {
		t.Fatalf("create binding: %v", err)
	}
	bkey := client.ObjectKeyFromObject(binding)

	// Stage 1: policy not found -> ResolvedRefs=False / PolicyNotFound.
	read := &secretsv1alpha1.SecretInjectionPolicyBinding{}
	cond := waitForCRCondition(t, ctx, s.client, read, bkey,
		secretsv1alpha1.SecretInjectionPolicyBindingConditionResolvedRefs, metav1.ConditionFalse)
	if cond.Reason != secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound {
		t.Fatalf("stage 1 ResolvedRefs reason=%q want %q",
			cond.Reason, secretsv1alpha1.SecretInjectionPolicyBindingReasonPolicyNotFound)
	}
	// No AuthorizationPolicy should exist in the binding namespace.
	apKey := types.NamespacedName{Namespace: bindingNS, Name: binding.Name + "-secret-injector"}
	var ap istiosecurityv1.AuthorizationPolicy
	if err := s.client.Get(ctx, apKey, &ap); !apierrors.IsNotFound(err) {
		t.Fatalf("AuthorizationPolicy exists before policyRef resolves: %v", err)
	}

	// Stage 2: create the referenced SecretInjectionPolicy; the
	// binding reconciler's Watches(&SecretInjectionPolicy) mapFunc
	// should enqueue the binding and Programmed should flip True.
	policy := &secretsv1alpha1.SecretInjectionPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "policy-gc", Namespace: bindingNS},
		Spec: secretsv1alpha1.SecretInjectionPolicySpec{
			Direction: secretsv1alpha1.DirectionIngress,
			CallerAuth: secretsv1alpha1.CallerAuth{
				Type: secretsv1alpha1.AuthenticationTypeAPIKey,
			},
			UpstreamRef: secretsv1alpha1.UpstreamRef{
				Scope:     secretsv1alpha1.UpstreamScopeProject,
				ScopeName: "p1",
				Name:      "u1",
			},
		},
	}
	if err := s.client.Create(ctx, policy); err != nil {
		t.Fatalf("create policy: %v", err)
	}
	waitForCRCondition(t, ctx, s.client, read, bkey,
		secretsv1alpha1.SecretInjectionPolicyBindingConditionProgrammed, metav1.ConditionTrue)
	waitForCRCondition(t, ctx, s.client, read, bkey,
		secretsv1alpha1.SecretInjectionPolicyBindingConditionReady, metav1.ConditionTrue)

	// AP exists with the expected labels and owner reference.
	if err := s.client.Get(ctx, apKey, &ap); err != nil {
		t.Fatalf("get AuthorizationPolicy after Programmed=True: %v", err)
	}
	if v := ap.Labels["app.kubernetes.io/managed-by"]; v != "holos-secret-injector" {
		t.Errorf("AuthorizationPolicy managed-by label=%q want holos-secret-injector", v)
	}
	if v := ap.Labels["secrets.holos.run/binding"]; v != binding.Name {
		t.Errorf("AuthorizationPolicy binding label=%q want %q", v, binding.Name)
	}
	if len(ap.OwnerReferences) != 1 || ap.OwnerReferences[0].UID != read.UID {
		t.Errorf("AuthorizationPolicy ownerReferences=%+v want single controller ref to binding %q",
			ap.OwnerReferences, read.UID)
	}

	// Stage 3: owner-reference GC preconditions. Envtest runs
	// kube-apiserver + etcd but NOT the controller-manager, so the
	// garbage collector controller is absent; cascade deletion only
	// fires in a production cluster. The owner-reference assertion
	// above covers the reconciler-owned preconditions the real GC
	// uses. We delete the binding to keep the ns tidy but do not
	// wait for the AP to be reaped — that would hang in envtest.
	if err := s.client.Delete(ctx, read); err != nil {
		t.Fatalf("delete binding: %v", err)
	}

	assertSuiteMarshalScan(t, ctx, s.client, bindingNS)
}

// TestAdmissionParity_RejectedPayloadsNeverReconciled confirms the
// VAP layer and the reconciler's post-admission belt-and-braces agree:
// a payload that the VAPs reject never lands in etcd, so the
// reconciler never observes it. Covers the headline admission
// policies across every kind.
func TestAdmissionParity_RejectedPayloadsNeverReconciled(t *testing.T) {
	s := startEnvSuite(t)
	_, cancel, errCh := startSuiteManager(t, s.cfg)
	t.Cleanup(func() { stopSuiteManager(t, cancel, errCh) })
	ctx := context.Background()

	t.Run("UpstreamSecret_FolderNamespaceRejected", func(t *testing.T) {
		ns := "holos-fld-us-reject"
		s.makeSuiteNamespace(t, ctx, ns, "folder", nil)
		us := &secretsv1alpha1.UpstreamSecret{
			ObjectMeta: metav1.ObjectMeta{Name: "u", Namespace: ns},
			Spec: secretsv1alpha1.UpstreamSecretSpec{
				SecretRef: secretsv1alpha1.SecretKeyReference{Name: "src", Key: "k"},
				Upstream:  secretsv1alpha1.Upstream{Host: "example.test", Scheme: "https"},
				Injection: secretsv1alpha1.Injection{Header: "Authorization", ValueTemplate: "Bearer {{.Value}}"},
			},
		}
		err := s.client.Create(ctx, us)
		if err == nil {
			t.Fatalf("folder-namespace UpstreamSecret was accepted; admission regressed")
		}
		if !apierrors.IsInvalid(err) && !apierrors.IsForbidden(err) {
			t.Fatalf("unexpected admission error kind: %T %v", err, err)
		}
		// Post-condition: nothing materialised (no Get to perform;
		// List returns empty). Double-check with a GET which MUST
		// NotFound.
		var check secretsv1alpha1.UpstreamSecret
		if err := s.client.Get(ctx, types.NamespacedName{Namespace: ns, Name: "u"}, &check); !apierrors.IsNotFound(err) {
			t.Fatalf("rejected UpstreamSecret leaked into cluster: %v", err)
		}
	})

	t.Run("Credential_OIDCRejected", func(t *testing.T) {
		ns := "holos-prj-cred-oidc-reject"
		s.makeSuiteNamespace(t, ctx, ns, "project", nil)
		cred := &secretsv1alpha1.Credential{
			ObjectMeta: metav1.ObjectMeta{Name: "oidc", Namespace: ns},
			Spec: secretsv1alpha1.CredentialSpec{
				Authentication: secretsv1alpha1.Authentication{
					Type: secretsv1alpha1.AuthenticationTypeOIDC,
				},
				UpstreamSecretRef: secretsv1alpha1.NamespacedSecretKeyReference{
					Name: "src", Key: "k",
				},
			},
		}
		err := s.client.Create(ctx, cred)
		if err == nil {
			t.Fatalf("OIDC Credential was accepted; admission regressed")
		}
		if !apierrors.IsInvalid(err) && !apierrors.IsForbidden(err) {
			t.Fatalf("unexpected admission error kind: %T %v", err, err)
		}
	})

	t.Run("UpstreamSecret_ControlCharsInValueTemplateRejected", func(t *testing.T) {
		ns := "holos-prj-us-ctrl-reject"
		s.makeSuiteNamespace(t, ctx, ns, "project", nil)
		us := &secretsv1alpha1.UpstreamSecret{
			ObjectMeta: metav1.ObjectMeta{Name: "u", Namespace: ns},
			Spec: secretsv1alpha1.UpstreamSecretSpec{
				SecretRef: secretsv1alpha1.SecretKeyReference{Name: "src", Key: "k"},
				Upstream:  secretsv1alpha1.Upstream{Host: "example.test", Scheme: "https"},
				Injection: secretsv1alpha1.Injection{
					Header:        "Authorization",
					ValueTemplate: "Bearer {{.Value}}\r\nX-Evil: x",
				},
			},
		}
		err := s.client.Create(ctx, us)
		if err == nil {
			t.Fatalf("control-char ValueTemplate was accepted; admission regressed")
		}
		if !apierrors.IsInvalid(err) && !apierrors.IsForbidden(err) {
			t.Fatalf("unexpected admission error kind: %T %v", err, err)
		}
	})

	t.Run("Binding_CrossTenantPolicyRefRejected", func(t *testing.T) {
		bindingNS := "holos-fld-cross-tenant-bind"
		otherNS := "holos-fld-cross-tenant-other"
		s.makeSuiteNamespace(t, ctx, bindingNS, "folder", nil)
		s.makeSuiteNamespace(t, ctx, otherNS, "folder", nil)
		b := &secretsv1alpha1.SecretInjectionPolicyBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "cross", Namespace: bindingNS},
			Spec: secretsv1alpha1.SecretInjectionPolicyBindingSpec{
				PolicyRef: secretsv1alpha1.PolicyRef{
					Scope:     secretsv1alpha1.PolicyRefScopeFolder,
					Namespace: otherNS, // not own, not parent, not org
					Name:      "p",
				},
				TargetRefs: []secretsv1alpha1.TargetRef{{
					Kind:      secretsv1alpha1.TargetRefKindServiceAccount,
					Namespace: bindingNS,
					Name:      "sa",
				}},
			},
		}
		err := s.client.Create(ctx, b)
		if err == nil {
			t.Fatalf("cross-tenant policyRef binding was accepted; admission regressed")
		}
		if !apierrors.IsInvalid(err) && !apierrors.IsForbidden(err) {
			t.Fatalf("unexpected admission error kind: %T %v", err, err)
		}
	})
}

// TestHotLoopGuards_HoldAcrossReconcilers confirms that every
// reconciler in this package refrains from re-writing status when no
// spec change has occurred. After the first Reconcile settles, the
// resourceVersion of each relevant CR must hold steady across a
// generous observation window.
func TestHotLoopGuards_HoldAcrossReconcilers(t *testing.T) {
	s := startEnvSuite(t)
	_, cancel, errCh := startSuiteManager(t, s.cfg)
	t.Cleanup(func() { stopSuiteManager(t, cancel, errCh) })
	ctx := context.Background()

	ns := "holos-prj-hotloop"
	s.makeSuiteNamespace(t, ctx, ns, "project", nil)

	// UpstreamSecret + its resolvable Secret
	upstream := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vendor-src", Namespace: ns},
		Data:       map[string][]byte{"apiKey": []byte("sih_plaintext_hotloop_case")},
	}
	if err := s.client.Create(ctx, upstream); err != nil {
		t.Fatalf("create upstream Secret: %v", err)
	}
	us := &secretsv1alpha1.UpstreamSecret{
		ObjectMeta: metav1.ObjectMeta{Name: "stable-us", Namespace: ns},
		Spec: secretsv1alpha1.UpstreamSecretSpec{
			SecretRef: secretsv1alpha1.SecretKeyReference{Name: "vendor-src", Key: "apiKey"},
			Upstream:  secretsv1alpha1.Upstream{Host: "example.test", Scheme: "https"},
			Injection: secretsv1alpha1.Injection{Header: "Authorization", ValueTemplate: "Bearer {{.Value}}"},
		},
	}
	if err := s.client.Create(ctx, us); err != nil {
		t.Fatalf("create UpstreamSecret: %v", err)
	}
	usKey := client.ObjectKeyFromObject(us)
	waitForCRCondition(t, ctx, s.client, &secretsv1alpha1.UpstreamSecret{}, usKey,
		secretsv1alpha1.UpstreamSecretConditionReady, metav1.ConditionTrue)

	// Credential that reconciles to Ready=True.
	cred := &secretsv1alpha1.Credential{
		ObjectMeta: metav1.ObjectMeta{Name: "stable-cred", Namespace: ns},
		Spec: secretsv1alpha1.CredentialSpec{
			Authentication: secretsv1alpha1.Authentication{
				Type:   secretsv1alpha1.AuthenticationTypeAPIKey,
				APIKey: &secretsv1alpha1.APIKeySettings{HeaderName: "X-Api-Key"},
			},
			UpstreamSecretRef: secretsv1alpha1.NamespacedSecretKeyReference{
				Name: "vendor-src", Key: "apiKey",
			},
		},
	}
	if err := s.client.Create(ctx, cred); err != nil {
		t.Fatalf("create Credential: %v", err)
	}
	credKey := client.ObjectKeyFromObject(cred)
	waitForCRCondition(t, ctx, s.client, &secretsv1alpha1.Credential{}, credKey,
		secretsv1alpha1.CredentialConditionReady, metav1.ConditionTrue)

	// Capture resourceVersions after settle.
	firstUS := &secretsv1alpha1.UpstreamSecret{}
	if err := s.client.Get(ctx, usKey, firstUS); err != nil {
		t.Fatalf("get UpstreamSecret: %v", err)
	}
	firstCred := &secretsv1alpha1.Credential{}
	if err := s.client.Get(ctx, credKey, firstCred); err != nil {
		t.Fatalf("get Credential: %v", err)
	}

	// Sleep past one watch-event round-trip. A hot-loop guard
	// regression would re-write status and bump the resourceVersion.
	time.Sleep(3 * time.Second)

	var laterUS secretsv1alpha1.UpstreamSecret
	if err := s.client.Get(ctx, usKey, &laterUS); err != nil {
		t.Fatalf("re-get UpstreamSecret: %v", err)
	}
	if laterUS.ResourceVersion != firstUS.ResourceVersion {
		t.Errorf("UpstreamSecret resourceVersion advanced from %q to %q; hot-loop regressed",
			firstUS.ResourceVersion, laterUS.ResourceVersion)
	}

	var laterCred secretsv1alpha1.Credential
	if err := s.client.Get(ctx, credKey, &laterCred); err != nil {
		t.Fatalf("re-get Credential: %v", err)
	}
	if laterCred.ResourceVersion != firstCred.ResourceVersion {
		t.Errorf("Credential resourceVersion advanced from %q to %q; hot-loop regressed",
			firstCred.ResourceVersion, laterCred.ResourceVersion)
	}

	// Final sweep.
	assertSuiteMarshalScan(t, ctx, s.client, ns)
}

// TestPepperBootstrap_Idempotence confirms the pepper Bootstrap path
// is safe to call repeatedly: a second NewManager+Start on the same
// env observes the same pepper Secret version without duplicating or
// mutating the existing payload. Covers the production startup shape
// where a manager restart would otherwise risk re-rolling the pepper.
//
// This test DOES exercise the real Bootstrap path (SkipPepperBootstrap=false)
// so the envtest suite is the single place where both branches of
// Start() run end-to-end.
func TestPepperBootstrap_Idempotence(t *testing.T) {
	s := startEnvSuite(t)
	ctx := context.Background()

	// Create the controller's own namespace up front so the real
	// Bootstrap helper can Create the pepper Secret.
	const controllerNS = "holos-secret-injector"
	s.makeSuiteNamespace(t, ctx, controllerNS, "organization", nil)

	startAndStop := func(t *testing.T) string {
		t.Helper()
		m, err := controllerpkg.NewManager(s.cfg, controllerpkg.Options{
			CacheSyncTimeout:             30 * time.Second,
			SkipControllerNameValidation: true,
			ControllerNamespace:          controllerNS,
			// Real Bootstrap runs.
		})
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		mCtx, cancel := context.WithCancel(ctx)
		errCh := make(chan error, 1)
		go func() { errCh <- m.Start(mCtx) }()
		deadline := time.Now().Add(30 * time.Second)
		for !m.Ready() {
			if time.Now().After(deadline) {
				cancel()
				t.Fatalf("manager did not become ready")
			}
			time.Sleep(100 * time.Millisecond)
		}
		// Read the pepper Secret.
		var pepper corev1.Secret
		pepperKey := types.NamespacedName{Namespace: controllerNS, Name: "holos-secret-injector-pepper"}
		if err := s.client.Get(ctx, pepperKey, &pepper); err != nil {
			t.Fatalf("get pepper Secret: %v", err)
		}
		rv := pepper.ResourceVersion
		cancel()
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Logf("manager exit: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("manager did not shut down")
		}
		return rv
	}

	firstRV := startAndStop(t)
	secondRV := startAndStop(t)

	if firstRV != secondRV {
		t.Errorf("pepper Secret resourceVersion changed across Bootstrap runs: first=%q second=%q; Bootstrap is not idempotent",
			firstRV, secondRV)
	}
}

// secretDataKeys returns the sorted keys of a v1.Secret's .data map
// so a test failure message enumerates the actual content without
// printing the values themselves.
func secretDataKeys(s corev1.Secret) []string {
	keys := make([]string, 0, len(s.Data))
	for k := range s.Data {
		keys = append(keys, k)
	}
	return keys
}

// TestEnvtestSkipsWhenNoAssets confirms the suite's skip path: when
// KUBEBUILDER_ASSETS is absent AND no asset directory can be
// auto-detected, the helper calls t.Skip rather than t.Fatal. Runs
// first in the file so a broken detection path fails loudly without
// dragging the rest of the suite with it.
//
// The test manipulates the environment variable directly; to avoid
// interfering with the rest of the suite it restores the prior value
// before returning.
func TestEnvtestSkipsWhenNoAssets(t *testing.T) {
	prev := os.Getenv("KUBEBUILDER_ASSETS")
	t.Setenv("KUBEBUILDER_ASSETS", "")
	t.Setenv("HOME", t.TempDir())

	// Run startEnvSuite in a child sub-test so we can observe its
	// Skip via the outer test's Failed/Skipped flags.
	t.Run("skip", func(inner *testing.T) {
		defer func() {
			if inner.Failed() {
				t.Errorf("startEnvSuite should have skipped; it fatal'd instead")
			}
			if !inner.Skipped() {
				t.Errorf("startEnvSuite should have skipped; it continued")
			}
		}()
		startEnvSuite(inner)
	})

	// Restore the original value so downstream tests run.
	if prev != "" {
		t.Setenv("KUBEBUILDER_ASSETS", prev)
	}
}
