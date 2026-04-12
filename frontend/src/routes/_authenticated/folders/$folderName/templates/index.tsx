import { useState, useMemo } from 'react'
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
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Lock } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useListTemplates, makeFolderScope } from '@/queries/templates'
import { useGetFolder } from '@/queries/folders'

/** Row type for the template table. */
type TemplateRow = {
  name: string
  description: string
  mandatory: boolean
  enabled: boolean
}

const columnHelper = createColumnHelper<TemplateRow>()

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/templates/',
)({
  component: FolderTemplatesIndexRoute,
})

function FolderTemplatesIndexRoute() {
  const { folderName } = Route.useParams()
  return <FolderTemplatesIndexPage folderName={folderName} />
}

export function FolderTemplatesIndexPage({
  orgName: propOrgName,
  folderName: propFolderName,
}: { orgName?: string; folderName?: string } = {}) {
  let routeParams: { folderName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const folderName = propFolderName ?? routeParams.folderName ?? ''

  // Load folder to derive orgName and userRole from the response
  const { data: folder } = useGetFolder(folderName)
  const orgName = propOrgName ?? folder?.organization ?? ''

  const scope = makeFolderScope(folderName)
  const { data: templates, isPending, error } = useListTemplates(scope)

  const userRole = folder?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  const [globalFilter, setGlobalFilter] = useState('')

  const data = useMemo<TemplateRow[]>(() => {
    if (!templates) return []
    return templates.map((t) => ({
      name: t.name,
      description: t.description ?? '',
      mandatory: t.mandatory ?? false,
      enabled: t.enabled ?? false,
    }))
  }, [templates])

  const columns = useMemo(
    () => [
      columnHelper.accessor('name', {
        header: 'Name',
        cell: ({ getValue }) => (
          <span className="text-sm font-medium font-mono">{getValue()}</span>
        ),
      }),
      columnHelper.accessor('description', {
        header: 'Description',
        cell: ({ getValue }) => {
          const desc = getValue()
          if (!desc) return <span className="text-muted-foreground">--</span>
          return (
            <span className="text-sm text-muted-foreground truncate max-w-[40ch] block">
              {desc.length > 60 ? `${desc.slice(0, 60)}...` : desc}
            </span>
          )
        },
      }),
      columnHelper.display({
        id: 'badges',
        header: 'Status',
        cell: ({ row }) => (
          <div className="flex items-center gap-2">
            {row.original.mandatory && (
              <Badge variant="secondary" className="flex items-center gap-1 text-xs">
                <Lock className="h-3 w-3" />
                Mandatory
              </Badge>
            )}
            {row.original.enabled ? (
              <Badge variant="outline" className="text-xs text-green-500 border-green-500/30">
                Enabled
              </Badge>
            ) : (
              <Badge variant="outline" className="text-xs text-muted-foreground">
                Disabled
              </Badge>
            )}
          </div>
        ),
      }),
    ],
    [],
  )

  const table = useReactTable({
    data,
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
        <CardContent className="pt-6 space-y-4">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-8 w-full" />
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
              <Link
                to="/folders/$folderName/settings"
                params={{ folderName }}
                className="hover:underline"
              >
                {folderName}
              </Link>
              {' / Platform Templates'}
            </p>
            <CardTitle className="mt-1">Platform Templates</CardTitle>
          </div>
          {canWrite && (
            <Link to="/folders/$folderName/templates/new" params={{ folderName }}>
              <Button size="sm">
                Create Template
              </Button>
            </Link>
          )}
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">
            Platform templates at folder scope are applied to projects within this folder hierarchy.
            Mandatory templates are marked with a lock badge.
          </p>
          <Separator />
          {data.length > 0 ? (
            <>
              <div>
                <Input
                  placeholder="Search templates..."
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
                    <TableRow key={row.id}>
                      <TableCell>
                        <Link
                          to="/folders/$folderName/templates/$templateName"
                          params={{ folderName, templateName: row.original.name }}
                          className="text-sm font-medium font-mono hover:underline"
                        >
                          {row.original.name}
                        </Link>
                      </TableCell>
                      {row.getVisibleCells().slice(1).map((cell) => (
                        <TableCell key={cell.id}>
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </TableCell>
                      ))}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </>
          ) : (
            <p className="text-sm text-muted-foreground">
              No platform templates found for this folder.
            </p>
          )}
        </CardContent>
    </Card>
  )
}
