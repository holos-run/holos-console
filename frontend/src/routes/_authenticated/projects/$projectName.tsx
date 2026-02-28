import { useState, useMemo } from 'react'
import { createFileRoute, Link, useNavigate, Outlet, useRouterState } from '@tanstack/react-router'
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
import { useAuth } from '@/lib/auth'
import { SharingPanel, type Grant } from '@/components/sharing-panel'
import { RawView } from '@/components/raw-view'
import { useGetProject, useDeleteProject, useUpdateProject, useUpdateProjectSharing } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { ProjectService } from '@/gen/holos/console/v1/projects_pb.js'

export const Route = createFileRoute('/_authenticated/projects/$projectName')({
  component: ProjectPage,
})

function ProjectPage() {
  const { projectName: name } = Route.useParams()
  const navigate = useNavigate()
  const { isAuthenticated, isLoading: authLoading, login } = useAuth()
  const transport = useTransport()

  const { data, isLoading, error } = useGetProject(name)
  const project = data?.project

  const deleteMutation = useDeleteProject()
  const updateMutation = useUpdateProject()
  const updateSharingMutation = useUpdateProjectSharing()

  const projectsClient = useMemo(() => createClient(ProjectService, transport), [transport])

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
  const [localProject, setLocalProject] = useState<typeof project | null>(null)

  // Check if we're rendering a child route (secrets)
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const isChildRoute = pathname !== `/projects/${name}`

  if (!authLoading && !isAuthenticated) {
    login(`/projects/${name}`)
  }

  // If we're on a child route, just render the outlet
  if (isChildRoute) {
    return <Outlet />
  }

  const effectiveProject = localProject ?? project
  const displayName = localDisplayName ?? effectiveProject?.displayName
  const description = localDescription ?? effectiveProject?.description

  const isOwner = effectiveProject?.userRole === Role.OWNER
  const isEditorOrAbove = effectiveProject != null && effectiveProject.userRole >= Role.EDITOR

  const handleSaveDisplayName = async (newDisplayName: string) => {
    try {
      await updateMutation.mutateAsync({ name, displayName: newDisplayName })
      setLocalDisplayName(newDisplayName)
      setEditingDisplayName(false)
    } catch { /* stay in editing mode; updateMutation.error is shown below */ }
  }

  const handleSaveDescription = async (newDescription: string) => {
    try {
      await updateMutation.mutateAsync({ name, description: newDescription })
      setLocalDescription(newDescription)
      setEditingDescription(false)
    } catch { /* stay in editing mode; updateMutation.error is shown below */ }
  }

  const handleSaveSharing = async (newUserGrants: Grant[], newRoleGrants: Grant[]) => {
    const response = await updateSharingMutation.mutateAsync({
      name,
      userGrants: newUserGrants,
      roleGrants: newRoleGrants,
    })
    if (response.project) setLocalProject(response.project)
  }

  const handleViewModeChange = async (newMode: string) => {
    setViewMode(newMode as 'editor' | 'raw')
    if (newMode === 'raw' && rawJson === null) {
      try {
        const response = await projectsClient.getProjectRaw({ name })
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
      navigate({ to: '/projects' })
    } catch { /* dialog stays open; deleteMutation.error is shown in the dialog */ }
  }

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

  const displayError = error || rawError
  if (displayError) {
    const msg = displayError.message.toLowerCase()
    let displayMessage = displayError.message
    if (msg.includes('not found') || msg.includes('not_found')) {
      displayMessage = `Project "${name}" not found`
    } else if (msg.includes('permission') || msg.includes('denied')) {
      displayMessage = 'Permission denied: You are not authorized to view this project'
    }
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive"><AlertDescription>{displayMessage}</AlertDescription></Alert>
        </CardContent>
      </Card>
    )
  }

  if (!effectiveProject) return null

  return (
    <Card>
      <CardContent className="pt-6 space-y-4">
        <p className="text-sm text-muted-foreground">
          {effectiveProject.organization && (
            <Link to="/organizations/$organizationName" params={{ organizationName: effectiveProject.organization }} className="hover:underline">
              {effectiveProject.organization}
            </Link>
          )}
          {effectiveProject.organization && ' / '}
          {effectiveProject.name}
        </p>

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
              <h2 className="text-xl font-semibold flex-1">{displayName || effectiveProject.name}</h2>
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
                placeholder="What is this project for?"
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

        {updateMutation.error && (
          <Alert variant="destructive"><AlertDescription>{updateMutation.error.message}</AlertDescription></Alert>
        )}

        <Tabs value={viewMode} onValueChange={handleViewModeChange}>
          <TabsList>
            <TabsTrigger value="editor">Editor</TabsTrigger>
            <TabsTrigger value="raw">Raw</TabsTrigger>
          </TabsList>
        </Tabs>

        {viewMode === 'raw' && rawJson && (
          <RawView raw={rawJson} includeAllFields={includeAllFields} onToggleIncludeAllFields={() => setIncludeAllFields((p) => !p)} />
        )}

        {viewMode === 'editor' && (
          <div className="flex flex-col sm:flex-row gap-2">
            <Button asChild>
              <Link to="/projects/$projectName/secrets" params={{ projectName: name }}>Secrets</Link>
            </Button>
            {isOwner && (
              <Button variant="destructive" onClick={() => setDeleteOpen(true)}>Delete</Button>
            )}
          </div>
        )}

        <SharingPanel
          userGrants={(localProject ?? effectiveProject).userGrants}
          roleGrants={(localProject ?? effectiveProject).roleGrants}
          isOwner={isOwner}
          onSave={handleSaveSharing}
          isSaving={updateSharingMutation.isPending}
        />
      </CardContent>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Project</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete project &quot;{name}&quot;? This will delete the namespace and all resources within it. This action cannot be undone.
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
