// Package statuscache provides a shared-informer-backed read-only cache of
// apps/v1 Deployment status, exposing a lightweight DeploymentStatusSummary
// projection for the DeploymentService listing hot path.
//
// Only apps/v1.Deployment is watched. No pod, replicaset, event, or
// configmap informers are added. All reads come from the local cache via a
// lister; there is no fallback to the API server. Callers treat a cache miss
// as "no data yet" and render the status column as UNSPECIFIED.
//
// Scope: the informer is filtered to resources labeled with
// app.kubernetes.io/managed-by=console.holos.run. The console creates the
// underlying apps/v1 Deployment by applying a rendered manifest whose labels
// include that managed-by value, so this selector bounds the watch to console
// managed deployments across all namespaces rather than enumerating managed
// namespaces up front.
package statuscache

import (
	"context"
	"log/slog"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	appsv1listers "k8s.io/client-go/listers/apps/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// initialSyncTimeout bounds how long the background sync watcher waits for
// the initial informer LIST/WATCH to complete before declaring the cache
// degraded and shutting the reflector down. Without this bound, a missing
// RBAC rule or a transiently unavailable API server would leave the
// reflector retrying LIST/WATCH for the lifetime of the process. The bound
// is deliberately short: status data is non-essential and the cache returns
// (nil, false) on misses anyway, so callers degrade to UNSPECIFIED until
// the operator fixes the underlying problem and restarts.
const initialSyncTimeout = 10 * time.Second

// initialSyncTimeoutForTest is the timeout actually used by New. Production
// code never overrides it; tests in this package replace it via
// TestMain-style cleanup hooks to exercise the failure path without
// blocking for the full production duration.
var initialSyncTimeoutForTest = initialSyncTimeout

// managedByLabelSelector limits the watch to Deployments labeled as managed
// by the console, matching the labels the deployments renderer applies to
// rendered resources.
var managedByLabelSelector = v1alpha2.LabelManagedBy + "=" + v1alpha2.ManagedByValue

// Cache exposes a read-only projection of apps/v1 Deployment status keyed by
// (namespace, name).
type Cache interface {
	// Summary returns the lightweight status projection for the Deployment
	// identified by (ns, name). The second return value is false on a cache
	// miss (including when the cache is not backed by a live cluster).
	Summary(ns, name string) (*consolev1.DeploymentStatusSummary, bool)
}

// nopCache is returned when no kubernetes.Interface is available (for
// example, when the console runs in dummy-secret-only mode). Summary always
// reports a miss so callers degrade to UNSPECIFIED status without panicking.
type nopCache struct{}

func (nopCache) Summary(string, string) (*consolev1.DeploymentStatusSummary, bool) {
	return nil, false
}

// informerCache is the live implementation, backed by a SharedInformerFactory
// scoped to console-managed Deployments. Until the underlying informer has
// completed its initial LIST/WATCH sync, Summary returns a miss so callers
// continue to render UNSPECIFIED rather than block.
type informerCache struct {
	lister    appsv1listers.DeploymentLister
	hasSynced func() bool
}

// New constructs a Cache. When client is nil (dummy-secret-only mode) the
// returned cache is a no-op that always reports misses.
//
// When client is non-nil, a SharedInformerFactory is created with a label
// selector tweak bounding the watch to console-managed Deployments. The
// informer is started in a background goroutine on a child context derived
// from ctx so server startup is never blocked on the initial LIST/WATCH —
// this matters because the cache is optional, callers already treat misses
// as "no data yet", and waiting up to initialSyncTimeout on every restart
// in the failure modes the fallback is meant to tolerate (missing RBAC,
// transient API unavailability) would turn an optional feature into a
// guaranteed multi-second readiness delay.
//
// A second background goroutine watches WaitForCacheSync. If the initial
// sync does not complete within initialSyncTimeout (or the parent context
// is cancelled), it cancels the child context, which stops the reflector
// goroutines so they do not leak and keep retrying LIST/WATCH for the
// lifetime of the process. The cache itself remains live but will simply
// keep reporting misses; callers should observe this via the returned
// log/metric and treat it as a degraded state.
//
// New never returns an error: the cache is best-effort by design.
func New(ctx context.Context, client kubernetes.Interface) Cache {
	if client == nil {
		return nopCache{}
	}
	factory := informers.NewSharedInformerFactoryWithOptions(
		client,
		0, // no resync: we rely on watch events for updates
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = managedByLabelSelector
		}),
	)
	depInformer := factory.Apps().V1().Deployments()
	// Register the informer with the factory so Start picks it up.
	informer := depInformer.Informer()

	// Derive a cancellable child context from the parent. We need a handle
	// to cancel() on the failure path (sync timeout) so the reflector
	// goroutines launched by factory.Start stop instead of retrying
	// LIST/WATCH for the lifetime of the process. The child is also
	// cancelled implicitly when ctx is cancelled, preserving the
	// shutdown-stops-informer contract.
	childCtx, cancel := context.WithCancel(ctx)
	factory.Start(childCtx.Done())

	cache := &informerCache{
		lister:    depInformer.Lister(),
		hasSynced: informer.HasSynced,
	}

	// Capture the sync timeout synchronously in New, before spawning the
	// watcher goroutine, so the value is read under the caller's happens-
	// before relationship rather than racing with a concurrent test that
	// swaps initialSyncTimeoutForTest between consecutive New calls.
	timeout := initialSyncTimeoutForTest

	// Watch the initial sync in the background. If it never completes
	// within initialSyncTimeout, cancel the child context to stop the
	// reflector and log the degradation. We deliberately do not block New
	// on this: server startup must not depend on optional status data.
	go func() {
		syncCtx, syncCancel := context.WithTimeout(childCtx, timeout)
		defer syncCancel()
		if !waitForCacheSync(syncCtx, informer.HasSynced) {
			slog.WarnContext(ctx,
				"deployment status informer did not sync within timeout, cancelling reflector to avoid background LIST/WATCH retry loop; cache will report UNSPECIFIED until the console is restarted",
				slog.Duration("timeout", timeout),
				slog.Any("parent_ctx_err", ctx.Err()),
			)
			cancel()
			return
		}
		slog.InfoContext(ctx, "deployment status informer synced")
	}()

	return cache
}

// waitForCacheSync polls hasSynced until it returns true or ctx is done. It
// mirrors cache.WaitForCacheSync but takes a context directly so the caller
// can derive deadlines without juggling stop channels.
func waitForCacheSync(ctx context.Context, hasSynced func() bool) bool {
	const interval = 100 * time.Millisecond
	timer := time.NewTicker(interval)
	defer timer.Stop()
	for {
		if hasSynced() {
			return true
		}
		select {
		case <-ctx.Done():
			return hasSynced()
		case <-timer.C:
		}
	}
}

// Summary returns the DeploymentStatusSummary for the given (ns, name) if the
// informer cache knows about the Deployment. Until the initial LIST/WATCH
// sync has completed, Summary always reports a miss so callers degrade to
// UNSPECIFIED rather than reading a partially-populated lister.
func (c *informerCache) Summary(ns, name string) (*consolev1.DeploymentStatusSummary, bool) {
	if c.hasSynced != nil && !c.hasSynced() {
		return nil, false
	}
	dep, err := c.lister.Deployments(ns).Get(name)
	if err != nil || dep == nil {
		return nil, false
	}
	return summaryFromDeployment(dep), true
}

// summaryFromDeployment projects an apps/v1.Deployment into the lightweight
// DeploymentStatusSummary proto. Phase derivation rules (see issue #914 and
// follow-up #941):
//
//   - ReplicaFailure=True or Progressing=False      -> FAILED
//   - Available=True, ready == desired, rollout converged  -> RUNNING
//   - otherwise                                     -> PENDING (reconciling)
//
// "rollout converged" means both:
//
//   - observedGeneration >= metadata.generation (controller has seen the
//     current spec), and
//   - updatedReplicas >= desired (every pod belongs to the latest ReplicaSet),
//     where desired is taken from spec.replicas (not status.replicas) so that
//     scale-ups before any new pod is created are not reported as RUNNING.
//
// Without these guards Kubernetes can satisfy Available=True and
// ready==desired from the previous ReplicaSet while a new rollout is still
// in progress, which would otherwise falsely render RUNNING for deployments
// that have not converged on the newly desired template.
//
// message is populated from the first FAILED-signaling condition so callers
// can surface a terse cause (e.g. "quota exceeded"). When no such condition
// exists, message is empty.
func summaryFromDeployment(dep *appsv1.Deployment) *consolev1.DeploymentStatusSummary {
	status := dep.Status
	// Derive desired from spec.replicas (Kubernetes defaults to 1 when nil) so
	// that in-progress rollouts and scale-ups are not reported as RUNNING. Using
	// status.Replicas would reflect the current ReplicaSet's pod count, which on
	// a scale-up before new pods exist still matches the old target.
	var desired int32 = 1
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	ready := status.ReadyReplicas
	updated := status.UpdatedReplicas
	observedGen := status.ObservedGeneration
	specGen := dep.Generation

	var (
		available        bool
		progressingFalse bool
		replicaFailure   bool
		failMessage      string
	)
	for _, c := range status.Conditions {
		switch c.Type {
		case appsv1.DeploymentAvailable:
			if c.Status == corev1.ConditionTrue {
				available = true
			}
		case appsv1.DeploymentProgressing:
			if c.Status == corev1.ConditionFalse {
				progressingFalse = true
				if failMessage == "" {
					failMessage = c.Message
				}
			}
		case appsv1.DeploymentReplicaFailure:
			if c.Status == corev1.ConditionTrue {
				replicaFailure = true
				if failMessage == "" {
					failMessage = c.Message
				}
			}
		}
	}

	// A deployment with desired==0 that Kubernetes marks Available=True is a
	// legitimate steady state (e.g. intentionally scaled to zero): do not
	// penalize it by forcing PENDING.
	converged := observedGen >= specGen && updated >= desired
	phase := consolev1.DeploymentPhase_DEPLOYMENT_PHASE_PENDING
	switch {
	case replicaFailure, progressingFalse:
		phase = consolev1.DeploymentPhase_DEPLOYMENT_PHASE_FAILED
	case available && ready == desired && converged:
		phase = consolev1.DeploymentPhase_DEPLOYMENT_PHASE_RUNNING
	}

	return &consolev1.DeploymentStatusSummary{
		Phase:              phase,
		ReadyReplicas:      ready,
		DesiredReplicas:    desired,
		AvailableReplicas:  status.AvailableReplicas,
		UpdatedReplicas:    status.UpdatedReplicas,
		ObservedGeneration: status.ObservedGeneration,
		Message:            failMessage,
	}
}
