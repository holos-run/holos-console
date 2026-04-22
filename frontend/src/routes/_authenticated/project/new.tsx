/**
 * /project/new — dedicated page for creating a new project.
 *
 * Accepts parent context via search params:
 *   - orgName    (required) — the organization that owns the project
 *   - folderName (optional) — the folder under which the project is nested
 *   - returnTo   (optional) — post-create redirect (validated by resolveReturnTo)
 *
 * On success navigates to `resolveReturnTo(search.returnTo, '/projects/$name/secrets')`.
 * The default fallback preserves the existing behaviour from create-project-dialog.tsx
 * (lines 101-104) where post-create navigation always went to the project's secrets page.
 *
 * Replaces CreateProjectDialog modal (HOL-871).
 */

import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useCreateProject } from '@/queries/projects'
import { ParentType } from '@/gen/holos/console/v1/folders_pb'
import { toSlug } from '@/lib/slug'
import { resolveReturnTo } from '@/lib/return-to'

export const Route = createFileRoute('/_authenticated/project/new')({
  validateSearch: (
    search: Record<string, unknown>,
  ): {
    orgName?: string
    folderName?: string
    returnTo?: string
  } => ({
    orgName: typeof search.orgName === 'string' ? search.orgName : undefined,
    folderName: typeof search.folderName === 'string' ? search.folderName : undefined,
    returnTo: typeof search.returnTo === 'string' ? search.returnTo : undefined,
  }),
  component: ProjectNewRoute,
})

function ProjectNewRoute() {
  const search = Route.useSearch()
  return (
    <ProjectNewPage
      orgName={search.orgName}
      folderName={search.folderName}
      returnTo={search.returnTo}
    />
  )
}

export interface ProjectNewPageProps {
  orgName?: string
  folderName?: string
  returnTo?: string
}

export function ProjectNewPage({ orgName, folderName, returnTo }: ProjectNewPageProps) {
  const navigate = useNavigate()

  // Parent context: folder takes precedence over org root.
  const parentType = folderName ? ParentType.FOLDER : ParentType.ORGANIZATION
  const parentName = folderName ?? orgName ?? ''

  // Cancel destination: honour returnTo, fall back to /organizations.
  const cancelTarget = resolveReturnTo(returnTo, '/organizations')

  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [nameEdited, setNameEdited] = useState(false)
  const [description, setDescription] = useState('')
  const [error, setError] = useState<string | null>(null)

  const { mutateAsync, isPending } = useCreateProject()

  const handleDisplayNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value
    setDisplayName(val)
    if (!nameEdited) {
      setName(toSlug(val))
    }
  }

  const handleNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setNameEdited(true)
    setName(e.target.value)
  }

  const handleResetName = () => {
    setNameEdited(false)
    setName(toSlug(displayName))
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!orgName || !name) return
    setError(null)
    try {
      const response = await mutateAsync({
        name,
        displayName,
        description,
        organization: orgName,
        parentType,
        parentName,
      })
      // Default fallback: navigate to the newly created project's secrets page,
      // preserving the behaviour that was hard-coded in create-project-dialog.tsx.
      const fallback = `/projects/${response.name}/secrets`
      const target = resolveReturnTo(returnTo, fallback)
      navigate({ to: target })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create project')
    }
  }

  // Missing required orgName — show an explanatory error with a link back.
  if (!orgName) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>New Project</CardTitle>
        </CardHeader>
        <CardContent>
          <Alert variant="destructive">
            <AlertDescription>
              An organization is required to create a project.{' '}
              <Link to="/organizations" className="underline">
                Select an organization
              </Link>{' '}
              first.
            </AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>New Project</CardTitle>
      </CardHeader>
      <CardContent>
        <form role="form" onSubmit={handleSubmit}>
          <div className="space-y-4 py-2">
            {error && (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
            <div className="space-y-1">
              <Label htmlFor="project-display-name">Display Name</Label>
              <Input
                id="project-display-name"
                value={displayName}
                onChange={handleDisplayNameChange}
                placeholder="My Project"
                autoFocus
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="project-name">Name</Label>
              <Input
                id="project-name"
                value={name}
                onChange={handleNameChange}
                placeholder="my-project"
                pattern="[a-z0-9-]+"
                required
              />
              {nameEdited ? (
                <button
                  type="button"
                  className="text-xs text-primary underline"
                  onClick={handleResetName}
                >
                  Auto-derive from display name
                </button>
              ) : (
                <p className="text-xs text-muted-foreground">
                  Auto-derived from display name. Lowercase letters, numbers, and hyphens only.
                </p>
              )}
            </div>
            <div className="space-y-1">
              <Label htmlFor="project-description">Description</Label>
              <Textarea
                id="project-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Optional description"
              />
            </div>
            <div className="space-y-1 text-sm text-muted-foreground">
              <p>
                Organization: <span className="font-medium text-foreground">{orgName}</span>
              </p>
              {folderName && (
                <p>
                  Folder: <span className="font-medium text-foreground">{folderName}</span>
                </p>
              )}
            </div>
          </div>
          <div className="flex items-center gap-3 pt-4">
            <Button type="submit" disabled={isPending || !name}>
              {isPending ? 'Creating…' : 'Create Project'}
            </Button>
            <Link to={cancelTarget}>
              <Button variant="ghost" type="button" aria-label="Cancel">
                Cancel
              </Button>
            </Link>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
