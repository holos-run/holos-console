/**
 * Deployments index page — reimplemented on ResourceGrid v1 (HOL-858).
 *
 * Default view: current project as the single parent with Parent column hidden.
 * A static description banner at the top of the Card explains what a Deployment is.
 * Phase and PolicyDrift badges are preserved via the `extraColumns` extension
 * on ResourceGrid v1.
 *
 * Detail, create, logs, and drift flows are unchanged — only the list page is
 * rewritten.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { ResourceGrid } from '@/components/resource-grid/ResourceGrid'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import { useListDeployments, useDeleteDeployment } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'
import { PhaseBadge } from '@/components/phase-badge'
import { PolicyDriftBadge } from '@/components/policy-drift/PolicySection'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import type { ColumnDef } from '@tanstack/react-table'
import type { Deployment } from '@/gen/holos/console/v1/deployments_pb'

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

export const Route = createFileRoute('/_authenticated/projects/$projectName/deployments/')({
  validateSearch: parseGridSearch,
  component: DeploymentsListPage,
})

// ---------------------------------------------------------------------------
// Description banner
// ---------------------------------------------------------------------------

/**
 * DeploymentsDescription renders a static three-bullet explanation of what a
 * Deployment is. The copy is verbatim from the HOL-858 acceptance criteria.
 */
export function DeploymentsDescription() {
  return (
    <div
      className="mb-4 rounded-md border border-border bg-muted/40 p-4 text-sm text-muted-foreground"
      data-testid="deployments-description"
    >
      <ul className="list-disc pl-5 space-y-1">
        <li>Deployment is a collection of resource declarations (configuration).</li>
        <li>Deploying is applying the configuration to the platform.</li>
        <li>Controllers reconcile current state with desired state.</li>
      </ul>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Extra columns — Phase badge and Policy Drift badge
// ---------------------------------------------------------------------------

/**
 * Builds the extra columns for the Deployments grid.
 * The `deploymentsByName` map lets each cell look up the original deployment
 * to access statusSummary without widening the Row type.
 */
function useDeploymentExtraColumns(
  deploymentsByName: Map<string, Deployment>,
): ColumnDef<Row>[] {
  return useMemo(
    () => [
      {
        id: 'phase',
        header: 'Phase',
        cell: ({ row }: { row: { original: Row } }) => {
          const dep = deploymentsByName.get(row.original.name)
          return (
            <div className="inline-flex items-center gap-2 flex-wrap">
              <PhaseBadge summary={dep?.statusSummary} />
              {dep?.statusSummary?.policyDrift ? (
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>
                        <PolicyDriftBadge />
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>
                      The deployment was rendered before a template policy changed; click Reconcile to re-render.
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              ) : null}
            </div>
          )
        },
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [deploymentsByName],
  )
}

// ---------------------------------------------------------------------------
// Page component
// ---------------------------------------------------------------------------

export function DeploymentsListPage() {
  const { projectName } = Route.useParams()
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })

  const { data: project } = useGetProject(projectName)
  const { data: deployments = [], isPending, error } = useListDeployments(projectName)
  const deleteMutation = useDeleteDeployment(projectName)

  const userRole = project?.userRole ?? Role.VIEWER
  const canCreate = userRole === Role.OWNER || userRole === Role.EDITOR

  // Build name→deployment lookup for extra columns.
  const deploymentsByName = useMemo(() => {
    const map = new Map<string, Deployment>()
    for (const dep of deployments) {
      if (dep) map.set(dep.name, dep)
    }
    return map
  }, [deployments])

  // Map Deployment → ResourceGrid Row
  const rows: Row[] = useMemo(
    () =>
      deployments.map((dep) => ({
        kind: 'Deployment',
        name: dep.name,
        namespace: projectName,
        id: dep.name,
        parentId: projectName,
        parentLabel: projectName,
        displayName: dep.displayName || dep.name,
        description: dep.description ?? '',
        createdAt: '',
        detailHref: `/projects/${projectName}/deployments/${dep.name}`,
      })),
    [deployments, projectName],
  )

  const kinds = [
    {
      id: 'Deployment',
      label: 'Deployment',
      newHref: `/projects/${projectName}/deployments/new`,
      canCreate,
    },
  ]

  const extraColumns = useDeploymentExtraColumns(deploymentsByName)

  const handleDelete = useCallback(
    async (row: Row) => {
      await deleteMutation.mutateAsync({ name: row.name })
    },
    [deleteMutation],
  )

  const handleSearchChange = useCallback(
    (updater: (prev: ResourceGridSearch) => ResourceGridSearch) => {
      navigate({
        search: (prev) => updater(prev as ResourceGridSearch),
      })
    },
    [navigate],
  )

  return (
    <ResourceGrid
      title={`${projectName} / Deployments`}
      kinds={kinds}
      rows={rows}
      onDelete={handleDelete}
      isLoading={isPending}
      error={error}
      search={search}
      onSearchChange={handleSearchChange}
      extraColumns={extraColumns}
      headerContent={<DeploymentsDescription />}
    />
  )
}
