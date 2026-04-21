import { createFileRoute, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { PhaseBadge } from '@/components/phase-badge'
import { QuotaPlaceholder } from '@/components/quota-placeholder'
import { ServiceStatusPanel } from '@/components/service-status-panel'
import type { Deployment } from '@/gen/holos/console/v1/deployments_pb'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useListDeployments } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'

export const Route = createFileRoute('/_authenticated/projects/$projectName/')({
  component: ProjectIndexRoute,
})

function ProjectIndexRoute() {
  const { projectName } = Route.useParams()
  return <ProjectIndexPage projectName={projectName} />
}

export function ProjectIndexPage({ projectName }: { projectName: string }) {
  const {
    data: deployments = [],
    isPending: deploymentsPending,
    error: deploymentsError,
  } = useListDeployments(projectName)
  const { data: project } = useGetProject(projectName)
  const userRole = project?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  // projectName is always non-empty under /_authenticated/projects/$projectName/,
  // but guard defensively so the hooks above can still be called unconditionally.
  if (!projectName) return null

  return (
    <div className="space-y-4">
      <DeploymentsSummary
        projectName={projectName}
        deployments={deployments}
        isPending={deploymentsPending}
        error={deploymentsError}
        canWrite={canWrite}
      />
      <QuotaPlaceholder />
      <ServiceStatusPanel />
    </div>
  )
}

interface DeploymentsSummaryProps {
  projectName: string
  deployments: Deployment[]
  isPending: boolean
  error: Error | null
  canWrite: boolean
}

function DeploymentsSummary({
  projectName,
  deployments,
  isPending,
  error,
  canWrite,
}: DeploymentsSummaryProps) {
  return (
    <Card>
      <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
        <CardTitle>Deployments</CardTitle>
        <div className="flex items-center gap-2">
          <Link to="/projects/$projectName/deployments" params={{ projectName }}>
            <Button size="sm" variant="outline">
              View all
            </Button>
          </Link>
          {canWrite && (
            <Link
              to="/projects/$projectName/deployments/new"
              params={{ projectName }}
            >
              <Button size="sm">Create Deployment</Button>
            </Link>
          )}
        </div>
      </CardHeader>
      <CardContent>
        {isPending ? (
          <div className="space-y-2" data-testid="deployments-loading">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : error ? (
          <Alert variant="destructive">
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        ) : deployments.length === 0 ? (
          <div className="rounded-md border border-dashed border-border p-6 text-center">
            <p className="text-sm font-medium">No deployments yet.</p>
            <p className="mt-1 text-sm text-muted-foreground">
              Deployments are the running applications in this project.
            </p>
          </div>
        ) : (
          <ul className="divide-y divide-border">
            {deployments.map((deployment) => (
              <li
                key={deployment.name}
                className="flex items-center justify-between gap-3 py-2"
              >
                <Link
                  to="/projects/$projectName/deployments/$deploymentName"
                  params={{ projectName, deploymentName: deployment.name }}
                  search={{ tab: 'status' }}
                  className="font-medium hover:underline"
                >
                  {deployment.name}
                </Link>
                <span className="flex items-center gap-3 text-sm text-muted-foreground">
                  <span className="font-mono">
                    {deployment.image}:{deployment.tag}
                  </span>
                  <PhaseBadge summary={deployment.statusSummary} />
                </span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
