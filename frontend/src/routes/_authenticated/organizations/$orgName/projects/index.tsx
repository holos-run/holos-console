/**
 * Organization-scoped Projects index — reimplemented on ResourceGrid v1
 * (HOL-990 AC2).
 *
 * Project rows expose `id: project.name` (HOL-990 AC1.1) and surface
 * `creatorEmail` as a hidden searchable field via `extraSearch` so operators
 * can find projects by their creator without crowding the visible columns.
 *
 * The Parent column is supplied via `parentLabel` (the organization name).
 * Projects all share the organization as their parent, so the column is hidden
 * by ResourceGrid for the usual organization-scoped listing.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'

import { StandardPageLayout } from '@/components/page-layout'
import { Button } from '@/components/ui/button'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import { useListProjects } from '@/queries/projects'
import { useResourcePermissions } from '@/queries/permissions'
import {
  createNamespacePermission,
  hasPermission,
} from '@/lib/resource-permissions'

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/projects/',
)({
  validateSearch: parseGridSearch,
  component: OrgProjectsIndexRoute,
})

function OrgProjectsIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgProjectsIndexPage orgName={orgName} />
}

export function OrgProjectsIndexPage({
  orgName: propOrgName,
}: { orgName?: string } = {}) {
  let routeOrgName: string | undefined
  let routeSearch: ResourceGridSearch | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeOrgName = Route.useParams().orgName
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeSearch = Route.useSearch() as ResourceGridSearch
  } catch {
    routeOrgName = undefined
    routeSearch = undefined
  }
  const orgName = propOrgName ?? routeOrgName ?? ''
  const search: ResourceGridSearch = routeSearch ?? {}

  const navigate = useNavigate()
  const { data, isLoading, error } = useListProjects(orgName)
  const projects = data?.projects ?? []
  const createPermission = useMemo(() => createNamespacePermission(), [])
  const permissionsQuery = useResourcePermissions([createPermission])
  const canCreate = hasPermission(permissionsQuery.data, createPermission)

  const rows: Row[] = projects.map((project) => ({
    kind: 'Project',
    name: project.name,
    namespace: project.parentName || orgName,
    id: project.name,
    parentId: project.parentName || orgName,
    parentLabel: project.parentName || orgName,
    displayName: project.displayName || project.name,
    description: project.description ?? '',
    createdAt: project.createdAt,
    extraSearch: { creator: project.creatorEmail ?? '' },
    detailHref: `/projects/${project.name}`,
  }))

  // Project creation uses a custom Link (not the ResourceGrid NewButton) so we
  // can keep the "Create Project" wording and route-search-param wiring expected
  // by the rest of the console (HOL-929). Pass `canCreate: false` so the grid
  // does not render its own New button — `headerActions` supplies one instead.
  const kinds = [
    {
      id: 'Project',
      label: 'Project',
      canCreate: false,
    },
  ]

  const createProjectButton = (
    canCreate ? (
      <Link
        to="/project/new"
        search={{ orgName, returnTo: `/organizations/${orgName}/projects` }}
      >
        <Button size="sm">Create Project</Button>
      </Link>
    ) : null
  )

  const handleSearchChange = useCallback(
    (updater: (prev: ResourceGridSearch) => ResourceGridSearch) => {
      navigate({
        // The default useNavigate() returns a router-wide navigate whose
        // search-updater signature cannot infer the route's search type.
        // Cast through unknown so the typed updater above flows through.
        search: ((prev: unknown) =>
          updater(prev as ResourceGridSearch)) as never,
      })
    },
    [navigate],
  )

  // Project deletion is performed from the project detail page, not from this
  // index. Pass a noop here since `showDeleteAction={false}` suppresses the
  // trash column entirely.
  const handleDelete = async () => undefined

  const emptyState = (
    <div className="flex flex-col items-center gap-3 py-8 text-center">
      <p className="text-muted-foreground">
        No projects in this organization yet.
      </p>
      {canCreate && (
        <Link
          to="/project/new"
          search={{ orgName, returnTo: `/organizations/${orgName}/projects` }}
        >
          <Button size="sm">Create Project</Button>
        </Link>
      )}
    </div>
  )

  return (
    <StandardPageLayout
      title="Projects"
      breadcrumbs={[
        { label: 'Organizations', href: '/organizations' },
        { label: orgName },
        { label: 'Projects' },
      ]}
      headerActions={createProjectButton}
      grid={{
        kinds,
        rows,
        onDelete: handleDelete,
        isLoading,
        error,
        search,
        onSearchChange: handleSearchChange,
        extraSearchFields: [{ id: 'creator', label: 'Creator' }],
        emptyStateContent: emptyState,
        showDeleteAction: false,
      }}
    />
  )
}
