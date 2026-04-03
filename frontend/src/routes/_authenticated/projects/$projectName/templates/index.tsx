import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
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
import { useListDeploymentTemplates, useDeleteDeploymentTemplate } from '@/queries/deployment-templates'
import { useGetProject } from '@/queries/projects'
import { CreateTemplateModal } from '@/components/create-template-modal'

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
  const deleteMutation = useDeleteDeploymentTemplate(projectName)

  const [createOpen, setCreateOpen] = useState(false)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

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
            <Button size="sm" onClick={() => setCreateOpen(true)}>Create Template</Button>
          )}
        </CardHeader>
        <CardContent>
          {templates.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-muted-foreground">No deployment templates yet. Create one to get started.</p>
              {canWrite && (
                <Button size="sm" onClick={() => setCreateOpen(true)}>Create Template</Button>
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

      <CreateTemplateModal
        projectName={projectName}
        open={createOpen}
        onOpenChange={setCreateOpen}
      />

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
