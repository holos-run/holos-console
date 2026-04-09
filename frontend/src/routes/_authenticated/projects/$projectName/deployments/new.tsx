import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Combobox } from '@/components/ui/combobox'
import { StringListInput } from '@/components/string-list-input'
import { EnvVarEditor, filterEnvVars } from '@/components/env-var-editor'
import type { EnvVar } from '@/gen/holos/console/v1/deployments_pb'
import { useCreateDeployment } from '@/queries/deployments'
import { useListDeploymentTemplates } from '@/queries/deployment-templates'

export const Route = createFileRoute('/_authenticated/projects/$projectName/deployments/new')({
  component: CreateDeploymentRoute,
})

function CreateDeploymentRoute() {
  const { projectName } = Route.useParams()
  return <CreateDeploymentPage projectName={projectName} />
}

export function CreateDeploymentPage({ projectName: propProjectName }: { projectName?: string } = {}) {
  let routeProjectName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeProjectName = Route.useParams().projectName
  } catch {
    routeProjectName = undefined
  }
  const projectName = propProjectName ?? routeProjectName ?? ''

  const navigate = useNavigate()
  const createMutation = useCreateDeployment(projectName)
  const { data: templates = [] } = useListDeploymentTemplates(projectName)

  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [template, setTemplate] = useState('')
  const [image, setImage] = useState('')
  const [tag, setTag] = useState('')
  const [port, setPort] = useState(8080)
  const [command, setCommand] = useState<string[]>([])
  const [args, setArgs] = useState<string[]>([])
  const [env, setEnv] = useState<EnvVar[]>([])
  const [error, setError] = useState<string | null>(null)

  const slugify = (val: string) =>
    val.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

  const handleDisplayNameChange = (val: string) => {
    setDisplayName(val)
    setName(slugify(val))
  }

  const handleTemplateChange = (templateName: string) => {
    setTemplate(templateName)
    const selected = templates.find((t) => t.name === templateName)
    const defaults = selected?.defaults
    // Pre-fill name and description from CUE-extracted defaults (ADR 018).
    if (defaults?.name) {
      setDisplayName(defaults.name)
      setName(slugify(defaults.name))
    }
    if (defaults?.description) {
      setDescription(defaults.description)
    }
    setImage(defaults?.image ?? '')
    setTag(defaults?.tag ?? '')
    setPort(defaults?.port || 8080)
    setCommand(defaults?.command ?? [])
    setArgs(defaults?.args ?? [])
    setEnv(defaults?.env ?? [])
  }

  const handleCreate = async () => {
    if (!name.trim()) {
      setError('Name is required')
      return
    }
    if (!template) {
      setError('Template is required')
      return
    }
    if (!image.trim()) {
      setError('Image is required')
      return
    }
    if (!tag.trim()) {
      setError('Tag is required')
      return
    }
    if (!port || port < 1 || port > 65535 || !Number.isInteger(port)) {
      setError('Port must be between 1 and 65535')
      return
    }
    setError(null)
    try {
      const result = await createMutation.mutateAsync({
        name: name.trim(),
        displayName: displayName.trim(),
        description: description.trim(),
        template,
        image: image.trim(),
        tag: tag.trim(),
        port,
        command,
        args,
        env: filterEnvVars(env),
      })
      const deploymentName = result.name || name.trim()
      await navigate({
        to: '/projects/$projectName/deployments/$deploymentName',
        params: { projectName, deploymentName },
        search: { tab: 'status' },
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Create Deployment</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          <div>
            <Label htmlFor="deployment-display-name">Display Name</Label>
            <Input
              id="deployment-display-name"
              aria-label="Display Name"
              autoFocus
              value={displayName}
              onChange={(e) => handleDisplayNameChange(e.target.value)}
              placeholder="My API"
            />
          </div>
          <div>
            <Label>Name (slug)</Label>
            <Input
              aria-label="Name slug"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-api"
            />
            <p className="text-xs text-muted-foreground mt-1">
              Auto-derived from display name. Lowercase alphanumeric and hyphens only.
            </p>
          </div>
          <div>
            <Label htmlFor="deployment-description">Description</Label>
            <Input
              id="deployment-description"
              aria-label="Description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What does this deployment serve?"
            />
          </div>
          <div>
            <Label>Template</Label>
            {templates.length === 0 ? (
              <p className="text-sm text-muted-foreground mt-1">
                No templates available.{' '}
                <Link
                  to="/projects/$projectName/templates/new"
                  params={{ projectName }}
                  className="underline"
                >
                  Create a template
                </Link>{' '}
                first.
              </p>
            ) : (
              <Combobox
                aria-label="Template"
                items={templates.map((t) => ({ value: t.name, label: t.name }))}
                value={template}
                onValueChange={handleTemplateChange}
                placeholder="Select a template..."
                searchPlaceholder="Search templates..."
                emptyMessage="No templates found."
              />
            )}
          </div>
          <div>
            <Label htmlFor="deployment-image">Image</Label>
            <Input
              id="deployment-image"
              aria-label="Image"
              value={image}
              onChange={(e) => setImage(e.target.value)}
              placeholder="ghcr.io/org/app"
            />
          </div>
          <div>
            <Label htmlFor="deployment-tag">Tag</Label>
            <Input
              id="deployment-tag"
              aria-label="Tag"
              value={tag}
              onChange={(e) => setTag(e.target.value)}
              placeholder="v1.0.0"
            />
          </div>
          <div>
            <Label htmlFor="deployment-port">Port</Label>
            <Input
              id="deployment-port"
              aria-label="Port"
              type="number"
              min={1}
              max={65535}
              value={port}
              onChange={(e) => setPort(parseInt(e.target.value, 10))}
              placeholder="8080"
            />
            <p className="text-xs text-muted-foreground mt-1">
              Container port the application listens on (HTTP)
            </p>
          </div>
          <div>
            <Label>Command</Label>
            <p className="text-xs text-muted-foreground mb-1">Override container ENTRYPOINT (optional)</p>
            <StringListInput
              value={command}
              onChange={setCommand}
              placeholder="command entry"
              addLabel="Add command"
            />
          </div>
          <div>
            <Label>Args</Label>
            <p className="text-xs text-muted-foreground mb-1">Override container CMD (optional)</p>
            <StringListInput
              value={args}
              onChange={setArgs}
              placeholder="args entry"
              addLabel="Add args"
            />
          </div>
          <div>
            <Label>Environment Variables</Label>
            <p className="text-xs text-muted-foreground mb-1">Set container environment variables (optional)</p>
            <EnvVarEditor
              project={projectName}
              value={env}
              onChange={setEnv}
            />
          </div>
          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          <div className="flex items-center gap-3 pt-2">
            <Button onClick={handleCreate} disabled={createMutation.isPending}>
              {createMutation.isPending ? 'Creating...' : 'Create Deployment'}
            </Button>
            <Link
              to="/projects/$projectName/deployments"
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
