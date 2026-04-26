/**
 * Project-scoped template clone page (HOL-974, HOL-975).
 *
 * The Service Owner selects a source from the organization's platform
 * templates (ancestor-scope linkable templates) and gives the clone a
 * display name. On success the mutation invalidates the project templates
 * list and routes to the new template's detail/edit page.
 *
 * Source picker uses useListLinkableTemplates(namespace), which returns
 * enabled ancestor-scope (org/folder) templates that can be cloned into
 * the project namespace. This is the "clone-as-authoring" flow described
 * in HOL-974.
 *
 * HOL-975: accepts an optional `cloneSource` search param encoding
 * "namespace/name" to pre-select a platform template when navigating from
 * the org-scope template detail page via the "Clone to project" CTA.
 *
 * HOL-1024: ScopePicker is rendered with value fixed to 'project' (disabled)
 * because the clone destination is always the project namespace encoded in
 * the route URL. The picker communicates the scope visually, aligning this
 * page with all other "new" pages in the HOL-1016 wave.
 */

import { useState, useEffect } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Combobox } from '@/components/ui/combobox'
import { ScopePicker } from '@/components/scope-picker/ScopePicker'
import {
  useCloneTemplate,
  useListLinkableTemplates,
} from '@/queries/templates'
import { namespaceForProject } from '@/lib/scope-labels'

// ---------------------------------------------------------------------------
// Route search — optional clone source pre-selection (HOL-975)
// ---------------------------------------------------------------------------

export interface CloneTemplateSearch {
  /** Encoded as "namespace/name". Set by the org template detail "Clone to project" CTA. */
  cloneSource?: string
}

function parseCloneTemplateSearch(raw: Record<string, unknown>): CloneTemplateSearch {
  const result: CloneTemplateSearch = {}
  const cloneSource = raw['cloneSource']
  if (typeof cloneSource === 'string' && cloneSource.length > 0) {
    result.cloneSource = cloneSource
  }
  return result
}

export const Route = createFileRoute('/_authenticated/projects/$projectName/templates/new')({
  validateSearch: parseCloneTemplateSearch,
  component: CloneTemplateRoute,
})

function CloneTemplateRoute() {
  const { projectName } = Route.useParams()
  const search = Route.useSearch() as CloneTemplateSearch
  return <CloneTemplatePage projectName={projectName} cloneSource={search.cloneSource} />
}

export function CloneTemplatePage({
  projectName,
  cloneSource,
}: {
  projectName: string
  /** Optional "namespace/name" pre-selection from the org template detail CTA. */
  cloneSource?: string
}) {
  const navigate = useNavigate()
  const namespace = namespaceForProject(projectName)

  // Source picker: ancestor-scope (org/folder) linkable templates.
  const { data: linkableTemplates = [], isPending: sourcesLoading } =
    useListLinkableTemplates(namespace)

  const cloneMutation = useCloneTemplate(namespace)

  const [sourceName, setSourceName] = useState('')
  const [sourceNamespace, setSourceNamespace] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)

  // Pre-select the source from the cloneSource query param (HOL-975).
  // Only fires once when templates have loaded and a cloneSource param is present.
  useEffect(() => {
    if (!cloneSource || sourcesLoading || linkableTemplates.length === 0) return
    const slash = cloneSource.indexOf('/')
    if (slash < 0) return
    const preNs = cloneSource.slice(0, slash)
    const preName = cloneSource.slice(slash + 1)
    const match = linkableTemplates.find(
      (t) => t.namespace === preNs && t.name === preName,
    )
    if (match) {
      setSourceNamespace(preNs)
      setSourceName(preName)
      if (!displayName) {
        const label = match.displayName || match.name
        setDisplayName(label)
        setName(label.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, ''))
      }
    }
  // Run only when linkableTemplates becomes non-empty for the first time, or cloneSource changes.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cloneSource, sourcesLoading, linkableTemplates.length])

  const slugify = (val: string) =>
    val.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

  const handleDisplayNameChange = (val: string) => {
    setDisplayName(val)
    // Only auto-derive the slug if the user has not manually edited it or if
    // the current slug still matches the previous auto-derived value.
    setName(slugify(val))
  }

  // The combobox item value encodes "namespace/name" so the picker can
  // distinguish same-named templates from different scopes.
  const comboboxItems = linkableTemplates.map((t) => ({
    value: `${t.namespace}/${t.name}`,
    label: t.displayName || t.name,
  }))

  const handleSourceChange = (value: string) => {
    const slash = value.indexOf('/')
    if (slash < 0) {
      setSourceNamespace('')
      setSourceName(value)
    } else {
      setSourceNamespace(value.slice(0, slash))
      setSourceName(value.slice(slash + 1))
    }
    // Pre-fill the display name from the selected template's label if the
    // field is currently empty.
    if (!displayName) {
      const item = comboboxItems.find((i) => i.value === value)
      if (item) {
        handleDisplayNameChange(item.label)
      }
    }
  }

  const handleClone = async () => {
    if (!sourceName) {
      setError('Select a source platform template')
      return
    }
    if (!name.trim()) {
      setError('Template name is required')
      return
    }
    setError(null)
    try {
      await cloneMutation.mutateAsync({
        sourceName,
        name: name.trim(),
        displayName: displayName.trim() || name.trim(),
      })
      await navigate({
        to: '/projects/$projectName/templates/$templateName',
        params: { projectName, templateName: name.trim() },
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const selectedValue = sourceName ? `${sourceNamespace}/${sourceName}` : ''

  return (
    <Card>
      <CardHeader>
        <CardTitle>Clone Platform Template</CardTitle>
      </CardHeader>
      <CardContent>
        {/* HOL-1024: scope is always 'project' for this clone-only route. The
            picker is disabled because the destination namespace is fixed by the
            $projectName URL segment. It communicates the scope visually and
            aligns this page with the other HOL-1016 "new" pages. */}
        <div className="mb-4 flex items-center gap-2">
          <span className="text-sm text-muted-foreground">Scope:</span>
          <ScopePicker value="project" onChange={() => {}} disabled />
        </div>
        <div className="space-y-4">
          <div>
            <Label>Source Platform Template</Label>
            {sourcesLoading ? (
              <p className="text-sm text-muted-foreground mt-1">Loading platform templates…</p>
            ) : linkableTemplates.length === 0 ? (
              <p className="text-sm text-muted-foreground mt-1">
                No platform templates are available to clone. Contact your organization
                administrator to enable platform templates.
              </p>
            ) : (
              <Combobox
                aria-label="Source Platform Template"
                items={comboboxItems}
                value={selectedValue}
                onValueChange={handleSourceChange}
                placeholder="Select a platform template…"
                searchPlaceholder="Search platform templates…"
                emptyMessage="No platform templates found."
              />
            )}
          </div>

          <div>
            <Label htmlFor="template-display-name">Display Name</Label>
            <Input
              id="template-display-name"
              aria-label="Display Name"
              autoFocus
              value={displayName}
              onChange={(e) => handleDisplayNameChange(e.target.value)}
              placeholder="My Web App Template"
            />
          </div>

          <div>
            <Label>Name (slug)</Label>
            <Input
              aria-label="Name slug"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-web-app-template"
            />
            <p className="text-xs text-muted-foreground mt-1">
              Auto-derived from display name. Lowercase alphanumeric and hyphens only.
            </p>
          </div>

          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          <div className="flex items-center gap-3 pt-2">
            <Button
              onClick={handleClone}
              disabled={cloneMutation.isPending || sourcesLoading}
            >
              {cloneMutation.isPending ? 'Cloning…' : 'Clone Template'}
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
