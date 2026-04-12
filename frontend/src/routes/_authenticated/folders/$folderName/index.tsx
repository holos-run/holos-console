import { useState, useMemo } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import {
  useReactTable,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  flexRender,
  createColumnHelper,
  type SortingState,
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
import { ArrowUpDown, ArrowUp, ArrowDown, Plus, Settings } from 'lucide-react'
import { useGetFolder, useListFolders } from '@/queries/folders'
import { useListProjectsByParent } from '@/queries/projects'
import { useGetOrganization } from '@/queries/organizations'
import { ParentType } from '@/gen/holos/console/v1/folders_pb'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateFolderDialog } from '@/components/create-folder-dialog'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/',
)({
  component: FolderIndexRoute,
})

function FolderIndexRoute() {
  const { folderName } = Route.useParams()
  return <FolderIndexPage folderName={folderName} />
}

/** Unified row type combining child folders and child projects. */
type FolderChild = {
  name: string
  displayName: string
  type: 'folder' | 'project'
  createdAt: string
  creatorEmail: string
}

const columnHelper = createColumnHelper<FolderChild>()

export function FolderIndexPage({ folderName: propFolderName }: { folderName?: string } = {}) {
  let routeParams: { folderName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const folderName = propFolderName ?? routeParams.folderName ?? ''

  let navigate: ReturnType<typeof useNavigate>
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    navigate = useNavigate()
  } catch {
    navigate = (() => {}) as unknown as ReturnType<typeof useNavigate>
  }

  const { data: folder, isPending: folderPending, error: folderError } = useGetFolder(folderName)
  const orgName = folder?.organization ?? ''

  const { data: org } = useGetOrganization(orgName)
  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  const { data: childFolders, isPending: foldersLoading } = useListFolders(orgName, ParentType.FOLDER, folderName)
  const { data: childProjects, isPending: projectsLoading } = useListProjectsByParent(orgName, ParentType.FOLDER, folderName)

  const [globalFilter, setGlobalFilter] = useState('')
  const [sorting, setSorting] = useState<SortingState>([{ id: 'displayName', desc: false }])
  const [createOpen, setCreateOpen] = useState(false)

  const data = useMemo<FolderChild[]>(() => {
    const items: FolderChild[] = []
    for (const f of childFolders ?? []) {
      items.push({
        name: f.name,
        displayName: f.displayName || f.name,
        type: 'folder',
        createdAt: f.createdAt ?? '',
        creatorEmail: f.creatorEmail ?? '',
      })
    }
    for (const p of childProjects ?? []) {
      items.push({
        name: p.name,
        displayName: p.displayName || p.name,
        type: 'project',
        createdAt: p.createdAt ?? '',
        creatorEmail: p.creatorEmail ?? '',
      })
    }
    return items
  }, [childFolders, childProjects])

  const columns = useMemo(() => [
    columnHelper.accessor('displayName', {
      header: ({ column }) => {
        const sorted = column.getIsSorted()
        return (
          <Button
            variant="ghost"
            size="sm"
            className="-ml-3 h-8 font-medium"
            onClick={() => column.toggleSorting(sorted === 'asc')}
          >
            Display Name
            {sorted === 'asc' ? (
              <ArrowUp className="ml-1 h-3.5 w-3.5" />
            ) : sorted === 'desc' ? (
              <ArrowDown className="ml-1 h-3.5 w-3.5" />
            ) : (
              <ArrowUpDown className="ml-1 h-3.5 w-3.5 opacity-50" />
            )}
          </Button>
        )
      },
      cell: ({ getValue }) => (
        <span className="font-medium">{getValue()}</span>
      ),
    }),
    columnHelper.accessor('type', {
      header: 'Type',
      cell: ({ getValue }) => {
        const t = getValue()
        return (
          <Badge variant="outline">
            {t === 'folder' ? 'Folder' : 'Project'}
          </Badge>
        )
      },
      enableGlobalFilter: false,
    }),
    columnHelper.accessor('createdAt', {
      header: ({ column }) => {
        const sorted = column.getIsSorted()
        return (
          <Button
            variant="ghost"
            size="sm"
            className="-ml-3 h-8 font-medium"
            onClick={() => column.toggleSorting(sorted === 'asc')}
          >
            Created
            {sorted === 'asc' ? (
              <ArrowUp className="ml-1 h-3.5 w-3.5" />
            ) : sorted === 'desc' ? (
              <ArrowDown className="ml-1 h-3.5 w-3.5" />
            ) : (
              <ArrowUpDown className="ml-1 h-3.5 w-3.5 opacity-50" />
            )}
          </Button>
        )
      },
      cell: ({ getValue }) => {
        const ts = getValue()
        if (!ts) return <span className="text-muted-foreground">--</span>
        try {
          return (
            <span className="text-muted-foreground text-sm">
              {new Intl.DateTimeFormat(undefined, {
                year: 'numeric',
                month: 'short',
                day: 'numeric',
              }).format(new Date(ts))}
            </span>
          )
        } catch {
          return <span className="text-muted-foreground text-sm">{ts}</span>
        }
      },
    }),
    columnHelper.accessor('creatorEmail', {
      header: ({ column }) => {
        const sorted = column.getIsSorted()
        return (
          <Button
            variant="ghost"
            size="sm"
            className="-ml-3 h-8 font-medium"
            onClick={() => column.toggleSorting(sorted === 'asc')}
          >
            Creator
            {sorted === 'asc' ? (
              <ArrowUp className="ml-1 h-3.5 w-3.5" />
            ) : sorted === 'desc' ? (
              <ArrowDown className="ml-1 h-3.5 w-3.5" />
            ) : (
              <ArrowUpDown className="ml-1 h-3.5 w-3.5 opacity-50" />
            )}
          </Button>
        )
      },
      cell: ({ getValue }) => {
        const email = getValue()
        if (!email) return <span className="text-muted-foreground">--</span>
        return <span className="text-muted-foreground text-sm">{email}</span>
      },
    }),
  ], [])

  const table = useReactTable({
    data,
    columns,
    state: { globalFilter, sorting },
    onGlobalFilterChange: setGlobalFilter,
    onSortingChange: setSorting,
    globalFilterFn: 'includesString',
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  const handleRowClick = (row: FolderChild) => {
    if (row.type === 'folder') {
      navigate({
        to: '/folders/$folderName',
        params: { folderName: row.name },
      })
    } else {
      navigate({
        to: '/projects/$projectName',
        params: { projectName: row.name },
      })
    }
  }

  const isLoading = folderPending || foldersLoading || projectsLoading

  if (isLoading) {
    return (
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <CardTitle>Contents</CardTitle>
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

  if (folderError) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive">
            <AlertDescription>{folderError.message}</AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  const displayName = folder?.displayName || folderName

  return (
    <>
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <div>
            <p className="text-sm text-muted-foreground">
              <Link to="/orgs/$orgName/settings" params={{ orgName }} className="hover:underline">
                {orgName}
              </Link>
              {' / '}
              <Link to="/orgs/$orgName/folders" params={{ orgName }} className="hover:underline">
                Folders
              </Link>
              {' / '}
              {folderName}
            </p>
            <CardTitle className="mt-1">{displayName}</CardTitle>
          </div>
          <div className="flex items-center gap-2">
            <Link
              to="/folders/$folderName/settings"
              params={{ folderName }}
              aria-label="Settings"
            >
              <Button variant="outline" size="sm">
                <Settings className="h-4 w-4 mr-1" />
                Settings
              </Button>
            </Link>
            {canWrite && (
              <Button size="sm" onClick={() => setCreateOpen(true)}>
                <Plus className="h-4 w-4 mr-1" />
                Create Folder
              </Button>
            )}
          </div>
        </CardHeader>
        <CardContent>
          {data.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-muted-foreground">No items in this folder yet.</p>
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
                  placeholder="Search items..."
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
        organization={orgName}
        parentType={ParentType.FOLDER}
        parentName={folderName}
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
