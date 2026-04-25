/**
 * Organization-scoped TemplatePolicyBinding index — migrated to ResourceGrid v1 (HOL-948).
 *
 * Shows org-scoped bindings only. The RPC is called with the org namespace directly
 * so only org-scoped bindings are returned. Folder-scoped binding browsing lives on
 * the folder detail pages.
 *
 * Extra columns:
 *   - Scope    — badge showing org or folder scope
 *   - Policy   — name of the bound TemplatePolicy
 *   - Targets  — count of target refs
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Badge } from '@/components/ui/badge'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { ResourceGrid } from '@/components/resource-grid/ResourceGrid'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import {
  useListTemplatePolicyBindings,
  useDeleteTemplatePolicyBinding,
} from '@/queries/templatePolicyBindings'
import type { TemplatePolicyBinding } from '@/queries/templatePolicyBindings'
import { useGetOrganization } from '@/queries/organizations'
import {
  scopeDisplayLabel,
  scopeNameFromNamespace,
  namespaceForOrg,
} from '@/lib/scope-labels'
import {
  resolveTemplateRowHref,
  parentLabelFromNamespace,
} from '@/lib/template-row-link'
import type { ColumnDef } from '@tanstack/react-table'

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-bindings/',
)({
  validateSearch: parseGridSearch,
  component: OrgTemplateBindingsIndexRoute,
})

function OrgTemplateBindingsIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplateBindingsIndexPage orgName={orgName} />
}

// ---------------------------------------------------------------------------
// Extra columns — Scope badge, Policy reference, and target count
// ---------------------------------------------------------------------------

function useBindingExtraColumns(
  bindingsByKey: Map<string, TemplatePolicyBinding>,
): ColumnDef<Row>[] {
  return useMemo(
    () => [
      {
        id: 'scope',
        header: 'Scope',
        accessorFn: (row: Row) => {
          const label = scopeDisplayLabel(row.namespace)
          const name = scopeNameFromNamespace(row.namespace)
          if (!label) return ''
          return name ? `${label}: ${name}` : label
        },
        cell: ({ row }: { row: { original: Row } }) => {
          const label = scopeDisplayLabel(row.original.namespace)
          const name = scopeNameFromNamespace(row.original.namespace)
          if (!label) {
            return (
              <Badge variant="outline" className="text-xs">
                unknown
              </Badge>
            )
          }
          return (
            <Badge variant="outline" className="text-xs">
              {label}
              {name ? `: ${name}` : ''}
            </Badge>
          )
        },
      },
      {
        id: 'policy',
        header: 'Policy',
        accessorFn: (row: Row) => {
          const b = bindingsByKey.get(`${row.namespace}/${row.name}`)
          return b?.policyRef?.name ?? ''
        },
        cell: ({ row }: { row: { original: Row } }) => {
          const b = bindingsByKey.get(`${row.original.namespace}/${row.original.name}`)
          const policyName = b?.policyRef?.name
          if (!policyName) {
            return <span className="text-muted-foreground">—</span>
          }
          return (
            <span className="font-mono text-sm text-blue-500">{policyName}</span>
          )
        },
      },
      {
        id: 'targets',
        header: 'Targets',
        accessorFn: (row: Row) => {
          const b = bindingsByKey.get(`${row.namespace}/${row.name}`)
          return b?.targetRefs?.length ?? 0
        },
        cell: ({ row }: { row: { original: Row } }) => {
          const b = bindingsByKey.get(`${row.original.namespace}/${row.original.name}`)
          const n = b?.targetRefs?.length ?? 0
          return (
            <Badge variant="outline" className="text-xs">
              {n} target{n === 1 ? '' : 's'}
            </Badge>
          )
        },
      },
    ],
    [bindingsByKey],
  )
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

export function OrgTemplateBindingsIndexPage({
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

  const orgNamespace = namespaceForOrg(orgName)
  const { data: bindings = [], isPending, error } = useListTemplatePolicyBindings(orgNamespace)
  const { data: org } = useGetOrganization(orgName)
  const deleteMutation = useDeleteTemplatePolicyBinding(orgNamespace)

  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  // Build a lookup map for extra columns to access the original binding object.
  const bindingsByKey = useMemo(() => {
    const map = new Map<string, TemplatePolicyBinding>()
    for (const b of bindings) {
      if (b) map.set(`${b.namespace}/${b.name}`, b)
    }
    return map
  }, [bindings])

  // Map TemplatePolicyBinding → ResourceGrid Row
  const rows: Row[] = useMemo(
    () =>
      bindings.map((b) => ({
        kind: 'TemplatePolicyBinding',
        name: b.name,
        namespace: b.namespace,
        id: `${b.namespace}/${b.name}`,
        parentId: b.namespace,
        parentLabel: parentLabelFromNamespace(b.namespace),
        displayName: b.displayName || b.name,
        description: b.description ?? '',
        createdAt: timestampToISOString(b.createdAt),
        detailHref: resolveTemplateRowHref('TemplatePolicyBinding', b.namespace, b.name),
      })),
    [bindings],
  )

  const kinds = useMemo(
    () => [
      {
        id: 'TemplatePolicyBinding',
        label: 'Template Binding',
        newHref: `/organizations/${orgName}/template-bindings/new`,
        canCreate: canWrite,
      },
    ],
    [orgName, canWrite],
  )

  const extraColumns = useBindingExtraColumns(bindingsByKey)

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
      title={`${orgName} / Template Bindings`}
      kinds={kinds}
      rows={rows}
      onDelete={handleDelete}
      isLoading={isPending}
      error={error}
      search={search}
      onSearchChange={handleSearchChange}
      extraColumns={extraColumns}
    />
  )
}
