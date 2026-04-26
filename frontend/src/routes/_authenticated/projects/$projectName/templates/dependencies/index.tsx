/**
 * Project-scoped Templates / Dependencies index (HOL-1013, HOL-1023).
 *
 * TemplateDependencies are project-scoped. The namespace is derived from the
 * $projectName URL parameter via namespaceForProject(). The project name also
 * keeps the Templates collapsible group open in the sidebar (HOL-1014).
 *
 * HOL-1023: added "New" header action gated on project OWNER/EDITOR role,
 * navigating to the org-scoped /template-dependencies/new route.
 *
 * The sidebar nesting was implemented in HOL-1014.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { StandardPageLayout } from '@/components/page-layout'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import {
  useListTemplateDependencies,
  useDeleteTemplateDependency,
} from '@/queries/templateDependencies'
import { useGetProject } from '@/queries/projects'
import { namespaceForProject } from '@/lib/scope-labels'
import { useOrg } from '@/lib/org-context'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Convert a proto Timestamp to an ISO-8601 string for ResourceGrid createdAt. */
function timestampToISOString(ts: { seconds: bigint } | undefined): string {
  if (!ts) return ''
  return new Date(Number(ts.seconds) * 1000).toISOString()
}

// ---------------------------------------------------------------------------
// Route definition
// ---------------------------------------------------------------------------

export const Route = createFileRoute(
  '/_authenticated/projects/$projectName/templates/dependencies/',
)({
  validateSearch: parseGridSearch,
  component: TemplateDependenciesIndexRoute,
})

function TemplateDependenciesIndexRoute() {
  const { projectName } = Route.useParams()
  return <TemplateDependenciesIndexPage projectName={projectName} />
}

// ---------------------------------------------------------------------------
// Page component (exported for tests)
// ---------------------------------------------------------------------------

export function TemplateDependenciesIndexPage({
  projectName,
}: {
  projectName: string
}) {
  const search = Route.useSearch() as ResourceGridSearch
  const navigate = useNavigate({ from: Route.fullPath })

  // TemplateDependencies are project-scoped — namespace comes from projectName.
  const namespace = namespaceForProject(projectName)

  // selectedOrg is used to build detailHref links and the New route URL.
  const { selectedOrg } = useOrg()

  // Project role — used to gate the "New" button (OWNER or EDITOR can create).
  const { data: project } = useGetProject(projectName)
  const userRole = project?.userRole ?? Role.VIEWER
  const canCreate = userRole === Role.OWNER || userRole === Role.EDITOR

  const {
    data: dependencies = [],
    isPending,
    error,
  } = useListTemplateDependencies(namespace)

  const deleteMutation = useDeleteTemplateDependency(namespace)

  // ---------------------------------------------------------------------------
  // Build rows
  // ---------------------------------------------------------------------------

  const rows: Row[] = useMemo(
    () =>
      dependencies.map((d) => ({
        kind: 'TemplateDependency',
        name: d.name,
        namespace,
        id: d.name,
        parentId: projectName,
        parentLabel: projectName,
        displayName: d.name,
        description: `${d.dependent?.name ?? ''} → ${d.requires?.name ?? ''}`,
        createdAt: timestampToISOString(d.createdAt),
        detailHref: selectedOrg
          ? `/organizations/${selectedOrg}/template-dependencies/${d.name}?namespace=${encodeURIComponent(namespace)}`
          : undefined,
      })),
    [dependencies, namespace, projectName, selectedOrg],
  )

  // ---------------------------------------------------------------------------
  // Kind definitions
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'TemplateDependency',
        label: 'Template Dependency',
        newHref: selectedOrg
          ? `/organizations/${selectedOrg}/template-dependencies/new`
          : undefined,
        canCreate: !!selectedOrg && canCreate,
      },
    ],
    [selectedOrg, canCreate],
  )

  // ---------------------------------------------------------------------------
  // Handlers
  // ---------------------------------------------------------------------------

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
      titleParts={[projectName, 'Templates', 'Dependencies']}
      grid={{
        kinds,
        rows,
        onDelete: handleDelete,
        isLoading: isPending,
        error,
        search,
        onSearchChange: handleSearchChange,
      }}
    />
  )
}
