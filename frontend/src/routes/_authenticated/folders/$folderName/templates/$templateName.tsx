import { useState, useEffect } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Copy } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import {
  useGetTemplate,
  useUpdateTemplate,
  useDeleteTemplate,
  useCloneTemplate,
  makeFolderScope,
} from '@/queries/templates'
import { useGetFolder } from '@/queries/folders'
import { useGetOrganization } from '@/queries/organizations'
import { CueTemplateEditor } from '@/components/cue-template-editor'
import { TemplateReleases } from '@/components/template-releases'
import { PlatformTemplateEnabledToggle } from '@/components/platform-template-enabled-toggle'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/templates/$templateName',
)({
  component: FolderTemplateDetailRoute,
})

function FolderTemplateDetailRoute() {
  const { folderName, templateName } = Route.useParams()
  return <FolderTemplateDetailPage folderName={folderName} templateName={templateName} />
}

export function FolderTemplateDetailPage({
  folderName: propFolderName,
  templateName: propTemplateName,
}: { folderName?: string; templateName?: string } = {}) {
  let routeParams: { folderName?: string; templateName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const folderName = propFolderName ?? routeParams.folderName ?? ''
  const templateName = propTemplateName ?? routeParams.templateName ?? ''

  let navigate: ReturnType<typeof useNavigate> | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    navigate = useNavigate()
  } catch {
    navigate = undefined
  }

  const { data: folder } = useGetFolder(folderName)
  const orgName = folder?.organization ?? ''
  // The authoring org's gatewayNamespace (HOL-526) is mirrored into the
  // platform-input preview default so the preview matches what the backend
  // will inject at render time. Folder templates inherit this from the
  // folder's parent organization.
  const { data: org } = useGetOrganization(orgName)

  const scope = makeFolderScope(folderName)
  const { data: template, isPending, error } = useGetTemplate(scope, templateName)
  const updateMutation = useUpdateTemplate(scope, templateName)
  const deleteMutation = useDeleteTemplate(scope)
  const cloneMutation = useCloneTemplate(scope)

  const [cueTemplate, setCueTemplate] = useState('')
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [cloneOpen, setCloneOpen] = useState(false)
  const [cloneName, setCloneName] = useState('')
  const [cloneDisplayName, setCloneDisplayName] = useState('')
  const [cloneError, setCloneError] = useState<string | null>(null)

  useEffect(() => {
    if (template?.cueTemplate !== undefined) {
      setCueTemplate(template.cueTemplate)
    }
  }, [template?.cueTemplate])

  // Only folder OWNERs can manage folder-level platform templates.
  const userRole = folder?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  // Falls back to "istio-ingress" when the org has not configured one.
  const gatewayNamespace = org?.gatewayNamespace || 'istio-ingress'
  const defaultPlatformInput = `platform: {\n  project:          "example-project"\n  namespace:        "prj-example-project"\n  gatewayNamespace: "${gatewayNamespace}"\n  claims: {\n    iss:            "https://login.example.com"\n    sub:            "user-abc123"\n    iat:            1743868800\n    exp:            1743872400\n    email:          "developer@example.com"\n    email_verified: true\n  }\n}`
  const defaultProjectInput = `input: {\n  name:  "example"\n  image: "nginx"\n  tag:   "latest"\n  port:  8080\n}`

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync({
        displayName: template?.displayName,
        description: template?.description,
        cueTemplate,
        enabled: template?.enabled,
      })
      toast.success('Saved')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  const handleToggleEnabled = async (checked: boolean) => {
    try {
      await updateMutation.mutateAsync({
        displayName: template?.displayName,
        description: template?.description,
        cueTemplate: template?.cueTemplate,
        enabled: checked,
      })
      toast.success(checked ? 'Template enabled' : 'Template disabled')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  const handleDeleteConfirm = async () => {
    try {
      await deleteMutation.mutateAsync({ name: templateName })
      setDeleteOpen(false)
      if (navigate) {
        navigate({ to: '/folders/$folderName/templates', params: { folderName } })
      }
    } catch {
      /* error shown via mutation */
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
      if (navigate) {
        navigate({
          to: '/folders/$folderName/templates/$templateName',
          params: { folderName, templateName: response.name },
        })
      }
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
              {' / '}
              <Link
                to="/folders/$folderName/templates"
                params={{ folderName }}
                className="hover:underline"
              >
                Platform Templates
              </Link>
              {' / '}
              {templateName}
            </p>
            <div className="flex items-center gap-2 mt-1">
              <h2 className="text-xl font-semibold">
                {template?.displayName || templateName}
              </h2>
              {/* Mandatory badge removed in HOL-555; TemplatePolicy REQUIRE
                  rules (HOL-558) will re-introduce an "always applied"
                  affordance. */}
            </div>
          </div>

          <div className="space-y-4">
            <h3 className="text-sm font-medium">General</h3>
            <Separator />

            <div className="flex items-center gap-2">
              <span className="w-36 text-sm text-muted-foreground shrink-0">Name</span>
              <span className="text-sm font-mono">{templateName}</span>
            </div>

            {template?.description && (
              <div className="flex items-start gap-2">
                <span className="w-36 text-sm text-muted-foreground shrink-0 pt-0.5">
                  Description
                </span>
                <span className="text-sm">{template.description}</span>
              </div>
            )}

            <PlatformTemplateEnabledToggle
              enabled={template?.enabled ?? false}
              canWrite={canWrite}
              isUpdating={updateMutation.isPending}
              onChange={handleToggleEnabled}
            />
          </div>

          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium">CUE Template</h3>
              {canWrite && (
                <Button variant="outline" size="sm" onClick={handleOpenClone}>
                  <Copy className="h-3.5 w-3.5 mr-1.5" />
                  Clone
                </Button>
              )}
            </div>
            <Separator />
            {!canWrite && (
              <Alert>
                <AlertDescription>
                  You need folder Owner permissions to edit platform templates.
                </AlertDescription>
              </Alert>
            )}
            <CueTemplateEditor
              cueTemplate={cueTemplate}
              onChange={setCueTemplate}
              readOnly={!canWrite}
              onSave={canWrite ? handleSave : undefined}
              isSaving={updateMutation.isPending}
              defaultPlatformInput={defaultPlatformInput}
              defaultProjectInput={defaultProjectInput}
              scope={scope}
            />
          </div>

          <TemplateReleases
            scope={scope}
            templateName={templateName}
            canWrite={canWrite}
            currentCueTemplate={cueTemplate}
            currentDefaults={template?.defaults}
          />

          {canWrite && (
            <div className="space-y-4">
              <h3 className="text-sm font-medium text-destructive">Danger Zone</h3>
              <Separator />
              <Button
                variant="destructive"
                onClick={() => {
                  deleteMutation.reset()
                  setDeleteOpen(true)
                }}
              >
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
              This will permanently delete template &quot;{templateName}&quot;. This action cannot
              be undone.
            </DialogDescription>
          </DialogHeader>
          {deleteMutation.error && (
            <Alert variant="destructive">
              <AlertDescription>{deleteMutation.error.message}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleDeleteConfirm}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={cloneOpen} onOpenChange={setCloneOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Clone Platform Template</DialogTitle>
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
            <Button variant="ghost" onClick={() => setCloneOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleCloneConfirm}
              disabled={cloneMutation.isPending || !cloneName}
            >
              {cloneMutation.isPending ? 'Cloning...' : 'Clone'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
