import { useState, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Check, Pencil, X } from 'lucide-react'
import { SharingPanel, type Grant } from '@/components/sharing-panel'
import { RawView } from '@/components/raw-view'
import { useGetOrganization, useDeleteOrganization, useUpdateOrganization, useUpdateOrganizationSharing } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { OrganizationService } from '@/gen/holos/console/v1/organizations_pb.js'

export const Route = createFileRoute('/_authenticated/organizations/$organizationName')({
  component: OrganizationPage,
})

function OrganizationPage() {
  const { organizationName: name } = Route.useParams()
  const navigate = useNavigate()
  const transport = useTransport()

  const { data, isLoading, error } = useGetOrganization(name)
  const organization = data?.organization

  const deleteMutation = useDeleteOrganization()
  const updateMutation = useUpdateOrganization()
  const updateSharingMutation = useUpdateOrganizationSharing()

  const organizationsClient = useMemo(() => createClient(OrganizationService, transport), [transport])

  const [editingDisplayName, setEditingDisplayName] = useState(false)
  const [draftDisplayName, setDraftDisplayName] = useState('')
  const [editingDescription, setEditingDescription] = useState(false)
  const [draftDescription, setDraftDescription] = useState('')

  const [viewMode, setViewMode] = useState<'editor' | 'raw'>('editor')
  const [rawJson, setRawJson] = useState<string | null>(null)
  const [rawError, setRawError] = useState<Error | null>(null)
  const [includeAllFields, setIncludeAllFields] = useState(false)

  const [deleteOpen, setDeleteOpen] = useState(false)

  const [localDisplayName, setLocalDisplayName] = useState<string | null>(null)
  const [localDescription, setLocalDescription] = useState<string | null>(null)
  const [localOrganization, setLocalOrganization] = useState<typeof organization | null>(null)

  const effectiveOrg = localOrganization ?? organization
  const displayName = localDisplayName ?? effectiveOrg?.displayName
  const description = localDescription ?? effectiveOrg?.description

  const isOwner = effectiveOrg?.userRole === Role.OWNER
  const isEditorOrAbove = effectiveOrg != null && effectiveOrg.userRole >= Role.EDITOR

  const handleSaveDisplayName = async (newDisplayName: string) => {
    try {
      await updateMutation.mutateAsync({ name, displayName: newDisplayName })
      setLocalDisplayName(newDisplayName)
      setEditingDisplayName(false)
    } catch { /* keep editing on failure */ }
  }

  const handleSaveDescription = async (newDescription: string) => {
    try {
      await updateMutation.mutateAsync({ name, description: newDescription })
      setLocalDescription(newDescription)
      setEditingDescription(false)
    } catch { /* keep editing on failure */ }
  }

  const handleSaveSharing = async (newUserGrants: Grant[], newRoleGrants: Grant[]) => {
    const response = await updateSharingMutation.mutateAsync({
      name,
      userGrants: newUserGrants,
      roleGrants: newRoleGrants,
    })
    if (response.organization) setLocalOrganization(response.organization)
  }

  const handleViewModeChange = async (newMode: string) => {
    setViewMode(newMode as 'editor' | 'raw')
    if (newMode === 'raw' && rawJson === null) {
      try {
        const response = await organizationsClient.getOrganizationRaw({ name })
        setRawJson(response.raw)
      } catch (err) {
        setRawError(err instanceof Error ? err : new Error(String(err)))
      }
    }
  }

  const handleDelete = async () => {
    try {
      await deleteMutation.mutateAsync({ name })
      setDeleteOpen(false)
      navigate({ to: '/organizations' })
    } catch { /* error via mutation */ }
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

  const displayError = error || rawError
  if (displayError) {
    const msg = displayError.message.toLowerCase()
    let displayMessage = displayError.message
    if (msg.includes('not found') || msg.includes('not_found')) {
      displayMessage = `Organization "${name}" not found`
    } else if (msg.includes('permission') || msg.includes('denied')) {
      displayMessage = 'Permission denied: You are not authorized to view this organization'
    }
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive"><AlertDescription>{displayMessage}</AlertDescription></Alert>
        </CardContent>
      </Card>
    )
  }

  if (!effectiveOrg) return null

  return (
    <Card>
      <CardContent className="pt-6 space-y-4">
        <p className="text-sm text-muted-foreground">{effectiveOrg.name}</p>

        {/* Display Name */}
        <div className="flex items-center gap-2">
          {editingDisplayName ? (
            <>
              <Input
                autoFocus
                value={draftDisplayName}
                onChange={(e) => setDraftDisplayName(e.target.value)}
                placeholder="Display name"
                disabled={updateMutation.isPending}
                onKeyDown={(e) => { if (e.key === 'Enter') handleSaveDisplayName(draftDisplayName) }}
                className="flex-1"
              />
              <Button variant="ghost" size="icon" aria-label="save display name" onClick={() => handleSaveDisplayName(draftDisplayName)} disabled={updateMutation.isPending}>
                <Check className="h-4 w-4" />
              </Button>
              <Button variant="ghost" size="icon" aria-label="cancel editing display name" onClick={() => setEditingDisplayName(false)}>
                <X className="h-4 w-4" />
              </Button>
            </>
          ) : (
            <>
              <h2 className="text-xl font-semibold flex-1">{displayName || effectiveOrg.name}</h2>
              {isEditorOrAbove && (
                <Button variant="ghost" size="icon" aria-label="edit display name" onClick={() => { setDraftDisplayName(displayName || ''); setEditingDisplayName(true) }}>
                  <Pencil className="h-4 w-4" />
                </Button>
              )}
            </>
          )}
        </div>

        {/* Description */}
        <div className="flex items-center gap-2">
          {editingDescription ? (
            <>
              <Input
                autoFocus
                value={draftDescription}
                onChange={(e) => setDraftDescription(e.target.value)}
                placeholder="What is this organization for?"
                disabled={updateMutation.isPending}
                onKeyDown={(e) => { if (e.key === 'Enter') handleSaveDescription(draftDescription) }}
                className="flex-1"
              />
              <Button variant="ghost" size="icon" aria-label="save description" onClick={() => handleSaveDescription(draftDescription)} disabled={updateMutation.isPending}>
                <Check className="h-4 w-4" />
              </Button>
              <Button variant="ghost" size="icon" aria-label="cancel editing description" onClick={() => setEditingDescription(false)}>
                <X className="h-4 w-4" />
              </Button>
            </>
          ) : (
            <>
              <p className={`flex-1 text-sm ${description ? '' : 'text-muted-foreground'}`}>
                {description || 'No description'}
              </p>
              {isEditorOrAbove && (
                <Button variant="ghost" size="icon" aria-label="edit description" onClick={() => { setDraftDescription(description || ''); setEditingDescription(true) }}>
                  <Pencil className="h-4 w-4" />
                </Button>
              )}
            </>
          )}
        </div>

        <Tabs value={viewMode} onValueChange={handleViewModeChange}>
          <TabsList>
            <TabsTrigger value="editor">Editor</TabsTrigger>
            <TabsTrigger value="raw">Raw</TabsTrigger>
          </TabsList>
        </Tabs>

        {viewMode === 'raw' && rawJson && (
          <RawView raw={rawJson} includeAllFields={includeAllFields} onToggleIncludeAllFields={() => setIncludeAllFields((p) => !p)} />
        )}

        {viewMode === 'editor' && isOwner && (
          <div className="flex flex-col sm:flex-row gap-2">
            <Button variant="destructive" onClick={() => setDeleteOpen(true)}>
              Delete
            </Button>
          </div>
        )}

        <SharingPanel
          userGrants={(localOrganization ?? effectiveOrg).userGrants}
          roleGrants={(localOrganization ?? effectiveOrg).roleGrants}
          isOwner={isOwner}
          onSave={handleSaveSharing}
          isSaving={updateSharingMutation.isPending}
        />
      </CardContent>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Organization</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete organization &quot;{name}&quot;? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {deleteMutation.error && (
            <Alert variant="destructive"><AlertDescription>{deleteMutation.error.message}</AlertDescription></Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)}>Cancel</Button>
            <Button variant="destructive" onClick={handleDelete} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  )
}
