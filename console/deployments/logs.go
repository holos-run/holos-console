package deployments

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

const defaultTailLines = 100

// LogReader reads container logs for a pod.
type LogReader interface {
	GetPodLogs(ctx context.Context, namespace, podName, container string, tailLines int64, previous bool) (io.ReadCloser, error)
}

// k8sLogReader implements LogReader using the real Kubernetes client.
type k8sLogReader struct {
	k8s *K8sClient
}

func (r *k8sLogReader) GetPodLogs(ctx context.Context, namespace, podName, container string, tailLines int64, previous bool) (io.ReadCloser, error) {
	opts := &corev1.PodLogOptions{
		Container: container,
		TailLines: &tailLines,
		Previous:  previous,
	}
	return r.k8s.client.CoreV1().Pods(namespace).GetLogs(podName, opts).Stream(ctx)
}

// GetDeploymentLogs returns recent container logs for a deployment's pods.
func (h *Handler) GetDeploymentLogs(
	ctx context.Context,
	req *connect.Request[consolev1.GetDeploymentLogsRequest],
) (*connect.Response[consolev1.GetDeploymentLogsResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	rk8s := h.requestK8s(ctx)
	ns := rk8s.Resolver.ProjectNamespace(project)

	// Find pods matching the deployment's label selector.
	dep, err := rk8s.client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, mapK8sError(err)
	}

	var labelSelector string
	if dep.Spec.Selector != nil {
		parts := make([]string, 0, len(dep.Spec.Selector.MatchLabels))
		for k, v := range dep.Spec.Selector.MatchLabels {
			parts = append(parts, k+"="+v)
		}
		if len(parts) > 0 {
			labelSelector = parts[0]
			for i := 1; i < len(parts); i++ {
				labelSelector += "," + parts[i]
			}
		}
	}

	podList, err := rk8s.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, mapK8sError(err)
	}

	tailLines := int64(req.Msg.TailLines)
	if tailLines <= 0 {
		tailLines = defaultTailLines
	}

	logReader := h.logReader
	if logReader == nil {
		logReader = &k8sLogReader{k8s: h.k8s}
	}

	var buf bytes.Buffer
	for _, pod := range podList.Items {
		rc, logErr := logReader.GetPodLogs(ctx, ns, pod.Name, req.Msg.Container, tailLines, req.Msg.Previous)
		if logErr != nil {
			slog.WarnContext(ctx, "failed to get pod logs",
				slog.String("pod", pod.Name),
				slog.Any("error", logErr),
			)
			continue
		}
		fmt.Fprintf(&buf, "=== %s ===\n", pod.Name)
		if _, copyErr := io.Copy(&buf, rc); copyErr != nil {
			slog.WarnContext(ctx, "failed to read pod logs",
				slog.String("pod", pod.Name),
				slog.Any("error", copyErr),
			)
		}
		rc.Close()
		fmt.Fprintln(&buf)
	}

	slog.InfoContext(ctx, "deployment logs read",
		slog.String("action", "deployment_logs_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
	)

	return connect.NewResponse(&consolev1.GetDeploymentLogsResponse{
		Logs: buf.String(),
	}), nil
}
