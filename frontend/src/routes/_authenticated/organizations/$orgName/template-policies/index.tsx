/**
 * Organization-scoped TemplatePolicy index — migrated to ResourceGrid v1 (HOL-948).
 *
 * Shows org-scoped policies only. The RPC is called with the org namespace directly
 * so only org-scoped policies are returned (HOL-917). Folder-scoped policy browsing
 * lives on the folder detail pages.
 *
 * Extra columns:
 *   - Scope  — badge showing org or folder scope
 *   - Rules  — REQUIRE / EXCLUDE rule counts
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Badge } from '@/components/ui/badge'
import { ResourceGrid } from '@/components/resource-grid/ResourceGrid'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import {
  useListTemplatePolicies,
  useDeleteTemplatePolicy,
  countRulesByKind,
} from '@/queries/templatePolicies'
import type { TemplatePolicy } from '@/queries/templatePolicies'
import { useResourcePermissions } from '@/queries/permissions'
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
import {
  createTemplateResourcePermission,
  hasPermission,
  templateResources,
} from '@/lib/resource-permissions'

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/template-policies/',
)({
  validateSearch: parseGridSearch,
  component: OrgTemplatePoliciesIndexRoute,
})

function OrgTemplatePoliciesIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplatePoliciesIndexPage orgName={orgName} />
}

// ---------------------------------------------------------------------------
// Extra columns — Scope badge and rule counts
// ---------------------------------------------------------------------------

function usePolicyExtraColumns(
  policiesByName: Map<string, TemplatePolicy>,
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
        id: 'rules',
        header: 'Rules',
        accessorFn: (row: Row) => {
          const p = policiesByName.get(`${row.namespace}/${row.name}`)
          const counts = countRulesByKind(p)
          return `${counts.require}R ${counts.exclude}E`
        },
        cell: ({ row }: { row: { original: Row } }) => {
          const p = policiesByName.get(`${row.original.namespace}/${row.original.name}`)
          const counts = countRulesByKind(p)
          return (
            <div className="flex items-center gap-1">
              {counts.require > 0 && (
                <Badge variant="outline" className="text-xs font-mono">
                  {counts.require} REQUIRE
                </Badge>
              )}
              {counts.exclude > 0 && (
                <Badge variant="destructive" className="text-xs font-mono">
                  {counts.exclude} EXCLUDE
                </Badge>
              )}
              {counts.require === 0 && counts.exclude === 0 && (
                <span className="text-muted-foreground text-xs">—</span>
              )}
            </div>
          )
        },
      },
    ],
    [policiesByName],
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

export function OrgTemplatePoliciesIndexPage({
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
  const { data: policies = [], isPending, error } = useListTemplatePolicies(orgNamespace)
  const deleteMutation = useDeleteTemplatePolicy(orgNamespace)

  const createPermission = useMemo(
    () => createTemplateResourcePermission(templateResources.templatePolicies, orgNamespace),
    [orgNamespace],
  )
  const permissionsQuery = useResourcePermissions([createPermission])
  const canCreate = hasPermission(permissionsQuery.data, createPermission)

  // Build a lookup map for extra columns to access the original policy object.
  const policiesByName = useMemo(() => {
    const map = new Map<string, TemplatePolicy>()
    for (const p of policies) {
      if (p) map.set(`${p.namespace}/${p.name}`, p)
    }
    return map
  }, [policies])

  // Map TemplatePolicy → ResourceGrid Row
  const rows: Row[] = useMemo(
    () =>
      policies.map((p) => ({
        kind: 'TemplatePolicy',
        name: p.name,
        namespace: p.namespace,
        id: `${p.namespace}/${p.name}`,
        parentId: p.namespace,
        parentLabel: parentLabelFromNamespace(p.namespace),
        displayName: p.displayName || p.name,
        description: p.description ?? '',
        createdAt: timestampToISOString(p.createdAt),
        detailHref: resolveTemplateRowHref('TemplatePolicy', p.namespace, p.name),
      })),
    [policies],
  )

  const kinds = useMemo(
    () => [
      {
        id: 'TemplatePolicy',
        label: 'Template Policy',
        newHref: `/organizations/${orgName}/template-policies/new`,
        canCreate,
      },
    ],
    [orgName, canCreate],
  )

  const extraColumns = usePolicyExtraColumns(policiesByName)

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
      title={`${orgName} / Template Policies`}
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
