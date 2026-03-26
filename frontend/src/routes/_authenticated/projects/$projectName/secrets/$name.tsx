import { useState, useEffect, useMemo } from 'react'
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
import { Check, Pencil, X, ExternalLink } from 'lucide-react'
import { useAuth } from '@/lib/auth'
import { SecretDataGrid } from '@/components/secret-data-grid'
import { RawView } from '@/components/raw-view'
import { SharingPanel, type Grant } from '@/components/sharing-panel'
import { isSafeUrl } from '@/lib/utils'
import { useGetSecret, useGetSecretMetadata, useUpdateSecret, useUpdateSecretSharing, useDeleteSecret } from '@/queries/secrets'
import { SecretsService } from '@/gen/holos/console/v1/secrets_pb.js'
import type { ShareGrant } from '@/gen/holos/console/v1/secrets_pb.js'
import { isOwner as computeIsOwner } from '@/lib/isOwner'

export const Route = createFileRoute('/_authenticated/projects/$projectName/secrets/$name')({
  component: SecretPage,
})

function serializeData(data: Record<string, Uint8Array>): string {
  const sorted = Object.keys(data).sort()
  const obj: Record<string, string> = {}
  const decoder = new TextDecoder()
  for (const key of sorted) {
    obj[key] = decoder.decode(data[key])
  }
  return JSON.stringify(obj)
}

function SecretPage() {
  const { projectName, name } = Route.useParams()
  const navigate = useNavigate()
  const { user, isAuthenticated, isLoading: authLoading } = useAuth()
  const transport = useTransport()

  const { data: fetchedData, isLoading: dataLoading, error: dataError } = useGetSecret(projectName, name)
  const { data: metadata, isLoading: metaLoading } = useGetSecretMetadata(projectName, name)

  const updateMutation = useUpdateSecret(projectName)
  const updateSharingMutation = useUpdateSecretSharing(projectName)
  const deleteMutation = useDeleteSecret(projectName)

  const secretsClient = useMemo(() => createClient(SecretsService, transport), [transport])

  const [secretData, setSecretData] = useState<Record<string, Uint8Array> | null>(null)
  const [description, setDescription] = useState<string | null>(null)
  const [url, setUrl] = useState<string | null>(null)
  const [originalDataSerialized, setOriginalDataSerialized] = useState<string | null>(null)
  const [originalDescription, setOriginalDescription] = useState<string | null>(null)
  const [originalUrl, setOriginalUrl] = useState<string | null>(null)

  // Inline editing state
  const [editingDescription, setEditingDescription] = useState(false)
  const [draftDescription, setDraftDescription] = useState('')
  const [editingUrl, setEditingUrl] = useState(false)
  const [draftUrl, setDraftUrl] = useState('')

  // View mode
  const [editMode, setEditMode] = useState(false)
  const [viewMode, setViewMode] = useState<'editor' | 'raw'>('editor')
  const [rawJson, setRawJson] = useState<string | null>(null)
  const [rawError, setRawError] = useState<Error | null>(null)
  const [includeAllFields, setIncludeAllFields] = useState(false)

  // Delete
  const [deleteOpen, setDeleteOpen] = useState(false)

  // Save
  const [saveError, setSaveError] = useState<string | null>(null)

  // Sharing state from metadata
  const [localUserGrants, setLocalUserGrants] = useState<ShareGrant[] | null>(null)
  const [localRoleGrants, setLocalRoleGrants] = useState<ShareGrant[] | null>(null)

  // Initialize local state from fetched data
  useEffect(() => {
    if (fetchedData && originalDataSerialized === null) {
      setOriginalDataSerialized(serializeData(fetchedData))
      if (secretData === null) setSecretData(fetchedData)
    }
  }, [fetchedData, originalDataSerialized, secretData])

  useEffect(() => {
    if (metadata && originalDescription === null) {
      setOriginalDescription(metadata.description ?? '')
      if (description === null) setDescription(metadata.description ?? '')
    }
    if (metadata && originalUrl === null) {
      setOriginalUrl(metadata.url ?? '')
      if (url === null) setUrl(metadata.url ?? '')
    }
  }, [metadata, originalDescription, description, originalUrl, url])

  const effectiveData = secretData ?? fetchedData ?? {}
  const effectiveDescription = description ?? metadata?.description ?? ''
  const effectiveUrl = url ?? metadata?.url ?? ''
  const effectiveUserGrants = localUserGrants ?? metadata?.userGrants ?? []
  const effectiveRoleGrants = localRoleGrants ?? metadata?.roleGrants ?? []

  const isDirty =
    originalDataSerialized !== null &&
    (serializeData(effectiveData) !== originalDataSerialized ||
     effectiveDescription !== (originalDescription ?? '') ||
     effectiveUrl !== (originalUrl ?? ''))

  const userEmail = user?.profile?.email as string | undefined
  const userGroups = Array.isArray((user?.profile as Record<string, unknown> | undefined)?.groups)
    ? ((user!.profile as Record<string, unknown>).groups as string[])
    : []
  const isOwner = computeIsOwner(userEmail, userGroups, effectiveUserGrants, effectiveRoleGrants)

  const handleSaveSharing = async (newUserGrants: Grant[], newRoleGrants: Grant[]) => {
    const response = await updateSharingMutation.mutateAsync({
      name,
      userGrants: newUserGrants,
      roleGrants: newRoleGrants,
    })
    if (response.metadata) {
      setLocalUserGrants(response.metadata.userGrants)
      setLocalRoleGrants(response.metadata.roleGrants)
    }
  }

  const handleViewModeChange = async (newMode: string) => {
    setViewMode(newMode as 'editor' | 'raw')
    if (newMode === 'raw' && rawJson === null) {
      try {
        const response = await secretsClient.getSecretRaw({ name, project: projectName })
        setRawJson(response.raw)
      } catch (err) {
        setRawError(err instanceof Error ? err : new Error(String(err)))
      }
    }
  }

  const handleSave = async () => {
    if (!isDirty) return
    setSaveError(null)
    try {
      await updateMutation.mutateAsync({
        name,
        data: effectiveData,
        description: effectiveDescription,
        url: effectiveUrl,
      })
      setOriginalDataSerialized(serializeData(effectiveData))
      setOriginalDescription(effectiveDescription)
      setOriginalUrl(effectiveUrl)
      setRawJson(null)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleCancel = () => {
    setEditMode(false)
    setSaveError(null)
    // Revert data changes
    if (fetchedData) setSecretData(fetchedData)
    if (originalDescription !== null) setDescription(originalDescription)
    if (originalUrl !== null) setUrl(originalUrl)
  }

  const handleDelete = async () => {
    try {
      await deleteMutation.mutateAsync(name)
      setDeleteOpen(false)
      navigate({ to: '/projects/$projectName/secrets', params: { projectName } })
    } catch { /* error via mutation */ }
  }

  const isLoading = dataLoading || metaLoading

  if (authLoading || (isAuthenticated && isLoading)) {
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

  const displayError = dataError || rawError
  if (displayError) {
    const msg = displayError.message.toLowerCase()
    let displayMessage = displayError.message
    if (msg.includes('not found') || msg.includes('not_found')) {
      displayMessage = `Secret "${name}" not found`
    } else if (msg.includes('permission') || msg.includes('denied')) {
      displayMessage = 'Permission denied: You are not authorized to view this secret'
    }
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive"><AlertDescription>{displayMessage}</AlertDescription></Alert>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardContent className="pt-6 space-y-4">
        <p className="text-sm text-muted-foreground">{projectName} / Secrets</p>
        <h2 className="text-xl font-semibold">{name}</h2>

        {/* Description */}
        <div className="flex items-center gap-2">
          {editingDescription ? (
            <>
              <Input
                autoFocus
                value={draftDescription}
                onChange={(e) => setDraftDescription(e.target.value)}
                placeholder="What is this secret used for?"
                onKeyDown={(e) => { if (e.key === 'Enter') { setDescription(draftDescription); setEditingDescription(false) } }}
                className="flex-1"
              />
              <Button variant="ghost" size="icon" aria-label="save description" onClick={() => { setDescription(draftDescription); setEditingDescription(false) }}>
                <Check className="h-4 w-4" />
              </Button>
              <Button variant="ghost" size="icon" aria-label="cancel editing description" onClick={() => setEditingDescription(false)}>
                <X className="h-4 w-4" />
              </Button>
            </>
          ) : (
            <>
              <p className={`flex-1 text-sm ${effectiveDescription ? '' : 'text-muted-foreground'}`}>
                {effectiveDescription || 'No description'}
              </p>
              <Button variant="ghost" size="icon" aria-label="edit description" onClick={() => { setDraftDescription(effectiveDescription); setEditingDescription(true) }}>
                <Pencil className="h-4 w-4" />
              </Button>
            </>
          )}
        </div>

        {/* URL */}
        <div className="flex items-center gap-2">
          {editingUrl ? (
            <>
              <Input
                autoFocus
                value={draftUrl}
                onChange={(e) => setDraftUrl(e.target.value)}
                placeholder="https://example.com/service"
                onKeyDown={(e) => { if (e.key === 'Enter') { setUrl(draftUrl); setEditingUrl(false) } }}
                className="flex-1"
              />
              <Button variant="ghost" size="icon" aria-label="save url" onClick={() => { setUrl(draftUrl); setEditingUrl(false) }}>
                <Check className="h-4 w-4" />
              </Button>
              <Button variant="ghost" size="icon" aria-label="cancel editing url" onClick={() => setEditingUrl(false)}>
                <X className="h-4 w-4" />
              </Button>
            </>
          ) : (
            <>
              {effectiveUrl ? (
                isSafeUrl(effectiveUrl) ? (
                  <a
                    href={effectiveUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex-1 text-sm text-primary hover:underline flex items-center gap-1"
                  >
                    <ExternalLink className="h-3 w-3" />
                    {effectiveUrl}
                  </a>
                ) : (
                  <p className="flex-1 text-sm text-muted-foreground">{effectiveUrl}</p>
                )
              ) : (
                <p className="flex-1 text-sm text-muted-foreground">No URL</p>
              )}
              <Button variant="ghost" size="icon" aria-label="edit url" onClick={() => { setDraftUrl(effectiveUrl); setEditingUrl(true) }}>
                <Pencil className="h-4 w-4" />
              </Button>
            </>
          )}
        </div>

        <Tabs value={viewMode} onValueChange={handleViewModeChange}>
          <TabsList>
            <TabsTrigger value="editor">Editor</TabsTrigger>
            <TabsTrigger value="raw">Raw</TabsTrigger>
          </TabsList>
        </Tabs>

        {viewMode === 'editor' && (
          <>
            <div className="flex items-center gap-2">
              {editMode ? (
                <>
                  <Button size="sm" onClick={handleSave} disabled={!isDirty || updateMutation.isPending}>
                    {updateMutation.isPending ? 'Saving...' : 'Save'}
                  </Button>
                  <Button variant="outline" size="sm" onClick={handleCancel}>
                    Cancel
                  </Button>
                </>
              ) : (
                <Button variant="outline" size="sm" onClick={() => setEditMode(true)}>
                  <Pencil className="h-4 w-4 mr-1" />
                  Edit
                </Button>
              )}
              <div className="flex-1" />
              <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>Delete</Button>
            </div>
            {saveError && (
              <Alert variant="destructive"><AlertDescription>{saveError}</AlertDescription></Alert>
            )}
            <SecretDataGrid data={effectiveData} onChange={(newData) => setSecretData(newData)} readOnly={!editMode} />
          </>
        )}

        {viewMode === 'raw' && rawJson && (
          <>
            <div className="flex items-center gap-2">
              <div className="flex-1" />
              <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>Delete</Button>
            </div>
            <RawView raw={rawJson} includeAllFields={includeAllFields} onToggleIncludeAllFields={() => setIncludeAllFields((p) => !p)} />
          </>
        )}

        <SharingPanel
          userGrants={effectiveUserGrants}
          roleGrants={effectiveRoleGrants}
          isOwner={isOwner}
          onSave={handleSaveSharing}
          isSaving={updateSharingMutation.isPending}
        />
      </CardContent>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Secret</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete secret &quot;{name}&quot;? This action cannot be undone.
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
