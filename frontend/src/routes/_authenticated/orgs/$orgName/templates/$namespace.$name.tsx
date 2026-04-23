import { useEffect, useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  useGetTemplate,
  useUpdateTemplate,
  useDeleteTemplate,
} from '@/queries/templates'
import type { TemplateExample } from '@/queries/templates'
import { CueTemplateEditor } from '@/components/cue-template-editor'
import { TemplateExamplePicker } from '@/components/templates/template-example-picker'

// DEFAULT_PLATFORM_INPUT seeds the preview pane with cosmetic placeholder
// values for the user-editable fields of #PlatformInput (project, namespace,
// claims). The backend always injects the authoritative platform-owned fields
// before evaluating the user-supplied input, so values like gatewayNamespace
// (HOL-526) and organization are resolved server-side and must not be set here.
// CUE unification merges the backend values with these seeds: fields present in
// both must agree (identical strings unify; conflicting strings surface a CUE
// error, which is the correct behavior for pinned vs. resolved mismatches).
const DEFAULT_PLATFORM_INPUT = `platform: {
  project:          "example-project"
  namespace:        "prj-example-project"
  claims: {
    iss:            "https://login.example.com"
    sub:            "user-abc123"
    iat:            1743868800
    exp:            1743872400
    email:          "developer@example.com"
    email_verified: true
  }
}`

const DEFAULT_PROJECT_INPUT = `input: {
  name:  "example"
  image: "nginx"
  tag:   "latest"
  port:  8080
}`

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

  const navigate = useNavigate()
  const { data: template, isPending, error } = useGetTemplate(namespace, name)
  const updateMutation = useUpdateTemplate(namespace, name)
  const deleteMutation = useDeleteTemplate(namespace)

  const [cueTemplate, setCueTemplate] = useState('')
  const [deleteOpen, setDeleteOpen] = useState(false)

  useEffect(() => {
    if (template?.cueTemplate !== undefined) {
      setCueTemplate(template.cueTemplate)
    }
  }, [template?.cueTemplate])

  const handleSelectExample = (example: TemplateExample) => {
    if (
      cueTemplate.trim().length > 0 &&
      !window.confirm(
        'Replace the current CUE template with the selected example? This cannot be undone until you save.',
      )
    ) {
      return
    }
    setCueTemplate(example.cueTemplate)
  }

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

  // Post-success: close the dialog, toast, and navigate to the canonical
  // org-scope templates index (see HOL-804 AC — do NOT navigate to a
  // scope-specific list). On error, deleteMutation.error surfaces inline
  // inside the dialog; we do not double-report via toast.
  const handleDelete = async () => {
    try {
      await deleteMutation.mutateAsync({ name })
      setDeleteOpen(false)
      toast.success('Template deleted')
      navigate({ to: '/orgs/$orgName/templates', params: { orgName } })
    } catch {
      /* error surfaced via deleteMutation.error inside the dialog */
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

  return (
    <Card>
      <CardContent className="pt-6 space-y-6">
        <div className="flex items-start gap-2">
          <div className="flex-1">
            <p className="text-sm text-muted-foreground">
              {orgName} / Templates / {namespace} / {name}
            </p>
            <div className="flex items-center gap-2 mt-1">
              <h2 className="text-xl font-semibold">{displayName}</h2>
            </div>
          </div>
          <Button
            variant="destructive"
            size="sm"
            onClick={() => setDeleteOpen(true)}
          >
            Delete
          </Button>
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
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium">CUE Template</h3>
            <TemplateExamplePicker onSelect={handleSelectExample} />
          </div>
          <Separator />
          <CueTemplateEditor
            cueTemplate={cueTemplate}
            onChange={setCueTemplate}
            onSave={handleSave}
            isSaving={updateMutation.isPending}
            defaultProjectInput={DEFAULT_PROJECT_INPUT}
            linkedTemplates={template?.linkedTemplates ?? []}
            namespace={namespace}
          />
        </div>
      </CardContent>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Template</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete template &quot;{namespace}/{name}&quot;?
              This action cannot be undone.
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
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  )
}
