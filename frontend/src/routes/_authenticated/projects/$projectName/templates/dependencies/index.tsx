/**
 * Project-scoped Templates / Dependencies index (HOL-1013).
 *
 * TemplateDependencies are project-scoped. The namespace is derived from the
 * $projectName URL parameter via namespaceForProject(). The project name also
 * keeps the Templates collapsible group open in the sidebar (HOL-1014).
 *
 * Sidebar nesting is handled in HOL-1014; for now the route exists and is
 * reachable by URL.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { ResourceGrid } from '@/components/resource-grid/ResourceGrid'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import {
  useListTemplateDependencies,
  useDeleteTemplateDependency,
} from '@/queries/templateDependencies'
import { namespaceForProject } from '@/lib/scope-labels'

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
        detailHref: undefined,
      })),
    [dependencies, namespace, projectName],
  )

  // ---------------------------------------------------------------------------
  // Kind definitions
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'TemplateDependency',
        label: 'TemplateDependency',
        // No create in this view — dependencies are managed programmatically.
        canCreate: false,
      },
    ],
    [],
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
    <ResourceGrid
      title={`${projectName} / Templates / Dependencies`}
      kinds={kinds}
      rows={rows}
      onDelete={handleDelete}
      isLoading={isPending}
      error={error}
      search={search}
      onSearchChange={handleSearchChange}
    />
  )
}
