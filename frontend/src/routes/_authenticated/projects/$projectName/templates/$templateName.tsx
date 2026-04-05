import { useState, useEffect } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
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
import { useDebouncedValue } from '@/hooks/use-debounced-value'

export const Route = createFileRoute('/_authenticated/projects/$projectName/templates/$templateName')({
  component: DeploymentTemplateDetailRoute,
})

interface RenderStatusIndicatorProps {
  isStale: boolean
  isRendering: boolean
  hasError: boolean
}

function RenderStatusIndicator({ isStale, isRendering, hasError }: RenderStatusIndicatorProps) {
  if (isRendering) {
    return (
      <span aria-label="Render status: rendering" className="flex items-center gap-1 text-xs text-muted-foreground">
        {/* Spinning loader */}
        <svg className="size-3 animate-spin" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
          <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z" />
        </svg>
        Rendering…
      </span>
    )
  }

  if (hasError) {
    return (
      <span aria-label="Render status: error" className="flex items-center gap-1 text-xs text-destructive">
        {/* X circle */}
        <svg className="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
          <circle cx="12" cy="12" r="10" />
          <path d="M15 9l-6 6M9 9l6 6" />
        </svg>
        Error
      </span>
    )
  }

  if (isStale) {
    return (
      <span aria-label="Render status: stale" className="flex items-center gap-1 text-xs text-amber-500">
        {/* Clock icon */}
        <svg className="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
          <circle cx="12" cy="12" r="10" />
          <path d="M12 6v6l4 2" />
        </svg>
        Out of date
      </span>
    )
  }

  return (
    <span aria-label="Render status: fresh" className="flex items-center gap-1 text-xs text-green-500">
      {/* Check circle */}
      <svg className="size-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
        <circle cx="12" cy="12" r="10" />
        <path d="M9 12l2 2 4-4" />
      </svg>
      Up to date
    </span>
  )
}

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
  const [activeTab, setActiveTab] = useState('editor')
  const [cueInput, setCueInput] = useState(
    `input: {\n  name:      "example"\n  image:     "nginx"\n  tag:       "latest"\n  project:   "${projectName}"\n  namespace: "holos-prj-${projectName}"\n}`
  )

  useEffect(() => {
    if (template?.cueTemplate !== undefined) {
      setCueTemplate(template.cueTemplate)
    }
  }, [template?.cueTemplate])

  const userRole = project?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  const canDelete = userRole === Role.OWNER

  const debouncedCueInput = useDebouncedValue(cueInput, 500)
  const debouncedCueTemplate = useDebouncedValue(cueTemplate, 500)
  const isStale = cueInput !== debouncedCueInput || cueTemplate !== debouncedCueTemplate
  const { data: renderData, error: renderError, isFetching: isRendering } = useRenderDeploymentTemplate(debouncedCueTemplate, debouncedCueInput, activeTab === 'preview')
  const renderedYaml = renderData?.renderedYaml

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

            {template?.description && (
              <div className="flex items-start gap-2">
                <span className="w-32 text-sm text-muted-foreground shrink-0 pt-0.5">Description</span>
                <span className="text-sm">{template.description}</span>
              </div>
            )}
          </div>

          <div className="space-y-4">
            <h3 className="text-sm font-medium">CUE Template</h3>
            <Separator />
            <Tabs value={activeTab} onValueChange={setActiveTab}>
              <TabsList>
                <TabsTrigger value="editor">Editor</TabsTrigger>
                <TabsTrigger value="preview">Preview</TabsTrigger>
              </TabsList>
              <TabsContent value="editor" className="mt-4 space-y-4">
                <div>
                  <Label htmlFor="cue-template-editor" className="sr-only">CUE Template</Label>
                  <Textarea
                    id="cue-template-editor"
                    aria-label="CUE Template"
                    value={cueTemplate}
                    onChange={(e) => setCueTemplate(e.target.value)}
                    rows={20}
                    className="font-mono text-sm field-sizing-normal max-h-96 overflow-y-auto"
                    readOnly={!canWrite}
                  />
                </div>
                {canWrite && (
                  <div className="flex justify-end">
                    <Button onClick={handleSave} disabled={updateMutation.isPending}>
                      {updateMutation.isPending ? 'Saving...' : 'Save'}
                    </Button>
                  </div>
                )}
              </TabsContent>
              <TabsContent value="preview" className="mt-4 space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="cue-input-editor">CUE Input</Label>
                  <Textarea
                    id="cue-input-editor"
                    aria-label="CUE Input"
                    value={cueInput}
                    onChange={(e) => setCueInput(e.target.value)}
                    rows={8}
                    className="font-mono text-sm field-sizing-normal max-h-64 overflow-y-auto"
                  />
                </div>
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <Label>Rendered YAML</Label>
                    <RenderStatusIndicator isStale={isStale} isRendering={isRendering} hasError={!!renderError} />
                  </div>
                  {renderError ? (
                    <Alert variant="destructive">
                      <AlertDescription aria-label="Preview error">{renderError.message}</AlertDescription>
                    </Alert>
                  ) : (
                    <pre
                      aria-label="Rendered YAML"
                      className="font-mono text-sm bg-muted rounded-md p-4 overflow-auto whitespace-pre"
                    >
                      {renderedYaml ?? ''}
                    </pre>
                  )}
                </div>
              </TabsContent>
            </Tabs>
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
