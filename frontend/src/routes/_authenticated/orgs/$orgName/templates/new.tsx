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
import type { TemplateExample } from '@/queries/templates'
import { namespaceForOrg } from '@/lib/scope-labels'
import { useGetOrganization } from '@/queries/organizations'
import { TemplateExamplePicker } from '@/components/templates/template-example-picker'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/templates/new')({
  component: CreateOrgTemplateRoute,
})

function CreateOrgTemplateRoute() {
  const { orgName } = Route.useParams()
  return <CreateOrgTemplatePage orgName={orgName} />
}

export function CreateOrgTemplatePage({ orgName: propOrgName }: { orgName?: string } = {}) {
  let routeOrgName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeOrgName = Route.useParams().orgName
  } catch {
    routeOrgName = undefined
  }
  const orgName = propOrgName ?? routeOrgName ?? ''

  const navigate = useNavigate()
  const namespace = namespaceForOrg(orgName)
  const createMutation = useCreateTemplate(namespace)
  const { data: org } = useGetOrganization(orgName)

  const userRole = org?.userRole ?? Role.VIEWER
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
      await createMutation.mutateAsync({
        name: name.trim(),
        displayName: displayName.trim(),
        description: description.trim(),
        cueTemplate,
        enabled,
      })
      await navigate({
        to: '/orgs/$orgName/templates/$namespace/$name',
        params: { orgName, namespace, name: name.trim() },
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
            <Link to="/orgs/$orgName/templates" params={{ orgName }} className="hover:underline">
              Templates
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
                <TemplateExamplePicker
                  onSelect={handleSelectExample}
                  disabled={!canWrite}
                />
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Info className="h-4 w-4 text-muted-foreground cursor-default" />
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>
                        Platform templates are unified with project deployment templates at render
                        time via CUE. This example constrains project resources to safe kinds and
                        provides an HTTPRoute for external access.
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
              Enabled (apply to projects in this organization)
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
              to="/orgs/$orgName/templates"
              params={{ orgName }}
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
