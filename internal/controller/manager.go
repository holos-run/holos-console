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
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	"github.com/holos-run/holos-console/console/deployments"
	"github.com/holos-run/holos-console/console/resourcerbac"
)

var controllerRuntimeLoggerMu sync.Mutex

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
	// Deployment CRD (deployments.holos.run/v1alpha1) — registered so the
	// controller-runtime cache-backed client can read/write Deployment CRs
	// and the CRWriter can issue SSA patches through the manager's client
	// (HOL-957).
	utilruntime.Must(deploymentsv1alpha1.AddToScheme(Scheme))
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

	// GrantCache is the TemplateGrantCache the TemplateGrantReconciler
	// keeps current. When nil, NewManager allocates a fresh cache so
	// callers that only need the cache-backed client can omit it.
	// The console wires its GrantCache here so ValidateGrant reads from
	// the same snapshot the reconciler maintains.
	GrantCache *deployments.TemplateGrantCache
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
	grantCache       *deployments.TemplateGrantCache
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
	// Wire controller-runtime's global logger to slog so internal components
	// (priorityqueue, cache, leader election) emit through the same pipeline
	// as the rest of the binary. Without this, controller-runtime prints a
	// one-shot "log.SetLogger(...) was never called" stack trace on first
	// use (HOL-765).
	controllerRuntimeLoggerMu.Lock()
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))
	controllerRuntimeLoggerMu.Unlock()
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
	metricsBindAddress := opts.MetricsBindAddress
	if metricsBindAddress == "" {
		metricsBindAddress = "0"
	}

	// HOL-694 retired the ConfigMap cache scope: AppliedRenderStateClient
	// is the last in-process consumer of the cache-backed client.Client
	// that was reading ConfigMaps, and it now reads RenderState CRDs
	// instead. Other ConfigMap call sites (release publishing, deployment
	// caches) continue to use client-go directly without going through the
	// controller-runtime cache. If a future caller needs cache-backed
	// ConfigMap reads, restore a label-scoped Cache.ByObject entry to
	// avoid watching every ConfigMap in the cluster.
	ctrlOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsBindAddress,
		},
		HealthProbeBindAddress: opts.HealthProbeBindAddress,
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
	rbacClientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("controller.NewManager: building RBAC clientset: %w", err)
	}

	// Register the three reconcilers. Each reconciler owns its own event
	// recorder keyed by controller name so emitted events are attributable
	// in the cluster event log.
	if err := (&TemplateReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("template-controller"), //nolint:staticcheck // Controller-runtime still accepts this recorder in the pinned version.
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplateReconciler: %w", err)
	}
	if err := (&TemplatePolicyReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("template-policy-controller"), //nolint:staticcheck // Controller-runtime still accepts this recorder in the pinned version.
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplatePolicyReconciler: %w", err)
	}
	if err := (&TemplatePolicyBindingReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		Recorder:           mgr.GetEventRecorderFor("template-policy-binding-controller"), //nolint:staticcheck // Controller-runtime still accepts this recorder in the pinned version.
		NamespacePrefix:    opts.NamespacePrefix,
		OrganizationPrefix: opts.OrganizationPrefix,
		FolderPrefix:       opts.FolderPrefix,
		ProjectPrefix:      opts.ProjectPrefix,
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplatePolicyBindingReconciler: %w", err)
	}

	// Register the TemplateGrantReconciler (HOL-958). It watches
	// TemplateGrant and Namespace objects and keeps the GrantCache current
	// so ValidateGrant calls never need to round-trip to the API server.
	grantCache := opts.GrantCache
	if grantCache == nil {
		grantCache = deployments.NewTemplateGrantCache()
	}
	if err := (&TemplateGrantReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Cache:  grantCache,
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplateGrantReconciler: %w", err)
	}

	// Register the TemplateDependencyReconciler (HOL-959). It watches
	// TemplateDependency and Deployment objects; for each matching dependent
	// Deployment it calls EnsureSingletonDependencyDeployment so the shared
	// singleton Requires Deployment is materialised with the correct set of
	// non-controller ownerReferences.
	if err := (&TemplateDependencyReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("template-dependency-controller"), //nolint:staticcheck // Controller-runtime still accepts this recorder in the pinned version.
		Validator: grantCache,
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplateDependencyReconciler: %w", err)
	}

	// Register the TemplateRequirementReconciler (HOL-960). It watches
	// TemplateRequirement objects stored in org/folder namespaces and
	// cluster-wide Deployment objects; for each Deployment whose project
	// matches a TemplateRequirement's targetRefs[], it calls
	// EnsureSingletonDependencyDeployment so the platform-mandated singleton
	// Requires Deployment is materialised with the correct set of
	// non-controller ownerReferences.
	if err := (&TemplateRequirementReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("template-requirement-controller"), //nolint:staticcheck // Controller-runtime still accepts this recorder in the pinned version.
		Validator: grantCache,
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplateRequirementReconciler: %w", err)
	}

	if err := (&DeploymentReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("deployment-controller"), //nolint:staticcheck // Controller-runtime still accepts this recorder in the pinned version.
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering DeploymentReconciler: %w", err)
	}

	if err := resourcerbac.SetupTemplateReconciler(mgr, rbacClientset); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering Template RBAC reconciler: %w", err)
	}
	if err := resourcerbac.SetupTemplatePolicyReconciler(mgr, rbacClientset); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplatePolicy RBAC reconciler: %w", err)
	}
	if err := resourcerbac.SetupTemplatePolicyBindingReconciler(mgr, rbacClientset); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplatePolicyBinding RBAC reconciler: %w", err)
	}
	if err := resourcerbac.SetupTemplateGrantReconciler(mgr, rbacClientset); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplateGrant RBAC reconciler: %w", err)
	}
	if err := resourcerbac.SetupTemplateDependencyReconciler(mgr, rbacClientset); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplateDependency RBAC reconciler: %w", err)
	}
	if err := resourcerbac.SetupTemplateRequirementReconciler(mgr, rbacClientset); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering TemplateRequirement RBAC reconciler: %w", err)
	}
	if err := resourcerbac.SetupOrganizationReconciler(mgr, rbacClientset); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering Organization RBAC reconciler: %w", err)
	}
	if err := resourcerbac.SetupFolderReconciler(mgr, rbacClientset); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering Folder RBAC reconciler: %w", err)
	}
	if err := resourcerbac.SetupProjectReconciler(mgr, rbacClientset); err != nil {
		return nil, fmt.Errorf("controller.NewManager: registering Project RBAC reconciler: %w", err)
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

	// Prime the RenderState informer (HOL-694). No reconciler in this
	// package watches RenderState yet (the live render path writes the
	// object on the success path of Create/Update render-target handlers),
	// so without this call the first read through mgr.GetClient() would
	// lazily register the informer after /readyz is already green.
	// Register it eagerly so cache-sync readiness covers the kind and an
	// absent CRD / RBAC gap surfaces at Start rather than on first request.
	if _, err := mgr.GetCache().GetInformer(context.Background(), &v1alpha1.RenderState{}); err != nil {
		return nil, fmt.Errorf("controller.NewManager: priming renderstate informer: %w", err)
	}

	// Prime the TemplateRelease informer (HOL-693). No reconciler in this
	// package watches TemplateRelease yet (HOL-694 owns the controller), so
	// without this call the first release read through mgr.GetClient() would
	// lazily register the informer after /readyz is already green. Register
	// it eagerly so cache-sync readiness covers the release kind and an
	// absent CRD / RBAC gap surfaces at Start rather than on first request.
	if _, err := mgr.GetCache().GetInformer(context.Background(), &v1alpha1.TemplateRelease{}); err != nil {
		return nil, fmt.Errorf("controller.NewManager: priming templaterelease informer: %w", err)
	}

	// Prime the Deployment informer (HOL-957). The CRWriter issues SSA
	// patches through the manager's cache-backed client; without this eager
	// registration a missing Deployment CRD or RBAC gap would surface as a
	// silent cache-miss on first write rather than a startup failure. Register
	// it eagerly so an absent CRD is caught at Start before the pod reports
	// /readyz green.
	if _, err := mgr.GetCache().GetInformer(context.Background(), &deploymentsv1alpha1.Deployment{}); err != nil {
		return nil, fmt.Errorf("controller.NewManager: priming deployment informer: %w", err)
	}

	// Store the grant cache on the Manager so console.go can retrieve it
	// and pass it to the validator path.
	m.grantCache = grantCache

	return m, nil
}

// GetClient returns the cache-backed client.Client. Writes go straight to
// the API server; reads come from the informer cache after sync. HOL-621
// rewires every storage client to consume this method.
func (m *Manager) GetClient() client.Client {
	return m.mgr.GetClient()
}

// GetGrantCache returns the TemplateGrantCache that the TemplateGrantReconciler
// keeps current. Callers that need to perform TemplateGrant validation (e.g.,
// the Phase 5 and Phase 6 reconcilers) should read from this cache rather than
// issuing direct API server reads.
func (m *Manager) GetGrantCache() *deployments.TemplateGrantCache {
	return m.grantCache
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
