/**
 * Org-scope Platform Templates index — unified four-facet surface (HOL-1006).
 *
 * Shows all template-family resources reachable from the org root across three
 * kinds — Template, TemplatePolicy, TemplatePolicyBinding — via fan-out hooks.
 * The ResourceGrid kind-filter toolbar lets users narrow to a single kind; the
 * scope dropdown further narrows by org / folder scope.
 *
 * Each row carries a scope-aware detailHref so both the resource ID cell and
 * the full row are clickable (using resolveTemplateRowHref from
 * lib/template-row-link.ts).
 *
 * Extra columns added via the ResourceGrid `extraColumns` prop:
 *   - Scope     — badge indicating org / folder / project + the owner name
 *   - Namespace — raw namespace string for operator debugging
 *
 * A scope dropdown filter is rendered above the grid via the ResourceGrid
 * `headerContent` slot. Selecting a scope replaces the URL `?scope=` param
 * so the filter survives refreshes.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { ResourceGrid } from '@/components/resource-grid/ResourceGrid'
import type { Row } from '@/components/resource-grid/types'
import type { ColumnDef } from '@tanstack/react-table'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import { useAllTemplatesForOrg } from '@/queries/templates'
import { useAllTemplatePoliciesForOrg } from '@/queries/templatePolicies'
import { useAllTemplatePolicyBindingsForOrg } from '@/queries/templatePolicyBindings'
import { useGetOrganization } from '@/queries/organizations'
import {
  scopeLabelFromNamespace,
  scopeNameFromNamespace,
  scopeDisplayLabel,
} from '@/lib/scope-labels'
import {
  resolveTemplateRowHref,
  parentLabelFromNamespace,
} from '@/lib/template-row-link'

// ---------------------------------------------------------------------------
// Route search — extends ResourceGridSearch with the scope filter
// ---------------------------------------------------------------------------

export interface OrgTemplatesSearch extends ResourceGridSearch {
  /** Optional scope filter: 'org' | 'folder'. Absent = all scopes. */
  scope?: 'org' | 'folder'
}

type ScopeFilter = 'all' | 'org' | 'folder'

function parseOrgTemplatesSearch(raw: Record<string, unknown>): OrgTemplatesSearch {
  const base = parseGridSearch(raw)
  const result: OrgTemplatesSearch = { ...base }
  const scope = raw['scope']
  if (scope === 'org' || scope === 'folder') {
    result.scope = scope
  }
  return result
}

// ---------------------------------------------------------------------------
// Route definition
// ---------------------------------------------------------------------------

export const Route = createFileRoute('/_authenticated/organizations/$orgName/templates/')({
  validateSearch: parseOrgTemplatesSearch,
  component: OrgTemplatesIndexRoute,
})

function OrgTemplatesIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplatesIndexPage orgName={orgName} />
}

// ---------------------------------------------------------------------------
// Extra columns: Scope badge + Namespace
// ---------------------------------------------------------------------------

const extraColumns: ColumnDef<Row>[] = [
  {
    id: 'scope',
    header: 'Scope',
    enableSorting: false,
    accessorFn: (row) => row.namespace,
    cell: ({ row }) => {
      const ns = row.original.namespace
      const label = scopeDisplayLabel(ns)
      const name = scopeNameFromNamespace(ns)
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
    id: 'namespace',
    header: 'Namespace',
    enableSorting: false,
    accessorFn: (row) => row.namespace,
    cell: ({ row }) => (
      <span className="text-muted-foreground font-mono text-sm">
        {row.original.namespace}
      </span>
    ),
  },
]

// ---------------------------------------------------------------------------
// Page component (exported for tests)
// ---------------------------------------------------------------------------

export function OrgTemplatesIndexPage({
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

  const search = Route.useSearch() as OrgTemplatesSearch
  const navigate = useNavigate({ from: Route.fullPath })

  const scopeFilter: ScopeFilter = search.scope ?? 'all'

  // Fan-out across all namespaces reachable from the org root.
  const templatesResult = useAllTemplatesForOrg(orgName)
  const policiesResult = useAllTemplatePoliciesForOrg(orgName)
  const bindingsResult = useAllTemplatePolicyBindingsForOrg(orgName)
  const { data: org } = useGetOrganization(orgName)

  const templates = useMemo(() => templatesResult.data ?? [], [templatesResult.data])
  const policies = useMemo(() => policiesResult.data ?? [], [policiesResult.data])
  const bindings = useMemo(() => bindingsResult.data ?? [], [bindingsResult.data])

  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  // Combine loading and error state across all three fan-outs.
  const isPending = templatesResult.isPending || policiesResult.isPending || bindingsResult.isPending
  const firstError = templatesResult.error ?? policiesResult.error ?? bindingsResult.error

  // ---------------------------------------------------------------------------
  // Build rows — scope-filtered, with scope-aware detailHref
  // ---------------------------------------------------------------------------

  const rows: Row[] = useMemo(() => {
    const templateRows: Row[] = templates.flatMap((t) => {
      const scope = scopeLabelFromNamespace(t.namespace)
      const scopeName = scopeNameFromNamespace(t.namespace)

      // Skip rows outside the active scope filter.
      if (scopeFilter !== 'all' && scope !== scopeFilter) return []

      const row: Row = {
        kind: 'Template',
        name: t.name,
        namespace: t.namespace,
        id: t.name,
        parentId: scopeName || t.namespace,
        parentLabel: parentLabelFromNamespace(t.namespace),
        displayName: t.displayName || t.name,
        description: t.description ?? '',
        createdAt: t.createdAt ?? '',
        detailHref: resolveTemplateRowHref('Template', t.namespace, t.name),
      }
      return [row]
    })

    const policyRows: Row[] = policies.flatMap((p) => {
      if (!p) return []
      const scope = scopeLabelFromNamespace(p.namespace)
      if (scopeFilter !== 'all' && scope !== scopeFilter) return []

      const createdAt = p.createdAt
        ? new Date(Number(p.createdAt.seconds) * 1000).toISOString()
        : ''

      const row: Row = {
        kind: 'TemplatePolicy',
        name: p.name,
        namespace: p.namespace,
        id: `${p.namespace}/${p.name}`,
        parentId: p.namespace,
        parentLabel: parentLabelFromNamespace(p.namespace),
        displayName: p.displayName || p.name,
        description: p.description ?? '',
        createdAt,
        detailHref: resolveTemplateRowHref('TemplatePolicy', p.namespace, p.name),
      }
      return [row]
    })

    const bindingRows: Row[] = bindings.flatMap((b) => {
      if (!b) return []
      const scope = scopeLabelFromNamespace(b.namespace)
      if (scopeFilter !== 'all' && scope !== scopeFilter) return []

      const createdAt = b.createdAt
        ? new Date(Number(b.createdAt.seconds) * 1000).toISOString()
        : ''

      const row: Row = {
        kind: 'TemplatePolicyBinding',
        name: b.name,
        namespace: b.namespace,
        id: `${b.namespace}/${b.name}`,
        parentId: b.namespace,
        parentLabel: parentLabelFromNamespace(b.namespace),
        displayName: b.displayName || b.name,
        description: b.description ?? '',
        createdAt,
        detailHref: resolveTemplateRowHref('TemplatePolicyBinding', b.namespace, b.name),
      }
      return [row]
    })

    return [...templateRows, ...policyRows, ...bindingRows]
  }, [templates, policies, bindings, scopeFilter])

  // ---------------------------------------------------------------------------
  // Kind definitions (three kinds for the unified surface)
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'Template',
        label: 'Template',
        newHref: `/organizations/${orgName}/templates/new`,
        canCreate: canWrite,
      },
      {
        id: 'TemplatePolicy',
        label: 'Template Policy',
        newHref: `/organizations/${orgName}/template-policies/new`,
        canCreate: canWrite,
      },
      {
        id: 'TemplatePolicyBinding',
        label: 'Template Binding',
        newHref: `/organizations/${orgName}/template-bindings/new`,
        canCreate: canWrite,
      },
    ],
    [orgName, canWrite],
  )

  // ---------------------------------------------------------------------------
  // Handlers
  // ---------------------------------------------------------------------------

  // The unified index spans multiple kinds; deletion is handled from each
  // resource's own detail page. This index is read-only for delete actions.
  const handleDelete = useCallback(async () => {
    throw new Error('Delete from the resource detail page.')
  }, [])

  const handleSearchChange = useCallback(
    (updater: (prev: ResourceGridSearch) => ResourceGridSearch) => {
      navigate({
        search: (prev) => {
          const typedPrev = prev as OrgTemplatesSearch
          const updated = updater(typedPrev)
          const next: OrgTemplatesSearch = { ...updated }
          if (typedPrev.scope) {
            next.scope = typedPrev.scope
          }
          return next
        },
      })
    },
    [navigate],
  )

  const handleScopeChange = useCallback(
    (value: string) => {
      const scope = value as ScopeFilter
      navigate({
        search: (prev) => {
          const typedPrev = prev as OrgTemplatesSearch
          const next: OrgTemplatesSearch = { ...typedPrev }
          if (scope === 'all') {
            delete next.scope
          } else {
            next.scope = scope as 'org' | 'folder'
          }
          return next
        },
      })
    },
    [navigate],
  )

  // ---------------------------------------------------------------------------
  // Scope filter toolbar content (rendered in ResourceGrid headerContent slot)
  // Note: TemplatePolicies and TemplatePolicyBindings only exist at org and
  // folder scope — project is intentionally absent from the dropdown.
  // ---------------------------------------------------------------------------

  const scopeFilterContent = (
    <div className="flex items-center gap-2 pb-3">
      <Select value={scopeFilter} onValueChange={handleScopeChange}>
        <SelectTrigger className="w-[180px]" aria-label="Filter by scope">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All scopes</SelectItem>
          <SelectItem value="org">Organization</SelectItem>
          <SelectItem value="folder">Folder</SelectItem>
        </SelectContent>
      </Select>
    </div>
  )

  return (
    <ResourceGrid
      title={`${orgName} / Templates`}
      kinds={kinds}
      rows={rows}
      onDelete={handleDelete}
      isLoading={isPending || orgName === ''}
      error={firstError}
      search={search}
      onSearchChange={handleSearchChange}
      extraColumns={extraColumns}
      headerContent={scopeFilterContent}
      sortableColumns={['createdAt']}
    />
  )
}
