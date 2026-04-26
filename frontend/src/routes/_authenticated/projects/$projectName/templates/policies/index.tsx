/**
 * Project-scoped Templates / Policies index (HOL-1009).
 *
 * TemplatePolicies are org/folder-scoped, not project-scoped. The namespace
 * is derived from the selected organization via useOrg().selectedOrg. The
 * project name still appears in the URL so the Templates collapsible group
 * can stay open in the sidebar. The collapsible Templates sidebar was
 * implemented in HOL-1014.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { ResourceGrid } from '@/components/resource-grid/ResourceGrid'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import { useListTemplatePolicies, useDeleteTemplatePolicy } from '@/queries/templatePolicies'
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
  '/_authenticated/projects/$projectName/templates/policies/',
)({
  validateSearch: parseGridSearch,
  component: TemplatePoliciesIndexRoute,
})

function TemplatePoliciesIndexRoute() {
  const { projectName } = Route.useParams()
  return <TemplatePoliciesIndexPage projectName={projectName} />
}

// ---------------------------------------------------------------------------
// Page component (exported for tests)
// ---------------------------------------------------------------------------

export function TemplatePoliciesIndexPage({
  projectName,
}: {
  projectName: string
}) {
  const search = Route.useSearch() as ResourceGridSearch
  const navigate = useNavigate({ from: Route.fullPath })

  // TemplatePolicies are org/folder-scoped — namespace comes from the selected
  // org, not the project. The project param keeps Templates sidebar active.
  const { selectedOrg } = useOrg()
  const namespace = namespaceForOrg(selectedOrg ?? '')

  const {
    data: policies = [],
    isPending,
    error,
  } = useListTemplatePolicies(namespace)

  const deleteMutation = useDeleteTemplatePolicy(namespace)

  // ---------------------------------------------------------------------------
  // Build rows
  // ---------------------------------------------------------------------------

  const rows: Row[] = useMemo(
    () =>
      policies.map((p) => ({
        kind: 'TemplatePolicy',
        name: p.name,
        namespace,
        id: p.name,
        parentId: selectedOrg ?? '',
        parentLabel: selectedOrg ?? '',
        displayName: p.displayName || p.name,
        description: p.description ?? '',
        createdAt: timestampToISOString(p.createdAt),
        detailHref: `/organizations/${selectedOrg}/template-policies/${p.name}`,
      })),
    [policies, namespace, selectedOrg],
  )

  // ---------------------------------------------------------------------------
  // Kind definitions
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'TemplatePolicy',
        label: 'TemplatePolicy',
        // No create in this view — policies are created from org-level pages.
        canCreate: false,
      },
    ],
    [],
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
    <ResourceGrid
      title={`${projectName} / Templates / Policies`}
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
