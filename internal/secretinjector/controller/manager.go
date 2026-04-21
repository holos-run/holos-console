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
)

// Scheme is the controller-runtime scheme shared by the secret-injector
// manager and any cache-backed client constructed from it. The scheme is
// populated at package init with corev1/etc. via client-go's
// clientgoscheme.AddToScheme and with the secrets.holos.run/v1alpha1 types
// registered by api/secrets/v1alpha1. M2 (reconcilers) keeps registering
// into this scheme.
var Scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	utilruntime.Must(secretsv1alpha1.AddToScheme(Scheme))
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

	return &Manager{mgr: mgr, cacheSyncTimeout: cacheSyncTimeout, logger: logger}, nil
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
