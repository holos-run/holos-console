/**
 * Organization-scoped TemplateGrant index (HOL-1022, HOL-1038).
 *
 * TemplateGrant objects live in organization or folder namespaces. This
 * org-scoped index shows grants in the current org namespace.
 *
 * HOL-1038: migrated from ResourceGrid directly to StandardPageLayout for
 * consistency with the project-scoped equivalents.
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
import { namespaceForOrg } from '@/lib/scope-labels'

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-grants/',
)({
  validateSearch: parseGridSearch,
  component: OrgTemplateGrantsIndexRoute,
})

function OrgTemplateGrantsIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplateGrantsIndexPage orgName={orgName} />
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

export function OrgTemplateGrantsIndexPage({
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

  // TemplateGrants are org-scoped.
  const namespace = namespaceForOrg(orgName)

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
        namespace: namespace,
        id: g.name,
        parentId: orgName,
        parentLabel: orgName,
        displayName: g.name,
        description: g.from.map((f) => f.namespace).join(', ') || '',
        createdAt: timestampToISOString(g.createdAt),
        detailHref: `/organizations/${orgName}/template-grants/${g.name}`,
      })),
    [grants, namespace, orgName],
  )

  // ---------------------------------------------------------------------------
  // Kind definitions
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'TemplateGrant',
        label: 'Template Grant',
        newHref: `/organizations/${orgName}/template-grants/new`,
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
    <StandardPageLayout
      titleParts={[orgName, 'Template Grants']}
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
