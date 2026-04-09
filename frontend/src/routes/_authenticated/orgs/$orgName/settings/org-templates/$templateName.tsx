import { useState, useEffect } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Lock, Copy } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useGetOrgTemplate, useUpdateOrgTemplate, useCloneOrgTemplate, useRenderOrgTemplate } from '@/queries/org-templates'
import { useGetOrganization } from '@/queries/organizations'
import { CueTemplateEditor } from '@/components/cue-template-editor'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/settings/org-templates/$templateName')({
  component: OrgTemplateDetailRoute,
})

function OrgTemplateDetailRoute() {
  const { orgName, templateName } = Route.useParams()
  return <OrgTemplateDetailPage orgName={orgName} templateName={templateName} />
}

export function OrgTemplateDetailPage({ orgName: propOrgName, templateName: propTemplateName }: { orgName?: string; templateName?: string } = {}) {
  let routeParams: { orgName?: string; templateName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const orgName = propOrgName ?? routeParams.orgName ?? ''
  const templateName = propTemplateName ?? routeParams.templateName ?? ''

  let navigate: ReturnType<typeof useNavigate> | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    navigate = useNavigate()
  } catch {
    navigate = undefined
  }

  const { data: template, isPending, error } = useGetOrgTemplate(orgName, templateName)
  const { data: org } = useGetOrganization(orgName)
  const updateMutation = useUpdateOrgTemplate(orgName, templateName)
  const cloneMutation = useCloneOrgTemplate(orgName)

  const [cueTemplate, setCueTemplate] = useState('')
  const [cloneOpen, setCloneOpen] = useState(false)
  const [cloneName, setCloneName] = useState('')
  const [cloneDisplayName, setCloneDisplayName] = useState('')
  const [cloneError, setCloneError] = useState<string | null>(null)

  useEffect(() => {
    if (template?.cueTemplate !== undefined) {
      setCueTemplate(template.cueTemplate)
    }
  }, [template?.cueTemplate])

  // Only org-level OWNERs can edit platform templates (backend enforces PERMISSION_SYSTEM_DEPLOYMENTS_EDIT).
  // Frontend mirrors this: show Save only for OWNER.
  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  const defaultPlatformInput = `platform: {\n  project:          "example-project"\n  namespace:        "prj-example-project"\n  gatewayNamespace: "istio-ingress"\n  claims: {\n    iss:            "https://login.example.com"\n    sub:            "user-abc123"\n    iat:            1743868800\n    exp:            1743872400\n    email:          "developer@example.com"\n    email_verified: true\n  }\n}`
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

  const handleToggleEnabled = async (checked: boolean) => {
    try {
      await updateMutation.mutateAsync({ enabled: checked })
      toast.success(checked ? 'Template enabled' : 'Template disabled')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
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
          to: '/orgs/$orgName/settings/org-templates/$templateName',
          params: { orgName, templateName: response.name },
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
            <p className="text-sm text-muted-foreground">{orgName} / Settings / System Templates / {templateName}</p>
            <div className="flex items-center gap-2 mt-1">
              <h2 className="text-xl font-semibold">{template?.displayName || templateName}</h2>
              {template?.mandatory && (
                <Badge variant="secondary" className="flex items-center gap-1">
                  <Lock className="h-3 w-3" />
                  Mandatory
                </Badge>
              )}
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
                <span className="w-36 text-sm text-muted-foreground shrink-0 pt-0.5">Description</span>
                <span className="text-sm">{template.description}</span>
              </div>
            )}

            <div className="flex items-center gap-2">
              <span className="w-36 text-sm text-muted-foreground shrink-0">Enabled</span>
              <Switch
                aria-label="Enabled"
                checked={template?.enabled ?? false}
                onCheckedChange={handleToggleEnabled}
                disabled={!canWrite || updateMutation.isPending}
              />
              <span className="text-sm text-muted-foreground">
                {template?.enabled ? 'Active — applied to new project namespaces' : 'Inactive — not applied to new project namespaces'}
              </span>
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
            {!canWrite && (
              <Alert>
                <AlertDescription>
                  You need org Owner permissions to edit platform templates.
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
              defaultUserInput={defaultUserInput}
              useRenderFn={useRenderOrgTemplate}
            />
          </div>
        </CardContent>
      </Card>

      <Dialog open={cloneOpen} onOpenChange={setCloneOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Clone System Template</DialogTitle>
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
    </>
  )
}
