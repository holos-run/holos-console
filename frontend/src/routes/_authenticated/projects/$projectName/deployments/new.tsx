import { useEffect, useRef, useState } from 'react'
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
import {
  useListTemplates,
  useGetTemplateDefaults,
  makeProjectScope,
  type TemplateDefaults,
} from '@/queries/templates'

export const Route = createFileRoute('/_authenticated/projects/$projectName/deployments/new')({
  component: CreateDeploymentRoute,
})

function CreateDeploymentRoute() {
  const { projectName } = Route.useParams()
  return <CreateDeploymentPage projectName={projectName} />
}

/**
 * Defaultable form fields per ADR 027 §6. This is the single code-level copy
 * of the list; the ADR is the authoritative enumeration. Keep in sync if the
 * ADR adds or removes fields.
 */
export const DEFAULTABLE_FIELDS = [
  'displayName',
  'name',
  'description',
  'image',
  'tag',
  'port',
  'command',
  'args',
  'env',
] as const

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
  const scope = makeProjectScope(projectName)
  const { data: templates = [] } = useListTemplates(scope)

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

  // ADR 027 §3: track a single isPristine boolean. Starts true; flips to false
  // on any user edit of a defaultable field; resets to true on a successful
  // selection pre-fill or a Load defaults click.
  const [isPristine, setIsPristine] = useState(true)

  // ADR 027 §1: the explicit GetTemplateDefaults RPC is the sole source of
  // pre-fill data. We intentionally ignore the embedded Template.defaults
  // field on ListTemplates responses.
  const defaultsQuery = useGetTemplateDefaults({ scope, name: template })
  const {
    data: fetchedDefaults,
    refetch: refetchDefaults,
    isFetching: defaultsFetching,
    isSuccess: defaultsSuccess,
    isError: defaultsIsError,
    error: defaultsError,
  } = defaultsQuery

  const slugify = (val: string) =>
    val.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

  /**
   * Apply a TemplateDefaults payload to the form. Per ADR 027, this is the
   * one place that knows how each field maps. The port rule preserves the
   * current value when the response carries zero so a user-friendly default
   * (8080) is not clobbered by an unset proto int32.
   */
  const applyDefaults = (d: TemplateDefaults | undefined) => {
    if (!d) {
      // Reset all defaultable fields to initial empty state so switching to a
      // template with no authored defaults does not leak the previous
      // template's values onto the form.
      setDisplayName('')
      setName('')
      setDescription('')
      setImage('')
      setTag('')
      setCommand([])
      setArgs([])
      setEnv([])
      return
    }
    if (d.name) {
      setDisplayName(d.name)
      setName(slugify(d.name))
    } else {
      setDisplayName('')
      setName('')
    }
    setDescription(d.description ?? '')
    setImage(d.image ?? '')
    setTag(d.tag ?? '')
    // Per acceptance criteria: only pre-fill port when response carries a
    // nonzero value, otherwise keep the current form value.
    if (d.port) {
      setPort(d.port)
    }
    setCommand(d.command ?? [])
    setArgs(d.args ?? [])
    setEnv(d.env ?? [])
  }

  // Track the last template name we applied defaults for (pristine path) so
  // we apply exactly once per successful fetch per template, and do not
  // re-apply after the user dirties the form.
  const lastAppliedTemplateRef = useRef<string>('')

  // Track the pending "Load defaults" click so the refetch's resolved data
  // is applied unconditionally (even when the form is dirty).
  //
  // This is React state (not a ref) so that clearing it triggers a re-render
  // and the pristine-prefill effect re-runs. Without that re-render, a user
  // who switches templates while a Load defaults refetch is in flight could
  // have the new template's query resolve during the pending window, bail
  // out of pristine pre-fill, and then never re-run after the pending flag
  // was cleared — leaving the new template's defaults unapplied.
  const [loadDefaultsPending, setLoadDefaultsPending] = useState(false)

  // Mirror the current template selection in a ref so handleLoadDefaults can
  // compare its click-time captured value against the *latest* selection when
  // the refetch resolves, without depending on a stale closure.
  const templateRef = useRef(template)
  useEffect(() => {
    templateRef.current = template
  }, [template])

  // ADR 027 §4: on every template change the selection hook refetches
  // automatically (the query key includes the template name). When the
  // response lands AND the form is pristine, overwrite all defaultable
  // fields. Otherwise leave the in-progress edits alone.
  useEffect(() => {
    if (!template) {
      lastAppliedTemplateRef.current = ''
      return
    }
    if (defaultsFetching) return
    // Only react to fresh data for the current template.
    if (lastAppliedTemplateRef.current === template) return
    if (loadDefaultsPending) {
      // Load defaults path handles its own overwrite below. Because this is
      // state (not a ref), clearing it re-runs this effect so a template
      // switch mid-flight still gets pristine pre-fill when the new query
      // resolves.
      return
    }
    // Only apply defaults when the RPC actually succeeded. On error, leave
    // the form as-is and surface the failure via defaultsIsError rather than
    // silently clearing defaultable fields (which would happen if we treated
    // undefined data as "no defaults"). Do not mark the template as applied
    // on failure, so a manual retry (Load defaults) or template change can
    // try again.
    if (!defaultsSuccess) return
    if (isPristine) {
      applyDefaults(fetchedDefaults)
      // Pre-fill is programmatic; keep the form pristine per ADR 027 §3.
      setIsPristine(true)
    }
    lastAppliedTemplateRef.current = template
    // applyDefaults is stable (closes over setters). We intentionally depend
    // only on the data we care about here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [template, fetchedDefaults, defaultsFetching, defaultsSuccess, isPristine, loadDefaultsPending])

  const handleTemplateChange = (templateName: string) => {
    setTemplate(templateName)
    // Clear the applied marker so the effect applies the new template's
    // defaults when the RPC resolves.
    lastAppliedTemplateRef.current = ''
  }

  const handleLoadDefaults = async () => {
    if (!template) return
    // Capture the template selected at click time. If the user changes the
    // selection before the refetch resolves, we must silently drop this
    // stale response rather than apply template A's defaults onto a form
    // that now reflects template B.
    const requestedTemplate = template
    setLoadDefaultsPending(true)
    try {
      const result = await refetchDefaults()
      // Stale-response guard: the selection changed while the request was
      // in flight. Drop the result without touching form state, pristine,
      // or error state — the new selection's own fetch will handle pre-fill.
      if (requestedTemplate !== templateRef.current) {
        return
      }
      // refetch() resolves to a query result even when the RPC errors.
      // Only apply defaults and reset pristine on success; on failure,
      // leave the user's edits intact and surface the error.
      if (result.status === 'error') {
        setError(
          result.error instanceof Error
            ? `Failed to load defaults: ${result.error.message}`
            : 'Failed to load defaults',
        )
        return
      }
      // ADR 027 §5: always overwrite, regardless of isPristine.
      applyDefaults(result.data)
      setIsPristine(true)
      lastAppliedTemplateRef.current = requestedTemplate
    } finally {
      setLoadDefaultsPending(false)
    }
  }

  // Wrap each defaultable-field setter so user edits flip isPristine to false
  // per ADR 027 §3. Pre-fill writes call the raw setters directly.
  const dirty = <T,>(setter: (val: T) => void) => (val: T) => {
    setter(val)
    setIsPristine(false)
  }

  const handleDisplayNameChange = (val: string) => {
    setDisplayName(val)
    setName(slugify(val))
    setIsPristine(false)
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
              <div className="flex items-center gap-2">
                <div className="flex-1">
                  <Combobox
                    aria-label="Template"
                    items={templates.map((t) => ({ value: t.name, label: t.name }))}
                    value={template}
                    onValueChange={handleTemplateChange}
                    placeholder="Select a template..."
                    searchPlaceholder="Search templates..."
                    emptyMessage="No templates found."
                  />
                </div>
                <Button
                  type="button"
                  variant="outline"
                  onClick={handleLoadDefaults}
                  disabled={!template || defaultsFetching}
                >
                  Load defaults
                </Button>
              </div>
            )}
          </div>
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
              onChange={(e) => dirty(setName)(e.target.value)}
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
              onChange={(e) => dirty(setDescription)(e.target.value)}
              placeholder="What does this deployment serve?"
            />
          </div>
          <div>
            <Label htmlFor="deployment-image">Image</Label>
            <Input
              id="deployment-image"
              aria-label="Image"
              value={image}
              onChange={(e) => dirty(setImage)(e.target.value)}
              placeholder="ghcr.io/org/app"
            />
          </div>
          <div>
            <Label htmlFor="deployment-tag">Tag</Label>
            <Input
              id="deployment-tag"
              aria-label="Tag"
              value={tag}
              onChange={(e) => dirty(setTag)(e.target.value)}
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
              onChange={(e) => dirty(setPort)(parseInt(e.target.value, 10))}
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
              onChange={dirty(setCommand)}
              placeholder="command entry"
              addLabel="Add command"
            />
          </div>
          <div>
            <Label>Args</Label>
            <p className="text-xs text-muted-foreground mb-1">Override container CMD (optional)</p>
            <StringListInput
              value={args}
              onChange={dirty(setArgs)}
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
              onChange={dirty(setEnv)}
            />
          </div>
          {defaultsIsError && (
            <Alert variant="destructive" role="alert" aria-label="Template defaults error">
              <AlertDescription>
                Failed to load template defaults
                {defaultsError instanceof Error ? `: ${defaultsError.message}` : ''}.
                Fields were not pre-filled; you can edit them manually or click
                Load defaults to retry.
              </AlertDescription>
            </Alert>
          )}
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
