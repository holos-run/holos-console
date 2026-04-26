/**
 * Project-scoped Templates / Requirements index (HOL-1013, HOL-1023).
 *
 * TemplateRequirements are org/folder-scoped, not project-scoped. The namespace
 * is derived from the selected organization via useOrg().selectedOrg. The
 * project name still appears in the URL so the Templates collapsible group
 * stays open in the sidebar (HOL-1014).
 *
 * HOL-1023: added "New" header action gated on org OWNER/EDITOR role,
 * navigating to the org-scoped /template-requirements/new route.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { StandardPageLayout } from '@/components/page-layout'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import {
  useListTemplateRequirements,
  useDeleteTemplateRequirement,
} from '@/queries/templateRequirements'
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
  // The project param keeps Templates sidebar active (HOL-1014).
  const { selectedOrg } = useOrg()
  const namespace = namespaceForOrg(selectedOrg ?? '')

  // Org role — used to gate the "New" button (OWNER or EDITOR can create).
  const { data: org } = useGetOrganization(selectedOrg ?? '')
  const userRole = org?.userRole ?? Role.VIEWER
  const canCreate = userRole === Role.OWNER || userRole === Role.EDITOR

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
        detailHref:
          namespace && selectedOrg
            ? `/organizations/${selectedOrg}/template-requirements/${r.name}`
            : undefined,
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
        label: 'Template Requirement',
        newHref: selectedOrg
          ? `/organizations/${selectedOrg}/template-requirements/new`
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
      titleParts={[projectName, 'Templates', 'Requirements']}
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
