/**
 * Organization-scoped TemplateDependency index (HOL-1020, HOL-1038).
 *
 * TemplateDependency objects live in project namespaces. This org-scoped index
 * shows dependencies from the currently-selected project. When no project is
 * selected, an empty state prompts the user to select one.
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
  useListTemplateDependencies,
  useDeleteTemplateDependency,
} from '@/queries/templateDependencies'
import { useResourcePermissions } from '@/queries/permissions'
import { useProject } from '@/lib/project-context'
import { namespaceForProject } from '@/lib/scope-labels'
import {
  createTemplateResourcePermission,
  hasPermission,
  templateResources,
} from '@/lib/resource-permissions'

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-dependencies/',
)({
  validateSearch: parseGridSearch,
  component: OrgTemplateDependenciesIndexRoute,
})

function OrgTemplateDependenciesIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplateDependenciesIndexPage orgName={orgName} />
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

export function OrgTemplateDependenciesIndexPage({
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

  const { selectedProject } = useProject()

  // TemplateDependencies are project-scoped. Use the selected project namespace.
  const namespace = selectedProject ? namespaceForProject(selectedProject) : ''
  const createPermission = useMemo(
    () => createTemplateResourcePermission(templateResources.templateDependencies, namespace),
    [namespace],
  )
  const permissionsQuery = useResourcePermissions([createPermission])
  const canCreate = hasPermission(permissionsQuery.data, createPermission)

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
        namespace: namespace,
        id: d.name,
        parentId: selectedProject ?? '',
        parentLabel: selectedProject ?? '',
        displayName: d.name,
        description: `${d.dependent?.name ?? ''} → ${d.requires?.name ?? ''}`,
        createdAt: timestampToISOString(d.createdAt),
        detailHref: namespace
          ? `/organizations/${orgName}/template-dependencies/${d.name}?namespace=${encodeURIComponent(namespace)}`
          : undefined,
      })),
    [dependencies, namespace, orgName, selectedProject],
  )

  // ---------------------------------------------------------------------------
  // Kind definitions
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'TemplateDependency',
        label: 'Template Dependency',
        newHref: `/organizations/${orgName}/template-dependencies/new`,
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

  const titleParts = selectedProject
    ? [orgName, 'Template Dependencies', selectedProject]
    : [orgName, 'Template Dependencies']

  return (
    <StandardPageLayout
      titleParts={titleParts}
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
