package statuscache

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// waitForSummary polls cache.Summary until ok matches wantOK or the deadline
// expires. New is non-blocking, so tests must wait for the informer to
// observe the seeded objects before asserting on Summary results.
func waitForSummary(t *testing.T, cache Cache, ns, name string, wantOK bool) (*consolev1.DeploymentStatusSummary, bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, ok := cache.Summary(ns, name)
		if ok == wantOK {
			return got, ok
		}
		if time.Now().After(deadline) {
			return got, ok
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// buildDeployment returns an apps/v1 Deployment with the given status fields
// and conditions wired up for test table entries. metaGeneration sets
// ObjectMeta.Generation; observedGeneration sets Status.ObservedGeneration.
// Tests that do not care about rollout progress pass the same value for both.
func buildDeployment(ns, name string, desired, ready, available, updated int32, metaGeneration, observedGeneration int64, conditions []appsv1.DeploymentCondition, message string) *appsv1.Deployment {
	_ = message // message is derived from conditions by Summary()
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Generation: metaGeneration,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "console.holos.run",
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:           desired,
			ReadyReplicas:      ready,
			AvailableReplicas:  available,
			UpdatedReplicas:    updated,
			ObservedGeneration: observedGeneration,
			Conditions:         conditions,
		},
	}
}

func cond(t appsv1.DeploymentConditionType, s corev1.ConditionStatus, reason, message string) appsv1.DeploymentCondition {
	return appsv1.DeploymentCondition{Type: t, Status: s, Reason: reason, Message: message}
}

func TestCacheSummary(t *testing.T) {
	tests := []struct {
		name      string
		dep       *appsv1.Deployment
		ns        string
		lookup    string
		wantFound bool
		wantPhase consolev1.DeploymentPhase
		wantReady int32
	}{
		{
			name: "running: Available=True and ready==desired",
			dep: buildDeployment("p-alpha", "web", 3, 3, 3, 3, 1, 1, []appsv1.DeploymentCondition{
				cond(appsv1.DeploymentAvailable, corev1.ConditionTrue, "MinimumReplicasAvailable", "ok"),
				cond(appsv1.DeploymentProgressing, corev1.ConditionTrue, "NewReplicaSetAvailable", "done"),
			}, ""),
			ns:        "p-alpha",
			lookup:    "web",
			wantFound: true,
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_RUNNING,
			wantReady: 3,
		},
		{
			name: "failed: ReplicaFailure=True",
			dep: buildDeployment("p-alpha", "broken", 3, 0, 0, 0, 1, 1, []appsv1.DeploymentCondition{
				cond(appsv1.DeploymentReplicaFailure, corev1.ConditionTrue, "FailedCreate", "quota exceeded"),
				cond(appsv1.DeploymentProgressing, corev1.ConditionTrue, "ReplicaSetUpdated", "progress"),
			}, ""),
			ns:        "p-alpha",
			lookup:    "broken",
			wantFound: true,
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_FAILED,
			wantReady: 0,
		},
		{
			name: "failed: Progressing=False",
			dep: buildDeployment("p-alpha", "stalled", 3, 0, 0, 0, 1, 1, []appsv1.DeploymentCondition{
				cond(appsv1.DeploymentProgressing, corev1.ConditionFalse, "ProgressDeadlineExceeded", "timed out"),
			}, ""),
			ns:        "p-alpha",
			lookup:    "stalled",
			wantFound: true,
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_FAILED,
			wantReady: 0,
		},
		{
			name: "pending: ready<desired and progressing",
			dep: buildDeployment("p-alpha", "starting", 3, 1, 1, 2, 1, 1, []appsv1.DeploymentCondition{
				cond(appsv1.DeploymentProgressing, corev1.ConditionTrue, "ReplicaSetUpdated", "progress"),
			}, ""),
			ns:        "p-alpha",
			lookup:    "starting",
			wantFound: true,
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_PENDING,
			wantReady: 1,
		},
		{
			name:      "pending: no conditions yet, ready<desired",
			dep:       buildDeployment("p-alpha", "fresh", 2, 0, 0, 0, 1, 1, nil, ""),
			ns:        "p-alpha",
			lookup:    "fresh",
			wantFound: true,
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_PENDING,
			wantReady: 0,
		},
		{
			name: "running: scaled to zero is a steady state",
			dep: buildDeployment("p-alpha", "idle", 0, 0, 0, 0, 1, 1, []appsv1.DeploymentCondition{
				cond(appsv1.DeploymentAvailable, corev1.ConditionTrue, "MinimumReplicasAvailable", "ok"),
			}, ""),
			ns:        "p-alpha",
			lookup:    "idle",
			wantFound: true,
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_RUNNING,
			wantReady: 0,
		},
		{
			// Rollout just started: spec generation advanced (2) but the
			// controller has not yet observed it (observedGeneration=1).
			// Available=True and ready==desired can remain true from the
			// previous ReplicaSet, but the new rollout has not converged.
			// Treat as PENDING rather than falsely reporting RUNNING.
			name: "pending: observedGeneration < metadata.generation during rollout",
			dep: buildDeployment("p-alpha", "rolling", 3, 3, 3, 3, 2, 1, []appsv1.DeploymentCondition{
				cond(appsv1.DeploymentAvailable, corev1.ConditionTrue, "MinimumReplicasAvailable", "ok"),
				cond(appsv1.DeploymentProgressing, corev1.ConditionTrue, "ReplicaSetUpdated", "progress"),
			}, ""),
			ns:        "p-alpha",
			lookup:    "rolling",
			wantFound: true,
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_PENDING,
			wantReady: 3,
		},
		{
			// Rollout in progress: updatedReplicas < desired means some
			// pods still belong to the old ReplicaSet. Kubernetes may mark
			// Available=True (old pods satisfy minAvailable) while the new
			// version is still rolling out. Treat as PENDING so callers do
			// not report RUNNING for deployments that have not converged
			// on the newly desired template.
			name: "pending: updatedReplicas < desired during rollout",
			dep: buildDeployment("p-alpha", "updating", 3, 3, 3, 1, 2, 2, []appsv1.DeploymentCondition{
				cond(appsv1.DeploymentAvailable, corev1.ConditionTrue, "MinimumReplicasAvailable", "ok"),
				cond(appsv1.DeploymentProgressing, corev1.ConditionTrue, "ReplicaSetUpdated", "progress"),
			}, ""),
			ns:        "p-alpha",
			lookup:    "updating",
			wantFound: true,
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_PENDING,
			wantReady: 3,
		},
		{
			name:      "miss: unknown deployment",
			dep:       buildDeployment("p-alpha", "web", 1, 1, 1, 1, 1, 1, nil, ""),
			ns:        "p-alpha",
			lookup:    "does-not-exist",
			wantFound: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			client := fake.NewClientset(tc.dep)
			cache := New(ctx, client)

			got, ok := waitForSummary(t, cache, tc.ns, tc.lookup, tc.wantFound)
			if ok != tc.wantFound {
				t.Fatalf("Summary(%q,%q) found=%v, want %v", tc.ns, tc.lookup, ok, tc.wantFound)
			}
			if !tc.wantFound {
				if got != nil {
					t.Fatalf("Summary miss: expected nil, got %+v", got)
				}
				return
			}
			if got.Phase != tc.wantPhase {
				t.Errorf("phase = %v, want %v", got.Phase, tc.wantPhase)
			}
			if got.ReadyReplicas != tc.wantReady {
				t.Errorf("readyReplicas = %d, want %d", got.ReadyReplicas, tc.wantReady)
			}
			if got.DesiredReplicas != tc.dep.Status.Replicas {
				t.Errorf("desiredReplicas = %d, want %d", got.DesiredReplicas, tc.dep.Status.Replicas)
			}
			if got.AvailableReplicas != tc.dep.Status.AvailableReplicas {
				t.Errorf("availableReplicas = %d, want %d", got.AvailableReplicas, tc.dep.Status.AvailableReplicas)
			}
			if got.UpdatedReplicas != tc.dep.Status.UpdatedReplicas {
				t.Errorf("updatedReplicas = %d, want %d", got.UpdatedReplicas, tc.dep.Status.UpdatedReplicas)
			}
			if got.ObservedGeneration != tc.dep.Status.ObservedGeneration {
				t.Errorf("observedGeneration = %d, want %d", got.ObservedGeneration, tc.dep.Status.ObservedGeneration)
			}
		})
	}
}

func TestCacheNilClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	cache := New(ctx, nil)
	got, ok := cache.Summary("any", "thing")
	if ok || got != nil {
		t.Fatalf("expected (nil, false) for nil-client cache, got (%v, %v)", got, ok)
	}
}

// TestNewDoesNotBlockOnFailingWatch is a regression test for codex review
// finding round-1 #1: New must return immediately even when the underlying
// LIST/WATCH cannot establish (for example, missing list/watch RBAC or a
// temporarily unavailable API server). Blocking up to initialSyncTimeout
// would turn an explicitly optional feature into a guaranteed multi-second
// startup delay in exactly the failure modes the fallback is meant to
// tolerate.
func TestNewDoesNotBlockOnFailingWatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := fake.NewClientset()
	// Simulate a permanently failing LIST so the informer never syncs.
	client.PrependReactor("list", "deployments", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden: missing list/watch on deployments")
	})

	start := time.Now()
	cache := New(ctx, client)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("New blocked for %s, expected immediate return", elapsed)
	}

	// While the informer is failing to sync, Summary must report misses
	// rather than panic or block.
	if got, ok := cache.Summary("any", "thing"); ok || got != nil {
		t.Fatalf("expected (nil, false) before sync, got (%v, %v)", got, ok)
	}
}

// TestNewCancelsReflectorOnSyncTimeout is a regression test for codex
// review finding round-1 #2: when the initial sync does not complete within
// initialSyncTimeout, the package must cancel the child context driving the
// informer factory so the reflector goroutines stop retrying LIST/WATCH for
// the lifetime of the process. We assert this by counting LIST attempts
// observed after the timeout has elapsed and verifying the count plateaus.
func TestNewCancelsReflectorOnSyncTimeout(t *testing.T) {
	// Shrink the timeout to keep the test fast. We swap initialSyncTimeout
	// out via the package-level variable below.
	prev := initialSyncTimeoutForTest
	initialSyncTimeoutForTest = 200 * time.Millisecond
	t.Cleanup(func() { initialSyncTimeoutForTest = prev })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var listCount atomic.Int64
	client := fake.NewClientset()
	client.PrependReactor("list", "deployments", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		listCount.Add(1)
		return true, nil, errors.New("forbidden")
	})

	_ = New(ctx, client)

	// Wait long enough for the timeout to fire and cancellation to
	// propagate to the reflector.
	time.Sleep(600 * time.Millisecond)
	countAfterCancel := listCount.Load()

	// Wait again. If the reflector were still running it would continue
	// retrying LIST under the client-go retry backoff (sub-second after a
	// failed list with the fake client). The count must not grow.
	time.Sleep(1 * time.Second)
	countLater := listCount.Load()

	if countLater > countAfterCancel {
		t.Fatalf("reflector kept retrying LIST after sync timeout: count grew from %d to %d", countAfterCancel, countLater)
	}
}
