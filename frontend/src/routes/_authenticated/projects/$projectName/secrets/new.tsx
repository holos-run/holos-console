/**
 * Secrets create route — hosts the inline secret creation form as a dedicated
 * page, consistent with the Deployments/Templates pattern (HOL-857).
 *
 * Removing the old inline dialog from the list page is out of scope for this
 * phase and queued for the sibling cleanup plan.
 */

import { useState, useEffect, useMemo } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Trash2 } from 'lucide-react'
import { SecretDataGrid } from '@/components/secret-data-grid'
import { useAuth } from '@/lib/auth'
import { useCreateSecret } from '@/queries/secrets'
import { useGetProject } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'

interface CreateGrant {
  principal: string
  role: Role
}

export const Route = createFileRoute('/_authenticated/projects/$projectName/secrets/new')({
  component: SecretNewRoute,
})

function SecretNewRoute() {
  const { projectName } = Route.useParams()
  return <SecretCreatePage projectName={projectName} />
}

export function SecretCreatePage({ projectName }: { projectName: string }) {
  const navigate = useNavigate()
  const { user } = useAuth()
  const { data: project } = useGetProject(projectName)

  const createMutation = useCreateSecret(projectName)

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [url, setUrl] = useState('')
  const [data, setData] = useState<Record<string, Uint8Array>>({})
  const [error, setError] = useState<string | null>(null)
  const [grantsInitialized, setGrantsInitialized] = useState(false)
  const [userGrants, setUserGrants] = useState<CreateGrant[]>([])
  const [roleGrants, setRoleGrants] = useState<CreateGrant[]>([])

  const creatorEmail = (user?.profile?.email as string) || ''
  const defaultUserGrants = useMemo(
    () => (project?.defaultUserGrants ?? []) as CreateGrant[],
    [project],
  )
  const defaultRoleGrants = useMemo(
    () => (project?.defaultRoleGrants ?? []) as CreateGrant[],
    [project],
  )
  const hasDefaults = defaultUserGrants.length > 0 || defaultRoleGrants.length > 0

  // Initialize grants once the project record loads. Using an effect (not a
  // useState lazy initializer) ensures we capture the real project defaults
  // rather than the undefined-on-first-render placeholder.
  useEffect(() => {
    if (grantsInitialized || !project) return
    const creatorGrant: CreateGrant = { principal: creatorEmail, role: Role.OWNER }
    const seenPrincipals = new Set([creatorEmail])
    const grants = [creatorGrant]
    for (const g of defaultUserGrants) {
      if (!seenPrincipals.has(g.principal)) {
        seenPrincipals.add(g.principal)
        grants.push({ principal: g.principal, role: g.role })
      }
    }
    setUserGrants(grants)
    setRoleGrants(defaultRoleGrants.map((g) => ({ principal: g.principal, role: g.role })))
    setGrantsInitialized(true)
  }, [project, grantsInitialized, creatorEmail, defaultUserGrants, defaultRoleGrants])

  const handleCreate = async () => {
    if (!name.trim()) {
      setError('Secret name is required')
      return
    }
    setError(null)
    try {
      await createMutation.mutateAsync({
        name: name.trim(),
        data,
        userGrants: userGrants.filter((g) => g.principal.trim() !== ''),
        roleGrants: roleGrants.filter((g) => g.principal.trim() !== ''),
        description: description.trim() || undefined,
        url: url.trim() || undefined,
      })
      await navigate({
        to: '/projects/$projectName/secrets',
        params: { projectName },
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Create Secret</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          <div>
            <Label>Name</Label>
            <Input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-secret"
            />
            <p className="text-xs text-muted-foreground mt-1">
              Lowercase alphanumeric and hyphens only
            </p>
          </div>
          <div>
            <Label>Description</Label>
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What is this secret used for?"
            />
          </div>
          <div>
            <Label>URL</Label>
            <Input
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://example.com/service"
            />
          </div>
          <div>
            <Label>Data</Label>
            <SecretDataGrid data={data} onChange={setData} />
          </div>
          <div>
            <Label>Sharing</Label>
            {hasDefaults && (
              <p className="text-xs text-muted-foreground mt-1">
                Pre-filled from project default sharing settings
              </p>
            )}
            <div className="mt-2 space-y-2">
              <p className="text-xs text-muted-foreground">Users</p>
              {userGrants.map((g, i) => (
                <div key={i} className="flex items-center gap-2">
                  <span className="text-sm flex-1">{g.principal}</span>
                  <Badge variant="outline">
                    {g.role === Role.OWNER
                      ? 'Owner'
                      : g.role === Role.EDITOR
                        ? 'Editor'
                        : 'Viewer'}
                  </Badge>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="remove"
                    onClick={() => setUserGrants(userGrants.filter((_, j) => j !== i))}
                  >
                    <Trash2 className="h-3 w-3" />
                  </Button>
                </div>
              ))}
              {roleGrants.length > 0 && (
                <>
                  <p className="text-xs text-muted-foreground">Roles</p>
                  {roleGrants.map((g, i) => (
                    <div key={i} className="flex items-center gap-2">
                      <span className="text-sm flex-1">{g.principal}</span>
                      <Badge variant="outline">
                        {g.role === Role.OWNER
                          ? 'Owner'
                          : g.role === Role.EDITOR
                            ? 'Editor'
                            : 'Viewer'}
                      </Badge>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label="remove"
                        onClick={() => setRoleGrants(roleGrants.filter((_, j) => j !== i))}
                      >
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    </div>
                  ))}
                </>
              )}
            </div>
          </div>
          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          <div className="flex items-center gap-3 pt-2">
            <Button onClick={handleCreate} disabled={createMutation.isPending}>
              {createMutation.isPending ? 'Creating...' : 'Create Secret'}
            </Button>
            <Link to="/projects/$projectName/secrets" params={{ projectName }}>
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
