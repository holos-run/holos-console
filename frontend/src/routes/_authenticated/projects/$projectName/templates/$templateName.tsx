import { useState, useEffect } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Pencil, Copy, Lock, ArrowUpCircle } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Checkbox } from '@/components/ui/checkbox'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useGetTemplate, useUpdateTemplate, useDeleteTemplate, useCloneTemplate, useListLinkableTemplates, useCheckUpdates, makeProjectScope, TemplateScope, linkableKey, parseLinkableKey } from '@/queries/templates'
import type { LinkedTemplateRef } from '@/queries/templates'
import { useGetProject } from '@/queries/projects'
import { CueTemplateEditor } from '@/components/cue-template-editor'
import { LinkifiedText } from '@/components/linkified-text'
import { UpgradeDialog } from '@/components/template-updates'

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
  const scope = makeProjectScope(projectName)
  const { data: template, isPending, error } = useGetTemplate(scope, templateName)
  const { data: project } = useGetProject(projectName)
  const { data: linkableTemplates = [], isSuccess: linkableReady } = useListLinkableTemplates(scope)
  const updateMutation = useUpdateTemplate(scope, templateName)
  const deleteMutation = useDeleteTemplate(scope)
  const cloneMutation = useCloneTemplate(scope)

  const [cueTemplate, setCueTemplate] = useState('')
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [descEditOpen, setDescEditOpen] = useState(false)
  const [draftDescription, setDraftDescription] = useState('')
  const [descEditError, setDescEditError] = useState<string | null>(null)
  const [cloneOpen, setCloneOpen] = useState(false)
  const [cloneName, setCloneName] = useState('')
  const [cloneDisplayName, setCloneDisplayName] = useState('')
  const [cloneError, setCloneError] = useState<string | null>(null)
  const [linkedEditOpen, setLinkedEditOpen] = useState(false)
  const [draftLinkedTemplateKeys, setDraftLinkedTemplateKeys] = useState<string[]>([])
  const [linkedEditError, setLinkedEditError] = useState<string | null>(null)
  const [upgradeOpen, setUpgradeOpen] = useState(false)

  // Check for available updates on this template's linked templates.
  const { data: templateUpdates = [] } = useCheckUpdates(scope, templateName)

  useEffect(() => {
    if (template?.cueTemplate !== undefined) {
      setCueTemplate(template.cueTemplate)
    }
  }, [template?.cueTemplate])

  const userRole = project?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  const canDelete = userRole === Role.OWNER
  const canEditLinks = userRole === Role.OWNER

  const defaultPlatformInput = `platform: {\n  project:          "${projectName}"\n  namespace:        "holos-prj-${projectName}"\n  gatewayNamespace: "istio-ingress"\n  claims: {\n    iss:            "https://login.example.com"\n    sub:            "user-abc123"\n    iat:            1743868800\n    exp:            1743872400\n    email:          "developer@example.com"\n    email_verified: true\n  }\n}`
  const defaultProjectInput = `input: {\n  name:  "example"\n  image: "nginx"\n  tag:   "latest"\n  port:  8080\n}`

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

  const handleOpenLinkedEdit = () => {
    setDraftLinkedTemplateKeys((template?.linkedTemplates ?? []).map(t => linkableKey(t.scope, t.scopeName, t.name)))
    setLinkedEditError(null)
    setLinkedEditOpen(true)
  }

  const handleSaveLinkedTemplates = async () => {
    try {
      // Parse composite keys back into LinkedTemplateRef objects.
      const linkedTemplates: LinkedTemplateRef[] = draftLinkedTemplateKeys
        .map((key) => {
          const parsed = parseLinkableKey(key)
          return { scope: parsed.scope, scopeName: parsed.scopeName, name: parsed.name } as LinkedTemplateRef
        })
      await updateMutation.mutateAsync({ linkedTemplates, updateLinkedTemplates: true })
      toast.success('Saved')
      setLinkedEditOpen(false)
    } catch (err) {
      setLinkedEditError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleOpenClone = () => {
    setCloneName('')
    setCloneDisplayName(template?.displayName ?? '')
    setCloneError(null)
    setCloneOpen(true)
  }

  const handleCloneConfirm = async () => {
    setCloneError(null)
    try {
      const response = await cloneMutation.mutateAsync({
        sourceName: templateName,
        name: cloneName,
        displayName: cloneDisplayName,
      })
      toast.success(`Cloned to "${response.name}"`)
      setCloneOpen(false)
      navigate({
        to: '/projects/$projectName/templates/$templateName',
        params: { projectName, templateName: response.name },
      })
    } catch (err) {
      setCloneError(err instanceof Error ? err.message : String(err))
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

            <div className="flex items-start gap-2">
              <span className="w-32 text-sm text-muted-foreground shrink-0 pt-0.5">Linked Platform Templates</span>
              <div className="flex items-start gap-1 flex-1">
                <div className="flex-1">
                  {(() => {
                    if (!linkableReady) {
                      return <span className="text-sm text-muted-foreground">Loading...</span>
                    }
                    if (linkableTemplates.length === 0) {
                      return (
                        <div className="space-y-1">
                          <span className="text-sm text-muted-foreground">None linked</span>
                          <p className="text-xs text-muted-foreground">No platform templates available to link. Create organization or folder templates to enable linking.</p>
                        </div>
                      )
                    }
                    const linkedKeys = (template?.linkedTemplates ?? []).map(t => linkableKey(t.scope, t.scopeName, t.name))
                    const mandatoryTemplates = linkableTemplates.filter((t) => t.mandatory)
                    const keyOf = (t: (typeof linkableTemplates)[number]) => linkableKey(t.scopeRef?.scope, t.scopeRef?.scopeName, t.name)
                    const allLinked = [
                      ...mandatoryTemplates.filter((t) => !linkedKeys.includes(keyOf(t))),
                      ...linkableTemplates.filter((t) => linkedKeys.includes(keyOf(t)) || t.mandatory),
                    ]
                    const dedupedLinked = allLinked.filter(
                      (t, i, arr) => arr.findIndex((x) => keyOf(x) === keyOf(t)) === i,
                    )
                    if (dedupedLinked.length === 0) {
                      return <span className="text-sm text-muted-foreground">None linked</span>
                    }
                    return (
                      <div className="flex flex-col gap-2">
                        <div className="flex flex-wrap gap-1">
                          {dedupedLinked.map((t) => {
                            const scopeLbl = t.scopeRef?.scope === TemplateScope.ORGANIZATION ? 'Org' : t.scopeRef?.scope === TemplateScope.FOLDER ? 'Folder' : undefined
                            // Look up the version constraint from the template's linkedTemplates.
                            const linkedRef = (template?.linkedTemplates ?? []).find(
                              (lt) => lt.scope === t.scopeRef?.scope && lt.scopeName === t.scopeRef?.scopeName && lt.name === t.name
                            )
                            const constraint = linkedRef?.versionConstraint
                            return (
                              <span key={keyOf(t)} className="inline-flex items-center gap-1 text-xs bg-muted px-2 py-0.5 rounded-full">
                                {t.displayName || t.name}
                                {scopeLbl && <span className="text-xs text-muted-foreground">{scopeLbl}</span>}
                                {constraint && <span className="text-xs font-mono text-muted-foreground">{constraint}</span>}
                                {t.mandatory && <Lock className="h-3 w-3 text-muted-foreground" aria-label="mandatory" />}
                              </span>
                            )
                          })}
                        </div>
                        {templateUpdates.length > 0 && (
                          <button
                            onClick={() => setUpgradeOpen(true)}
                            className="inline-flex items-center gap-1 text-xs text-primary hover:underline cursor-pointer w-fit"
                          >
                            <ArrowUpCircle className="h-3 w-3" />
                            {templateUpdates.length === 1 ? '1 update available' : `${templateUpdates.length} updates available`}
                          </button>
                        )}
                      </div>
                    )
                  })()}
                </div>
                {canWrite && linkableTemplates.length > 0 && (
                  <button
                    aria-label="edit linked platform templates"
                    onClick={handleOpenLinkedEdit}
                    className="ml-1 p-0.5 text-muted-foreground hover:text-foreground shrink-0"
                  >
                    <Pencil className="size-3.5" />
                  </button>
                )}
              </div>
            </div>
          </div>

          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium">CUE Template</h3>
              <Button variant="outline" size="sm" onClick={handleOpenClone}>
                <Copy className="h-3.5 w-3.5 mr-1.5" />
                Clone
              </Button>
            </div>
            <Separator />
            <CueTemplateEditor
              cueTemplate={cueTemplate}
              onChange={setCueTemplate}
              readOnly={!canWrite}
              onSave={handleSave}
              isSaving={updateMutation.isPending}
              defaultPlatformInput={defaultPlatformInput}
              defaultProjectInput={defaultProjectInput}
              scope={scope}
              linkedTemplates={template?.linkedTemplates ?? []}
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

      <Dialog open={linkedEditOpen} onOpenChange={setLinkedEditOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Linked Platform Templates</DialogTitle>
            <DialogDescription>
              Select platform templates to unify with this deployment template at render time. Mandatory templates are always included.
            </DialogDescription>
          </DialogHeader>
          {!canEditLinks && (
            <Alert>
              <AlertDescription>OWNER permission is required to modify linked templates.</AlertDescription>
            </Alert>
          )}
          <div className="space-y-4" aria-label="Linked platform templates">
            {(() => {
              const orgTemplates = linkableTemplates.filter(
                (t) => t.scopeRef?.scope === TemplateScope.ORGANIZATION,
              )
              const folderTemplates = linkableTemplates.filter(
                (t) => t.scopeRef?.scope === TemplateScope.FOLDER,
              )
              const renderGroup = (templates: typeof linkableTemplates) =>
                templates.map((t) => {
                  const key = linkableKey(t.scopeRef?.scope, t.scopeRef?.scopeName, t.name)
                  return (
                  <div key={key} className="flex items-start gap-2">
                    <Checkbox
                      id={`linked-edit-${key}`}
                      checked={t.mandatory || draftLinkedTemplateKeys.includes(key)}
                      disabled={t.mandatory || !canEditLinks}
                      onCheckedChange={(checked) => {
                        if (t.mandatory || !canEditLinks) return
                        setDraftLinkedTemplateKeys((prev) =>
                          checked ? [...prev, key] : prev.filter((k) => k !== key),
                        )
                      }}
                    />
                    <div className="flex flex-col">
                      <label htmlFor={`linked-edit-${key}`} className="text-sm font-medium leading-none cursor-pointer flex items-center gap-1">
                        {t.displayName || t.name}
                        {t.mandatory && (
                          <TooltipProvider>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <Lock className="h-3 w-3 text-muted-foreground" aria-label="mandatory" />
                              </TooltipTrigger>
                              <TooltipContent>
                                <p>This platform template is mandatory and always applied.</p>
                              </TooltipContent>
                            </Tooltip>
                          </TooltipProvider>
                        )}
                      </label>
                      {t.description && (
                        <p className="text-xs text-muted-foreground mt-0.5">{t.description}</p>
                      )}
                      {(() => {
                        const linkedRef = (template?.linkedTemplates ?? []).find(
                          (lt) => lt.scope === t.scopeRef?.scope && lt.scopeName === t.scopeRef?.scopeName && lt.name === t.name
                        )
                        if (!linkedRef?.versionConstraint) return null
                        return (
                          <span className="text-xs font-mono text-muted-foreground mt-0.5">
                            Version: {linkedRef.versionConstraint}
                          </span>
                        )
                      })()}
                    </div>
                  </div>
                  )
                })
              return (
                <>
                  {orgTemplates.length > 0 && (
                    <div className="space-y-2">
                      <h4 className="text-sm font-medium text-muted-foreground">Organization Templates</h4>
                      {renderGroup(orgTemplates)}
                    </div>
                  )}
                  {folderTemplates.length > 0 && (
                    <div className="space-y-2">
                      <h4 className="text-sm font-medium text-muted-foreground">Folder Templates</h4>
                      {renderGroup(folderTemplates)}
                    </div>
                  )}
                </>
              )
            })()}
          </div>
          {linkedEditError && (
            <Alert variant="destructive">
              <AlertDescription>{linkedEditError}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setLinkedEditOpen(false)}>Cancel</Button>
            {canEditLinks && (
              <Button onClick={handleSaveLinkedTemplates} disabled={updateMutation.isPending}>
                {updateMutation.isPending ? 'Saving...' : 'Save'}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={cloneOpen} onOpenChange={setCloneOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Clone Deployment Template</DialogTitle>
            <DialogDescription>
              Create a copy of &quot;{templateName}&quot; with a new name.
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

      <UpgradeDialog
        open={upgradeOpen}
        onOpenChange={setUpgradeOpen}
        updates={templateUpdates}
        scope={scope}
        templateName={templateName}
        linkedTemplates={template?.linkedTemplates ?? []}
      />
    </>
  )
}
