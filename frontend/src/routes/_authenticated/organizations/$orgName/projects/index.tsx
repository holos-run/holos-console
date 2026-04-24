import { useState } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
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
import { useListProjects } from '@/queries/projects'
import { formatCreatedAt } from '@/lib/format-created-at'
import type { Project } from '@/gen/holos/console/v1/projects_pb'

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/projects/',
)({
  component: OrgProjectsIndexRoute,
})

function OrgProjectsIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgProjectsIndexPage orgName={orgName} />
}

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
const columnHelper = createColumnHelper<Project>()

const columns: ColumnDef<Project, string>[] = [
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

export function OrgProjectsIndexPage({
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

  const navigate = useNavigate()
  const { data, isLoading, error } = useListProjects(orgName)
  const projects = data?.projects ?? []

  const [globalFilter, setGlobalFilter] = useState('')
  // Default sort: Created At descending (newest first).
  const [sorting, setSorting] = useState<SortingState>([
    { id: 'createdAt', desc: true },
  ])

  const table = useReactTable({
    data: projects,
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

  if (isLoading) {
    return (
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <CardTitle>Projects</CardTitle>
          <Button size="sm" disabled>
            <Plus className="h-4 w-4 mr-1" />
            Create Project
          </Button>
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
          <div>
            <p className="text-sm text-muted-foreground">
              <Link to="/organizations" className="hover:underline">
                Organizations
              </Link>
              {' / '}
              {orgName}
              {' / Projects'}
            </p>
            <CardTitle className="mt-1">Projects</CardTitle>
          </div>
          <Link
            to="/project/new"
            search={{ orgName, returnTo: `/organizations/${orgName}/projects` }}
          >
            <Button size="sm">
              <Plus className="h-4 w-4 mr-1" />
              Create Project
            </Button>
          </Link>
        </CardHeader>
        <CardContent>
          {projects.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-muted-foreground">
                No projects in this organization yet.
              </p>
              <Link
                to="/project/new"
                search={{ orgName, returnTo: `/organizations/${orgName}/projects` }}
              >
                <Button size="sm">Create Project</Button>
              </Link>
            </div>
          ) : (
            <>
              <div className="mb-3">
                <Input
                  placeholder="Search projects…"
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
                    <TableRow
                      key={row.id}
                      className="cursor-pointer"
                      onClick={() =>
                        navigate({
                          to: '/projects/$projectName',
                          params: { projectName: row.original.name },
                        })
                      }
                    >
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
