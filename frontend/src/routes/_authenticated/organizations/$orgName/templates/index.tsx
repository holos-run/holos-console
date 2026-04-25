import { useState, useMemo } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import {
  useReactTable,
  getCoreRowModel,
  getFilteredRowModel,
  flexRender,
  createColumnHelper,
} from '@tanstack/react-table'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useAllTemplatesForOrg } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'
import type { Template } from '@/gen/holos/console/v1/templates_pb'
import {
  scopeDisplayLabel,
  scopeLabelFromNamespace,
  scopeNameFromNamespace,
} from '@/lib/scope-labels'

export const Route = createFileRoute('/_authenticated/organizations/$orgName/templates/')({
  component: OrgTemplatesIndexRoute,
})

function OrgTemplatesIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplatesIndexPage orgName={orgName} />
}

const columnHelper = createColumnHelper<Template>()

// HOL-793: the scope filter narrows the grid to rows of a single scope. The
// `project` option is shown even though templates at project scope are
// browsed from the project sidebar — the org-level view is for discovery
// across all scopes, and filtering lets users zero in on one.
type ScopeFilter = 'all' | 'org' | 'folder' | 'project'

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

  const { data, isPending, error } = useAllTemplatesForOrg(orgName)
  const { data: org } = useGetOrganization(orgName)
  // When orgName is empty the fan-out is effectively disabled: isPending is
  // false (idle) and data is `[]`. The skeleton branch still needs to cover
  // the authenticated-but-resolving case, so gate on isPending AND data not
  // yet materialized.
  const templates = useMemo(() => data ?? [], [data])

  const userRole = org?.userRole ?? Role.VIEWER
  // Creation at org scope requires OWNER. Folder/project-scope creates use
  // their own scoped routes (HOL-793 explicitly leaves those flows alone).
  const canWrite = userRole === Role.OWNER

  const [globalFilter, setGlobalFilter] = useState('')
  const [scopeFilter, setScopeFilter] = useState<ScopeFilter>('all')

  const rows = useMemo(() => {
    if (scopeFilter === 'all') return templates
    return templates.filter(
      (t) => scopeLabelFromNamespace(t.namespace) === scopeFilter,
    )
  }, [templates, scopeFilter])

  const columns = useMemo(
    () => [
      columnHelper.accessor((row) => row.displayName || row.name, {
        id: 'displayName',
        header: 'Display Name',
        cell: ({ row }) => {
          const t = row.original
          const label = t.displayName || t.name
          const scope = scopeLabelFromNamespace(t.namespace)
          // Scope-aware link. A namespace that does not match any known
          // prefix renders as plain text so we never forge a link to a 404.
          if (scope === 'org') {
            return (
              <Link
                to="/organizations/$orgName/templates/$namespace/$name"
                params={{ orgName, namespace: t.namespace, name: t.name }}
                title={t.name}
                className="hover:underline font-medium"
              >
                {label}
              </Link>
            )
          }
          if (scope === 'folder') {
            const folderName = scopeNameFromNamespace(t.namespace)
            if (folderName) {
              return (
                <Link
                  to="/folders/$folderName/templates/$templateName"
                  params={{ folderName, templateName: t.name }}
                  title={t.name}
                  className="hover:underline font-medium"
                >
                  {label}
                </Link>
              )
            }
          }
          if (scope === 'project') {
            const projectName = scopeNameFromNamespace(t.namespace)
            if (projectName) {
              return (
                <Link
                  to="/projects/$projectName/templates/$templateName"
                  params={{ projectName, templateName: t.name }}
                  title={t.name}
                  className="hover:underline font-medium"
                >
                  {label}
                </Link>
              )
            }
          }
          return (
            <span className="font-medium" title={t.name}>
              {label}
            </span>
          )
        },
      }),
      columnHelper.accessor((row) => scopeCellText(row.namespace), {
        id: 'scope',
        header: 'Scope',
        cell: ({ row }) => <ScopeBadge namespace={row.original.namespace} />,
      }),
      columnHelper.accessor('namespace', {
        header: 'Namespace',
        cell: ({ getValue }) => (
          <span className="text-muted-foreground font-mono text-sm">
            {getValue()}
          </span>
        ),
      }),
      columnHelper.accessor('name', {
        header: 'Name',
        cell: ({ getValue }) => (
          <span className="text-muted-foreground font-mono text-sm">
            {getValue()}
          </span>
        ),
      }),
    ],
    [orgName],
  )

  const table = useReactTable({
    data: rows,
    columns,
    state: { globalFilter },
    onGlobalFilterChange: setGlobalFilter,
    globalFilterFn: 'includesString',
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  })

  const queryDisabled = orgName === ''
  if (isPending || queryDisabled) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Templates</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-2" data-testid="templates-loading">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        </CardContent>
      </Card>
    )
  }

  // Fall through to the full grid when the fan-out has both an error and
  // partial data, so successfully-loaded rows remain visible. The banner
  // below the header surfaces the error without blanking the table.
  if (error && templates.length === 0) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive">
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
        <div>
          <p className="text-sm text-muted-foreground">
            {orgName} / Templates
          </p>
          <CardTitle className="mt-1">Templates</CardTitle>
        </div>
        {canWrite && (
          <Link to="/organizations/$orgName/templates/new" params={{ orgName }}>
            <Button size="sm">Create Template</Button>
          </Link>
        )}
      </CardHeader>
      <CardContent>
        {error && (
          <Alert
            variant="destructive"
            className="mb-4"
            data-testid="templates-partial-error"
          >
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        )}
        {templates.length === 0 ? (
          <div className="flex flex-col items-center gap-3 py-8 text-center">
            <p className="text-muted-foreground">
              No templates yet.
              {canWrite
                ? ' Create one to get started.'
                : ' Ask an organization owner to create one.'}
            </p>
          </div>
        ) : (
          <>
            <div className="mb-3 flex flex-col sm:flex-row gap-2 sm:items-center">
              <Input
                placeholder="Search templates…"
                value={globalFilter}
                onChange={(e) => setGlobalFilter(e.target.value)}
                className="max-w-sm"
                aria-label="Search templates"
              />
              <Select
                value={scopeFilter}
                onValueChange={(v) => setScopeFilter(v as ScopeFilter)}
              >
                <SelectTrigger
                  className="w-[180px]"
                  aria-label="Filter by scope"
                >
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
            {rows.length === 0 && (
              <div className="mb-3 rounded-md border border-dashed border-border p-4 text-center">
                <p className="text-sm text-muted-foreground">
                  No templates match the current filters.
                </p>
              </div>
            )}
            <Table>
              <TableHeader>
                {table.getHeaderGroups().map((headerGroup) => (
                  <TableRow key={headerGroup.id}>
                    {headerGroup.headers.map((header) => (
                      <TableHead key={header.id}>
                        {header.isPlaceholder
                          ? null
                          : flexRender(
                              header.column.columnDef.header,
                              header.getContext(),
                            )}
                      </TableHead>
                    ))}
                  </TableRow>
                ))}
              </TableHeader>
              <TableBody>
                {table.getRowModel().rows.map((row) => (
                  <TableRow key={row.id}>
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext(),
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </>
        )}
      </CardContent>
    </Card>
  )
}

function scopeCellText(namespace: string): string {
  const label = scopeDisplayLabel(namespace)
  const name = scopeNameFromNamespace(namespace)
  if (!label) return ''
  return name ? `${label}: ${name}` : label
}

function ScopeBadge({ namespace }: { namespace: string }) {
  const label = scopeDisplayLabel(namespace)
  const name = scopeNameFromNamespace(namespace)
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
}
