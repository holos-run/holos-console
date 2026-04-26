/**
 * Project-scoped Templates / Grants index (HOL-1013, HOL-1023).
 *
 * TemplateGrants are org/folder-scoped, not project-scoped. The namespace is
 * derived from the selected organization via useOrg().selectedOrg. The project
 * name still appears in the URL so the Templates collapsible group stays open
 * in the sidebar (HOL-1014).
 *
 * HOL-1023: added "New" header action gated on org OWNER/EDITOR role,
 * navigating to the org-scoped /template-grants/new route.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { StandardPageLayout } from '@/components/page-layout'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import {
  useListTemplateGrants,
  useDeleteTemplateGrant,
} from '@/queries/templateGrants'
import { useGetOrganization } from '@/queries/organizations'
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
  // The project param keeps Templates sidebar active (HOL-1014).
  const { selectedOrg } = useOrg()
  const namespace = namespaceForOrg(selectedOrg ?? '')

  // Org role — used to gate the "New" button (OWNER or EDITOR can create).
  const { data: org } = useGetOrganization(selectedOrg ?? '')
  const userRole = org?.userRole ?? Role.VIEWER
  const canCreate = userRole === Role.OWNER || userRole === Role.EDITOR

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
        detailHref: selectedOrg
          ? `/organizations/${selectedOrg}/template-grants/${g.name}`
          : undefined,
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
        label: 'Template Grant',
        newHref: selectedOrg
          ? `/organizations/${selectedOrg}/template-grants/new`
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
      titleParts={[projectName, 'Templates', 'Grants']}
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
