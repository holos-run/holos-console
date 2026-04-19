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

// Package controller embeds a sigs.k8s.io/controller-runtime manager inside
// the holos-console binary. The manager owns the informer caches and the
// reconcilers for every CRD in the templates.holos.run API group.
//
// The ADR behind this layout is ADR 030 in holos-console-docs: the console
// is a web application that happens to embed a controller manager, rather
// than a controller binary that happens to serve HTTP. HOL-620 lands the
// manager and the reconcilers; HOL-621 rewires the existing RPC handler
// K8sClients to read from `mgr.GetClient()` (a cache-backed client) so the
// cache becomes the authoritative read path for every list/watchable kind.
package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"

	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// Scheme is the controller-runtime scheme shared by the embedded manager and
// by any cache-backed client constructed from the manager. Callers that need
// a standalone client (for envtest, for example) should use this scheme so
// the registered types line up with what the manager reconciles.
var Scheme = runtime.NewScheme()

func init() {
	// Core types — every controller pulls list/get on Namespace for the
	// console.holos.run/resource-type label read.
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	// Our custom types.
	utilruntime.Must(v1alpha1.AddToScheme(Scheme))
}

// Options holds the knobs the console needs to construct a controller-runtime
// manager. LeaderElection stays off in HOL-620 — the console currently runs
// as a singleton Deployment and there is no active-active controller path.
// HOL-621 will re-evaluate when the cache becomes the authoritative read
// path for RPC handlers.
type Options struct {
	// MetricsBindAddress controls the controller-runtime metrics listener.
	// Leave empty to disable — the console already exposes Prometheus
	// metrics via its own /metrics handler. Use ":8081" or similar to
	// enable the separate controller-runtime metrics server.
	MetricsBindAddress string
	// HealthProbeBindAddress controls the controller-runtime probe
	// listener. Leave empty to disable — the console serves /healthz and
	// /readyz from its own http.ServeMux. HOL-620 gates the console's
	// /readyz on the cache-sync flag exposed by the Manager.
	HealthProbeBindAddress string
	// CacheSyncTimeout caps how long the manager waits for every informer
	// cache to complete its initial LIST/WATCH sync. Callers that need a
	// shorter deadline (envtest, local development) can override here.
	CacheSyncTimeout time.Duration
	// Logger is the slog-style logger used for top-level manager logs. The
	// reconcilers themselves use log.FromContext(ctx) inside Reconcile.
	// Leave nil to use slog.Default().
	Logger *slog.Logger
	// SkipControllerNameValidation disables controller-runtime's
	// process-global check that every controller name is unique. This
	// exists exclusively for the envtest suite, where each test spins up
	// its own manager using the same hard-coded controller names — the
	// metric uniqueness constraint is not meaningful in that context and
	// would otherwise force every test to either share a manager (fragile)
	// or invent per-test controller names (uglier). Production callers
	// leave this false; NewManager enforces that by never exposing a path
	// that flips it on in the main binary.
	SkipControllerNameValidation bool
	// NamespacePrefix, OrganizationPrefix, FolderPrefix, and
	// ProjectPrefix mirror the resolver.Resolver configuration the
	// console reads from flags. The TemplatePolicyBinding reconciler
	// uses these to compute <NamespacePrefix><ProjectPrefix><projectName>
	// when resolving ProjectTemplate target refs. Empty strings for
	// anything except NamespacePrefix fall back to "org-"/"fld-"/"prj-"
	// inside the reconciler.
	NamespacePrefix    string
	OrganizationPrefix string
	FolderPrefix       string
	ProjectPrefix      string
}

// Manager wraps a sigs.k8s.io/controller-runtime manager.Manager plus a
// lightweight readiness flag. HOL-620 exposes exactly the surface later
// phases need:
//
//   - GetClient returns the cache-backed client.Client. HOL-621 will hand
//     this to every storage client so reads come out of the informer cache.
//   - Ready reports true after the manager's cache has completed its initial
//     sync. The console /readyz probe fans out to Ready() in addition to the
//     pre-existing `ready` atomic so readiness remains 503 until the cache
//     is warm.
//   - Start blocks until the provided context is cancelled, returning any
//     error from the underlying manager. Callers run it in a supervised
//     goroutine and propagate errors via an errgroup or an error channel.
type Manager struct {
	mgr              manager.Manager
	ready            atomic.Bool
	cacheSyncTimeout time.Duration
	logger           *slog.Logger
}

// NewManager constructs a Manager from the provided rest config, scheme, and
// Options. The typed reconcilers are registered with
// `ctrl.NewControllerManagedBy(mgr)` inside this constructor so callers get a
// fully-wired manager back — they only need to call Start(ctx).
//
// cfg is required; scheme is optional and falls back to the package-global
// Scheme when nil so envtests and production callers can share the common
// registration path. Callers that need additional types registered (e.g., a
// test that adds apiextensions/v1 for CRD installation) should pass their own
// scheme built on top of controller.Scheme.
func NewManager(cfg *rest.Config, scheme *runtime.Scheme, opts Options) (*Manager, error) {
	if cfg == nil {
		return nil, errors.New("controller.NewManager: rest config is required")
	}
	if scheme == nil {
		scheme = Scheme
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	cacheSyncTimeout := opts.CacheSyncTimeout
	if cacheSyncTimeout == 0 {
		// controller-runtime's default is 2 minutes. We ship a tighter
		// default so the console's /readyz probe flips within a
		// kubelet readinessProbe cycle in the common case and the
		// deployment rolls cleanly. The envtest suite overrides with
		// a shorter value so CI does not hang on a misconfigured
		// cluster.
		cacheSyncTimeout = 90 * time.Second
	}

	// Restrict the ConfigMap cache to console-managed render-state records
	// (HOL-622). ConfigMap is a cluster-wide high-churn kind; watching every
	// ConfigMap in the cluster would explode the cache. The
	// AppliedRenderStateClient only reads ConfigMaps labelled
	// app.kubernetes.io/managed-by=holos-console,console.holos.run/resource-type=render-state,
	// so we scope the informer to that label pair and the rest of the
	// cluster's ConfigMaps never touch memory. Reads for unlabelled
	// ConfigMaps would still hit the apiserver via the delegating client's
	// fallback path, but the render-state path goes entirely through the
	// cache.
	renderStateSelector := labels.SelectorFromSet(labels.Set{
		v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeRenderState,
	})
	ctrlOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: opts.MetricsBindAddress,
		},
		HealthProbeBindAddress: opts.HealthProbeBindAddress,
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.ConfigMap{}: {Label: renderStateSelector},
			},
		},
		// Console is a singleton Deployment today; leader election is
		// off. Revisit when the cache becomes the authoritative read
		// path (HOL-621+) and multi-replica rollouts are explored.
		LeaderElection:   false,
		LeaderElectionID: "holos-console-controller-lock",
	}
	if opts.SkipControllerNameValidation {
		skip := true
		ctrlOpts.Controller = config.Controller{SkipNameValidation: &skip}
	}
	mgr, err := ctrl.NewManager(cfg, ctrlOpts)
	if err != nil {
		return nil, fmt.Errorf("controller.NewManager: building manager: %w", err)
	}

	m := &Manager{mgr: mgr, cacheSyncTimeout: cacheSyncTimeout, logger: logger}

	// Register the three reconcilers. Each reconciler owns its own event
	// recorder keyed by controller name so emitted events are attributable
	// in the cluster event log.
	if err := (&TemplateReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("template-controller"),
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplateReconciler: %w", err)
	}
	if err := (&TemplatePolicyReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("template-policy-controller"),
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplatePolicyReconciler: %w", err)
	}
	if err := (&TemplatePolicyBindingReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		Recorder:           mgr.GetEventRecorderFor("template-policy-binding-controller"),
		NamespacePrefix:    opts.NamespacePrefix,
		OrganizationPrefix: opts.OrganizationPrefix,
		FolderPrefix:       opts.FolderPrefix,
		ProjectPrefix:      opts.ProjectPrefix,
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplatePolicyBindingReconciler: %w", err)
	}

	// Prime the Namespace informer so the reconcilers (HOL-621+) can read
	// console.holos.run/resource-type labels without round-trips. Namespace
	// is otherwise not brought into the cache by any of the three
	// reconcilers above since none of them watches Namespace directly.
	//
	// We register the informer via GetInformer so the cache LIST happens
	// during manager Start(), not now. Start-time registration is a
	// controller-runtime pattern for "I want this kind watched but I
	// don't have a reconciler on it".
	if _, err := mgr.GetCache().GetInformer(context.Background(), &corev1.Namespace{}); err != nil {
		return nil, fmt.Errorf("controller.NewManager: priming namespace informer: %w", err)
	}

	// Prime the ConfigMap informer for render-state ConfigMaps (HOL-622).
	// The label-scoped cache option set above means this informer only
	// holds the console-managed render-state subset.
	// AppliedRenderStateClient reads through mgr.GetClient(); with the
	// informer primed the read lands in the cache instead of round-
	// tripping to the apiserver on every drift check.
	if _, err := mgr.GetCache().GetInformer(context.Background(), &corev1.ConfigMap{}); err != nil {
		return nil, fmt.Errorf("controller.NewManager: priming configmap informer: %w", err)
	}

	return m, nil
}

// GetClient returns the cache-backed client.Client. Writes go straight to
// the API server; reads come from the informer cache after sync. HOL-621
// rewires every storage client to consume this method.
func (m *Manager) GetClient() client.Client {
	return m.mgr.GetClient()
}

// GetManager returns the underlying controller-runtime manager.Manager for
// rare callers (notably tests) that need access to the whole surface. HOL-621
// should avoid using this — prefer GetClient() for storage and Ready() for
// readiness.
func (m *Manager) GetManager() manager.Manager {
	return m.mgr
}

// Ready reports whether the manager's informer caches have completed their
// initial sync. The console /readyz probe ANDs this with its own readiness
// flag: the listener is up AND the cache is warm AND the rest of the server
// has finished wiring up.
func (m *Manager) Ready() bool {
	return m.ready.Load()
}

// Start runs the manager. It blocks until ctx is cancelled or the manager
// returns with an error. The cache-sync watcher is started in-line so the
// readiness flag flips the moment the caches go healthy — callers that need
// a cache-sync deadline as a hard error should wait for ctx.Done() on a
// context of their choosing.
//
// Start is intended to be called exactly once per Manager. Callers running
// multiple managers (the envtest multi-manager freshness test is the only
// current example) construct one Manager per process region and each call
// Start() in its own goroutine.
func (m *Manager) Start(ctx context.Context) error {
	// Spawn a watcher that flips the readiness flag when the informer
	// cache has synced. We use a separate context so the watcher tears
	// down cleanly when the root context is cancelled even before the
	// cache finishes syncing (the graceful-shutdown test exercises this
	// path).
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()

	go func() {
		attempt := m.cacheSyncTimeout
		if attempt <= 0 {
			attempt = 90 * time.Second
		}
		// Retry WaitForCacheSync until the watch context is cancelled
		// (manager shutdown). Each attempt has a bounded deadline so the
		// warn-log fires early enough for an operator to notice a
		// degraded start, but Ready() can still flip to true the moment
		// the cache finally warms on a slow cluster — without this
		// outer loop a one-shot WaitForCacheSync that times out would
		// leave /readyz at 503 forever even though the manager is
		// healthy.
		for {
			if watchCtx.Err() != nil {
				return
			}
			waitCtx, cancelWait := context.WithTimeout(watchCtx, attempt)
			ok := m.mgr.GetCache().WaitForCacheSync(waitCtx)
			cancelWait()
			if ok {
				m.ready.Store(true)
				m.logger.Info("controller manager cache synced")
				return
			}
			if watchCtx.Err() != nil {
				// Parent context cancelled — shutdown in flight.
				// Leave ready=false and return quietly.
				return
			}
			// Deadline fired before any cache synced. Warn and try
			// again on the next attempt window.
			m.logger.Warn("controller manager cache did not sync within deadline; retrying",
				"deadline", attempt)
		}
	}()

	if err := m.mgr.Start(ctx); err != nil {
		// Manager returning an error before it ever went ready is the
		// signal HOL-620's "sync error" test asserts on: CRD missing,
		// RBAC missing, API server unreachable, etc.
		return fmt.Errorf("controller manager: %w", err)
	}
	return nil
}
