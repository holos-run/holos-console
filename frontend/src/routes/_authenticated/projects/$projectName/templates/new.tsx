import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { Info } from 'lucide-react'
import { useCreateTemplate, useRenderTemplate, makeProjectScope } from '@/queries/templates'
import { useDebouncedValue } from '@/hooks/use-debounced-value'

const DEFAULT_CUE_TEMPLATE = `// Use generated type definitions from api/v1alpha2 (prepended by renderer).
// Additional CUE constraints narrow the generated types for this template.
input: #ProjectInput & {
  name: =~"^[a-z][a-z0-9-]*$"
  env:  [...#EnvVar] | *[]
  port: >0 & <=65535 | *8080
}
platform: #PlatformInput

// _labels are the standard labels required on every resource.
_labels: {
  "app.kubernetes.io/name":       input.name
  "app.kubernetes.io/managed-by": "console.holos.run"
}

// _annotations are standard annotations applied to every resource.
_annotations: {
  "console.holos.run/deployer-email": platform.claims.email
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

// projectResources collects all rendered Kubernetes resources.
projectResources: {
  namespacedResources: #Namespaced & {
    (platform.namespace): {
      Deployment: (input.name): {
        apiVersion: "apps/v1"
        kind:       "Deployment"
        metadata: {
          name:        input.name
          namespace:   platform.namespace
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

// EXAMPLE_HTTPBIN_TEMPLATE is the example project-level deployment template CUE content.
// It matches console/templates/example_httpbin.cue.
const EXAMPLE_HTTPBIN_TEMPLATE = `// Project-level deployment template for go-httpbin.
// Produces: ServiceAccount, Deployment, Service.
// Allowed by the org constraint: Deployment, Service, ServiceAccount.
//
// Pair with console/org_templates/example_httpbin_platform.cue to add an
// HTTPRoute that routes gateway traffic to the Service.

// Use generated type definitions from api/v1alpha2 (prepended by renderer).
// Additional CUE constraints narrow the generated types for this template.
input: #ProjectInput & {
	name:  =~"^[a-z][a-z0-9-]*$" // DNS label
	image: string | *"ghcr.io/mccutchen/go-httpbin"
	tag:   string | *"2.21.0"
	port:  >0 & <=65535 | *8080
}
platform: #PlatformInput

// _labels are the standard labels required on every resource.
// app.kubernetes.io/managed-by MUST equal "console.holos.run" or the
// render will be rejected.
_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

// _annotations are standard annotations applied to every resource.
// console.holos.run/deployer-email records the identity of the user
// who last rendered and applied this resource.
_annotations: {
	"console.holos.run/deployer-email": platform.claims.email
}

// #Namespaced constrains namespaced resource struct keys to match resource metadata.
// Structure: namespaced.<namespace>.<Kind>.<name>
// The struct path keys must match the corresponding resource metadata fields.
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
// Structure: cluster.<Kind>.<name>
// The struct path keys must match the corresponding resource metadata fields.
#Cluster: [Kind=string]: [Name=string]: {
	kind: Kind
	metadata: {
		name: Name
		...
	}
	...
}

projectResources: {
	namespacedResources: #Namespaced & {
		(platform.namespace): {
			// ServiceAccount provides a Kubernetes identity for the pods.
			ServiceAccount: (input.name): {
				apiVersion: "v1"
				kind:       "ServiceAccount"
				metadata: {
					name:        input.name
					namespace:   platform.namespace
					labels:      _labels
					annotations: _annotations
				}
			}

			// Deployment runs the go-httpbin container.
			// go-httpbin listens on port 8080 by default and needs no special
			// command or args — the image's default entrypoint works.
			Deployment: (input.name): {
				apiVersion: "apps/v1"
				kind:       "Deployment"
				metadata: {
					name:        input.name
					namespace:   platform.namespace
					labels:      _labels
					annotations: _annotations
				}
				spec: {
					replicas: 1
					selector: matchLabels: "app.kubernetes.io/name": input.name
					template: {
						metadata: labels: _labels
						spec: {
							serviceAccountName: input.name
							containers: [{
								name:  input.name
								image: input.image + ":" + input.tag
								ports: [{containerPort: input.port, name: "http"}]
							}]
						}
					}
				}
			}

			// Service exposes port 80 → container port input.port (named "http").
			// The HTTPRoute in the org platform template routes gateway traffic here.
			Service: (input.name): {
				apiVersion: "v1"
				kind:       "Service"
				metadata: {
					name:        input.name
					namespace:   platform.namespace
					labels:      _labels
					annotations: _annotations
				}
				spec: {
					selector: "app.kubernetes.io/name": input.name
					ports: [{port: 80, targetPort: "http", name: "http"}]
				}
			}
		}
	}

	// clusterResources organizes cluster-scoped resources (none for this template).
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
  const scope = makeProjectScope(projectName)
  const createMutation = useCreateTemplate(scope)

  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [cueTemplate, setCueTemplate] = useState(DEFAULT_CUE_TEMPLATE)
  const [error, setError] = useState<string | null>(null)
  const [previewOpen, setPreviewOpen] = useState(false)

  const previewCuePlatformInput = `platform: {
\tproject:          "${projectName}"
\tnamespace:        "holos-prj-${projectName}"
\tgatewayNamespace: "istio-ingress"
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
  const renderQuery = useRenderTemplate(
    scope,
    debouncedCueTemplate,
    previewCueInput,
    previewOpen,
    previewCuePlatformInput,
  )

  const handleLoadHttpbinExample = () => {
    setCueTemplate(EXAMPLE_HTTPBIN_TEMPLATE)
  }

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
            <div className="flex items-center justify-between mb-1">
              <Label htmlFor="template-cue-template">CUE Template</Label>
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" type="button" onClick={handleLoadHttpbinExample}>
                  Load httpbin Example
                </Button>
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Info className="h-4 w-4 text-muted-foreground cursor-default" />
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>Deploys go-httpbin with a ServiceAccount, Deployment, and Service as an example.</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </div>
            </div>
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
