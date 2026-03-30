import { useState, useMemo } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import {
  useReactTable,
  getCoreRowModel,
  getSortedRowModel,
  flexRender,
  createColumnHelper,
  type SortingState,
} from '@tanstack/react-table'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { ArrowUpDown, ArrowUp, ArrowDown, Lock, Trash2 } from 'lucide-react'
import { useAuth } from '@/lib/auth'
import { SecretDataGrid } from '@/components/secret-data-grid'
import { useListSecrets, useCreateSecret, useDeleteSecret } from '@/queries/secrets'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import type { SecretMetadata } from '@/gen/holos/console/v1/secrets_pb.js'

export const Route = createFileRoute('/_authenticated/projects/$projectName/secrets/')({
  component: SecretsListPage,
})

function sharingSummary(userCount: number, roleCount: number): string | undefined {
  const parts: string[] = []
  if (userCount > 0) parts.push(`${userCount} user${userCount !== 1 ? 's' : ''}`)
  if (roleCount > 0) parts.push(`${roleCount} role${roleCount !== 1 ? 's' : ''}`)
  return parts.length > 0 ? parts.join(', ') : undefined
}

const columnHelper = createColumnHelper<SecretMetadata>()

function SecretsListPage() {
  const { projectName } = Route.useParams()
  const { user, isAuthenticated, isLoading: authLoading } = useAuth()

  const { data: secrets = [], isLoading, error } = useListSecrets(projectName)
  const createMutation = useCreateSecret(projectName)
  const deleteMutation = useDeleteSecret(projectName)

  const [sorting, setSorting] = useState<SortingState>([{ id: 'name', desc: false }])

  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createDescription, setCreateDescription] = useState('')
  const [createUrl, setCreateUrl] = useState('')
  const [createData, setCreateData] = useState<Record<string, Uint8Array>>({})
  const [createError, setCreateError] = useState<string | null>(null)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

  const columns = useMemo(() => [
    columnHelper.accessor('name', {
      header: ({ column }) => {
        const sorted = column.getIsSorted()
        return (
          <Button
            variant="ghost"
            size="sm"
            className="-ml-3 h-8 font-medium"
            onClick={() => column.toggleSorting(sorted === 'asc')}
          >
            Name
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
      cell: ({ row }) => {
        const secret = row.original
        if (!secret.accessible) {
          return <span className="font-medium opacity-50">{secret.name}</span>
        }
        return (
          <Link
            to="/projects/$projectName/secrets/$name"
            params={{ projectName, name: secret.name }}
            className="font-medium hover:underline"
          >
            {secret.name}
          </Link>
        )
      },
    }),
    columnHelper.accessor('description', {
      header: 'Description',
      cell: ({ getValue }) => {
        const desc = getValue()
        if (!desc) return <span className="text-muted-foreground">—</span>
        return (
          <span className="text-muted-foreground truncate max-w-[60ch] block">
            {desc.length > 60 ? `${desc.slice(0, 60)}…` : desc}
          </span>
        )
      },
    }),
    columnHelper.display({
      id: 'sharing',
      header: 'Sharing',
      cell: ({ row }) => {
        const secret = row.original
        if (!secret.accessible) {
          return (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="outline">
                    <Lock className="h-3 w-3 mr-1" />
                    No access
                  </Badge>
                </TooltipTrigger>
                <TooltipContent><p>You do not have access to this secret</p></TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )
        }
        const summary = sharingSummary(secret.userGrants.length, secret.roleGrants.length)
        return summary ? (
          <Badge variant="outline">{summary}</Badge>
        ) : (
          <span className="text-muted-foreground">—</span>
        )
      },
    }),
    columnHelper.display({
      id: 'actions',
      header: '',
      cell: ({ row }) => {
        const secret = row.original
        if (!secret.accessible) return null
        return (
          <Button
            variant="ghost"
            size="icon"
            aria-label={`delete ${secret.name}`}
            onClick={() => { setDeleteTarget(secret.name); deleteMutation.reset(); setDeleteOpen(true) }}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        )
      },
    }),
  ], [projectName, deleteMutation])

  const table = useReactTable({
    data: secrets,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  const handleCreateOpen = () => {
    setCreateName('')
    setCreateDescription('')
    setCreateUrl('')
    setCreateData({})
    setCreateError(null)
    setCreateOpen(true)
  }

  const handleCreate = async () => {
    if (!createName.trim()) {
      setCreateError('Secret name is required')
      return
    }
    setCreateError(null)
    try {
      await createMutation.mutateAsync({
        name: createName.trim(),
        data: createData,
        userGrants: [{ principal: (user?.profile?.email as string) || '', role: Role.OWNER }],
        roleGrants: [],
        description: createDescription.trim() || undefined,
        url: createUrl.trim() || undefined,
      })
      setCreateOpen(false)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return
    try {
      await deleteMutation.mutateAsync(deleteTarget)
      setDeleteOpen(false)
      setDeleteTarget(null)
    } catch { /* error via mutation */ }
  }

  if (authLoading || (isAuthenticated && isLoading)) {
    return (
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <CardTitle>{projectName ? `${projectName} / Secrets` : 'Secrets'}</CardTitle>
          <Button size="sm" disabled>Create Secret</Button>
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
          <Alert variant="destructive"><AlertDescription>{error.message}</AlertDescription></Alert>
        </CardContent>
      </Card>
    )
  }

  return (
    <>
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <CardTitle>{projectName ? `${projectName} / Secrets` : 'Secrets'}</CardTitle>
          <Button size="sm" onClick={handleCreateOpen}>Create Secret</Button>
        </CardHeader>
        <CardContent>
          {secrets.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-muted-foreground">No secrets yet.</p>
              <Button size="sm" onClick={handleCreateOpen}>Create Secret</Button>
            </div>
          ) : (
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
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </TableCell>
                    ))}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Create Secret</DialogTitle>
            <DialogDescription>Create a new secret. You will be added as the Owner.</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div>
              <Label>Name</Label>
              <Input
                autoFocus
                value={createName}
                onChange={(e) => setCreateName(e.target.value)}
                placeholder="my-secret"
              />
              <p className="text-xs text-muted-foreground mt-1">Lowercase alphanumeric and hyphens only</p>
            </div>
            <div>
              <Label>Description</Label>
              <Input
                value={createDescription}
                onChange={(e) => setCreateDescription(e.target.value)}
                placeholder="What is this secret used for?"
              />
            </div>
            <div>
              <Label>URL</Label>
              <Input
                value={createUrl}
                onChange={(e) => setCreateUrl(e.target.value)}
                placeholder="https://example.com/service"
              />
            </div>
            <div>
              <Label>Data</Label>
              <SecretDataGrid data={createData} onChange={setCreateData} />
            </div>
            {createError && (
              <Alert variant="destructive"><AlertDescription>{createError}</AlertDescription></Alert>
            )}
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button onClick={handleCreate} disabled={createMutation.isPending}>
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Secret</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete secret &quot;{deleteTarget}&quot;? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {deleteMutation.error && (
            <Alert variant="destructive"><AlertDescription>{deleteMutation.error.message}</AlertDescription></Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)}>Cancel</Button>
            <Button variant="destructive" onClick={handleDeleteConfirm} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
