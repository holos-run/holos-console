/**
 * Organization-scoped TemplateRequirement index (HOL-1021, HOL-1038).
 *
 * TemplateRequirement objects live in organization or folder namespaces. This
 * org-scoped index shows requirements in the current org namespace.
 *
 * HOL-1038: migrated from ResourceGrid directly to StandardPageLayout for
 * consistency with the project-scoped equivalents.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { StandardPageLayout } from '@/components/page-layout'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import {
  useListTemplateRequirements,
  useDeleteTemplateRequirement,
} from '@/queries/templateRequirements'
import { useResourcePermissions } from '@/queries/permissions'
import { namespaceForOrg } from '@/lib/scope-labels'
import {
  createTemplateResourcePermission,
  hasPermission,
  templateResources,
} from '@/lib/resource-permissions'

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

  // TemplateRequirements are org-scoped.
  const namespace = namespaceForOrg(orgName)
  const createPermission = useMemo(
    () => createTemplateResourcePermission(templateResources.templateRequirements, namespace),
    [namespace],
  )
  const permissionsQuery = useResourcePermissions([createPermission])
  const canCreate = hasPermission(permissionsQuery.data, createPermission)

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
        canCreate,
      },
    ],
    [orgName, canCreate],
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
      titleParts={[orgName, 'Template Requirements']}
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
