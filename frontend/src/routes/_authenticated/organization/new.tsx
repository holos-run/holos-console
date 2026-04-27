/**
 * /organization/new — dedicated page for creating a new organization.
 *
 * On success navigates to `resolveReturnTo(search.returnTo, '/organizations/$name/projects')`.
 * This routes the user to the newly created org's projects page (HOL-977).
 * The Cancel button falls back to '/organizations'.
 *
 * Replaces the CreateOrgDialog modal (HOL-869).
 */

import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Checkbox } from '@/components/ui/checkbox'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { Info } from 'lucide-react'
import { useCreateOrganization } from '@/queries/organizations'
import { toSlug } from '@/lib/slug'
import { resolveReturnTo } from '@/lib/return-to'
import { connectErrorMessage } from '@/lib/connect-toast'

// No orgName search param: organizations have no parent entity, so callers
// pass only returnTo. This is an intentional omission — see HOL-934 audit.
export const Route = createFileRoute('/_authenticated/organization/new')({
  validateSearch: (search: Record<string, unknown>): { returnTo?: string } => ({
    returnTo: typeof search.returnTo === 'string' ? search.returnTo : undefined,
  }),
  component: OrganizationNewRoute,
})

function OrganizationNewRoute() {
  const search = Route.useSearch()
  return <OrganizationNewPage returnTo={search.returnTo} />
}

export function OrganizationNewPage({ returnTo }: { returnTo?: string }) {
  const navigate = useNavigate()
  const cancelTarget = resolveReturnTo(returnTo, '/organizations')

  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [nameEdited, setNameEdited] = useState(false)
  const [description, setDescription] = useState('')
  const [populateDefaults, setPopulateDefaults] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const { mutateAsync, isPending } = useCreateOrganization()

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
    if (!name) return
    setError(null)
    try {
      await mutateAsync({
        name,
        displayName,
        description,
        ...(populateDefaults ? { populateDefaults: true } : {}),
      })
      // Default fallback: navigate to the newly created org's home page.
      const fallback = `/organizations/${name}/projects`
      const target = resolveReturnTo(returnTo, fallback)
      navigate({ to: target })
    } catch (err) {
      setError(connectErrorMessage(err))
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>New Organization</CardTitle>
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
              <Label htmlFor="org-display-name">Display Name</Label>
              <Input
                id="org-display-name"
                value={displayName}
                onChange={handleDisplayNameChange}
                placeholder="My Organization"
                autoFocus
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="org-name">Name</Label>
              <Input
                id="org-name"
                value={name}
                onChange={handleNameChange}
                placeholder="my-org"
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
              <Label htmlFor="org-description">Description</Label>
              <Textarea
                id="org-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Optional description"
              />
            </div>
            <div className="flex items-center gap-2">
              <Checkbox
                id="populate-defaults"
                checked={populateDefaults}
                onCheckedChange={(checked) => setPopulateDefaults(checked === true)}
              />
              <Label htmlFor="populate-defaults" className="text-sm cursor-pointer">
                Populate with example resources
              </Label>
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Info className="h-4 w-4 text-muted-foreground cursor-default" />
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Creates a default folder and project structure with example templates at each level, including an org-level HTTPRoute platform template and a project-level deployment template.</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            </div>
          </div>
          <div className="flex items-center gap-3 pt-4">
            <Button type="submit" disabled={isPending || !name}>
              {isPending ? 'Creating…' : 'Create Organization'}
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
