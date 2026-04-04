import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { CreateTemplateModal } from '@/components/create-template-modal'
import { StringListInput } from '@/components/string-list-input'
import { EnvVarEditor } from '@/components/env-var-editor'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Trash2 } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { DeploymentPhase } from '@/gen/holos/console/v1/deployments_pb'
import type { EnvVar } from '@/gen/holos/console/v1/deployments_pb'
import { useListDeployments, useCreateDeployment, useDeleteDeployment } from '@/queries/deployments'
import { useListDeploymentTemplates } from '@/queries/deployment-templates'
import { useGetProject } from '@/queries/projects'

// filterEnvVars removes incomplete rows before submit.
// A row is valid if the name is non-empty and the source is complete.
function filterEnvVars(envVars: EnvVar[]): EnvVar[] {
  return envVars.filter((ev) => {
    if (!ev.name.trim()) return false
    if (ev.source.case === 'value') return true
    if (ev.source.case === 'secretKeyRef') return !!(ev.source.value.name && ev.source.value.key)
    if (ev.source.case === 'configMapKeyRef') return !!(ev.source.value.name && ev.source.value.key)
    return false
  })
}

export const Route = createFileRoute('/_authenticated/projects/$projectName/deployments/')({
  component: DeploymentsRoute,
})

function DeploymentsRoute() {
  const { projectName } = Route.useParams()
  return <DeploymentsPage projectName={projectName} />
}

export function DeploymentsPage({ projectName: propProjectName }: { projectName?: string } = {}) {
  let routeProjectName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeProjectName = Route.useParams().projectName
  } catch {
    routeProjectName = undefined
  }
  const projectName = propProjectName ?? routeProjectName ?? ''

  const { data: deployments = [], isLoading, error } = useListDeployments(projectName)
  const { data: project } = useGetProject(projectName)
  const { data: templates = [] } = useListDeploymentTemplates(projectName)
  const createMutation = useCreateDeployment(projectName)
  const deleteMutation = useDeleteDeployment(projectName)

  const [createOpen, setCreateOpen] = useState(false)
  const [createDisplayName, setCreateDisplayName] = useState('')
  const [createName, setCreateName] = useState('')
  const [createDescription, setCreateDescription] = useState('')
  const [createTemplate, setCreateTemplate] = useState('')
  const [createImage, setCreateImage] = useState('')
  const [createTag, setCreateTag] = useState('')
  const [createCommand, setCreateCommand] = useState<string[]>([])
  const [createArgs, setCreateArgs] = useState<string[]>([])
  const [createEnv, setCreateEnv] = useState<EnvVar[]>([])
  const [createError, setCreateError] = useState<string | null>(null)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

  const [templateSubModalOpen, setTemplateSubModalOpen] = useState(false)

  const userRole = project?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  const canDelete = userRole === Role.OWNER

  const slugify = (val: string) =>
    val.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

  const handleDisplayNameChange = (val: string) => {
    setCreateDisplayName(val)
    setCreateName(slugify(val))
  }

  const handleCreateOpen = () => {
    setCreateDisplayName('')
    setCreateName('')
    setCreateDescription('')
    setCreateTemplate('')
    setCreateImage('')
    setCreateTag('')
    setCreateCommand([])
    setCreateArgs([])
    setCreateEnv([])
    setCreateError(null)
    createMutation.reset()
    setCreateOpen(true)
  }

  const handleCreate = async () => {
    if (!createName.trim()) {
      setCreateError('Name is required')
      return
    }
    if (!createTemplate) {
      setCreateError('Template is required')
      return
    }
    if (!createImage.trim()) {
      setCreateError('Image is required')
      return
    }
    if (!createTag.trim()) {
      setCreateError('Tag is required')
      return
    }
    setCreateError(null)
    try {
      await createMutation.mutateAsync({
        name: createName.trim(),
        displayName: createDisplayName.trim(),
        description: createDescription.trim(),
        template: createTemplate,
        image: createImage.trim(),
        tag: createTag.trim(),
        command: createCommand,
        args: createArgs,
        env: filterEnvVars(createEnv),
      })
      setCreateOpen(false)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return
    try {
      await deleteMutation.mutateAsync({ name: deleteTarget })
      setDeleteOpen(false)
      setDeleteTarget(null)
      toast.success('Deployment deleted')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{projectName} / Deployments</CardTitle>
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
          <CardTitle>{projectName} / Deployments</CardTitle>
          {canWrite && (
            <Button size="sm" onClick={handleCreateOpen}>Create Deployment</Button>
          )}
        </CardHeader>
        <CardContent>
          {deployments.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-muted-foreground">No deployments yet. Create one to get started.</p>
              {canWrite && (
                <Button size="sm" onClick={handleCreateOpen}>Create Deployment</Button>
              )}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Image</TableHead>
                  <TableHead>Tag</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {deployments.map((deployment) => (
                  <TableRow key={deployment.name}>
                    <TableCell>
                      <Link
                        to="/projects/$projectName/deployments/$deploymentName"
                        params={{ projectName, deploymentName: deployment.name }}
                        className="font-medium hover:underline"
                      >
                        {deployment.name}
                      </Link>
                    </TableCell>
                    <TableCell className="font-mono text-sm">{deployment.image}</TableCell>
                    <TableCell className="font-mono text-sm">{deployment.tag}</TableCell>
                    <TableCell>
                      <PhaseBadge phase={deployment.phase} />
                    </TableCell>
                    <TableCell className="text-right">
                      {canDelete && (
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={`delete ${deployment.name}`}
                          onClick={() => { setDeleteTarget(deployment.name); deleteMutation.reset(); setDeleteOpen(true) }}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Create Deployment</DialogTitle>
            <DialogDescription>Deploy a new application to this project.</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div>
              <Label htmlFor="create-display-name">Display Name</Label>
              <Input
                id="create-display-name"
                aria-label="Display Name"
                autoFocus
                value={createDisplayName}
                onChange={(e) => handleDisplayNameChange(e.target.value)}
                placeholder="My API"
              />
            </div>
            <div>
              <Label>Name (slug)</Label>
              <Input
                aria-label="Name slug"
                value={createName}
                onChange={(e) => setCreateName(e.target.value)}
                placeholder="my-api"
              />
              <p className="text-xs text-muted-foreground mt-1">Auto-derived from display name. Lowercase alphanumeric and hyphens only.</p>
            </div>
            <div>
              <Label htmlFor="create-description">Description</Label>
              <Input
                id="create-description"
                aria-label="Description"
                value={createDescription}
                onChange={(e) => setCreateDescription(e.target.value)}
                placeholder="What does this deployment serve?"
              />
            </div>
            <div>
              <Label>Template</Label>
              <Select value={createTemplate} onValueChange={setCreateTemplate}>
                <SelectTrigger aria-label="Template">
                  <SelectValue placeholder="Select a template..." />
                </SelectTrigger>
                <SelectContent>
                  {templates.map((t) => (
                    <SelectItem key={t.name} value={t.name}>{t.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {templates.length === 0 && canWrite && (
                <p className="text-sm text-muted-foreground mt-1">
                  No templates yet.{' '}
                  <button
                    type="button"
                    className="underline"
                    onClick={() => setTemplateSubModalOpen(true)}
                  >
                    Create one now
                  </button>
                </p>
              )}
            </div>
            <div>
              <Label htmlFor="create-image">Image</Label>
              <Input
                id="create-image"
                aria-label="Image"
                value={createImage}
                onChange={(e) => setCreateImage(e.target.value)}
                placeholder="ghcr.io/org/app"
              />
            </div>
            <div>
              <Label htmlFor="create-tag">Tag</Label>
              <Input
                id="create-tag"
                aria-label="Tag"
                value={createTag}
                onChange={(e) => setCreateTag(e.target.value)}
                placeholder="v1.0.0"
              />
            </div>
            <div>
              <Label>Command</Label>
              <p className="text-xs text-muted-foreground mb-1">Override container ENTRYPOINT (optional)</p>
              <StringListInput
                value={createCommand}
                onChange={setCreateCommand}
                placeholder="command entry"
                addLabel="Add command"
              />
            </div>
            <div>
              <Label>Args</Label>
              <p className="text-xs text-muted-foreground mb-1">Override container CMD (optional)</p>
              <StringListInput
                value={createArgs}
                onChange={setCreateArgs}
                placeholder="args entry"
                addLabel="Add args"
              />
            </div>
            <div>
              <Label>Environment Variables</Label>
              <p className="text-xs text-muted-foreground mb-1">Set container environment variables (optional)</p>
              <EnvVarEditor
                project={projectName}
                value={createEnv}
                onChange={setCreateEnv}
              />
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
            <DialogTitle>Delete Deployment</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete deployment &quot;{deleteTarget}&quot;? This action cannot be undone.
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

      <CreateTemplateModal
        projectName={projectName}
        open={templateSubModalOpen}
        onOpenChange={setTemplateSubModalOpen}
        onCreated={(name) => {
          setCreateTemplate(name)
          setTemplateSubModalOpen(false)
        }}
      />
    </>
  )
}

function PhaseBadge({ phase }: { phase: DeploymentPhase }) {
  switch (phase) {
    case DeploymentPhase.RUNNING:
      return <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200 border-transparent">Running</Badge>
    case DeploymentPhase.PENDING:
      return <Badge className="bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200 border-transparent">Pending</Badge>
    case DeploymentPhase.FAILED:
      return <Badge variant="destructive">Failed</Badge>
    case DeploymentPhase.SUCCEEDED:
      return <Badge className="bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200 border-transparent">Succeeded</Badge>
    default:
      return <Badge variant="outline">Unknown</Badge>
  }
}
