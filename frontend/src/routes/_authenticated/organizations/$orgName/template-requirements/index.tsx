/**
 * Organization-scoped TemplateRequirement index (HOL-1021).
 *
 * TemplateRequirement objects live in organization or folder namespaces. This
 * org-scoped index shows requirements in the current org namespace.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { ResourceGrid } from '@/components/resource-grid/ResourceGrid'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import {
  useListTemplateRequirements,
  useDeleteTemplateRequirement,
} from '@/queries/templateRequirements'
import { useGetOrganization } from '@/queries/organizations'
import { namespaceForOrg } from '@/lib/scope-labels'

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-requirements/',
)({
  validateSearch: parseGridSearch,
  component: OrgTemplateRequirementsIndexRoute,
})

function OrgTemplateRequirementsIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplateRequirementsIndexPage orgName={orgName} />
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function timestampToISOString(ts: { seconds: bigint } | undefined): string {
  if (!ts) return ''
  return new Date(Number(ts.seconds) * 1000).toISOString()
}

// ---------------------------------------------------------------------------
// Page component (exported for tests)
// ---------------------------------------------------------------------------

export function OrgTemplateRequirementsIndexPage({
  orgName: propOrgName,
}: { orgName?: string } = {}) {
  let routeOrgName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeOrgName = Route.useParams().orgName
  } catch {
    routeOrgName = undefined
  }
  const orgName = propOrgName ?? routeOrgName ?? ''

  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })

  const { data: org } = useGetOrganization(orgName)

  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  // TemplateRequirements are org-scoped.
  const namespace = namespaceForOrg(orgName)

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
        namespace: namespace,
        id: r.name,
        parentId: orgName,
        parentLabel: orgName,
        displayName: r.name,
        description: r.requires?.name ?? '',
        createdAt: timestampToISOString(r.createdAt),
        detailHref: `/organizations/${orgName}/template-requirements/${r.name}`,
      })),
    [requirements, namespace, orgName],
  )

  // ---------------------------------------------------------------------------
  // Kind definitions
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'TemplateRequirement',
        label: 'Template Requirement',
        newHref: `/organizations/${orgName}/template-requirements/new`,
        canCreate: canWrite,
      },
    ],
    [orgName, canWrite],
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
        replace: true,
      })
    },
    [navigate],
  )

  return (
    <ResourceGrid
      title={`${orgName} / Template Requirements`}
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
