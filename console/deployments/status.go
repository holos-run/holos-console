package deployments

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// summaryFromCache returns the cached DeploymentStatusSummary for the given
// (namespace, name) if a status cache has been configured and the informer
// knows about the Deployment. Returns (nil, false) on a cache miss or when no
// cache is configured so callers can degrade to UNSPECIFIED without failing.
func (h *Handler) summaryFromCache(ns, name string) (*consolev1.DeploymentStatusSummary, bool) {
	if h.statusCache == nil {
		return nil, false
	}
	return h.statusCache.Summary(ns, name)
}

// GetDeploymentStatusSummary returns the lightweight cached status summary for
// a single deployment. It reads from the in-process informer cache only, never
// issuing a direct K8s API call. A cache miss (informer still catching up or
// no data for this deployment) is returned as a summary with phase
// DEPLOYMENT_PHASE_UNSPECIFIED and zero replica counts so callers can render a
// stable placeholder without branching on a nil summary.
func (h *Handler) GetDeploymentStatusSummary(
	ctx context.Context,
	req *connect.Request[consolev1.GetDeploymentStatusSummaryRequest],
) (*connect.Response[consolev1.GetDeploymentStatusSummaryResponse], error) {
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
	summary, ok := h.summaryFromCache(ns, name)
	if !ok {
		summary = &consolev1.DeploymentStatusSummary{
			Phase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_UNSPECIFIED,
		}
	}

	// Merge the cached output-url annotation from the deployment ConfigMap
	// so single-row polling carries the same URL the listing page already
	// shows. The handler reads the ConfigMap directly (cheap single GET) —
	// the status cache stays focused on apps/v1.Deployment state only. A
	// missing ConfigMap or annotation simply leaves summary.Output unset.
	// Aggregated links (HOL-574) are merged from the same cache so polling
	// clients receive the same `output.links` and promoted primary URL
	// the list/detail RPCs serve, keeping the three read paths
	// observably consistent.
	if cm, cmErr := rk8s.GetDeployment(ctx, project, name); cmErr == nil {
		mergeOutputURLAnnotation(summary, cm)
		mergeAggregatedLinksAnnotation(summary, cm)
	} else {
		slog.DebugContext(ctx, "could not read deployment ConfigMap for output-url merge",
			slog.String("project", project),
			slog.String("name", name),
			slog.Any("error", cmErr),
		)
	}

	// Populate policy_drift when a PolicyDriftChecker is wired. Silent
	// skip on error — drift is an advisory signal for the UI, not a
	// first-class failure mode: an outage in the TemplatePolicy resolver
	// MUST NOT block status reads.
	h.applyPolicyDrift(ctx, project, name, summary)

	return connect.NewResponse(&consolev1.GetDeploymentStatusSummaryResponse{
		Summary: summary,
	}), nil
}

// applyPolicyDrift sets summary.PolicyDrift when a PolicyDriftChecker is
// configured and reports drift for the target. No-op when the checker is nil
// (local/dev wiring without a policy resolver) or when the target has no
// applied render state yet (a freshly-created deployment that never ran
// through the post-HOL-567 apply path). Errors are logged at DEBUG and
// swallowed so drift remains advisory.
func (h *Handler) applyPolicyDrift(ctx context.Context, project, name string, summary *consolev1.DeploymentStatusSummary) {
	if h.policyDriftChecker == nil || summary == nil {
		return
	}
	drift, hasApplied, err := h.policyDriftChecker.Drift(ctx, project, name)
	if err != nil {
		slog.DebugContext(ctx, "policy drift check failed; skipping summary.policy_drift",
			slog.String("project", project),
			slog.String("deployment", name),
			slog.Any("error", err),
		)
		return
	}
	if !hasApplied {
		// No applied render state yet — drift is meaningless. Leave the
		// field unset so the UI renders the "not-yet-initialized" state
		// rather than a falsy "no drift" state.
		return
	}
	summary.PolicyDrift = &drift
}

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

	rk8s := h.requestK8s(ctx)
	ns := rk8s.Resolver.ProjectNamespace(project)

	dep, err := rk8s.client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
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

	podList, err := rk8s.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Fetch deployment-level events using field selectors. In production the API
	// server filters server-side. The fake K8s client ignores field selectors and
	// returns all events, so tests seed only relevant events for correctness.
	depEventList, err := rk8s.client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
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
		podEventList, err := rk8s.client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
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

	// Replica scalars always come from the live apps/v1.Deployment.Status we
	// fetched above: this RPC has the freshest data, and the informer cache
	// is eventually consistent (it lags during rollouts and immediately
	// after updates). The cached summary is still surfaced for derived
	// phase/message display so the detail page shares the same status
	// derivation path as the listing RPC; on a cache miss the summary is
	// nil and callers render UNSPECIFIED for phase.
	summary, _ := h.summaryFromCache(ns, name)
	status := &consolev1.DeploymentStatus{
		Conditions:        conditions,
		Pods:              pods,
		Events:            depEvents,
		Summary:           summary,
		ReadyReplicas:     dep.Status.ReadyReplicas,
		DesiredReplicas:   dep.Status.Replicas,
		AvailableReplicas: dep.Status.AvailableReplicas,
	}

	return connect.NewResponse(&consolev1.GetDeploymentStatusResponse{
		Status: status,
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
	// Sort by last_seen descending (most recent first). Events without a
	// last_seen timestamp sort after events that have one.
	sort.Slice(events, func(i, j int) bool {
		switch {
		case events[i].LastSeen == nil && events[j].LastSeen == nil:
			return false
		case events[i].LastSeen == nil:
			return false
		case events[j].LastSeen == nil:
			return true
		default:
			return events[i].LastSeen.AsTime().After(events[j].LastSeen.AsTime())
		}
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
