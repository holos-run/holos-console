import { useState, useEffect } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Pencil } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useGetDeploymentTemplate, useUpdateDeploymentTemplate, useDeleteDeploymentTemplate, useRenderDeploymentTemplate } from '@/queries/deployment-templates'
import { useGetProject } from '@/queries/projects'
import { CueTemplateEditor } from '@/components/cue-template-editor'
import { LinkifiedText } from '@/components/linkified-text'

export const Route = createFileRoute('/_authenticated/projects/$projectName/templates/$templateName')({
  component: DeploymentTemplateDetailRoute,
})

function DeploymentTemplateDetailRoute() {
  const { projectName, templateName } = Route.useParams()
  return <DeploymentTemplateDetailPage projectName={projectName} templateName={templateName} />
}

export function DeploymentTemplateDetailPage({ projectName: propProjectName, templateName: propTemplateName }: { projectName?: string; templateName?: string } = {}) {
  let routeParams: { projectName?: string; templateName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const projectName = propProjectName ?? routeParams.projectName ?? ''
  const templateName = propTemplateName ?? routeParams.templateName ?? ''

  const navigate = useNavigate()
  const { data: template, isPending, error } = useGetDeploymentTemplate(projectName, templateName)
  const { data: project } = useGetProject(projectName)
  const updateMutation = useUpdateDeploymentTemplate(projectName, templateName)
  const deleteMutation = useDeleteDeploymentTemplate(projectName)

  const [cueTemplate, setCueTemplate] = useState('')
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [descEditOpen, setDescEditOpen] = useState(false)
  const [draftDescription, setDraftDescription] = useState('')
  const [descEditError, setDescEditError] = useState<string | null>(null)

  useEffect(() => {
    if (template?.cueTemplate !== undefined) {
      setCueTemplate(template.cueTemplate)
    }
  }, [template?.cueTemplate])

  const userRole = project?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  const canDelete = userRole === Role.OWNER

  const defaultSystemInput = `system: {\n  project:          "${projectName}"\n  namespace:        "holos-prj-${projectName}"\n  gatewayNamespace: "istio-ingress"\n  claims: {\n    iss:            "https://login.example.com"\n    sub:            "user-abc123"\n    iat:            1743868800\n    exp:            1743872400\n    email:          "developer@example.com"\n    email_verified: true\n  }\n}`
  const defaultUserInput = `input: {\n  name:  "example"\n  image: "nginx"\n  tag:   "latest"\n  port:  8080\n}`

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync({
        displayName: template?.displayName,
        description: template?.description,
        cueTemplate,
      })
      toast.success('Saved')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  const handleDeleteConfirm = async () => {
    try {
      await deleteMutation.mutateAsync({ name: templateName })
      setDeleteOpen(false)
      navigate({ to: '/projects/$projectName/templates', params: { projectName } })
    } catch { /* error shown via mutation */ }
  }

  const handleOpenDescEdit = () => {
    setDraftDescription(template?.description ?? '')
    setDescEditError(null)
    setDescEditOpen(true)
  }

  const handleSaveDescription = async () => {
    try {
      await updateMutation.mutateAsync({ description: draftDescription })
      toast.success('Saved')
      setDescEditOpen(false)
    } catch (err) {
      setDescEditError(err instanceof Error ? err.message : String(err))
    }
  }

  if (isPending) {
    return (
      <Card>
        <CardContent className="pt-6 space-y-4">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-40 w-full" />
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
        <CardContent className="pt-6 space-y-6">
          <div>
            <p className="text-sm text-muted-foreground">{projectName} / Templates / {templateName}</p>
            <h2 className="text-xl font-semibold mt-1">{template?.displayName || templateName}</h2>
          </div>

          <div className="space-y-4">
            <h3 className="text-sm font-medium">General</h3>
            <Separator />

            <div className="flex items-center gap-2">
              <span className="w-32 text-sm text-muted-foreground shrink-0">Name</span>
              <span className="text-sm font-mono">{templateName}</span>
            </div>

            <div className="flex items-start gap-2">
              <span className="w-32 text-sm text-muted-foreground shrink-0 pt-0.5">Description</span>
              <div className="flex items-start gap-1 flex-1">
                {template?.description ? (
                  <span className="text-sm"><LinkifiedText text={template.description} /></span>
                ) : (
                  <span className="text-sm text-muted-foreground">No description</span>
                )}
                {canWrite && (
                  <button
                    aria-label="edit description"
                    onClick={handleOpenDescEdit}
                    className="ml-1 p-0.5 text-muted-foreground hover:text-foreground shrink-0"
                  >
                    <Pencil className="size-3.5" />
                  </button>
                )}
              </div>
            </div>
          </div>

          <div className="space-y-4">
            <h3 className="text-sm font-medium">CUE Template</h3>
            <Separator />
            <CueTemplateEditor
              cueTemplate={cueTemplate}
              onChange={setCueTemplate}
              readOnly={!canWrite}
              onSave={handleSave}
              isSaving={updateMutation.isPending}
              defaultSystemInput={defaultSystemInput}
              defaultUserInput={defaultUserInput}
              useRenderFn={useRenderDeploymentTemplate}
            />
          </div>

          {canDelete && (
            <div className="space-y-4">
              <h3 className="text-sm font-medium text-destructive">Danger Zone</h3>
              <Separator />
              <Button variant="destructive" onClick={() => { deleteMutation.reset(); setDeleteOpen(true) }}>
                Delete Template
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog open={descEditOpen} onOpenChange={setDescEditOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Edit Description</DialogTitle>
            <DialogDescription>
              Update the description for template &quot;{templateName}&quot;.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="desc-edit-textarea">Description</Label>
            <Textarea
              id="desc-edit-textarea"
              aria-label="Description"
              value={draftDescription}
              onChange={(e) => setDraftDescription(e.target.value)}
              rows={4}
            />
          </div>
          {descEditError && (
            <Alert variant="destructive">
              <AlertDescription>{descEditError}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDescEditOpen(false)}>Cancel</Button>
            <Button onClick={handleSaveDescription} disabled={updateMutation.isPending}>
              {updateMutation.isPending ? 'Saving...' : 'Save'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Template</DialogTitle>
            <DialogDescription>
              This will permanently delete template &quot;{templateName}&quot;. This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {deleteMutation.error && (
            <Alert variant="destructive">
              <AlertDescription>{deleteMutation.error.message}</AlertDescription>
            </Alert>
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
