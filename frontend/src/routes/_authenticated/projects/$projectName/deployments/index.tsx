/**
 * Deployments index page — reimplemented on ResourceGrid v1 (HOL-858).
 *
 * Default view: current project as the single parent with Parent column hidden.
 * Phase and PolicyDrift badges are preserved via the `extraColumns` extension
 * on ResourceGrid v1.
 *
 * Detail, create, logs, and drift flows are unchanged — only the list page is
 * rewritten.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { StandardPageLayout } from '@/components/page-layout'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import { useListDeployments, useDeleteDeployment } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'
import { PhaseBadge } from '@/components/phase-badge'
import { PolicyDriftBadge } from '@/components/policy-drift/PolicySection'
import { SharedDependencyBadge } from '@/components/deployments/SharedDependencyBadge'
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
// Extra columns — Phase badge and Policy Drift badge
// ---------------------------------------------------------------------------

/**
 * Builds the extra columns for the Deployments grid.
 * The `deploymentsByName` map lets each cell look up the original deployment
 * to access statusSummary without widening the Row type.
 */
function useDeploymentExtraColumns(
  deploymentsByName: Map<string, Deployment>,
  projectName: string,
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
      {
        id: 'sharedDependency',
        header: 'Dependency',
        cell: ({ row }: { row: { original: Row } }) => {
          // Singleton rows carry the resolved (TemplateDependency /
          // TemplateRequirement) edges as `deployment.dependencies`. The badge
          // tooltip surfaces each originating CRD object; the link target
          // remains the singleton's own detail page (no in-app routes exist
          // for the originating CRD kinds yet).
          const detailHref = row.original.detailHref ?? `/projects/${projectName}/deployments/${row.original.name}`
          const dep = deploymentsByName.get(row.original.name)
          return (
            <SharedDependencyBadge
              name={row.original.name}
              dependencies={dep?.dependencies}
              linkHref={detailHref}
            />
          )
        },
      },
    ],
    [deploymentsByName, projectName],
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
        createdAt: dep.createdAt,
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

  const extraColumns = useDeploymentExtraColumns(deploymentsByName, projectName)

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
    <StandardPageLayout
      titleParts={[projectName, 'Deployments']}
      grid={{
        kinds,
        rows,
        onDelete: handleDelete,
        isLoading: isPending,
        error,
        search,
        onSearchChange: handleSearchChange,
        extraColumns,
      }}
    />
  )
}
