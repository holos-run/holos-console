import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Pencil, Trash2 } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useListDeploymentTemplates, useCreateDeploymentTemplate, useDeleteDeploymentTemplate } from '@/queries/deployment-templates'
import { useGetProject } from '@/queries/projects'

const DEFAULT_CUE_TEMPLATE = `// deployment.cue — default deployment template
package holos

// #Deployment defines the shape of a deployment.
#Deployment: {
  name:      string
  namespace: string
  image:     string
  replicas?: int & >=1 | *1
}
`

export const Route = createFileRoute('/_authenticated/projects/$projectName/templates/')({
  component: DeploymentTemplatesRoute,
})

function DeploymentTemplatesRoute() {
  const { projectName } = Route.useParams()
  return <DeploymentTemplatesPage projectName={projectName} />
}

export function DeploymentTemplatesPage({ projectName: propProjectName }: { projectName?: string } = {}) {
  let routeProjectName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeProjectName = Route.useParams().projectName
  } catch {
    routeProjectName = undefined
  }
  const projectName = propProjectName ?? routeProjectName ?? ''

  const { data: templates = [], isLoading, error } = useListDeploymentTemplates(projectName)
  const { data: project } = useGetProject(projectName)
  const createMutation = useCreateDeploymentTemplate(projectName)
  const deleteMutation = useDeleteDeploymentTemplate(projectName)

  const [createOpen, setCreateOpen] = useState(false)
  const [createDisplayName, setCreateDisplayName] = useState('')
  const [createName, setCreateName] = useState('')
  const [createDescription, setCreateDescription] = useState('')
  const [createCueTemplate, setCreateCueTemplate] = useState(DEFAULT_CUE_TEMPLATE)
  const [createError, setCreateError] = useState<string | null>(null)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

  const userRole = project?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  const canDelete = userRole === Role.OWNER

  const handleCreateOpen = () => {
    setCreateDisplayName('')
    setCreateName('')
    setCreateDescription('')
    setCreateCueTemplate(DEFAULT_CUE_TEMPLATE)
    setCreateError(null)
    createMutation.reset()
    setCreateOpen(true)
  }

  const slugify = (val: string) =>
    val.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

  const handleDisplayNameChange = (val: string) => {
    setCreateDisplayName(val)
    setCreateName(slugify(val))
  }

  const handleCreate = async () => {
    if (!createName.trim()) {
      setCreateError('Template name is required')
      return
    }
    setCreateError(null)
    try {
      await createMutation.mutateAsync({
        name: createName.trim(),
        displayName: createDisplayName.trim(),
        description: createDescription.trim(),
        cueTemplate: createCueTemplate,
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
      toast.success('Template deleted')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{projectName} / Templates</CardTitle>
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
          <CardTitle>{projectName} / Templates</CardTitle>
          {canWrite && (
            <Button size="sm" onClick={handleCreateOpen}>Create Template</Button>
          )}
        </CardHeader>
        <CardContent>
          {templates.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-muted-foreground">No deployment templates yet. Create one to get started.</p>
              {canWrite && (
                <Button size="sm" onClick={handleCreateOpen}>Create Template</Button>
              )}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Description</TableHead>
                  <TableHead></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {templates.map((template) => (
                  <TableRow key={template.name}>
                    <TableCell>
                      <Link
                        to="/projects/$projectName/templates/$templateName"
                        params={{ projectName, templateName: template.name }}
                        className="font-medium hover:underline"
                      >
                        {template.name}
                      </Link>
                    </TableCell>
                    <TableCell>
                      {template.description ? (
                        <span className="text-muted-foreground truncate max-w-[60ch] block">
                          {template.description.length > 60 ? `${template.description.slice(0, 60)}…` : template.description}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        {canWrite && (
                          <Link
                            to="/projects/$projectName/templates/$templateName"
                            params={{ projectName, templateName: template.name }}
                          >
                            <Button variant="ghost" size="icon" aria-label={`edit ${template.name}`}>
                              <Pencil className="h-4 w-4" />
                            </Button>
                          </Link>
                        )}
                        {canDelete && (
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={`delete ${template.name}`}
                            onClick={() => { setDeleteTarget(template.name); deleteMutation.reset(); setDeleteOpen(true) }}
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        )}
                      </div>
                    </TableCell>
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
            <DialogTitle>Create Deployment Template</DialogTitle>
            <DialogDescription>Define a CUE-based deployment template for this project.</DialogDescription>
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
                placeholder="My Web App"
              />
            </div>
            <div>
              <Label>Name (slug)</Label>
              <Input
                aria-label="Name slug"
                value={createName}
                onChange={(e) => setCreateName(e.target.value)}
                placeholder="my-web-app"
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
                placeholder="What does this template produce?"
              />
            </div>
            <div>
              <Label htmlFor="create-cue-template">CUE Template</Label>
              <Textarea
                id="create-cue-template"
                aria-label="CUE Template"
                value={createCueTemplate}
                onChange={(e) => setCreateCueTemplate(e.target.value)}
                rows={10}
                className="font-mono text-sm"
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
            <DialogTitle>Delete Template</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete template &quot;{deleteTarget}&quot;? This action cannot be undone.
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
