import { useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { useGetTemplate, useUpdateTemplate } from '@/queries/templates'
import { CueTemplateEditor } from '@/components/cue-template-editor'

export const Route = createFileRoute(
  '/_authenticated/orgs/$orgName/templates/$namespace/$name',
)({
  component: ConsolidatedTemplateEditorRoute,
})

function ConsolidatedTemplateEditorRoute() {
  const { orgName, namespace, name } = Route.useParams()
  return (
    <ConsolidatedTemplateEditorPage
      orgName={orgName}
      namespace={namespace}
      name={name}
    />
  )
}

// The consolidated editor intentionally references only the primitive identity
// of a template (namespace, name, display_name) plus the CUE body. Scope
// discrimination lives on the backend — HOL-607 AC requires that this page
// render identically regardless of whether the template originated at org,
// folder, or project scope.
export function ConsolidatedTemplateEditorPage({
  orgName: propOrgName,
  namespace: propNamespace,
  name: propName,
}: { orgName?: string; namespace?: string; name?: string } = {}) {
  let routeParams: { orgName?: string; namespace?: string; name?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const orgName = propOrgName ?? routeParams.orgName ?? ''
  const namespace = propNamespace ?? routeParams.namespace ?? ''
  const name = propName ?? routeParams.name ?? ''

  const { data: template, isPending, error } = useGetTemplate(namespace, name)
  const updateMutation = useUpdateTemplate(namespace, name)

  const [cueTemplate, setCueTemplate] = useState('')

  useEffect(() => {
    if (template?.cueTemplate !== undefined) {
      setCueTemplate(template.cueTemplate)
    }
  }, [template?.cueTemplate])

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync({
        displayName: template?.displayName,
        description: template?.description,
        cueTemplate,
        enabled: template?.enabled ?? false,
        linkedTemplates: template?.linkedTemplates ?? [],
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

  const displayName = template?.displayName || name
  // Platform/project input defaults are plain placeholders; the backend
  // injects org-owned values (e.g. gatewayNamespace — HOL-526) at render
  // time regardless of what the preview pane ships up.
  const defaultPlatformInput = `platform: {\n  project:          "example-project"\n  namespace:        "prj-example-project"\n  claims: {\n    iss:            "https://login.example.com"\n    sub:            "user-abc123"\n    iat:            1743868800\n    exp:            1743872400\n    email:          "developer@example.com"\n    email_verified: true\n  }\n}`
  const defaultProjectInput = `input: {\n  name:  "example"\n  image: "nginx"\n  tag:   "latest"\n  port:  8080\n}`

  return (
    <Card>
      <CardContent className="pt-6 space-y-6">
        <div>
          <p className="text-sm text-muted-foreground">
            {orgName} / Templates / {namespace} / {name}
          </p>
          <div className="flex items-center gap-2 mt-1">
            <h2 className="text-xl font-semibold">{displayName}</h2>
          </div>
        </div>

        <div className="space-y-4">
          <h3 className="text-sm font-medium">General</h3>
          <Separator />

          <div className="flex items-center gap-2">
            <span className="w-36 text-sm text-muted-foreground shrink-0">Namespace</span>
            <span className="text-sm font-mono">{namespace}</span>
          </div>

          <div className="flex items-center gap-2">
            <span className="w-36 text-sm text-muted-foreground shrink-0">Name</span>
            <span className="text-sm font-mono">{name}</span>
          </div>
        </div>

        <div className="space-y-4">
          <h3 className="text-sm font-medium">CUE Template</h3>
          <Separator />
          <CueTemplateEditor
            cueTemplate={cueTemplate}
            onChange={setCueTemplate}
            onSave={handleSave}
            isSaving={updateMutation.isPending}
            defaultPlatformInput={defaultPlatformInput}
            defaultProjectInput={defaultProjectInput}
            namespace={namespace}
          />
        </div>
      </CardContent>
    </Card>
  )
}
