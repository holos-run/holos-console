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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	istiosecurityv1 "istio.io/client-go/pkg/apis/security/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	secretsv1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
	sicrypto "github.com/holos-run/holos-console/internal/secretinjector/crypto"
)

// Scheme is the controller-runtime scheme shared by the secret-injector
// manager and any cache-backed client constructed from it. The scheme is
// populated at package init with corev1/etc. via client-go's
// clientgoscheme.AddToScheme, the secrets.holos.run/v1alpha1 types
// registered by api/secrets/v1alpha1, and istio's security.istio.io/v1
// types registered by istio.io/client-go. M2 (reconcilers) keeps
// registering into this scheme.
//
// Vendor-weight tradeoff (HOL-752). The security.istio.io/v1
// AuthorizationPolicy types are the declarative mesh artifact the
// SecretInjectionPolicyBindingReconciler emits. We pull the Go types
// from istio.io/client-go (which transitively vendors
// istio.io/api/security/v1beta1 for the nested spec proto) rather than
// defining a local thin wrapper because a controller-runtime
// Owns(&AuthorizationPolicy{}) watch requires a registered
// runtime.Object that matches the CRD on the cluster. Pinning the istio
// minor version in go.mod is the agreed upgrade contract — bump when
// istio ships a CRD schema change that affects AuthorizationPolicy.
var Scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	utilruntime.Must(secretsv1alpha1.AddToScheme(Scheme))
	utilruntime.Must(istiosecurityv1.AddToScheme(Scheme))
}

// Options holds the knobs the holos-secret-injector binary needs to construct
// a controller-runtime manager. The shape mirrors
// internal/controller.Options so operators see a consistent surface across
// both holos binaries.
//
// No reconciler-specific options live here yet — M2 adds them as the
// reconcilers for SecretRequest, SecretInjectorPolicy, and
// SecretInjectorPolicyBinding land (see ADR 031 §4).
type Options struct {
	// MetricsBindAddress controls the controller-runtime metrics listener.
	// Leave empty to disable. Use ":8081" or similar to enable.
	MetricsBindAddress string
	// HealthProbeBindAddress controls the controller-runtime probe
	// listener. Leave empty to disable.
	HealthProbeBindAddress string
	// CacheSyncTimeout caps how long the manager waits for every informer
	// cache to complete its initial LIST/WATCH sync. Leave zero for the
	// 90s default that mirrors internal/controller.
	CacheSyncTimeout time.Duration
	// Logger is the slog-style logger used for top-level manager logs.
	// Leave nil to use slog.Default().
	Logger *slog.Logger
	// SkipControllerNameValidation disables controller-runtime's
	// process-global check that every controller name is unique. This
	// exists exclusively for envtest-style suites where each test spins up
	// its own manager with the same hard-coded controller names.
	SkipControllerNameValidation bool
	// ControllerNamespace is the namespace the pepper Bootstrap helper
	// writes into on first manager start. Leave empty to read from
	// POD_NAMESPACE via [sicrypto.ControllerNamespace]; an empty env var
	// and empty option together cause Start() to fail loudly rather than
	// silently sealing the pepper into the wrong namespace. envtest
	// suites set this field directly.
	ControllerNamespace string
	// SkipPepperBootstrap disables the pepper self-seal in Start(). The
	// flag exists exclusively for the narrow class of tests that
	// construct a Manager purely to verify wiring (for example, the
	// controller-runtime logger regression test in
	// manager_logger_test.go) and do not need the pepper Secret to
	// exist. Production callers MUST leave this false; the reconciler
	// cannot hash without a pepper.
	SkipPepperBootstrap bool

	// MeshTrustDomain is the SPIFFE trust domain the installed mesh
	// presents on ServiceAccount certificates. The
	// SecretInjectionPolicyBindingReconciler stamps this value into
	// every emitted AuthorizationPolicy's source.principals entry
	// (`<MeshTrustDomain>/ns/<ns>/sa/<name>`). Leave empty to use the
	// upstream Istio default (`cluster.local`); operators with a
	// re-pegged mesh MUST set MeshConfig.trustDomain here. HOL-752
	// review round 2 introduced this knob after flagging the hard-coded
	// default as silently-wrong on non-default meshes.
	MeshTrustDomain string
}

// Manager wraps a sigs.k8s.io/controller-runtime manager.Manager plus a
// lightweight readiness flag. The surface mirrors
// internal/controller.Manager so later phases (HOL-695+ reconcilers, HOL-712+
// ext_authz Runnable) can reuse the same patterns.
//
// No reconcilers are registered in this phase — NewManager returns a manager
// that stands up its informer caches (none, currently) and runs until ctx is
// cancelled. M2 adds reconciler registrations in place.
type Manager struct {
	mgr              manager.Manager
	ready            atomic.Bool
	cacheSyncTimeout time.Duration
	logger           *slog.Logger
	// cfg is retained so Start() can build a direct (non-cached)
	// client.Client for the one-shot pepper Bootstrap before the
	// informer caches come up. The informer-cache-backed client cannot
	// be used for bootstrap because the cache is not synced until
	// mgr.Start runs.
	cfg *rest.Config
	// controllerNamespace is the resolved namespace Bootstrap writes
	// into. Empty means Start() will consult POD_NAMESPACE.
	controllerNamespace string
	// skipPepperBootstrap mirrors the Options field; see the GoDoc
	// there for the narrow test-only use case.
	skipPepperBootstrap bool
	// credentialReconciler is the M2 Credential anchor. Start() injects
	// the pepper Loader into this pointer once the direct client has
	// been built — the pointer lets Reconcile observe the late-bound
	// Loader without a read/write race because the assignment happens
	// before mgr.Start returns control to any Runnable.
	credentialReconciler *CredentialReconciler
}

// NewManager constructs a Manager from the provided rest config and Options.
// cfg is required; Options are all defaulted. The scheme is the package-global
// Scheme populated in init().
//
// No reconcilers are registered here — M2 (HOL-695 and HOL-696) layers them in
// without changing the function signature. Callers run Start(ctx) in the main
// goroutine and block until the signal handler cancels ctx.
func NewManager(cfg *rest.Config, opts Options) (*Manager, error) {
	if cfg == nil {
		return nil, errors.New("controller.NewManager: rest config is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// Wire controller-runtime's global logger to slog so internal components
	// (priorityqueue, cache, leader election) emit through the same pipeline
	// as the rest of the binary. Without this, controller-runtime prints a
	// one-shot "log.SetLogger(...) was never called" stack trace on first
	// use (HOL-765).
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))
	cacheSyncTimeout := opts.CacheSyncTimeout
	if cacheSyncTimeout == 0 {
		// 90s matches the console manager so both binaries roll with the
		// same readiness window in production.
		cacheSyncTimeout = 90 * time.Second
	}

	ctrlOpts := ctrl.Options{
		Scheme: Scheme,
		Metrics: metricsserver.Options{
			BindAddress: opts.MetricsBindAddress,
		},
		HealthProbeBindAddress: opts.HealthProbeBindAddress,
		// Secret injector runs as a singleton today. Leader election is
		// off until the ext_authz Runnable needs active-active semantics.
		LeaderElection:   false,
		LeaderElectionID: "holos-secret-injector-controller-lock",
	}
	if opts.SkipControllerNameValidation {
		skip := true
		ctrlOpts.Controller = config.Controller{SkipNameValidation: &skip}
	}
	mgr, err := ctrl.NewManager(cfg, ctrlOpts)
	if err != nil {
		return nil, fmt.Errorf("controller.NewManager: building manager: %w", err)
	}

	// Register the secrets-group reconcilers. Each reconciler owns its own
	// event recorder keyed by controller name so emitted events are
	// attributable in the cluster event log. M2 lands one reconciler per
	// kind; HOL-750 registers UpstreamSecret first so the reconciler-
	// runtime wiring pattern is exercised in the smallest possible surface
	// area before the Credential and Binding reconcilers land.
	if err := (&UpstreamSecretReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("upstream-secret-controller"),
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering UpstreamSecretReconciler: %w", err)
	}

	// CredentialReconciler wiring — M2 anchor (HOL-751). The pepper
	// Loader is constructed lazily in Start() against a direct client
	// because the manager's cache is not yet synced at NewManager time
	// and because the shipped ClusterRole deliberately withholds
	// list/watch on core/v1 Secret. The reconciler is wired here with
	// a KDF from sicrypto.Default() and a nil Loader placeholder; Start
	// replaces the Loader once the direct client is built. Hot-path
	// reconciles before Start() completes are impossible because
	// controller-runtime only dispatches events after mgr.Start returns
	// from its Runnable init.
	credReconciler := &CredentialReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("credential-controller"),
		KDF:      sicrypto.Default(),
	}
	if err := credReconciler.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering CredentialReconciler: %w", err)
	}

	// SecretInjectionPolicyBindingReconciler wiring — M2 anchor (HOL-752).
	// The reconciler resolves spec.policyRef along the admission-validated
	// three-path rule (same namespace / parent label / organization label)
	// and emits a controller-owned security.istio.io/v1 AuthorizationPolicy
	// that declares the ext_authz allow-list for the named provider. The
	// provider name is a package-local constant so every binding emits the
	// same holos-secret-injector reference for M3; the trust domain
	// defaults to the upstream Istio value (cluster.local) but operators
	// with a re-pegged mesh set opts.MeshTrustDomain to override.
	meshTrustDomain := opts.MeshTrustDomain
	if meshTrustDomain == "" {
		meshTrustDomain = defaultBindingAuthzTrustDomain
	}
	if err := (&SecretInjectionPolicyBindingReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		Recorder:    mgr.GetEventRecorderFor("secretinjectionpolicybinding-controller"),
		TrustDomain: meshTrustDomain,
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering SecretInjectionPolicyBindingReconciler: %w", err)
	}

	return &Manager{
		credentialReconciler: credReconciler,
		mgr:                  mgr,
		cacheSyncTimeout:     cacheSyncTimeout,
		logger:               logger,
		cfg:                  cfg,
		controllerNamespace:  opts.ControllerNamespace,
		skipPepperBootstrap:  opts.SkipPepperBootstrap,
	}, nil
}

// GetClient returns the cache-backed client.Client. Writes go straight to the
// API server; reads come from the informer cache after sync. M2 reconcilers
// consume this via ctrl.NewControllerManagedBy(mgr).
func (m *Manager) GetClient() client.Client {
	return m.mgr.GetClient()
}

// GetManager returns the underlying controller-runtime manager.Manager for
// rare callers (notably tests) that need access to the whole surface.
func (m *Manager) GetManager() manager.Manager {
	return m.mgr
}

// Ready reports whether the manager's informer caches have completed their
// initial sync. M3 gates the ext_authz Runnable's health endpoint on Ready().
func (m *Manager) Ready() bool {
	return m.ready.Load()
}

// Start runs the manager. It blocks until ctx is cancelled or the manager
// returns an error. The cache-sync watcher is started in-line so the
// readiness flag flips the moment the caches go healthy. Callers typically
// pass ctrl.SetupSignalHandler() so SIGINT/SIGTERM drive a clean shutdown.
//
// Start is intended to be called exactly once per Manager.
func (m *Manager) Start(ctx context.Context) error {
	// Seal the pepper Secret before any reconciler runs. Bootstrap is
	// idempotent, so a warm restart is a single Get; on a cold start we
	// generate crypto/rand bytes and Create data["pepper-1"]. A
	// failure here is fatal: the Credential reconciler (HOL-751) cannot
	// Hash without a pepper, and falling back would produce an
	// unpeppered hash that silently weakens every credential written
	// after the missing bootstrap.
	if !m.skipPepperBootstrap {
		if err := m.bootstrapPepper(ctx); err != nil {
			return err
		}
	}

	// Wire the pepper Loader into the Credential reconciler now that the
	// direct client is ready. Done after Bootstrap so the first
	// reconcile after cache sync observes a non-empty pepper Secret; the
	// Loader itself does no caching so a later rotation takes effect on
	// its next Active() call without a manager restart. Skipped together
	// with bootstrap when the test-only SkipPepperBootstrap is set.
	if !m.skipPepperBootstrap && m.credentialReconciler != nil {
		ns := m.controllerNamespace
		if ns == "" {
			ns = sicrypto.ControllerNamespace()
		}
		directClient, err := client.New(m.cfg, client.Options{Scheme: Scheme})
		if err != nil {
			return fmt.Errorf("secret-injector controller manager: building pepper-loader client: %w", err)
		}
		loader, err := sicrypto.NewSecretLoader(directClient, ns)
		if err != nil {
			return fmt.Errorf("secret-injector controller manager: wiring pepper Loader: %w", err)
		}
		m.credentialReconciler.Pepper = loader
	}

	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()

	go func() {
		attempt := m.cacheSyncTimeout
		if attempt <= 0 {
			attempt = 90 * time.Second
		}
		for {
			if watchCtx.Err() != nil {
				return
			}
			waitCtx, cancelWait := context.WithTimeout(watchCtx, attempt)
			ok := m.mgr.GetCache().WaitForCacheSync(waitCtx)
			cancelWait()
			if ok {
				m.ready.Store(true)
				m.logger.Info("secret-injector controller manager cache synced")
				return
			}
			if watchCtx.Err() != nil {
				return
			}
			m.logger.Warn("secret-injector controller manager cache did not sync within deadline; retrying",
				"deadline", attempt)
		}
	}()

	if err := m.mgr.Start(ctx); err != nil {
		return fmt.Errorf("secret-injector controller manager: %w", err)
	}
	return nil
}

// bootstrapPepper runs the one-shot pepper self-seal against the
// controller's own namespace. Called from Start() before mgr.Start so the
// pepper Secret exists the moment a reconciler tries to Hash.
//
// The helper uses a direct (non-cached) client.Client constructed from
// m.cfg because the manager's cache is not yet synced at this point.
// Factored out of Start() so the control flow in Start() stays readable
// and so a future follow-up can stub the bootstrap path in tests by
// overriding this method via struct embedding if the need arises.
func (m *Manager) bootstrapPepper(ctx context.Context) error {
	ns := m.controllerNamespace
	if ns == "" {
		ns = sicrypto.ControllerNamespace()
	}
	if ns == "" {
		return fmt.Errorf("controller.Manager.Start: pepper bootstrap requires a controller namespace; set %s via the downward API or Options.ControllerNamespace",
			sicrypto.PodNamespaceEnv)
	}

	directClient, err := client.New(m.cfg, client.Options{Scheme: Scheme})
	if err != nil {
		return fmt.Errorf("controller.Manager.Start: building bootstrap client: %w", err)
	}

	result, err := sicrypto.Bootstrap(ctx, directClient, ns)
	if err != nil {
		return fmt.Errorf("controller.Manager.Start: pepper bootstrap: %w", err)
	}
	// Telemetry invariant: only the integer version and the byte length
	// are emitted. The pepper bytes themselves never touch the logger.
	m.logger.Info("pepper bootstrap complete",
		"namespace", ns,
		"secret", sicrypto.PepperSecretName,
		"activeVersion", result.ActiveVersion,
		"created", result.Created,
		"bytesLength", result.BytesLength)
	return nil
}
