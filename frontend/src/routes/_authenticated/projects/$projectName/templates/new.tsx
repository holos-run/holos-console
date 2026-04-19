import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { Info } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useCreateTemplate, useRenderTemplate, useListLinkableTemplates, makeProjectScope, linkableKey, parseLinkableKey } from '@/queries/templates'
import { TemplateScope, namespaceFor, scopeFromNamespace, scopeNameFromNamespace } from '@/lib/scope-shim'
import type { LinkedTemplateRef } from '@/queries/templates'
import { create } from '@bufbuild/protobuf'
import { LinkedTemplateRefSchema } from '@/gen/holos/console/v1/policy_state_pb.js'
import { useGetProject } from '@/queries/projects'
import { useGetOrganization } from '@/queries/organizations'
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
  const { data: project } = useGetProject(projectName)
  // The authoring org's gatewayNamespace (HOL-526) is mirrored into the
  // platform-input preview default so the preview matches what the backend
  // will inject at render time.
  const { data: org, isPending: orgPending, error: orgError } = useGetOrganization(project?.organization ?? '')
  const { data: linkableTemplates = [], isPending: linkablePending } = useListLinkableTemplates(scope)

  const userRole = project?.userRole ?? Role.VIEWER
  const canLink = userRole === Role.OWNER

  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [cueTemplate, setCueTemplate] = useState(DEFAULT_CUE_TEMPLATE)
  const [error, setError] = useState<string | null>(null)
  const [previewOpen, setPreviewOpen] = useState(false)
  const [selectedLinkedKeys, setSelectedLinkedKeys] = useState<string[]>([])
  const [selectedVersionConstraints, setSelectedVersionConstraints] = useState<Map<string, string>>(new Map())

  // Group linkable templates by scope for display.
  const orgTemplates = linkableTemplates.filter(
    (t) => scopeFromNamespace(t.namespace) === TemplateScope.ORGANIZATION,
  )
  const folderTemplates = linkableTemplates.filter(
    (t) => scopeFromNamespace(t.namespace) === TemplateScope.FOLDER,
  )

  // Fall back to "istio-ingress" only after the org query has successfully
  // resolved with no value configured. While the org load is pending or
  // errored (e.g. a project EDITOR may not have org-read permission), omit
  // the field entirely so the preview never advertises a value that may be
  // incorrect — the backend (HOL-644) still injects the org's actual value
  // at render time.
  const orgLoaded = (project?.organization ?? '').length > 0 && !orgPending && !orgError
  const gatewayNamespace = orgLoaded ? (org?.gatewayNamespace || 'istio-ingress') : ''
  const gatewayNamespaceLine = gatewayNamespace
    ? `\tgatewayNamespace: "${gatewayNamespace}"\n`
    : ''
  const previewCuePlatformInput = `platform: {
\tproject:          "${projectName}"
\tnamespace:        "holos-prj-${projectName}"
${gatewayNamespaceLine}\tclaims: {
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

  // Build LinkedTemplateRef objects from the currently selected keys for the
  // preview render so the preview pane shows grouped output (platform + project).
  const previewLinkedTemplates: LinkedTemplateRef[] = selectedLinkedKeys.map((key) => {
    const parsed = parseLinkableKey(key)
    const vc = selectedVersionConstraints.get(key) ?? ''
    return {
      namespace: namespaceFor(parsed.scope, parsed.scopeName),
      name: parsed.name,
      versionConstraint: vc,
    } as LinkedTemplateRef
  })

  const debouncedCueTemplate = useDebouncedValue(cueTemplate, 500)
  const renderQuery = useRenderTemplate(
    scope,
    debouncedCueTemplate,
    previewCueInput,
    previewOpen,
    previewCuePlatformInput,
    previewLinkedTemplates,
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
      // Build LinkedTemplateRef objects from scope-qualified keys with version constraints.
      const linkedTemplates: LinkedTemplateRef[] = selectedLinkedKeys
        .map((key) => {
          const parsed = parseLinkableKey(key)
          const vc = selectedVersionConstraints.get(key) ?? ''
          return create(LinkedTemplateRefSchema, {
            namespace: namespaceFor(parsed.scope, parsed.scopeName),
            name: parsed.name,
            versionConstraint: vc,
          })
        })

      await createMutation.mutateAsync({
        name: name.trim(),
        displayName: displayName.trim(),
        description: description.trim(),
        cueTemplate,
        linkedTemplates,
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
          <div className="space-y-3">
            <Label>Linked Platform Templates</Label>
            {linkablePending ? (
              <p className="text-sm text-muted-foreground">Loading platform templates...</p>
            ) : linkableTemplates.length === 0 ? (
              <p className="text-sm text-muted-foreground">No platform templates available to link. Create organization or folder templates to enable linking.</p>
            ) : canLink ? (
              <div className="space-y-4">
                {orgTemplates.length > 0 && (
                  <div className="space-y-2">
                    <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Organization Templates</p>
                    {orgTemplates.map((t) => {
                      const key = linkableKey(scopeFromNamespace(t.namespace), scopeNameFromNamespace(t.namespace), t.name)
                      const hasReleases = t.releases && t.releases.length > 0
                      const forced = !!t.forced
                      return (
                      <div key={key} className="flex items-start gap-2">
                        <Checkbox
                          id={`linked-create-${key}`}
                          checked={forced || selectedLinkedKeys.includes(key)}
                          disabled={forced}
                          onCheckedChange={(checked) => {
                            if (forced) return
                            setSelectedLinkedKeys((prev) =>
                              checked ? [...prev, key] : prev.filter((k) => k !== key),
                            )
                          }}
                        />
                        <div className="flex flex-col gap-1">
                          <label htmlFor={`linked-create-${key}`} className={`text-sm font-medium leading-none flex items-center gap-1 ${forced ? 'cursor-default' : 'cursor-pointer'}`}>
                            {t.displayName || t.name}
                            {forced && (
                              <span className="inline-flex items-center rounded bg-muted px-1.5 py-0.5 text-xs font-medium text-muted-foreground">
                                Always applied
                              </span>
                            )}
                          </label>
                          {t.description && (
                            <p className="text-xs text-muted-foreground">{t.description}</p>
                          )}
                          {hasReleases && (
                            <Select
                              value={selectedVersionConstraints.get(key) ?? ''}
                              onValueChange={(val) => {
                                setSelectedVersionConstraints((prev) => {
                                  const next = new Map(prev)
                                  next.set(key, val === '__latest__' ? '' : val)
                                  return next
                                })
                              }}
                            >
                              <SelectTrigger size="sm" className="w-40 text-xs">
                                <SelectValue placeholder="Latest (auto-update)" />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="__latest__">Latest (auto-update)</SelectItem>
                                {t.releases.map((r) => (
                                  <SelectItem key={r.version} value={r.version}>{r.version}</SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          )}
                        </div>
                      </div>
                      )
                    })}
                  </div>
                )}
                {folderTemplates.length > 0 && (
                  <div className="space-y-2">
                    <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Folder Templates</p>
                    {folderTemplates.map((t) => {
                      const key = linkableKey(scopeFromNamespace(t.namespace), scopeNameFromNamespace(t.namespace), t.name)
                      const hasReleases = t.releases && t.releases.length > 0
                      const forced = !!t.forced
                      return (
                      <div key={key} className="flex items-start gap-2">
                        <Checkbox
                          id={`linked-create-${key}`}
                          checked={forced || selectedLinkedKeys.includes(key)}
                          disabled={forced}
                          onCheckedChange={(checked) => {
                            if (forced) return
                            setSelectedLinkedKeys((prev) =>
                              checked ? [...prev, key] : prev.filter((k) => k !== key),
                            )
                          }}
                        />
                        <div className="flex flex-col gap-1">
                          <label htmlFor={`linked-create-${key}`} className={`text-sm font-medium leading-none flex items-center gap-1 ${forced ? 'cursor-default' : 'cursor-pointer'}`}>
                            {t.displayName || t.name}
                            {forced && (
                              <span className="inline-flex items-center rounded bg-muted px-1.5 py-0.5 text-xs font-medium text-muted-foreground">
                                Always applied
                              </span>
                            )}
                          </label>
                          {t.description && (
                            <p className="text-xs text-muted-foreground">{t.description}</p>
                          )}
                          {hasReleases && (
                            <Select
                              value={selectedVersionConstraints.get(key) ?? ''}
                              onValueChange={(val) => {
                                setSelectedVersionConstraints((prev) => {
                                  const next = new Map(prev)
                                  next.set(key, val === '__latest__' ? '' : val)
                                  return next
                                })
                              }}
                            >
                              <SelectTrigger size="sm" className="w-40 text-xs">
                                <SelectValue placeholder="Latest (auto-update)" />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="__latest__">Latest (auto-update)</SelectItem>
                                {t.releases.map((r) => (
                                  <SelectItem key={r.version} value={r.version}>{r.version}</SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          )}
                        </div>
                      </div>
                      )
                    })}
                  </div>
                )}
              </div>
            ) : (
              <div className="space-y-2">
                <p className="text-xs text-muted-foreground">Only owners can link platform templates.</p>
              </div>
            )}
          </div>
          <div>
            <Button variant="outline" type="button" onClick={() => setPreviewOpen((v) => !v)}>
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
                      {renderQuery.error instanceof Error ? renderQuery.error.message : String(renderQuery.error)}
                    </AlertDescription>
                  </Alert>
                )}
                {renderQuery.data && (() => {
                  const platformJson = renderQuery.data.platformResourcesJson ?? ''
                  const projectJson = renderQuery.data.projectResourcesJson ?? ''
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
                          <p className="text-sm text-muted-foreground">No platform resources rendered by this template.</p>
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
