/**
 * Project-scoped Templates / Grants index (HOL-1013).
 *
 * TemplateGrants are org/folder-scoped, not project-scoped. The namespace is
 * derived from the selected organization via useOrg().selectedOrg. The project
 * name still appears in the URL so the Templates collapsible group can stay
 * open in a later sidebar phase (HOL-1014).
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
  useListTemplateGrants,
  useDeleteTemplateGrant,
} from '@/queries/templateGrants'
import { useOrg } from '@/lib/org-context'
import { namespaceForOrg } from '@/lib/scope-labels'

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
  '/_authenticated/projects/$projectName/templates/grants/',
)({
  validateSearch: parseGridSearch,
  component: TemplateGrantsIndexRoute,
})

function TemplateGrantsIndexRoute() {
  const { projectName } = Route.useParams()
  return <TemplateGrantsIndexPage projectName={projectName} />
}

// ---------------------------------------------------------------------------
// Page component (exported for tests)
// ---------------------------------------------------------------------------

export function TemplateGrantsIndexPage({
  projectName,
}: {
  projectName: string
}) {
  const search = Route.useSearch() as ResourceGridSearch
  const navigate = useNavigate({ from: Route.fullPath })

  // TemplateGrants are org/folder-scoped — namespace comes from the selected
  // org, not the project. The project param keeps Templates sidebar active
  // in a later phase.
  const { selectedOrg } = useOrg()
  const namespace = namespaceForOrg(selectedOrg ?? '')

  const {
    data: grants = [],
    isPending,
    error,
  } = useListTemplateGrants(namespace)

  const deleteMutation = useDeleteTemplateGrant(namespace)

  // ---------------------------------------------------------------------------
  // Build rows
  // ---------------------------------------------------------------------------

  const rows: Row[] = useMemo(
    () =>
      grants.map((g) => ({
        kind: 'TemplateGrant',
        name: g.name,
        namespace,
        id: g.name,
        parentId: selectedOrg ?? '',
        parentLabel: selectedOrg ?? '',
        displayName: g.name,
        description:
          g.from.map((f) => f.namespace).join(', ') || '',
        createdAt: timestampToISOString(g.createdAt),
        detailHref: undefined,
      })),
    [grants, namespace, selectedOrg],
  )

  // ---------------------------------------------------------------------------
  // Kind definitions
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'TemplateGrant',
        label: 'TemplateGrant',
        // No create in this view — grants are created from org-level pages.
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
      title={`${projectName} / Templates / Grants`}
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
