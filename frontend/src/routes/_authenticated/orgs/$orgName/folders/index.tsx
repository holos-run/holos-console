import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
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
import { useListFolders } from '@/queries/folders'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { ParentType } from '@/gen/holos/console/v1/folders_pb'
import type { Folder } from '@/gen/holos/console/v1/folders_pb'
import { CreateFolderDialog } from '@/components/create-folder-dialog'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/folders/')({
  component: FoldersIndexRoute,
})

function FoldersIndexRoute() {
  const { orgName } = Route.useParams()
  return <FoldersIndexPage orgName={orgName} />
}

const columnHelper = createColumnHelper<Folder>()

export function FoldersIndexPage({ orgName }: { orgName?: string } = {}) {
  let routeOrgName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeOrgName = Route.useParams().orgName
  } catch {
    routeOrgName = undefined
  }
  const org = orgName ?? routeOrgName ?? ''

  const navigate = useNavigate()
  const { data: folders, isLoading, error } = useListFolders(org, ParentType.ORGANIZATION, org)
  const { data: orgData } = useGetOrganization(org)
  const userRole = orgData?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

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
      cell: ({ getValue }) => {
        const role = getValue()
        const label =
          role === Role.OWNER ? 'Owner' : role === Role.EDITOR ? 'Editor' : 'Viewer'
        return <Badge variant="outline">{label}</Badge>
      },
    }),
  ]

  const table = useReactTable({
    data: folders ?? [],
    columns,
    state: { globalFilter },
    onGlobalFilterChange: setGlobalFilter,
    globalFilterFn: 'includesString',
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  })

  const handleRowClick = (folder: Folder) => {
    navigate({
      to: '/folders/$folderName',
      params: { folderName: folder.name },
    })
  }

  if (isLoading) {
    return (
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <CardTitle>Folders</CardTitle>
          <Button size="sm" disabled>
            <Plus className="h-4 w-4 mr-1" />
            Create Folder
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

  const folderList = folders ?? []

  return (
    <>
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <div>
            <p className="text-sm text-muted-foreground">
              <Link to="/orgs/$orgName/settings" params={{ orgName: org }} className="hover:underline">
                {org}
              </Link>
              {' / Folders'}
            </p>
            <CardTitle className="mt-1">Folders</CardTitle>
          </div>
          {canWrite && (
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              <Plus className="h-4 w-4 mr-1" />
              Create Folder
            </Button>
          )}
        </CardHeader>
        <CardContent>
          {folderList.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-muted-foreground">No folders yet. Create one to organize projects.</p>
              {canWrite && (
                <Button size="sm" onClick={() => setCreateOpen(true)}>
                  Create Folder
                </Button>
              )}
            </div>
          ) : (
            <>
              <div className="mb-3">
                <Input
                  placeholder="Search folders…"
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
            </>
          )}
        </CardContent>
      </Card>

      <CreateFolderDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        organization={org}
        parentType={ParentType.ORGANIZATION}
        parentName={org}
        onCreated={(name) => {
          navigate({
            to: '/folders/$folderName',
            params: { folderName: name },
          })
        }}
      />
    </>
  )
}
