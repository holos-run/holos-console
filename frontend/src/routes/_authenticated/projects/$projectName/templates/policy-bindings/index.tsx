/**
 * Project-scoped Templates / Policy Bindings index (HOL-1009).
 *
 * TemplatePolicyBindings are org/folder-scoped, not project-scoped. The
 * namespace is derived from the selected organization via useOrg().selectedOrg.
 * The project name still appears in the URL so the Templates collapsible group
 * can stay open in the sidebar. The collapsible Templates sidebar was
 * implemented in HOL-1014.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { ResourceGrid } from '@/components/resource-grid/ResourceGrid'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import {
  useListTemplatePolicyBindings,
  useDeleteTemplatePolicyBinding,
} from '@/queries/templatePolicyBindings'
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
  '/_authenticated/projects/$projectName/templates/policy-bindings/',
)({
  validateSearch: parseGridSearch,
  component: TemplatePolicyBindingsIndexRoute,
})

function TemplatePolicyBindingsIndexRoute() {
  const { projectName } = Route.useParams()
  return <TemplatePolicyBindingsIndexPage projectName={projectName} />
}

// ---------------------------------------------------------------------------
// Page component (exported for tests)
// ---------------------------------------------------------------------------

export function TemplatePolicyBindingsIndexPage({
  projectName,
}: {
  projectName: string
}) {
  const search = Route.useSearch() as ResourceGridSearch
  const navigate = useNavigate({ from: Route.fullPath })

  // TemplatePolicyBindings are org/folder-scoped — namespace comes from the
  // selected org, not the project. The project param keeps Templates sidebar
  // active in the sidebar (HOL-1014).
  const { selectedOrg } = useOrg()
  const namespace = namespaceForOrg(selectedOrg ?? '')

  const {
    data: bindings = [],
    isPending,
    error,
  } = useListTemplatePolicyBindings(namespace)

  const deleteMutation = useDeleteTemplatePolicyBinding(namespace)

  // ---------------------------------------------------------------------------
  // Build rows
  // ---------------------------------------------------------------------------

  const rows: Row[] = useMemo(
    () =>
      bindings.map((b) => ({
        kind: 'TemplatePolicyBinding',
        name: b.name,
        namespace,
        id: b.name,
        parentId: selectedOrg ?? '',
        parentLabel: selectedOrg ?? '',
        displayName: b.displayName || b.name,
        description: b.description ?? '',
        createdAt: timestampToISOString(b.createdAt),
        detailHref: `/organizations/${selectedOrg}/template-bindings/${b.name}`,
      })),
    [bindings, namespace, selectedOrg],
  )

  // ---------------------------------------------------------------------------
  // Kind definitions
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'TemplatePolicyBinding',
        label: 'TemplatePolicyBinding',
        // No create in this view — bindings are created from org-level pages.
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
      title={`${projectName} / Templates / Policy Bindings`}
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
