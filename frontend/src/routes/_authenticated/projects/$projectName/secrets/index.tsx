/**
 * Secrets index page — reimplemented on ResourceGrid v1 (HOL-857).
 *
 * Default view: current project as the single parent with Parent column hidden.
 *
 * Detail and new/edit flows are unchanged — only the list page is rewritten.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { StandardPageLayout } from '@/components/page-layout'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import { useAllSecretsForProject, useDeleteSecret } from '@/queries/secrets'
import { useResourcePermissions } from '@/queries/permissions'
import { namespaceForProject } from '@/lib/scope-labels'
import {
  createNamespacedResourcePermission,
  hasPermission,
} from '@/lib/resource-permissions'

export const Route = createFileRoute('/_authenticated/projects/$projectName/secrets/')({
  validateSearch: parseGridSearch,
  component: SecretsListPage,
})

export function SecretsListPage() {
  const { projectName } = Route.useParams()
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })

  const { data: secretRows = [], isPending, error } = useAllSecretsForProject(projectName)

  const deleteMutation = useDeleteSecret(projectName)

  const namespace = namespaceForProject(projectName)
  const createPermission = useMemo(
    () => createNamespacedResourcePermission('', 'secrets', namespace),
    [namespace],
  )
  const permissionsQuery = useResourcePermissions([createPermission])
  const canCreate = hasPermission(permissionsQuery.data, createPermission)

  // Map SecretRow → ResourceGrid Row.
  // HOL-990 AC1.1: Resource ID is the bare metadata.name, not a composite
  // "<scope>/<name>" string. Scope is still rendered in the Parent column.
  const rows: Row[] = secretRows.map(({ secret, scope }) => ({
    kind: 'Secret',
    name: secret.name,
    namespace: scope,
    id: secret.name,
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
    <StandardPageLayout
      titleParts={[projectName, 'Secrets']}
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
