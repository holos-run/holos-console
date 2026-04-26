/**
 * Project-scoped Templates / Requirements index (HOL-1013).
 *
 * TemplateRequirements are org/folder-scoped, not project-scoped. The namespace
 * is derived from the selected organization via useOrg().selectedOrg. The
 * project name still appears in the URL so the Templates collapsible group
 * can stay open in a later sidebar phase (HOL-1014).
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
  useListTemplateRequirements,
  useDeleteTemplateRequirement,
} from '@/queries/templateRequirements'
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
  '/_authenticated/projects/$projectName/templates/requirements/',
)({
  validateSearch: parseGridSearch,
  component: TemplateRequirementsIndexRoute,
})

function TemplateRequirementsIndexRoute() {
  const { projectName } = Route.useParams()
  return <TemplateRequirementsIndexPage projectName={projectName} />
}

// ---------------------------------------------------------------------------
// Page component (exported for tests)
// ---------------------------------------------------------------------------

export function TemplateRequirementsIndexPage({
  projectName,
}: {
  projectName: string
}) {
  const search = Route.useSearch() as ResourceGridSearch
  const navigate = useNavigate({ from: Route.fullPath })

  // TemplateRequirements are org/folder-scoped — namespace comes from the
  // selected org, not the project. The project param keeps Templates sidebar
  // active in a later phase.
  const { selectedOrg } = useOrg()
  const namespace = namespaceForOrg(selectedOrg ?? '')

  const {
    data: requirements = [],
    isPending,
    error,
  } = useListTemplateRequirements(namespace)

  const deleteMutation = useDeleteTemplateRequirement(namespace)

  // ---------------------------------------------------------------------------
  // Build rows
  // ---------------------------------------------------------------------------

  const rows: Row[] = useMemo(
    () =>
      requirements.map((r) => ({
        kind: 'TemplateRequirement',
        name: r.name,
        namespace,
        id: r.name,
        parentId: selectedOrg ?? '',
        parentLabel: selectedOrg ?? '',
        displayName: r.name,
        description: r.requires?.name ?? '',
        createdAt: timestampToISOString(r.createdAt),
        detailHref: undefined,
      })),
    [requirements, namespace, selectedOrg],
  )

  // ---------------------------------------------------------------------------
  // Kind definitions
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'TemplateRequirement',
        label: 'TemplateRequirement',
        // No create in this view — requirements are created from org-level pages.
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
      title={`${projectName} / Templates / Requirements`}
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
