package deployments

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// fakeStatusCache is a test double for statuscache.Cache keyed by (ns, name).
type fakeStatusCache struct {
	entries map[string]*consolev1.DeploymentStatusSummary
}

func newFakeStatusCache() *fakeStatusCache {
	return &fakeStatusCache{entries: map[string]*consolev1.DeploymentStatusSummary{}}
}

func (f *fakeStatusCache) set(ns, name string, s *consolev1.DeploymentStatusSummary) {
	f.entries[ns+"/"+name] = s
}

func (f *fakeStatusCache) Summary(ns, name string) (*consolev1.DeploymentStatusSummary, bool) {
	s, ok := f.entries[ns+"/"+name]
	return s, ok
}

// k8sDeployment constructs a fake appsv1.Deployment for testing.
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
	cache := newFakeStatusCache()
	cache.set(ns, "my-app", &consolev1.DeploymentStatusSummary{
		Phase:             consolev1.DeploymentPhase_DEPLOYMENT_PHASE_PENDING,
		DesiredReplicas:   3,
		ReadyReplicas:     2,
		AvailableReplicas: 2,
	})
	h := newStatusHandler(fakeClient).WithStatusCache(cache)

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
	if s.Summary == nil {
		t.Fatal("summary: got nil, want non-nil (handler must populate cached summary)")
	}
	if s.Summary.Phase != consolev1.DeploymentPhase_DEPLOYMENT_PHASE_PENDING {
		t.Errorf("summary.phase: got %v, want PENDING", s.Summary.Phase)
	}
}

func TestGetDeploymentStatus_CacheMissFallsBackToLiveReplicas(t *testing.T) {
	const ns = "prj-my-project"
	// Live Deployment reports 3 desired / 3 ready / 3 available.
	dep := k8sDeployment(ns, "my-app", 3, 3, 3, nil)

	fakeClient := fake.NewClientset(dep)
	// No cache configured → cache miss path. Even so, the handler must
	// populate replica scalars from the live apps/v1.Deployment.Status so a
	// cold informer (right after startup, after the 10s sync timeout, or
	// without watch RBAC) does not render a healthy deployment as 0/0 ready.
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
	if s.Summary != nil {
		t.Errorf("summary: got %v, want nil on cache miss", s.Summary)
	}
	if s.DesiredReplicas != 3 {
		t.Errorf("desired_replicas: got %d, want 3 (fallback to live dep.Status.Replicas)", s.DesiredReplicas)
	}
	if s.ReadyReplicas != 3 {
		t.Errorf("ready_replicas: got %d, want 3 (fallback to live dep.Status.ReadyReplicas)", s.ReadyReplicas)
	}
	if s.AvailableReplicas != 3 {
		t.Errorf("available_replicas: got %d, want 3 (fallback to live dep.Status.AvailableReplicas)", s.AvailableReplicas)
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

// k8sEvent constructs a corev1.Event for testing.
func k8sEvent(ns, name, involvedName, involvedKind, eventType, reason, message, source string, count int32, first, last time.Time) *corev1.Event {
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		InvolvedObject: corev1.ObjectReference{
			Name: involvedName,
			Kind: involvedKind,
		},
		Type:           eventType,
		Reason:         reason,
		Message:        message,
		Source:         corev1.EventSource{Component: source},
		Count:          count,
		FirstTimestamp: metav1.NewTime(first),
		LastTimestamp:  metav1.NewTime(last),
	}
}

func TestGetDeploymentStatus_DeploymentEvents(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 1, 1, 1, nil)

	now := time.Now()
	earlier := now.Add(-5 * time.Minute)
	ev1 := k8sEvent(ns, "evt-1", "my-app", "Deployment", "Normal", "ScalingReplicaSet", "Scaled up replica set my-app-abc to 1", "deployment-controller", 1, earlier, earlier)
	ev2 := k8sEvent(ns, "evt-2", "my-app", "Deployment", "Warning", "FailedCreate", "Error creating: quota exceeded", "deployment-controller", 3, earlier, now)

	fakeClient := fake.NewClientset(dep, ev1, ev2)
	h := newStatusHandler(fakeClient)

	ctx := authedCtx("viewer@example.com", nil)
	resp, err := h.GetDeploymentStatus(ctx, connect.NewRequest(&consolev1.GetDeploymentStatusRequest{
		Name:    "my-app",
		Project: "my-project",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := resp.Msg.Status.Events
	if len(events) != 2 {
		t.Fatalf("deployment events: got %d, want 2", len(events))
	}

	// Events sorted by last_seen descending — most recent first.
	if events[0].Reason != "FailedCreate" {
		t.Errorf("first event reason: got %q, want %q", events[0].Reason, "FailedCreate")
	}
	if events[0].Type != "Warning" {
		t.Errorf("first event type: got %q, want %q", events[0].Type, "Warning")
	}
	if events[0].Count != 3 {
		t.Errorf("first event count: got %d, want 3", events[0].Count)
	}
	if events[0].Source != "deployment-controller" {
		t.Errorf("first event source: got %q, want %q", events[0].Source, "deployment-controller")
	}
	if events[0].InvolvedObjectName != "my-app" {
		t.Errorf("first event involved_object_name: got %q, want %q", events[0].InvolvedObjectName, "my-app")
	}

	if events[1].Reason != "ScalingReplicaSet" {
		t.Errorf("second event reason: got %q, want %q", events[1].Reason, "ScalingReplicaSet")
	}
	if events[1].Type != "Normal" {
		t.Errorf("second event type: got %q, want %q", events[1].Type, "Normal")
	}
}

func TestGetDeploymentStatus_PodEvents(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 1, 0, 0, nil)
	pod := k8sPod(ns, "my-app-abc", "my-app", corev1.PodPending, false, 0)

	now := time.Now()
	podEvent := k8sEvent(ns, "pod-evt-1", "my-app-abc", "Pod", "Warning", "FailedScheduling", "0/3 nodes are available", "default-scheduler", 1, now, now)

	fakeClient := fake.NewClientset(dep, pod, podEvent)
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
	podEvents := pods[0].Events
	if len(podEvents) != 1 {
		t.Fatalf("pod events: got %d, want 1", len(podEvents))
	}
	if podEvents[0].Reason != "FailedScheduling" {
		t.Errorf("pod event reason: got %q, want %q", podEvents[0].Reason, "FailedScheduling")
	}
	if podEvents[0].InvolvedObjectName != "my-app-abc" {
		t.Errorf("pod event involved_object_name: got %q, want %q", podEvents[0].InvolvedObjectName, "my-app-abc")
	}
}

func TestGetDeploymentStatus_ContainerWaiting(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 1, 0, 0, nil)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-app-abc",
			Namespace: ns,
			Labels:    map[string]string{"app": "my-app"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "nginx:latest"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					Image:        "nginx:latest",
					Ready:        false,
					RestartCount: 2,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "ImagePullBackOff",
							Message: "Back-off pulling image",
						},
					},
				},
			},
		},
	}

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
	cs := pods[0].ContainerStatuses
	if len(cs) != 1 {
		t.Fatalf("container_statuses: got %d, want 1", len(cs))
	}
	if cs[0].Name != "app" {
		t.Errorf("container name: got %q, want %q", cs[0].Name, "app")
	}
	if cs[0].State != "waiting" {
		t.Errorf("container state: got %q, want %q", cs[0].State, "waiting")
	}
	if cs[0].Reason != "ImagePullBackOff" {
		t.Errorf("container reason: got %q, want %q", cs[0].Reason, "ImagePullBackOff")
	}
	if cs[0].Message != "Back-off pulling image" {
		t.Errorf("container message: got %q, want %q", cs[0].Message, "Back-off pulling image")
	}
	if cs[0].Image != "nginx:latest" {
		t.Errorf("container image: got %q, want %q", cs[0].Image, "nginx:latest")
	}
	if cs[0].Ready {
		t.Error("container ready: got true, want false")
	}
	if cs[0].RestartCount != 2 {
		t.Errorf("container restart_count: got %d, want 2", cs[0].RestartCount)
	}
}

func TestGetDeploymentStatus_ContainerRunning(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 1, 1, 1, nil)

	startedAt := time.Now().Add(-10 * time.Minute)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-app-abc",
			Namespace: ns,
			Labels:    map[string]string{"app": "my-app"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "nginx:1.25"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					Image:        "nginx:1.25",
					Ready:        true,
					RestartCount: 0,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.NewTime(startedAt),
						},
					},
				},
			},
		},
	}

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

	cs := resp.Msg.Status.Pods[0].ContainerStatuses
	if len(cs) != 1 {
		t.Fatalf("container_statuses: got %d, want 1", len(cs))
	}
	if cs[0].State != "running" {
		t.Errorf("container state: got %q, want %q", cs[0].State, "running")
	}
	if cs[0].Ready != true {
		t.Error("container ready: got false, want true")
	}
	if cs[0].StartedAt == nil {
		t.Fatal("container started_at: got nil, want non-nil")
	}
	if cs[0].StartedAt.AsTime().Unix() != startedAt.Unix() {
		t.Errorf("container started_at: got %v, want %v", cs[0].StartedAt.AsTime(), startedAt)
	}
}

func TestGetDeploymentStatus_ContainerTerminated(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 1, 0, 0, nil)

	startedAt := time.Now().Add(-1 * time.Hour)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-app-abc",
			Namespace: ns,
			Labels:    map[string]string{"app": "my-app"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "myapp:v1"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					Image:        "myapp:v1",
					Ready:        false,
					RestartCount: 5,
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:    "OOMKilled",
							Message:   "Container exceeded memory limit",
							ExitCode:  137,
							StartedAt: metav1.NewTime(startedAt),
						},
					},
				},
			},
		},
	}

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

	cs := resp.Msg.Status.Pods[0].ContainerStatuses
	if len(cs) != 1 {
		t.Fatalf("container_statuses: got %d, want 1", len(cs))
	}
	if cs[0].State != "terminated" {
		t.Errorf("container state: got %q, want %q", cs[0].State, "terminated")
	}
	if cs[0].Reason != "OOMKilled" {
		t.Errorf("container reason: got %q, want %q", cs[0].Reason, "OOMKilled")
	}
	if cs[0].Message != "Container exceeded memory limit" {
		t.Errorf("container message: got %q, want %q", cs[0].Message, "Container exceeded memory limit")
	}
	if cs[0].RestartCount != 5 {
		t.Errorf("container restart_count: got %d, want 5", cs[0].RestartCount)
	}
	if cs[0].StartedAt == nil {
		t.Fatal("container started_at: got nil, want non-nil")
	}
}

func TestGetDeploymentStatus_NoEvents(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 1, 1, 1, nil)
	pod := k8sPod(ns, "my-app-abc", "my-app", corev1.PodRunning, true, 0)

	// No events seeded.
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

	if len(resp.Msg.Status.Events) != 0 {
		t.Errorf("deployment events: got %d, want 0", len(resp.Msg.Status.Events))
	}
	if len(resp.Msg.Status.Pods[0].Events) != 0 {
		t.Errorf("pod events: got %d, want 0", len(resp.Msg.Status.Pods[0].Events))
	}
}

func TestGetDeploymentStatus_EventWithZeroTimestamp(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 1, 1, 1, nil)

	now := time.Now()
	// ev1 has timestamps, ev2 has zero timestamps (LastSeen will be nil).
	ev1 := k8sEvent(ns, "evt-1", "my-app", "Deployment", "Normal", "ScalingReplicaSet", "Scaled up", "deployment-controller", 1, now, now)
	ev2 := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "evt-2", Namespace: ns},
		InvolvedObject: corev1.ObjectReference{
			Name: "my-app",
			Kind: "Deployment",
		},
		Type:    "Normal",
		Reason:  "Scheduled",
		Message: "Successfully assigned",
		Source:  corev1.EventSource{Component: "default-scheduler"},
		Count:   1,
		// FirstTimestamp and LastTimestamp deliberately left at zero value.
	}

	fakeClient := fake.NewClientset(dep, ev1, ev2)
	h := newStatusHandler(fakeClient)

	ctx := authedCtx("viewer@example.com", nil)
	resp, err := h.GetDeploymentStatus(ctx, connect.NewRequest(&consolev1.GetDeploymentStatusRequest{
		Name:    "my-app",
		Project: "my-project",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := resp.Msg.Status.Events
	if len(events) != 2 {
		t.Fatalf("deployment events: got %d, want 2", len(events))
	}
	// Event with timestamp should sort first; event without timestamp sorts last.
	if events[0].Reason != "ScalingReplicaSet" {
		t.Errorf("first event reason: got %q, want %q", events[0].Reason, "ScalingReplicaSet")
	}
	if events[1].Reason != "Scheduled" {
		t.Errorf("second event reason: got %q, want %q", events[1].Reason, "Scheduled")
	}
	if events[1].LastSeen != nil {
		t.Errorf("second event last_seen: got non-nil, want nil")
	}
}

func TestGetDeploymentStatus_InitContainerStatuses(t *testing.T) {
	const ns = "prj-my-project"
	dep := k8sDeployment(ns, "my-app", 1, 0, 0, nil)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-app-abc",
			Namespace: ns,
			Labels:    map[string]string{"app": "my-app"},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init", Image: "busybox:latest"}},
			Containers:     []corev1.Container{{Name: "app", Image: "nginx:latest"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "init",
					Image:        "busybox:latest",
					Ready:        false,
					RestartCount: 0,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "PodInitializing",
							Message: "Init container is starting",
						},
					},
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "app",
					Image: "nginx:latest",
					Ready: false,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "PodInitializing",
						},
					},
				},
			},
		},
	}

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

	cs := resp.Msg.Status.Pods[0].ContainerStatuses
	if len(cs) != 2 {
		t.Fatalf("container_statuses: got %d, want 2 (init + app)", len(cs))
	}
	// Init container should appear first.
	if cs[0].Name != "init" {
		t.Errorf("first container name: got %q, want %q", cs[0].Name, "init")
	}
	if cs[1].Name != "app" {
		t.Errorf("second container name: got %q, want %q", cs[1].Name, "app")
	}
}

func TestGetDeploymentStatusSummary(t *testing.T) {
	const (
		ns      = "prj-my-project"
		project = "my-project"
		name    = "my-app"
	)

	type tc struct {
		desc       string
		ctx        context.Context
		cachedSum  *consolev1.DeploymentStatusSummary
		req        *consolev1.GetDeploymentStatusSummaryRequest
		wantCode   connect.Code // zero means no error expected
		wantPhase  consolev1.DeploymentPhase
		wantReady  int32
	}
	cases := []tc{
		{
			desc: "populated summary (RUNNING)",
			ctx:  authedCtx("viewer@example.com", nil),
			cachedSum: &consolev1.DeploymentStatusSummary{
				Phase:           consolev1.DeploymentPhase_DEPLOYMENT_PHASE_RUNNING,
				ReadyReplicas:   3,
				DesiredReplicas: 3,
			},
			req:       &consolev1.GetDeploymentStatusSummaryRequest{Project: project, Name: name},
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_RUNNING,
			wantReady: 3,
		},
		{
			desc:      "cache miss returns UNSPECIFIED",
			ctx:       authedCtx("viewer@example.com", nil),
			cachedSum: nil,
			req:       &consolev1.GetDeploymentStatusSummaryRequest{Project: project, Name: name},
			wantPhase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_UNSPECIFIED,
		},
		{
			desc:     "unauthenticated is rejected",
			ctx:      context.Background(),
			req:      &consolev1.GetDeploymentStatusSummaryRequest{Project: project, Name: name},
			wantCode: connect.CodeUnauthenticated,
		},
		{
			desc:     "unauthorized user is denied",
			ctx:      authedCtx("nobody@example.com", nil),
			req:      &consolev1.GetDeploymentStatusSummaryRequest{Project: project, Name: name},
			wantCode: connect.CodePermissionDenied,
		},
		{
			desc:     "empty project is rejected",
			ctx:      authedCtx("viewer@example.com", nil),
			req:      &consolev1.GetDeploymentStatusSummaryRequest{Project: "", Name: name},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			desc:     "empty name is rejected",
			ctx:      authedCtx("viewer@example.com", nil),
			req:      &consolev1.GetDeploymentStatusSummaryRequest{Project: project, Name: ""},
			wantCode: connect.CodeInvalidArgument,
		},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			fakeClient := fake.NewClientset()
			h := newStatusHandler(fakeClient)
			if c.cachedSum != nil {
				cache := newFakeStatusCache()
				cache.set(ns, name, c.cachedSum)
				h = h.WithStatusCache(cache)
			}

			resp, err := h.GetDeploymentStatusSummary(c.ctx, connect.NewRequest(c.req))
			if c.wantCode != 0 {
				if err == nil {
					t.Fatalf("expected error with code %v, got nil", c.wantCode)
				}
				if connect.CodeOf(err) != c.wantCode {
					t.Errorf("code: got %v, want %v", connect.CodeOf(err), c.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := resp.Msg.Summary
			if got == nil {
				t.Fatal("summary: got nil, want non-nil (miss must still return a summary)")
			}
			if got.Phase != c.wantPhase {
				t.Errorf("phase: got %v, want %v", got.Phase, c.wantPhase)
			}
			if got.ReadyReplicas != c.wantReady {
				t.Errorf("ready_replicas: got %d, want %d", got.ReadyReplicas, c.wantReady)
			}
		})
	}
}
