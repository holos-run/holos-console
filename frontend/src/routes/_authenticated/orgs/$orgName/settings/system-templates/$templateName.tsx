import { useState, useEffect } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Lock } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useGetSystemTemplate, useUpdateSystemTemplate, useRenderSystemTemplate } from '@/queries/system-templates'
import { useGetOrganization } from '@/queries/organizations'
import { CueTemplateEditor } from '@/components/cue-template-editor'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/settings/system-templates/$templateName')({
  component: SystemTemplateDetailRoute,
})

function SystemTemplateDetailRoute() {
  const { orgName, templateName } = Route.useParams()
  return <SystemTemplateDetailPage orgName={orgName} templateName={templateName} />
}

export function SystemTemplateDetailPage({ orgName: propOrgName, templateName: propTemplateName }: { orgName?: string; templateName?: string } = {}) {
  let routeParams: { orgName?: string; templateName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const orgName = propOrgName ?? routeParams.orgName ?? ''
  const templateName = propTemplateName ?? routeParams.templateName ?? ''

  const { data: template, isPending, error } = useGetSystemTemplate(orgName, templateName)
  const { data: org } = useGetOrganization(orgName)
  const updateMutation = useUpdateSystemTemplate(orgName, templateName)

  const [cueTemplate, setCueTemplate] = useState('')

  useEffect(() => {
    if (template?.cueTemplate !== undefined) {
      setCueTemplate(template.cueTemplate)
    }
  }, [template?.cueTemplate])

  // Only org-level OWNERs can edit system templates (backend enforces PERMISSION_SYSTEM_DEPLOYMENTS_EDIT).
  // Frontend mirrors this: show Save only for OWNER.
  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  const defaultSystemInput = `system: {\n  project:   "example-project"\n  namespace: "prj-example-project"\n  claims: {\n    iss:            "https://login.example.com"\n    sub:            "user-abc123"\n    iat:            1743868800\n    exp:            1743872400\n    email:          "developer@example.com"\n    email_verified: true\n  }\n}`
  const gatewayNamespace = template?.gatewayNamespace ?? 'istio-ingress'
  const defaultUserInput = `input: gatewayNamespace: "${gatewayNamespace}"`

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

          <div className="flex items-center gap-2">
            <span className="w-36 text-sm text-muted-foreground shrink-0">Gateway Namespace</span>
            <span className="text-sm font-mono">{gatewayNamespace}</span>
          </div>

          {template?.description && (
            <div className="flex items-start gap-2">
              <span className="w-36 text-sm text-muted-foreground shrink-0 pt-0.5">Description</span>
              <span className="text-sm">{template.description}</span>
            </div>
          )}
        </div>

        <div className="space-y-4">
          <h3 className="text-sm font-medium">CUE Template</h3>
          <Separator />
          {!canWrite && (
            <Alert>
              <AlertDescription>
                You need org Owner permissions to edit system templates.
              </AlertDescription>
            </Alert>
          )}
          <CueTemplateEditor
            cueTemplate={cueTemplate}
            onChange={setCueTemplate}
            readOnly={!canWrite}
            onSave={canWrite ? handleSave : undefined}
            isSaving={updateMutation.isPending}
            defaultSystemInput={defaultSystemInput}
            defaultUserInput={defaultUserInput}
            useRenderFn={useRenderSystemTemplate}
          />
        </div>
      </CardContent>
    </Card>
  )
}
