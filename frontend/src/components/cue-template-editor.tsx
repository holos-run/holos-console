import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useDebouncedValue } from '@/hooks/use-debounced-value'

interface RenderStatusIndicatorProps {
  isStale: boolean
  isRendering: boolean
  hasError: boolean
}

export function RenderStatusIndicator({ isStale, isRendering, hasError }: RenderStatusIndicatorProps) {
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

export type RenderFn = (cueTemplate: string, cueInput: string, enabled: boolean, cuePlatformInput: string) => {
  data?: { renderedYaml: string; renderedJson: string }
  error?: Error | null
  isFetching: boolean
}

export interface CueTemplateEditorProps {
  /** Current CUE template source */
  cueTemplate: string
  /** Called when the CUE template is edited */
  onChange: (value: string) => void
  /** When true, the textarea is read-only and the Save button is hidden */
  readOnly?: boolean
  /** Called when the Save button is clicked */
  onSave?: () => Promise<void>
  /** Whether a save operation is in progress */
  isSaving?: boolean
  /** Default platform input for the preview tab */
  defaultPlatformInput?: string
  /** Default user input for the preview tab */
  defaultUserInput?: string
  /** Hook to use for rendering (injectable for testability) */
  useRenderFn: RenderFn
}

/**
 * CueTemplateEditor is a shared component that renders a tabbed CUE template
 * editor + live preview. It is used by both the deployment template detail page
 * and the platform template detail page.
 */
export function CueTemplateEditor({
  cueTemplate,
  onChange,
  readOnly = false,
  onSave,
  isSaving = false,
  defaultPlatformInput = '',
  defaultUserInput = '',
  useRenderFn,
}: CueTemplateEditorProps) {
  const [activeTab, setActiveTab] = useState('editor')
  const [cuePlatformInput, setCuePlatformInput] = useState(defaultPlatformInput)
  const [cueInput, setCueInput] = useState(defaultUserInput)

  const debouncedCueInput = useDebouncedValue(cueInput, 500)
  const debouncedCuePlatformInput = useDebouncedValue(cuePlatformInput, 500)
  const debouncedCueTemplate = useDebouncedValue(cueTemplate, 500)

  const isStale =
    cueInput !== debouncedCueInput ||
    cuePlatformInput !== debouncedCuePlatformInput ||
    cueTemplate !== debouncedCueTemplate

  const { data: renderData, error: renderError, isFetching: isRendering } = useRenderFn(
    debouncedCueTemplate,
    debouncedCueInput,
    activeTab === 'preview',
    debouncedCuePlatformInput,
  )

  const renderedYaml = renderData?.renderedYaml

  return (
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
            onChange={(e) => onChange(e.target.value)}
            rows={20}
            className="font-mono text-sm field-sizing-normal max-h-96 overflow-y-auto"
            readOnly={readOnly}
          />
        </div>
        {!readOnly && onSave && (
          <div className="flex justify-end">
            <Button onClick={onSave} disabled={isSaving}>
              {isSaving ? 'Saving...' : 'Save'}
            </Button>
          </div>
        )}
      </TabsContent>
      <TabsContent value="preview" className="mt-4 space-y-4">
        <div className="space-y-2">
          <Label htmlFor="cue-platform-input-editor">Platform Input</Label>
          <p className="text-xs text-muted-foreground">
            These values are set by the console at deployment time and include the authenticated user&apos;s OIDC claims.
          </p>
          <Textarea
            id="cue-platform-input-editor"
            aria-label="Platform Input"
            value={cuePlatformInput}
            onChange={(e) => setCuePlatformInput(e.target.value)}
            rows={10}
            className="font-mono text-sm field-sizing-normal max-h-64 overflow-y-auto"
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="cue-input-editor">User Input (deployment parameters)</Label>
          <Textarea
            id="cue-input-editor"
            aria-label="User Input"
            value={cueInput}
            onChange={(e) => setCueInput(e.target.value)}
            rows={6}
            className="font-mono text-sm field-sizing-normal max-h-48 overflow-y-auto"
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
  )
}
