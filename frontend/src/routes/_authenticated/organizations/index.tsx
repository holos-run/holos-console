/**
 * Organizations index — migrated to ResourceGrid v1 (HOL-976).
 *
 * Replaces the manual TanStack Table + local state implementation with
 * ResourceGrid v1 backed by URL state. Uses useListOrganizationsKPD so
 * stale rows are preserved while search-param changes are in flight.
 *
 * Row click navigates to /organizations/$orgName/projects. The
 * organizations/$orgName layout (OrgLayout) syncs setSelectedOrg automatically
 * via useEffect when the route param changes, so no manual store write is
 * needed here.
 */

import { useCallback } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'

import { Button } from '@/components/ui/button'
import { ResourceGrid } from '@/components/resource-grid/ResourceGrid'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import { useListOrganizationsKPD } from '@/queries/organizations'

export const Route = createFileRoute('/_authenticated/organizations/')({
  validateSearch: parseGridSearch,
  component: OrganizationsIndexRoute,
})

function OrganizationsIndexRoute() {
  return <OrganizationsIndexPage />
}

export function OrganizationsIndexPage() {
  let routeSearch: ResourceGridSearch | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeSearch = Route.useSearch() as ResourceGridSearch
  } catch {
    routeSearch = undefined
  }
  const search: ResourceGridSearch = routeSearch ?? {}

  const navigate = useNavigate()
  const { data: organizations = [], isPending, error } = useListOrganizationsKPD()

  const rows: Row[] = organizations.map((org) => ({
    kind: 'Organization',
    name: org.name,
    namespace: '',
    id: org.name,
    parentId: '',
    parentLabel: '',
    displayName: org.displayName || org.name,
    description: org.description ?? '',
    createdAt: org.createdAt,
    detailHref: `/organizations/${org.name}/projects`,
  }))

  // Organization deletion is not exposed from this index.
  const kinds = [
    {
      id: 'Organization',
      label: 'Organization',
      canCreate: false,
    },
  ]

  const createOrgButton = (
    <Link to="/organization/new" search={{ returnTo: '/organizations' }}>
      <Button size="sm">Create Organization</Button>
    </Link>
  )

  const handleSearchChange = useCallback(
    (updater: (prev: ResourceGridSearch) => ResourceGridSearch) => {
      navigate({
        search: ((prev: unknown) =>
          updater(prev as ResourceGridSearch)) as never,
      })
    },
    [navigate],
  )

  const handleDelete = async () => undefined

  const emptyState = (
    <div className="flex flex-col items-center gap-3 py-8 text-center">
      <p className="text-muted-foreground">No organizations yet. Create one.</p>
      <Link to="/organization/new" search={{ returnTo: '/organizations' }}>
        <Button size="sm">Create Organization</Button>
      </Link>
    </div>
  )

  return (
    <ResourceGrid
      title="Organizations"
      kinds={kinds}
      rows={rows}
      onDelete={handleDelete}
      isLoading={isPending}
      error={error}
      search={search}
      onSearchChange={handleSearchChange}
      emptyStateContent={emptyState}
      headerActions={createOrgButton}
      showDeleteAction={false}
    />
  )
}
