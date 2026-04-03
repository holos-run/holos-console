package deployments

import (
	"context"
	"io"
	"strings"
	"testing"

	"connectrpc.com/connect"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// logsK8sDeployment creates a minimal apps/v1.Deployment with a label selector.
func logsK8sDeployment(ns, name string) *appsv1.Deployment {
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
	}
}

// logsPod creates a pod with a matching label for the given deployment.
func logsPod(ns, podName, deploymentName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
			Labels:    map[string]string{"app": deploymentName},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

// newLogsHandlerWithReader creates a Handler with a stub log reader.
func newLogsHandlerWithReader(fakeClient *fake.Clientset, reader LogReader) *Handler {
	k8s := NewK8sClient(fakeClient, testResolver())
	pr := &stubProjectResolver{
		users: map[string]string{"viewer@example.com": "viewer"},
		roles: map[string]string{},
	}
	return &Handler{
		k8s:             k8s,
		projectResolver: pr,
		logReader:       reader,
	}
}

// TestGetDeploymentLogs_ReturnsPodLogs verifies that logs are returned for matching pods.
func TestGetDeploymentLogs_ReturnsPodLogs(t *testing.T) {
	const ns = "prj-my-project"
	dep := logsK8sDeployment(ns, "my-app")
	pod := logsPod(ns, "my-app-xyz", "my-app")

	fakeClient := fake.NewClientset(dep, pod)
	stubReader := &stubLogReader{logs: map[string]string{"my-app-xyz": "hello from pod\n"}}
	h := newLogsHandlerWithReader(fakeClient, stubReader)

	ctx := authedCtx("viewer@example.com", nil)
	resp, err := h.GetDeploymentLogs(ctx, connect.NewRequest(&consolev1.GetDeploymentLogsRequest{
		Name:    "my-app",
		Project: "my-project",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Msg.Logs, "hello from pod") {
		t.Errorf("logs should contain pod output, got: %q", resp.Msg.Logs)
	}
	if !strings.Contains(resp.Msg.Logs, "my-app-xyz") {
		t.Errorf("logs should be prefixed with pod name, got: %q", resp.Msg.Logs)
	}
}

// TestGetDeploymentLogs_TailLines verifies that tail_lines is forwarded.
func TestGetDeploymentLogs_TailLines(t *testing.T) {
	const ns = "prj-my-project"
	dep := logsK8sDeployment(ns, "my-app")
	pod := logsPod(ns, "my-app-xyz", "my-app")

	fakeClient := fake.NewClientset(dep, pod)
	stubReader := &stubLogReader{logs: map[string]string{"my-app-xyz": "line1\nline2\n"}}
	h := newLogsHandlerWithReader(fakeClient, stubReader)

	ctx := authedCtx("viewer@example.com", nil)
	_, err := h.GetDeploymentLogs(ctx, connect.NewRequest(&consolev1.GetDeploymentLogsRequest{
		Name:      "my-app",
		Project:   "my-project",
		TailLines: 50,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stubReader.lastTailLines != 50 {
		t.Errorf("tail_lines: got %d, want 50", stubReader.lastTailLines)
	}
}

// TestGetDeploymentLogs_DefaultTailLines verifies that tail_lines defaults to 100.
func TestGetDeploymentLogs_DefaultTailLines(t *testing.T) {
	const ns = "prj-my-project"
	dep := logsK8sDeployment(ns, "my-app")
	pod := logsPod(ns, "my-app-xyz", "my-app")

	fakeClient := fake.NewClientset(dep, pod)
	stubReader := &stubLogReader{logs: map[string]string{"my-app-xyz": "line1\n"}}
	h := newLogsHandlerWithReader(fakeClient, stubReader)

	ctx := authedCtx("viewer@example.com", nil)
	_, err := h.GetDeploymentLogs(ctx, connect.NewRequest(&consolev1.GetDeploymentLogsRequest{
		Name:    "my-app",
		Project: "my-project",
		// TailLines not set — should default to 100.
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stubReader.lastTailLines != 100 {
		t.Errorf("default tail_lines: got %d, want 100", stubReader.lastTailLines)
	}
}

// TestGetDeploymentLogs_Previous verifies the previous flag is forwarded.
func TestGetDeploymentLogs_Previous(t *testing.T) {
	const ns = "prj-my-project"
	dep := logsK8sDeployment(ns, "my-app")
	pod := logsPod(ns, "my-app-xyz", "my-app")

	fakeClient := fake.NewClientset(dep, pod)
	stubReader := &stubLogReader{logs: map[string]string{"my-app-xyz": "crash log\n"}}
	h := newLogsHandlerWithReader(fakeClient, stubReader)

	ctx := authedCtx("viewer@example.com", nil)
	_, err := h.GetDeploymentLogs(ctx, connect.NewRequest(&consolev1.GetDeploymentLogsRequest{
		Name:     "my-app",
		Project:  "my-project",
		Previous: true,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stubReader.lastPrevious {
		t.Error("previous flag should have been forwarded to log reader")
	}
}

// TestGetDeploymentLogs_UnauthenticatedDenied verifies unauthenticated access is rejected.
func TestGetDeploymentLogs_UnauthenticatedDenied(t *testing.T) {
	fakeClient := fake.NewClientset()
	k8s := NewK8sClient(fakeClient, testResolver())
	pr := &stubProjectResolver{users: map[string]string{}}
	h := &Handler{k8s: k8s, projectResolver: pr}

	_, err := h.GetDeploymentLogs(context.Background(), connect.NewRequest(&consolev1.GetDeploymentLogsRequest{
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

// TestGetDeploymentLogs_ViewerCanRead verifies viewer role can access logs.
func TestGetDeploymentLogs_ViewerCanRead(t *testing.T) {
	const ns = "prj-my-project"
	dep := logsK8sDeployment(ns, "my-app")
	pod := logsPod(ns, "my-app-xyz", "my-app")

	fakeClient := fake.NewClientset(dep, pod)
	stubReader := &stubLogReader{logs: map[string]string{"my-app-xyz": "ok\n"}}
	h := newLogsHandlerWithReader(fakeClient, stubReader)

	ctx := authedCtx("viewer@example.com", nil)
	_, err := h.GetDeploymentLogs(ctx, connect.NewRequest(&consolev1.GetDeploymentLogsRequest{
		Name:    "my-app",
		Project: "my-project",
	}))
	if err != nil {
		t.Errorf("viewer should be able to read logs: %v", err)
	}
}

// stubLogReader implements LogReader for tests.
type stubLogReader struct {
	logs          map[string]string // podName → log output
	lastTailLines int64
	lastPrevious  bool
}

func (s *stubLogReader) GetPodLogs(_ context.Context, _, podName, _ string, tailLines int64, previous bool) (io.ReadCloser, error) {
	s.lastTailLines = tailLines
	s.lastPrevious = previous
	content := s.logs[podName]
	return io.NopCloser(strings.NewReader(content)), nil
}
