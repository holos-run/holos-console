/**
 * /folder/new — dedicated page for creating a new folder.
 *
 * Accepts parent context via search params:
 *   - orgName  (required) — the organization that owns the folder
 *   - parentType (optional) — "Organization" | "Folder"; defaults to "Organization"
 *   - parentName (optional) — the parent resource name; defaults to orgName
 *   - returnTo (optional) — post-create redirect (validated by resolveReturnTo)
 *
 * On success navigates to `resolveReturnTo(search.returnTo, fallback)` where
 * fallback is derived from the parent context.
 *
 * Replaces CreateFolderDialog modal (HOL-870).
 */

import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useCreateFolder } from '@/queries/folders'
import { ParentType } from '@/gen/holos/console/v1/folders_pb'
import { toSlug } from '@/lib/slug'
import { resolveReturnTo } from '@/lib/return-to'

export const Route = createFileRoute('/_authenticated/folder/new')({
  validateSearch: (
    search: Record<string, unknown>,
  ): {
    orgName?: string
    parentType?: string
    parentName?: string
    returnTo?: string
  } => ({
    orgName: typeof search.orgName === 'string' ? search.orgName : undefined,
    parentType: typeof search.parentType === 'string' ? search.parentType : undefined,
    parentName: typeof search.parentName === 'string' ? search.parentName : undefined,
    returnTo: typeof search.returnTo === 'string' ? search.returnTo : undefined,
  }),
  component: FolderNewRoute,
})

function FolderNewRoute() {
  const search = Route.useSearch()
  return (
    <FolderNewPage
      orgName={search.orgName}
      parentType={search.parentType}
      parentName={search.parentName}
      returnTo={search.returnTo}
    />
  )
}

export interface FolderNewPageProps {
  orgName?: string
  parentType?: string
  parentName?: string
  returnTo?: string
}

/** Resolve "Organization" | "Folder" string param → ParentType enum value */
function resolveParentType(value: string | undefined): ParentType {
  if (value === 'Folder') return ParentType.FOLDER
  return ParentType.ORGANIZATION
}

export function FolderNewPage({ orgName, parentType, parentName, returnTo }: FolderNewPageProps) {
  const navigate = useNavigate()

  const resolvedParentType = resolveParentType(parentType)
  const resolvedParentName = parentName ?? orgName ?? ''

  // Post-create and Cancel destination: honour returnTo, fall back to /organizations.
  const cancelTarget = resolveReturnTo(returnTo, '/organizations')

  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [nameEdited, setNameEdited] = useState(false)
  const [description, setDescription] = useState('')
  const [error, setError] = useState<string | null>(null)

  const { mutateAsync, isPending } = useCreateFolder(orgName ?? '')

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
      await mutateAsync({
        name,
        displayName,
        description,
        parentType: resolvedParentType,
        parentName: resolvedParentName,
      })
      const target = resolveReturnTo(returnTo, '/organizations')
      navigate({ to: target })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create folder')
    }
  }

  // Missing required orgName — show an explanatory error with a link back.
  if (!orgName) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>New Folder</CardTitle>
        </CardHeader>
        <CardContent>
          <Alert variant="destructive">
            <AlertDescription>
              An organization is required to create a folder.{' '}
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
        <CardTitle>New Folder</CardTitle>
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
              <Label htmlFor="folder-display-name">Display Name</Label>
              <Input
                id="folder-display-name"
                value={displayName}
                onChange={handleDisplayNameChange}
                placeholder="Payments Team"
                autoFocus
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="folder-name">Name</Label>
              <Input
                id="folder-name"
                value={name}
                onChange={handleNameChange}
                placeholder="payments"
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
              <Label htmlFor="folder-description">Description</Label>
              <Textarea
                id="folder-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Optional description"
              />
            </div>
            <div className="space-y-1 text-sm text-muted-foreground">
              <p>
                Organization: <span className="font-medium text-foreground">{orgName}</span>
              </p>
              <p>
                Parent:{' '}
                <span className="font-medium text-foreground">
                  {resolvedParentType === ParentType.FOLDER ? 'Folder' : 'Organization'} /{' '}
                  {resolvedParentName}
                </span>
              </p>
            </div>
          </div>
          <div className="flex items-center gap-3 pt-4">
            <Button type="submit" disabled={isPending || !name}>
              {isPending ? 'Creating…' : 'Create Folder'}
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
