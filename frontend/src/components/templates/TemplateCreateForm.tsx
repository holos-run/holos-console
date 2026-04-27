import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { Info } from 'lucide-react'
import {
  useRenderTemplate,
} from '@/queries/templates'
import type {
  TemplateExample,
} from '@/queries/templates'
import { useDebouncedValue } from '@/hooks/use-debounced-value'
import { TemplateExamplePicker } from '@/components/templates/template-example-picker'
import { connectErrorMessage } from '@/lib/connect-toast'

export type TemplateScope = 'organization' | 'folder' | 'project'

export type TemplateCreateParams = {
  name: string
  displayName: string
  description: string
  cueTemplate: string
  enabled?: boolean
}

export type TemplateCreateFormProps = {
  scopeType: TemplateScope
  /** Namespace the new template lives in. */
  namespace: string
  /** Organization name. Optional; passed through to callers but not used
   * to construct preview platform input (the backend injects authoritative
   * platform context). */
  organization?: string
  /** Project name. Required for project scope; ignored for org/folder scopes. */
  projectName?: string
  /** Whether the user may fill/submit the form. */
  canWrite: boolean
  isPending?: boolean
  onSubmit: (values: TemplateCreateParams) => Promise<void>
  onCancel: () => void
}

// DEFAULT_CUE_TEMPLATE is the minimal CUE starter shown on an empty project-scope
// create form. It is NOT an example — it is a blank scaffold so users have
// something to start from before selecting a real example via the picker.
const DEFAULT_CUE_TEMPLATE = `// Use generated type definitions from api/v1alpha2 (prepended by renderer).
// Additional CUE constraints narrow the generated types for this template.
input: #ProjectInput & {
  name: =~"^[a-z][a-z0-9-]*$"
  env:  [...#EnvVar] | *[]
  port: >0 & <=65535 | *8080
}
platform: #PlatformInput

// projectResources collects all rendered Kubernetes resources.
projectResources: {
  namespacedResources: {}
  clusterResources: {}
}
`

/**
 * TemplateCreateForm renders the shared create form for Holos templates at
 * any of the three authoring scopes (organization, folder, project).
 *
 * Scope-specific behavior:
 *  - organization: Platform template. Enabled toggle defaults to false.
 *    CUE editor shows an Info tooltip explaining platform templates unify
 *    with project deployment templates at render time via TemplatePolicyBinding.
 *  - folder:       Platform template. Enabled toggle defaults to true
 *    (HOL-789 AC 5). Enabled label carries a tooltip pointing at
 *    TemplatePolicyBinding.
 *  - project:      Deployment template. No Enabled toggle. Adds the
 *    Preview pane, which renders the CUE with project input; platform context
 *    is injected by the backend via TemplatePolicyBinding rules.
 */
export function TemplateCreateForm({
  scopeType,
  namespace,
  canWrite,
  isPending = false,
  onSubmit,
  onCancel,
}: TemplateCreateFormProps) {
  const isProject = scopeType === 'project'
  const isFolder = scopeType === 'folder'
  const isOrg = scopeType === 'organization'

  const submitLabel = isProject ? 'Create Template' : 'Create'
  const pendingLabel = 'Creating...'

  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [cueTemplate, setCueTemplate] = useState(
    isProject ? DEFAULT_CUE_TEMPLATE : '',
  )
  // Enabled defaults by scope: folder ships live (HOL-789); org ships disabled
  // so an author can review before rollout. Project scope has no toggle.
  const [enabled, setEnabled] = useState(isFolder)
  const [error, setError] = useState<string | null>(null)

  // Project-scope-only state.
  const [previewOpen, setPreviewOpen] = useState(false)

  const previewCueInput = `input: {
\tname:  "go-httpbin"
\timage: "ghcr.io/mccutchen/go-httpbin"
\ttag:   "2.21"
\tport:  8080
}`

  const debouncedCueTemplate = useDebouncedValue(cueTemplate, 500)
  const renderQuery = useRenderTemplate(
    isProject ? namespace : '',
    debouncedCueTemplate,
    previewCueInput,
    isProject && previewOpen,
  )

  const slugify = (val: string) =>
    val
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')

  const handleDisplayNameChange = (val: string) => {
    setDisplayName(val)
    setName(slugify(val))
  }

  const handleSelectExample = (example: TemplateExample) => {
    setDisplayName(example.displayName)
    setName(example.name)
    setDescription(example.description)
    setCueTemplate(example.cueTemplate)
  }

  const handleCreate = async () => {
    if (!name.trim()) {
      setError('Template name is required')
      return
    }
    setError(null)
    try {
      await onSubmit({
        name: name.trim(),
        displayName: displayName.trim(),
        description: description.trim(),
        cueTemplate,
        enabled: isProject ? undefined : enabled,
      })
    } catch (err) {
      setError(connectErrorMessage(err))
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="template-display-name">Display Name</Label>
        <Input
          id="template-display-name"
          aria-label="Display Name"
          autoFocus
          value={displayName}
          onChange={(e) => handleDisplayNameChange(e.target.value)}
          placeholder={isProject ? 'My Web App' : 'My Template'}
          disabled={!canWrite}
        />
      </div>
      <div>
        <Label>Name (slug)</Label>
        <Input
          aria-label="Name slug"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={isProject ? 'my-web-app' : 'my-template'}
          disabled={!canWrite}
        />
        <p className="text-xs text-muted-foreground mt-1">
          Auto-derived from display name. Lowercase alphanumeric and hyphens
          only.
        </p>
      </div>
      <div>
        <Label htmlFor="template-description">Description</Label>
        <Input
          id="template-description"
          aria-label="Description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="What does this template produce?"
          disabled={!canWrite}
        />
      </div>
      <div>
        <div className="flex items-center justify-between mb-1">
          <Label htmlFor="template-cue-template">CUE Template</Label>
          <div className="flex items-center gap-2">
            <TemplateExamplePicker
              onSelect={handleSelectExample}
              disabled={!canWrite}
            />
            {isOrg && (
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Info className="h-4 w-4 text-muted-foreground cursor-default" />
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>
                      Platform templates are unified with project deployment
                      templates at render time via CUE. This example constrains
                      project resources to safe kinds and provides an HTTPRoute
                      for external access.
                    </p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            )}
          </div>
        </div>
        <Textarea
          id="template-cue-template"
          aria-label="CUE Template"
          value={cueTemplate}
          onChange={(e) => setCueTemplate(e.target.value)}
          rows={20}
          className="font-mono text-sm field-sizing-normal max-h-[600px] overflow-y-auto"
          disabled={!canWrite}
        />
      </div>

      {!isProject && (
        <div className="flex items-center gap-3">
          <Switch
            id="template-enabled"
            aria-label="Enabled"
            checked={enabled}
            onCheckedChange={setEnabled}
            disabled={!canWrite}
          />
          <Label htmlFor="template-enabled" className="text-sm cursor-pointer">
            {isOrg ? 'Enabled (apply to projects in this organization)' : 'Enabled'}
          </Label>
          {isFolder && (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Info
                    aria-label="Enabled tooltip"
                    className="h-4 w-4 text-muted-foreground cursor-default"
                  />
                </TooltipTrigger>
                <TooltipContent>
                  <p>
                    Unified with resources bound to this Template by Policy
                    when enabled. See TemplatePolicyBinding.
                  </p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
        </div>
      )}

      {isProject && (
        <div>
          <Button
            variant="outline"
            type="button"
            onClick={() => setPreviewOpen((v) => !v)}
          >
            {previewOpen ? 'Hide Preview' : 'Preview'}
          </Button>
          {previewOpen && (
            <div className="mt-2 min-w-0">
              {renderQuery.isLoading && (
                <p className="text-sm text-muted-foreground">Rendering...</p>
              )}
              {renderQuery.isError && renderQuery.error && (
                <Alert variant="destructive">
                  <AlertDescription>
                    {renderQuery.error instanceof Error
                      ? renderQuery.error.message
                      : String(renderQuery.error)}
                  </AlertDescription>
                </Alert>
              )}
              {renderQuery.data &&
                (() => {
                  const platformJson =
                    renderQuery.data.platformResourcesJson ?? ''
                  const projectJson =
                    renderQuery.data.projectResourcesJson ?? ''
                  const hasPerCollection = !!(platformJson || projectJson)
                  if (hasPerCollection) {
                    return (
                      <div className="space-y-3 min-w-0">
                        <Label>Platform Resources</Label>
                        {platformJson ? (
                          <pre
                            aria-label="Platform Resources JSON"
                            className="font-mono text-sm bg-muted rounded-md p-4 overflow-auto whitespace-pre max-w-full"
                          >
                            {platformJson}
                          </pre>
                        ) : (
                          <p className="text-sm text-muted-foreground">
                            No platform resources rendered by this template.
                          </p>
                        )}
                        <Label>Project Resources</Label>
                        <pre
                          aria-label="Project Resources JSON"
                          className="font-mono text-sm bg-muted rounded-md p-4 overflow-auto whitespace-pre max-w-full"
                        >
                          {projectJson}
                        </pre>
                      </div>
                    )
                  }
                  if (renderQuery.data.renderedJson) {
                    return (
                      <pre className="font-mono text-sm bg-muted rounded-md p-4 overflow-auto whitespace-pre max-w-full">
                        {renderQuery.data.renderedJson}
                      </pre>
                    )
                  }
                  return null
                })()}
            </div>
          )}
        </div>
      )}

      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <div className="flex items-center gap-3 pt-2">
        <Button onClick={handleCreate} disabled={isPending || !canWrite}>
          {isPending ? pendingLabel : submitLabel}
        </Button>
        <Button
          variant="ghost"
          type="button"
          aria-label="Cancel"
          onClick={onCancel}
        >
          Cancel
        </Button>
      </div>
    </div>
  )
}
