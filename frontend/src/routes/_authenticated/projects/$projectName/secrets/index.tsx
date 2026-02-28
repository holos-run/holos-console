import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
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
import { Lock, Trash2, ExternalLink } from 'lucide-react'
import { useAuth } from '@/lib/auth'
import { SecretDataEditor } from '@/components/secret-data-editor'
import { useListSecrets, useCreateSecret, useDeleteSecret } from '@/queries/secrets'
import { Role } from '@/gen/holos/console/v1/rbac_pb'

export const Route = createFileRoute('/_authenticated/projects/$projectName/secrets/')({
  component: SecretsListPage,
})

function sharingSummary(userCount: number, roleCount: number): string | undefined {
  const parts: string[] = []
  if (userCount > 0) parts.push(`${userCount} user${userCount !== 1 ? 's' : ''}`)
  if (roleCount > 0) parts.push(`${roleCount} role${roleCount !== 1 ? 's' : ''}`)
  return parts.length > 0 ? parts.join(', ') : undefined
}

function SecretsListPage() {
  const { projectName } = Route.useParams()
  const { user, isAuthenticated, isLoading: authLoading } = useAuth()

  const { data: secrets = [], isLoading, error } = useListSecrets(projectName)
  const createMutation = useCreateSecret(projectName)
  const deleteMutation = useDeleteSecret(projectName)

  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createDescription, setCreateDescription] = useState('')
  const [createUrl, setCreateUrl] = useState('')
  const [createData, setCreateData] = useState<Record<string, Uint8Array>>({})
  const [createError, setCreateError] = useState<string | null>(null)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

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
        <CardContent className="pt-6">
          <div className="flex items-center gap-2">
            <div className="h-5 w-5 animate-spin rounded-full border-2 border-primary border-t-transparent" />
            <span>Loading...</span>
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
            <p className="text-muted-foreground">
              No secrets available. Secrets must have the label{' '}
              <code className="text-xs bg-muted px-1 py-0.5 rounded">app.kubernetes.io/managed-by=console.holos.run</code>{' '}
              to appear here.
            </p>
          ) : (
            <ul className="divide-y">
              {secrets.map((secret) => (
                <li key={secret.name} className="flex items-center gap-2 py-2">
                  {secret.accessible ? (
                    <Link
                      to="/projects/$projectName/secrets/$name"
                      params={{ projectName, name: secret.name }}
                      className="flex-1 min-w-0 hover:underline"
                    >
                      <div className="font-medium truncate">{secret.name}</div>
                      <div className="text-sm text-muted-foreground truncate">
                        {secret.description || sharingSummary(secret.userGrants.length, secret.roleGrants.length) || secret.name}
                      </div>
                    </Link>
                  ) : (
                    <div className="flex-1 min-w-0 opacity-50">
                      <div className="font-medium truncate">{secret.name}</div>
                      <div className="text-sm text-muted-foreground truncate">
                        {secret.description || secret.name}
                      </div>
                    </div>
                  )}
                  {secret.url && (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`open ${secret.name} url`}
                      onClick={(e) => {
                        e.stopPropagation()
                        e.preventDefault()
                        window.open(secret.url, '_blank', 'noopener,noreferrer')
                      }}
                    >
                      <ExternalLink className="h-4 w-4" />
                    </Button>
                  )}
                  {secret.description && sharingSummary(secret.userGrants.length, secret.roleGrants.length) && (
                    <Badge variant="outline" className="shrink-0">
                      {sharingSummary(secret.userGrants.length, secret.roleGrants.length)}
                    </Badge>
                  )}
                  {!secret.accessible ? (
                    <TooltipProvider>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Badge variant="outline" className="shrink-0">
                            <Lock className="h-3 w-3 mr-1" />
                            No access
                          </Badge>
                        </TooltipTrigger>
                        <TooltipContent><p>You do not have access to this secret</p></TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  ) : (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`delete ${secret.name}`}
                      onClick={() => { setDeleteTarget(secret.name); deleteMutation.reset(); setDeleteOpen(true) }}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  )}
                </li>
              ))}
            </ul>
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
              <SecretDataEditor initialData={createData} onChange={setCreateData} />
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
