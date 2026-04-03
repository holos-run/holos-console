package deployments

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
		pods = append(pods, &consolev1.PodStatus{
			Name:         pod.Name,
			Phase:        string(pod.Status.Phase),
			Ready:        ready,
			RestartCount: restartCount,
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
		},
	}), nil
}
