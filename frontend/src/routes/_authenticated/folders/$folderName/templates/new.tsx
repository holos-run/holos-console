import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { Info } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useCreateTemplate } from '@/queries/templates'
import { namespaceForFolder } from '@/lib/scope-labels'
import { useGetFolder } from '@/queries/folders'

// EXAMPLE_FOLDER_PLATFORM_TEMPLATE is the example folder-level platform template CUE content.
// It provides an HTTPRoute into the org-configured ingress-gateway namespace
// (platform.gatewayNamespace, set per-org via Settings; falls back to
// "istio-ingress" when unset). Authors who need a literal pin should use the
// same value the org is configured with — see HOL-526.
const EXAMPLE_FOLDER_PLATFORM_TEMPLATE = `// Folder-level platform template — HTTPRoute for the org-configured ingress gateway.
// Applied to projects within this folder hierarchy. The HTTPRoute lands in
// platform.gatewayNamespace, which the backend resolves from the org's
// console.holos.run/gateway-namespace annotation. Configure it on the org's
// Settings page (see HOL-526).
platformResources: {
    namespacedResources: (platform.gatewayNamespace): {
        HTTPRoute: (input.name): {
            apiVersion: "gateway.networking.k8s.io/v1"
            kind:       "HTTPRoute"
            metadata: {
                name:      input.name
                namespace: platform.gatewayNamespace
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
`

export const Route = createFileRoute('/_authenticated/folders/$folderName/templates/new')({
  component: CreateFolderTemplateRoute,
})

function CreateFolderTemplateRoute() {
  const { folderName } = Route.useParams()
  return <CreateFolderTemplatePage folderName={folderName} />
}

export function CreateFolderTemplatePage({ folderName: propFolderName }: { folderName?: string } = {}) {
  let routeFolderName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeFolderName = Route.useParams().folderName
  } catch {
    routeFolderName = undefined
  }
  const folderName = propFolderName ?? routeFolderName ?? ''

  const navigate = useNavigate()
  const namespace = namespaceForFolder(folderName)
  const createMutation = useCreateTemplate(namespace)
  const { data: folder } = useGetFolder(folderName)

  const orgName = folder?.organization ?? ''
  const userRole = folder?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [cueTemplate, setCueTemplate] = useState('')
  const [enabled, setEnabled] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const slugify = (val: string) =>
    val.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

  const handleDisplayNameChange = (val: string) => {
    setDisplayName(val)
    setName(slugify(val))
  }

  const handleLoadExample = () => {
    setName('httproute-ingress')
    setDisplayName('HTTPRoute Ingress')
    setDescription(
      'Provides an HTTPRoute for the org-configured ingress gateway, routing traffic to project services.',
    )
    setCueTemplate(EXAMPLE_FOLDER_PLATFORM_TEMPLATE)
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
        enabled,
      })
      await navigate({
        to: '/folders/$folderName/templates/$templateName',
        params: { folderName, templateName: name.trim() },
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Card>
      <CardHeader>
        <div>
          <p className="text-sm text-muted-foreground">
            <Link to="/orgs/$orgName/settings" params={{ orgName }} className="hover:underline">
              {orgName}
            </Link>
            {' / '}
            <Link to="/orgs/$orgName/resources" params={{ orgName }} className="hover:underline">
              Resources
            </Link>
            {' / '}
            <Link
              to="/folders/$folderName/settings"
              params={{ folderName }}
              className="hover:underline"
            >
              {folderName}
            </Link>
            {' / '}
            <Link
              to="/folders/$folderName/templates"
              params={{ folderName }}
              className="hover:underline"
            >
              Platform Templates
            </Link>
            {' / New'}
          </p>
          <CardTitle className="mt-1">Create Platform Template</CardTitle>
        </div>
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
              placeholder="My Template"
              disabled={!canWrite}
            />
          </div>
          <div>
            <Label>Name (slug)</Label>
            <Input
              aria-label="Name slug"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-template"
              disabled={!canWrite}
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
              disabled={!canWrite}
            />
          </div>
          <div>
            <div className="flex items-center justify-between mb-1">
              <Label htmlFor="template-cue-template">CUE Template</Label>
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" type="button" onClick={handleLoadExample} disabled={!canWrite}>
                  Load Example
                </Button>
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Info className="h-4 w-4 text-muted-foreground cursor-default" />
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>
                        Platform templates are unified with project deployment templates at render
                        time via CUE. This example provides an HTTPRoute for the org-configured
                        ingress gateway (platform.gatewayNamespace).
                      </p>
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
              disabled={!canWrite}
            />
          </div>
          <div className="flex items-center gap-3">
            <Switch
              id="template-enabled"
              aria-label="Enabled"
              checked={enabled}
              onCheckedChange={setEnabled}
              disabled={!canWrite}
            />
            <Label htmlFor="template-enabled" className="text-sm cursor-pointer">
              Enabled (apply to projects in this folder)
            </Label>
          </div>
          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          <div className="flex items-center gap-3 pt-2">
            <Button onClick={handleCreate} disabled={createMutation.isPending || !canWrite}>
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </Button>
            <Link
              to="/folders/$folderName/templates"
              params={{ folderName }}
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
