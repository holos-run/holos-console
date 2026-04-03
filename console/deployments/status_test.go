package deployments

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// ownerRef returns an owner reference pointing to the named deployment ConfigMap
// for use in pod metadata (mirrors what a real Deployment controller would set,
// but simplified to just the deployment name via label for these tests).
func k8sDeployment(ns, name string, desired, ready, available int32, conds []appsv1.DeploymentCondition) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:          desired,
			ReadyReplicas:     ready,
			AvailableReplicas: available,
			Conditions:        conds,
		},
	}
}

func k8sPod(ns, name, deploymentName string, phase corev1.PodPhase, ready bool, restarts int32) *corev1.Pod {
	var conditions []corev1.PodCondition
	if ready {
		conditions = []corev1.PodCondition{
			{Type: corev1.PodReady, Status: corev1.ConditionTrue},
		}
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{"app": deploymentName},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
		Status: corev1.PodStatus{
			Phase:      phase,
			Conditions: conditions,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					Ready:        ready,
					RestartCount: restarts,
				},
			},
		},
	}
}

func newStatusHandler(fakeClient *fake.Clientset) *Handler {
	k8s := NewK8sClient(fakeClient, testResolver())
	pr := &stubProjectResolver{
		users: map[string]string{"viewer@example.com": "viewer"},
		roles: map[string]string{},
	}
	return NewHandler(k8s, pr, nil, nil, nil, nil)
}

func TestGetDeploymentStatus_ReplicaCounts(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 3, 2, 2, nil)
	pod1 := k8sPod(ns, "my-app-1", "my-app", corev1.PodRunning, true, 0)
	pod2 := k8sPod(ns, "my-app-2", "my-app", corev1.PodRunning, false, 1)

	fakeClient := fake.NewClientset(dep, pod1, pod2)
	h := newStatusHandler(fakeClient)

	ctx := authedCtx("viewer@example.com", nil)
	resp, err := h.GetDeploymentStatus(ctx, connect.NewRequest(&consolev1.GetDeploymentStatusRequest{
		Name:    "my-app",
		Project: "my-project",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := resp.Msg.Status
	if s.DesiredReplicas != 3 {
		t.Errorf("desired_replicas: got %d, want 3", s.DesiredReplicas)
	}
	if s.ReadyReplicas != 2 {
		t.Errorf("ready_replicas: got %d, want 2", s.ReadyReplicas)
	}
	if s.AvailableReplicas != 2 {
		t.Errorf("available_replicas: got %d, want 2", s.AvailableReplicas)
	}
	if len(s.Pods) != 2 {
		t.Errorf("pods: got %d, want 2", len(s.Pods))
	}
}

func TestGetDeploymentStatus_Conditions(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 1, 1, 1, []appsv1.DeploymentCondition{
		{
			Type:    appsv1.DeploymentAvailable,
			Status:  corev1.ConditionTrue,
			Reason:  "MinimumReplicasAvailable",
			Message: "Deployment has minimum availability.",
		},
	})

	fakeClient := fake.NewClientset(dep)
	h := newStatusHandler(fakeClient)

	ctx := authedCtx("viewer@example.com", nil)
	resp, err := h.GetDeploymentStatus(ctx, connect.NewRequest(&consolev1.GetDeploymentStatusRequest{
		Name:    "my-app",
		Project: "my-project",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	conds := resp.Msg.Status.Conditions
	if len(conds) != 1 {
		t.Fatalf("conditions: got %d, want 1", len(conds))
	}
	if conds[0].Type != "Available" {
		t.Errorf("condition type: got %q, want %q", conds[0].Type, "Available")
	}
	if conds[0].Status != "True" {
		t.Errorf("condition status: got %q, want %q", conds[0].Status, "True")
	}
}

func TestGetDeploymentStatus_PodStatus(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 1, 0, 0, nil)
	pod := k8sPod(ns, "my-app-abc", "my-app", corev1.PodRunning, false, 5)

	fakeClient := fake.NewClientset(dep, pod)
	h := newStatusHandler(fakeClient)

	ctx := authedCtx("viewer@example.com", nil)
	resp, err := h.GetDeploymentStatus(ctx, connect.NewRequest(&consolev1.GetDeploymentStatusRequest{
		Name:    "my-app",
		Project: "my-project",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pods := resp.Msg.Status.Pods
	if len(pods) != 1 {
		t.Fatalf("pods: got %d, want 1", len(pods))
	}
	p := pods[0]
	if p.Name != "my-app-abc" {
		t.Errorf("pod name: got %q, want %q", p.Name, "my-app-abc")
	}
	if p.Phase != "Running" {
		t.Errorf("pod phase: got %q, want %q", p.Phase, "Running")
	}
	if p.Ready {
		t.Errorf("pod ready: got true, want false")
	}
	if p.RestartCount != 5 {
		t.Errorf("restart count: got %d, want 5", p.RestartCount)
	}
}

func TestGetDeploymentStatus_UnauthenticatedDenied(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 1, 1, 1, nil)
	fakeClient := fake.NewClientset(dep)
	h := newStatusHandler(fakeClient)

	_, err := h.GetDeploymentStatus(context.Background(), connect.NewRequest(&consolev1.GetDeploymentStatusRequest{
		Name:    "my-app",
		Project: "my-project",
	}))
	if err == nil {
		t.Fatal("expected error for unauthenticated request, got nil")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("expected Unauthenticated error, got %v", err)
	}
}

func TestGetDeploymentStatus_ViewerCanRead(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 1, 1, 1, nil)
	fakeClient := fake.NewClientset(dep)
	h := newStatusHandler(fakeClient)

	ctx := authedCtx("viewer@example.com", nil)
	_, err := h.GetDeploymentStatus(ctx, connect.NewRequest(&consolev1.GetDeploymentStatusRequest{
		Name:    "my-app",
		Project: "my-project",
	}))
	if err != nil {
		t.Errorf("viewer should be able to read status: %v", err)
	}
}
