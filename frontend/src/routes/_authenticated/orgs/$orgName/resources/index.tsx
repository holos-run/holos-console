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
import { useListResources } from '@/queries/resources'
import {
  ResourceType,
  type Resource,
} from '@/gen/holos/console/v1/resources_pb'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/resources/')({
  component: ResourcesIndexPage,
})

const columnHelper = createColumnHelper<Resource>()

function typeBadge(type: ResourceType) {
  if (type === ResourceType.FOLDER) {
    return <Badge variant="outline">Folder</Badge>
  }
  if (type === ResourceType.PROJECT) {
    return <Badge variant="outline">Project</Badge>
  }
  // The server contract forbids UNSPECIFIED entries. Render a destructive
  // badge so the backend bug is visible instead of blending in.
  return <Badge variant="destructive">Unknown</Badge>
}

// PathCell renders the root→leaf display-name breadcrumb with the leaf
// (this resource) on the right. The root element (index 0) is the
// organization; subsequent elements are ancestor folders; the leaf is the
// resource itself. Every element is a TanStack Router Link so navigation
// stays SPA. Slugs surface via `title` so colliding display names can be
// disambiguated.
function PathCell({ resource }: { resource: Resource }) {
  const org = resource.path[0]
  const orgName = org?.name ?? ''
  const leafDisplay = resource.displayName || resource.name

  return (
    <span className="flex flex-wrap items-center gap-1 text-sm">
      {resource.path.map((element, i) => {
        const display = element.displayName || element.name
        return (
          <span key={`${element.name}-${i}`} className="flex items-center gap-1">
            {i === 0 ? (
              <Link
                to="/orgs/$orgName"
                params={{ orgName: element.name }}
                title={element.name}
                className="hover:underline text-muted-foreground"
              >
                {display}
              </Link>
            ) : (
              <Link
                to="/orgs/$orgName/folders/$folderName"
                params={{ orgName, folderName: element.name }}
                title={element.name}
                className="hover:underline text-muted-foreground"
              >
                {display}
              </Link>
            )}
            <span className="text-muted-foreground">/</span>
          </span>
        )
      })}
      {resource.type === ResourceType.FOLDER ? (
        <Link
          to="/orgs/$orgName/folders/$folderName"
          params={{ orgName, folderName: resource.name }}
          title={resource.name}
          className="hover:underline font-medium"
        >
          {leafDisplay}
        </Link>
      ) : (
        <Link
          to="/projects/$projectName"
          params={{ projectName: resource.name }}
          title={resource.name}
          className="hover:underline font-medium"
        >
          {leafDisplay}
        </Link>
      )}
    </span>
  )
}

export function ResourcesIndexPage() {
  const { orgName } = Route.useParams()
  const { data, isLoading, error } = useListResources(orgName)
  const resources = useMemo(() => data?.resources ?? [], [data])

  const [globalFilter, setGlobalFilter] = useState('')

  const columns = useMemo(
    () => [
      columnHelper.accessor((row) => typeLabel(row.type), {
        id: 'type',
        header: 'Type',
        cell: ({ row }) => typeBadge(row.original.type),
      }),
      columnHelper.accessor((row) => pathSearchString(row), {
        id: 'path',
        header: 'Path',
        cell: ({ row }) => <PathCell resource={row.original} />,
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
    [],
  )

  const table = useReactTable({
    data: resources,
    columns,
    state: { globalFilter },
    onGlobalFilterChange: setGlobalFilter,
    globalFilterFn: 'includesString',
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  })

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Resources</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-2" data-testid="resources-loading">
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
    <Card>
      <CardHeader>
        <CardTitle>Resources</CardTitle>
      </CardHeader>
      <CardContent>
        {resources.length === 0 ? (
          <div className="flex flex-col items-center gap-3 py-8 text-center">
            <p className="text-muted-foreground">
              No resources yet. Create a folder or project to get started.
            </p>
          </div>
        ) : (
          <>
            <div className="mb-3">
              <Input
                placeholder="Search resources…"
                value={globalFilter}
                onChange={(e) => setGlobalFilter(e.target.value)}
                className="max-w-sm"
                aria-label="Search resources"
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

function typeLabel(type: ResourceType) {
  return type === ResourceType.FOLDER
    ? 'folder'
    : type === ResourceType.PROJECT
      ? 'project'
      : 'unknown'
}

// pathSearchString serializes the row's display-name breadcrumb plus the
// leaf display name and slug so globalFilter `includesString` matches
// anywhere in the visible path OR the underlying resource slug.
function pathSearchString(resource: Resource): string {
  const crumbs = resource.path.map((p) => p.displayName || p.name)
  crumbs.push(resource.displayName || resource.name)
  if (resource.name !== (resource.displayName || resource.name)) {
    crumbs.push(resource.name)
  }
  return crumbs.join(' / ')
}
