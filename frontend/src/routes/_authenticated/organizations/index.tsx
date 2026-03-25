import { useState } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Trash2 } from 'lucide-react'
import { useAuth } from '@/lib/auth'
import { useListOrganizations, useDeleteOrganization, useCreateOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { slugify } from '@/utils/slugify'

export const Route = createFileRoute('/_authenticated/organizations/')({
  component: OrganizationsListPage,
})

function roleName(role: Role): string {
  switch (role) {
    case Role.OWNER: return 'Owner'
    case Role.EDITOR: return 'Editor'
    case Role.VIEWER: return 'Viewer'
    default: return 'None'
  }
}

function OrganizationsListPage() {
  const navigate = useNavigate()
  const { user } = useAuth()

  const { data, isLoading, error } = useListOrganizations()
  const organizations = data?.organizations ?? []

  const deleteMutation = useDeleteOrganization()
  const createMutation = useCreateOrganization()

  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createDisplayName, setCreateDisplayName] = useState('')
  const [createDescription, setCreateDescription] = useState('')
  const [createError, setCreateError] = useState<string | null>(null)
  const [nameManuallyEdited, setNameManuallyEdited] = useState(false)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

  const handleCreateOpen = () => {
    setCreateName('')
    setCreateDisplayName('')
    setCreateDescription('')
    setCreateError(null)
    setNameManuallyEdited(false)
    setCreateOpen(true)
  }

  const handleCreate = async () => {
    if (!createName.trim()) {
      setCreateError('Organization name is required')
      return
    }
    setCreateError(null)
    try {
      await createMutation.mutateAsync({
        name: createName.trim(),
        displayName: createDisplayName.trim(),
        description: createDescription.trim(),
        userGrants: [{ principal: (user?.profile?.email as string) || '', role: Role.OWNER }],
        roleGrants: [],
      })
      setCreateOpen(false)
      navigate({ to: '/organizations/$organizationName', params: { organizationName: createName.trim() } })
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return
    try {
      await deleteMutation.mutateAsync({ name: deleteTarget })
      setDeleteOpen(false)
      setDeleteTarget(null)
    } catch { /* dialog stays open; deleteMutation.error is shown in the dialog */ }
  }

  if (isLoading) {
    return (
      <Card>
        <CardContent className="pt-6">
          <div className="flex items-center gap-2">
            <div className="h-5 w-5 animate-spin rounded-full border-2 border-primary border-t-transparent" />
            <span>Loading...</span>
          </div>
        </CardContent>
      </Card>
    )
  }

  if (error) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive"><AlertDescription>{error.message}</AlertDescription></Alert>
        </CardContent>
      </Card>
    )
  }

  return (
    <>
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <CardTitle>Organizations</CardTitle>
          <Button size="sm" onClick={handleCreateOpen}>Create Organization</Button>
        </CardHeader>
        <CardContent>
          {organizations.length === 0 ? (
            <p className="text-muted-foreground">No organizations available.</p>
          ) : (
            <ul className="divide-y">
              {organizations.map((org) => (
                <li key={org.name} className="flex items-center gap-2 py-2">
                  <Link
                    to="/organizations/$organizationName"
                    params={{ organizationName: org.name }}
                    className="flex-1 min-w-0 hover:underline"
                  >
                    <div className="font-medium truncate">{org.displayName || org.name}</div>
                    <div className="text-sm text-muted-foreground truncate">{org.description || org.name}</div>
                  </Link>
                  <Badge variant="outline" className="shrink-0">{roleName(org.userRole)}</Badge>
                  {org.userRole === Role.OWNER && (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`delete ${org.name}`}
                      onClick={() => { setDeleteTarget(org.name); deleteMutation.reset(); setDeleteOpen(true) }}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create Organization</DialogTitle>
            <DialogDescription>Create a new organization. You will be added as the Owner.</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div>
              <Label>Display Name</Label>
              <Input
                autoFocus
                value={createDisplayName}
                onChange={(e) => {
                  setCreateDisplayName(e.target.value)
                  if (!nameManuallyEdited) setCreateName(slugify(e.target.value))
                }}
                placeholder="My Organization"
              />
            </div>
            <div>
              <Label>Name</Label>
              <Input
                value={createName}
                onChange={(e) => {
                  setCreateName(e.target.value)
                  setNameManuallyEdited(e.target.value !== '')
                }}
                placeholder="my-org"
              />
              <p className="text-xs text-muted-foreground mt-1">Lowercase alphanumeric and hyphens</p>
            </div>
            <div>
              <Label>Description</Label>
              <Input
                value={createDescription}
                onChange={(e) => setCreateDescription(e.target.value)}
                placeholder="What is this organization for?"
              />
            </div>
            {createError && (
              <Alert variant="destructive"><AlertDescription>{createError}</AlertDescription></Alert>
            )}
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button onClick={handleCreate} disabled={createMutation.isPending}>
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Organization</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete organization &quot;{deleteTarget}&quot;? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {deleteMutation.error && (
            <Alert variant="destructive"><AlertDescription>{deleteMutation.error.message}</AlertDescription></Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)}>Cancel</Button>
            <Button variant="destructive" onClick={handleDeleteConfirm} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
