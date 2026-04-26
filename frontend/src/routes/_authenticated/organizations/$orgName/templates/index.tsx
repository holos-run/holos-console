/**
 * Org-scope Platform Templates index — migrated to ResourceGrid v1 (HOL-975).
 *
 * Shows all templates reachable from the org root (org, folder, project scopes)
 * via the useAllTemplatesForOrg fan-out hook. Each row carries a scope-aware
 * detailHref so both the resource ID cell and the full row are clickable.
 *
 * Extra columns added via the ResourceGrid `extraColumns` prop:
 *   - Scope    — badge indicating org / folder / project + the owner name
 *   - Namespace — raw namespace string for operator debugging
 *
 * A scope dropdown filter (preserved from the pre-refactor page) is rendered
 * above the grid via the ResourceGrid `headerContent` slot. Selecting a scope
 * replaces the URL `?scope=` param so the filter survives refreshes.
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
import { useGetOrganization } from '@/queries/organizations'
import {
  scopeLabelFromNamespace,
  scopeNameFromNamespace,
  scopeDisplayLabel,
} from '@/lib/scope-labels'

// ---------------------------------------------------------------------------
// Route search — extends ResourceGridSearch with the scope filter
// ---------------------------------------------------------------------------

export interface OrgTemplatesSearch extends ResourceGridSearch {
  /** Optional scope filter: 'org' | 'folder' | 'project'. Absent = all. */
  scope?: 'org' | 'folder' | 'project'
}

type ScopeFilter = 'all' | 'org' | 'folder' | 'project'

function parseOrgTemplatesSearch(raw: Record<string, unknown>): OrgTemplatesSearch {
  const base = parseGridSearch(raw)
  const result: OrgTemplatesSearch = { ...base }
  const scope = raw['scope']
  if (scope === 'org' || scope === 'folder' || scope === 'project') {
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

  // eslint-disable-next-line react-hooks/rules-of-hooks
  const search = Route.useSearch() as OrgTemplatesSearch
  const navigate = useNavigate({ from: Route.fullPath })

  const scopeFilter: ScopeFilter = search.scope ?? 'all'

  // Fan-out across all namespaces reachable from the org root.
  const { data, isPending, error } = useAllTemplatesForOrg(orgName)
  const { data: org } = useGetOrganization(orgName)

  const templates = useMemo(() => data ?? [], [data])
  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  // ---------------------------------------------------------------------------
  // Build rows — scope-filtered, with scope-aware detailHref
  // ---------------------------------------------------------------------------

  const rows: Row[] = useMemo(() => {
    const all = templates.map((t): Row => {
      const scope = scopeLabelFromNamespace(t.namespace)
      const scopeName = scopeNameFromNamespace(t.namespace)
      let detailHref: string | undefined

      if (scope === 'org') {
        detailHref = `/organizations/${orgName}/templates/${t.namespace}/${t.name}`
      } else if (scope === 'folder' && scopeName) {
        detailHref = `/folders/${scopeName}/templates/${t.name}`
      } else if (scope === 'project' && scopeName) {
        detailHref = `/projects/${scopeName}/templates/${t.name}`
      }

      return {
        kind: 'Template',
        name: t.name,
        namespace: t.namespace,
        id: t.name,
        parentId: scopeName || t.namespace,
        parentLabel: scopeName || t.namespace,
        displayName: t.displayName || t.name,
        description: t.description ?? '',
        createdAt: t.createdAt ?? '',
        detailHref,
      }
    })

    if (scopeFilter === 'all') return all
    return all.filter((r) => scopeLabelFromNamespace(r.namespace) === scopeFilter)
  }, [templates, orgName, scopeFilter])

  // ---------------------------------------------------------------------------
  // Kind definitions
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'Template',
        label: 'Template',
        newHref: `/organizations/${orgName}/templates/new`,
        canCreate: canWrite,
      },
    ],
    [orgName, canWrite],
  )

  // ---------------------------------------------------------------------------
  // Handlers
  // ---------------------------------------------------------------------------

  const handleDelete = useCallback(async (_row: Row) => {
    // Org templates index shows templates from multiple scopes; deletion is
    // handled from each template's own detail/editor page. This index is
    // read-only for delete actions — the ResourceGrid delete button is not
    // rendered when onDelete is undefined, but we provide a no-op so the type
    // is satisfied. Actual deletion is on the detail page.
    throw new Error('Delete from the template detail page.')
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
            next.scope = scope as 'org' | 'folder' | 'project'
          }
          return next
        },
      })
    },
    [navigate],
  )

  // ---------------------------------------------------------------------------
  // Scope filter toolbar content (rendered in ResourceGrid headerContent slot)
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
          <SelectItem value="project">Project</SelectItem>
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
      error={error}
      search={search}
      onSearchChange={handleSearchChange}
      extraColumns={extraColumns}
      headerContent={scopeFilterContent}
      sortableColumns={['createdAt']}
    />
  )
}

