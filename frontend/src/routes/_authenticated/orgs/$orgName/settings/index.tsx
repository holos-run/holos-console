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
import { Check, Pencil, X, Table2, Braces, ChevronRight } from 'lucide-react'
import { SharingPanel, type Grant } from '@/components/sharing-panel'
import { ViewModeToggle } from '@/components/view-mode-toggle'
import { RawView } from '@/components/raw-view'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import {
  useGetOrganization,
  useGetOrganizationRaw,
  useUpdateOrganization,
  useUpdateOrganizationSharing,
  useUpdateOrganizationDefaultSharing,
  useDeleteOrganization,
} from '@/queries/organizations'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/settings/')({
  component: OrgSettingsRoute,
})

function OrgSettingsRoute() {
  const { orgName } = Route.useParams()
  return <OrgSettingsPage orgName={orgName} />
}

export function OrgSettingsPage({ orgName: propOrgName }: { orgName?: string } = {}) {
  // Support both direct prop (for tests) and route params
  let routeOrgName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeOrgName = Route.useParams().orgName
  } catch {
    routeOrgName = undefined
  }
  const orgName = propOrgName ?? routeOrgName ?? ''

  const navigate = useNavigate()
  const { data: org, isPending, error } = useGetOrganization(orgName)
  const updateOrganization = useUpdateOrganization()
  const updateOrganizationSharing = useUpdateOrganizationSharing()
  const updateOrganizationDefaultSharing = useUpdateOrganizationDefaultSharing()
  const deleteOrganization = useDeleteOrganization()

  // View mode: data or raw
  const [viewMode, setViewMode] = useState<'data' | 'raw'>('data')
  const { data: rawJson } = useGetOrganizationRaw(orgName)
  const [includeAllFields, setIncludeAllFields] = useState(false)

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
      await updateOrganization.mutateAsync({ name: orgName, displayName: draftDisplayName })
      setEditingDisplayName(false)
      toast.success('Saved')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  const handleSaveDescription = async () => {
    try {
      await updateOrganization.mutateAsync({ name: orgName, description: draftDescription })
      setEditingDescription(false)
      toast.success('Saved')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  const handleSaveSharing = async (userGrants: Grant[], roleGrants: Grant[]) => {
    await updateOrganizationSharing.mutateAsync({ name: orgName, userGrants, roleGrants })
  }

  const handleSaveDefaultSharing = async (defaultUserGrants: Grant[], defaultRoleGrants: Grant[]) => {
    await updateOrganizationDefaultSharing.mutateAsync({ name: orgName, defaultUserGrants, defaultRoleGrants })
  }

  const handleDelete = async () => {
    try {
      await deleteOrganization.mutateAsync({ name: orgName })
      setDeleteOpen(false)
      navigate({ to: '/' })
    } catch { /* error shown via mutation */ }
  }

  const isOwner = org?.userRole === Role.OWNER

  if (isPending) {
    return (
      <Card>
        <CardContent className="pt-6 space-y-4">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-8 w-full" />
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

  const displayName = org?.displayName ?? ''
  const description = org?.description ?? ''
  const creatorEmail = org?.creatorEmail ?? ''
  const createdAt = org?.createdAt ?? ''
  const createdAtFormatted = createdAt ? new Date(createdAt).toLocaleString() : 'Unknown'
  const userGrants = (org?.userGrants ?? []) as Grant[]
  const roleGrants = (org?.roleGrants ?? []) as Grant[]
  const defaultUserGrants = (org?.defaultUserGrants ?? []) as Grant[]
  const defaultRoleGrants = (org?.defaultRoleGrants ?? []) as Grant[]

  return (
    <Card>
      <CardContent className="pt-6 space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-muted-foreground">{orgName} / Settings</p>
            <h2 className="text-xl font-semibold mt-1">Settings</h2>
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
            {/* General section */}
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
                      onKeyDown={(e) => { if (e.key === 'Enter') handleSaveDisplayName() }}
                    />
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="save display name"
                      onClick={handleSaveDisplayName}
                      disabled={updateOrganization.isPending}
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
                    <span className="flex-1 text-sm">{displayName || <span className="text-muted-foreground">No display name</span>}</span>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="edit display name"
                      onClick={() => { setDraftDisplayName(displayName); setEditingDisplayName(true) }}
                    >
                      <Pencil className="h-4 w-4" />
                    </Button>
                  </>
                )}
              </div>

              {/* Name (slug) - read-only */}
              <div className="flex items-center gap-2">
                <span className="w-32 text-sm text-muted-foreground shrink-0">Name (slug)</span>
                <span className="flex-1 text-sm font-mono">{orgName}</span>
              </div>

              {/* Creator - read-only */}
              <div className="flex items-center gap-2">
                <span className="w-32 text-sm text-muted-foreground shrink-0">Creator</span>
                <span className="flex-1 text-sm">{creatorEmail || 'Unknown'}</span>
              </div>

              {/* Created - read-only */}
              <div className="flex items-center gap-2">
                <span className="w-32 text-sm text-muted-foreground shrink-0">Created</span>
                <span className="flex-1 text-sm">{createdAtFormatted}</span>
              </div>

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
                        disabled={updateOrganization.isPending}
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
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="edit description"
                      onClick={() => { setDraftDescription(description); setEditingDescription(true) }}
                    >
                      <Pencil className="h-4 w-4" />
                    </Button>
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
              isSaving={updateOrganizationSharing.isPending}
            />

            {/* Default Sharing section */}
            <SharingPanel
              title="Default Sharing"
              description="These grants are automatically applied to every new project and secret created in this organization."
              userGrants={defaultUserGrants}
              roleGrants={defaultRoleGrants}
              isOwner={isOwner}
              onSave={handleSaveDefaultSharing}
              isSaving={updateOrganizationDefaultSharing.isPending}
            />

            {/* System Templates section */}
            <div className="space-y-4">
              <h3 className="text-sm font-medium">System Templates</h3>
              <Separator />
              <p className="text-sm text-muted-foreground">
                Platform templates are automatically applied to new projects in this organization.
              </p>
              <Link
                to="/orgs/$orgName/settings/system-templates"
                params={{ orgName }}
                aria-label="System Templates"
                className="flex items-center justify-between p-3 rounded-md border border-border hover:bg-muted transition-colors"
              >
                <span className="text-sm">Manage System Templates</span>
                <ChevronRight className="h-4 w-4 text-muted-foreground" />
              </Link>
            </div>

            {/* Danger Zone */}
            {isOwner && (
              <div className="space-y-4">
                <h3 className="text-sm font-medium text-destructive">Danger Zone</h3>
                <Separator />
                <Button
                  variant="destructive"
                  onClick={() => setDeleteOpen(true)}
                >
                  Delete Organization
                </Button>
              </div>
            )}
          </>
        )}
      </CardContent>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Organization</DialogTitle>
            <DialogDescription>
              This will permanently delete {orgName} and all its projects. This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {deleteOrganization.error && (
            <Alert variant="destructive">
              <AlertDescription>{deleteOrganization.error.message}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)}>Cancel</Button>
            <Button variant="destructive" onClick={handleDelete} disabled={deleteOrganization.isPending}>
              {deleteOrganization.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  )
}
