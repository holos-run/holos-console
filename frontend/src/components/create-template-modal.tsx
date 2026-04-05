import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useCreateDeploymentTemplate, useRenderDeploymentTemplate } from '@/queries/deployment-templates'

const DEFAULT_CUE_TEMPLATE = `// package deployment is the required CUE package declaration.
package deployment

// #KeyRef identifies a key within a Kubernetes Secret or ConfigMap.
#KeyRef: {
  name: string
  key:  string
}

// #EnvVar represents a container environment variable.
#EnvVar: {
  name:               string
  value?:             string
  secretKeyRef?:      #KeyRef
  configMapKeyRef?:   #KeyRef
}

// #Input defines the fields the console fills in at render time.
#Input: {
  name:      string & =~"^[a-z][a-z0-9-]*$"
  image:     string
  tag:       string
  project:   string
  namespace: string
  command?: [...string]
  args?: [...string]
  env: [...#EnvVar] | *[]
}

input: #Input

// _labels are the standard labels required on every resource.
_labels: {
  "app.kubernetes.io/name":       input.name
  "app.kubernetes.io/managed-by": "console.holos.run"
}

// #Namespaced constrains namespaced resource struct keys to match resource metadata.
#Namespaced: [Namespace=string]: [Kind=string]: [Name=string]: {
  kind: Kind
  metadata: {
    name:      Name
    namespace: Namespace
    ...
  }
  ...
}

// #Cluster constrains cluster-scoped resource struct keys to match resource metadata.
#Cluster: [Kind=string]: [Name=string]: {
  kind: Kind
  metadata: {
    name: Name
    ...
  }
  ...
}

namespaced: #Namespaced & {
  (input.namespace): {
    Deployment: (input.name): {
      apiVersion: "apps/v1"
      kind:       "Deployment"
      metadata: {
        name:      input.name
        namespace: input.namespace
        labels:    _labels
      }
      spec: {
        replicas: 1
        selector: matchLabels: "app.kubernetes.io/name": input.name
        template: {
          metadata: labels: _labels
          spec: containers: [{
            name:  input.name
            image: input.image + ":" + input.tag
          }]
        }
      }
    }
  }
}

cluster: #Cluster & {}
`

const HTTPBIN_EXAMPLE_NAME = 'go-httpbin'
const HTTPBIN_EXAMPLE_IMAGE = 'ghcr.io/mccutchen/go-httpbin'
const HTTPBIN_EXAMPLE_TAG = '2.21'

export interface CreateTemplateModalProps {
  projectName: string
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated?: (templateName: string) => void
}

export function CreateTemplateModal({ projectName, open, onOpenChange, onCreated }: CreateTemplateModalProps) {
  const createMutation = useCreateDeploymentTemplate(projectName)

  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [cueTemplate, setCueTemplate] = useState(DEFAULT_CUE_TEMPLATE)
  const [error, setError] = useState<string | null>(null)
  const [previewOpen, setPreviewOpen] = useState(false)

  const renderQuery = useRenderDeploymentTemplate(
    projectName,
    cueTemplate,
    HTTPBIN_EXAMPLE_NAME,
    HTTPBIN_EXAMPLE_IMAGE,
    HTTPBIN_EXAMPLE_TAG,
    open && previewOpen,
  )

  const slugify = (val: string) =>
    val.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

  const handleDisplayNameChange = (val: string) => {
    setDisplayName(val)
    setName(slugify(val))
  }

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      setDisplayName('')
      setName('')
      setDescription('')
      setCueTemplate(DEFAULT_CUE_TEMPLATE)
      setError(null)
      setPreviewOpen(false)
      createMutation.reset()
    }
    onOpenChange(nextOpen)
  }

  const handleCreate = async () => {
    if (!name.trim()) {
      setError('Template name is required')
      return
    }
    setError(null)
    try {
      await createMutation.mutateAsync({
        name: name.trim(),
        displayName: displayName.trim(),
        description: description.trim(),
        cueTemplate,
      })
      onCreated?.(name.trim())
      handleOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Create Deployment Template</DialogTitle>
          <DialogDescription>Define a CUE-based deployment template for this project.</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div>
            <Label htmlFor="template-display-name">Display Name</Label>
            <Input
              id="template-display-name"
              aria-label="Display Name"
              autoFocus
              value={displayName}
              onChange={(e) => handleDisplayNameChange(e.target.value)}
              placeholder="My Web App"
            />
          </div>
          <div>
            <Label>Name (slug)</Label>
            <Input
              aria-label="Name slug"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-web-app"
            />
            <p className="text-xs text-muted-foreground mt-1">Auto-derived from display name. Lowercase alphanumeric and hyphens only.</p>
          </div>
          <div>
            <Label htmlFor="template-description">Description</Label>
            <Input
              id="template-description"
              aria-label="Description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What does this template produce?"
            />
          </div>
          <div>
            <Label htmlFor="template-cue-template">CUE Template</Label>
            <Textarea
              id="template-cue-template"
              aria-label="CUE Template"
              value={cueTemplate}
              onChange={(e) => setCueTemplate(e.target.value)}
              rows={10}
              className="font-mono text-sm field-sizing-normal max-h-96 overflow-y-auto"
            />
          </div>
          <div>
            <Button variant="outline" type="button" onClick={() => setPreviewOpen((v) => !v)}>
              {previewOpen ? 'Hide Preview' : 'Preview'}
            </Button>
            {previewOpen && (
              <div className="mt-2">
                {renderQuery.isLoading && (
                  <p className="text-sm text-muted-foreground">Rendering...</p>
                )}
                {renderQuery.isError && renderQuery.error && (
                  <Alert variant="destructive">
                    <AlertDescription>
                      {renderQuery.error instanceof Error ? renderQuery.error.message : String(renderQuery.error)}
                    </AlertDescription>
                  </Alert>
                )}
                {renderQuery.data?.renderedJson && (
                  <pre className="font-mono text-sm bg-muted p-3 rounded-md max-h-96 overflow-y-auto whitespace-pre-wrap break-all">
                    {renderQuery.data.renderedJson}
                  </pre>
                )}
              </div>
            )}
          </div>
          {error && (
            <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => handleOpenChange(false)}>Cancel</Button>
          <Button onClick={handleCreate} disabled={createMutation.isPending}>
            {createMutation.isPending ? 'Creating...' : 'Create'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
