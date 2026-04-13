import { Badge } from '@/components/ui/badge'
import { DeploymentPhase, type DeploymentStatusSummary } from '@/gen/holos/console/v1/deployments_pb'

/**
 * DeploymentStatus is the minimal shape PhaseBadge needs to render.
 * Accepts either the full status_summary message or undefined/missing.
 */
export interface PhaseBadgeProps {
  /** Lightweight status snapshot, typically from deployment.statusSummary. */
  summary?: DeploymentStatusSummary
}

function phaseBadge(phase: DeploymentPhase) {
  switch (phase) {
    case DeploymentPhase.RUNNING:
      return <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200 border-transparent">Running</Badge>
    case DeploymentPhase.PENDING:
      return <Badge className="bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200 border-transparent">Pending</Badge>
    case DeploymentPhase.FAILED:
      return <Badge variant="destructive">Failed</Badge>
    case DeploymentPhase.SUCCEEDED:
      return <Badge className="bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200 border-transparent">Succeeded</Badge>
    default:
      return <Badge variant="outline">Unknown</Badge>
  }
}

/**
 * PhaseBadge renders a deployment phase badge, optionally with a
 * ready/desired replica count (e.g. "Running 2/3") when summary is present
 * and desired_replicas > 0.
 *
 * - If summary is provided, phase is taken from summary.phase and the replica
 *   count is shown inline when desired_replicas > 0.
 * - If summary is absent, the Unknown badge is rendered. This happens while
 *   the informer cache is still warming up or the Deployment has not yet
 *   been observed.
 */
export function PhaseBadge({ summary }: PhaseBadgeProps) {
  if (!summary) {
    return phaseBadge(DeploymentPhase.UNSPECIFIED)
  }
  const badge = phaseBadge(summary.phase)
  if (summary.desiredReplicas > 0) {
    return (
      <span className="inline-flex items-center gap-2">
        {badge}
        <span className="font-mono text-sm text-muted-foreground">
          {summary.readyReplicas}/{summary.desiredReplicas}
        </span>
      </span>
    )
  }
  return badge
}
