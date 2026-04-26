/**
 * Project-scoped template detail and editor (HOL-974).
 *
 * This page replaces the former redirect shim (HOL-607/612) with a
 * direct detail/edit view for project-owned deployment templates. The
 * query key (keys.templates.get(namespace, name)) is shared with the
 * list page (keys.templates.list(namespace)) via the existing mutation
 * invalidation in useUpdateTemplate and useDeleteTemplate.
 *
 * Preview tab: CueTemplateEditor provides a "Project Input" textarea for
 * deployment parameters. There is no additional Platform Input panel —
 * platform context is injected by the backend via TemplatePolicyBinding
 * rules.
 */

import { useEffect, useState } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { ArrowLeft } from 'lucide-react'
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
import { CueTemplateEditor } from '@/components/cue-template-editor'
import { TemplateExamplePicker } from '@/components/templates/template-example-picker'
import {
  useGetTemplate,
  useUpdateTemplate,
  useDeleteTemplate,
  useGetTemplateDefaults,
} from '@/queries/templates'
import type { TemplateDefaults, TemplateExample } from '@/queries/templates'
import { namespaceForProject } from '@/lib/scope-labels'

// ---------------------------------------------------------------------------
// Utility: TemplateDefaults → CUE project-input snippet
// ---------------------------------------------------------------------------

/**
 * templateDefaultsToCueInput converts a TemplateDefaults message into a CUE
 * input snippet suitable for pre-populating the Project Input textarea in the
 * preview tab. Only non-empty string and non-zero int32 fields are emitted.
 */
function templateDefaultsToCueInput(defaults: TemplateDefaults | undefined): string {
  if (!defaults) return ''

  const lines: string[] = []
  if (defaults.name) lines.push(`    name:        ${JSON.stringify(defaults.name)}`)
  if (defaults.image) lines.push(`    image:       ${JSON.stringify(defaults.image)}`)
  if (defaults.tag) lines.push(`    tag:         ${JSON.stringify(defaults.tag)}`)
  if (defaults.description) lines.push(`    description: ${JSON.stringify(defaults.description)}`)
  if (defaults.port !== 0) lines.push(`    port:        ${defaults.port}`)

  if (lines.length === 0) return ''
  return `input: {\n${lines.join('\n')}\n}`
}

// ---------------------------------------------------------------------------
// Route definition
// ---------------------------------------------------------------------------

export const Route = createFileRoute(
  '/_authenticated/projects/$projectName/templates/$templateName',
)({
  component: ProjectTemplateDetailRoute,
})

function ProjectTemplateDetailRoute() {
  const { projectName, templateName } = Route.useParams()
  return <ProjectTemplateDetailPage projectName={projectName} templateName={templateName} />
}

// ---------------------------------------------------------------------------
// Page component (exported for tests)
// ---------------------------------------------------------------------------

export function ProjectTemplateDetailPage({
  projectName,
  templateName,
}: {
  projectName: string
  templateName: string
}) {
  const navigate = useNavigate()
  const namespace = namespaceForProject(projectName)

  const { data: template, isPending, error } = useGetTemplate(namespace, templateName)
  const { data: templateDefaults } = useGetTemplateDefaults({ namespace, name: templateName })
  const updateMutation = useUpdateTemplate(namespace, templateName)
  const deleteMutation = useDeleteTemplate(namespace)

  const [cueTemplate, setCueTemplate] = useState('')
  const [deleteOpen, setDeleteOpen] = useState(false)

  // Sync editor state when template data loads. This mirrors the same
  // pattern used by the org-scope consolidated editor ($namespace.$name.tsx).
  useEffect(() => {
    if (template?.cueTemplate !== undefined) {
      setCueTemplate(template.cueTemplate) // eslint-disable-line react-hooks/set-state-in-effect
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
      })
      toast.success('Saved')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  const handleDelete = async () => {
    try {
      await deleteMutation.mutateAsync({ name: templateName })
      setDeleteOpen(false)
      toast.success('Template deleted')
      navigate({
        to: '/projects/$projectName/templates',
        params: { projectName },
      })
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

  const displayName = template?.displayName || templateName

  return (
    <Card>
      <CardContent className="pt-6 space-y-6">
        <div className="flex items-start gap-2">
          <div className="flex-1">
            <Link
              to="/projects/$projectName/templates"
              params={{ projectName }}
              className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground mb-2"
            >
              <ArrowLeft className="h-4 w-4" />
              Templates
            </Link>
            <p className="text-sm text-muted-foreground">
              {projectName} / Templates / {templateName}
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
            <span className="text-sm font-mono">{templateName}</span>
          </div>
        </div>

        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium">CUE Template</h3>
            <TemplateExamplePicker onSelect={handleSelectExample} />
          </div>
          <Separator />
          {/* Preview tab: CueTemplateEditor provides Project Input textarea.
              No Platform Input panel — platform context is injected by the
              backend via TemplatePolicyBinding rules. */}
          <CueTemplateEditor
            cueTemplate={cueTemplate}
            onChange={setCueTemplate}
            onSave={handleSave}
            isSaving={updateMutation.isPending}
            defaultProjectInput={templateDefaultsToCueInput(templateDefaults)}
            namespace={namespace}
          />
        </div>
      </CardContent>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Template</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete template &quot;{templateName}&quot;?
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
