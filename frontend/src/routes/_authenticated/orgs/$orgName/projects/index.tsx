import { useState, useEffect } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
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
import { Badge } from '@/components/ui/badge'
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
import { useListProjects } from '@/queries/projects'
import { useProject } from '@/lib/project-context'
import { useOrg } from '@/lib/org-context'
import { CreateProjectDialog } from '@/components/create-project-dialog'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import type { Project } from '@/gen/holos/console/v1/projects_pb'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/projects/')({
  component: ProjectsIndexPage,
})

const columnHelper = createColumnHelper<Project>()

function roleBadge(role: Role) {
  const label =
    role === Role.OWNER ? 'Owner' : role === Role.EDITOR ? 'Editor' : 'Viewer'
  return <Badge variant="outline">{label}</Badge>
}

export function ProjectsIndexPage() {
  const { orgName } = Route.useParams()
  const navigate = useNavigate()
  const { setSelectedProject } = useProject()
  const { selectedOrg, setSelectedOrg } = useOrg()
  const { data, isLoading, error } = useListProjects(orgName)
  const projects = data?.projects ?? []

  // Sync org context when navigating directly to this URL via bookmark
  useEffect(() => {
    if (selectedOrg !== orgName) {
      setSelectedOrg(orgName)
    }
  }, [orgName, selectedOrg, setSelectedOrg])

  const [globalFilter, setGlobalFilter] = useState('')
  const [createOpen, setCreateOpen] = useState(false)

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
    columnHelper.accessor('userRole', {
      header: 'Role',
      cell: ({ getValue }) => roleBadge(getValue()),
    }),
  ]

  const table = useReactTable({
    data: projects,
    columns,
    state: { globalFilter },
    onGlobalFilterChange: setGlobalFilter,
    globalFilterFn: 'includesString',
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: { pagination: { pageSize: 25 } },
  })

  const handleRowClick = (project: Project) => {
    setSelectedProject(project.name)
    navigate({
      to: '/projects/$projectName/secrets',
      params: { projectName: project.name },
    })
  }

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
          <CardTitle>Projects</CardTitle>
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4 mr-1" />
            Create Project
          </Button>
        </CardHeader>
        <CardContent>
          {projects.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-muted-foreground">No projects yet. Create one.</p>
              <Button size="sm" onClick={() => setCreateOpen(true)}>
                Create Project
              </Button>
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

      <CreateProjectDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        defaultOrganization={orgName}
        onCreated={(name) => setSelectedProject(name)}
      />
    </>
  )
}
