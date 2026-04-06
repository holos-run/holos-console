import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useCreateDeploymentTemplate, useRenderDeploymentTemplate } from '@/queries/deployment-templates'
import { useDebouncedValue } from '@/hooks/use-debounced-value'

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

// #Input defines the user-provided fields the console fills in at render time.
#Input: {
  name:    string & =~"^[a-z][a-z0-9-]*$"
  image:   string
  tag:     string
  command?: [...string]
  args?: [...string]
  env:  [...#EnvVar] | *[]
  port: int & >0 & <=65535 | *8080
}

// #Claims carries the OIDC ID token claims of the authenticated user.
#Claims: {
  iss:            string
  sub:            string
  exp:            int
  iat:            int
  email:          string
  email_verified: bool
  name?:          string
  groups?: [...string]
  ... // allow provider-specific claims
}

// #System defines the trusted system-provided fields set by the console backend.
#System: {
  project:   string
  namespace: string
  claims:    #Claims
}

input:  #Input
system: #System

// _labels are the standard labels required on every resource.
_labels: {
  "app.kubernetes.io/name":       input.name
  "app.kubernetes.io/managed-by": "console.holos.run"
}

// _annotations are standard annotations applied to every resource.
_annotations: {
  "console.holos.run/deployer-email": system.claims.email
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

// output collects all rendered Kubernetes resources.
output: {
  namespacedResources: #Namespaced & {
    (system.namespace): {
      Deployment: (input.name): {
        apiVersion: "apps/v1"
        kind:       "Deployment"
        metadata: {
          name:        input.name
          namespace:   system.namespace
          labels:      _labels
          annotations: _annotations
        }
        spec: {
          replicas: 1
          selector: matchLabels: "app.kubernetes.io/name": input.name
          template: {
            metadata: labels: _labels
            spec: containers: [{
              name:  input.name
              image: input.image + ":" + input.tag
              ports: [{containerPort: input.port, name: "http"}]
            }]
          }
        }
      }
    }
  }
  clusterResources: #Cluster & {}
}
`

export const Route = createFileRoute('/_authenticated/projects/$projectName/templates/new')({
  component: CreateTemplateRoute,
})

function CreateTemplateRoute() {
  const { projectName } = Route.useParams()
  return <CreateTemplatePage projectName={projectName} />
}

export function CreateTemplatePage({ projectName: propProjectName }: { projectName?: string } = {}) {
  let routeProjectName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeProjectName = Route.useParams().projectName
  } catch {
    routeProjectName = undefined
  }
  const projectName = propProjectName ?? routeProjectName ?? ''

  const navigate = useNavigate()
  const createMutation = useCreateDeploymentTemplate(projectName)

  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [cueTemplate, setCueTemplate] = useState(DEFAULT_CUE_TEMPLATE)
  const [error, setError] = useState<string | null>(null)
  const [previewOpen, setPreviewOpen] = useState(false)

  const previewCueSystemInput = `system: {
\tproject:   "${projectName}"
\tnamespace: "holos-prj-${projectName}"
\tclaims: {
\t\tiss:            "https://login.example.com"
\t\tsub:            "user-abc123"
\t\tiat:            1743868800
\t\texp:            1743872400
\t\temail:          "developer@example.com"
\t\temail_verified: true
\t}
}`

  const previewCueInput = `input: {
\tname:  "go-httpbin"
\timage: "ghcr.io/mccutchen/go-httpbin"
\ttag:   "2.21"
\tport:  8080
}`

  const debouncedCueTemplate = useDebouncedValue(cueTemplate, 500)
  const renderQuery = useRenderDeploymentTemplate(
    debouncedCueTemplate,
    previewCueInput,
    previewOpen,
    previewCueSystemInput,
  )

  const slugify = (val: string) =>
    val.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

  const handleDisplayNameChange = (val: string) => {
    setDisplayName(val)
    setName(slugify(val))
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
      await navigate({
        to: '/projects/$projectName/templates/$templateName',
        params: { projectName, templateName: name.trim() },
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Create Deployment Template</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
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
            <p className="text-xs text-muted-foreground mt-1">
              Auto-derived from display name. Lowercase alphanumeric and hyphens only.
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
            />
          </div>
          <div>
            <Label htmlFor="template-cue-template">CUE Template</Label>
            <Textarea
              id="template-cue-template"
              aria-label="CUE Template"
              value={cueTemplate}
              onChange={(e) => setCueTemplate(e.target.value)}
              rows={20}
              className="font-mono text-sm field-sizing-normal max-h-[600px] overflow-y-auto"
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
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          <div className="flex items-center gap-3 pt-2">
            <Button onClick={handleCreate} disabled={createMutation.isPending}>
              {createMutation.isPending ? 'Creating...' : 'Create Template'}
            </Button>
            <Link
              to="/projects/$projectName/templates"
              params={{ projectName }}
            >
              <Button variant="ghost" type="button" aria-label="Cancel">
                Cancel
              </Button>
            </Link>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
