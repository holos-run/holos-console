import { useMemo, useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Check, Pencil, X, Table2, Braces } from 'lucide-react'
import { SharingPanel, type Grant } from '@/components/sharing-panel'
import { ViewModeToggle } from '@/components/view-mode-toggle'
import { RawView } from '@/components/raw-view'
import { useGetProject, useUpdateProject, useUpdateProjectSharing, useUpdateProjectDefaultSharing, useDeleteProject } from '@/queries/projects'
import { useGetProjectSettings, useGetProjectSettingsRaw, useUpdateProjectSettings } from '@/queries/project-settings'
import { useGetOrganization } from '@/queries/organizations'
import { useResourcePermissions } from '@/queries/permissions'
import { connectErrorMessage } from '@/lib/connect-toast'
import {
  deleteNamespacePermission,
  hasPermission,
  updateNamespacePermission,
} from '@/lib/resource-permissions'
import { namespaceForOrg, namespaceForProject } from '@/lib/scope-labels'

export const Route = createFileRoute('/_authenticated/projects/$projectName/settings/')({
  component: ProjectSettingsRoute,
})

function ProjectSettingsRoute() {
  const { projectName } = Route.useParams()
  return <ProjectSettingsPage projectName={projectName} />
}

export function ProjectSettingsPage({ projectName: propProjectName }: { projectName?: string } = {}) {
  // Support both direct prop (for tests) and route params
  let routeProjectName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeProjectName = Route.useParams().projectName
  } catch {
    routeProjectName = undefined
  }
  const projectName = propProjectName ?? routeProjectName ?? ''

  const navigate = useNavigate()
  const { data: project, isPending, error } = useGetProject(projectName)
  const updateProject = useUpdateProject()
  const updateProjectSharing = useUpdateProjectSharing()
  const updateProjectDefaultSharing = useUpdateProjectDefaultSharing()
  const deleteProject = useDeleteProject()
  const { data: projectSettings } = useGetProjectSettings(projectName)
  const updateProjectSettings = useUpdateProjectSettings(projectName)

  // Fetch org data for display.
  const { data: org } = useGetOrganization(project?.organization ?? '')
  const projectNamespace = namespaceForProject(projectName)
  const orgNamespace = project?.organization ? namespaceForOrg(project.organization) : ''
  const updateProjectPermission = useMemo(
    () => updateNamespacePermission(projectNamespace),
    [projectNamespace],
  )
  const deleteProjectPermission = useMemo(
    () => deleteNamespacePermission(projectNamespace),
    [projectNamespace],
  )
  const updateOrgPermission = useMemo(
    () => updateNamespacePermission(orgNamespace),
    [orgNamespace],
  )
  const permissionsQuery = useResourcePermissions([
    updateProjectPermission,
    deleteProjectPermission,
    updateOrgPermission,
  ])
  const canWrite = hasPermission(permissionsQuery.data, updateProjectPermission)
  const canDelete = hasPermission(permissionsQuery.data, deleteProjectPermission)
  const canUpdateProjectSettings = hasPermission(permissionsQuery.data, updateOrgPermission)

  // View mode: data or raw
  const [viewMode, setViewMode] = useState<'data' | 'raw'>('data')
  const { data: rawJson } = useGetProjectSettingsRaw(projectName)
  const [includeAllFields, setIncludeAllFields] = useState(false)

  // Display Name inline edit
  const [editingDisplayName, setEditingDisplayName] = useState(false)
  const [draftDisplayName, setDraftDisplayName] = useState('')

  // Description inline edit
  const [editingDescription, setEditingDescription] = useState(false)
  const [draftDescription, setDraftDescription] = useState('')

  // Delete dialog
  const [deleteOpen, setDeleteOpen] = useState(false)

  // Resolve the current parent display text
  const currentParentDisplay = (() => {
    if (!project) return ''
    return `Organization: ${org?.displayName || project.parentName || project.organization}`
  })()

  const handleSaveDisplayName = async () => {
    try {
      await updateProject.mutateAsync({ name: projectName, displayName: draftDisplayName })
      setEditingDisplayName(false)
      toast.success('Saved')
    } catch (err) {
      toast.error(connectErrorMessage(err))
    }
  }

  const handleSaveDescription = async () => {
    try {
      await updateProject.mutateAsync({ name: projectName, description: draftDescription })
      setEditingDescription(false)
      toast.success('Saved')
    } catch (err) {
      toast.error(connectErrorMessage(err))
    }
  }

  const handleSaveSharing = async (userGrants: Grant[], roleGrants: Grant[]) => {
    await updateProjectSharing.mutateAsync({ name: projectName, userGrants, roleGrants })
  }

  const handleSaveDefaultSharing = async (defaultUserGrants: Grant[], defaultRoleGrants: Grant[]) => {
    await updateProjectDefaultSharing.mutateAsync({ name: projectName, defaultUserGrants, defaultRoleGrants })
  }

  const handleDelete = async () => {
    try {
      await deleteProject.mutateAsync({ name: projectName })
      setDeleteOpen(false)
      navigate({ to: '/' })
    } catch (err) {
      toast.error(connectErrorMessage(err))
    }
  }

  const isOwner = canDelete

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

  const displayName = project?.displayName ?? ''
  const description = project?.description ?? ''
  const creatorEmail = project?.creatorEmail ?? ''
  const createdAt = project?.createdAt ?? ''
  const createdAtFormatted = createdAt ? new Date(createdAt).toLocaleString() : 'Unknown'
  const userGrants = (project?.userGrants ?? []) as Grant[]
  const roleGrants = (project?.roleGrants ?? []) as Grant[]
  const defaultUserGrants = (project?.defaultUserGrants ?? []) as Grant[]
  const defaultRoleGrants = (project?.defaultRoleGrants ?? []) as Grant[]

  return (
    <Card>
      <CardContent className="pt-6 space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-muted-foreground">{projectName} / Settings</p>
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
                      disabled={updateProject.isPending}
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
                    {canWrite && (
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label="edit display name"
                        onClick={() => { setDraftDisplayName(displayName); setEditingDisplayName(true) }}
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
                <span className="flex-1 text-sm">{currentParentDisplay}</span>
              </div>

              {/* Name (slug) - read-only */}
              <div className="flex items-center gap-2">
                <span className="w-32 text-sm text-muted-foreground shrink-0">Name (slug)</span>
                <span className="flex-1 text-sm font-mono">{projectName}</span>
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
                        disabled={updateProject.isPending}
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
                        onClick={() => { setDraftDescription(description); setEditingDescription(true) }}
                      >
                        <Pencil className="h-4 w-4" />
                      </Button>
                    )}
                  </>
                )}
              </div>
            </div>

            {/* Features section */}
            <div className="space-y-4">
              <h3 className="text-sm font-medium">Features</h3>
              <Separator />
              <div className="flex items-start justify-between gap-4">
                <div className="space-y-1">
                  <Label htmlFor="deployments-toggle" className="text-sm font-medium">Deployments</Label>
                  <p className="text-sm text-muted-foreground">Enable application deployments in this project.</p>
                </div>
                <Switch
                  id="deployments-toggle"
                  aria-label="Deployments"
                  checked={projectSettings?.deploymentsEnabled ?? false}
                  disabled={!canUpdateProjectSettings || updateProjectSettings.isPending}
                  onCheckedChange={async (checked) => {
                    try {
                      await updateProjectSettings.mutateAsync({ deploymentsEnabled: checked })
                      toast.success('Saved')
                    } catch (err) {
                      toast.error(connectErrorMessage(err))
                    }
                  }}
                />
              </div>
            </div>

            {/* Sharing section */}
            <SharingPanel
              userGrants={userGrants}
              roleGrants={roleGrants}
              isOwner={isOwner}
              onSave={handleSaveSharing}
              isSaving={updateProjectSharing.isPending}
            />

            {/* Default Secret Sharing section */}
            <SharingPanel
              title="Default Secret Sharing"
              description="These grants are automatically applied to every new secret created in this project."
              userGrants={defaultUserGrants}
              roleGrants={defaultRoleGrants}
              isOwner={isOwner}
              onSave={handleSaveDefaultSharing}
              isSaving={updateProjectDefaultSharing.isPending}
            />

            {/* Danger Zone */}
            {isOwner && (
              <div className="space-y-4">
                <h3 className="text-sm font-medium text-destructive">Danger Zone</h3>
                <Separator />
                <Button
                  variant="destructive"
                  onClick={() => setDeleteOpen(true)}
                >
                  Delete Project
                </Button>
              </div>
            )}
          </>
        )}
      </CardContent>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Project</DialogTitle>
            <DialogDescription>
              This will permanently delete {projectName}. This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {deleteProject.error && (
            <Alert variant="destructive">
              <AlertDescription>{connectErrorMessage(deleteProject.error)}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)}>Cancel</Button>
            <Button variant="destructive" onClick={handleDelete} disabled={deleteProject.isPending}>
              {deleteProject.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

    </Card>
  )
}
