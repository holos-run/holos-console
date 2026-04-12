import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
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
import { Pencil, Trash2, Copy } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useListTemplates, useDeleteTemplate, useCloneTemplate, useCheckUpdates, useGetTemplate, makeProjectScope } from '@/queries/templates'
import { useGetProject } from '@/queries/projects'
import { UpdatesAvailableBadge, UpgradeDialog } from '@/components/template-updates'

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

  const scope = makeProjectScope(projectName)
  const { data: templates = [], isLoading, error } = useListTemplates(scope)
  const { data: project } = useGetProject(projectName)
  const deleteMutation = useDeleteTemplate(scope)
  const cloneMutation = useCloneTemplate(scope)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [cloneOpen, setCloneOpen] = useState(false)
  const [cloneSource, setCloneSource] = useState<string | null>(null)
  const [cloneName, setCloneName] = useState('')
  const [cloneDisplayName, setCloneDisplayName] = useState('')
  const [cloneError, setCloneError] = useState<string | null>(null)
  const [upgradeOpen, setUpgradeOpen] = useState(false)
  const [upgradeTemplateName, setUpgradeTemplateName] = useState<string | null>(null)

  // Fetch updates for the selected upgrade template (when dialog is open).
  const { data: upgradeUpdates = [] } = useCheckUpdates(scope, upgradeTemplateName ?? '')
  // Fetch the selected template to get its linkedTemplates for the dialog.
  const { data: upgradeTemplate } = useGetTemplate(scope, upgradeTemplateName ?? '')

  const handleOpenUpgrade = (templateName: string) => {
    setUpgradeTemplateName(templateName)
    setUpgradeOpen(true)
  }

  const userRole = project?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  const canDelete = userRole === Role.OWNER

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

  const handleOpenClone = (sourceName: string) => {
    setCloneSource(sourceName)
    setCloneName('')
    setCloneDisplayName('')
    setCloneError(null)
    setCloneOpen(true)
  }

  const handleCloneConfirm = async () => {
    if (!cloneSource) return
    setCloneError(null)
    try {
      await cloneMutation.mutateAsync({
        sourceName: cloneSource,
        name: cloneName,
        displayName: cloneDisplayName,
      })
      toast.success(`Cloned to "${cloneName}"`)
      setCloneOpen(false)
      setCloneSource(null)
    } catch (err) {
      setCloneError(err instanceof Error ? err.message : String(err))
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
            <Link to="/projects/$projectName/templates/new" params={{ projectName }}>
              <Button size="sm">Create Template</Button>
            </Link>
          )}
        </CardHeader>
        <CardContent>
          {templates.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-muted-foreground">No deployment templates yet. Create one to get started.</p>
              {canWrite && (
                <Link to="/projects/$projectName/templates/new" params={{ projectName }}>
                  <Button size="sm">Create Template</Button>
                </Link>
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
                      <div className="flex items-center gap-2">
                        <Link
                          to="/projects/$projectName/templates/$templateName"
                          params={{ projectName, templateName: template.name }}
                          className="font-medium hover:underline"
                        >
                          {template.name}
                        </Link>
                        <UpdatesAvailableBadge
                          scope={scope}
                          templateName={template.name}
                          onClick={() => handleOpenUpgrade(template.name)}
                        />
                      </div>
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
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={`clone ${template.name}`}
                          onClick={() => handleOpenClone(template.name)}
                        >
                          <Copy className="h-4 w-4" />
                        </Button>
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

      <Dialog open={cloneOpen} onOpenChange={setCloneOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Clone Deployment Template</DialogTitle>
            <DialogDescription>
              Create a copy of &quot;{cloneSource}&quot; with a new name.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="clone-name">Name</Label>
              <Input
                id="clone-name"
                aria-label="Name"
                value={cloneName}
                onChange={(e) => setCloneName(e.target.value)}
                placeholder="my-template-copy"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="clone-display-name">Display Name</Label>
              <Input
                id="clone-display-name"
                aria-label="Display Name"
                value={cloneDisplayName}
                onChange={(e) => setCloneDisplayName(e.target.value)}
                placeholder="My Template Copy"
              />
            </div>
          </div>
          {cloneError && (
            <Alert variant="destructive">
              <AlertDescription>{cloneError}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCloneOpen(false)}>Cancel</Button>
            <Button onClick={handleCloneConfirm} disabled={cloneMutation.isPending || !cloneName}>
              {cloneMutation.isPending ? 'Cloning...' : 'Clone'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {upgradeTemplateName && (
        <UpgradeDialog
          open={upgradeOpen}
          onOpenChange={(open) => {
            setUpgradeOpen(open)
            if (!open) setUpgradeTemplateName(null)
          }}
          updates={upgradeUpdates}
          scope={scope}
          templateName={upgradeTemplateName}
          linkedTemplates={upgradeTemplate?.linkedTemplates ?? []}
        />
      )}
    </>
  )
}
