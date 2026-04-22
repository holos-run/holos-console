/**
 * Secrets index page — reimplemented on ResourceGrid v1 (HOL-857).
 *
 * Default view: current project as the single parent with Parent column hidden.
 * Expanding the lineage filter to ancestors/both surfaces folder- and org-level
 * secrets without leaving the page.
 *
 * Detail and new/edit flows are unchanged — only the list page is rewritten.
 */

import { useCallback } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { ResourceGrid } from '@/components/resource-grid/ResourceGrid'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import { useAllSecretsForProject, useDeleteSecret } from '@/queries/secrets'
import { useGetProject } from '@/queries/projects'

export const Route = createFileRoute('/_authenticated/projects/$projectName/secrets/')({
  validateSearch: parseGridSearch,
  component: SecretsListPage,
})

export function SecretsListPage() {
  const { projectName } = Route.useParams()
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })

  const { data: project } = useGetProject(projectName)

  const lineage = search.lineage ?? 'descendants'
  const { data: secretRows = [], isPending, error } = useAllSecretsForProject(projectName, { lineage })

  const deleteMutation = useDeleteSecret(projectName)

  const userRole = project?.userRole ?? Role.VIEWER
  const canCreate = userRole === Role.OWNER || userRole === Role.EDITOR

  // Map SecretRow → ResourceGrid Row
  const rows: Row[] = secretRows.map(({ secret, scope }) => ({
    kind: 'Secret',
    name: secret.name,
    namespace: scope,
    id: `${scope}/${secret.name}`,
    parentId: scope,
    parentLabel: scope,
    displayName: secret.name,
    description: secret.description ?? '',
    createdAt: secret.createdAt,
    detailHref: `/projects/${projectName}/secrets/${secret.name}`,
  }))

  const kinds = [
    {
      id: 'Secret',
      label: 'Secret',
      newHref: `/projects/${projectName}/secrets/new`,
      canCreate,
    },
  ]

  const handleDelete = useCallback(
    async (row: Row) => {
      await deleteMutation.mutateAsync(row.name)
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
      title={`${projectName} / Secrets`}
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
