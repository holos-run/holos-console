import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import {
  useReactTable,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  flexRender,
  createColumnHelper,
} from '@tanstack/react-table'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
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
import { Plus } from 'lucide-react'
import { useListOrganizations } from '@/queries/organizations'
import { useOrg } from '@/lib/org-context'
import type { Organization } from '@/gen/holos/console/v1/organizations_pb'

export const Route = createFileRoute('/_authenticated/organizations/')({
  component: OrganizationsIndexPage,
})

const columnHelper = createColumnHelper<Organization>()

export function OrganizationsIndexPage() {
  const navigate = useNavigate()
  const { setSelectedOrg } = useOrg()
  const { data, isLoading, error } = useListOrganizations()
  const organizations = data?.organizations ?? []

  const [globalFilter, setGlobalFilter] = useState('')

  const columns = [
    columnHelper.accessor((row) => row.displayName || row.name, {
      id: 'displayName',
      header: 'Display Name',
      cell: ({ row }) => (
        <span className="font-medium">{row.original.displayName || row.original.name}</span>
      ),
    }),
    columnHelper.accessor('name', {
      header: 'Name',
      cell: ({ getValue }) => (
        <span className="text-muted-foreground font-mono text-sm">{getValue()}</span>
      ),
    }),
    columnHelper.accessor('description', {
      header: 'Description',
      cell: ({ getValue }) => {
        const desc = getValue()
        if (!desc) return <span className="text-muted-foreground">—</span>
        return (
          <span className="text-muted-foreground truncate max-w-[40ch] block">
            {desc.length > 40 ? `${desc.slice(0, 40)}…` : desc}
          </span>
        )
      },
    }),
  ]

  const table = useReactTable({
    data: organizations,
    columns,
    state: { globalFilter },
    onGlobalFilterChange: setGlobalFilter,
    globalFilterFn: 'includesString',
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: { pagination: { pageSize: 25 } },
  })

  const handleRowClick = (org: Organization) => {
    // Switching organizations from this page sets the selected org and lands
    // on the org's Resources listing (the unified folders + projects view,
    // introduced in HOL-606). Reusing OrgContext keeps the selection
    // persistent across reloads.
    setSelectedOrg(org.name)
    navigate({
      to: '/orgs/$orgName/resources',
      params: { orgName: org.name },
    })
  }

  if (isLoading) {
    return (
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <CardTitle>Organizations</CardTitle>
          <Link to="/organization/new" search={{ returnTo: '/organizations' }}>
            <Button size="sm" disabled>
              <Plus className="h-4 w-4 mr-1" />
              Create Organization
            </Button>
          </Link>
        </CardHeader>
        <CardContent>
          <div className="space-y-2">
            {[...Array(3)].map((_, i) => (
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
    <>
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <CardTitle>Organizations</CardTitle>
          <Link to="/organization/new" search={{ returnTo: '/organizations' }}>
            <Button size="sm">
              <Plus className="h-4 w-4 mr-1" />
              Create Organization
            </Button>
          </Link>
        </CardHeader>
        <CardContent>
          {organizations.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-muted-foreground">No organizations yet. Create one.</p>
              <Link to="/organization/new" search={{ returnTo: '/organizations' }}>
                <Button size="sm">
                  Create Organization
                </Button>
              </Link>
            </div>
          ) : (
            <>
              <div className="mb-3">
                <Input
                  placeholder="Search organizations…"
                  value={globalFilter}
                  onChange={(e) => setGlobalFilter(e.target.value)}
                  className="max-w-sm"
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
                            : flexRender(header.column.columnDef.header, header.getContext())}
                        </TableHead>
                      ))}
                    </TableRow>
                  ))}
                </TableHeader>
                <TableBody>
                  {table.getRowModel().rows.map((row) => (
                    <TableRow
                      key={row.id}
                      className="cursor-pointer"
                      onClick={() => handleRowClick(row.original)}
                    >
                      {row.getVisibleCells().map((cell) => (
                        <TableCell key={cell.id}>
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </TableCell>
                      ))}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {table.getPageCount() > 1 && (
                <div className="flex items-center justify-end gap-2 mt-3">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => table.previousPage()}
                    disabled={!table.getCanPreviousPage()}
                  >
                    Previous
                  </Button>
                  <span className="text-sm text-muted-foreground">
                    Page {table.getState().pagination.pageIndex + 1} of{' '}
                    {table.getPageCount()}
                  </span>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => table.nextPage()}
                    disabled={!table.getCanNextPage()}
                  >
                    Next
                  </Button>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </>
  )
}
