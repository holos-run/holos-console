// Package crdmgrtesting exposes a shared envtest bootstrap for the three
// HOL-661 / HOL-662 storage suites in console/templates,
// console/templatepolicies, and console/templatepolicybindings. Before
// HOL-663 each of those packages carried a ~80-line inline copy of the
// same envtest.Environment + controller-runtime Manager wiring; this
// package extracts that boilerplate so a storage test suite can spin up
// a cache-backed Manager in a single call.
//
// Design:
//
//   - One envtest.Environment per process, guarded by a sync.Once. The
//     three storage suites issue per-test unique namespace and resource
//     names, so they do not contend on the shared apiserver — and the
//     ~3s envtest startup cost (kube-apiserver + etcd binaries) amortizes
//     across every test in the package rather than every test function.
//
//   - The CRDs in config/holos-console/crd/ are installed at startup time. The
//     ValidatingAdmissionPolicy manifests in config/holos-console/admission/ are
//     applied once and their registration is awaited so the CEL compiler
//     is warm before any test runs.
//
//   - Each StartManager(t) call builds a new controller-runtime Manager
//     against the shared REST config, primes the informers the caller
//     asks for, and registers t.Cleanup so the Manager is shut down at
//     test-end.
//
//   - The underlying envtest.Environment itself (the kube-apiserver +
//     etcd subprocesses and their temp data dirs) is owned by the shared
//     singleton. Consumers wire up deterministic teardown by calling
//     RunTestsWithSharedEnv from their package's TestMain so the
//     apiserver is stopped after every test in the package runs. A
//     SIGINT / SIGTERM handler is also installed as a safety net so an
//     interrupted `go test` run does not leak the apiserver.
//
// Skip semantics: when KUBEBUILDER_ASSETS is unset and no cached
// kubebuilder-envtest download is present, StartManager calls t.Skip so
// developers without `setup-envtest use` can still run `go test ./...`.
package crdmgrtesting

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	runtimepkg "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Env bundles the shared envtest primitives a storage test suite needs.
// The REST config is shared across tests in the same process; every other
// field is per-StartManager and scoped to the current test via
// t.Cleanup.
type Env struct {
	// Cfg is the shared REST config pointing at the process-wide envtest
	// kube-apiserver. Tests that need to construct additional clients
	// (for example, a second cache-backed Manager for multi-freshness
	// assertions) reuse this Cfg.
	Cfg *rest.Config
	// Client is the cache-backed delegating client from the test's
	// Manager. Reads come from informers primed by StartManager; writes
	// fall through to the apiserver. This is the client production
	// wires into NewK8sClient(...), so using it here keeps the
	// cache-freshness regressions honest.
	Client ctrlclient.Client
	// Direct is an uncached controller-runtime client that round-trips
	// to the apiserver on every Get / List. Tests use it for seed
	// writes (namespace Create, pre-populated fixtures) so the test
	// body is not tangled with Eventually-wraps on trivial setup.
	Direct ctrlclient.Client
}

// Options controls what StartManager primes in the cache before
// returning. Callers pass the CRD kinds their suite operates on so the
// informer is registered and synced before any test writes land.
type Options struct {
	// Scheme must register every kind the test suite reads or writes,
	// including templates.holos.run/v1alpha1 and core/v1. Typically the
	// caller constructs this once per package and reuses it.
	Scheme *runtimepkg.Scheme
	// InformerObjects is the set of typed objects whose informers the
	// Manager primes before starting. Each entry must be registered in
	// Scheme. Without priming, the first List through the delegating
	// client lazily starts a watch and can race a just-completed Create.
	InformerObjects []ctrlclient.Object
	// WaitForAdmissionPolicies names the ValidatingAdmissionPolicies the
	// caller expects to be live before tests run. StartManager waits
	// for each name to appear on the apiserver so test bodies do not
	// race the CEL compiler.
	WaitForAdmissionPolicies []string
}

// sharedEnv holds the process-singleton envtest state. Populated by the
// first StartManager call; subsequent calls reuse it. stopped is set by
// stopSharedEnv at process-exit paths (not t.Cleanup, because t outlives
// a single test but the process outlives all tests).
type sharedEnv struct {
	env *envtest.Environment
	cfg *rest.Config
	// admissionInstalled guards one-time application of the
	// config/holos-console/admission/*.yaml manifests. After the first suite primes
	// them, subsequent suites only need to wait for registration; the
	// VAPs themselves are apiserver-wide state.
	admissionInstalled sync.Once
	admissionErr       error
}

var (
	sharedEnvOnce sync.Once
	sharedEnvErr  error
	shared        *sharedEnv
)

// StartManager boots (or reuses) the process-singleton envtest
// environment, constructs a controller-runtime Manager primed with the
// requested informers, and returns an Env value the test suite can
// inject into its K8sClient.
//
// StartManager calls t.Skip when the envtest binaries are not
// discoverable, so a developer without `setup-envtest use` installed
// still gets a green `go test ./...` run.
func StartManager(t *testing.T, opts Options) *Env {
	t.Helper()

	if opts.Scheme == nil {
		t.Fatalf("crdmgrtesting.StartManager: Options.Scheme is required")
	}

	ensureSharedEnv(t)
	if shared == nil {
		// ensureSharedEnv already called t.Skip or t.Fatalf.
		return nil
	}

	// The VAPs are apiserver-wide state; install them once per process.
	// Subsequent calls still wait for the named policies to be visible
	// via the current test's client so the CEL compiler is warm before
	// the test body runs.
	shared.admissionInstalled.Do(func() {
		repoRoot, err := findRepoRoot()
		if err != nil {
			shared.admissionErr = fmt.Errorf("find repo root: %w", err)
			return
		}
		admDir := filepath.Join(repoRoot, "config", "holos-console", "admission")
		if _, err := os.Stat(admDir); err != nil {
			// No admission dir — this is fine; the suite just won't
			// have VAPs to wait on.
			return
		}
		direct, err := ctrlclient.New(shared.cfg, ctrlclient.Options{Scheme: opts.Scheme})
		if err != nil {
			shared.admissionErr = fmt.Errorf("construct direct client for admission install: %w", err)
			return
		}
		if err := applyAdmissionYAMLFiles(context.Background(), direct, admDir); err != nil {
			shared.admissionErr = fmt.Errorf("apply admission policies: %w", err)
			return
		}
	})
	if shared.admissionErr != nil {
		t.Fatalf("installing admission policies: %v", shared.admissionErr)
	}

	// Uncached client for test setup (namespace Create, seed writes,
	// fixture pre-population). Each StartManager call builds its own
	// against the shared REST config — cheap, and keeps the scheme
	// scoped to the caller's needs.
	direct, err := ctrlclient.New(shared.cfg, ctrlclient.Options{Scheme: opts.Scheme})
	if err != nil {
		t.Fatalf("constructing direct client: %v", err)
	}

	// Cache-backed delegating Manager. Mirrors the production wiring in
	// console.go: writes go to the apiserver, reads go through the
	// informer cache.
	mgr, err := ctrl.NewManager(shared.cfg, ctrl.Options{
		Scheme: opts.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // disable metrics listener in tests
		},
		HealthProbeBindAddress: "0", // disable readiness listener
	})
	if err != nil {
		t.Fatalf("constructing manager: %v", err)
	}

	// Prime informers before Start so the cache has watches registered
	// against the apiserver before the first List. Without this the
	// first cache-backed read lazily starts a watch and can race a
	// just-issued Create — the exact flake the acceptance criterion
	// calls out.
	for _, obj := range opts.InformerObjects {
		if _, err := mgr.GetCache().GetInformer(context.Background(), obj); err != nil {
			t.Fatalf("priming informer for %T: %v", obj, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- mgr.Start(ctx)
	}()

	// Bound the cache-sync wait so a broken CRD install or scheme
	// mismatch fails the test promptly rather than timing out on the
	// first List.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer waitCancel()
	if !mgr.GetCache().WaitForCacheSync(waitCtx) {
		cancel()
		t.Fatalf("manager cache did not sync within deadline")
	}

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Logf("manager exit: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Logf("manager did not shut down within deadline")
		}
	})

	// Wait for each requested VAP to appear before returning so the
	// test body never races the CEL compiler. The policies themselves
	// were installed by the admissionInstalled.Do branch above
	// (possibly in a prior suite); this loop just observes
	// registration.
	for _, name := range opts.WaitForAdmissionPolicies {
		waitForAdmissionPolicy(t, context.Background(), direct, name)
	}

	return &Env{
		Cfg:    shared.cfg,
		Client: mgr.GetClient(),
		Direct: direct,
	}
}

// ensureSharedEnv starts the process-singleton envtest.Environment on
// first use. t.Skip is called with a helpful message when envtest
// binaries are not installed.
func ensureSharedEnv(t *testing.T) {
	t.Helper()

	sharedEnvOnce.Do(func() {
		if os.Getenv("KUBEBUILDER_ASSETS") == "" {
			if assets := detectEnvtestAssets(); assets != "" {
				// setenv here (not t.Setenv) because this runs once
				// per process and the setting must persist across
				// every subsequent StartManager call.
				if err := os.Setenv("KUBEBUILDER_ASSETS", assets); err != nil {
					sharedEnvErr = fmt.Errorf("setenv KUBEBUILDER_ASSETS: %w", err)
					return
				}
			} else {
				sharedEnvErr = errSkipNoEnvtest
				return
			}
		}

		repoRoot, err := findRepoRoot()
		if err != nil {
			sharedEnvErr = fmt.Errorf("find repo root: %w", err)
			return
		}

		e := &envtest.Environment{
			CRDDirectoryPaths:     []string{filepath.Join(repoRoot, "config", "holos-console", "crd")},
			ErrorIfCRDPathMissing: true,
		}
		cfg, err := e.Start()
		if err != nil {
			sharedEnvErr = fmt.Errorf("start envtest: %w", err)
			return
		}
		shared = &sharedEnv{env: e, cfg: cfg}
		// The singleton owns the apiserver/etcd subprocesses for the
		// rest of the test binary's lifetime. Deterministic shutdown
		// happens via RunTestsWithSharedEnv (invoked from the test
		// package's TestMain); the signal handler below is a safety
		// net that catches ^C / SIGTERM so an interrupted `go test`
		// run does not leak envtest children.
		installShutdownSignalHandler()
	})
	if sharedEnvErr != nil {
		if errors.Is(sharedEnvErr, errSkipNoEnvtest) {
			t.Skip("envtest binaries not found; set KUBEBUILDER_ASSETS or run `setup-envtest use` to download")
			return
		}
		t.Fatalf("starting shared envtest environment: %v", sharedEnvErr)
	}
}

// errSkipNoEnvtest is a sentinel for the "envtest binaries missing"
// branch of ensureSharedEnv so the t.Skip path is distinguishable from
// a real bootstrap failure.
var errSkipNoEnvtest = errors.New("envtest binaries not found")

// applyAdmissionYAMLFiles reads every *.yaml file in dir and applies
// each ValidatingAdmissionPolicy / ValidatingAdmissionPolicyBinding
// document through the controller-runtime client. Pre-HOL-663 each
// storage suite carried its own copy of this helper; this is the
// single authoritative implementation.
func applyAdmissionYAMLFiles(ctx context.Context, c ctrlclient.Client, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		if e.Name() == "kustomization.yaml" {
			// kustomization.yaml is a kustomize index; skip it.
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		for _, doc := range splitYAMLDocuments(data) {
			if len(strings.TrimSpace(string(doc))) == 0 {
				continue
			}
			if err := applyAdmissionDoc(ctx, c, doc); err != nil {
				return fmt.Errorf("apply doc from %s: %w", e.Name(), err)
			}
		}
	}
	return nil
}

// splitYAMLDocuments splits a multi-doc YAML stream on "---" lines.
// Preserves the exact behavior of the pre-HOL-663 copies so existing
// config/holos-console/admission/*.yaml files continue to parse unchanged.
func splitYAMLDocuments(data []byte) [][]byte {
	var docs [][]byte
	var current []byte
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "---" {
			if len(current) > 0 {
				docs = append(docs, current)
			}
			current = nil
			continue
		}
		current = append(current, []byte(line+"\n")...)
	}
	if len(current) > 0 {
		docs = append(docs, current)
	}
	return docs
}

// applyAdmissionDoc decodes a single YAML doc and Creates it through
// the controller-runtime client. AlreadyExists is treated as success so
// repeat applies are idempotent — the process-singleton env may have
// already installed the policy in a prior suite run.
func applyAdmissionDoc(ctx context.Context, c ctrlclient.Client, doc []byte) error {
	kindProbe := struct {
		Kind string `json:"kind"`
	}{}
	if err := yaml.Unmarshal(doc, &kindProbe); err != nil {
		return fmt.Errorf("unmarshal kind: %w", err)
	}
	switch kindProbe.Kind {
	case "ValidatingAdmissionPolicy":
		policy := &admissionregistrationv1.ValidatingAdmissionPolicy{}
		if err := yaml.Unmarshal(doc, policy); err != nil {
			return fmt.Errorf("unmarshal policy: %w", err)
		}
		if err := c.Create(ctx, policy); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		return nil
	case "ValidatingAdmissionPolicyBinding":
		binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
		if err := yaml.Unmarshal(doc, binding); err != nil {
			return fmt.Errorf("unmarshal binding: %w", err)
		}
		if err := c.Create(ctx, binding); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported admission kind %q", kindProbe.Kind)
	}
}

// waitForAdmissionPolicy polls for a ValidatingAdmissionPolicy to be
// registered with the API server. Without this poll, the first Create
// races the apiserver's CEL compiler and the calling test sees a false
// negative (admission should have rejected, but the policy wasn't
// compiled yet).
func waitForAdmissionPolicy(t *testing.T, ctx context.Context, c ctrlclient.Client, name string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
		if err := c.Get(ctx, types.NamespacedName{Name: name}, vap); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("admission policy %q not registered within deadline", name)
}

// detectEnvtestAssets probes the default kubebuilder-envtest cache
// location for an installed apiserver. Returns an empty string when no
// cached install is present; callers interpret that as a Skip trigger.
// Matches the helper the pre-HOL-663 suites carried inline so the same
// environments continue to satisfy the bootstrap.
func detectEnvtestAssets() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	base := filepath.Join(home, ".local", "share", "kubebuilder-envtest", "k8s")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	var best string
	for _, en := range entries {
		if !en.IsDir() {
			continue
		}
		cand := filepath.Join(base, en.Name())
		if _, err := os.Stat(filepath.Join(cand, "kube-apiserver")); err == nil {
			if best == "" || en.Name() > filepath.Base(best) {
				best = cand
			}
		}
	}
	return best
}

// RunTestsWithSharedEnv is the TestMain wrapper a consumer package uses
// to guarantee the shared envtest.Environment is stopped after every
// test in the package runs, rather than relying on the apiserver
// subprocess being reaped when the test binary exits.
//
// Usage in a consumer package:
//
//	func TestMain(m *testing.M) {
//	    os.Exit(crdmgrtesting.RunTestsWithSharedEnv(m))
//	}
//
// The returned code is the exit code from m.Run(); callers pass it to
// os.Exit themselves so os.Exit defers (such as this package's deferred
// stopSharedEnv) run before the process actually exits.
func RunTestsWithSharedEnv(m *testing.M) int {
	code := m.Run()
	stopSharedEnv()
	return code
}

// stopSharedEnv shuts down the process-singleton envtest environment if
// one was started. Idempotent: subsequent calls are no-ops. Safe to call
// from both RunTestsWithSharedEnv and the signal handler.
func stopSharedEnv() {
	sharedStopOnce.Do(func() {
		if shared == nil || shared.env == nil {
			return
		}
		if err := shared.env.Stop(); err != nil {
			// Stop() failures are logged but not fatal: we are already
			// on the exit path, and leaving a stray message on stderr
			// is the most useful thing we can do.
			fmt.Fprintf(os.Stderr, "crdmgrtesting: envtest.Environment.Stop: %v\n", err)
		}
	})
}

var sharedStopOnce sync.Once

// installShutdownSignalHandler registers a one-shot SIGINT / SIGTERM
// handler that stops the shared envtest environment before re-raising
// the signal default behavior (process exit with the conventional
// 128+signo code). Installed from inside sharedEnvOnce so it is only
// wired up if the shared env actually started.
func installShutdownSignalHandler() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-ch
		stopSharedEnv()
		// Restore default disposition and re-raise so the process exits
		// with the expected 128+signo code rather than swallowing the
		// signal.
		signal.Reset(sig.(syscall.Signal))
		if p, err := os.FindProcess(os.Getpid()); err == nil {
			_ = p.Signal(sig)
		}
	}()
}

// findRepoRoot walks up from this source file looking for go.mod.
// Mirrors the inline copies the pre-HOL-663 suites carried; the
// runtime.Caller frame resolves to this file's directory so the walk
// starts inside console/crdmgr/testing/.
func findRepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %q", file)
		}
		dir = parent
	}
}
