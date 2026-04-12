import { useState, useMemo } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
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
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Lock, Info } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useListTemplates, useCreateTemplate, makeFolderScope } from '@/queries/templates'
import { useGetFolder } from '@/queries/folders'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'

// EXAMPLE_FOLDER_PLATFORM_TEMPLATE is the example folder-level platform template CUE content.
// It provides an HTTPRoute into the istio-ingress namespace using platformResources.
const EXAMPLE_FOLDER_PLATFORM_TEMPLATE = `// Folder-level platform template — HTTPRoute for istio-ingress gateway.
// Applied to projects within this folder hierarchy.
platformResources: {
    namespacedResources: ("istio-ingress"): {
        HTTPRoute: (input.name): {
            apiVersion: "gateway.networking.k8s.io/v1"
            kind:       "HTTPRoute"
            metadata: {
                name:      input.name
                namespace: "istio-ingress"
                labels: {
                    "app.kubernetes.io/managed-by": "console.holos.run"
                    "app.kubernetes.io/name":       input.name
                }
            }
            spec: {
                parentRefs: [{
                    group:     "gateway.networking.k8s.io"
                    kind:      "Gateway"
                    namespace: platform.gatewayNamespace
                    name:      "default"
                }]
                rules: [{
                    backendRefs: [{
                        name: input.name
                        port: 80
                    }]
                }]
            }
        }
    }
    clusterResources: {}
}
`

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
  const createMutation = useCreateTemplate(scope)

  const userRole = folder?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  const [globalFilter, setGlobalFilter] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createDisplayName, setCreateDisplayName] = useState('')
  const [createDescription, setCreateDescription] = useState('')
  const [createCueTemplate, setCreateCueTemplate] = useState('')
  const [createEnabled, setCreateEnabled] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

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

  const handleOpenCreate = () => {
    setCreateName('')
    setCreateDisplayName('')
    setCreateDescription('')
    setCreateCueTemplate('')
    setCreateEnabled(false)
    setCreateError(null)
    setCreateOpen(true)
  }

  const handleLoadExample = () => {
    setCreateName('httproute-ingress')
    setCreateDisplayName('HTTPRoute Ingress')
    setCreateDescription(
      'Provides an HTTPRoute for the istio-ingress gateway, routing traffic to project services.',
    )
    setCreateCueTemplate(EXAMPLE_FOLDER_PLATFORM_TEMPLATE)
  }

  const handleCreateConfirm = async () => {
    if (!createName.trim()) {
      setCreateError('Name is required')
      return
    }
    setCreateError(null)
    try {
      await createMutation.mutateAsync({
        name: createName.trim(),
        displayName: createDisplayName.trim(),
        description: createDescription.trim(),
        cueTemplate: createCueTemplate,
        mandatory: false,
        enabled: createEnabled,
      })
      toast.success(`Created platform template "${createName}"`)
      setCreateOpen(false)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    }
  }

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
            <Button size="sm" onClick={handleOpenCreate}>
              Create Template
            </Button>
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

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Create Platform Template</DialogTitle>
            <DialogDescription>
              Create a new platform template for folder &quot;{folderName}&quot;.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="flex items-center gap-2">
              <Button variant="outline" size="sm" onClick={handleLoadExample}>
                Load Example
              </Button>
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Info className="h-4 w-4 text-muted-foreground cursor-default" />
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>
                      Platform templates are unified with project deployment templates at render
                      time via CUE. This example provides an HTTPRoute for the istio-ingress
                      gateway.
                    </p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            </div>
            <div className="space-y-2">
              <Label htmlFor="create-name">Name</Label>
              <Input
                id="create-name"
                aria-label="Name"
                value={createName}
                onChange={(e) => setCreateName(e.target.value)}
                placeholder="my-template"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="create-display-name">Display Name</Label>
              <Input
                id="create-display-name"
                aria-label="Display Name"
                value={createDisplayName}
                onChange={(e) => setCreateDisplayName(e.target.value)}
                placeholder="My Template"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="create-description">Description</Label>
              <Input
                id="create-description"
                aria-label="Description"
                value={createDescription}
                onChange={(e) => setCreateDescription(e.target.value)}
                placeholder="What does this template produce?"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="create-cue-template">CUE Template</Label>
              <Textarea
                id="create-cue-template"
                aria-label="CUE Template"
                value={createCueTemplate}
                onChange={(e) => setCreateCueTemplate(e.target.value)}
                placeholder="// CUE template content"
                className="font-mono text-xs min-h-[120px]"
              />
            </div>
            <div className="flex items-center gap-3">
              <Switch
                id="create-enabled"
                aria-label="Enabled"
                checked={createEnabled}
                onCheckedChange={setCreateEnabled}
              />
              <Label htmlFor="create-enabled" className="text-sm cursor-pointer">
                Enabled (apply to projects in this folder)
              </Label>
            </div>
          </div>
          {createError && (
            <Alert variant="destructive">
              <AlertDescription>{createError}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreateOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleCreateConfirm}
              disabled={createMutation.isPending || !createName}
            >
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
