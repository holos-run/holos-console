import { useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import {
  useReactTable,
  getCoreRowModel,
  getFilteredRowModel,
  flexRender,
  createColumnHelper,
} from '@tanstack/react-table'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
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
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useAllTemplatePolicyBindingsForOrg } from '@/queries/templatePolicyBindings'
import type { TemplatePolicyBinding } from '@/queries/templatePolicyBindings'
import { useGetOrganization } from '@/queries/organizations'
import {
  scopeDisplayLabel,
  scopeLabelFromNamespace,
  scopeNameFromNamespace,
} from '@/lib/scope-labels'

export const Route = createFileRoute(
  '/_authenticated/orgs/$orgName/template-policy-bindings/',
)({
  component: OrgTemplatePolicyBindingsIndexRoute,
})

function OrgTemplatePolicyBindingsIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplatePolicyBindingsIndexPage orgName={orgName} />
}

const columnHelper = createColumnHelper<TemplatePolicyBinding>()

// Scope filter values. `all` means no filtering; `org` / `folder` match the
// ScopeLabel returned by scopeLabelFromNamespace. `project` is intentionally
// omitted because bindings do not exist at project scope (HOL-590), but the
// option still appears in the filter so users learn the constraint from the
// empty-result state rather than the UI silently hiding an option.
type ScopeFilter = 'all' | 'org' | 'folder' | 'project'

export function OrgTemplatePolicyBindingsIndexPage({
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

  const { data: bindings, isPending, error } =
    useAllTemplatePolicyBindingsForOrg(orgName)
  const { data: org } = useGetOrganization(orgName)

  const userRole = org?.userRole ?? Role.VIEWER
  // PERMISSION_TEMPLATE_POLICIES_WRITE cascades to editors too (bindings reuse
  // the policy permission family — see HOL-595).
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  const [globalFilter, setGlobalFilter] = useState('')
  const [scopeFilter, setScopeFilter] = useState<ScopeFilter>('all')

  const rows = useMemo(() => {
    const all = bindings ?? []
    if (scopeFilter === 'all') return all
    return all.filter((b) => scopeLabelFromNamespace(b.namespace) === scopeFilter)
  }, [bindings, scopeFilter])

  const columns = useMemo(
    () => [
      columnHelper.accessor((row) => row.displayName || row.name, {
        id: 'name',
        header: 'Name',
        cell: ({ row }) => {
          const b = row.original
          const label = b.displayName || b.name
          const scope = scopeLabelFromNamespace(b.namespace)
          // Scope-aware link: bindings live at org or folder scope. A
          // namespace that fails both prefix checks (stale cache, proto
          // drift) renders as plain text rather than forging a link that
          // would 404.
          if (scope === 'folder') {
            const folderName = scopeNameFromNamespace(b.namespace)
            if (folderName) {
              return (
                <Link
                  to="/folders/$folderName/template-policy-bindings/$bindingName"
                  params={{ folderName, bindingName: b.name }}
                  title={b.name}
                  className="hover:underline font-medium"
                >
                  {label}
                </Link>
              )
            }
          } else if (scope === 'org') {
            return (
              <Link
                to="/orgs/$orgName/template-policy-bindings/$bindingName"
                params={{ orgName, bindingName: b.name }}
                title={b.name}
                className="hover:underline font-medium"
              >
                {label}
              </Link>
            )
          }
          return (
            <span className="font-medium" title={b.name}>
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
      columnHelper.accessor((row) => row.policyRef?.name ?? '', {
        id: 'policy',
        header: 'Policy',
        cell: ({ row }) => {
          const p = row.original.policyRef
          if (!p?.name) {
            return <span className="text-muted-foreground">—</span>
          }
          return (
            <span className="font-mono text-sm text-blue-500">{p.name}</span>
          )
        },
      }),
      columnHelper.accessor((row) => row.targetRefs.length, {
        id: 'targets',
        header: 'Targets',
        cell: ({ getValue }) => {
          const n = getValue() as number
          return (
            <Badge variant="outline" className="text-xs">
              {n} target{n === 1 ? '' : 's'}
            </Badge>
          )
        },
      }),
      columnHelper.accessor('description', {
        header: 'Description',
        cell: ({ getValue }) => {
          const d = getValue()
          if (!d) return <span className="text-muted-foreground">—</span>
          return (
            <span className="text-sm text-muted-foreground truncate">{d}</span>
          )
        },
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

  if (isPending) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Template Policy Bindings</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-2" data-testid="bindings-loading">
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
  if (error && (bindings ?? []).length === 0) {
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
            {orgName} / Template Policy Bindings
          </p>
          <CardTitle className="mt-1">Template Policy Bindings</CardTitle>
        </div>
        {canWrite && (
          <Link
            to="/orgs/$orgName/template-policy-bindings/new"
            params={{ orgName }}
          >
            <Button size="sm">Create Binding</Button>
          </Link>
        )}
      </CardHeader>
      <CardContent>
        {error && (
          <Alert
            variant="destructive"
            className="mb-4"
            data-testid="bindings-partial-error"
          >
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        )}
        {(bindings ?? []).length === 0 ? (
          <div className="rounded-md border border-dashed border-border p-6 text-center">
            <p className="text-sm font-medium">
              No template policy bindings yet.
            </p>
            <p className="mt-1 text-sm text-muted-foreground">
              Bindings attach a single TemplatePolicy to project templates and
              deployments via target refs. Bindings live only at folder or
              organization scope.
            </p>
          </div>
        ) : (
          <>
            <div className="mb-3 flex flex-col sm:flex-row gap-2 sm:items-center">
              <Input
                placeholder="Search bindings…"
                value={globalFilter}
                onChange={(e) => setGlobalFilter(e.target.value)}
                className="max-w-sm"
                aria-label="Search template policy bindings"
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
            {rows.length === 0 ? (
              <div className="rounded-md border border-dashed border-border p-6 text-center">
                <p className="text-sm text-muted-foreground">
                  No bindings match the current filters.
                </p>
              </div>
            ) : (
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
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}

// scopeCellText supplies the string the global search filter matches against
// when the user types a scope label. Having an accessor that returns text
// (rather than a ReactNode cell) lets `includesString` search this column.
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
