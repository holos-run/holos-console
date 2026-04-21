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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useSearchTemplates } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'
import type { Template } from '@/gen/holos/console/v1/templates_pb'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/templates/')({
  component: OrgTemplatesIndexRoute,
})

function OrgTemplatesIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplatesIndexPage orgName={orgName} />
}

const columnHelper = createColumnHelper<Template>()

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

  const { data, isLoading, error } = useSearchTemplates({ organization: orgName })
  const { data: org } = useGetOrganization(orgName)
  // When orgName is empty the query is disabled: isLoading is false and data is
  // undefined. Treat the disabled state the same as loading so we never render
  // the "No templates yet" empty state for an org that hasn't resolved yet.
  const queryDisabled = orgName === ''
  const templates = useMemo(() => data ?? [], [data])

  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  const [globalFilter, setGlobalFilter] = useState('')

  const columns = useMemo(
    () => [
      columnHelper.accessor((row) => row.displayName || row.name, {
        id: 'displayName',
        header: 'Display Name',
        cell: ({ row }) => {
          const t = row.original
          const label = t.displayName || t.name
          return (
            <Link
              to="/orgs/$orgName/templates/$namespace/$name"
              params={{ orgName, namespace: t.namespace, name: t.name }}
              title={t.name}
              className="hover:underline font-medium"
            >
              {label}
            </Link>
          )
        },
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
    data: templates,
    columns,
    state: { globalFilter },
    onGlobalFilterChange: setGlobalFilter,
    globalFilterFn: 'includesString',
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  })

  if (isLoading || queryDisabled) {
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

  if (error) {
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
        <CardTitle>Templates</CardTitle>
        {canWrite && (
          <Link to="/orgs/$orgName/templates/new" params={{ orgName }}>
            <Button size="sm">Create Template</Button>
          </Link>
        )}
      </CardHeader>
      <CardContent>
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
            <div className="mb-3">
              <Input
                placeholder="Search templates…"
                value={globalFilter}
                onChange={(e) => setGlobalFilter(e.target.value)}
                className="max-w-sm"
                aria-label="Search templates"
              />
            </div>
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
