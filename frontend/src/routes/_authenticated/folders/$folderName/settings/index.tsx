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
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Combobox, type ComboboxItem } from '@/components/ui/combobox'
import { Check, Pencil, X, Table2, Braces } from 'lucide-react'
import { SharingPanel, type Grant } from '@/components/sharing-panel'
import { ViewModeToggle } from '@/components/view-mode-toggle'
import { RawView } from '@/components/raw-view'
import { useGetFolder, useGetFolderRaw, useUpdateFolder, useListFolders, useUpdateFolderSharing, useUpdateFolderDefaultSharing } from '@/queries/folders'
import { useGetOrganization } from '@/queries/organizations'
import { ParentType } from '@/gen/holos/console/v1/folders_pb'
import { Role } from '@/gen/holos/console/v1/rbac_pb'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/settings/',
)({
  component: FolderSettingsRoute,
})

function FolderSettingsRoute() {
  const { folderName } = Route.useParams()
  return <FolderDetailPage folderName={folderName} />
}

export function FolderDetailPage({
  orgName: propOrgName,
  folderName: propFolderName,
}: { orgName?: string; folderName?: string } = {}) {
  let routeParams: { folderName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const folderName = propFolderName ?? routeParams.folderName ?? ''

  let navigate: ReturnType<typeof useNavigate> | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    navigate = useNavigate()
  } catch {
    navigate = undefined
  }

  // Load folder first -- orgName is derived from the response
  const { data: folder, isPending, error } = useGetFolder(folderName)
  const orgName = propOrgName ?? folder?.organization ?? ''

  const { data: org } = useGetOrganization(orgName)
  const updateMutation = useUpdateFolder(orgName, folderName)
  const { data: allFolders } = useListFolders(orgName)
  const updateFolderSharing = useUpdateFolderSharing(orgName, folderName)
  const updateFolderDefaultSharing = useUpdateFolderDefaultSharing(orgName, folderName)

  // View mode: data or raw
  const [viewMode, setViewMode] = useState<'data' | 'raw'>('data')
  const { data: rawJson } = useGetFolderRaw(folderName, orgName)
  const [includeAllFields, setIncludeAllFields] = useState(false)

  const userRole = org?.userRole ?? Role.VIEWER
  const folderUserRole = folder?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  const isOwner = folderUserRole === Role.OWNER

  // Display Name inline edit
  const [editingDisplayName, setEditingDisplayName] = useState(false)
  const [draftDisplayName, setDraftDisplayName] = useState('')

  // Description inline edit
  const [editingDescription, setEditingDescription] = useState(false)
  const [draftDescription, setDraftDescription] = useState('')

  // Delete dialog
  const [deleteOpen, setDeleteOpen] = useState(false)

  // Parent picker
  const [parentPickerOpen, setParentPickerOpen] = useState(false)
  const [pendingParent, setPendingParent] = useState<{ type: ParentType; name: string; displayLabel: string } | null>(null)
  const [reparentDialogOpen, setReparentDialogOpen] = useState(false)

  // Build the set of descendant folder names (to exclude from parent options)
  const descendantNames = new Set<string>()
  if (allFolders) {
    // BFS: start from this folder, collect all children recursively
    const queue = [folderName]
    while (queue.length > 0) {
      const current = queue.shift()!
      for (const f of allFolders) {
        if (f.parentType === ParentType.FOLDER && f.parentName === current && !descendantNames.has(f.name)) {
          descendantNames.add(f.name)
          queue.push(f.name)
        }
      }
    }
  }

  // Depth helpers: mirrors backend computeFolderDepth / computeSubtreeDepth.
  // maxFolderDepth must match console/folders/handler.go:maxFolderDepth.
  const maxFolderDepth = 3

  // computeFolderDepth: count folder levels from a folder up to the org root.
  // Org root = 0, folder directly under org = 1, nested under another folder = 2, etc.
  const computeFolderDepth = (name: string): number => {
    if (!allFolders) return 0
    let depth = 0
    let current = name
    for (let i = 0; i <= maxFolderDepth; i++) {
      const f = allFolders.find((fld) => fld.name === current)
      if (!f) return depth
      depth++ // count this folder as a level
      if (f.parentType !== ParentType.FOLDER) return depth
      current = f.parentName
    }
    return depth
  }

  // computeSubtreeDepth: max folder depth below and including a folder. Leaf = 1.
  const computeSubtreeDepth = (name: string): number => {
    if (!allFolders) return 1
    const children = allFolders.filter(
      (f) => f.parentType === ParentType.FOLDER && f.parentName === name,
    )
    if (children.length === 0) return 1
    return 1 + Math.max(...children.map((c) => computeSubtreeDepth(c.name)))
  }

  const subtreeDepth = computeSubtreeDepth(folderName)

  // Build parent picker options: org root + folders that pass all filters:
  // not self, not a descendant, and would not exceed maxFolderDepth.
  const parentOptions: ComboboxItem[] = [
    { value: `org:${orgName}`, label: `${org?.displayName || orgName} (organization root)` },
    ...(allFolders ?? [])
      .filter((f) => {
        if (f.name === folderName || descendantNames.has(f.name)) return false
        // Depth check: the folder being moved (with its subtree) would sit
        // under this candidate. candidateDepth counts folder levels above
        // the candidate; placing a subtree of subtreeDepth under it yields
        // candidateDepth + subtreeDepth total depth.
        const candidateDepth = computeFolderDepth(f.name)
        return candidateDepth + subtreeDepth <= maxFolderDepth
      })
      .map((f) => ({ value: `folder:${f.name}`, label: f.displayName || f.name })),
  ]

  // Resolve the current parent display text
  const currentParentDisplay = (() => {
    if (!folder) return ''
    if (folder.parentType === ParentType.ORGANIZATION) {
      return `Organization: ${org?.displayName || folder.parentName}`
    }
    if (folder.parentType === ParentType.FOLDER) {
      const parentFolder = allFolders?.find((f) => f.name === folder.parentName)
      return `Folder: ${parentFolder?.displayName || folder.parentName}`
    }
    return folder.parentName
  })()

  const handleParentSelect = (comboValue: string) => {
    let type: ParentType
    let name: string
    let displayLabel: string
    if (comboValue.startsWith('org:')) {
      type = ParentType.ORGANIZATION
      name = comboValue.slice(4)
      displayLabel = org?.displayName || name
    } else {
      type = ParentType.FOLDER
      name = comboValue.slice(7)
      const f = allFolders?.find((fld) => fld.name === name)
      displayLabel = f?.displayName || name
    }
    // Only show confirmation if the parent is actually changing
    if (type === folder?.parentType && name === folder?.parentName) {
      setParentPickerOpen(false)
      return
    }
    setPendingParent({ type, name, displayLabel })
    setReparentDialogOpen(true)
  }

  const handleConfirmReparent = async () => {
    if (!pendingParent) return
    try {
      await updateMutation.mutateAsync({ parentType: pendingParent.type, parentName: pendingParent.name })
      setReparentDialogOpen(false)
      setParentPickerOpen(false)
      setPendingParent(null)
      toast.success('Parent changed')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

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

  const handleSaveSharing = async (userGrants: Grant[], roleGrants: Grant[]) => {
    await updateFolderSharing.mutateAsync({ userGrants, roleGrants })
  }

  const handleSaveDefaultSharing = async (defaultUserGrants: Grant[], defaultRoleGrants: Grant[]) => {
    await updateFolderDefaultSharing.mutateAsync({ defaultUserGrants, defaultRoleGrants })
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
  const userGrants = (folder?.userGrants ?? []) as Grant[]
  const roleGrants = (folder?.roleGrants ?? []) as Grant[]
  const defaultUserGrants = (folder?.defaultUserGrants ?? []) as Grant[]
  const defaultRoleGrants = (folder?.defaultRoleGrants ?? []) as Grant[]

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
                {' / '}
                <Link to="/folders/$folderName/settings" params={{ folderName }} className="hover:underline">
                  {folderName}
                </Link>
                {' / Settings'}
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

            {/* Parent */}
            <div className="flex items-center gap-2">
              <span className="w-32 text-sm text-muted-foreground shrink-0">Parent</span>
              {parentPickerOpen ? (
                <div className="flex-1 flex items-center gap-2">
                  <Combobox
                    items={parentOptions}
                    value={
                      folder?.parentType === ParentType.ORGANIZATION
                        ? `org:${folder.parentName}`
                        : `folder:${folder?.parentName ?? ''}`
                    }
                    onValueChange={handleParentSelect}
                    placeholder="Select parent..."
                    searchPlaceholder="Search folders..."
                    aria-label="parent picker"
                    className="flex-1"
                  />
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="cancel parent change"
                    onClick={() => setParentPickerOpen(false)}
                  >
                    <X className="h-4 w-4" />
                  </Button>
                </div>
              ) : (
                <>
                  <span className="flex-1 text-sm">{currentParentDisplay}</span>
                  {isOwner && (
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label="change parent"
                      onClick={() => setParentPickerOpen(true)}
                    >
                      Change Parent
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

          {/* Sharing section */}
          <SharingPanel
            userGrants={userGrants}
            roleGrants={roleGrants}
            isOwner={isOwner}
            onSave={handleSaveSharing}
            isSaving={updateFolderSharing.isPending}
          />

          {/* Default Sharing section */}
          <SharingPanel
            title="Default Sharing"
            description="These grants are automatically applied to every new secret created in projects within this folder."
            userGrants={defaultUserGrants}
            roleGrants={defaultRoleGrants}
            isOwner={isOwner}
            onSave={handleSaveDefaultSharing}
            isSaving={updateFolderDefaultSharing.isPending}
          />

          {/* Platform Templates section */}
          <div className="space-y-4">
            <h3 className="text-sm font-medium">Platform Templates</h3>
            <Separator />
            <p className="text-sm text-muted-foreground">
              Platform templates authored at this folder scope are applied to projects in this folder.
            </p>
            <Link
              to="/folders/$folderName/templates"
              params={{ folderName }}
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

      <AlertDialog open={reparentDialogOpen} onOpenChange={setReparentDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Move folder?</AlertDialogTitle>
            <AlertDialogDescription>
              Moving this folder to {pendingParent?.displayLabel} will change permission inheritance for it and all its descendants. This cannot be undone automatically.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => { setReparentDialogOpen(false); setPendingParent(null) }}>
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleConfirmReparent} disabled={updateMutation.isPending}>
              Move
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
