import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Check, Pencil, X, Table2, Braces } from 'lucide-react'
import { ViewModeToggle } from '@/components/view-mode-toggle'
import { RawView } from '@/components/raw-view'
import { useGetFolder, useGetFolderRaw, useUpdateFolder } from '@/queries/folders'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'

export const Route = createFileRoute(
  '/_authenticated/orgs/$orgName/folders/$folderName/',
)({
  component: FolderDetailRoute,
})

function FolderDetailRoute() {
  const { orgName, folderName } = Route.useParams()
  return <FolderDetailPage orgName={orgName} folderName={folderName} />
}

export function FolderDetailPage({
  orgName: propOrgName,
  folderName: propFolderName,
}: { orgName?: string; folderName?: string } = {}) {
  let routeParams: { orgName?: string; folderName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const orgName = propOrgName ?? routeParams.orgName ?? ''
  const folderName = propFolderName ?? routeParams.folderName ?? ''

  let navigate: ReturnType<typeof useNavigate> | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    navigate = useNavigate()
  } catch {
    navigate = undefined
  }

  const { data: folder, isPending, error } = useGetFolder(orgName, folderName)
  const { data: org } = useGetOrganization(orgName)
  const updateMutation = useUpdateFolder(orgName, folderName)

  // View mode: data or raw
  const [viewMode, setViewMode] = useState<'data' | 'raw'>('data')
  const { data: rawJson } = useGetFolderRaw(orgName, folderName)
  const [includeAllFields, setIncludeAllFields] = useState(false)

  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

  // Display Name inline edit
  const [editingDisplayName, setEditingDisplayName] = useState(false)
  const [draftDisplayName, setDraftDisplayName] = useState('')

  // Description inline edit
  const [editingDescription, setEditingDescription] = useState(false)
  const [draftDescription, setDraftDescription] = useState('')

  // Delete dialog
  const [deleteOpen, setDeleteOpen] = useState(false)

  const handleSaveDisplayName = async () => {
    try {
      await updateMutation.mutateAsync({ displayName: draftDisplayName })
      setEditingDisplayName(false)
      toast.success('Saved')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  const handleSaveDescription = async () => {
    try {
      await updateMutation.mutateAsync({ description: draftDescription })
      setEditingDescription(false)
      toast.success('Saved')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  if (isPending) {
    return (
      <Card>
        <CardContent className="pt-6 space-y-4">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-8 w-full" />
        </CardContent>
      </Card>
    )
  }

  if (error) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive">
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  const displayName = folder?.displayName ?? ''
  const description = folder?.description ?? ''
  const creatorEmail = folder?.creatorEmail ?? ''

  return (
    <>
      <Card>
        <CardContent className="pt-6 space-y-6">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-muted-foreground">
                <Link to="/orgs/$orgName/settings" params={{ orgName }} className="hover:underline">
                  {orgName}
                </Link>
                {' / '}
                <Link to="/orgs/$orgName/folders" params={{ orgName }} className="hover:underline">
                  Folders
                </Link>
                {` / ${folderName}`}
              </p>
              <h2 className="text-xl font-semibold mt-1">{displayName || folderName}</h2>
            </div>
            <ViewModeToggle
              value={viewMode}
              onValueChange={(v) => setViewMode(v as 'data' | 'raw')}
              options={[
                { value: 'data', label: 'Data', icon: <Table2 className="h-3.5 w-3.5" /> },
                { value: 'raw', label: 'Resource', icon: <Braces className="h-3.5 w-3.5" /> },
              ]}
            />
          </div>

          {viewMode === 'raw' && rawJson && (
            <RawView
              raw={rawJson}
              includeAllFields={includeAllFields}
              onToggleIncludeAllFields={() => setIncludeAllFields(!includeAllFields)}
            />
          )}

          {viewMode === 'data' && (
          <>
          <div className="space-y-4">
            <h3 className="text-sm font-medium">General</h3>
            <Separator />

            {/* Display Name */}
            <div className="flex items-center gap-2">
              <span className="w-32 text-sm text-muted-foreground shrink-0">Display Name</span>
              {editingDisplayName ? (
                <>
                  <Input
                    autoFocus
                    aria-label="display name"
                    value={draftDisplayName}
                    onChange={(e) => setDraftDisplayName(e.target.value)}
                    className="flex-1"
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') handleSaveDisplayName()
                    }}
                  />
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="save display name"
                    onClick={handleSaveDisplayName}
                    disabled={updateMutation.isPending}
                  >
                    <Check className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="cancel display name"
                    onClick={() => setEditingDisplayName(false)}
                  >
                    <X className="h-4 w-4" />
                  </Button>
                </>
              ) : (
                <>
                  <span className="flex-1 text-sm">
                    {displayName || <span className="text-muted-foreground">No display name</span>}
                  </span>
                  {canWrite && (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="edit display name"
                      onClick={() => {
                        setDraftDisplayName(displayName)
                        setEditingDisplayName(true)
                      }}
                    >
                      <Pencil className="h-4 w-4" />
                    </Button>
                  )}
                </>
              )}
            </div>

            {/* Name (slug) - read-only */}
            <div className="flex items-center gap-2">
              <span className="w-32 text-sm text-muted-foreground shrink-0">Name (slug)</span>
              <span className="flex-1 text-sm font-mono">{folderName}</span>
            </div>

            {/* Organization */}
            <div className="flex items-center gap-2">
              <span className="w-32 text-sm text-muted-foreground shrink-0">Organization</span>
              <span className="flex-1 text-sm font-mono">{orgName}</span>
            </div>

            {/* Creator */}
            {creatorEmail && (
              <div className="flex items-center gap-2">
                <span className="w-32 text-sm text-muted-foreground shrink-0">Creator</span>
                <span className="flex-1 text-sm">{creatorEmail}</span>
              </div>
            )}

            {/* Description */}
            <div className="flex items-start gap-2">
              <span className="w-32 text-sm text-muted-foreground shrink-0 pt-2">Description</span>
              {editingDescription ? (
                <>
                  <Textarea
                    autoFocus
                    aria-label="description"
                    value={draftDescription}
                    onChange={(e) => setDraftDescription(e.target.value)}
                    className="flex-1"
                  />
                  <div className="flex flex-col gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="save description"
                      onClick={handleSaveDescription}
                      disabled={updateMutation.isPending}
                    >
                      <Check className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="cancel description"
                      onClick={() => setEditingDescription(false)}
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  </div>
                </>
              ) : (
                <>
                  <span className={`flex-1 text-sm ${description ? '' : 'text-muted-foreground'}`}>
                    {description || 'No description'}
                  </span>
                  {canWrite && (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="edit description"
                      onClick={() => {
                        setDraftDescription(description)
                        setEditingDescription(true)
                      }}
                    >
                      <Pencil className="h-4 w-4" />
                    </Button>
                  )}
                </>
              )}
            </div>
          </div>

          {/* Platform Templates section */}
          <div className="space-y-4">
            <h3 className="text-sm font-medium">Platform Templates</h3>
            <Separator />
            <p className="text-sm text-muted-foreground">
              Platform templates authored at this folder scope are applied to projects in this folder.
            </p>
            <Link
              to="/orgs/$orgName/folders/$folderName/templates"
              params={{ orgName, folderName }}
              aria-label="Folder Platform Templates"
              className="flex items-center justify-between p-3 rounded-md border border-border hover:bg-muted transition-colors"
            >
              <span className="text-sm">Manage Folder Platform Templates</span>
            </Link>
          </div>

          {/* Danger Zone */}
          {canWrite && (
            <div className="space-y-4">
              <h3 className="text-sm font-medium text-destructive">Danger Zone</h3>
              <Separator />
              <Button variant="destructive" onClick={() => setDeleteOpen(true)}>
                Delete Folder
              </Button>
            </div>
          )}
          </>
          )}
        </CardContent>
      </Card>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Folder</DialogTitle>
            <DialogDescription>
              This will permanently delete the folder &quot;{folderName}&quot;. This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={async () => {
                try {
                  setDeleteOpen(false)
                  navigate?.({
                    to: '/orgs/$orgName/folders',
                    params: { orgName },
                  })
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : String(err))
                }
              }}
            >
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
