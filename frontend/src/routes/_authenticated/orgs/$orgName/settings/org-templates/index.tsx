import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Lock, Info } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useListTemplates, useCreateTemplate, makeOrgScope } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'

// EXAMPLE_HTTPBIN_PLATFORM_TEMPLATE is the example org-level platform template CUE content.
// It matches console/org_templates/example_httpbin_platform.cue.
const EXAMPLE_HTTPBIN_PLATFORM_TEMPLATE = `// Org-level platform template — evaluated at organization scope.
// Any changes here affect every project in the org.
//
// This template does two things:
//  1. Provides an HTTPRoute in platformResources so the gateway routes
//     traffic to the deployment's Service.
//  2. Closes projectResources.namespacedResources to Deployment, Service,
//     and ServiceAccount (ADR 016 Decision 9) so project templates cannot
//     produce any other resource kind.
//
// Pair with console/templates/example_httpbin.cue for the project-level template.

// input and platform are available because platform templates are unified with
// the deployment template before evaluation (ADR 016 Decision 8).
input: #ProjectInput & {
	port: >0 & <=65535 | *8080
}
platform: #PlatformInput

// ── Platform resources (managed by the platform team) ───────────────────────

// platformResources holds resources the platform team manages. The renderer
// reads these only from organization/folder-level templates — project templates
// that define platformResources are silently ignored (ADR 016 Decision 8).
platformResources: {
	namespacedResources: (platform.namespace): {
		// HTTPRoute exposes the deployment's Service via the gateway.
		// It routes all traffic from the gateway to the Service named input.name
		// on port 80 (the Service port, which forwards to containerPort input.port).
		HTTPRoute: (input.name): {
			apiVersion: "gateway.networking.k8s.io/v1"
			kind:       "HTTPRoute"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       input.name
				}
			}
			spec: {
				parentRefs: [{
					group:     "gateway.networking.k8s.io"
					kind:      "Gateway"
					namespace: platform.gatewayNamespace
					name:      "default"
				}]
				rules: [{
					backendRefs: [{
						name: input.name
						port: 80
					}]
				}]
			}
		}
	}
	clusterResources: {}
}

// ── Project resource constraints (enforced by the platform team) ─────────────

// Close projectResources.namespacedResources so that every namespace bucket
// may only contain Deployment, Service, or ServiceAccount. Using close() with
// optional fields is the correct CUE pattern: the close() call marks the struct
// as closed (no additional fields allowed), and the ? marks each listed field
// as optional (a namespace bucket need not contain all three). Any unlisted
// Kind key — such as RoleBinding — is a CUE constraint violation at evaluation
// time, before any Kubernetes API call (ADR 016 Decision 9).
projectResources: namespacedResources: [_]: close({
	Deployment?:     _
	Service?:        _
	ServiceAccount?: _
})
`

export const Route = createFileRoute('/_authenticated/orgs/$orgName/settings/org-templates/')({
  component: OrgTemplatesListRoute,
})

function OrgTemplatesListRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplatesListPage orgName={orgName} />
}

export function OrgTemplatesListPage({ orgName: propOrgName }: { orgName?: string } = {}) {
  let routeOrgName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeOrgName = Route.useParams().orgName
  } catch {
    routeOrgName = undefined
  }
  const orgName = propOrgName ?? routeOrgName ?? ''

  const scope = makeOrgScope(orgName)
  const { data: templates, isPending, error } = useListTemplates(scope)
  const { data: org } = useGetOrganization(orgName)
  const createMutation = useCreateTemplate(scope)

  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createDisplayName, setCreateDisplayName] = useState('')
  const [createDescription, setCreateDescription] = useState('')
  const [createCueTemplate, setCreateCueTemplate] = useState('')
  const [createEnabled, setCreateEnabled] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  const handleOpenCreate = () => {
    setCreateName('')
    setCreateDisplayName('')
    setCreateDescription('')
    setCreateCueTemplate('')
    setCreateEnabled(false)
    setCreateError(null)
    setCreateOpen(true)
  }

  const handleLoadHttpbinExample = () => {
    setCreateName('httpbin-platform')
    setCreateDisplayName('httpbin Platform')
    setCreateDescription('Provides an HTTPRoute for gateway access and constrains project templates to Deployment, Service, and ServiceAccount only.')
    setCreateCueTemplate(EXAMPLE_HTTPBIN_PLATFORM_TEMPLATE)
  }

  const handleCreateConfirm = async () => {
    if (!createName.trim()) {
      setCreateError('Name is required')
      return
    }
    setCreateError(null)
    try {
      await createMutation.mutateAsync({
        name: createName.trim(),
        displayName: createDisplayName.trim(),
        description: createDescription.trim(),
        cueTemplate: createCueTemplate,
        mandatory: false,
        enabled: createEnabled,
      })
      toast.success(`Created platform template "${createName}"`)
      setCreateOpen(false)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    }
  }

  if (isPending) {
    return (
      <Card>
        <CardContent className="pt-6 space-y-4">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-8 w-full" />
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
    <>
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <div>
            <p className="text-sm text-muted-foreground">{orgName} / Settings / Platform Templates</p>
            <CardTitle className="mt-1">Platform Templates</CardTitle>
          </div>
          {canWrite && (
            <Button size="sm" onClick={handleOpenCreate}>Create Template</Button>
          )}
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">
            Platform templates are automatically applied to project namespaces when projects are created.
            Mandatory templates are marked with a lock badge.
          </p>
          <Separator />
          {templates && templates.length > 0 ? (
            <ul className="space-y-2">
              {templates.map((tmpl) => (
                <li key={tmpl.name}>
                  <Link
                    to="/orgs/$orgName/settings/org-templates/$templateName"
                    params={{ orgName, templateName: tmpl.name }}
                    className="flex items-center gap-2 p-3 rounded-md hover:bg-muted transition-colors"
                  >
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium font-mono">{tmpl.name}</span>
                        {tmpl.mandatory && (
                          <Badge variant="secondary" className="flex items-center gap-1 text-xs">
                            <Lock className="h-3 w-3" />
                            Mandatory
                          </Badge>
                        )}
                        {tmpl.enabled ? (
                          <Badge variant="outline" className="text-xs text-green-500 border-green-500/30">
                            Enabled
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="text-xs text-muted-foreground">
                            Disabled
                          </Badge>
                        )}
                      </div>
                      {tmpl.description && (
                        <p className="text-xs text-muted-foreground truncate mt-0.5">{tmpl.description}</p>
                      )}
                    </div>
                  </Link>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-muted-foreground">No platform templates found.</p>
          )}
        </CardContent>
      </Card>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Create Platform Template</DialogTitle>
            <DialogDescription>
              Create a new platform template for organization &quot;{orgName}&quot;.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="flex items-center gap-2">
              <Button variant="outline" size="sm" onClick={handleLoadHttpbinExample}>
                Load httpbin Example
              </Button>
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Info className="h-4 w-4 text-muted-foreground cursor-default" />
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Platform templates are unified with project deployment templates at render time via CUE. This example constrains project resources to safe kinds and provides an HTTPRoute for external access.</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            </div>
            <div className="space-y-2">
              <Label htmlFor="create-name">Name</Label>
              <Input
                id="create-name"
                aria-label="Name"
                value={createName}
                onChange={(e) => setCreateName(e.target.value)}
                placeholder="my-template"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="create-display-name">Display Name</Label>
              <Input
                id="create-display-name"
                aria-label="Display Name"
                value={createDisplayName}
                onChange={(e) => setCreateDisplayName(e.target.value)}
                placeholder="My Template"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="create-description">Description</Label>
              <Input
                id="create-description"
                aria-label="Description"
                value={createDescription}
                onChange={(e) => setCreateDescription(e.target.value)}
                placeholder="What does this template produce?"
              />
            </div>
            <div className="flex items-center gap-3">
              <Switch
                id="create-enabled"
                aria-label="Enabled"
                checked={createEnabled}
                onCheckedChange={setCreateEnabled}
              />
              <Label htmlFor="create-enabled" className="text-sm cursor-pointer">
                Enabled (apply to new project namespaces)
              </Label>
            </div>
          </div>
          {createError && (
            <Alert variant="destructive">
              <AlertDescription>{createError}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button onClick={handleCreateConfirm} disabled={createMutation.isPending || !createName}>
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
