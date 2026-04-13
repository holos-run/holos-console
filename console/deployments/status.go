package deployments

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// GetDeploymentStatus returns the live status of the K8s Deployment and its pods.
func (h *Handler) GetDeploymentStatus(
	ctx context.Context,
	req *connect.Request[consolev1.GetDeploymentStatusRequest],
) (*connect.Response[consolev1.GetDeploymentStatusResponse], error) {
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

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsRead); err != nil {
		return nil, err
	}

	ns := h.k8s.Resolver.ProjectNamespace(project)

	dep, err := h.k8s.client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Build conditions from the K8s Deployment conditions.
	conditions := make([]*consolev1.DeploymentCondition, 0, len(dep.Status.Conditions))
	for _, c := range dep.Status.Conditions {
		conditions = append(conditions, &consolev1.DeploymentCondition{
			Type:    string(c.Type),
			Status:  string(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
		})
	}

	// Fetch pods matching the deployment's label selector.
	selector := dep.Spec.Selector
	var labelSelector string
	if selector != nil {
		parts := make([]string, 0, len(selector.MatchLabels))
		for k, v := range selector.MatchLabels {
			parts = append(parts, k+"="+v)
		}
		if len(parts) > 0 {
			labelSelector = parts[0]
			for i := 1; i < len(parts); i++ {
				labelSelector += "," + parts[i]
			}
		}
	}

	podList, err := h.k8s.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Fetch deployment-level events using field selectors. In production the API
	// server filters server-side. The fake K8s client ignores field selectors and
	// returns all events, so tests seed only relevant events for correctness.
	depEventList, err := h.k8s.client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.name=" + name + ",involvedObject.kind=Deployment",
	})
	if err != nil {
		return nil, mapK8sError(err)
	}
	depEvents := mapEvents(depEventList.Items)

	pods := make([]*consolev1.PodStatus, 0, len(podList.Items))
	for _, pod := range podList.Items {
		ready := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				ready = true
				break
			}
		}
		var restartCount int32
		for _, cs := range pod.Status.ContainerStatuses {
			restartCount += cs.RestartCount
		}

		// Map container statuses: init containers first, then regular containers.
		containerStatuses := mapContainerStatuses(pod.Status.InitContainerStatuses)
		containerStatuses = append(containerStatuses, mapContainerStatuses(pod.Status.ContainerStatuses)...)

		// Fetch pod-level events using field selectors.
		podEventList, err := h.k8s.client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
			FieldSelector: "involvedObject.name=" + pod.Name + ",involvedObject.kind=Pod",
		})
		if err != nil {
			return nil, mapK8sError(err)
		}
		podEvents := mapEvents(podEventList.Items)

		pods = append(pods, &consolev1.PodStatus{
			Name:              pod.Name,
			Phase:             string(pod.Status.Phase),
			Ready:             ready,
			RestartCount:      restartCount,
			ContainerStatuses: containerStatuses,
			Events:            podEvents,
		})
	}

	slog.InfoContext(ctx, "deployment status read",
		slog.String("action", "deployment_status_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
	)

	return connect.NewResponse(&consolev1.GetDeploymentStatusResponse{
		Status: &consolev1.DeploymentStatus{
			ReadyReplicas:     dep.Status.ReadyReplicas,
			DesiredReplicas:   dep.Status.Replicas,
			AvailableReplicas: dep.Status.AvailableReplicas,
			Conditions:        conditions,
			Pods:              pods,
			Events:            depEvents,
		},
	}), nil
}

// mapEvents converts K8s events to proto Event messages, sorted by last_seen
// descending (most recent first).
func mapEvents(k8sEvents []corev1.Event) []*consolev1.Event {
	if len(k8sEvents) == 0 {
		return nil
	}
	events := make([]*consolev1.Event, 0, len(k8sEvents))
	for _, ev := range k8sEvents {
		protoEvent := &consolev1.Event{
			Type:               ev.Type,
			Reason:             ev.Reason,
			Message:            ev.Message,
			Source:             ev.Source.Component,
			Count:              ev.Count,
			InvolvedObjectName: ev.InvolvedObject.Name,
		}
		if !ev.FirstTimestamp.IsZero() {
			protoEvent.FirstSeen = timestamppb.New(ev.FirstTimestamp.Time)
		}
		if !ev.LastTimestamp.IsZero() {
			protoEvent.LastSeen = timestamppb.New(ev.LastTimestamp.Time)
		}
		events = append(events, protoEvent)
	}
	// Sort by last_seen descending (most recent first).
	sort.Slice(events, func(i, j int) bool {
		ti := events[i].LastSeen.AsTime()
		tj := events[j].LastSeen.AsTime()
		return ti.After(tj)
	})
	return events
}

// mapContainerStatuses converts K8s ContainerStatus slices to proto
// ContainerStatus messages.
func mapContainerStatuses(k8sStatuses []corev1.ContainerStatus) []*consolev1.ContainerStatus {
	result := make([]*consolev1.ContainerStatus, 0, len(k8sStatuses))
	for _, cs := range k8sStatuses {
		proto := &consolev1.ContainerStatus{
			Name:         cs.Name,
			Image:        cs.Image,
			Ready:        cs.Ready,
			RestartCount: cs.RestartCount,
		}
		switch {
		case cs.State.Waiting != nil:
			proto.State = "waiting"
			proto.Reason = cs.State.Waiting.Reason
			proto.Message = cs.State.Waiting.Message
		case cs.State.Running != nil:
			proto.State = "running"
			if !cs.State.Running.StartedAt.IsZero() {
				proto.StartedAt = timestamppb.New(cs.State.Running.StartedAt.Time)
			}
		case cs.State.Terminated != nil:
			proto.State = "terminated"
			proto.Reason = cs.State.Terminated.Reason
			proto.Message = cs.State.Terminated.Message
			if !cs.State.Terminated.StartedAt.IsZero() {
				proto.StartedAt = timestamppb.New(cs.State.Terminated.StartedAt.Time)
			}
		}
		result = append(result, proto)
	}
	return result
}
