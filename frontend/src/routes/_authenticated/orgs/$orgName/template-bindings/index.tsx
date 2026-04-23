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
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useListTemplatePolicyBindings } from '@/queries/templatePolicyBindings'
import type { TemplatePolicyBinding } from '@/queries/templatePolicyBindings'
import { useGetOrganization } from '@/queries/organizations'
import {
  scopeDisplayLabel,
  scopeLabelFromNamespace,
  scopeNameFromNamespace,
  namespaceForOrg,
} from '@/lib/scope-labels'

export const Route = createFileRoute(
  '/_authenticated/orgs/$orgName/template-bindings/',
)({
  component: OrgTemplateBindingsIndexRoute,
})

function OrgTemplateBindingsIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplateBindingsIndexPage orgName={orgName} />
}

const columnHelper = createColumnHelper<TemplatePolicyBinding>()

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

  // This page is org-scoped only. The RPC is called with the org namespace
  // directly so only org-scoped bindings are returned. Folder-scoped bindings
  // are excluded — the folder-scoped index remains at
  // /folders/$folderName/template-policy-bindings.
  const orgNamespace = namespaceForOrg(orgName)
  const { data: bindings, isPending, error } =
    useListTemplatePolicyBindings(orgNamespace)
  const { data: org } = useGetOrganization(orgName)

  const userRole = org?.userRole ?? Role.VIEWER
  // PERMISSION_TEMPLATE_POLICIES_WRITE cascades to editors too (bindings reuse
  // the policy permission family — see HOL-595).
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  const [globalFilter, setGlobalFilter] = useState('')

  const rows = useMemo(() => {
    return bindings ?? []
  }, [bindings])

  const columns = useMemo(
    () => [
      columnHelper.accessor((row) => row.displayName || row.name, {
        id: 'name',
        header: 'Name',
        cell: ({ row }) => {
          const b = row.original
          const label = b.displayName || b.name
          const scope = scopeLabelFromNamespace(b.namespace)
          // This page shows org-scoped bindings only (namespaceForOrg). Any
          // row from a folder namespace would indicate stale cache / proto
          // drift — render as plain text to avoid a broken link.
          if (scope === 'org') {
            return (
              <Link
                to="/orgs/$orgName/template-bindings/$bindingName"
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
          <CardTitle>Template Bindings</CardTitle>
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
            {orgName} / Template Bindings
          </p>
          <CardTitle className="mt-1">Template Bindings</CardTitle>
        </div>
        {canWrite && (
          <Link
            to="/orgs/$orgName/template-bindings/new"
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
              No template bindings yet.
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
                aria-label="Search template bindings"
              />
            </div>
            {rows.length === 0 ? (
              <div className="rounded-md border border-dashed border-border p-6 text-center">
                <p className="text-sm text-muted-foreground">
                  No bindings match the current search.
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
