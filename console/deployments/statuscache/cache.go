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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	appsv1listers "k8s.io/client-go/listers/apps/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

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
// scoped to console-managed Deployments.
type informerCache struct {
	lister appsv1listers.DeploymentLister
}

// New constructs a Cache. When client is nil (dummy-secret-only mode) the
// returned cache is a no-op that always reports misses.
//
// When client is non-nil, a SharedInformerFactory is created with a label
// selector tweak bounding the watch to console-managed Deployments. The
// factory is started with ctx and its informers are stopped when ctx is
// cancelled. New blocks until the deployment informer's cache has synced so
// callers may immediately issue reads.
func New(ctx context.Context, client kubernetes.Interface) (Cache, error) {
	if client == nil {
		return nopCache{}, nil
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
	_ = depInformer.Informer()

	factory.Start(ctx.Done())
	synced := factory.WaitForCacheSync(ctx.Done())
	for informerType, ok := range synced {
		if !ok {
			return nil, fmt.Errorf("statuscache: informer %v failed to sync", informerType)
		}
	}
	return &informerCache{lister: depInformer.Lister()}, nil
}

// Summary returns the DeploymentStatusSummary for the given (ns, name) if the
// informer cache knows about the Deployment.
func (c *informerCache) Summary(ns, name string) (*consolev1.DeploymentStatusSummary, bool) {
	dep, err := c.lister.Deployments(ns).Get(name)
	if err != nil || dep == nil {
		return nil, false
	}
	return summaryFromDeployment(dep), true
}

// summaryFromDeployment projects an apps/v1.Deployment into the lightweight
// DeploymentStatusSummary proto. Phase derivation rules (see issue #914):
//
//   - Available=True and ready == desired           -> RUNNING
//   - ReplicaFailure=True or Progressing=False      -> FAILED
//   - ready < desired (and none of the above)       -> PENDING
//   - otherwise                                     -> PENDING (still settling)
//
// message is populated from the first FAILED-signaling condition so callers
// can surface a terse cause (e.g. "quota exceeded"). When no such condition
// exists, message is empty.
func summaryFromDeployment(dep *appsv1.Deployment) *consolev1.DeploymentStatusSummary {
	status := dep.Status
	desired := status.Replicas
	ready := status.ReadyReplicas

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

	phase := consolev1.DeploymentPhase_DEPLOYMENT_PHASE_PENDING
	switch {
	case replicaFailure, progressingFalse:
		phase = consolev1.DeploymentPhase_DEPLOYMENT_PHASE_FAILED
	case available && ready == desired && desired > 0:
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
