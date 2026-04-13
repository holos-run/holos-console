package statuscache

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// buildDeployment returns an apps/v1 Deployment with the given status fields
// and conditions wired up for test table entries.
func buildDeployment(ns, name string, desired, ready, available, updated int32, generation int64, conditions []appsv1.DeploymentCondition, message string) *appsv1.Deployment {
	_ = message // message is derived from conditions by Summary()
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Generation: generation,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "console.holos.run",
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:           desired,
			ReadyReplicas:      ready,
			AvailableReplicas:  available,
			UpdatedReplicas:    updated,
			ObservedGeneration: generation,
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
			dep: buildDeployment("p-alpha", "web", 3, 3, 3, 3, 1, []appsv1.DeploymentCondition{
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
			dep: buildDeployment("p-alpha", "broken", 3, 0, 0, 0, 1, []appsv1.DeploymentCondition{
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
			dep: buildDeployment("p-alpha", "stalled", 3, 0, 0, 0, 1, []appsv1.DeploymentCondition{
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
			dep: buildDeployment("p-alpha", "starting", 3, 1, 1, 2, 1, []appsv1.DeploymentCondition{
				cond(appsv1.DeploymentProgressing, corev1.ConditionTrue, "ReplicaSetUpdated", "progress"),
			}, ""),
			ns:        "p-alpha",
			lookup:    "starting",
			wantFound: true,
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_PENDING,
			wantReady: 1,
		},
		{
			name: "pending: no conditions yet, ready<desired",
			dep: buildDeployment("p-alpha", "fresh", 2, 0, 0, 0, 1, nil, ""),
			ns:        "p-alpha",
			lookup:    "fresh",
			wantFound: true,
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_PENDING,
			wantReady: 0,
		},
		{
			name: "running: scaled to zero is a steady state",
			dep: buildDeployment("p-alpha", "idle", 0, 0, 0, 0, 1, []appsv1.DeploymentCondition{
				cond(appsv1.DeploymentAvailable, corev1.ConditionTrue, "MinimumReplicasAvailable", "ok"),
			}, ""),
			ns:        "p-alpha",
			lookup:    "idle",
			wantFound: true,
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_RUNNING,
			wantReady: 0,
		},
		{
			name:      "miss: unknown deployment",
			dep:       buildDeployment("p-alpha", "web", 1, 1, 1, 1, 1, nil, ""),
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
			cache, err := New(ctx, client)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			got, ok := cache.Summary(tc.ns, tc.lookup)
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

	cache, err := New(ctx, nil)
	if err != nil {
		t.Fatalf("New(nil): %v", err)
	}
	got, ok := cache.Summary("any", "thing")
	if ok || got != nil {
		t.Fatalf("expected (nil, false) for nil-client cache, got (%v, %v)", got, ok)
	}
}
