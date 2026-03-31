import { useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
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
import { Check, Pencil, X } from 'lucide-react'
import { SharingPanel, type Grant } from '@/components/sharing-panel'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useGetProject, useUpdateProject, useUpdateProjectSharing, useDeleteProject } from '@/queries/projects'

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
  const deleteProject = useDeleteProject()

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
      await updateProject.mutateAsync({ name: projectName, displayName: draftDisplayName })
      setEditingDisplayName(false)
      toast.success('Saved')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  const handleSaveDescription = async () => {
    try {
      await updateProject.mutateAsync({ name: projectName, description: draftDescription })
      setEditingDescription(false)
      toast.success('Saved')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  const handleSaveSharing = async (userGrants: Grant[], roleGrants: Grant[]) => {
    await updateProjectSharing.mutateAsync({ name: projectName, userGrants, roleGrants })
  }

  const handleDelete = async () => {
    try {
      await deleteProject.mutateAsync({ name: projectName })
      setDeleteOpen(false)
      navigate({ to: '/' })
    } catch { /* error shown via mutation */ }
  }

  const isOwner = project?.userRole === Role.OWNER

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
  const userGrants = (project?.userGrants ?? []) as Grant[]
  const roleGrants = (project?.roleGrants ?? []) as Grant[]

  return (
    <Card>
      <CardContent className="pt-6 space-y-6">
        <div>
          <p className="text-sm text-muted-foreground">{projectName} / Settings</p>
          <h2 className="text-xl font-semibold mt-1">Settings</h2>
        </div>

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
            <span className="flex-1 text-sm font-mono">{projectName}</span>
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
          isSaving={updateProjectSharing.isPending}
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
              <AlertDescription>{deleteProject.error.message}</AlertDescription>
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
