import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import {
  useReactTable,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  flexRender,
  createColumnHelper,
  type SortingState,
  type ColumnDef,
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
import { ChevronUp, ChevronDown, ChevronsUpDown, Plus } from 'lucide-react'
import { useListOrganizations } from '@/queries/organizations'
import { useOrg } from '@/lib/org-context'
import { formatCreatedAt } from '@/lib/format-created-at'
import type { Organization } from '@/gen/holos/console/v1/organizations_pb'

export const Route = createFileRoute('/_authenticated/organizations/')({
  component: OrganizationsIndexPage,
})

// SortIcon renders a chevron indicator for a sortable column header.
function SortIcon({ isSorted }: { isSorted: false | 'asc' | 'desc' }) {
  if (isSorted === 'asc')
    return <ChevronUp className="inline h-3.5 w-3.5 ml-1 opacity-80" />
  if (isSorted === 'desc')
    return <ChevronDown className="inline h-3.5 w-3.5 ml-1 opacity-80" />
  return <ChevronsUpDown className="inline h-3.5 w-3.5 ml-1 opacity-40" />
}

// columnHelper and columns are defined at module scope so they are stable
// across re-renders and do not trigger unnecessary TanStack Table updates.
const columnHelper = createColumnHelper<Organization>()

const columns: ColumnDef<Organization, string>[] = [
  columnHelper.accessor((row) => row.displayName || row.name, {
    id: 'displayName',
    header: ({ column }) => (
      <button
        className="flex items-center gap-0.5 font-medium select-none cursor-pointer"
        onClick={() => column.toggleSorting(column.getIsSorted() === 'asc')}
      >
        Display Name
        <SortIcon isSorted={column.getIsSorted()} />
      </button>
    ),
    cell: ({ row }) => (
      <span className="font-medium">
        {row.original.displayName || row.original.name}
      </span>
    ),
    enableSorting: true,
  }),
  columnHelper.accessor('name', {
    header: ({ column }) => (
      <button
        className="flex items-center gap-0.5 font-medium select-none cursor-pointer"
        onClick={() => column.toggleSorting(column.getIsSorted() === 'asc')}
      >
        Name
        <SortIcon isSorted={column.getIsSorted()} />
      </button>
    ),
    cell: ({ getValue }) => (
      <span className="text-muted-foreground font-mono text-sm">
        {getValue()}
      </span>
    ),
    enableSorting: true,
  }),
  columnHelper.accessor('description', {
    header: ({ column }) => (
      <button
        className="flex items-center gap-0.5 font-medium select-none cursor-pointer"
        onClick={() => column.toggleSorting(column.getIsSorted() === 'asc')}
      >
        Description
        <SortIcon isSorted={column.getIsSorted()} />
      </button>
    ),
    cell: ({ getValue }) => {
      const desc = getValue()
      if (!desc) return <span className="text-muted-foreground">—</span>
      return (
        <span className="text-muted-foreground truncate max-w-[40ch] block">
          {desc.length > 40 ? `${desc.slice(0, 40)}…` : desc}
        </span>
      )
    },
    enableSorting: true,
  }),
  columnHelper.accessor('createdAt', {
    id: 'createdAt',
    header: ({ column }) => (
      <button
        className="flex items-center gap-0.5 font-medium select-none cursor-pointer"
        onClick={() => column.toggleSorting(column.getIsSorted() === 'asc')}
      >
        Created At
        <SortIcon isSorted={column.getIsSorted()} />
      </button>
    ),
    cell: ({ getValue }) => (
      <span className="text-muted-foreground text-sm whitespace-nowrap">
        {formatCreatedAt(getValue()) || '—'}
      </span>
    ),
    enableSorting: true,
    // ISO 8601 strings sort correctly as plain strings.
    sortingFn: 'alphanumeric',
  }),
]

export function OrganizationsIndexPage() {
  const navigate = useNavigate()
  const { setSelectedOrg } = useOrg()
  const { data, isLoading, error } = useListOrganizations()
  const organizations = data?.organizations ?? []

  const [globalFilter, setGlobalFilter] = useState('')
  // Default sort: Created At descending (newest first).
  const [sorting, setSorting] = useState<SortingState>([
    { id: 'createdAt', desc: true },
  ])

  const table = useReactTable({
    data: organizations,
    columns,
    state: { globalFilter, sorting },
    onGlobalFilterChange: setGlobalFilter,
    onSortingChange: setSorting,
    globalFilterFn: 'includesString',
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: { pagination: { pageSize: 25 } },
  })

  const handleRowClick = (org: Organization) => {
    setSelectedOrg(org.name)
    navigate({
      to: '/organizations/$orgName/projects',
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
